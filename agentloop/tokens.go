package agentloop

import (
	"fmt"
	"slices"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/pkoukk/tiktoken-go"
)

// Tokenizer manages tiktoken encodings for token counting.
// It caches encodings to avoid repeated initialization.
type Tokenizer struct {
	mu       sync.RWMutex
	encoding string
	tke      *tiktoken.Tiktoken
}

// NewTokenizer creates a new tokenizer for the specified model.
// If model is empty, defaults to "gpt-3.5-turbo" (cl100k_base encoding).
func NewTokenizer(model string) (*Tokenizer, error) {
	if model == "" {
		model = "gpt-3.5-turbo"
	}

	tkm, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return nil, fmt.Errorf("failed to get encoding for model %s: %w", model, err)
	}

	return &Tokenizer{
		encoding: model,
		tke:      tkm,
	}, nil
}

// CountTokens returns the exact token count for the given text.
func (t *Tokenizer) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.tke.Encode(text, nil, nil))
}

// CountMessageTokens returns the token count for a message.
// This follows OpenAI's token counting methodology for chat messages.
// See: https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb
func (t *Tokenizer) CountMessageTokens(msg *schema.Message) int {
	if msg == nil {
		return 0
	}

	tokens := 0

	// Base tokens per message (depends on model version)
	// For most models: 3 tokens per message
	tokensPerMessage := 3

	// Adjust for older models if needed
	if t.encoding == "gpt-3.5-turbo-0301" {
		tokensPerMessage = 4
	}

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
func (t *Tokenizer) CountMessagesTokens(messages []*schema.Message) int {
	total := 0
	for _, msg := range messages {
		total += t.CountMessageTokens(msg)
	}
	// Every reply is primed with <|start|>assistant<|message|>
	total += 3
	return total
}

// PruneMessagesToTokenLimit prunes messages to fit within the token limit.
// It preserves system messages and the most recent messages.
// Returns the pruned slice of messages.
func (t *Tokenizer) PruneMessagesToTokenLimit(messages []*schema.Message, maxTokens int) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}
	messages = slices.Clone(messages)

	// Calculate current tokens
	currentTokens := t.CountMessagesTokens(messages)

	if currentTokens <= maxTokens {
		return messages
	}

	// Find system messages (should be kept)
	var systemMessages []*schema.Message
	var otherMessages []*schema.Message

	for _, msg := range messages {
		if msg.Role == schema.System {
			systemMessages = append(systemMessages, msg)
		} else {
			otherMessages = append(otherMessages, msg)
		}
	}

	// Calculate tokens used by system messages
	systemTokens := 0
	for _, msg := range systemMessages {
		systemTokens += t.CountMessageTokens(msg)
	}
	systemTokens += 3 // priming tokens

	if systemTokens > maxTokens {
		// System messages alone exceed limit, this is problematic
		// Return only system messages as they're critical
		return systemMessages
	}

	// Calculate remaining tokens for other messages
	remainingTokens := maxTokens - systemTokens

	// Keep the most recent messages that fit in remaining tokens
	var prunedOtherMessages []*schema.Message
	currentOtherTokens := 0

	// Iterate from end (most recent) to beginning
	for i := len(otherMessages) - 1; i >= 0; i-- {
		msg := otherMessages[i]
		msgTokens := t.CountMessageTokens(msg)

		if currentOtherTokens+msgTokens <= remainingTokens {
			// Prepend to maintain order
			prunedOtherMessages = append([]*schema.Message{msg}, prunedOtherMessages...)
			currentOtherTokens += msgTokens
		}
	}

	// Combine system and pruned other messages
	result := append(systemMessages, prunedOtherMessages...)
	return result
}
