package tools

import "context"

// Tool represents a tool that can be used by the agent.
type Tool interface {
	// Name returns the name of the tool.
	Name() string

	// Description returns a description of the tool.
	Description() string

	// Parameters returns the JSON schema for the tool parameters.
	Parameters() map[string]*ParameterInfo

	// Execute executes the tool with the given arguments.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ParameterInfo represents tool parameter information (simplified version).
type ParameterInfo struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolFunc is a function-based tool implementation.
type ToolFunc struct {
	name        string
	description string
	parameters  map[string]*ParameterInfo
	execute     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// NewToolFunc creates a new function-based tool.
func NewToolFunc(
	name string,
	description string,
	parameters map[string]interface{},
	execute func(ctx context.Context, args map[string]interface{}) (string, error),
) Tool {
	// Convert map[string]interface{} to map[string]*ParameterInfo
	paramInfo := make(map[string]*ParameterInfo)
	for key, val := range parameters {
		if paramMap, ok := val.(map[string]interface{}); ok {
			paramInfo[key] = &ParameterInfo{
				Type:        getString(paramMap, "type"),
				Description: getString(paramMap, "description"),
			}
		}
	}

	return &ToolFunc{
		name:        name,
		description: description,
		parameters:  paramInfo,
		execute:     execute,
	}
}

// getString safely gets a string value from a map.
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// Name returns the name of the tool.
func (t *ToolFunc) Name() string {
	return t.name
}

// Description returns the description of the tool.
func (t *ToolFunc) Description() string {
	return t.description
}

// Parameters returns the JSON schema for the tool parameters.
func (t *ToolFunc) Parameters() map[string]*ParameterInfo {
	return t.parameters
}

// Execute executes the tool with the given arguments.
func (t *ToolFunc) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.execute(ctx, args)
}

// TodoItem represents a task in the todo list.
type TodoItem struct {
	ID          string `json:"id"`
	Task        string `json:"task"`
	Status      string `json:"status"`             // "pending", "in_progress", "completed", "failed"
	Priority    string `json:"priority,omitempty"` // "high", "medium", "low"
	Description string `json:"description,omitempty"`
}
