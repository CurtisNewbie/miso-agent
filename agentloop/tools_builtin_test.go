package agentloop

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltinTools_ReadFile(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	result, err := tool.Execute(ctx, map[string]interface{}{
		"path": testPath,
	})
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if result != testContent {
		t.Errorf("Expected content %q, got %q", testContent, result)
	}
}

func TestBuiltinTools_ReadFile_NotFound(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	tool, ok := registry.Get("read_file")
	if !ok {
		t.Fatal("read_file tool not found")
	}

	_, err := tool.Execute(ctx, map[string]interface{}{
		"path": "nonexistent.txt",
	})
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestBuiltinTools_WriteFile(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	testPath := "write_test.txt"
	testContent := "Written content"

	tool, ok := registry.Get("write_file")
	if !ok {
		t.Fatal("write_file tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"path":    testPath,
		"content": testContent,
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test single replacement
	result, err := tool.Execute(ctx, map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hi",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test replace all
	result, err := tool.Execute(ctx, map[string]interface{}{
		"path":        testPath,
		"old_string":  "Hello",
		"new_string":  "Hi",
		"replace_all": true,
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test string not found
	_, err := tool.Execute(ctx, map[string]interface{}{
		"path":       testPath,
		"old_string": "Goodbye",
		"new_string": "Hi",
	})
	if err == nil {
		t.Error("Expected error for string not found, got nil")
	}

	if !strings.Contains(err.Error(), "String not found") {
		t.Errorf("Expected 'String not found' error, got %v", err)
	}
}

func TestBuiltinTools_EditFile_MultipleOccurrencesNoReplaceAll(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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
	_, err := tool.Execute(ctx, map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hi",
	})
	if err == nil {
		t.Error("Expected error for multiple occurrences without replace_all, got nil")
	}

	if !strings.Contains(err.Error(), "appears 3 times") {
		t.Errorf("Expected error about multiple occurrences, got %v", err)
	}
}

func TestBuiltinTools_EditFile_SameStrings(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test same old_string and new_string
	_, err := tool.Execute(ctx, map[string]interface{}{
		"path":       testPath,
		"old_string": "Hello",
		"new_string": "Hello",
	})
	if err == nil {
		t.Error("Expected error for same old_string and new_string, got nil")
	}

	if !strings.Contains(err.Error(), "must be different") {
		t.Errorf("Expected error about different strings, got %v", err)
	}
}

func TestBuiltinTools_EditFile_FileNotFound(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	tool, ok := registry.Get("edit_file")
	if !ok {
		t.Fatal("edit_file tool not found")
	}

	// Test editing nonexistent file
	_, err := tool.Execute(ctx, map[string]interface{}{
		"path":       "nonexistent.txt",
		"old_string": "Hello",
		"new_string": "Hi",
	})
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

	result, err := tool.Execute(ctx, map[string]interface{}{
		"reflection": "I have gathered information about the topic. I need to search for more specific details.",
	})
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

	_, err := tool.Execute(ctx, map[string]interface{}{})
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

	result, err := tool.Execute(ctx, map[string]interface{}{
		"reflection": "",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Create test files and directories
	be.WriteFile(ctx, "file1.txt", []byte("content1"))
	be.WriteFile(ctx, "file2.go", []byte("content2"))
	be.WriteFile(ctx, "dir1/file3.md", []byte("content3"))

	tool, ok := registry.Get("list_directory")
	if !ok {
		t.Fatal("list_directory tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"path": ".",
	})
	if err != nil {
		t.Fatalf("Failed to list directory: %v", err)
	}

	if !strings.Contains(result, "file1.txt") || !strings.Contains(result, "file2.go") || !strings.Contains(result, "dir1") {
		t.Errorf("Expected directory listing to contain files, got %q", result)
	}
}

func TestBuiltinTools_Glob_Simple(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Create test files
	be.WriteFile(ctx, "file1.go", []byte("content"))
	be.WriteFile(ctx, "file2.go", []byte("content"))
	be.WriteFile(ctx, "file1.txt", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"pattern": "*.go",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test **/*.go
	result, err := tool.Execute(ctx, map[string]interface{}{
		"pattern": "**/*.go",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

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

	// Test src/**/*.ts
	result, err := tool.Execute(ctx, map[string]interface{}{
		"pattern": "src/**/*.ts",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Create test files
	be.WriteFile(ctx, "file1.go", []byte("content"))
	be.WriteFile(ctx, "file2.go", []byte("content"))
	be.WriteFile(ctx, "file10.go", []byte("content"))
	be.WriteFile(ctx, "file.go", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	// Test file?.go (should match file1.go and file2.go)
	result, err := tool.Execute(ctx, map[string]interface{}{
		"pattern": "file?.go",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Create test files
	be.WriteFile(ctx, "test/file1.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/file2.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/nested/file3.go", []byte("content"))
	be.WriteFile(ctx, "test/subdir/nested/README.md", []byte("content"))

	tool, ok := registry.Get("glob")
	if !ok {
		t.Fatal("glob tool not found")
	}

	// Test test/** (should match all files under test/)
	result, err := tool.Execute(ctx, map[string]interface{}{
		"pattern": "test/**",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	tool, ok := registry.Get("add_todo")
	if !ok {
		t.Fatal("add_todo tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"task":        "Test task",
		"description": "Test description",
	})
	if err != nil {
		t.Fatalf("Failed to add todo: %v", err)
	}

	if !strings.Contains(result, "Added todo") {
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

func TestBuiltinTools_UpdateTodo(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Add a todo first
	id, _ := todoManager.AddTodo("Test task", "Test description")

	tool, ok := registry.Get("update_todo")
	if !ok {
		t.Fatal("update_todo tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"id":     id,
		"status": "completed",
	})
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
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Add some todos
	todoManager.AddTodo("Task 1", "Description 1")
	todoManager.AddTodo("Task 2", "Description 2")

	tool, ok := registry.Get("list_todos")
	if !ok {
		t.Fatal("list_todos tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Failed to list todos: %v", err)
	}

	if !strings.Contains(result, "Task 1") || !strings.Contains(result, "Task 2") {
		t.Errorf("Expected todo list to contain tasks, got %q", result)
	}
}

func TestBuiltinTools_DeleteTodo(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	// Add a todo first
	id, _ := todoManager.AddTodo("Test task", "Test description")

	tool, ok := registry.Get("delete_todo")
	if !ok {
		t.Fatal("delete_todo tool not found")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"id": id,
	})
	if err != nil {
		t.Fatalf("Failed to delete todo: %v", err)
	}

	if !strings.Contains(result, "Deleted todo") {
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

func TestBuiltinTools_DeleteTodo_NotFound(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	tool, ok := registry.Get("delete_todo")
	if !ok {
		t.Fatal("delete_todo tool not found")
	}

	_, err := tool.Execute(ctx, map[string]interface{}{
		"id": "nonexistent-id",
	})
	if err == nil {
		t.Error("Expected error for nonexistent todo, got nil")
	}
}

func TestBuiltinTools_AllToolsRegistered(t *testing.T) {
	be := NewMemFileStore()
	todoManager := NewTodoManager()
	registry := BuiltinTools(be, todoManager, false)

	expectedTools := []string{
		"read_file",
		"write_file",
		"edit_file",
		"list_directory",
		"glob",
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

func TestBuiltinTools_FinishTool_Enabled(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()

	// Test with finish tool enabled
	registry := BuiltinTools(be, todoManager, true)

	tool, ok := registry.Get("finish_tool")
	if !ok {
		t.Fatal("finish_tool not found when enabled")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{
		"response": "Task completed successfully",
	})
	if err != nil {
		t.Fatalf("Failed to execute finish tool: %v", err)
	}

	expected := "Task completed successfully"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestBuiltinTools_FinishTool_Disabled(t *testing.T) {
	be := NewMemFileStore()
	todoManager := NewTodoManager()

	// Test with finish tool disabled
	registry := BuiltinTools(be, todoManager, false)

	_, ok := registry.Get("finish_tool")
	if ok {
		t.Error("finish_tool should not be registered when disabled")
	}
}

func TestBuiltinTools_FinishTool_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	be := NewMemFileStore()
	todoManager := NewTodoManager()

	registry := BuiltinTools(be, todoManager, true)

	tool, ok := registry.Get("finish_tool")
	if !ok {
		t.Fatal("finish_tool not found when enabled")
	}

	result, err := tool.Execute(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Failed to execute finish tool with empty response: %v", err)
	}

	expected := "Task completed"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}
