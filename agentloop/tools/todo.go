package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/curtisnewbie/miso/errs"
)

// TodoManager manages the todo list for the agent.
type TodoManager struct {
	mu     sync.RWMutex
	todos  []TodoItem
	nextID int
}

// NewTodoManager creates a new todo manager.
func NewTodoManager() *TodoManager {
	return &TodoManager{
		todos:  make([]TodoItem, 0),
		nextID: 1,
	}
}

// AddTodo adds a new todo item.
func (tm *TodoManager) AddTodo(task, priority, description string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if task == "" {
		return "", errs.NewErrf("task cannot be empty")
	}

	if priority == "" {
		priority = "medium"
	}

	id := fmt.Sprintf("todo-%d", tm.nextID)
	tm.nextID++

	todo := TodoItem{
		ID:          id,
		Task:        task,
		Status:      "pending",
		Priority:    priority,
		Description: description,
	}

	tm.todos = append(tm.todos, todo)
	return id, nil
}

// UpdateTodoStatus updates the status of a todo item.
func (tm *TodoManager) UpdateTodoStatus(id, status string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i, todo := range tm.todos {
		if todo.ID == id {
			tm.todos[i].Status = status
			return nil
		}
	}

	return errs.NewErrf("todo %s not found", id)
}

// ListTodos returns all todo items.
func (tm *TodoManager) ListTodos() []TodoItem {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]TodoItem, len(tm.todos))
	copy(result, tm.todos)
	return result
}

// GetTodo returns a specific todo item.
func (tm *TodoManager) GetTodo(id string) (TodoItem, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, todo := range tm.todos {
		if todo.ID == id {
			return todo, true
		}
	}

	return TodoItem{}, false
}

// DeleteTodo deletes a todo item.
func (tm *TodoManager) DeleteTodo(id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i, todo := range tm.todos {
		if todo.ID == id {
			tm.todos = append(tm.todos[:i], tm.todos[i+1:]...)
			return nil
		}
	}

	return errs.NewErrf("todo %s not found", id)
}

// ClearCompleted removes all completed todos.
func (tm *TodoManager) ClearCompleted() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var remaining []TodoItem
	for _, todo := range tm.todos {
		if todo.Status != "completed" {
			remaining = append(remaining, todo)
		}
	}
	tm.todos = remaining
}

// Format returns a formatted string representation of all todos.
func (tm *TodoManager) Format() string {
	todos := tm.ListTodos()
	if len(todos) == 0 {
		return "No todos"
	}

	var sb strings.Builder
	sb.WriteString("Todo List:\n")

	for _, todo := range todos {
		priorityIcon := "○"
		switch todo.Priority {
		case "high":
			priorityIcon = "🔴"
		case "medium":
			priorityIcon = "🟡"
		case "low":
			priorityIcon = "🟢"
		}

		statusIcon := "⬜"
		switch todo.Status {
		case "in_progress":
			statusIcon = "🔄"
		case "completed":
			statusIcon = "✅"
		case "failed":
			statusIcon = "❌"
		}

		sb.WriteString(fmt.Sprintf("%s %s [%s] %s", statusIcon, priorityIcon, todo.ID, todo.Task))
		if todo.Description != "" {
			sb.WriteString(fmt.Sprintf(" - %s", todo.Description))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ToState returns the todos as a slice for state persistence.
func (tm *TodoManager) ToState() []TodoItem {
	return tm.ListTodos()
}

// FromState restores the todos from state.
func (tm *TodoManager) FromState(todos []TodoItem) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.todos = todos
	// Update nextID to avoid conflicts
	for _, todo := range todos {
		if strings.HasPrefix(todo.ID, "todo-") {
			var id int
			fmt.Sscanf(todo.ID, "todo-%d", &id)
			if id >= tm.nextID {
				tm.nextID = id + 1
			}
		}
	}
}

// TodoTools returns the todo tools.
func TodoTools(todoManager *TodoManager) *Registry {
	registry := NewRegistry()

	registry.Register(NewToolFunc(
		"add_todo",
		"Add a new todo item to the list.",
		map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task description",
			},
			"priority": map[string]interface{}{
				"type":        "string",
				"description": "Priority level: high, medium, or low (default: medium)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Additional details about the task",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			task, _ := args["task"].(string)
			priority, _ := args["priority"].(string)
			description, _ := args["description"].(string)

			id, err := todoManager.AddTodo(task, priority, description)
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("Added todo %s", id), nil
		},
	))

	registry.Register(NewToolFunc(
		"update_todo",
		"Update the status of a todo item.",
		map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "The todo item ID",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "New status: pending, in_progress, completed, or failed",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			id, _ := args["id"].(string)
			status, _ := args["status"].(string)

			if err := todoManager.UpdateTodoStatus(id, status); err != nil {
				return "", err
			}

			return fmt.Sprintf("Updated todo %s to %s", id, status), nil
		},
	))

	registry.Register(NewToolFunc(
		"list_todos",
		"List all todo items.",
		map[string]interface{}{},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return todoManager.Format(), nil
		},
	))

	registry.Register(NewToolFunc(
		"delete_todo",
		"Delete a todo item.",
		map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "The todo item ID",
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			id, _ := args["id"].(string)

			if err := todoManager.DeleteTodo(id); err != nil {
				return "", err
			}

			return fmt.Sprintf("Deleted todo %s", id), nil
		},
	))

	return registry
}
