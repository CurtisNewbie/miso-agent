package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/atom"
	"github.com/curtisnewbie/miso/util/hash"
	"github.com/curtisnewbie/miso/util/json"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/slutil"
	"github.com/curtisnewbie/miso/util/strutil"
)

type MaterialExtract struct {
	ops   *MaterialExtractOps
	graph compose.Runnable[MaterialExtractInput, MaterialExtractOutput]
}

type Material struct {
	Content string `json:"content"`
	Source  string `json:"source"`
}

type MaterialExtractInput struct {
	Context   string             `json:"context"`
	Materials []Material         `json:"materials"`
	Fields    []ExtractFieldSpec `json:"fields"`
	now       atom.Time
}

type ExtractFieldSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

type ExtractFieldSpecs []ExtractFieldSpec

func (e ExtractFieldSpecs) format() string {
	if len(e) == 0 {
		return "No specific fields specified. Extract all relevant information."
	}

	sb := strutil.NewBuilder()
	for i, f := range e {
		if f.Example != "" {
			sb.Printlnf("%d. %s: %s (E.g., %s)", i+1, f.Name, f.Description, strutil.SAddLineIndent(f.Example, " "))
		} else {
			sb.Printlnf("%d. %s: %s", i+1, f.Name, f.Description)
		}
	}
	return sb.String()
}

type MaterialExtractOutput struct {
	ExtractedInfo map[string]string `json:"extractedInfo"`
}

type MaterialExtractOps struct {
	genops *GenericOps

	// Injected variables: ${context}, ${language}, ${now}
	SystemMessagePrompt string

	// Injected variables: ${materials}, ${fields}, ${extractedInfo}
	UserMessagePrompt string

	TimeZoneHourOffset float64
}

func NewMaterialExtractOps(g *GenericOps) *MaterialExtractOps {
	return &MaterialExtractOps{
		genops: g,
		SystemMessagePrompt: `
You are a information extraction assistant. Your task is to carefully review the provided materials and extract specific information as requested.
${context}

You should:
1. Use ${language}
2. Read through the materials systematically
3. Extract missing information based on the requirements
4. Use the fillExtractedInfoTool to fill in the extracted information
5. Be thorough and accurate in your extraction

Current Time: ${now}

IMPORTANT: When calling fillExtractedInfoTool, you must provide the extracted information as a JSON object (key-value pairs, both of the key and value MUST BE string), NOT an array.
Example of correct fillExtractedInfoTool call:
- extractedInfo: {"field1": "value1", "field2": "value2"}

Example of INCORRECT fillExtractedInfoTool call:
- extractedInfo: ["field1", "value1", "field2", "value2"] <-- This is WRONG, don't use arrays!

You will work in an iterative manner, reviewing materials and extracting information until all materials are reviewed.
`,

		UserMessagePrompt: `
<current_material>
${material}
</current_material>

<fields_to_extract>
${fields}
</fields_to_extract>

<already_extracted>
${extractedInfo}
</already_extracted>
`,
	}
}

