package agentloop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/curtisnewbie/miso/flow"
)

type agentLoopState struct {
	taskInput         taskInput
	messages          []*schema.Message
	cycleCount        int
	compactionSummary string
}

// shouldContinueLoop reports whether the agent loop should continue after the given assistant message.
// The loop continues when the assistant has tool calls; it stops on a plain-text response.
func shouldContinueLoop(lastMsg *schema.Message) bool {
	return lastMsg.Role == schema.Assistant && len(lastMsg.ToolCalls) > 0
}

// resolveBranchTarget returns the next graph node name.
// "tools" when shouldContinue is true, "final_output" otherwise.
func resolveBranchTarget(shouldContinue bool) string {
	if shouldContinue {
		return "tools"
	}
	return "final_output"
}

// TokenUsage tracks total token consumption across all LLM calls in a single agent execution.
type TokenUsage struct {
	PromptTokens     int // Total input tokens consumed across all LLM calls
	CompletionTokens int // Total output tokens generated across all LLM calls
	TotalTokens      int // PromptTokens + CompletionTokens
}

// Artifact represents a discovered or created artifact during agent execution
type Artifact struct {
	Path        string            // Backend file path
	SizeInBytes int64             // File size in bytes
	Meta        map[string]string // Additional metadata (title, url, etc.)
}

// TaskOutput represents the output from an agent execution
type TaskOutput struct {
	Response   string         // Main response (research report)
	Artifacts  []Artifact     // Artifacts collected during execution
	Metadata   map[string]any // Snapshot of MetadataStore at end of execution
	TokenUsage TokenUsage     // Aggregate token usage across all LLM calls
}

// taskOutput is the internal output type used by the graph
type taskOutput = TaskOutput

// taskInput is the input to the ReAct agent graph.
type taskInput struct {
	task   string
	skills *Skills
	store  FileStore
}

