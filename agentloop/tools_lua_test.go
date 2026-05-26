package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateLuaInputPath_EmptyPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "empty string", path: ""},
		{name: "whitespace only", path: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateLuaInputPath(tt.path); err == nil {
				t.Error("expected error for empty path, got nil")
			}
		})
	}
}

func TestValidateLuaInputPath_TraversalRejected(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "simple traversal", path: "/input/../etc/passwd"},
		{name: "traversal at start", path: "/../secret"},
		{name: "double traversal", path: "/a/../../b"},
		{name: "traversal only", path: ".."},
		{name: "traversal segment", path: "/input/.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLuaInputPath(tt.path)
			if err == nil {
				t.Errorf("expected traversal error for path %q, got nil", tt.path)
			}
			if !strings.Contains(err.Error(), "traversal") {
				t.Errorf("expected 'traversal' in error message, got: %v", err)
			}
		})
	}
}

func TestValidateLuaInputPath_ValidPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "absolute path", path: "/input/data.csv"},
		{name: "nested path", path: "/input/subdir/file.txt"},
		{name: "relative path", path: "input/data.csv"},
		{name: "root file", path: "/data.csv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateLuaInputPath(tt.path); err != nil {
				t.Errorf("expected no error for path %q, got: %v", tt.path, err)
			}
		})
	}
}

