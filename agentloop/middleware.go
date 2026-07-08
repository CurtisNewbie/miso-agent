package agentloop

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Middleware is the core extension point for the agent loop.
// All methods have default no-op implementations via BaseMiddleware —
// embed it and override only what you need.
type Middleware interface {
	// Name returns a unique identifier for this middleware.
	// Used as the key for per-middleware private state storage.
	Name() string

	// BeforeAgent is called once at the start of Agent.Execute(),
	// before the graph begins running. Returning a non-nil error aborts execution.
	BeforeAgent(agentCtx AgentContext) error

	// WrapModelCall is called on every LLM invocation in the loop.
	// Use it to inject prompt fragments, filter messages, or observe the model response.
	WrapModelCall(ctx context.Context, req *ModelCallRequest, next ModelCallHandler) (*ModelCallResponse, error)

	// WrapToolCall is called for every tool execution.
	// Use it to intercept results, enforce permissions, or log tool calls.
	WrapToolCall(ctx context.Context, req *ToolCallRequest, next ToolCallHandler) (*ToolCallResponse, error)

	// AfterAgent is called once after Agent.Execute() finishes (success or error).
	// Errors from AfterAgent are logged but do not suppress the result.
	AfterAgent(agentCtx AgentContext, res *TaskOutput, err error) error

	// Tools returns additional tools this middleware contributes to the agent.
	// Called once at NewAgent() time; tools are merged into the shared ToolRegistry.
	Tools() []Tool

	// SystemPromptFragment returns a string to append to the system prompt.
	// Called once per Execute() during prompt assembly, after the user's custom
	// prompt but before the base ReAct prompt. Return empty string to contribute nothing.
	SystemPromptFragment(ctx context.Context) string
}

// ModelCallRequest is the input to WrapModelCall.
type ModelCallRequest struct {
	Messages []*schema.Message // Full message history sent to the model
	Task     string            // The original user task for this execution
}

// ModelCallResponse is the output of WrapModelCall.
type ModelCallResponse struct {
	Message *schema.Message // The assistant reply
}

// ModelCallHandler is the next function in the WrapModelCall chain.
type ModelCallHandler func(ctx context.Context, req *ModelCallRequest) (*ModelCallResponse, error)

// ToolCallRequest is the input to WrapToolCall.
type ToolCallRequest struct {
	Name     string                 // Tool name
	Args     map[string]interface{} // Parsed arguments
	RawInput string                 // Original JSON string from the LLM
}

// ToolCallResponse is the output of WrapToolCall.
type ToolCallResponse struct {
	Result  string // String result returned to the LLM
	IsError bool   // When true, the result is an error message (LLM can self-correct)
}

// ToolCallHandler is the next function in the WrapToolCall chain.
type ToolCallHandler func(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error)

// BaseMiddleware provides no-op defaults for all Middleware methods.
// Embed it and override only the hooks you need.
type BaseMiddleware struct{}

// Name returns an empty string. Override in your middleware to provide a unique name.
func (BaseMiddleware) Name() string { return "" }

// BeforeAgent is a no-op. Override to add pre-execution logic.
func (BaseMiddleware) BeforeAgent(_ AgentContext) error { return nil }

// WrapModelCall passes through to the next handler unchanged.
func (BaseMiddleware) WrapModelCall(ctx context.Context, req *ModelCallRequest, next ModelCallHandler) (*ModelCallResponse, error) {
	return next(ctx, req)
}

// WrapToolCall passes through to the next handler unchanged.
func (BaseMiddleware) WrapToolCall(ctx context.Context, req *ToolCallRequest, next ToolCallHandler) (*ToolCallResponse, error) {
	return next(ctx, req)
}

// AfterAgent is a no-op. Override to add post-execution logic.
func (BaseMiddleware) AfterAgent(_ AgentContext, _ *TaskOutput, _ error) error { return nil }

// Tools returns nil. Override to contribute tools to the agent.
func (BaseMiddleware) Tools() []Tool { return nil }

// SystemPromptFragment returns empty string. Override to inject prompt text.
func (BaseMiddleware) SystemPromptFragment(_ context.Context) string { return "" }

// buildModelCallChain composes middlewares into a single ModelCallHandler.
// Execution order: middleware[0] → middleware[1] → … → terminal.
func buildModelCallChain(middlewares []Middleware, terminal ModelCallHandler) ModelCallHandler {
	chain := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		m := middlewares[i]
		next := chain
		chain = func(ctx context.Context, req *ModelCallRequest) (*ModelCallResponse, error) {
			return m.WrapModelCall(ctx, req, next)
		}
	}
	return chain
}

// buildToolCallChain composes middlewares into a single ToolCallHandler.
// Execution order: middleware[0] → middleware[1] → … → terminal.
func buildToolCallChain(middlewares []Middleware, terminal ToolCallHandler) ToolCallHandler {
	chain := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		m := middlewares[i]
		next := chain
		chain = func(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
			return m.WrapToolCall(ctx, req, next)
		}
	}
	return chain
}
