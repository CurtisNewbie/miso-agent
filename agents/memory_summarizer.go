package agents

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/llm"
	"github.com/curtisnewbie/miso/util/strutil"
)

type MemorySummarizer struct {
	genops *GenericOps
	graph  compose.Runnable[MemorySummarizerInput, MemorySummarizerOutput]
}

type MemorySummarizerInput struct {
	LongTermMemory     string `json:"longTermMemory"`
	RecentConversation string `json:"recentConversation"`
}

type MemorySummarizerOutput struct {
	Summary string `json:"summary"`
}

type MemorySummarizerOps struct {
	genops *GenericOps

	// Injected variables: ${language}
	SystemMessagePrompt string

	// Injected variables: ${context}, ${report}
	UserMessagePrompt string
}

func NewMemorySummarizerOps(g *GenericOps) *MemorySummarizerOps {
	return &MemorySummarizerOps{
		genops: g,
		SystemMessagePrompt: `
Your task is to create a short but context rich summary of the conversation so far, paying close attention to the user's explicit requests and your previous actions.

This summary should be thorough in capturing user's requests, intents, interests and requirements that would be essential for continuing the conversation without losing context. This summary will replace the given <long_term_memory>.

Your summary should include the following sections:

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Explicit Requirements
   - [Requirement 1]
   - [Requirement 2]

2. Key Concepts:
   - [Concept 1]
   - [Concept 2]
   - [...]

3. Problem Solving:
   [Description of solved problems and ongoing troubleshooting]

4. Pending Tasks:
   - [Task 1]
   - [Task 2]
   - [...]

5. Current Work:
   [Precise description of current work]
</summary>

Please provide your summary based on the conversation so far, following this structure and ensuring precision and thoroughness in your response.
`,

		UserMessagePrompt: `
<recent_conversation>
${recent_conversation}
</recent_conversation>

<long_term_memory>
${long_term_memory}
</long_term_memory>
`,
	}
}

func NewMemorySummarizer(rail flow.Rail, chatModel model.ToolCallingChatModel, ops *MemorySummarizerOps) (*MemorySummarizer, error) {

	g := compose.NewGraph[MemorySummarizerInput, MemorySummarizerOutput]()

	_ = g.AddLambdaNode("prepare_messages", compose.InvokableLambda(func(ctx context.Context, in MemorySummarizerInput) ([]*schema.Message, error) {

		systemMessage := schema.SystemMessage(strings.TrimSpace(ops.SystemMessagePrompt))
		userMessage := schema.UserMessage(strings.TrimSpace(
			strutil.NamedSprintf(ops.UserMessagePrompt, map[string]any{
				"recent_conversation": in.RecentConversation,
				"long_term_memory":    in.LongTermMemory,
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

	_ = g.AddChatModelNode("compact_memory", chatModel, compose.WithNodeName("Compact Memory"))
	_ = g.AddLambdaNode("remove_think", compose.InvokableLambda(func(ctx context.Context, msg *schema.Message) (MemorySummarizerOutput, error) {
		_, s := llm.ParseThink(msg.Content)
		return MemorySummarizerOutput{Summary: s}, nil
	}), compose.WithNodeName("Remove Think"))

	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "compact_memory")
	_ = g.AddEdge("compact_memory", "remove_think")
	_ = g.AddEdge("remove_think", compose.END)

	runnable, err := CompileGraph(rail, ops.genops, g, compose.WithGraphName("MemorySummarizer"))
	if err != nil {
		return nil, errs.Wrap(err)
	}

	return &MemorySummarizer{graph: runnable, genops: ops.genops}, nil
}

func (w *MemorySummarizer) Execute(rail flow.Rail, input MemorySummarizerInput) (MemorySummarizerOutput, error) {
	start := time.Now()
	defer rail.TimeOp(start, "MemorySummarizer")

	cops := []compose.Option{}
	if w.genops.LogOnStart {
		cops = append(cops, WithTraceCallback("MemorySummarizer", w.genops.LogInputs))
	}
	return w.graph.Invoke(rail, input, cops...)
}
