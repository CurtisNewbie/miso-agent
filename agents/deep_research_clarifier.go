package agents

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/json"
	"github.com/curtisnewbie/miso/util/strutil"
)

type DeepResearchClarifier struct {
	genops *GenericOps
	graph  compose.Runnable[DeepResearchClarifierInput, DeepResearchClarifierOutput]
}

type DeepResearchClarifierInput struct {
	Now          string `json:"now"`
	Conversation string `json:"conversation"`
	Memory       string `json:"memory"`
}

type DeepResearchClarifierOutput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type DeepResearchClarifierOps struct {
	genops *GenericOps

	// Injected variables: ${language}
	SystemMessagePrompt string

	// Injected variables: ${now} ${conversation}, ${memory}
	UserMessagePrompt string
}

func NewDeepResearchClarifierOps(g *GenericOps) *DeepResearchClarifierOps {
	return &DeepResearchClarifierOps{
		genops: g,
		SystemMessagePrompt: `
You are a research assistant, you are given a historical conversation between you and the user.
Your task is to analyze the conversation, guess what are the research title and description that user wants, and use the tool 'FillResearchInfo' to fill in the fields.

# Requirements
1. Previous conversation may include what have been decided to be the research title and description, if so, just use the ones mentioned in the conversation.
2. You should only focus the most recent conversation, if conversation contains multiple topics, only pick the last one. Do not attempt to include everything.
3. The generated research title and description are mainly suggestion for user's convenience, user may modify them if necessary.
4. If you don't know what user wants, leave the field empty.
5. It must be written in ${language}.
`,
		UserMessagePrompt: `
<now>
${now}
</now>

<conversation>
${conversation}
</conversation>

<memory>
${memory}
</memory>
`,
	}
}

func NewDeepResearchClarifier(rail flow.Rail, chatModel model.ToolCallingChatModel, ops *DeepResearchClarifierOps) (*DeepResearchClarifier, error) {

	g := compose.NewGraph[DeepResearchClarifierInput, DeepResearchClarifierOutput]()

	_ = g.AddLambdaNode("prepare_messages", compose.InvokableLambda(func(ctx context.Context, in DeepResearchClarifierInput) ([]*schema.Message, error) {

		systemMessage := schema.SystemMessage(strings.TrimSpace(
			strutil.NamedSprintf(ops.SystemMessagePrompt, map[string]any{
				"language": ops.genops.Language,
			})))
		userMessage := schema.UserMessage(strings.TrimSpace(
			strutil.NamedSprintf(ops.UserMessagePrompt, map[string]any{
				"conversation": in.Conversation,
				"memory":       in.Memory,
			})),
		)
		rail.Debugf("System Message: %v", systemMessage.Content)
		rail.Debugf("User Message: %v", userMessage.Content)

		if ops.genops.RepeatPrompt {
			return []*schema.Message{
				systemMessage,
				userMessage,
				systemMessage,
				userMessage,
			}, nil
		}

		return []*schema.Message{
			systemMessage,
			userMessage,
		}, nil
	}), compose.WithNodeName("Prepare Messages"))

	type FillResearchInfoInput struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}

	fillResearchInfoTool := utils.NewTool(
		&schema.ToolInfo{
			Name: "FillResearchInfo",
			Desc: "Fill in Deep Research Title and Description",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"title": {
					Type: "string",
					Desc: "Research Title (less then 100 characters)",
				},
				"description": {
					Type: "string",
					Desc: "Research Description (less then 300 characters)",
				},
			}),
		},
		func(ctx context.Context, input FillResearchInfoInput) (output DeepResearchClarifierOutput, err error) {
			return DeepResearchClarifierOutput(input), nil
		})

	info, err := fillResearchInfoTool.Info(context.TODO())
	if err != nil {
		return nil, err
	}

	chatModel, err = chatModel.WithTools([]*schema.ToolInfo{info})
	if err != nil {
		return nil, err
	}

	toolNode, err := compose.NewToolNode(context.TODO(), &compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{
			fillResearchInfoTool,
		},
	})
	if err != nil {
		return nil, err
	}
	_ = g.AddToolsNode("tools", toolNode)
	_ = g.AddChatModelNode("clarify_generate_research", chatModel, compose.WithNodeName("Generate Summary"))
	_ = g.AddLambdaNode("extract_tool_output", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) (output DeepResearchClarifierOutput, err error) {
		o := DeepResearchClarifierOutput{}
		for _, m := range input {
			if m == nil {
				continue
			}
			if m.ToolName == "FillResearchInfo" {
				if err := json.SParseJson(m.Content, &o); err != nil {
					return DeepResearchClarifierOutput{}, err
				}
				return o, nil
			}
		}
		return o, nil
	}), compose.WithNodeName("Extract Tool Output"))

	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "clarify_generate_research")
	_ = g.AddEdge("clarify_generate_research", "tools")
	_ = g.AddEdge("tools", "extract_tool_output")
	_ = g.AddEdge("extract_tool_output", compose.END)

	runnable, err := CompileGraph(rail, ops.genops, g, compose.WithGraphName("DeepResearchClarifier"))
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &DeepResearchClarifier{graph: runnable, genops: ops.genops}, nil
}

func (w *DeepResearchClarifier) Execute(rail flow.Rail, input DeepResearchClarifierInput) (DeepResearchClarifierOutput, error) {
	start := time.Now()
	defer rail.TimeOp(start, "DeepResearchClarifier")

	cops := []compose.Option{}
	if w.genops.LogOnStart {
		cops = append(cops, WithTraceCallback("DeepResearchClarifier", w.genops.LogInputs))
	}
	return w.graph.Invoke(rail, input, cops...)
}
