package agentloop

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Tool represents a tool that can be used by the agent.
type Tool interface {
	// Name returns the name of the tool.
	Name() string

	// Description returns a description of the tool.
	Description() string

	// Parameters returns the JSON schema for the tool parameters.
	Parameters() map[string]*schema.ParameterInfo

	// Execute executes the tool with the given arguments.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolFunc is a function-based tool implementation.
type ToolFunc struct {
	name        string
	description string
	parameters  map[string]*schema.ParameterInfo
	execute     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// NewToolFunc creates a new function-based tool.
//
// Parameters should be built using the typed helper functions:
//   - [StringParam](desc, required) - for string parameters
//   - [IntParam](desc, required) - for integer parameters
//   - [NumberParam](desc, required) - for numeric parameters
//   - [BoolParam](desc, required) - for boolean parameters
//   - [ArrayParam](desc, elemInfo, required) - for array parameters
//   - [ObjectParam](desc, subParams, required) - for object parameters
//
// Example:
//
//	NewToolFunc(
//	    "finish_tool",
//	    "Call this tool when you have completed the task",
//	    map[string]*schema.ParameterInfo{
//	        "response": StringParam("Your final answer to the task", false),
//	    },
//	    func(ctx context.Context, args map[string]interface{}) (string, error) {
//	        response := cast.ToString(args["response"])
//	        return response, nil
//	    },
//	)
func NewToolFunc(
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, args map[string]interface{}) (string, error),
) Tool {
	return &ToolFunc{
		name:        name,
		description: description,
		parameters:  parameters,
		execute:     execute,
	}
}

// newAwareToolFunc creates a tool that automatically extracts a dependency from args.
// This is a generic helper for creating tools that need access to stateful components
// like FileStore or TodoManager, which are injected via context and args.
func newAwareToolFunc[T any](
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	key string,
	execute func(ctx context.Context, deps T, args map[string]interface{}) (string, error),
) Tool {
	return NewToolFunc(name, description, parameters,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var deps T
			if v, ok := args[key]; ok {
				if vv, ok := v.(T); ok {
					deps = vv
				}
			}
			return execute(ctx, deps, args)
		})
}

// NewStoreAwareToolFunc creates a tool that needs FileStore access.
// The FileStore is automatically injected via context by the agent loop.
//
// Parameters should be built using the typed helper functions:
//   - [StringParam](desc, required) - for string parameters
//   - [IntParam](desc, required) - for integer parameters
//   - [NumberParam](desc, required) - for numeric parameters
//   - [BoolParam](desc, required) - for boolean parameters
//   - [ArrayParam](desc, elemInfo, required) - for array parameters
//   - [ObjectParam](desc, subParams, required) - for object parameters
//
// Example:
//
//	NewStoreAwareToolFunc(
//	    "read_file",
//	    "Read file content",
//	    map[string]*schema.ParameterInfo{
//	        "path":   StringParam("The absolute path to the file to read", true),
//	        "offset": NumberParam("Optional: Line number to start reading from", false),
//	    },
//	    func(ctx context.Context, store FileStore, args map[string]interface{}) (string, error) {
//	        path := cast.ToString(args["path"])
//	        // ... use store to read file
//	    },
//	)
func NewStoreAwareToolFunc(
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, store FileStore, args map[string]interface{}) (string, error),
) Tool {
	return newAwareToolFunc(name, description, parameters, ArgKeyAgentLoopFileStore, execute)
}

// NewTodoAwareToolFunc creates a tool that needs TodoManager access.
// The TodoManager is automatically injected via context by the agent loop.
//
// Parameters should be built using the typed helper functions:
//   - [StringParam](desc, required) - for string parameters
//   - [IntParam](desc, required) - for integer parameters
//   - [NumberParam](desc, required) - for numeric parameters
//   - [BoolParam](desc, required) - for boolean parameters
//   - [ArrayParam](desc, elemInfo, required) - for array parameters
//   - [ObjectParam](desc, subParams, required) - for object parameters
//
// Example:
//
//	NewTodoAwareToolFunc(
//	    "add_todo",
//	    "Add a todo item",
//	    map[string]*schema.ParameterInfo{
//	        "task": StringParam("The task description", true),
//	    },
//	    func(ctx context.Context, tm *TodoManager, args map[string]interface{}) (string, error) {
//	        task := cast.ToString(args["task"])
//	        // ... use tm to add todo
//	    },
//	)
func NewTodoAwareToolFunc(
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, store *TodoManager, args map[string]interface{}) (string, error),
) Tool {
	return newAwareToolFunc(name, description, parameters, ArgKeyAgentLoopTodoManager, execute)
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
func (t *ToolFunc) Parameters() map[string]*schema.ParameterInfo {
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
	Status      string `json:"status"` // "pending", "completed"
	Description string `json:"description,omitempty"`
}

// Parameter helpers for building type-safe tool schemas

// StringParam creates a string parameter.
func StringParam(desc string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.String,
		Desc:     desc,
		Required: required,
	}
}

// StringParamEnum creates a string parameter with enum values.
func StringParamEnum(desc string, enumValues []string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.String,
		Desc:     desc,
		Enum:     enumValues,
		Required: required,
	}
}

// IntParam creates an integer parameter.
func IntParam(desc string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.Integer,
		Desc:     desc,
		Required: required,
	}
}

// NumberParam creates a number parameter.
func NumberParam(desc string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.Number,
		Desc:     desc,
		Required: required,
	}
}

// BoolParam creates a boolean parameter.
func BoolParam(desc string, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.Boolean,
		Desc:     desc,
		Required: required,
	}
}

// ArrayParam creates an array parameter with element info.
func ArrayParam(desc string, elemInfo *schema.ParameterInfo, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:     schema.Array,
		Desc:     desc,
		ElemInfo: elemInfo,
		Required: required,
	}
}

// ObjectParam creates an object parameter with sub-parameters.
func ObjectParam(desc string, subParams map[string]*schema.ParameterInfo, required bool) *schema.ParameterInfo {
	return &schema.ParameterInfo{
		Type:      schema.Object,
		Desc:      desc,
		SubParams: subParams,
		Required:  required,
	}
}
