package agentloop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
)

// AgentSpec defines a sub-agent that can be delegated tasks by the parent agent.
// Builder is called on each invocation with the current AgentContext; callers may
// cache the *Agent internally if construction is expensive.
type AgentSpec struct {
	Capabilities string
	Builder      func(AgentContext) (*Agent, error)
}

type subAgentArgs struct {
	AgentName string `json:"agent_name"`
	Task      string `json:"task"`
}

// NewSubAgentTool creates a tool named "task" that allows the agent to delegate
// work to one of the provided sub-agents. Each sub-agent is identified by the
// map key and described to the LLM via AgentSpec.Capabilities.
//
// Note: sub-agents created via this tool are not permitted to create further
// sub-agents. Callers should not include a "task" tool in sub-agent configs.
func NewSubAgentTool(specs map[string]*AgentSpec) Tool {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}

	var sb strings.Builder
	sb.WriteString("Delegate a task to a specialized sub-agent.\n\nAvailable sub-agents:\n")
	for _, name := range names {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", name, specs[name].Capabilities))
	}

	return NewTypedCtxAwareToolFunc[subAgentArgs](
		"task",
		sb.String(),
		map[string]*schema.ParameterInfo{
			"agent_name": StringParamEnum("Name of the sub-agent to delegate to", names, true),
			"task":       StringParam("The task to delegate to the sub-agent", true),
		},
		func(ctx context.Context, agentCtx AgentContext, args subAgentArgs) (string, error) {
			spec, ok := specs[args.AgentName]
			if !ok {
				return "", errs.NewErrf("unknown sub-agent: %s", args.AgentName)
			}

			agent, err := spec.Builder(agentCtx)
			if err != nil {
				return "", errs.Wrapf(err, "failed to build sub-agent: %s", args.AgentName)
			}
			if agent == nil {
				return "", errs.NewErrf("builder returned nil agent for: %s", args.AgentName)
			}

			out, err := agent.Execute(flow.NewRail(ctx), AgentRequest{
				UserInput: args.Task,
			})
			if err != nil {
				return "", errs.Wrapf(err, "sub-agent %s failed", args.AgentName)
			}

			return out.Response, nil
		},
	)
}
