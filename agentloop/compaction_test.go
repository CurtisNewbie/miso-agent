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

// TestSelectForCompaction_Empty checks that inputs shorter than [system, user, one-more]
// return nil for both slices — nothing to compact.
// Also checks that inputs with unexpected roles at [0] or [1] return nil, nil gracefully.
func TestSelectForCompaction_Empty(t *testing.T) {
	tok := NewTokenizer()

	for _, msgs := range [][]*schema.Message{
		{},
		{schema.SystemMessage("sys")},
		{schema.SystemMessage("sys"), schema.UserMessage("task")},
		// Invariant violated: messages[0] is not system
		{schema.UserMessage("task"), schema.UserMessage("task2"), {Role: schema.Assistant, Content: "hi"}},
		// Invariant violated: messages[1] is not user
		{schema.SystemMessage("sys"), {Role: schema.Assistant, Content: "hi"}, schema.UserMessage("task")},
	} {
		toSum, toKeep := selectForCompaction(msgs, tok, 1000)
		if toSum != nil || toKeep != nil {
			t.Errorf("input len=%d: expected (nil, nil), got (%v, %v)", len(msgs), toSum, toKeep)
		}
	}
}

// TestSelectForCompaction_AllFit checks that when keepTokens is large all messages after
// [system, user] go to toKeep and nothing goes to toSummarize.
func TestSelectForCompaction_AllFit(t *testing.T) {
	tok := NewTokenizer()

	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("task"),
		{Role: schema.Assistant, Content: "hi there"},
		{Role: schema.Tool, Content: "result"},
	}

	toSum, toKeep := selectForCompaction(messages, tok, 100000)
	if len(toSum) != 0 {
		t.Errorf("expected empty toSummarize, got %d messages", len(toSum))
	}
	if len(toKeep) != 2 {
		t.Errorf("expected 2 messages in toKeep (assistant+tool), got %d", len(toKeep))
	}
}

// TestSelectForCompaction_SystemAndUserExcluded checks that neither system nor the original
// user task appear in either output slice — they are always preserved by the caller.
func TestSelectForCompaction_SystemAndUserExcluded(t *testing.T) {
	tok := NewTokenizer()

	messages := []*schema.Message{
		schema.SystemMessage("you are a bot"),
		schema.UserMessage("original task"),
		{Role: schema.Assistant, Content: "hi"},
	}

	toSum, toKeep := selectForCompaction(messages, tok, 100000)

	for _, m := range append(toSum, toKeep...) {
		if m.Role == schema.System {
			t.Errorf("system message must not appear in either output slice")
		}
		if m.Role == schema.User && m.Content == "original task" {
			t.Errorf("original user task must not appear in either output slice")
		}
	}
	// Only the assistant message is a candidate.
	if len(toKeep) != 1 {
		t.Errorf("expected 1 message in toKeep (assistant), got %d", len(toKeep))
	}
}

// TestSelectForCompaction_OrphanToolAdvance checks that the orphan-tool guard advances splitIdx
// past a leading Tool message so toKeep never starts with an orphaned tool result.
func TestSelectForCompaction_OrphanToolAdvance(t *testing.T) {
	tok := NewTokenizer()

	largeContent := strings.Repeat("word ", 300) // ~300 tokens
	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("original task"),
		{Role: schema.Assistant, Content: largeContent},
		{Role: schema.Tool, Content: "tool result"},
		schema.UserMessage("follow-up"),
	}

	// keepTokens=10: follow-up + tool fit, but assistant(large) pushes splitIdx to tool.
	// Guard advances splitIdx past tool so toKeep starts with follow-up.
	toSum, toKeep := selectForCompaction(messages, tok, 10)

	if len(toKeep) == 0 {
		t.Fatalf("expected at least one message in toKeep")
	}
	if toKeep[0].Role == schema.Tool {
		t.Errorf("toKeep must not start with a Tool message (orphan tool guard failed)")
	}

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

	messages := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("task"),
		{Role: schema.Tool, Content: "tool result"},
		schema.UserMessage("follow-up"),
	}

	// All fit under huge keepTokens → splitIdx=0, orphan guard must not fire.
	toSum, toKeep := selectForCompaction(messages, tok, 100000)

	if len(toSum) != 0 {
		t.Errorf("expected empty toSummarize, got %d messages", len(toSum))
	}
	if len(toKeep) != 2 {
		t.Errorf("expected 2 messages in toKeep (tool+follow-up), got %d", len(toKeep))
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

// TestSelectForCompaction_FirstUserMessageAnchored verifies the first user message (original task)
// never appears in toSummarize or toKeep — it is excluded from the compaction window entirely.
func TestSelectForCompaction_FirstUserMessageAnchored(t *testing.T) {
	tok := NewTokenizer()
	largeContent := strings.Repeat("word ", 300) // ~300 tokens

	messages := []*schema.Message{
		schema.SystemMessage("system prompt"),
		schema.UserMessage("original task description"),
		{Role: schema.Assistant, Content: largeContent},
		{Role: schema.Tool, Content: "tool result"},
	}

	toSum, toKeep := selectForCompaction(messages, tok, 10)

	for _, m := range append(toSum, toKeep...) {
		if m.Role == schema.User && m.Content == "original task description" {
			t.Error("original user task must never appear in toSummarize or toKeep")
		}
	}
}
