package agentloop

import (
	"fmt"
	"strings"
	"sync"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/hash"
	"github.com/curtisnewbie/miso/util/slutil"
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
func (tm *TodoManager) AddTodo(task, description string) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if task == "" {
		return "", errs.NewErrf("task cannot be empty")
	}

	id := fmt.Sprintf("todo-%d", tm.nextID)
	tm.nextID++

	todo := TodoItem{
		ID:          id,
		Task:        task,
		Status:      "pending",
		Description: description,
	}

	tm.todos = append(tm.todos, todo)
	return id, nil
}

// AddTodos adds multiple todo items atomically.
func (tm *TodoManager) AddTodos(todos []TodoItem) ([]string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(todos) == 0 {
		return nil, errs.NewErrf("todos list cannot be empty")
	}

	ids := make([]string, 0, len(todos))
	for _, td := range todos {
		if td.Task == "" {
			return nil, errs.NewErrf("task cannot be empty")
		}

		id := fmt.Sprintf("todo-%d", tm.nextID)
		tm.nextID++

		todo := TodoItem{
			ID:          id,
			Task:        td.Task,
			Status:      "pending",
			Description: td.Description,
		}

		tm.todos = append(tm.todos, todo)
		ids = append(ids, id)
	}

	return ids, nil
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

// DeleteTodos deletes multiple todo items atomically.
func (tm *TodoManager) DeleteTodos(ids []string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(ids) == 0 {
		return errs.NewErrf("ids list cannot be empty")
	}

	// Create a set of IDs to delete for O(1) lookup
	idSet := hash.NewSet(ids...)

	// Filter out todos that are in the deletion set
	var remaining []TodoItem = slutil.Filter(tm.todos,
		func(ti TodoItem) (incl bool) { return !idSet.Has(ti.ID) })

	tm.todos = remaining
	return nil
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
		status := "[ ]"
		switch strings.ToUpper(todo.Status) {
		case "COMPLETED":
			status = "[x]"
		}

		sb.WriteString(fmt.Sprintf("%s [%s] %s", status, todo.ID, todo.Task))
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
