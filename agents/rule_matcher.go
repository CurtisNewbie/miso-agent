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
	"github.com/curtisnewbie/miso/util/async"
	"github.com/curtisnewbie/miso/util/atom"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/slutil"
	"github.com/curtisnewbie/miso/util/strutil"
)

type RuleMatcher struct {
	ops   *RuleMatcherOps
	graph compose.Runnable[RuleMatcherInput, RuleMatcherOutput]
}

type Rule struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type RuleMatcherInput struct {
	// Additional instruction about the task
	//
	// How should LLM make decision on whether the target matches the given rule or not.
	TaskInstruction string `json:"taskInstruction"`

	// context about the target
	//
	// e.g., if we are running a background check for a company, the context will be the information about the company.
	Context string `json:"context"`

	// Rules
	Rules []Rule `json:"Rules"`

	now atom.Time
}

type RuleResult struct {
	Matched bool   `json:"matched"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
}

type RuleMatcherOutput struct {
	Rules []RuleResult `json:"rules"`
}

type RuleMatcherOps struct {
	genops *GenericOps

	// Injected variables: ${taskInstruction}, ${language}, ${now}
	SystemMessagePrompt string

	// Injected variables: ${rule}, ${context}
	UserMessagePrompt string

	TimeZoneHourOffset float64
}

func NewRuleMatcherOps(g *GenericOps) *RuleMatcherOps {
	return &RuleMatcherOps{
		genops: g,
		SystemMessagePrompt: `
You are a rule matcher. Your task is to carefully review the provided context information and check if the given rule matches for the given context.
${taskInstruction}

You should:
1. Use ${language}
2. Read through the Rules systematically
4. Always use the tool RecordMatchRuleTool to record the information about the rule regardless of whether the rule matches.
5. Be thorough and accurate.

Current Time: ${now}

You will work in an iterative manner, reviewing each rule until all rules are checked.
`,

		UserMessagePrompt: `
<current_rule>
${rule}
</current_rule>

<context>
${context}
</context>
`,
	}
}

func NewRuleMatcher(rail flow.Rail, chatModel model.ToolCallingChatModel, ops *RuleMatcherOps) (*RuleMatcher, error) {
	type RuleMatcherState struct {
		RuleIndex int
		Rules     []RuleResult
		input     RuleMatcherInput
	}
	type toolOutputResult struct {
		ToolOutput *RuleResult
	}
	type shouldContinueResult struct {
		ShouldContinue bool
		Rules          []RuleResult
	}

	g := compose.NewGraph[RuleMatcherInput, RuleMatcherOutput](
		compose.WithGenLocalState(func(ctx context.Context) *RuleMatcherState {
			return &RuleMatcherState{
				RuleIndex: 0,
				Rules:     make([]RuleResult, 0, 5),
			}
		}),
	)

	RecordMatchRuleToolName := "RecordMatchRuleTool"
	RecordMatchRuleTool := utils.NewTool(
		&schema.ToolInfo{
			Name: "RecordMatchRuleTool",
			Desc: "Call this tool to record the information about the rule matched.",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"reason": {
					Type: "string",
					Desc: "How you make your decision",
				},
				"matched": {
					Type: "bool",
					Desc: "Whether current rule matches",
				},
				"name": {
					Type: "string",
					Desc: "current rule name",
				},
			}),
		},
		func(ctx context.Context, input RuleResult) (output RuleResult, err error) {
			return input, nil
		})

	info, err := RecordMatchRuleTool.Info(context.TODO())
	if err != nil {
		return nil, err
	}

	chatModel, err = chatModel.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		return nil, err
	}

	toolNode, err := compose.NewToolNode(context.TODO(), &compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{RecordMatchRuleTool},
	})
	if err != nil {
		return nil, err
	}

	_ = g.AddLambdaNode("prepare_input", compose.InvokableLambda(func(ctx context.Context, in RuleMatcherInput) (RuleMatcherInput, error) {
		err := compose.ProcessState(ctx, func(ctx context.Context, state *RuleMatcherState) error {
			state.input = in
			return nil
		})
		return in, err
	}), compose.WithNodeName("Prepare Input"))

	_ = g.AddLambdaNode("select_rule", compose.InvokableLambda(func(ctx context.Context, _ any) ([]*schema.Message, error) {
		var (
			taskInstruct string
			RuleText     string
			contexts     string
			now          string
		)

		err := compose.ProcessState(ctx, func(ctx context.Context, state *RuleMatcherState) error {
			flow.NewRail(ctx).Infof("Checking Rule: %v, %v", state.RuleIndex, state.input.Rules[state.RuleIndex].Name)
			if state.RuleIndex >= len(state.input.Rules) {
				RuleText = ""
				return nil
			}

			crule := state.input.Rules[state.RuleIndex]
			RuleText = fmt.Sprintf("Rule Name: %s\nRule Content: %s\n", crule.Name, crule.Content)
			now = state.input.now.FormatStdLocale()
			contexts = state.input.Context
			taskInstruct = state.input.TaskInstruction

			return nil
		})
		if err != nil {
			return nil, err
		}
		if RuleText == "" {
			return []*schema.Message{}, nil
		}

		systemMessage := schema.SystemMessage(strings.TrimSpace(strutil.NamedSprintf(ops.SystemMessagePrompt, map[string]any{
			"taskInstruction": taskInstruct,
			"language":        ops.genops.Language,
			"now":             now,
		})))
		userMessage := schema.UserMessage(strings.TrimSpace(strutil.NamedSprintf(ops.UserMessagePrompt, map[string]any{
			"rule":    RuleText,
			"context": contexts,
		})))
		rail.Infof("System Message: %v", systemMessage.Content)
		rail.Infof("User Message: %v", userMessage.Content)

		return []*schema.Message{systemMessage, userMessage}, nil
	}), compose.WithNodeName("Prepare User Message"))

	_ = g.AddChatModelNode("match_rule", chatModel, compose.WithNodeName("Match Rule"))
	_ = g.AddToolsNode("tools", toolNode)

	_ = g.AddLambdaNode("extract_tool_output", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) (toolOutputResult, error) {
		result := toolOutputResult{}
		for _, m := range input {
			if m == nil {
				continue
			}
			if m.ToolName == RecordMatchRuleToolName {
				doneInput, err := llm.ParseLLMJsonAs[RuleResult](m.Content)
				if err != nil {
					rail.Warnf("Failed to parse tool output: %v", err)
					return toolOutputResult{}, nil
				} else {
					rail.Infof("Parsed tool output: %#v", doneInput)
				}
				return toolOutputResult{ToolOutput: &doneInput}, nil
			}
		}
		return result, nil
	}), compose.WithNodeName("Extract Tool Output"))

	_ = g.AddLambdaNode("update_state", compose.InvokableLambda(func(ctx context.Context, in toolOutputResult) (shouldContinueResult, error) {
		var result shouldContinueResult

		err := compose.ProcessState(ctx, func(ctx context.Context, state *RuleMatcherState) error {
			if in.ToolOutput != nil {
				state.Rules = append(state.Rules, *in.ToolOutput)
			}
			state.RuleIndex++
			result.ShouldContinue = state.RuleIndex < len(state.input.Rules)
			result.Rules = state.Rules
			return nil
		})
		if err != nil {
			return shouldContinueResult{}, err
		}

		return result, nil
	}), compose.WithNodeName("Update State"))

	_ = g.AddLambdaNode("final_output", compose.InvokableLambda(func(ctx context.Context, in any) (RuleMatcherOutput, error) {
		if result, ok := in.(shouldContinueResult); ok {
			return RuleMatcherOutput{Rules: result.Rules}, nil
		}
		return RuleMatcherOutput{}, nil
	}), compose.WithNodeName("Final Output"))

	_ = g.AddBranch("update_state", compose.NewGraphBranch(func(ctx context.Context, in shouldContinueResult) (string, error) {
		flow.NewRail(ctx).Infof("Branch: %v", in.ShouldContinue)
		if in.ShouldContinue {
			return "select_rule", nil
		}
		return "final_output", nil
	}, map[string]bool{
		"select_rule":  true,
		"final_output": true,
	}))

	_ = g.AddEdge(compose.START, "prepare_input")
	_ = g.AddEdge("prepare_input", "select_rule")
	_ = g.AddEdge("select_rule", "match_rule")
	_ = g.AddEdge("match_rule", "tools")
	_ = g.AddEdge("tools", "extract_tool_output")
	_ = g.AddEdge("extract_tool_output", "update_state")
	_ = g.AddEdge("final_output", compose.END)

	runnable, err := CompileGraph(rail, ops.genops, g, compose.WithGraphName("RuleMatcher"), compose.WithNodeTriggerMode(compose.AnyPredecessor))
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &RuleMatcher{graph: runnable, ops: ops}, nil
}

// Execute.
//
// If there are lots of rules, use [RuleMatcher.ParallelExecute] instead.
func (b *RuleMatcher) Execute(rail flow.Rail, input RuleMatcherInput) (RuleMatcherOutput, error) {
	if len(input.Rules) < 1 {
		return RuleMatcherOutput{}, nil
	}

	now := atom.Now()
	if b.ops.TimeZoneHourOffset > 0 {
		now = now.InZone(float64(b.ops.TimeZoneHourOffset))
	}
	input.now = now

	cops := []compose.Option{}
	if b.ops.genops.LogOnStart {
		cops = append(cops, WithTraceCallback("RuleMatcher", b.ops.genops.LogInputs))
	}
	out, err := b.graph.Invoke(rail, input, cops...)
	if err != nil {
		return out, err
	}
	return out, nil
}

func (b *RuleMatcher) ParallelExecute(rail flow.Rail, input RuleMatcherInput, batchSize int, pool async.AsyncPool) (RuleMatcherOutput, error) {
	aw := async.NewAwaitFutures[RuleMatcherOutput](pool)
	if batchSize < 1 {
		batchSize = 2
	}
	slutil.SplitSubSlices(input.Rules, batchSize, func(sub []Rule) error {
		aw.SubmitAsync(func() (RuleMatcherOutput, error) {
			return b.Execute(rail.NextSpan(), RuleMatcherInput{
				Context:         input.Context,
				TaskInstruction: input.TaskInstruction,
				Rules:           sub,
			})
		})
		return nil
	})
	r, err := aw.AwaitResultAnyErr()
	if err != nil {
		return RuleMatcherOutput{}, err
	}
	merged := make([]RuleResult, 0, len(input.Rules))
	for _, v := range r {
		merged = append(merged, v.Rules...)
	}
	return RuleMatcherOutput{Rules: merged}, nil
}
