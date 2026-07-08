package agentloop

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/ptr"
)

// AgentSpec defines a sub-agent that can be delegated tasks by the parent agent.
// Builder is called on each invocation with the current AgentContext; callers may
// cache the *Agent internally if construction is expensive.
type AgentSpec struct {
	// Name identifies the sub-agent; used by the LLM to select which agent to call.
	Name         string
	Capabilities string
	Builder      func(AgentContext) (*Agent, error)
}

type subAgentArgs struct {
	AgentName string `json:"agent_name"`
	Task      string `json:"task"`
}

// NewSubAgentTool creates a tool named "task" that allows the agent to delegate
// work to one of the provided sub-agents. Each sub-agent is identified by
// AgentSpec.Name and described to the LLM via AgentSpec.Capabilities.
//
// The compiled *Agent for each spec is lazily initialized on first use and
// reused across subsequent calls.
//
// Note: sub-agents created via this tool are not permitted to create further
// sub-agents. Callers should not include a "task" tool in sub-agent configs.
func NewSubAgentTool(specs ...*AgentSpec) Tool {
	type cachedSpec struct {
		spec  *AgentSpec
		agent *Agent
		mu    sync.Mutex
	}

	getAgent := func(cached *cachedSpec, agentCtx AgentContext) (*Agent, error) {
		cached.mu.Lock()
		defer cached.mu.Unlock()
		if cached.agent == nil {
			agent, err := cached.spec.Builder(agentCtx)
			if err != nil {
				return nil, errs.Wrapf(err, "failed to build sub-agent: %s", cached.spec.Name)
			}
			if agent == nil {
				return nil, errs.NewErrf("builder returned nil agent for: %s", cached.spec.Name)
			}
			cached.agent = agent
		}
		return cached.agent, nil
	}

	index := make(map[string]*cachedSpec, len(specs))
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		index[s.Name] = &cachedSpec{spec: s}
		names = append(names, s.Name)
	}

	var sb strings.Builder
	sb.WriteString("Delegate a task to a specialized sub-agent.\n\nAvailable sub-agents:\n")
	for _, s := range specs {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Capabilities))
	}

	return NewTypedCtxAwareToolFunc[subAgentArgs](
		"task",
		sb.String(),
		map[string]*schema.ParameterInfo{
			"agent_name": StringParamEnum("Name of the sub-agent to delegate to", names, true),
			"task":       StringParam("The task to delegate to the sub-agent", true),
		},
		func(ctx context.Context, agentCtx AgentContext, args subAgentArgs) (string, error) {
			cached, ok := index[args.AgentName]
			if !ok {
				return "", errs.NewErrf("unknown sub-agent: %s", args.AgentName)
			}

			agent, err := getAgent(cached, agentCtx)
			if err != nil {
				return "", err
			}

			out, err := agent.Execute(flow.NewRail(ctx).NextSpanId(), AgentRequest{
				UserInput: args.Task,
			})
			if err != nil {
				return "", errs.Wrapf(err, "sub-agent %s failed", args.AgentName)
			}

			if parentAcc, ok := ctx.Value(tokenAccCtxKey).(*tokenAccumulator); ok && parentAcc != nil {
				tu := out.TokenUsage
				parentAcc.add(tu.PromptTokens, tu.CompletionTokens, tu.CachedTokens)
			}

			return out.Response, nil
		},
	)
}

const defaultExplorerSystemPrompt = `You are a research specialist. Your role is to find accurate, well-sourced information on any topic.

Capabilities:
- Web search and information retrieval
- Official documentation lookup
- Fact-finding and verification
- Synthesizing information from multiple sources

Behavior:
- Provide evidence-based answers; cite sources when possible
- Be concise and direct
- Distinguish between confirmed facts and uncertain claims
- Quote relevant excerpts when helpful`

// NewExplorerAgentSpec creates an AgentSpec for a general-purpose research sub-agent.
// Pass the returned spec to [NewSubAgentTool] to give a parent agent research capabilities.
//
// name identifies the sub-agent; model is the model name, host is the API base URL,
// and apiKey are forwarded to [agents.NewOpenAIChatModel] with [agents.WithBaseURL].
// tools are the search/retrieval tools available to the explorer (e.g. TavilySearch).
// modelOpts are additional optional model configuration overrides
// (e.g. [agents.WithTemperature], [agents.WithMaxToken]).
// File tools are disabled; the explorer is read-only by design.
//
// Example:
//
//	agentloop.NewSubAgentTool(
//	    agentloop.NewExplorerAgentSpec("explorer",
//	        "qwen3-max", agents.AliBailianCnBaseURL, apiKey,
//	        []agentloop.Tool{tavilyTool},
//	        agents.WithTemperature(0.3),
//	    ),
//	)
func NewExplorerAgentSpec(name, model, host, apiKey string, tools []Tool, modelOpts ...agents.OpenAIChatModelOpt) *AgentSpec {
	return &AgentSpec{
		Name:         name,
		Capabilities: "General research and information gathering: web search, documentation lookup, fact-finding, and synthesis.",
		Builder: func(_ AgentContext) (*Agent, error) {
			chatModel, err := agents.NewOpenAIChatModel(model, apiKey, append(modelOpts, agents.WithBaseURL(host))...)
			if err != nil {
				return nil, errs.Wrapf(err, "failed to create explorer chat model")
			}
			return NewAgent(AgentConfig{
				Name:           "Explorer",
				Model:          chatModel,
				MaxRunSteps:    30,
				SystemPrompt:   defaultExplorerSystemPrompt,
				Tools:          tools,
				EnableFileTool: ptr.ValPtr(false),
			})
		},
	}
}
