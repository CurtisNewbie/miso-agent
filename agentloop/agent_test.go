package agentloop

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestAgent_shouldEvictToolResult(t *testing.T) {
	tests := []struct {
		name        string
		config      AgentConfig
		msg         *schema.Message
		wantEvict   bool
		description string
	}{
		{
			name: "Eviction disabled (threshold = 0)",
			config: AgentConfig{
				EvictToolResultsThreshold: 0,
			},
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    generateLargeContent(2000), // Large content
				ToolCallID: "call_123",
			},
			wantEvict:   false,
			description: "Should not evict when threshold is 0",
		},
		{
			name: "Eviction disabled (threshold negative)",
			config: AgentConfig{
				EvictToolResultsThreshold: -1,
			},
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    generateLargeContent(2000),
				ToolCallID: "call_123",
			},
			wantEvict:   false,
			description: "Should not evict when threshold is negative",
		},
		{
			name: "Not a tool message",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			msg: &schema.Message{
				Role:    schema.User,
				Content: generateLargeContent(2000),
			},
			wantEvict:   false,
			description: "Should not evict non-tool messages",
		},
		{
			name: "Tool message without ToolCallID",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			msg: &schema.Message{
				Role:    schema.Tool,
				Content: generateLargeContent(2000),
			},
			wantEvict:   false,
			description: "Should not evict tool messages without ToolCallID",
		},
		{
			name: "Small tool result",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    generateLargeContent(100), // Small content
				ToolCallID: "call_123",
			},
			wantEvict:   false,
			description: "Should not evict small tool results",
		},
		{
			name: "Large tool result",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    generateLargeContent(2000), // Large content
				ToolCallID: "call_123",
			},
			wantEvict:   true,
			description: "Should evict large tool results",
		},
		{
			name: "Exactly at threshold",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			msg: &schema.Message{
				Role:       schema.Tool,
				Content:    generateLargeContent(1000), // Exactly at threshold
				ToolCallID: "call_123",
			},
			wantEvict:   false,
			description: "Should not evict when exactly at threshold (only evict > threshold)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer, err := NewTokenizer("gpt-3.5-turbo")
			if err != nil {
				t.Fatalf("Failed to create tokenizer: %v", err)
			}

			agent := &Agent{
				config:    tt.config,
				tokenizer: tokenizer,
			}

			got := agent.shouldEvictToolResult(tt.msg)
			if got != tt.wantEvict {
				t.Errorf("shouldEvictToolResult() = %v, want %v (%s)", got, tt.wantEvict, tt.description)
			}
		})
	}
}

func TestAgent_evictLargeToolResults(t *testing.T) {
	tests := []struct {
		name        string
		config      AgentConfig
		messages    []*schema.Message
		wantEvicted int
		description string
	}{
		{
			name: "Eviction disabled",
			config: AgentConfig{
				EvictToolResultsThreshold: 0,
			},
			messages: []*schema.Message{
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_1",
				},
			},
			wantEvicted: 0,
			description: "Should not evict when threshold is 0",
		},
		{
			name: "No tool messages",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			messages: []*schema.Message{
				{Role: schema.User, Content: "Hello"},
				{Role: schema.Assistant, Content: "Hi"},
			},
			wantEvicted: 0,
			description: "Should not evict when there are no tool messages",
		},
		{
			name: "Last message is large tool result",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			messages: []*schema.Message{
				{Role: schema.User, Content: "Hello"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_1",
				},
			},
			wantEvicted: 0,
			description: "Should not evict last message (agent is actively using it)",
		},
		{
			name: "Multiple messages, last is large tool result",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			messages: []*schema.Message{
				{Role: schema.User, Content: "First"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_1",
				},
				{Role: schema.User, Content: "Second"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_2",
				},
			},
			wantEvicted: 1,
			description: "Should evict first large tool result but not last",
		},
		{
			name: "Multiple large tool results",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			messages: []*schema.Message{
				{Role: schema.User, Content: "First"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_1",
				},
				{Role: schema.User, Content: "Second"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_2",
				},
				{Role: schema.User, Content: "Third"},
			},
			wantEvicted: 2,
			description: "Should evict all large tool results except last",
		},
		{
			name: "Mixed small and large tool results",
			config: AgentConfig{
				EvictToolResultsThreshold: 1000,
			},
			messages: []*schema.Message{
				{Role: schema.User, Content: "First"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(2000),
					ToolCallID: "call_1",
				},
				{Role: schema.User, Content: "Second"},
				{
					Role:       schema.Tool,
					Content:    generateLargeContent(100),
					ToolCallID: "call_2",
				},
				{Role: schema.User, Content: "Third"},
			},
			wantEvicted: 1,
			description: "Should evict only large tool results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenizer, err := NewTokenizer("gpt-3.5-turbo")
			if err != nil {
				t.Fatalf("Failed to create tokenizer: %v", err)
			}

			be := NewMemFileStore()
			config := tt.config
			config.BackendFactory = func() FileStore { return be }

			agent := &Agent{
				config:    config,
				tokenizer: tokenizer,
			}
			result := agent.evictLargeToolResults(be, tt.messages)

			// Count evicted messages
			evicted := 0
			for i, msg := range result {
				if msg.Role == schema.Tool && i < len(result)-1 {
					// Check if this was evicted (contains reference message)
					if containsString(msg.Content, "[Tool result evicted to:") {
						evicted++
					}
				}
			}

			if evicted != tt.wantEvicted {
				t.Errorf("evictLargeToolResults() evicted %d messages, want %d (%s)", evicted, tt.wantEvicted, tt.description)
			}
		})
	}
}

