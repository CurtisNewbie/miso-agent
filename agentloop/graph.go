package agentloop

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/graph"
	"github.com/curtisnewbie/miso/util/llm"
)

type agentLoopState struct {
	taskInput taskInput
	messages  []*schema.Message
}

type finalOutput struct {
	response string
}

// finishToolArgs represents the arguments for the finish_tool.
type finishToolArgs struct {
	Response string `json:"response"`
}

// taskInput is the input to the ReAct agent graph.
type taskInput struct {
	task   string
	skills *Skills
	store  FileStore
}

// buildGraph builds the Eino graph for the ReAct agent.
// The graph is compiled once, but backend/skills are passed via taskInput for each execution.
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
		// Build system prompt with skills from taskInput
		promptBuilder := NewPromptBuilder().
			WithCustomPrompt(agent.config.SystemPrompt).
			WithTaskPrompt(agent.config.TaskPrompt).
			WithSkills(input.skills).
			WithLanguage(agent.config.Language).
			WithCurrentTime(GetCurrentTime(agent.config.Timezone)).
			WithFinishToolEnabled(agent.config.EnableFinishTool)

		systemMsg, err := promptBuilder.Build(ctx)
		if err != nil {
			return nil, err
		}

		// Build user message with task
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

	// StatePreHandler: appends new messages to state and returns all accumulated messages
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
		if lastMessage != nil {
			// Check if the last message has a finish_tool call
			if lastMessage.Role == schema.Assistant && len(lastMessage.ToolCalls) > 0 {
				for _, toolCall := range lastMessage.ToolCalls {
					if toolCall.Function.Name == finishToolName {
						// Extract response from finish_tool call
						// Arguments is a JSON string, parse it
						if toolCall.Function.Arguments != "" {
							if args, err := llm.ParseLLMJsonAs[finishToolArgs](toolCall.Function.Arguments); err == nil {
								response = args.Response
							}
						}
						break
					}
				}
			}
			// If no finish_tool response, use the assistant's content
			if response == "" && lastMessage.Role == schema.Assistant {
				response = lastMessage.Content
			}
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
					// Check if finish_tool was called
					if agent.config.EnableFinishTool {
						for _, toolCall := range lastMsg.ToolCalls {
							if toolCall.Function.Name == finishToolName {
								// Finish tool was called, exit the loop
								shouldContinue = false
								return nil
							}
						}
					}
					// Other tool calls, continue loop
					shouldContinue = true
				}
			}
			return nil
		})
		if err != nil {
			return "", err
		}

		// If finish_tool is enabled and no tool calls were made, continue the loop
		// This allows the agent to call finish_tool on the next iteration
		if !shouldContinue && agent.config.EnableFinishTool {
			// Check if we should continue to allow the agent to call finish_tool
			// We continue if the last message doesn't have tool calls and we haven't finished yet
			err := compose.ProcessState(ctx, func(ctx context.Context, state *agentLoopState) error {
				if len(state.messages) > 0 {
					lastMsg := state.messages[len(state.messages)-1]
					if lastMsg.Role == schema.Assistant && len(lastMsg.ToolCalls) == 0 {
						// Assistant made a regular response, continue loop so it can call finish_tool
						shouldContinue = true
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
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
