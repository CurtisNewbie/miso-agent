package agentloop

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// TestSerializeForCompaction covers every branch in serializeForCompaction.
func TestSerializeForCompaction(t *testing.T) {
	multibyteExact := strings.Repeat("中", compactionToolOutputMax)  // 2000 runes × 3 bytes = 6000 bytes > limit, but rune count == limit
	multibyteOver := strings.Repeat("中", compactionToolOutputMax+1) // 2001 runes → truncated
	asciiOver := strings.Repeat("a", compactionToolOutputMax+1)     // 2001 bytes → truncated

	tests := []struct {
		name      string
		msg       *schema.Message
		want      string
		wantEmpty bool
	}{
		{
			name:      "System message returns empty",
			msg:       schema.SystemMessage("you are a bot"),
			wantEmpty: true,
		},
		{
			name: "User normal message",
			msg:  schema.UserMessage("hello world"),
			want: "[User]: hello world",
		},
		{
			name:      "User checkpoint message skipped",
			msg:       schema.UserMessage("<conversation-checkpoint>some summary</conversation-checkpoint>"),
			wantEmpty: true,
		},
		{
			name: "Assistant content only",
			msg:  &schema.Message{Role: schema.Assistant, Content: "I can help"},
			want: "[Assistant]: I can help",
		},
		{
			name: "Assistant tool call only (no content)",
			msg: &schema.Message{
				Role: schema.Assistant,
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "read_file", Arguments: `{"path":"/tmp/foo"}`}},
				},
			},
			want: `[Tool call]: read_file({"path":"/tmp/foo"})`,
		},
		{
			name: "Assistant content and tool call",
			msg: &schema.Message{
				Role:    schema.Assistant,
				Content: "calling tool",
				ToolCalls: []schema.ToolCall{
					{Function: schema.FunctionCall{Name: "write_file", Arguments: `{"path":"out"}`}},
				},
			},
			want: "[Assistant]: calling tool\n[Tool call]: write_file({\"path\":\"out\"})",
		},
		{
			name: "Tool result within byte limit",
			msg: &schema.Message{
				Role:    schema.Tool,
				Content: "short result",
			},
			want: "[Tool result]: short result",
		},
		{
			name: "Tool result ASCII content exceeds byte limit → truncated",
			msg: &schema.Message{
				Role:    schema.Tool,
				Content: asciiOver,
			},
			want: "[Tool result]: " + strings.Repeat("a", compactionToolOutputMax) + "\n[truncated]",
		},
		{
			name: "Tool result multi-byte content byte length > limit but rune count == limit → NOT truncated",
			msg: &schema.Message{
				Role:    schema.Tool,
				Content: multibyteExact,
			},
			want: "[Tool result]: " + multibyteExact,
		},
		{
			name: "Tool result multi-byte content rune count > limit → truncated at rune boundary",
			msg: &schema.Message{
				Role:    schema.Tool,
				Content: multibyteOver,
			},
			want: "[Tool result]: " + strings.Repeat("中", compactionToolOutputMax) + "\n[truncated]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serializeForCompaction(tt.msg)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestSelectForCompaction_Empty checks empty input returns nil toSummarize and empty toKeep.
func TestSelectForCompaction_Empty(t *testing.T) {
	tok := NewTokenizer()

	toSum, toKeep := selectForCompaction([]*schema.Message{}, tok, 1000)
	if toSum != nil {
		t.Errorf("toSummarize should be nil, got %v", toSum)
	}
	if toKeep == nil {
		t.Errorf("toKeep should be non-nil empty slice, got nil")
	}
	if len(toKeep) != 0 {
		t.Errorf("toKeep should be empty, got len=%d", len(toKeep))
	}
}

// TestSelectForCompaction_AllFit checks that when keepTokens is large all non-system messages go to toKeep.
func TestSelectForCompaction_AllFit(t *testing.T) {
	tok := NewTokenizer()

	messages := []*schema.Message{
		schema.UserMessage("hello"),
		{Role: schema.Assistant, Content: "hi there"},
		{Role: schema.Tool, Content: "result"},
	}

	toSum, toKeep := selectForCompaction(messages, tok, 100000)
	if len(toSum) != 0 {
		t.Errorf("expected empty toSummarize, got %d messages", len(toSum))
	}
	if len(toKeep) != 3 {
		t.Errorf("expected 3 messages in toKeep, got %d", len(toKeep))
	}
}

// TestSelectForCompaction_SystemExcluded checks that system messages are not in either output slice.
func TestSelectForCompaction_SystemExcluded(t *testing.T) {
	tok := NewTokenizer()

	messages := []*schema.Message{
		schema.SystemMessage("you are a bot"),
		schema.UserMessage("hello"),
		{Role: schema.Assistant, Content: "hi"},
	}

	toSum, toKeep := selectForCompaction(messages, tok, 100000)

	for _, m := range toSum {
		if m.Role == schema.System {
			t.Errorf("system message must not appear in toSummarize")
		}
	}
	for _, m := range toKeep {
		if m.Role == schema.System {
			t.Errorf("system message must not appear in toKeep")
		}
	}
	// Both user and assistant should be in toKeep (all fit).
	if len(toKeep) != 2 {
		t.Errorf("expected 2 non-system messages in toKeep, got %d", len(toKeep))
	}
}

// TestSelectForCompaction_OrphanToolAdvance checks that the orphan-tool guard advances splitIdx
// past a leading Tool message so toKeep never starts with an orphaned tool result.
func TestSelectForCompaction_OrphanToolAdvance(t *testing.T) {
	tok := NewTokenizer()

	largeContent := strings.Repeat("word ", 300) // ~300 tokens
	messages := []*schema.Message{
		schema.UserMessage("first user message"),
		{Role: schema.Assistant, Content: largeContent},
		{Role: schema.Tool, Content: "tool result"},
		schema.UserMessage("second user message"),
	}

	// keepTokens=10: userMsg2 + toolMsg fit, but assistantMsg(large) causes splitIdx=2 (toolMsg).
	// Guard advances splitIdx to 3 (userMsg2).
	toSum, toKeep := selectForCompaction(messages, tok, 10)

	if len(toKeep) == 0 {
		t.Fatalf("expected at least one message in toKeep")
	}
	if toKeep[0].Role == schema.Tool {
		t.Errorf("toKeep must not start with a Tool message (orphan tool guard failed)")
	}

	// Tool message should be in toSummarize.
	foundTool := false
	for _, m := range toSum {
		if m.Role == schema.Tool {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Errorf("tool message should appear in toSummarize when orphan guard fires")
	}
}

// TestSelectForCompaction_SplitIdxZeroGuard checks that the orphan guard does NOT fire when splitIdx=0.
func TestSelectForCompaction_SplitIdxZeroGuard(t *testing.T) {
	tok := NewTokenizer()

	// Messages with no system prefix; all fit under huge keepTokens so splitIdx stays 0.
	messages := []*schema.Message{
		{Role: schema.Tool, Content: "some tool result"},
		schema.UserMessage("user msg"),
	}

	toSum, toKeep := selectForCompaction(messages, tok, 100000)

	if len(toSum) != 0 {
		t.Errorf("expected empty toSummarize, got %d messages", len(toSum))
	}
	if len(toKeep) != 2 {
		t.Errorf("expected both messages in toKeep, got %d", len(toKeep))
	}
}

// TestBuildCompactionPrompt_NoSummary checks prompt structure when there is no previous summary.
func TestBuildCompactionPrompt_NoSummary(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("please summarize this"),
	}
	prompt := buildCompactionPrompt("", msgs)

	if !strings.Contains(prompt, "Create a new anchored summary") {
		t.Errorf("expected 'Create a new anchored summary' in prompt, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "<previous-summary>") {
		t.Errorf("prompt must not contain <previous-summary> when no previous summary provided")
	}
	if !strings.Contains(prompt, "please summarize this") {
		t.Errorf("prompt must contain serialized user message content")
	}
}

// TestBuildCompactionPrompt_WithSummary checks prompt structure when a previous summary exists.
func TestBuildCompactionPrompt_WithSummary(t *testing.T) {
	prev := "## Goal\n- some goal"
	msgs := []*schema.Message{
		schema.UserMessage("new conversation turn"),
	}
	prompt := buildCompactionPrompt(prev, msgs)

	if !strings.Contains(prompt, "<previous-summary>") {
		t.Errorf("expected <previous-summary> block in prompt")
	}
	if !strings.Contains(prompt, prev) {
		t.Errorf("prompt must contain the previous summary text")
	}
	if !strings.Contains(prompt, "Update the previous summary") {
		t.Errorf("expected 'Update the previous summary' in prompt")
	}
	if strings.Contains(prompt, "Update the anchored summary below") {
		t.Errorf("old phrasing 'Update the anchored summary below' must not appear")
	}
}

// TestBuildCompactionPrompt_CheckpointSkipped checks that a checkpoint User message body
// does not appear in the prompt but the previousSummary still does.
func TestBuildCompactionPrompt_CheckpointSkipped(t *testing.T) {
	prev := "## Goal\n- write tests"
	msgs := []*schema.Message{
		schema.UserMessage("<conversation-checkpoint>old summary content</conversation-checkpoint>"),
	}
	prompt := buildCompactionPrompt(prev, msgs)

	if strings.Contains(prompt, "<conversation-checkpoint>") {
		t.Errorf("checkpoint XML must not appear in the prompt body")
	}
	if !strings.Contains(prompt, prev) {
		t.Errorf("previousSummary must still appear via <previous-summary> block")
	}
}

// TestRunCompaction_EmptyToSummarize checks that runCompaction returns previousSummary unchanged
// when toSummarize is empty (no model call needed).
func TestRunCompaction_EmptyToSummarize(t *testing.T) {
	prev := "existing summary"
	got, err := runCompaction(nil, nil, prev, []*schema.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != prev {
		t.Errorf("expected %q, got %q", prev, got)
	}
}

// TestRunCompaction_AllSerializeToEmpty checks that runCompaction returns previousSummary unchanged
// when all messages in toSummarize serialize to "" (e.g. only checkpoint messages).
// No model call should be made.
func TestRunCompaction_AllSerializeToEmpty(t *testing.T) {
	prev := "existing summary"
	// Checkpoint messages serialize to "" — passing only these should skip the model call.
	checkpoint := schema.UserMessage("<conversation-checkpoint>\n<summary>prev</summary>\n</conversation-checkpoint>")
	got, err := runCompaction(nil, nil, prev, []*schema.Message{checkpoint})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != prev {
		t.Errorf("expected %q, got %q", prev, got)
	}
}
