package agentloop

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/util/llm"
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

type SelfInvokeTool interface {
	ExecuteJson(ctx context.Context, jsonArg string) (string, error)
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

// NewCtxAwareToolFunc creates a tool that needs access to AgentContext (Store and Todos).
// The AgentContext is automatically injected via context by the agent loop.
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
//	NewCtxAwareToolFunc(
//	    "read_file",
//	    "Read file content",
//	    map[string]*schema.ParameterInfo{
//	        "path": StringParam("The absolute path to the file to read", true),
//	    },
//	    func(ctx context.Context, agentCtx AgentContext, args map[string]interface{}) (string, error) {
//	        path := cast.ToString(args["path"])
//	        content, err := agentCtx.Store.ReadFile(ctx, path)
//	        return string(content), err
//	    },
//	)
func NewCtxAwareToolFunc(
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, agentCtx AgentContext, args map[string]interface{}) (string, error),
) Tool {
	return NewToolFunc(name, description, parameters,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			var agentCtx AgentContext
			if v := ctx.Value(agentCtxKey); v != nil {
				if ac, ok := v.(AgentContext); ok {
					agentCtx = ac
				}
			}
			return execute(ctx, agentCtx, args)
		})
}

// TypedToolFunc is a tool that accepts typed arguments via JSON deserialization.
type TypedToolFunc[T any] struct {
	name        string
	description string
	parameters  map[string]*schema.ParameterInfo
	execute     func(ctx context.Context, args T) (string, error)
}

// Name returns the name of the tool.
func (t *TypedToolFunc[T]) Name() string {
	return t.name
}

// Description returns the description of the tool.
func (t *TypedToolFunc[T]) Description() string {
	return t.description
}

// Parameters returns the JSON schema for the tool parameters.
func (t *TypedToolFunc[T]) Parameters() map[string]*schema.ParameterInfo {
	return t.parameters
}

// Execute executes the tool with the given arguments (untyped).
func (t *TypedToolFunc[T]) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", nil // This shouldn't be called since we implement SelfInvokeTool
}

// ExecuteJson executes the tool with JSON arguments.
func (t *TypedToolFunc[T]) ExecuteJson(ctx context.Context, jsonArg string) (string, error) {
	var args T
	if jsonArg != "" {
		parsedArgs, err := llm.ParseLLMJsonAs[T](jsonArg)
		if err != nil {
			return "", err
		}
		args = parsedArgs
	}
	return t.execute(ctx, args)
}

// NewTypedToolFunc creates a tool that accepts typed arguments via JSON deserialization.
// The execute function receives a struct of type T instead of a map.
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
//	type ReadFileArgs struct {
//	    Path   string `json:"path"`
//	    Offset int    `json:"offset"`
//	    Limit  int    `json:"limit"`
//	}
//
//	NewTypedToolFunc(
//	    "read_file",
//	    "Read file content",
//	    map[string]*schema.ParameterInfo{
//	        "path":   StringParam("The absolute path to the file to read", true),
//	        "offset": IntParam("Optional: Line number to start reading from", false),
//	        "limit":  IntParam("Optional: Maximum number of lines to read", false),
//	    },
//	    func(ctx context.Context, args ReadFileArgs) (string, error) {
//	        // args is already typed as ReadFileArgs
//	        // No need for cast.ToString or cast.ToInt
//	        return "file content", nil
//	    },
//	)
func NewTypedToolFunc[T any](
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, args T) (string, error),
) Tool {
	return &TypedToolFunc[T]{
		name:        name,
		description: description,
		parameters:  parameters,
		execute:     execute,
	}
}

// TypedCtxAwareToolFunc is a tool that accepts typed arguments and has AgentContext access.
type TypedCtxAwareToolFunc[T any] struct {
	name        string
	description string
	parameters  map[string]*schema.ParameterInfo
	execute     func(ctx context.Context, agentCtx AgentContext, args T) (string, error)
}

// Name returns the name of the tool.
func (t *TypedCtxAwareToolFunc[T]) Name() string {
	return t.name
}

// Description returns the description of the tool.
func (t *TypedCtxAwareToolFunc[T]) Description() string {
	return t.description
}

// Parameters returns the JSON schema for the tool parameters.
func (t *TypedCtxAwareToolFunc[T]) Parameters() map[string]*schema.ParameterInfo {
	return t.parameters
}

// Execute executes the tool with the given arguments (untyped).
func (t *TypedCtxAwareToolFunc[T]) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return "", nil // This shouldn't be called since we implement SelfInvokeTool
}

// ExecuteJson executes the tool with JSON arguments.
func (t *TypedCtxAwareToolFunc[T]) ExecuteJson(ctx context.Context, jsonArg string) (string, error) {
	var args T
	if jsonArg != "" {
		parsedArgs, err := llm.ParseLLMJsonAs[T](jsonArg)
		if err != nil {
			return "", err
		}
		args = parsedArgs
	}

	// Get AgentContext from context
	var agentCtx AgentContext
	if v := ctx.Value(agentCtxKey); v != nil {
		if ac, ok := v.(AgentContext); ok {
			agentCtx = ac
		}
	}

	return t.execute(ctx, agentCtx, args)
}

// NewTypedCtxAwareToolFunc creates a tool that accepts typed arguments and has AgentContext access.
// The execute function receives a struct of type T instead of a map.
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
//	type ReadFileArgs struct {
//	    Path   string `json:"path"`
//	    Offset int    `json:"offset"`
//	    Limit  int    `json:"limit"`
//	}
//
//	NewTypedCtxAwareToolFunc(
//	    "read_file",
//	    "Read file content",
//	    map[string]*schema.ParameterInfo{
//	        "path":   StringParam("The absolute path to the file to read", true),
//	        "offset": IntParam("Optional: Line number to start reading from", false),
//	        "limit":  IntParam("Optional: Maximum number of lines to read", false),
//	    },
//	    func(ctx context.Context, agentCtx AgentContext, args ReadFileArgs) (string, error) {
//	        // args is already typed as ReadFileArgs
//	        // No need for cast.ToString or cast.ToInt
//	        content, err := agentCtx.Store.ReadFile(ctx, args.Path)
//	        return string(content), err
//	    },
//	)
func NewTypedCtxAwareToolFunc[T any](
	name string,
	description string,
	parameters map[string]*schema.ParameterInfo,
	execute func(ctx context.Context, agentCtx AgentContext, args T) (string, error),
) Tool {
	return &TypedCtxAwareToolFunc[T]{
		name:        name,
		description: description,
		parameters:  parameters,
		execute:     execute,
	}
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