func NewMaterialExtract(rail flow.Rail, chatModel model.ToolCallingChatModel, ops *MaterialExtractOps) (*MaterialExtract, error) {
	type materialExtractState struct {
		materialIndex int
		extractedInfo map[string]string
		input         MaterialExtractInput
	}
	type toolOutputResult struct {
		ExtractedInfo map[string]string
	}
	type shouldContinueResult struct {
		ShouldContinue bool
		ExtractedInfo  map[string]string
	}
	type ExtractToolInput struct {
		ExtractedInfo map[string]string `json:"extractedInfo"`
	}

	g := compose.NewGraph[MaterialExtractInput, MaterialExtractOutput](
		compose.WithGenLocalState(func(ctx context.Context) *materialExtractState {
			return &materialExtractState{
				materialIndex: 0,
				extractedInfo: map[string]string{},
			}
		}),
	)

	fillExtractedInfoToolName := "fillExtractedInfoTool"
	fillExtractedInfoTool := utils.NewTool(
		&schema.ToolInfo{
			Name: fillExtractedInfoToolName,
			Desc: "Call this tool fill in the extracted information.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"extractedInfo": {
					Type: "object",
					Desc: "Extracted information in JSON format",
				},
			}),
		},
		func(ctx context.Context, input ExtractToolInput) (output MaterialExtractOutput, err error) {
			return MaterialExtractOutput{ExtractedInfo: input.ExtractedInfo}, nil
		})

	info, err := fillExtractedInfoTool.Info(context.TODO())
	if err != nil {
		return nil, err
	}

	chatModel, err = chatModel.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		return nil, err
	}

	toolNode, err := compose.NewToolNode(context.TODO(), &compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{fillExtractedInfoTool},
	})
	if err != nil {
		return nil, err
	}

	_ = g.AddLambdaNode("prepare_input", compose.InvokableLambda(func(ctx context.Context, in MaterialExtractInput) (MaterialExtractInput, error) {
		err := compose.ProcessState(ctx, func(ctx context.Context, state *materialExtractState) error {
			state.input = in
			return nil
		})
		return in, err
	}), compose.WithNodeName("Prepare Input"))

	_ = g.AddLambdaNode("select_material", compose.InvokableLambda(func(ctx context.Context, _ any) ([]*schema.Message, error) {
		var (
			materialText  string
			extractedInfo string
			fields        string
			contexts      string
			now           string
		)

		err := compose.ProcessState(ctx, func(ctx context.Context, state *materialExtractState) error {
			flow.NewRail(ctx).Infof("Reading material: %v, %v", state.materialIndex, state.input.Materials[state.materialIndex].Source)
			if state.materialIndex >= len(state.input.Materials) {
				materialText = ""
				return nil
			}

			currentMaterial := state.input.Materials[state.materialIndex]
			materialText = fmt.Sprintf("Material %d (Source: %s):\n%s\n", state.materialIndex+1, currentMaterial.Source, currentMaterial.Content)
			extractedInfo = json.TrySWriteJson(state.extractedInfo)
			fields = ExtractFieldSpecs(state.input.Fields).format()

			ctxbd := strutil.NewBuilder()
			if state.input.Context != "" {
				ctxbd.WriteRune('\n')
				ctxbd.WriteString(state.input.Context)
			}
			contexts = ctxbd.String()
			now = state.input.now.FormatStdLocale()

			return nil
		})
		if err != nil {
			return nil, err
		}
		if materialText == "" {
			return []*schema.Message{}, nil
		}

		systemMessage := schema.SystemMessage(strings.TrimSpace(strutil.NamedSprintf(ops.SystemMessagePrompt, map[string]any{
			"context":  contexts,
			"language": ops.genops.Language,
			"now":      now,
		})))
		userMessage := schema.UserMessage(strings.TrimSpace(strutil.NamedSprintf(ops.UserMessagePrompt, map[string]any{
			"material":      materialText,
			"fields":        fields,
			"extractedInfo": extractedInfo,
		})))
		rail.Infof("System Message: %v", systemMessage.Content)
		rail.Infof("User Message: %v", userMessage.Content)

		return []*schema.Message{systemMessage, userMessage}, nil
	}), compose.WithNodeName("Prepare User Message"))

	_ = g.AddChatModelNode("extract_info", chatModel, compose.WithNodeName("Extract Info"))
	_ = g.AddToolsNode("tools", toolNode)

	_ = g.AddLambdaNode("extract_tool_output", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) (toolOutputResult, error) {
		result := toolOutputResult{}
		for _, m := range input {
			if m == nil {
				continue
			}
			if m.ToolName == fillExtractedInfoToolName {
				doneInput, err := llm.ParseLLMJsonAs[ExtractToolInput](m.Content)
				if err != nil {
					rail.Warnf("Failed to parse tool output: %v", err)
					return toolOutputResult{ExtractedInfo: map[string]string{}}, nil
				} else {
					rail.Infof("Parsed tool output: %#v", doneInput)
				}
				return toolOutputResult{ExtractedInfo: doneInput.ExtractedInfo}, nil
			}
		}
		return result, nil
	}), compose.WithNodeName("Extract Tool Output"))

	_ = g.AddLambdaNode("update_state", compose.InvokableLambda(func(ctx context.Context, in toolOutputResult) (shouldContinueResult, error) {
		var result shouldContinueResult

		err := compose.ProcessState(ctx, func(ctx context.Context, state *materialExtractState) error {
			if len(in.ExtractedInfo) > 0 {
				merged := hash.NewSet[string]()
				for k, v := range in.ExtractedInfo {
					if v != "" {
						if strings.HasSuffix(k, "Reason") {
							continue // skip reason field in first iteration
						}
						state.extractedInfo[k] = v
						merged.Add(k)
					}
				}

				// only update reason field when the original field is written
				for k := range merged.All() {
					rk := k + "Reason"
					state.extractedInfo[rk] = in.ExtractedInfo[rk]
				}

				// update reason fields when the original fields are empty
				missing := hash.NewSet[string]()
				for k, v := range state.extractedInfo {
					if strings.HasSuffix(k, "Reason") {
						continue // skip reason field in first iteration
					}
					if v == "" {
						missing.Add(k)
					}
				}
				for k := range missing.All() {
					// we only update the reason fields, explaining why the field is missing
					kr := k + "Reason"
					v, ok := state.extractedInfo[kr]
					if !ok {
						continue
					}
					state.extractedInfo[kr] = v
				}
			}
			state.materialIndex++
			result.ShouldContinue = state.materialIndex < len(state.input.Materials)
			result.ExtractedInfo = state.extractedInfo
			return nil
		})
		if err != nil {
			return shouldContinueResult{}, err
		}

		return result, nil
	}), compose.WithNodeName("Update State"))

	_ = g.AddLambdaNode("final_output", compose.InvokableLambda(func(ctx context.Context, in any) (MaterialExtractOutput, error) {
		if result, ok := in.(shouldContinueResult); ok {
			return MaterialExtractOutput{ExtractedInfo: result.ExtractedInfo}, nil
		}
		return MaterialExtractOutput{ExtractedInfo: map[string]string{}}, nil
	}), compose.WithNodeName("Final Output"))

	_ = g.AddBranch("update_state", compose.NewGraphBranch(func(ctx context.Context, in shouldContinueResult) (string, error) {
		flow.NewRail(ctx).Infof("Branch: %v", in.ShouldContinue)
		if in.ShouldContinue {
			return "select_material", nil
		}
		return "final_output", nil
	}, map[string]bool{
		"select_material": true,
		"final_output":    true,
	}))

	_ = g.AddEdge(compose.START, "prepare_input")
	_ = g.AddEdge("prepare_input", "select_material")
	_ = g.AddEdge("select_material", "extract_info")
	_ = g.AddEdge("extract_info", "tools")
	_ = g.AddEdge("tools", "extract_tool_output")
	_ = g.AddEdge("extract_tool_output", "update_state")
	_ = g.AddEdge("final_output", compose.END)

	runnable, err := CompileGraph(rail, ops.genops, g, compose.WithGraphName("MaterialExtract"), compose.WithNodeTriggerMode(compose.AnyPredecessor))
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &MaterialExtract{graph: runnable, ops: ops}, nil
}

