package agentloop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const compactionSummaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Goal
- [single-sentence task summary]

## Constraints & Preferences
- [user constraints, preferences, specs, or "(none)"]

## Progress
### Done
- [completed work or "(none)"]

### In Progress
- [current work or "(none)"]

### Blocked
- [blockers or "(none)"]

## Key Decisions
- [decision and why, or "(none)"]

## Next Steps
- [ordered next actions or "(none)"]

## Critical Context
- [important technical facts, errors, open questions, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, commands, error strings, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

const compactionToolOutputMax = 2000

// serializeForCompaction converts a message to text for the compaction LLM prompt.
// System messages are excluded (return "").
func serializeForCompaction(msg *schema.Message) string {
	switch msg.Role {
	case schema.System:
		return ""
	case schema.User:
		// Skip compaction checkpoints — the summary text is already provided via
		// buildCompactionPrompt's previousSummary parameter, so including the
		// checkpoint message here would duplicate it in the prompt.
		if strings.HasPrefix(msg.Content, "<conversation-checkpoint>") {
			return ""
		}
		return "[User]: " + msg.Content
	case schema.Assistant:
		var parts []string
		if msg.Content != "" {
			parts = append(parts, "[Assistant]: "+msg.Content)
		}
		for _, tc := range msg.ToolCalls {
			parts = append(parts, fmt.Sprintf("[Tool call]: %s(%s)", tc.Function.Name, tc.Function.Arguments))
		}
		return strings.Join(parts, "\n")
	case schema.Tool:
		content := msg.Content
		// Fast path: skip rune conversion when content is within byte limit.
		// When multi-byte chars are present, byte length may exceed the threshold
		// while rune length does not — the inner check handles that correctly.
		if len(content) > compactionToolOutputMax {
			runes := []rune(content)
			if len(runes) > compactionToolOutputMax {
				content = string(runes[:compactionToolOutputMax]) + "\n[truncated]"
			}
		}
		return "[Tool result]: " + content
	default:
		return msg.Content
	}
}

// selectForCompaction splits messages[2:] into toSummarize and toKeep.
// messages[0] (system) and messages[1] (original user task) are always preserved
// by the caller — they are not included in either return slice.
func selectForCompaction(messages []*schema.Message, tokenizer Tokenizer, keepTokens int) (toSummarize, toKeep []*schema.Message) {
	// Need at least [system, user, one-more] for there to be anything to compact.
	if len(messages) < 3 {
		return nil, nil
	}
	if messages[0].Role != schema.System || messages[1].Role != schema.User {
		// Invariant violated — skip compaction rather than operating on unexpected structure.
		return nil, nil
	}
	candidate := messages[2:]

	recentTokens := 0
	splitIdx := 0
	for i := len(candidate) - 1; i >= 0; i-- {
		t := tokenizer.CountMessageTokens(candidate[i])
		if recentTokens+t > keepTokens {
			splitIdx = i + 1
			break
		}
		recentTokens += t
	}

	// Advance splitIdx past any leading Tool messages so toKeep never starts with
	// an orphaned tool result (no matching assistant message in scope).
	// Guard: only advance when splitIdx > 0; at 0 the intent is "keep everything".
	if splitIdx > 0 {
		for splitIdx < len(candidate) && candidate[splitIdx].Role == schema.Tool {
			splitIdx++
		}
	}

	return candidate[:splitIdx], candidate[splitIdx:]
}

// buildCompactionPrompt builds the LLM prompt for summarizing toSummarize messages.
func buildCompactionPrompt(previousSummary string, toSummarize []*schema.Message) string {
	var lines []string
	for _, msg := range toSummarize {
		text := serializeForCompaction(msg)
		if text != "" {
			lines = append(lines, text)
		}
	}
	conversationText := strings.Join(lines, "\n\n")

	var intro string
	if previousSummary != "" {
		intro = fmt.Sprintf(
			"<previous-summary>\n%s\n</previous-summary>\n\n"+
				"Update the previous summary (shown above) using the conversation history (shown further above).\n"+
				"Preserve still-true details, remove stale details, and merge in the new facts.",
			previousSummary,
		)
	} else {
		intro = "Create a new anchored summary from the conversation history above."
	}

	return strings.Join([]string{conversationText, intro, compactionSummaryTemplate}, "\n\n")
}

// runCompaction calls the model to summarize toSummarize and returns the summary text.
// Returns previousSummary unchanged if toSummarize is empty or all messages serialize to nothing.
func runCompaction(ctx context.Context, m model.ToolCallingChatModel, previousSummary string, toSummarize []*schema.Message) (string, error) {
	if len(toSummarize) == 0 {
		return previousSummary, nil
	}
	// Skip the model call if every message serializes to "" (e.g. toSummarize contains
	// only a prior checkpoint message). There is no new information to summarize.
	hasContent := false
	for _, msg := range toSummarize {
		if serializeForCompaction(msg) != "" {
			hasContent = true
			break
		}
	}
	if !hasContent {
		return previousSummary, nil
	}
	prompt := buildCompactionPrompt(previousSummary, toSummarize)
	resp, err := m.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
