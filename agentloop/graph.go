package agentloop

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/graph"
)

type agentLoopState struct {
	taskInput taskInput
	messages  []*schema.Message
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

// Artifact represents a discovered or created artifact during agent execution
type Artifact struct {
	Path        string            // Backend file path
	SizeInBytes int64             // File size in bytes
	Meta        map[string]string // Additional metadata (title, url, etc.)
}

// TaskOutput represents the output from an agent execution
type TaskOutput struct {
	Response  string     // Main response (research report)
	Artifacts []Artifact // Artifacts collected during execution
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
	initialCap := agent.genops.MaxRunSteps
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
		promptBuilder := NewPromptBuilder().
			WithCustomPrompt(agent.config.SystemPrompt).
			WithTaskPrompt(agent.config.TaskPrompt).
			WithSkills(input.skills).
			WithLanguage(agent.genops.Language).
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
	toolInfos := agent.tools.ToEinoTools()
	toolInfoList := make([]*schema.ToolInfo, len(toolInfos))
	for i, tool := range toolInfos {
		info, err := tool.Info(context.Background())
		if err != nil {
			return nil, err
		}
		toolInfoList[i] = info
	}

	chatModel, err := agent.config.Model.WithTools(toolInfoList)
	if err != nil {
		return nil, err
	}

	modelPreHandle := func(ctx context.Context, input []*schema.Message, state *agentLoopState) ([]*schema.Message, error) {
		state.messages = append(state.messages, input...)

		// Evict large tool results if configured
		if agent.config.EvictToolResultsThreshold > 0 && agent.tokenizer != nil {
			state.messages = agent.evictLargeToolResults(state.taskInput.store, state.messages)
		}

		// Prune messages if MaxTokens is set and exceeded
		if agent.config.MaxTokens > 0 && agent.tokenizer != nil {
			state.messages = agent.tokenizer.PruneMessagesToTokenLimit(state.messages, agent.config.MaxTokens)
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

	// Tools node - executes tool calls
	toolNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools: toolInfos,
	})
	if err != nil {
		return nil, err
	}
	_ = g.AddToolsNode("tools", toolNode)

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
		if agentCtxVal := ctx.Value(agentCtxKey); agentCtxVal != nil {
			if agentCtx, ok := agentCtxVal.(AgentContext); ok && agentCtx.Artifacts != nil {
				artifacts = agentCtx.Artifacts.ListArtifacts()
			}
		}

		return TaskOutput{
			Response:  response,
			Artifacts: artifacts,
		}, nil
	}), compose.WithNodeName("Final Output"))

	// Branch: continue loop or finish
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

	// Add edges - ReAct loop pattern
	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "chat_model")
	_ = g.AddEdge("chat_model", "update_state")
	_ = g.AddEdge("tools", "chat_model")
	_ = g.AddEdge("final_output", compose.END)

	return graph.CompileGraph(agent.genops, g, compose.WithGraphName("AgentLoop"))
}