// buildGraph builds the Eino graph for the ReAct agent.
// The graph is compiled once, but backend/skills are passed via taskInput for each execution.
func buildGraph(agent *Agent) (compose.Runnable[taskInput, taskOutput], error) {
	// Compute initial slice capacity: use MaxRunSteps when positive, otherwise a sensible default.
	initialCap := agent.ops.maxRunSteps
	if initialCap <= 0 {
		initialCap = 32
	}

	g := compose.NewGraph[taskInput, taskOutput](
		compose.WithGenLocalState(func(ctx context.Context) *agentLoopState {
			return &agentLoopState{
				messages: make([]*schema.Message, 0, initialCap+1),
			}
		}),
	)

	// Prepare messages node - runs once at start
	_ = g.AddLambdaNode("prepare_messages", compose.InvokableLambda(func(ctx context.Context, input taskInput) ([]*schema.Message, error) {
		fragments := make([]string, 0, len(agent.middleware))
		for _, m := range agent.middleware {
			if f := m.SystemPromptFragment(ctx); f != "" {
				fragments = append(fragments, f)
			}
		}
		promptBuilder := NewPromptBuilder().
			WithCustomPrompt(agent.config.SystemPrompt).
			WithMiddlewareFragments(fragments).
			WithSkills(input.skills).
			WithLanguage(agent.ops.language).
			WithCurrentTime(GetCurrentTime(agent.config.Timezone))
		systemMsg, err := promptBuilder.Build(ctx)
		if err != nil {
			return nil, err
		}

		userMsg := schema.UserMessage(input.task)

		_ = compose.ProcessState(ctx, func(ctx context.Context, st *agentLoopState) error {
			st.taskInput = input
			return nil
		})

		return []*schema.Message{systemMsg, userMsg}, nil
	}), compose.WithNodeName("Prepare Messages"))

	// Chat model node - uses StatePreHandler to manage message accumulation
	toolInfos := agent.tools.ToEinoToolsWithChain(agent.middleware)
	toolInfoList := make([]*schema.ToolInfo, len(toolInfos))
	for i, tool := range toolInfos {
		info, err := tool.Info(context.Background())
		if err != nil {
			return nil, err
		}
		toolInfoList[i] = info
	}

	var chatModel model.ToolCallingChatModel
	if len(toolInfoList) > 0 {
		var err error
		chatModel, err = agent.config.Model.WithTools(toolInfoList)
		if err != nil {
			return nil, err
		}
	} else {
		chatModel = agent.config.Model
	}

	// Wrap chatModel with middleware WrapModelCall chain, if any middleware registered.
	// This preserves AddChatModelNode semantics (callbacks, token tracking) while allowing
	// middleware to intercept model inputs and outputs.
	if len(agent.middleware) > 0 {
		inner := chatModel
		terminal := func(ctx context.Context, req *ModelCallRequest) (*ModelCallResponse, error) {
			msg, err := inner.Generate(ctx, req.Messages)
			if err != nil {
				return nil, err
			}
			return &ModelCallResponse{Message: msg}, nil
		}
		chain := buildModelCallChain(agent.middleware, terminal)
		chatModel = &middlewareModel{ToolCallingChatModel: inner, chain: chain}
	}

	modelPreHandle := func(ctx context.Context, input []*schema.Message, state *agentLoopState) ([]*schema.Message, error) {
		state.cycleCount++
		if agent.ops.enableToolOffload {
			input = offloadToolResults(ctx, input, state.messages, state.taskInput.store, agent.tokenizer, agent.ops.toolOffloadTokenLimit, agent.ops.toolOffloadResultsPathPrefix)
		}
		state.messages = append(state.messages, input...)

		// Compact if MaxTokens is set and exceeded threshold
		if agent.config.MaxTokens > 0 && agent.ops.compaction && agent.tokenizer.CountMessagesTokens(state.messages) > agent.config.MaxTokens-agent.ops.compactBuffer {
			rail := flow.NewRail(ctx)
			toSummarize, toKeep := selectForCompaction(state.messages, agent.tokenizer, agent.ops.compactPreserveRecentTokens)
			if toSummarize == nil && toKeep == nil && len(state.messages) >= 3 {
				rail.Warnf("Compaction skipped: unexpected message structure (messages[0]=%s, messages[1]=%s)", state.messages[0].Role, state.messages[1].Role)
				return state.messages, nil
			}
			if len(toSummarize) > 0 {
				rail.Infof("Compaction started: summarizing %d messages, keeping %d (target preserve_recent_tokens: %v)", len(toSummarize), len(toKeep), agent.ops.compactPreserveRecentTokens)
				summary, err := runCompaction(ctx, agent.config.Model, state.compactionSummary, toSummarize)
				if err == nil && summary != "" {
					state.compactionSummary = summary
					checkpoint := schema.UserMessage(fmt.Sprintf(
						"<conversation-checkpoint>\n<summary>\n%s\n</summary>\n</conversation-checkpoint>",
						summary,
					))

					// Rebuild as [system, original_user_task, checkpoint, recent...]
					// messages[0] and messages[1] are always preserved intact.
					newMessages := make([]*schema.Message, 0, 3+len(toKeep))
					newMessages = append(newMessages, state.messages[0], state.messages[1])
					newMessages = append(newMessages, checkpoint)
					newMessages = append(newMessages, toKeep...)
					state.messages = newMessages
					rail.Infof("Compaction succeeded: summary %d chars, new message set ~%d tokens", len([]rune(summary)), agent.tokenizer.CountMessagesTokens(newMessages))
				} else {
					if err != nil {
						rail.Warnf("Compaction failed: %v", err)
					} else {
						rail.Warnf("Compaction returned empty summary")
					}
				}
			}
		}

		return state.messages, nil
	}

	_ = g.AddChatModelNode("chat_model", chatModel,
		compose.WithStatePreHandler(modelPreHandle),
		compose.WithNodeName("Chat Model"))

	// Update state node - append assistant message from chat_model to state
	_ = g.AddLambdaNode("update_state", compose.InvokableLambda(func(ctx context.Context, input *schema.Message) (*schema.Message, error) {
		err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
			state.messages = append(state.messages, input)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return input, nil
	}), compose.WithNodeName("Update State"))

	if len(toolInfos) > 0 {
		toolNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
			Tools:               toolInfos,
			UnknownToolsHandler: buildUnknownToolHandler(toolInfos),
		})
		if err != nil {
			return nil, err
		}
		_ = g.AddToolsNode("tools", toolNode)
		_ = g.AddEdge("tools", "chat_model")
	}

	// Final output node
	_ = g.AddLambdaNode("final_output", compose.InvokableLambda(func(ctx context.Context, input any) (taskOutput, error) {
		var lastMessage *schema.Message
		err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
			if len(state.messages) > 0 {
				lastMessage = state.messages[len(state.messages)-1]
			}
			return nil
		})
		if err != nil {
			return taskOutput{}, err
		}

		response := ""
		if lastMessage != nil && lastMessage.Role == schema.Assistant {
			response = lastMessage.Content
		}

		// Collect artifacts from AgentContext
		var artifacts []Artifact
		var metadata map[string]any
		if agentCtxVal := ctx.Value(agentCtxKey); agentCtxVal != nil {
			if agentCtx, ok := agentCtxVal.(AgentContext); ok {
				if agentCtx.Artifacts != nil {
					artifacts = agentCtx.Artifacts.ListArtifacts()
				}
				if agentCtx.Metadata != nil {
					metadata = agentCtx.Metadata.All()
				}
			}
		}

		return TaskOutput{
			Response:  response,
			Artifacts: artifacts,
			Metadata:  metadata,
		}, nil
	}), compose.WithNodeName("Final Output"))

	// Branch: continue loop or finish
	if len(toolInfos) > 0 {
		_ = g.AddBranch("update_state", compose.NewGraphBranch(func(ctx context.Context, input *schema.Message) (string, error) {
			shouldContinue := false
			err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
				if len(state.messages) > 0 {
					lastMsg := state.messages[len(state.messages)-1]
					shouldContinue = shouldContinueLoop(lastMsg)
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			return resolveBranchTarget(shouldContinue), nil
		}, map[string]bool{
			"tools":        true,
			"final_output": true,
		}))
	} else {
		_ = g.AddEdge("update_state", "final_output")
	}

	// Add edges - ReAct loop pattern
	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "chat_model")
	_ = g.AddEdge("chat_model", "update_state")
	_ = g.AddEdge("final_output", compose.END)

	compileOpts := []compose.GraphCompileOption{compose.WithGraphName("AgentLoop")}
	if agent.ops.maxRunSteps > 0 {
		compileOpts = append(compileOpts, compose.WithMaxRunSteps(agent.ops.maxRunSteps))
	}
	return g.Compile(context.Background(), compileOpts...)
}

// buildUnknownToolHandler returns a handler that tells the model which tool name it hallucinated
// and lists the names that are actually registered, so it can self-correct on the next turn.
func buildUnknownToolHandler(tools []tool.BaseTool) func(ctx context.Context, name, input string) (string, error) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if info, err := t.Info(context.Background()); err == nil {
			names = append(names, info.Name)
		}
	}
	available := strings.Join(names, ", ")
	return func(ctx context.Context, name, input string) (string, error) {
		return fmt.Sprintf("tool %q does not exist. Available tools: %s", name, available), nil
	}
}

// middlewareModel wraps a model.ToolCallingChatModel to intercept Generate calls
// through the WrapModelCall middleware chain. All other interface methods delegate
// to the embedded inner model.
type middlewareModel struct {
	model.ToolCallingChatModel
	chain ModelCallHandler
}

// Generate runs the WrapModelCall middleware chain and returns the assistant reply.
func (m *middlewareModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	var task string
	_ = compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
		task = state.taskInput.task
		return nil
	})
	resp, err := m.chain(ctx, &ModelCallRequest{Messages: input, Task: task})
	if err != nil {
		return nil, err
	}
	return resp.Message, nil
}