func TestRunLuaScript_ReturnString(t *testing.T) {
	result, err := runLuaScript(`return "hello"`, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestRunLuaScript_InputInjected(t *testing.T) {
	script := `return input`
	result, err := runLuaScript(script, "csv content here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "csv content here" {
		t.Errorf("expected input to be returned, got %q", result)
	}
}

func TestRunLuaScript_TransformInput(t *testing.T) {
	script := `
local out = {}
for line in input:gmatch("[^\n]+") do
    out[#out + 1] = "row: " .. line
end
return table.concat(out, "\n")
`
	input := "a,b\nc,d"
	result, err := runLuaScript(script, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "row: a,b\nrow: c,d"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestRunLuaScript_NoReturnValue(t *testing.T) {
	_, err := runLuaScript(`local x = 1`, "")
	if err == nil {
		t.Error("expected error for script with no return value, got nil")
	}
	if !strings.Contains(err.Error(), "did not return a value") {
		t.Errorf("expected 'did not return a value' in error, got: %v", err)
	}
}

func TestRunLuaScript_NonStringReturn(t *testing.T) {
	_, err := runLuaScript(`return 42`, "")
	if err == nil {
		t.Error("expected error for non-string return, got nil")
	}
	if !strings.Contains(err.Error(), "must return a string") {
		t.Errorf("expected 'must return a string' in error, got: %v", err)
	}
}

func TestRunLuaScript_SyntaxError(t *testing.T) {
	_, err := runLuaScript(`this is not lua !!!`, "")
	if err == nil {
		t.Error("expected error for invalid Lua syntax, got nil")
	}
}

func TestRunLuaScript_RuntimeError(t *testing.T) {
	_, err := runLuaScript(`return undefined_func()`, "")
	if err == nil {
		t.Error("expected error for runtime error, got nil")
	}
}

func TestRunLuaScript_OsBlocked(t *testing.T) {
	_, err := runLuaScript(`return os.exit()`, "")
	if err == nil {
		t.Error("expected error when accessing blocked 'os' package, got nil")
	}
}

func TestRunLuaScript_IoBlocked(t *testing.T) {
	_, err := runLuaScript(`return io.open("file.txt")`, "")
	if err == nil {
		t.Error("expected error when accessing blocked 'io' package, got nil")
	}
}

func TestRunLuaScript_RequireBlocked(t *testing.T) {
	_, err := runLuaScript(`require("os")`, "")
	if err == nil {
		t.Error("expected error when calling blocked 'require', got nil")
	}
}

func TestNewTransformCsvLuaTool_Success(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	if err := be.WriteFile(ctx, "/input/data.csv", []byte("name,age\nAlice,30\nBob,25")); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/data.csv",
		"script":     `return input`,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "name,age\nAlice,30\nBob,25" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestNewTransformCsvLuaTool_FileNotFound(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/missing.csv",
		"script":     `return input`,
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestNewTransformCsvLuaTool_TraversalRejected(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/../secret.txt",
		"script":     `return input`,
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for traversal path, got nil")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected 'traversal' in error, got: %v", err)
	}
}

func TestNewTransformCsvLuaTool_EmptyInputPath(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "",
		"script":     `return input`,
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for empty input_path, got nil")
	}
}

func TestNewTransformCsvLuaTool_EmptyScript(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	be.WriteFile(ctx, "/input/data.csv", []byte("a,b"))

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/data.csv",
		"script":     "",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for empty script, got nil")
	}
}

func TestNewTransformCsvLuaTool_ScriptError(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	be.WriteFile(ctx, "/input/data.csv", []byte("a,b"))

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/data.csv",
		"script":     `error("intentional failure")`,
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for failing script, got nil")
	}
	if !strings.Contains(err.Error(), "lua script execution failed") {
		t.Errorf("expected 'lua script execution failed' in error, got: %v", err)
	}
}

func TestRunLuaScript_RowsInjected(t *testing.T) {
	script := `
local out = {}
for i = 1, #rows do
    out[#out + 1] = rows[i][1] .. "=" .. rows[i][2]
end
return table.concat(out, ",")
`
	result, err := runLuaScript(script, "a,1\nb,2\nc,3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a=1,b=2,c=3" {
		t.Errorf("expected %q, got %q", "a=1,b=2,c=3", result)
	}
}

func TestRunLuaScript_RowsEmptyOnInvalidCSV(t *testing.T) {
	script := `return tostring(#rows)`
	result, err := runLuaScript(script, "\"unclosed quote")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "0" {
		t.Errorf("expected empty rows table (0) for invalid CSV, got %q", result)
	}
}

func TestRunLuaScript_RowsAndInputBothAvailable(t *testing.T) {
	script := `return rows[1][1] .. "|" .. input:sub(1, 1)`
	result, err := runLuaScript(script, "hello,world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello|h" {
		t.Errorf("expected %q, got %q", "hello|h", result)
	}
}

func TestNewTransformCsvLuaTool_RowsTransform(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	csv := "name,dept\nAlice,Eng\nBob,Product"
	if err := be.WriteFile(ctx, "/input/data.csv", []byte(csv)); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	script := `
local out = {}
local header = rows[1]
for i = 2, #rows do
    local parts = {}
    for j = 1, #header do
        parts[#parts + 1] = header[j] .. ": " .. rows[i][j]
    end
    out[#out + 1] = table.concat(parts, "\n")
end
return table.concat(out, "\n\n")
`
	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path": "/input/data.csv",
		"script":     script,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "name: Alice\ndept: Eng\n\nname: Bob\ndept: Product"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}
}

func TestNewTransformCsvLuaTool_OutputPath(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	if err := be.WriteFile(ctx, "/input/data.csv", []byte("a,b\n1,2")); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path":  "/input/data.csv",
		"output_path": "/output/result.txt",
		"script":      `return rows[1][1] .. "," .. rows[1][2]`,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "/output/result.txt") {
		t.Errorf("expected confirmation message with output path, got %q", result)
	}

	written, err := be.ReadFile(ctx, "/output/result.txt")
	if err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}
	if string(written) != "a,b" {
		t.Errorf("expected %q written to output_path, got %q", "a,b", string(written))
	}
}

func TestNewTransformCsvLuaTool_OutputPathTraversalRejected(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	be.WriteFile(ctx, "/input/data.csv", []byte("a,b"))

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	tool := NewTransformCsvLuaTool()
	args, _ := json.Marshal(map[string]interface{}{
		"input_path":  "/input/data.csv",
		"output_path": "/output/../secret.txt",
		"script":      `return input`,
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("expected error for traversal output_path, got nil")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected 'traversal' in error, got: %v", err)
	}
}