func (b *MaterialExtract) Execute(rail flow.Rail, input MaterialExtractInput) (MaterialExtractOutput, error) {
	if len(input.Materials) < 1 {
		return MaterialExtractOutput{}, nil
	}

	now := atom.Now()
	if b.ops.TimeZoneHourOffset > 0 {
		now = now.InZone(float64(b.ops.TimeZoneHourOffset))
	}
	input.now = now

	// append extra reason fields for existing fields
	reasonFields := make([]ExtractFieldSpec, 0, len(input.Fields))
	for _, f := range input.Fields {
		reasonFields = append(reasonFields, ExtractFieldSpec{Name: f.Name + "Reason", Description: "Based on what and how you extract field " + f.Name})
	}
	input.Fields = append(reasonFields, input.Fields...)

	cops := []compose.Option{}
	if b.ops.genops.LogOnStart {
		cops = append(cops, WithTraceCallback("MaterialExtract", b.ops.genops.LogInputs))
	}
	out, err := b.graph.Invoke(rail, input, cops...)
	if err != nil {
		return out, err
	}

	required := hash.NewSet[string](slutil.MapTo(input.Fields, func(f ExtractFieldSpec) string { return f.Name })...)
	if out.ExtractedInfo == nil {
		out.ExtractedInfo = map[string]string{}
	}
	for k := range required.All() {
		if _, ok := out.ExtractedInfo[k]; !ok {
			out.ExtractedInfo[k] = ""
		}
	}
	return out, nil
}
