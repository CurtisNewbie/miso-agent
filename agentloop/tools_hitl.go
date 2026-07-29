package agentloop

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

type interruptForHumanArgs struct {
	Reason string `json:"reason"`
}

// NewInterruptForHumanTool creates the built-in interrupt_for_human tool.
// The LLM calls this tool to pause execution and wait for human input.
func NewInterruptForHumanTool() Tool {
	return NewTypedCtxAwareToolFunc(
		"interrupt_for_human",
		"Pause the current task and wait for a human to perform an action before continuing. "+
			"Use when human intervention is required: completing MFA, providing credentials, "+
			"approving a sensitive action, or supplying information the agent cannot obtain autonomously. "+
			"The agent state is persisted and execution resumes when the human sends a follow-up message.",
		map[string]*schema.ParameterInfo{
			"reason": StringParam("Describe what the user needs to do before the agent can continue (e.g. 'Please complete MFA at https://...')", true),
		},
		func(ctx context.Context, agentCtx AgentContext, args interruptForHumanArgs) (string, error) {
			RequestHitlInterrupt(agentCtx, args.Reason)
			return "Execution paused. Waiting for human to complete: " + args.Reason, nil
		},
	)
}
