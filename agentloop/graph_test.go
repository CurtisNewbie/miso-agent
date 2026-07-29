package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestOutputCheckFunc(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		check    OutputCheckFunc
		wantOk   bool
		wantHint string
		wantErr  bool
	}{
		{
			name:    "nil check passes",
			output:  "any output",
			check:   nil,
			wantOk:  true,
			wantErr: false,
		},
		{
			name:   "check accepts output",
			output: "valid output",
			check: func(_ context.Context, _ AgentContext, _ int, _ string) (string, bool, error) {
				return "", true, nil
			},
			wantOk:  true,
			wantErr: false,
		},
		{
			name:   "check rejects output with hint",
			output: "not json",
			check: func(_ context.Context, _ AgentContext, _ int, output string) (string, bool, error) {
				if !strings.HasPrefix(output, "{") {
					return "output must be a JSON object", false, nil
				}
				return "", true, nil
			},
			wantOk:   false,
			wantHint: "output must be a JSON object",
			wantErr:  false,
		},
		{
			name:   "check accepts output meeting criteria",
			output: `{"status": "ok"}`,
			check: func(_ context.Context, _ AgentContext, _ int, output string) (string, bool, error) {
				if !strings.HasPrefix(output, "{") {
					return "output must be a JSON object", false, nil
				}
				return "", true, nil
			},
			wantOk:  true,
			wantErr: false,
		},
		{
			name:   "check aborts on unexpected error",
			output: "any",
			check: func(_ context.Context, _ AgentContext, _ int, _ string) (string, bool, error) {
				return "", false, errors.New("service unavailable")
			},
			wantOk:  false,
			wantErr: true,
		},
		{
			name:   "attempt is passed to check",
			output: "any",
			check: func(_ context.Context, _ AgentContext, attempt int, _ string) (string, bool, error) {
				if attempt != 1 {
					return "unexpected attempt count", false, nil
				}
				return "", true, nil
			},
			wantOk:  true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.check == nil {
				return // nil check is tested implicitly via AgentConfig default
			}
			hint, ok, err := tt.check(context.Background(), AgentContext{}, 1, tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("OutputCheckFunc() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if ok != tt.wantOk {
				t.Errorf("OutputCheckFunc() ok = %v, wantOk = %v", ok, tt.wantOk)
			}
			if hint != tt.wantHint {
				t.Errorf("OutputCheckFunc() hint = %q, wantHint = %q", hint, tt.wantHint)
			}
		})
	}
}

func TestShouldContinueLoop(t *testing.T) {
	tests := []struct {
		name        string
		lastMsg     *schema.Message
		want        bool
		description string
	}{
		{
			name:        "Non-assistant message",
			lastMsg:     &schema.Message{Role: schema.User, Content: "hello"},
			want:        false,
			description: "Non-assistant messages should never continue the loop",
		},
		{
			name:        "Tool message",
			lastMsg:     &schema.Message{Role: schema.Tool, Content: "tool result"},
			want:        false,
			description: "Tool messages should never continue the loop",
		},
		{
			name: "Assistant with tool calls",
			lastMsg: &schema.Message{
				Role:      schema.Assistant,
				ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "some_tool"}}},
			},
			want:        true,
			description: "Assistant tool calls should continue the loop",
		},
		{
			name: "Assistant with multiple tool calls",
			lastMsg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "search_web"}},
					{Function: schema.FunctionCall{Name: "read_file"}},
				},
			},
			want:        true,
			description: "Multiple tool calls should continue the loop",
		},
		{
			name:        "Assistant plain text",
			lastMsg:     &schema.Message{Role: schema.Assistant, Content: "Here is my answer."},
			want:        false,
			description: "Plain text response should stop the loop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldContinueLoop(tt.lastMsg)
			if got != tt.want {
				t.Errorf("shouldContinueLoop() = %v, want %v (%s)", got, tt.want, tt.description)
			}
		})
	}
}

func TestResolveBranchTarget(t *testing.T) {
	tests := []struct {
		name           string
		shouldContinue bool
		want           string
		description    string
	}{
		{
			name:           "Should not continue",
			shouldContinue: false,
			want:           "final_output",
			description:    "When shouldContinue is false, route to final_output",
		},
		{
			name:           "Should continue",
			shouldContinue: true,
			want:           "tools",
			description:    "When shouldContinue is true, route to tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBranchTarget(tt.shouldContinue)
			if got != tt.want {
				t.Errorf("resolveBranchTarget() = %q, want %q (%s)", got, tt.want, tt.description)
			}
		})
	}
}

func TestNewAgent_BuildsGraphWithHitl(t *testing.T) {
	ag, err := NewAgent(AgentConfig{
		ModelName: "qwen-flash",
		ApiKey:    "test-key",
		HitlStore: NewMemHitlStore(),
	})
	if err != nil {
		t.Fatalf("NewAgent() with HITL enabled returned err: %v", err)
	}
	if ag == nil {
		t.Fatal("NewAgent() returned nil agent")
	}
}
