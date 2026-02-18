package agentloop

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestNewTokenizer(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{
			name:    "GPT-4",
			model:   "gpt-4",
			wantErr: false,
		},
		{
			name:    "GPT-3.5-turbo",
			model:   "gpt-3.5-turbo",
			wantErr: false,
		},
		{
			name:    "GPT-4o",
			model:   "gpt-4o",
			wantErr: false,
		},
		{
			name:    "Unknown model",
			model:   "unknown-model",
			wantErr: true, // Will error for unknown model
		},
		{
			name:    "Empty model",
			model:   "",
			wantErr: false, // Should use default encoding
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer, err := NewTokenizer(tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTokenizer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tokenizer == nil {
				t.Error("NewTokenizer() returned nil tokenizer")
			}
		})
	}
}

func TestTokenizer_CountTokens(t *testing.T) {
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

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
			min:  10,
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
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

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
			max: 10,
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
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

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

func TestTokenizer_PruneMessagesToTokenLimit(t *testing.T) {
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	// Create a long conversation
	messages := []*schema.Message{
		{Role: schema.System, Content: "You are a helpful assistant."},
		{Role: schema.User, Content: "Hello"},
		{Role: schema.Assistant, Content: "Hi there!"},
		{Role: schema.User, Content: "How are you?"},
		{Role: schema.Assistant, Content: "I'm doing great!"},
		{Role: schema.User, Content: "What can you do?"},
		{Role: schema.Assistant, Content: "I can help with many tasks."},
	}

	tests := []struct {
		name         string
		messages     []*schema.Message
		maxTokens    int
		expectPruned bool
	}{
		{
			name:         "No pruning needed (high limit)",
			messages:     messages,
			maxTokens:    10000,
			expectPruned: false,
		},
		{
			name:         "Zero limit (will prune to system message)",
			messages:     messages,
			maxTokens:    0,
			expectPruned: true, // Will prune because 0 < current tokens
		},
		{
			name:         "Pruning needed (low limit)",
			messages:     messages,
			maxTokens:    50,
			expectPruned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenizer.PruneMessagesToTokenLimit(tt.messages, tt.maxTokens)

			if tt.expectPruned {
				if len(result) >= len(tt.messages) {
					t.Errorf("PruneMessagesToTokenLimit() should have reduced message count, got %d (was %d)", len(result), len(tt.messages))
				}
				// Verify last messages are kept (unless limit is 0, which only keeps system message)
				if tt.maxTokens > 0 && len(result) > 0 {
					lastOriginal := tt.messages[len(tt.messages)-1]
					lastResult := result[len(result)-1]
					if lastOriginal.Content != lastResult.Content {
						t.Errorf("PruneMessagesToTokenLimit() should keep last message, got %v, want %v", lastResult, lastOriginal)
					}
				}
			} else {
				if len(result) != len(tt.messages) {
					t.Errorf("PruneMessagesToTokenLimit() should not have pruned, got %d messages, want %d", len(result), len(tt.messages))
				}
			}

			// Verify token count is within limit
			if tt.maxTokens > 0 {
				resultTokens := tokenizer.CountMessagesTokens(result)
				if resultTokens > tt.maxTokens {
					t.Errorf("PruneMessagesToTokenLimit() result has %d tokens, exceeds limit %d", resultTokens, tt.maxTokens)
				}
			}
		})
	}
}

func TestTokenizer_PruneMessagesToTokenLimit_KeepsSystem(t *testing.T) {
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: "You are a helpful assistant."},
		{Role: schema.User, Content: "First message"},
		{Role: schema.User, Content: "Second message"},
		{Role: schema.User, Content: "Third message"},
		{Role: schema.User, Content: "Fourth message"},
	}

	// Set a low limit to force pruning
	result := tokenizer.PruneMessagesToTokenLimit(messages, 30)

	if len(result) == 0 {
		t.Fatal("PruneMessagesToTokenLimit() returned empty result")
	}

	// System message should be kept
	if result[0].Role != schema.System {
		t.Errorf("PruneMessagesToTokenLimit() should keep system message first, got role %v", result[0].Role)
	}

	// Last messages should be kept
	lastOriginal := messages[len(messages)-1]
	lastResult := result[len(result)-1]
	if lastOriginal.Content != lastResult.Content {
		t.Errorf("PruneMessagesToTokenLimit() should keep last message, got %v, want %v", lastResult, lastOriginal)
	}
}

func TestTokenizer_PruneMessagesToTokenLimit_PreservesOrder(t *testing.T) {
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	messages := []*schema.Message{
		{Role: schema.System, Content: "System"},
		{Role: schema.User, Content: "User1"},
		{Role: schema.Assistant, Content: "Assistant1"},
		{Role: schema.User, Content: "User2"},
		{Role: schema.Assistant, Content: "Assistant2"},
	}

	result := tokenizer.PruneMessagesToTokenLimit(messages, 20)

	if len(result) < 2 {
		t.Fatal("PruneMessagesToTokenLimit() should keep at least 2 messages")
	}

	// Verify order is preserved (system first, then alternating)
	expectedOrder := []schema.RoleType{schema.System, schema.User, schema.Assistant}
	for i, msg := range result {
		if i >= len(expectedOrder) {
			break
		}
		if msg.Role != expectedOrder[i] {
			t.Errorf("Message order not preserved at index %d, got role %v, want %v", i, msg.Role, expectedOrder[i])
		}
	}
}
