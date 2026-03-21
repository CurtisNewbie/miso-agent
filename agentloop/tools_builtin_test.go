package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBuiltinTools_ReadFile(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "test.txt"
	testContent := "Hello, World!"
	if err := be.WriteFile(ctx, testPath, []byte(testContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test reading the file
	tool, ok := registry.Get("read_file")
	if !ok {
		t.Fatal("read_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path": testPath,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if result != testContent {
		t.Errorf("Expected content %q, got %q", testContent, result)
	}
}

func TestBuiltinTools_ReadFile_NotFound(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	tool, ok := registry.Get("read_file")
	if !ok {
		t.Fatal("read_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path": "nonexistent.txt",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestBuiltinTools_WriteFile(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	testPath := "write_test.txt"
	testContent := "Written content"

	tool, ok := registry.Get("write_file")
	if !ok {
		t.Fatal("write_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path":    testPath,
		"content": testContent,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	if !strings.Contains(result, "Successfully wrote") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify content
	content, err := be.ReadFile(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(content))
	}
}

func TestBuiltinTools_EditFile_Success(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "edit_test.txt"
	initialContent := "Hello, World!\nThis is a test.\nGoodbye again."
	if err := be.WriteFile(ctx, testPath, []byte(initialContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test single replacement
	args, _ := json.Marshal(map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hi",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to edit file: %v", err)
	}

	if !strings.Contains(result, "Successfully replaced 1 instance(s)") {
		t.Errorf("Expected success message with 1 occurrence, got %q", result)
	}

	// Verify the replacement
	content, err := be.ReadFile(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to read edited file: %v", err)
	}

	expectedContent := "Hi, World!\nThis is a test.\nGoodbye again."
	if string(content) != expectedContent {
		t.Errorf("Expected content %q, got %q", expectedContent, string(content))
	}
}

func TestBuiltinTools_EditFile_ReplaceAll(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "edit_all_test.txt"
	initialContent := "Hello, World!\nHello test.\nHello again."
	if err := be.WriteFile(ctx, testPath, []byte(initialContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test replace all
	args, _ := json.Marshal(map[string]interface{}{
		"path":        testPath,
		"old_string":  "Hello",
		"new_string":  "Hi",
		"replace_all": true,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to edit file: %v", err)
	}

	if !strings.Contains(result, "Successfully replaced 3 instance(s)") {
		t.Errorf("Expected success message with 3 occurrences, got %q", result)
	}

	// Verify the replacement
	content, err := be.ReadFile(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to read edited file: %v", err)
	}

	expectedContent := "Hi, World!\nHi test.\nHi again."
	if string(content) != expectedContent {
		t.Errorf("Expected content %q, got %q", expectedContent, string(content))
	}
}

func TestBuiltinTools_EditFile_NotFound(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "edit_notfound_test.txt"
	initialContent := "Hello, World!"
	if err := be.WriteFile(ctx, testPath, []byte(initialContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test string not found
	args, _ := json.Marshal(map[string]interface{}{
		"path":       testPath,
		"old_string": "Goodbye",
		"new_string": "Hi",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for string not found, got nil")
	}

	if !strings.Contains(err.Error(), "String not found") {
		t.Errorf("Expected 'String not found' error, got %v", err)
	}
}

func TestBuiltinTools_EditFile_MultipleOccurrencesNoReplaceAll(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()

	// Setup AgentContext for tool execution
	agentCtx := AgentContext{
		Store: be,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file with multiple occurrences
	testPath := "edit_multi_test.txt"
	initialContent := "Hello, World!\nHello test.\nHello again."
	if err := be.WriteFile(ctx, testPath, []byte(initialContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	// Test multiple occurrences without replace_all flag
	args, _ := json.Marshal(map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hi",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for multiple occurrences without replace_all, got nil")
	}

	if !strings.Contains(err.Error(), "appears 3 times") {
		t.Errorf("Expected error about multiple occurrences, got %v", err)
	}
}

func TestBuiltinTools_EditFile_SameStrings(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "edit_same_test.txt"
	initialContent := "Hello, World!"
	if err := be.WriteFile(ctx, testPath, []byte(initialContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test same old_string and new_string
	args, _ := json.Marshal(map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hello",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for same old_string and new_string, got nil")
	}

	if !strings.Contains(err.Error(), "must be different") {
		t.Errorf("Expected error about different strings, got %v", err)
	}
}

func TestBuiltinTools_EditFile_FileNotFound(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test editing nonexistent file
	args, _ := json.Marshal(map[string]interface{}{
		"path":       "nonexistent.txt",
		"old_string": "Hello",
		"new_string": "Hi",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("Expected error about reading file, got %v", err)
	}
}

func TestNewThinkTool_Success(t *testing.T) {
	ctx := context.Background()
	tool := NewThinkTool()

	args, _ := json.Marshal(map[string]interface{}{
		"reflection": "I have gathered information about the topic. I need to search for more specific details.",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to execute think tool: %v", err)
	}

	expected := "Reflection recorded: I have gathered information about the topic. I need to search for more specific details."
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestNewThinkTool_MissingReflection(t *testing.T) {
	ctx := context.Background()
	tool := NewThinkTool()

	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, "{}")
	if err == nil {
		t.Error("Expected error for missing reflection, got nil")
	}

	if !strings.Contains(err.Error(), "reflection is required") {
		t.Errorf("Expected 'reflection is required' error, got %v", err)
	}
}

func TestNewThinkTool_EmptyReflection(t *testing.T) {
	ctx := context.Background()
	tool := NewThinkTool()

	args, _ := json.Marshal(map[string]interface{}{
		"reflection": "",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to execute think tool with empty reflection: %v", err)
	}

	expected := "Reflection recorded: "
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuiltinTools_ListDirectory(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files and directories
	be.WriteFile(ctx, "file1.txt", []byte("content1"))
	be.WriteFile(ctx, "file2.go", []byte("content2"))
	be.WriteFile(ctx, "dir1/file3.md", []byte("content3"))

	tool, ok := registry.Get("list_directory")
	if !ok {
		t.Fatal("list_directory tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path": ".",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to list directory: %v", err)
	}

	if !strings.Contains(result, "file1.txt") || !strings.Contains(result, "file2.go") || !strings.Contains(result, "dir1") {
		t.Errorf("Expected directory listing to contain files, got %q", result)
	}
}

func TestBuiltinTools_Glob_Simple(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files
	be.WriteFile(ctx, "file1.go", []byte("content"))
	be.WriteFile(ctx, "file2.go", []byte("content"))
	be.WriteFile(ctx, "file1.txt", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"pattern": "*.go",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to glob: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 .go files, got %d: %q", len(lines), result)
	}
}

func TestBuiltinTools_Glob_DoubleStar(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files in nested directories
	be.WriteFile(ctx, "file1.go", []byte("content"))
	be.WriteFile(ctx, "dir1/file2.go", []byte("content"))
	be.WriteFile(ctx, "dir1/subdir/file3.go", []byte("content"))
	be.WriteFile(ctx, "dir2/file4.go", []byte("content"))
	be.WriteFile(ctx, "dir1/file1.txt", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test **/*.go
	args, _ := json.Marshal(map[string]interface{}{
		"pattern": "**/*.go",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to glob: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	// Should find file1.go, dir1/file2.go, dir1/subdir/file3.go, dir2/file4.go
	if len(lines) != 4 {
		t.Errorf("Expected 4 .go files in nested directories, got %d: %q", len(lines), result)
	}
}

func TestBuiltinTools_Glob_DoubleStarInMiddle(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files
	be.WriteFile(ctx, "src/file1.ts", []byte("content"))
	be.WriteFile(ctx, "src/subdir/file2.ts", []byte("content"))
	be.WriteFile(ctx, "src/subdir/nested/file3.ts", []byte("content"))
	be.WriteFile(ctx, "src/subdir/file4.go", []byte("content"))
	be.WriteFile(ctx, "test/file5.ts", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test src/**/*.ts
	args, _ := json.Marshal(map[string]interface{}{
		"pattern": "src/**/*.ts",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to glob: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	// Should find src/file1.ts, src/subdir/file2.ts, src/subdir/nested/file3.ts
	// Should NOT find test/file5.ts or src/subdir/file4.go
	if len(lines) != 3 {
		t.Errorf("Expected 3 .ts files under src/, got %d: %q", len(lines), result)
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, "src/") || !strings.HasSuffix(line, ".ts") {
			t.Errorf("Expected file to start with src/ and end with .ts, got %q", line)
		}
	}
}

func TestBuiltinTools_Glob_QuestionMark(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files
	be.WriteFile(ctx, "file1.go", []byte("content"))
	be.WriteFile(ctx, "file2.go", []byte("content"))
	be.WriteFile(ctx, "file10.go", []byte("content"))
	be.WriteFile(ctx, "file.go", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test file?.go (should match file1.go and file2.go)
	args, _ := json.Marshal(map[string]interface{}{
		"pattern": "file?.go",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to glob: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 files matching file?.go, got %d: %q", len(lines), result)
	}
}

func TestBuiltinTools_Glob_DoubleStarAtEnd(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create test files
	be.WriteFile(ctx, "test/file1.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/file2.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/nested/file3.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/nested/README.md", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	agentCtx := AgentContext{
		Store: be,
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Test test/** (should match all files under test/)
	args, _ := json.Marshal(map[string]interface{}{
		"pattern": "test/**",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to glob: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	// Should find all files under test/
	if len(lines) != 4 {
		t.Errorf("Expected 4 files under test/**, got %d: %q", len(lines), result)
	}
}

func TestBuiltinTools_AddTodo(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	tool, ok := registry.Get("add_todo")
	if !ok {
		t.Fatal("add_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"task":        "Test task",
				"description": "Test description",
			},
		},
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to add todo: %v", err)
	}

	if !strings.Contains(result, "Added 1 todos") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify todo was added
	todos := todoManager.ListTodos()
	if len(todos) != 1 {
		t.Fatalf("Expected 1 todo, got %d", len(todos))
	}

	if todos[0].Task != "Test task" {
		t.Errorf("Expected task 'Test task', got %q", todos[0].Task)
	}
}

func TestBuiltinTools_AddTodoMultiple(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	tool, ok := registry.Get("add_todo")
	if !ok {
		t.Fatal("add_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"task":        "Task 1",
				"description": "Description 1",
			},
			map[string]interface{}{
				"task":        "Task 2",
				"description": "Description 2",
			},
			map[string]interface{}{
				"task":        "Task 3",
				"description": "Description 3",
			},
		},
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to add todos: %v", err)
	}

	if !strings.Contains(result, "Added 3 todos") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify todos were added
	todos := todoManager.ListTodos()
	if len(todos) != 3 {
		t.Fatalf("Expected 3 todos, got %d", len(todos))
	}

	// Verify each todo
	expectedTasks := []string{"Task 1", "Task 2", "Task 3"}
	for i, expectedTask := range expectedTasks {
		if todos[i].Task != expectedTask {
			t.Errorf("Expected task '%s', got %q", expectedTask, todos[i].Task)
		}
		if todos[i].Status != "pending" {
			t.Errorf("Expected status 'pending', got %q", todos[i].Status)
		}
	}
}

func TestBuiltinTools_UpdateTodo(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Add a todo first
	id, _ := todoManager.AddTodo("Test task", "Test description")

	tool, ok := registry.Get("update_todo")
	if !ok {
		t.Fatal("update_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"id":     id,
		"status": "completed",
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to update todo: %v", err)
	}

	if !strings.Contains(result, "Updated todo") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify todo was updated
	todo, ok := todoManager.GetTodo(id)
	if !ok {
		t.Fatal("Todo not found")
	}

	if todo.Status != "completed" {
		t.Errorf("Expected status 'completed', got %q", todo.Status)
	}
}

func TestBuiltinTools_ListTodos(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Add some todos
	todoManager.AddTodo("Task 1", "Description 1")
	todoManager.AddTodo("Task 2", "Description 2")

	tool, ok := registry.Get("list_todos")
	if !ok {
		t.Fatal("list_todos tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, "{}")
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}

	if !strings.Contains(result, "Task 1") || !strings.Contains(result, "Task 2") {
		t.Errorf("Expected todo list to contain tasks, got %q", result)
	}
}

func TestBuiltinTools_DeleteTodo(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Add a todo first
	id, _ := todoManager.AddTodo("Test task", "Test description")

	tool, ok := registry.Get("delete_todo")
	if !ok {
		t.Fatal("delete_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"ids": []interface{}{id},
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to delete todo: %v", err)
	}

	if !strings.Contains(result, "Deleted 1 todos") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify todo was deleted
	_, ok = todoManager.GetTodo(id)
	if ok {
		t.Error("Expected todo to be deleted, but it still exists")
	}

	if len(todoManager.ListTodos()) != 0 {
		t.Errorf("Expected 0 todos, got %d", len(todoManager.ListTodos()))
	}
}

func TestBuiltinTools_DeleteTodoMultiple(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Add multiple todos
	id1, _ := todoManager.AddTodo("Task 1", "Description 1")
	id2, _ := todoManager.AddTodo("Task 2", "Description 2")
	id3, _ := todoManager.AddTodo("Task 3", "Description 3")

	tool, ok := registry.Get("delete_todo")
	if !ok {
		t.Fatal("delete_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	// Delete two todos
	args, _ := json.Marshal(map[string]interface{}{
		"ids": []interface{}{id1, id3},
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to delete todos: %v", err)
	}

	if !strings.Contains(result, "Deleted 2 todos") {
		t.Errorf("Expected success message, got %q", result)
	}

	// Verify todos were deleted
	_, ok = todoManager.GetTodo(id1)
	if ok {
		t.Error("Expected todo 1 to be deleted, but it still exists")
	}

	_, ok = todoManager.GetTodo(id3)
	if ok {
		t.Error("Expected todo 3 to be deleted, but it still exists")
	}

	// Verify the remaining todo still exists
	todo, ok := todoManager.GetTodo(id2)
	if !ok {
		t.Error("Expected todo 2 to still exist")
	} else if todo.Task != "Task 2" {
		t.Errorf("Expected task 'Task 2', got %q", todo.Task)
	}

	// Verify count
	if len(todoManager.ListTodos()) != 1 {
		t.Errorf("Expected 1 todo, got %d", len(todoManager.ListTodos()))
	}
}

func TestBuiltinTools_DeleteTodo_NotFound(t *testing.T) {
	ctx := context.Background()
	todoManager := NewTodoManager()
	registry := BuiltinTools(WithEnableFileTool(true))

	tool, ok := registry.Get("delete_todo")
	if !ok {
		t.Fatal("delete_todo tool not found")
	}

	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: todoManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"ids": []interface{}{"nonexistent-id"},
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for nonexistent todo, got nil")
	}
}

func TestBuiltinTools_AllToolsRegistered(t *testing.T) {
	registry := BuiltinTools(WithEnableFileTool(true))

	expectedTools := []string{
		"read_file",
		"write_file",
		"edit_file",
		"list_directory",
		"glob",
		"add_artifact",
		"add_todo",
		"update_todo",
		"list_todos",
		"delete_todo",
	}

	for _, toolName := range expectedTools {
		if _, ok := registry.Get(toolName); !ok {
			t.Errorf("Expected tool %q to be registered", toolName)
		}
	}
}

func TestTypedToolFunc(t *testing.T) {
	// Define a typed argument struct
	type HelloArgs struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	// Create a typed tool
	tool := NewTypedToolFunc(
		"hello",
		"Say hello",
		map[string]*schema.ParameterInfo{
			"name":  StringParam("The name to greet", true),
			"count": IntParam("Number of times to greet", false),
		},
		func(ctx context.Context, args HelloArgs) (string, error) {
			// args is already typed, no need for casting
			result := ""
			for i := 0; i < args.Count; i++ {
				result += "Hello, " + args.Name + "!\n"
			}
			return result, nil
		},
	)

	// Test via ExecuteJson (SelfInvokeTool interface)
	ctx := context.Background()
	jsonInput := `{"name":"Alice","count":3}`
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, jsonInput)
	if err != nil {
		t.Fatalf("Failed to execute typed tool: %v", err)
	}

	expected := "Hello, Alice!\nHello, Alice!\nHello, Alice!\n"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestTypedCtxAwareToolFunc(t *testing.T) {
	// Define a typed argument struct
	type WriteFileArgs struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	// Create a typed context-aware tool
	tool := NewTypedCtxAwareToolFunc(
		"typed_write_file",
		"Write content to a file",
		map[string]*schema.ParameterInfo{
			"path":    StringParam("The absolute path to the file to write", true),
			"content": StringParam("The content to write to the file", true),
		},
		func(ctx context.Context, agentCtx AgentContext, args WriteFileArgs) (string, error) {
			// args is already typed, no need for casting
			err := agentCtx.Store.WriteFile(ctx, args.Path, []byte(args.Content))
			if err != nil {
				return "", err
			}
			return "Successfully wrote to " + args.Path, nil
		},
	)

	// Test via ExecuteJson with context containing AgentContext
	ctx := context.Background()
	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	jsonInput := `{"path":"test.txt","content":"Hello, typed tool!"}`
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, jsonInput)
	if err != nil {
		t.Fatalf("Failed to execute typed context-aware tool: %v", err)
	}

	expected := "Successfully wrote to test.txt"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestTypedTodoAwareToolFunc(t *testing.T) {
	// Define a typed argument struct
	type AddTodoArgs struct {
		Task        string `json:"task"`
		Description string `json:"description"`
	}

	// Create a typed context-aware tool for todo operations
	tool := NewTypedCtxAwareToolFunc(
		"typed_add_todo",
		"Add a todo item",
		map[string]*schema.ParameterInfo{
			"task":        StringParam("The task description", true),
			"description": StringParam("Additional details about the task", false),
		},
		func(ctx context.Context, agentCtx AgentContext, args AddTodoArgs) (string, error) {
			// args is already typed, no need for casting
			id, err := agentCtx.Todos.AddTodo(args.Task, args.Description)
			if err != nil {
				return "", err
			}
			return "Added todo: " + id, nil
		},
	)

	// Test via ExecuteJson with context containing AgentContext
	ctx := context.Background()
	agentCtx := AgentContext{
		Store: newTestMemFileStore(),
		Todos: NewTodoManager(),
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	jsonInput := `{"task":"Test task","description":"Test description"}`
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, jsonInput)
	if err != nil {
		t.Fatalf("Failed to execute typed context-aware tool: %v", err)
	}

	expectedPrefix := "Added todo: todo-"
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("Expected result to start with %q, got %q", expectedPrefix, result)
	}
}

func TestBuiltinTools_AddArtifact(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "/test/artifact.txt"
	testContent := "This is a test artifact file"
	if err := be.WriteFile(ctx, testPath, []byte(testContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test adding an artifact
	tool, ok := registry.Get("add_artifact")
	if !ok {
		t.Fatal("add_artifact tool not found")
	}

	artifactManager := NewArtifactManager()
	agentCtx := AgentContext{
		Store:     be,
		Todos:     NewTodoManager(),
		Artifacts: artifactManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path":     testPath,
		"metadata": map[string]string{"title": "Test Artifact", "source": "test"},
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to add artifact: %v", err)
	}

	// Verify the result message
	expectedPrefix := "Successfully registered artifact: " + testPath
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("Expected result to start with %q, got %q", expectedPrefix, result)
	}

	// Verify the artifact was added to the manager
	artifacts := artifactManager.ListArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(artifacts))
	}

	// Verify artifact details
	if artifacts[0].Path != testPath {
		t.Errorf("Expected path %q, got %q", testPath, artifacts[0].Path)
	}

	if artifacts[0].SizeInBytes != int64(len(testContent)) {
		t.Errorf("Expected size %d, got %d", len(testContent), artifacts[0].SizeInBytes)
	}

	if artifacts[0].Meta["title"] != "Test Artifact" {
		t.Errorf("Expected title 'Test Artifact', got %q", artifacts[0].Meta["title"])
	}
}

func TestBuiltinTools_AddArtifact_NoMetadata(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Create a test file
	testPath := "/test/artifact2.txt"
	testContent := "Another test artifact"
	if err := be.WriteFile(ctx, testPath, []byte(testContent)); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test adding an artifact without metadata
	tool, ok := registry.Get("add_artifact")
	if !ok {
		t.Fatal("add_artifact tool not found")
	}

	artifactManager := NewArtifactManager()
	agentCtx := AgentContext{
		Store:     be,
		Todos:     NewTodoManager(),
		Artifacts: artifactManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path": testPath,
	})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("Failed to add artifact: %v", err)
	}

	// Verify the result message
	expectedPrefix := "Successfully registered artifact: " + testPath
	if !strings.HasPrefix(result, expectedPrefix) {
		t.Errorf("Expected result to start with %q, got %q", expectedPrefix, result)
	}

	// Verify the artifact was added
	artifacts := artifactManager.ListArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("Expected 1 artifact, got %d", len(artifacts))
	}
}

func TestBuiltinTools_AddArtifact_FileNotFound(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Test adding an artifact for a non-existent file
	tool, ok := registry.Get("add_artifact")
	if !ok {
		t.Fatal("add_artifact tool not found")
	}

	artifactManager := NewArtifactManager()
	agentCtx := AgentContext{
		Store:     be,
		Todos:     NewTodoManager(),
		Artifacts: artifactManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path":     "/nonexistent/file.txt",
		"metadata": map[string]string{"title": "Non-existent"},
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}

	// Verify no artifact was added
	artifacts := artifactManager.ListArtifacts()
	if len(artifacts) != 0 {
		t.Errorf("Expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestBuiltinTools_AddArtifact_EmptyPath(t *testing.T) {
	ctx := context.Background()
	be := newTestMemFileStore()
	registry := BuiltinTools(WithEnableFileTool(true))

	// Test adding an artifact with empty path
	tool, ok := registry.Get("add_artifact")
	if !ok {
		t.Fatal("add_artifact tool not found")
	}

	artifactManager := NewArtifactManager()
	agentCtx := AgentContext{
		Store:     be,
		Todos:     NewTodoManager(),
		Artifacts: artifactManager,
	}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{
		"path": "",
	})
	_, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err == nil {
		t.Error("Expected error for empty path, got nil")
	}

	// Verify no artifact was added
	artifacts := artifactManager.ListArtifacts()
	if len(artifacts) != 0 {
		t.Errorf("Expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestAgentContext_Artifacts(t *testing.T) {
	// Create test file
	testPath := "/test/file.txt"
	testContent := "Test content"

	// Create AgentContext with ArtifactManager
	artifactManager := NewArtifactManager()
	agentCtx := AgentContext{
		Artifacts: artifactManager,
	}

	// Add an artifact via ArtifactManager
	artifact := Artifact{
		Path:        testPath,
		SizeInBytes: int64(len(testContent)),
		Meta:        map[string]string{"title": "Test"},
	}
	err := agentCtx.Artifacts.AddArtifact(artifact)
	if err != nil {
		t.Fatalf("Failed to add artifact: %v", err)
	}

	// Verify artifact is accessible
	artifacts := agentCtx.Artifacts.ListArtifacts()
	if len(artifacts) != 1 {
		t.Errorf("Expected 1 artifact, got %d", len(artifacts))
	}

	if artifacts[0].Path != testPath {
		t.Errorf("Expected path %q, got %q", testPath, artifacts[0].Path)
	}
}

func TestTaskOutput(t *testing.T) {
	// Create a TaskOutput with artifacts
	artifacts := []Artifact{
		{Path: "/test/file1.txt", SizeInBytes: 100, Meta: map[string]string{"title": "File 1"}},
		{Path: "/test/file2.txt", SizeInBytes: 200, Meta: map[string]string{"title": "File 2"}},
	}

	output := TaskOutput{
		Response:  "Task completed successfully",
		Artifacts: artifacts,
	}

	// Verify Response
	if output.Response != "Task completed successfully" {
		t.Errorf("Expected response 'Task completed successfully', got %q", output.Response)
	}

	// Verify Artifacts
	if len(output.Artifacts) != 2 {
		t.Errorf("Expected 2 artifacts, got %d", len(output.Artifacts))
	}

	if output.Artifacts[0].Path != "/test/file1.txt" {
		t.Errorf("Expected path '/test/file1.txt', got %q", output.Artifacts[0].Path)
	}

	if output.Artifacts[1].SizeInBytes != 200 {
		t.Errorf("Expected size 200, got %d", output.Artifacts[1].SizeInBytes)
	}
}

func TestAgentRequest(t *testing.T) {
	// Create an AgentRequest with callback
	callbackCalled := false
	var callbackStore FileStore
	var callbackArtifacts []Artifact

	req := AgentRequest{
		UserInput: "Test input",
		ArtifactCallback: func(store FileStore, artifacts []Artifact) error {
			callbackCalled = true
			callbackStore = store
			callbackArtifacts = artifacts
			return nil
		},
	}

	// Verify UserInput
	if req.UserInput != "Test input" {
		t.Errorf("Expected UserInput 'Test input', got %q", req.UserInput)
	}

	// Verify ArtifactCallback is set
	if req.ArtifactCallback == nil {
		t.Error("Expected ArtifactCallback to be set, got nil")
	}

	// Test calling the callback
	if err := req.ArtifactCallback(NewTmpFileStore(), []Artifact{{Path: "/test.txt", SizeInBytes: 100}}); err != nil {
		t.Fatalf("Failed to call ArtifactCallback: %v", err)
	}

	if !callbackCalled {
		t.Error("Expected callback to be called")
	}

	if callbackStore == nil {
		t.Error("Expected callbackStore to be set, got nil")
	}

	if len(callbackArtifacts) != 1 {
		t.Errorf("Expected 1 artifact in callback, got %d", len(callbackArtifacts))
	}
}

func TestAgentRequest_PreloadBackendFiles(t *testing.T) {
	t.Run("callback is invoked with the backend store", func(t *testing.T) {
		var receivedStore FileStore
		called := false

		req := AgentRequest{
			UserInput: "test",
			PreloadBackendFiles: func(store FileStore) error {
				called = true
				receivedStore = store
				return nil
			},
		}

		store := NewTmpFileStore()
		if err := req.PreloadBackendFiles(store); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Error("expected PreloadBackendFiles to be called")
		}
		if receivedStore != store {
			t.Error("expected the store passed to callback to match the provided store")
		}
	})

	t.Run("nil callback does not panic", func(t *testing.T) {
		req := AgentRequest{
			UserInput:           "test",
			PreloadBackendFiles: nil,
		}
		// Should not panic
		if req.PreloadBackendFiles != nil {
			t.Error("expected PreloadBackendFiles to be nil")
		}
	})

	t.Run("callback error is propagated", func(t *testing.T) {
		expectedErr := errors.New("preload failed")
		req := AgentRequest{
			UserInput: "test",
			PreloadBackendFiles: func(store FileStore) error {
				return expectedErr
			},
		}

		err := req.PreloadBackendFiles(NewTmpFileStore())
		if err == nil {
			t.Error("expected error to be returned")
		}
	})
}

func TestBuiltinTools_FileToolDisabled(t *testing.T) {
	// When EnableFileTool is not set, file tools must not be registered.
	registry := BuiltinTools()

	fileTools := []string{
		"read_file",
		"write_file",
		"edit_file",
		"list_directory",
		"glob",
		"add_artifact",
	}
	for _, name := range fileTools {
		if _, ok := registry.Get(name); ok {
			t.Errorf("Expected file tool %q to be absent when EnableFileTool is false, but it was registered", name)
		}
	}

	// Todo tools must still be registered regardless.
	todoTools := []string{"add_todo", "update_todo", "list_todos", "delete_todo"}
	for _, name := range todoTools {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("Expected todo tool %q to be registered even when EnableFileTool is false", name)
		}
	}
}
