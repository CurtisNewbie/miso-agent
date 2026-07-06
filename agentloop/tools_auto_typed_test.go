package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type descTagArgs struct {
	Path   string `json:"path"             desc:"Absolute path to the file"`
	Offset int    `json:"offset,omitempty" desc:"Line to start from"`
}

func buildDescTagTool(t *testing.T) Tool {
	t.Helper()
	return NewAutoTypedCtxAwareToolFunc[descTagArgs](
		"desc_tag_tool",
		"Test desc tag reflection",
		func(ctx context.Context, agentCtx AgentContext, args descTagArgs) (string, error) {
			return args.Path, nil
		},
	)
}

func schemaToMap(t *testing.T, tool Tool) map[string]interface{} {
	t.Helper()
	dt, ok := tool.(deductedTool)
	if !ok {
		t.Fatalf("tool does not implement deductedTool")
	}
	jsonSchema, err := dt.ParamsOneOf().ToJSONSchema()
	if err != nil {
		t.Fatalf("ToJSONSchema failed: %v", err)
	}
	raw, err := json.Marshal(jsonSchema)
	if err != nil {
		t.Fatalf("marshal schema failed: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal schema map failed: %v", err)
	}
	return m
}

func TestAutoSchemaModifier_DescTag(t *testing.T) {
	tool := buildDescTagTool(t)
	m := schemaToMap(t, tool)

	props, ok := m["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map, got %T", m["properties"])
	}

	tests := []struct {
		field    string
		wantDesc string
	}{
		{"path", "Absolute path to the file"},
		{"offset", "Line to start from"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			fieldSchema, ok := props[tt.field].(map[string]interface{})
			if !ok {
				t.Fatalf("field %q not found in properties", tt.field)
			}
			desc, _ := fieldSchema["description"].(string)
			if desc != tt.wantDesc {
				t.Errorf("field %q: want description %q, got %q", tt.field, tt.wantDesc, desc)
			}
		})
	}
}

func TestAutoSchemaModifier_RequiredFromOmitempty(t *testing.T) {
	tool := buildDescTagTool(t)
	m := schemaToMap(t, tool)

	required, _ := m["required"].([]interface{})
	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	if !requiredSet["path"] {
		t.Errorf("expected 'path' (no omitempty) to be in required, got required=%v", required)
	}
	if requiredSet["offset"] {
		t.Errorf("expected 'offset' (has omitempty) to NOT be in required, got required=%v", required)
	}
}

type greetArgs struct {
	Name string `json:"name" desc:"Name to greet"`
}

func TestNewAutoTypedCtxAwareToolFunc_Execute(t *testing.T) {
	tool := NewAutoTypedCtxAwareToolFunc[greetArgs](
		"greet",
		"Greet someone",
		func(ctx context.Context, agentCtx AgentContext, args greetArgs) (string, error) {
			return "Hello, " + args.Name, nil
		},
	)

	sit, ok := tool.(SelfInvokeTool)
	if !ok {
		t.Fatalf("tool does not implement SelfInvokeTool")
	}

	result, err := sit.ExecuteJson(context.Background(), `{"name":"World"}`)
	if err != nil {
		t.Fatalf("ExecuteJson failed: %v", err)
	}
	if result != "Hello, World" {
		t.Errorf("expected 'Hello, World', got %q", result)
	}
}

func TestNewAutoTypedCtxAwareToolFunc_AgentContextInjected(t *testing.T) {
	var capturedCtx AgentContext
	tool := NewAutoTypedCtxAwareToolFunc[greetArgs](
		"greet_ctx",
		"Greet with context",
		func(ctx context.Context, agentCtx AgentContext, args greetArgs) (string, error) {
			capturedCtx = agentCtx
			return "ok", nil
		},
	)

	store := newTestMemFileStore()
	todos := NewTodoManager()
	agentCtx := AgentContext{Store: store, Todos: todos}
	ctx := context.WithValue(context.Background(), agentCtxKey, agentCtx)

	sit := tool.(SelfInvokeTool)
	_, err := sit.ExecuteJson(ctx, `{"name":"test"}`)
	if err != nil {
		t.Fatalf("ExecuteJson failed: %v", err)
	}

	if capturedCtx.Store == nil {
		t.Errorf("expected AgentContext.Store to be non-nil")
	}
	if capturedCtx.Todos == nil {
		t.Errorf("expected AgentContext.Todos to be non-nil")
	}
}

type emptyArgs struct {
	Value string `json:"value,omitempty" desc:"Optional value"`
}

func TestNewAutoTypedCtxAwareToolFunc_EmptyArgs(t *testing.T) {
	tool := NewAutoTypedCtxAwareToolFunc[emptyArgs](
		"empty_tool",
		"Tool with empty args",
		func(ctx context.Context, agentCtx AgentContext, args emptyArgs) (string, error) {
			return "value=" + args.Value, nil
		},
	)

	sit := tool.(SelfInvokeTool)
	result, err := sit.ExecuteJson(context.Background(), "")
	if err != nil {
		t.Fatalf("ExecuteJson with empty args failed: %v", err)
	}
	if result != "value=" {
		t.Errorf("expected 'value=', got %q", result)
	}
}

func TestNewAutoTypedCtxAwareToolFunc_ExecuteError(t *testing.T) {
	wantErr := errors.New("execution failed")
	tool := NewAutoTypedCtxAwareToolFunc[greetArgs](
		"error_tool",
		"Always errors",
		func(ctx context.Context, agentCtx AgentContext, args greetArgs) (string, error) {
			return "", wantErr
		},
	)

	sit := tool.(SelfInvokeTool)
	_, gotErr := sit.ExecuteJson(context.Background(), `{"name":"test"}`)
	if gotErr == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(gotErr.Error(), "execution failed") {
		t.Errorf("expected error to contain 'execution failed', got %q", gotErr.Error())
	}
}

func TestToolWrapper_Info_DeductedTool(t *testing.T) {
	tool := NewAutoTypedCtxAwareToolFunc[greetArgs](
		"greet_info",
		"Greet tool for info test",
		func(ctx context.Context, agentCtx AgentContext, args greetArgs) (string, error) {
			return "Hello, " + args.Name, nil
		},
	)

	w := &toolWrapper{tool: tool}
	info, err := w.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() failed: %v", err)
	}

	if info.Name != "greet_info" {
		t.Errorf("expected Name='greet_info', got %q", info.Name)
	}
	if info.Desc != "Greet tool for info test" {
		t.Errorf("expected Desc='Greet tool for info test', got %q", info.Desc)
	}
	if info.ParamsOneOf == nil {
		t.Errorf("expected ParamsOneOf to be non-nil")
	}

	// Verify ParamsOneOf matches what deductedTool.ParamsOneOf() returns directly
	dt, ok := tool.(deductedTool)
	if !ok {
		t.Fatalf("tool does not implement deductedTool")
	}
	if info.ParamsOneOf != dt.ParamsOneOf() {
		t.Errorf("expected Info().ParamsOneOf to be the same pointer as dt.ParamsOneOf()")
	}
}
