package agentloop

// @author yongj.zhuang

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestShouldContinueLoop(t *testing.T) {
	tests := []struct {
		name             string
		lastMsg          *schema.Message
		enableFinishTool bool
		want             bool
		description      string
	}{
		{
			name: "Non-assistant message",
			lastMsg: &schema.Message{
				Role:    schema.User,
				Content: "hello",
			},
			enableFinishTool: false,
			want:             false,
			description:      "Non-assistant messages should never continue the loop",
		},
		{
			name: "Tool message",
			lastMsg: &schema.Message{
				Role:    schema.Tool,
				Content: "tool result",
			},
			enableFinishTool: false,
			want:             false,
			description:      "Tool messages should never continue the loop",
		},
		{
			name: "Assistant with tool calls, EnableFinishTool=false",
			lastMsg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "some_tool"}},
				},
			},
			enableFinishTool: false,
			want:             true,
			description:      "Tool calls without finish_tool mode should continue",
		},
		{
			name: "Assistant with non-finish tool calls, EnableFinishTool=true",
			lastMsg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "search_web"}},
					{Function: schema.FunctionCall{Name: "read_file"}},
				},
			},
			enableFinishTool: true,
			want:             true,
			description:      "Non-finish tool calls should continue the loop",
		},
		{
			name: "Assistant called finish_tool, EnableFinishTool=true",
			lastMsg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: finishToolName}},
				},
			},
			enableFinishTool: true,
			want:             false,
			description:      "finish_tool call should stop the loop",
		},
		{
			name: "Assistant called finish_tool among other tools",
			lastMsg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "search_web"}},
					{Function: schema.FunctionCall{Name: finishToolName}},
				},
			},
			enableFinishTool: true,
			want:             false,
			description:      "finish_tool call among other tool calls should stop the loop",
		},
		{
			name: "Assistant plain text, EnableFinishTool=false",
			lastMsg: &schema.Message{
				Role:    schema.Assistant,
				Content: "Here is my answer.",
			},
			enableFinishTool: false,
			want:             false,
			description:      "Plain text with no finish_tool mode should stop the loop",
		},
		{
			name: "Assistant plain text, EnableFinishTool=true (the bug scenario)",
			lastMsg: &schema.Message{
				Role:    schema.Assistant,
				Content: "Here is my answer without calling finish_tool.",
			},
			enableFinishTool: true,
			want:             true,
			description:      "Plain text with EnableFinishTool=true should continue loop (route back to chat_model)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldContinueLoop(tt.lastMsg, tt.enableFinishTool)
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
		input          *schema.Message
		want           string
		description    string
	}{
		{
			name:           "Should not continue",
			shouldContinue: false,
			input:          &schema.Message{Role: schema.Assistant, Content: "done"},
			want:           "final_output",
			description:    "When shouldContinue is false, always route to final_output",
		},
		{
			name:           "Continue with tool calls",
			shouldContinue: true,
			input: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "search_web"}},
				},
			},
			want:        "tools",
			description: "When shouldContinue and has tool calls, route to tools node",
		},
		{
			name:           "Continue without tool calls (bug fix scenario)",
			shouldContinue: true,
			input: &schema.Message{
				Role:    schema.Assistant,
				Content: "I think I am done, but forgot to call finish_tool.",
			},
			want:        "loop_back_model",
			description: "When shouldContinue but no tool calls, route to loop_back_model adapter (not tools) to avoid ToolsNode panic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBranchTarget(tt.shouldContinue, tt.input)
			if got != tt.want {
				t.Errorf("resolveBranchTarget() = %q, want %q (%s)", got, tt.want, tt.description)
			}
		})
	}
}
