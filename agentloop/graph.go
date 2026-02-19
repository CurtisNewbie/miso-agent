package agentloop

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/graph"
)

type agentLoopState struct {
	messages []*schema.Message
}

type finalOutput struct {
	response string
}

// taskInput is the input to the ReAct agent graph.
type taskInput struct {
	task string
}

// buildGraph builds the Eino graph for the ReAct agent.
func buildGraph(agent *Agent) (compose.Runnable[taskInput, finalOutput], error) {
	g := compose.NewGraph[taskInput, finalOutput](
		compose.WithGenLocalState(func(ctx context.Context) *agentLoopState {
			return &agentLoopState{
				messages: make([]*schema.Message, 0, agent.config.MaxRunSteps+1),
			}
		}),
	)

	// Prepare messages node - runs once at start
	_ = g.AddLambdaNode("prepare_messages", compose.InvokableLambda(func(ctx context.Context, input taskInput) ([]*schema.Message, error) {
		// Build system prompt
		promptBuilder := NewPromptBuilder().
			WithCustomPrompt(agent.config.SystemPrompt).
			WithTaskPrompt(agent.config.TaskPrompt).
			WithSkills(agent.skills).
			WithLanguage(agent.config.Language).
			WithCurrentTime(GetCurrentTime(agent.config.Timezone))

		systemMsg, err := promptBuilder.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Build user message with task
		userMsg := schema.UserMessage(input.task)

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

	// StatePreHandler: appends new messages to state and returns all accumulated messages
	modelPreHandle := func(ctx context.Context, input []*schema.Message, state *agentLoopState) ([]*schema.Message, error) {
		state.messages = append(state.messages, input...)

		// Evict large tool results if configured
		if agent.config.EvictToolResultsThreshold > 0 && agent.tokenizer != nil {
			state.messages = agent.evictLargeToolResults(state.messages)
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
	_ = g.AddLambdaNode("final_output", compose.InvokableLambda(func(ctx context.Context, input any) (finalOutput, error) {
		var lastMessage *schema.Message
		err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
			if len(state.messages) > 0 {
				lastMessage = state.messages[len(state.messages)-1]
			}
			return nil
		})

		if err != nil {
			return finalOutput{}, err
		}

		response := ""
		if lastMessage != nil && lastMessage.Role == schema.Assistant {
			response = lastMessage.Content
		}

		return finalOutput{response: response}, nil
	}), compose.WithNodeName("Final Output"))

	// Branch: continue loop or finish
	_ = g.AddBranch("update_state", compose.NewGraphBranch(func(ctx context.Context, input *schema.Message) (string, error) {
		shouldContinue := false
		err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
			// Check if the last message has tool calls
			if len(state.messages) > 0 {
				lastMsg := state.messages[len(state.messages)-1]
				if lastMsg.Role == schema.Assistant && len(lastMsg.ToolCalls) > 0 {
					shouldContinue = true
				}
			}
			return nil
		})
		if err != nil {
			return "", err
		}

		if shouldContinue {
			return "tools", nil
		}
		return "final_output", nil
	}, map[string]bool{
		"tools":        true,
		"final_output": true,
	}))

	// Add edges - ReAct loop pattern (matching Eino)
	_ = g.AddEdge(compose.START, "prepare_messages")
	_ = g.AddEdge("prepare_messages", "chat_model")
	_ = g.AddEdge("chat_model", "update_state")

	// Loop back: tools → chat_model
	_ = g.AddEdge("tools", "chat_model")

	// Finish: final_output → END
	_ = g.AddEdge("final_output", compose.END)

	return graph.CompileGraph(agent.config.GenericOps, g, compose.WithGraphName("AgentLoop"))
}
