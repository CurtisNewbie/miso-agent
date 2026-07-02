package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

// mockFileStore is a simple in-memory FileStore for testing.
type mockFileStore struct {
	files  map[string][]byte
	failOn string // if set, WriteFile returns an error for this path
}

func newMockFileStore() *mockFileStore {
	return &mockFileStore{files: make(map[string][]byte)}
}

func (m *mockFileStore) ReadFile(_ context.Context, path string) ([]byte, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

func (m *mockFileStore) WriteFile(_ context.Context, path string, content []byte) error {
	if m.failOn != "" && path == m.failOn {
		return errors.New("write failed")
	}
	m.files[path] = content
	return nil
}

func (m *mockFileStore) ListDirectory(_ context.Context, _ string) ([]FileInfo, error) {
	return nil, nil
}

func (m *mockFileStore) FileExists(_ context.Context, path string) (bool, error) {
	_, ok := m.files[path]
	return ok, nil
}

func (m *mockFileStore) DeleteFile(_ context.Context, path string) error {
	delete(m.files, path)
	return nil
}

// TestSanitizeForPath verifies safe chars are kept and unsafe chars become underscores.
func TestSanitizeForPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "safe chars unchanged", input: "abc-123.def_ghi", want: "abc-123.def_ghi"},
		{name: "slash replaced", input: "call/id/123", want: "call_id_123"},
		{name: "colon replaced", input: "call:id:v2", want: "call_id_v2"},
		{name: "space replaced", input: "tool call id", want: "tool_call_id"},
		{name: "mixed unsafe", input: "abc@#$%^&*()", want: "abc_________"},
		{name: "empty string", input: "", want: ""},
		{name: "all safe", input: "A-Z.a-z_0-9", want: "A-Z.a-z_0-9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForPath(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBuildPreview verifies short text is returned as-is and long text is truncated.
func TestBuildPreview(t *testing.T) {
	t.Run("short text returned as-is", func(t *testing.T) {
		short := strings.Repeat("a", offloadPreviewHeadChars+offloadPreviewTailChars)
		got := buildPreview(short)
		if got != short {
			t.Errorf("expected short text unchanged, got different output")
		}
	})

	t.Run("exact boundary returned as-is", func(t *testing.T) {
		exact := strings.Repeat("b", offloadPreviewHeadChars+offloadPreviewTailChars)
		got := buildPreview(exact)
		if got != exact {
			t.Errorf("exact boundary text should be returned unchanged")
		}
	})

	t.Run("long text truncated with separator", func(t *testing.T) {
		long := strings.Repeat("x", offloadPreviewHeadChars+offloadPreviewTailChars+500)
		got := buildPreview(long)
		if !strings.Contains(got, "[... 500 characters omitted ...]") {
			t.Errorf("expected omission separator in preview, got: %s", got[:min(200, len(got))])
		}
		// Head should be first offloadPreviewHeadChars runes
		head := string([]rune(long)[:offloadPreviewHeadChars])
		if !strings.HasPrefix(got, head) {
			t.Errorf("preview should start with head content")
		}
		// Tail should be last offloadPreviewTailChars runes
		runes := []rune(long)
		tail := string(runes[len(runes)-offloadPreviewTailChars:])
		if !strings.HasSuffix(got, tail) {
			t.Errorf("preview should end with tail content")
		}
	})
}

// TestMaybeOffloadToolResult covers all branching cases.
func TestMaybeOffloadToolResult(t *testing.T) {
	tokenizer := NewTokenizer()
	const tokenLimit = 10 // small limit for tests; 10 tokens ≈ 40 chars

	bigContent := strings.Repeat("a", (tokenLimit+1)*4+1) // > tokenLimit tokens

	t.Run("non-tool message returned as-is", func(t *testing.T) {
		msg := schema.UserMessage("hello world")
		store := newMockFileStore()
		got := maybeOffloadToolResult(context.Background(), msg, "some_tool", store, tokenizer, tokenLimit, "/prefix")
		if got != msg {
			t.Errorf("expected original message pointer, got different")
		}
	})

	t.Run("excluded tool returned as-is", func(t *testing.T) {
		msg := &schema.Message{Role: schema.Tool, Content: bigContent, ToolCallID: "id1"}
		store := newMockFileStore()
		for toolName := range offloadExcludedTools {
			got := maybeOffloadToolResult(context.Background(), msg, toolName, store, tokenizer, tokenLimit, "/prefix")
			if got != msg {
				t.Errorf("excluded tool %q: expected original message pointer", toolName)
			}
		}
	})

	t.Run("content under threshold returned as-is", func(t *testing.T) {
		smallContent := strings.Repeat("a", tokenLimit*4-4) // < tokenLimit tokens
		msg := &schema.Message{Role: schema.Tool, Content: smallContent, ToolCallID: "id2"}
		store := newMockFileStore()
		got := maybeOffloadToolResult(context.Background(), msg, "some_tool", store, tokenizer, tokenLimit, "/prefix")
		if got != msg {
			t.Errorf("expected original message pointer for small content")
		}
	})

	t.Run("content over threshold, write succeeds → replacement returned", func(t *testing.T) {
		msg := &schema.Message{Role: schema.Tool, Content: bigContent, ToolCallID: "call_abc123"}
		store := newMockFileStore()
		got := maybeOffloadToolResult(context.Background(), msg, "web_search", store, tokenizer, tokenLimit, "/large_tool_results")
		if got == msg {
			t.Fatal("expected a new replacement message, got original")
		}
		if !strings.Contains(got.Content, "/large_tool_results/call_abc123") {
			t.Errorf("replacement should contain file path, got: %s", got.Content)
		}
		// Original content should be stored.
		expectedPath := "/large_tool_results/call_abc123"
		stored, ok := store.files[expectedPath]
		if !ok {
			t.Errorf("expected content written to store at %s", expectedPath)
		}
		if string(stored) != bigContent {
			t.Errorf("stored content mismatch")
		}
		// ToolCallID preserved on replacement message.
		if got.ToolCallID != msg.ToolCallID {
			t.Errorf("ToolCallID should be preserved")
		}
	})

	t.Run("content over threshold, write fails → original returned non-fatal", func(t *testing.T) {
		msg := &schema.Message{Role: schema.Tool, Content: bigContent, ToolCallID: "call_fail"}
		store := newMockFileStore()
		store.failOn = "/prefix/call_fail"
		got := maybeOffloadToolResult(context.Background(), msg, "web_search", store, tokenizer, tokenLimit, "/prefix")
		if got != msg {
			t.Errorf("on write failure, original message should be returned")
		}
	})

	t.Run("unsafe call ID sanitized in file path", func(t *testing.T) {
		msg := &schema.Message{Role: schema.Tool, Content: bigContent, ToolCallID: "call/abc:123"}
		store := newMockFileStore()
		got := maybeOffloadToolResult(context.Background(), msg, "web_search", store, tokenizer, tokenLimit, "/prefix")
		if got == msg {
			t.Fatal("expected replacement message")
		}
		expectedPath := "/prefix/call_abc_123"
		if _, ok := store.files[expectedPath]; !ok {
			t.Errorf("expected sanitized path %s in store, got keys: %v", expectedPath, store.files)
		}
	})
}

// TestOffloadToolResults tests the offloadToolResults helper directly.
func TestOffloadToolResults(t *testing.T) {
	tokenizer := NewTokenizer()
	const tokenLimit = 10 // 10 tokens ≈ 40 chars

	bigContent := strings.Repeat("a", (tokenLimit+1)*4+1) // > tokenLimit tokens

	t.Run("tokenLimit zero → passthrough", func(t *testing.T) {
		msgs := []*schema.Message{schema.UserMessage("hello")}
		out := offloadToolResults(context.Background(), msgs, nil, newMockFileStore(), tokenizer, 0, "/prefix")
		if len(out) != 1 || out[0] != msgs[0] {
			t.Errorf("expected passthrough of original slice when tokenLimit is 0")
		}
	})

	t.Run("store nil → passthrough", func(t *testing.T) {
		msgs := []*schema.Message{schema.UserMessage("hello")}
		out := offloadToolResults(context.Background(), msgs, nil, nil, tokenizer, tokenLimit, "/prefix")
		if len(out) != 1 || out[0] != msgs[0] {
			t.Errorf("expected passthrough when store is nil")
		}
	})

	t.Run("tool message over threshold → offloaded", func(t *testing.T) {
		store := newMockFileStore()
		// Accumulated messages contain the assistant message that issued the tool call.
		assistantMsg := &schema.Message{
			Role: schema.Assistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call_xyz", Function: schema.FunctionCall{Name: "web_search"}},
			},
		}
		accumulated := []*schema.Message{assistantMsg}
		toolMsg := &schema.Message{Role: schema.Tool, Content: bigContent, ToolCallID: "call_xyz"}
		msgs := []*schema.Message{toolMsg}

		out := offloadToolResults(context.Background(), msgs, accumulated, store, tokenizer, tokenLimit, "/prefix")
		if len(out) != 1 {
			t.Fatalf("expected 1 output message, got %d", len(out))
		}
		if out[0] == toolMsg {
			t.Fatal("expected replacement message, got original pointer")
		}
		if !strings.Contains(out[0].Content, "/prefix/call_xyz") {
			t.Errorf("replacement should reference file path, got: %s", out[0].Content)
		}
		if _, ok := store.files["/prefix/call_xyz"]; !ok {
			t.Errorf("expected content written to store at /prefix/call_xyz")
		}
	})

	t.Run("non-tool message → passed through unchanged", func(t *testing.T) {
		store := newMockFileStore()
		userMsg := schema.UserMessage(bigContent)
		msgs := []*schema.Message{userMsg}
		out := offloadToolResults(context.Background(), msgs, nil, store, tokenizer, tokenLimit, "/prefix")
		if len(out) != 1 || out[0] != userMsg {
			t.Errorf("non-tool message should be passed through unchanged")
		}
	})
}
