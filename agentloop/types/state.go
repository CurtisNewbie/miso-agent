package types

// TodoItem represents a task in the todo list.
type TodoItem struct {
	ID          string `json:"id"`
	Task        string `json:"task"`
	Status      string `json:"status"`             // "pending", "in_progress", "completed", "failed"
	Priority    string `json:"priority,omitempty"` // "high", "medium", "low"
	Description string `json:"description,omitempty"`
}
