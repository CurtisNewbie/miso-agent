package agentloop

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/util/llm"
)

// Registry manages tool registration and retrieval.
type ToolRegistry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register registers a tool in the registry.
func (r *ToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Merge merges another registry into this one.
func (r *ToolRegistry) Merge(other *ToolRegistry) {
	if other == nil {
		return
	}
	for name, t := range other.tools {
		r.tools[name] = t
	}
}

// toolWrapper wraps a Tool to implement tool.BaseTool.
type toolWrapper struct {
	tool          Tool
	toolCallChain ToolCallHandler // nil when no middleware registered
}

func (w *toolWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	var paramsOneOf *schema.ParamsOneOf
	if dt, ok := w.tool.(deductedTool); ok {
		paramsOneOf = dt.ParamsOneOf()
	} else {
		paramsOneOf = schema.NewParamsOneOfByParams(w.tool.Parameters())
	}
	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: paramsOneOf,
	}, nil
}

func (w *toolWrapper) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	if w.toolCallChain != nil {
		var args map[string]interface{}
		if input != "" {
			parsedArgs, err := llm.ParseLLMJsonAs[map[string]interface{}](input)
			if err == nil {
				args = parsedArgs
			}
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		resp, err := w.toolCallChain(ctx, &ToolCallRequest{Name: w.tool.Name(), Args: args, RawInput: input})
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return resp.Result, nil
	}

	// No middleware: use direct execution path.
	if selfInvokeTool, ok := w.tool.(SelfInvokeTool); ok {
		result, err := selfInvokeTool.ExecuteJson(ctx, input)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		return result, nil
	}

	var args map[string]interface{}
	if input != "" {
		parsedArgs, err := llm.ParseLLMJsonAs[map[string]interface{}](input)
		if err == nil {
			args = parsedArgs
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	result, err := w.tool.Execute(ctx, args)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return result, nil
}

// ToEinoTools converts the registry to Eino tool instances.
func (r *ToolRegistry) ToEinoTools() []tool.BaseTool {
	result := make([]tool.BaseTool, 0, len(r.tools))
	for _, t := range r.tools {
		wrapper := &toolWrapper{tool: t}
		result = append(result, wrapper)
	}
	return result
}

// ToEinoToolsWithChain converts the registry to Eino tool instances with a per-tool
// WrapToolCall middleware chain. When middlewares is empty, equivalent to ToEinoTools.
func (r *ToolRegistry) ToEinoToolsWithChain(middlewares []Middleware) []tool.BaseTool {
	if len(middlewares) == 0 {
		return r.ToEinoTools()
	}
	result := make([]tool.BaseTool, 0, len(r.tools))
	for _, t := range r.tools {
		tLocal := t
		terminal := func(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
			if st, ok := tLocal.(SelfInvokeTool); ok {
				result, err := st.ExecuteJson(ctx, req.RawInput)
				if err != nil {
					return &ToolCallResponse{Result: fmt.Sprintf("Error: %v", err), IsError: true}, nil
				}
				return &ToolCallResponse{Result: result}, nil
			}
			res, err := tLocal.Execute(ctx, req.Args)
			if err != nil {
				return &ToolCallResponse{Result: fmt.Sprintf("Error: %v", err), IsError: true}, nil
			}
			return &ToolCallResponse{Result: res}, nil
		}
		chain := buildToolCallChain(middlewares, terminal)
		result = append(result, &toolWrapper{tool: t, toolCallChain: chain})
	}
	return result
}
