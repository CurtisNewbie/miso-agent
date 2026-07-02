package agentloop

import (
	"context"
	"fmt"
	"regexp"

	"github.com/cloudwego/eino/schema"

	"github.com/curtisnewbie/miso/flow"
)

const (
	defaultLargeToolResultsPathPrefix = "/large_tool_results"
	offloadPreviewHeadChars           = 2000
	offloadPreviewTailChars           = 1000
)

// offloadExcludedTools is the static set of tool names whose results are never
// offloaded to prevent infinite read loops (e.g. offloading read_file output
// then telling the agent to call read_file to retrieve it).
var offloadExcludedTools = map[string]bool{
	"read_file":      true,
	"write_file":     true,
	"edit_file":      true,
	"list_directory": true,
	"glob":           true,
	"grep":           true,
	"delete":         true,
}

var invalidPathCharsRe = regexp.MustCompile(`[^a-zA-Z0-9\-._]`)

// sanitizeForPath replaces filesystem-unsafe characters with underscores.
func sanitizeForPath(s string) string {
	return invalidPathCharsRe.ReplaceAllString(s, "_")
}

// buildPreview returns a head+tail preview of text with an omission separator.
// If text fits within the combined head+tail budget, it is returned as-is.
func buildPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= offloadPreviewHeadChars+offloadPreviewTailChars {
		return text
	}
	head := string(runes[:offloadPreviewHeadChars])
	tail := string(runes[len(runes)-offloadPreviewTailChars:])
	omitted := len(runes) - offloadPreviewHeadChars - offloadPreviewTailChars
	return fmt.Sprintf("%s\n\n[... %d characters omitted ...]\n\n%s", head, omitted, tail)
}

// maybeOffloadToolResult checks whether the tool result message content exceeds
// tokenLimit tokens and, if so, writes it to store and returns a replacement
// message with a preview and a file pointer. Non-tool messages and excluded tools
// are returned unchanged. On write failure the original message is returned
// (best-effort, non-fatal).
func maybeOffloadToolResult(ctx context.Context, msg *schema.Message, toolName string, store FileStore, tokenizer Tokenizer, tokenLimit int, pathPrefix string) *schema.Message {
	if msg.Role != schema.Tool {
		return msg
	}
	if msg.ToolCallID == "" {
		return msg
	}
	if offloadExcludedTools[toolName] {
		return msg
	}
	if tokenizer.CountTokens(msg.Content) <= tokenLimit {
		return msg
	}

	filePath := pathPrefix + "/" + sanitizeForPath(msg.ToolCallID)

	if err := store.WriteFile(ctx, filePath, []byte(msg.Content)); err != nil {
		flow.NewRail(ctx).Errorf("offload tool result (non-fatal): failed to write %s: %v", filePath, err)
		return msg
	}

	preview := buildPreview(msg.Content)
	replacement := fmt.Sprintf(
		"Tool result was too large and has been saved to: %s\n\nUse the file-read tool on that path to retrieve the full content (supports pagination).\n\nPreview (head and tail):\n\n%s",
		filePath, preview,
	)

	out := *msg
	out.Content = replacement
	return &out
}

// offloadToolResults applies large-tool-result eviction to msgs.
// accumulated is the current state.messages slice (before the new msgs are appended).
// The most recent assistant message in accumulated provides the callID→toolName mapping.
// Returns the (possibly replaced) messages. Safe to call on non-tool messages (they pass through).
func offloadToolResults(ctx context.Context, msgs []*schema.Message, accumulated []*schema.Message, store FileStore, tokenizer Tokenizer, tokenLimit int, pathPrefix string) []*schema.Message {
	if tokenLimit <= 0 || store == nil {
		return msgs
	}
	if pathPrefix == "" {
		pathPrefix = defaultLargeToolResultsPathPrefix
	}

	// Build callID → toolName from the most recent assistant message with tool calls.
	callIDToName := make(map[string]string)
	for i := len(accumulated) - 1; i >= 0; i-- {
		msg := accumulated[i]
		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				callIDToName[tc.ID] = tc.Function.Name
			}
			break
		}
	}

	out := make([]*schema.Message, len(msgs))
	for i, msg := range msgs {
		out[i] = maybeOffloadToolResult(ctx, msg, callIDToName[msg.ToolCallID], store, tokenizer, tokenLimit, pathPrefix)
	}
	return out
}
