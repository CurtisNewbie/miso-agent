package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
)

type errorTool struct {
	name string
	err  error
}

func (t *errorTool) Name() string        { return t.name }
func (t *errorTool) Description() string { return "always fails" }
func (t *errorTool) Parameters() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{}
}
func (t *errorTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", t.err
}

type typedErrorTool struct {
	name string
	err  error
}

func (t *typedErrorTool) Name() string        { return t.name }
func (t *typedErrorTool) Description() string { return "always fails (typed)" }
func (t *typedErrorTool) Parameters() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{}
}
func (t *typedErrorTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", t.err
}
func (t *typedErrorTool) ExecuteJson(ctx context.Context, input string) (string, error) {
	return "", t.err
}

func TestToolWrapper_InvokableRun_UntypedToolError(t *testing.T) {
	toolErr := errs.NewErrf("disk full")
	w := &toolWrapper{tool: &errorTool{name: "failing_tool", err: toolErr}}

	args, _ := json.Marshal(map[string]interface{}{})
	result, err := w.InvokableRun(context.Background(), string(args))

	if err != nil {
		t.Fatalf("expected nil error (error converted to string), got: %v", err)
	}
	if !strings.HasPrefix(result, "Error:") {
		t.Errorf("expected result to start with 'Error:', got: %q", result)
	}
	if !strings.Contains(result, "disk full") {
		t.Errorf("expected result to contain original error message, got: %q", result)
	}
}

func TestToolWrapper_InvokableRun_TypedToolError(t *testing.T) {
	toolErr := errs.NewErrf("invalid argument: foo must be positive")
	w := &toolWrapper{tool: &typedErrorTool{name: "typed_failing_tool", err: toolErr}}

	args, _ := json.Marshal(map[string]interface{}{"foo": -1})
	result, err := w.InvokableRun(context.Background(), string(args))

	if err != nil {
		t.Fatalf("expected nil error (error converted to string), got: %v", err)
	}
	if !strings.HasPrefix(result, "Error:") {
		t.Errorf("expected result to start with 'Error:', got: %q", result)
	}
	if !strings.Contains(result, "invalid argument") {
		t.Errorf("expected result to contain original error message, got: %q", result)
	}
}

func TestToolMiddleware_ShowsToolResultToLLM(t *testing.T) {
	toolErr := errs.NewErrf("permission denied: user lacks required privilege")
	failTool := &typedErrorTool{name: "check_access", err: toolErr}

	registry := NewToolRegistry()
	registry.Register(failTool)
	einoTools := registry.ToEinoTools()

	var capturedResult string
	middleware := compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				rail := flow.NewRail(ctx)
				rail.Infof("[tool_call]   name=%s  args=%s", input.Name, input.Arguments)
				output, err := next(ctx, input)
				if err != nil {
					rail.Infof("[tool_result] name=%s  ERROR=%v", input.Name, err)
					return output, err
				}
				rail.Infof("[tool_result] name=%s  result=%s", input.Name, output.Result)
				capturedResult = output.Result
				return output, nil
			}
		},
	}

	toolNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools:               einoTools,
		ToolCallMiddlewares: []compose.ToolMiddleware{middleware},
	})
	if err != nil {
		t.Fatalf("failed to create tool node: %v", err)
	}

	msg := &schema.Message{
		Role: schema.Assistant,
		ToolCalls: []schema.ToolCall{
			{
				ID: "call_001",
				Function: schema.FunctionCall{
					Name:      "check_access",
					Arguments: `{"user": "alice", "resource": "admin_panel"}`,
				},
			},
		},
	}

	_, err = toolNode.Invoke(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(capturedResult, "Error:") {
		t.Errorf("expected tool result to start with 'Error:', got: %q", capturedResult)
	}
	if !strings.Contains(capturedResult, "permission denied") {
		t.Errorf("expected tool result to contain error message, got: %q", capturedResult)
	}

	t.Logf("AI sees this as tool result message: %q", capturedResult)
}

func TestToolWrapper_InvokableRun_UntypedToolSuccess(t *testing.T) {
	successTool := NewToolFunc("ok_tool", "succeeds", map[string]*schema.ParameterInfo{},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return "done", nil
		},
	)
	w := &toolWrapper{tool: successTool}

	result, err := w.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("expected 'done', got %q", result)
	}
}
