package agents

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/strutil"
)

type ExecutiveSummaryWriter struct {
	graph compose.Runnable[ExecutiveSummaryWriterInput, *ExecutiveSummaryWriterOutput]
}

type ExecutiveSummaryWriterInput struct {
	Report  string `json:"report"`
	Context string `json:"context"`
}

type ExecutiveSummaryWriterOutput struct {
	Summary string `json:"summary"`
}

type executiveSummaryWriterOps struct {
	SystemMessagePrompt string
	UserMessagePrompt   string
}

func NewExecutiveSummaryWriterOps(o *genericOps) *executiveSummaryWriterOps {
	return &executiveSummaryWriterOps{
		SystemMessagePrompt: strutil.NamedSprintfkv(`
A report has been written. Your task is to analyze the report, understand it's content, and write a short executive summary for the report.

The executive summary should be around 100~200 words.
It must be written in ${language}.
It should capture the most important information from the report.
Do not include any markdown titles (e.g., ##), just output the content.
Use bullet points if necessary.`, "language", o.Language),

		UserMessagePrompt: `
<context>
${context}
</context>

<final_report>
${report}
</final_report>
`,
	}
}

func NewExecutiveSummaryWriter(rail flow.Rail, chatModel model.ToolCallingChatModel, ops *executiveSummaryWriterOps) (*ExecutiveSummaryWriter, error) {

	g := compose.NewGraph[ExecutiveSummaryWriterInput, *ExecutiveSummaryWriterOutput]()

	_ = g.AddLambdaNode("prepare_messages", compose.InvokableLambda(func(ctx context.Context, in ExecutiveSummaryWriterInput) ([]*schema.Message, error) {

		systemMessage := schema.SystemMessage(strings.TrimSpace(ops.SystemMessagePrompt))
		userMessage := schema.UserMessage(strings.TrimSpace(
			strutil.NamedSprintf(ops.UserMessagePrompt, map[string]any{
				"context": in.Context,
				"report":  in.Report,
			})),
		)

		return []*schema.Message{
			systemMessage,
			userMessage,
		}, nil
	}))

	_ = g.AddChatModelNode("generate_summary", chatModel, compose.WithNodeName("ChatModel"))
	_ = g.AddLambdaNode("remove_think", compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (*ExecutiveSummaryWriterOutput, error) {
		_, s := llm.ParseThink(msg.Content)
		return &ExecutiveSummaryWriterOutput{
			Summary: s,
		}, nil
	}))

	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "generate_summary")
	_ = g.AddEdge("generate_summary", "remove_think")
	_ = g.AddEdge("remove_think", compose.END)

	runnable, err := g.Compile(rail)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &ExecutiveSummaryWriter{graph: runnable}, nil
}

func (w *ExecutiveSummaryWriter) Execute(rail flow.Rail, input ExecutiveSummaryWriterInput) (*ExecutiveSummaryWriterOutput, error) {
	return w.graph.Invoke(rail, input)
}