func TestAgent_evictToolResult(t *testing.T) {
	be := NewMemFileStore()
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	config := AgentConfig{
		EvictToolResultsKeepPreview: 100,
	}
	agent := &Agent{
		config:    config,
		tokenizer: tokenizer,
	}

	originalMsg := &schema.Message{
		Role:       schema.Tool,
		Content:    generateLargeContent(2000),
		ToolCallID: "call_123",
	}

	result := agent.evictToolResult(be, originalMsg)

	// Verify reference message structure
	if !containsString(result.Content, "[Tool result evicted to:") {
		t.Error("evictToolResult() should create reference message with eviction marker")
	}

	if !containsString(result.Content, "Tokens:") {
		t.Error("evictToolResult() should include token count in reference")
	}

	if !containsString(result.Content, "read_file") {
		t.Error("evictToolResult() should include read_file instructions")
	}

	// Verify ToolCallID is preserved
	if result.ToolCallID != originalMsg.ToolCallID {
		t.Errorf("evictToolResult() should preserve ToolCallID, got %v, want %v", result.ToolCallID, originalMsg.ToolCallID)
	}

	// Verify Role is preserved
	if result.Role != originalMsg.Role {
		t.Errorf("evictToolResult() should preserve Role, got %v, want %v", result.Role, originalMsg.Role)
	}
}

func TestAgent_evictToolResult_WithPreview(t *testing.T) {
	be := NewMemFileStore()
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	config := AgentConfig{
		EvictToolResultsKeepPreview: 100,
	}
	agent := &Agent{
		config:    config,
		tokenizer: tokenizer,
	}

	originalMsg := &schema.Message{
		Role:       schema.Tool,
		Content:    generateLargeContent(2000),
		ToolCallID: "call_123",
	}

	result := agent.evictToolResult(be, originalMsg)

	// Verify preview is included
	if !containsString(result.Content, "Preview:") {
		t.Error("evictToolResult() should include preview when configured")
	}

	// Verify file was stored in backend
	if !containsString(result.Content, "/tool-results/") {
		t.Error("evictToolResult() should store file in /tool-results/ directory")
	}
}

func TestAgent_evictToolResult_NoPreview(t *testing.T) {
	be := NewMemFileStore()
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	config := AgentConfig{
		EvictToolResultsKeepPreview: 0,
	}
	agent := &Agent{
		config:    config,
		tokenizer: tokenizer,
	}

	originalMsg := &schema.Message{
		Role:       schema.Tool,
		Content:    generateLargeContent(2000),
		ToolCallID: "call_123",
	}

	result := agent.evictToolResult(be, originalMsg)

	// Verify preview is empty when disabled (no truncated marker)
	if containsString(result.Content, "... [truncated]") {
		t.Error("evictToolResult() should not include preview content when disabled")
	}
}

func TestAgent_evictToolResult_WriteFailure(t *testing.T) {
	// Create a backend that always fails to write
	be := &mockFailingBackend{}
	tokenizer, err := NewTokenizer("gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer: %v", err)
	}

	config := AgentConfig{}
	agent := &Agent{
		config:    config,
		tokenizer: tokenizer,
	}

	originalMsg := &schema.Message{
		Role:       schema.Tool,
		Content:    generateLargeContent(2000),
		ToolCallID: "call_123",
	}

	result := agent.evictToolResult(be, originalMsg)

	// On write failure, should return original message
	if result.Content != originalMsg.Content {
		t.Error("evictToolResult() should return original message on write failure")
	}
}

// Helper functions

func generateLargeContent(wordCount int) string {
	words := make([]string, wordCount)
	for i := 0; i < wordCount; i++ {
		words[i] = "word"
	}
	result := ""
	for i, word := range words {
		if i > 0 {
			result += " "
		}
		result += word
	}
	return result
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockFailingBackend is a mock backend that always fails to write
type mockFailingBackend struct{}

func (m *mockFailingBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return nil, nil
}

func (m *mockFailingBackend) WriteFile(ctx context.Context, path string, content []byte) error {
	return &mockError{msg: "write failed"}
}

func (m *mockFailingBackend) ListDirectory(ctx context.Context, path string) ([]FileInfo, error) {
	return nil, nil
}

func (m *mockFailingBackend) FileExists(ctx context.Context, path string) (bool, error) {
	return false, nil
}

func (m *mockFailingBackend) DeleteFile(ctx context.Context, path string) error {
	return nil
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
