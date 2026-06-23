package agentloop

import (
	"github.com/cloudwego/eino/schema"
)

// Tokenizer counts tokens using a simple len/4 approximation.
type Tokenizer struct{}

// NewTokenizer creates a new Tokenizer.
func NewTokenizer() Tokenizer {
	return Tokenizer{}
}

// CountTokens returns an approximate token count for the given text.
// Uses the same heuristic as opencode: 4 characters per token on average.
// This is model-agnostic and avoids model-specific tokenizer dependencies.
// Accuracy varies by content (~4 chars/token for English prose, less accurate
// for code or non-Latin scripts), but is sufficient for context window budgeting.
func (t Tokenizer) CountTokens(text string) int {
	return len(text) / 4
}

// CountMessageTokens returns the token count for a message.
// This follows OpenAI's token counting methodology for chat messages.
// See: https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb
func (t Tokenizer) CountMessageTokens(msg *schema.Message) int {
	if msg == nil {
		return 0
	}

	tokens := 0

	// Base tokens per message (depends on model version)
	// For most models: 3 tokens per message
	tokensPerMessage := 3

	// Count message structure
	tokens += tokensPerMessage

	// Count role
	tokens += t.CountTokens(string(msg.Role))

	// Count content
	tokens += t.CountTokens(msg.Content)

	// Count tool calls if present
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			// Tool call has additional overhead
			tokens += 4 // overhead for tool call structure
			tokens += t.CountTokens(tc.Function.Name)
			tokens += t.CountTokens(tc.Function.Arguments)
		}
	}

	return tokens
}

// CountMessagesTokens returns the total token count for a slice of messages.
// Includes the 3 token priming for assistant reply.
func (t Tokenizer) CountMessagesTokens(messages []*schema.Message) int {
	total := 0
	for _, msg := range messages {
		total += t.CountMessageTokens(msg)
	}
	// Every reply is primed with <|start|>assistant<|message|>
	total += 3
	return total
}
