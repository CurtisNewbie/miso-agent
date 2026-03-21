package agentloop

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

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
