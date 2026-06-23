package agentloop

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNewTokenizer(t *testing.T) {
	_ = NewTokenizer()
}

func TestTokenizer_CountTokens(t *testing.T) {
	tokenizer := NewTokenizer()

	tests := []struct {
		name string
		text string
		min  int // Minimum expected tokens
		max  int // Maximum expected tokens
	}{
		{
			name: "Empty string",
			text: "",
			min:  0,
			max:  0,
		},
		{
			name: "Single word",
			text: "Hello",
			min:  1,
			max:  2,
		},
		{
			name: "Simple sentence",
			text: "Hello, world!",
			min:  3,
			max:  5,
		},
		{
			name: "Longer text",
			text: "This is a longer sentence that should have more tokens.",
			min:  10,
			max:  15,
		},
		{
			name: "Code snippet",
			text: "func main() { fmt.Println(\"Hello\") }",
			min:  8,
			max:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountTokens(tt.text)
			if tokens < tt.min || tokens > tt.max {
				t.Errorf("CountTokens() = %d, want between %d and %d", tokens, tt.min, tt.max)
			}
		})
	}
}

func TestTokenizer_CountMessageTokens(t *testing.T) {
	tokenizer := NewTokenizer()

	tests := []struct {
		name string
		msg  *schema.Message
		min  int
		max  int
	}{
		{
			name: "User message",
			msg: &schema.Message{
				Role:    schema.User,
				Content: "Hello, how are you?",
			},
			min: 5,
			max: 10,
		},
		{
			name: "Assistant message",
			msg: &schema.Message{
				Role:    schema.Assistant,
				Content: "I'm doing great, thanks for asking!",
			},
			min: 8,
			max: 15,
		},
		{
			name: "System message",
			msg: &schema.Message{
				Role:    schema.System,
				Content: "You are a helpful assistant.",
			},
			min: 5,
			max: 15,
		},
		{
			name: "Tool message",
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    "Tool output: Result successful",
				ToolCallID: "call_123",
			},
			min: 5,
			max: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountMessageTokens(tt.msg)
			if tokens < tt.min || tokens > tt.max {
				t.Errorf("CountMessageTokens() = %d, want between %d and %d", tokens, tt.min, tt.max)
			}
		})
	}
}

func TestTokenizer_CountMessagesTokens(t *testing.T) {
	tokenizer := NewTokenizer()

	tests := []struct {
		name     string
		messages []*schema.Message
		min      int
		max      int
	}{
		{
			name:     "Empty message list",
			messages: []*schema.Message{},
			min:      3, // Always adds 3 tokens for assistant reply
			max:      3,
		},
		{
			name: "Single message",
			messages: []*schema.Message{
				{
					Role:    schema.User,
					Content: "Hello",
				},
			},
			min: 5,
			max: 10,
		},
		{
			name: "Multiple messages",
			messages: []*schema.Message{
				{
					Role:    schema.User,
					Content: "What is the weather?",
				},
				{
					Role:    schema.Assistant,
					Content: "The weather is sunny.",
				},
			},
			min: 15,
			max: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenizer.CountMessagesTokens(tt.messages)
			if tokens < tt.min || tokens > tt.max {
				t.Errorf("CountMessagesTokens() = %d, want between %d and %d", tokens, tt.min, tt.max)
			}
		})
	}
}
