package tools

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Registry manages tool registration and retrieval.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register registers a tool in the registry.
func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *Registry) List() []Tool {
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}

// Merge merges another registry into this one.
func (r *Registry) Merge(other *Registry) {
	if other == nil {
		return
	}
	for name, t := range other.tools {
		r.tools[name] = t
	}
}

// toolWrapper wraps a Tool to implement tool.BaseTool.
type toolWrapper struct {
	tool Tool
}

func (w *toolWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := make(map[string]*schema.ParameterInfo)
	for name, param := range w.tool.Parameters() {
		params[name] = &schema.ParameterInfo{
			Type: schema.DataType(param.Type),
			Desc: param.Description,
		}
	}

	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

func (w *toolWrapper) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	args := make(map[string]interface{})
	if input != "" {
		_ = json.Unmarshal([]byte(input), &args)
	}
	return w.tool.Execute(ctx, args)
}

// ToEinoTools converts the registry to Eino tool instances.
func (r *Registry) ToEinoTools() []tool.BaseTool {
	result := make([]tool.BaseTool, 0, len(r.tools))
	for _, t := range r.tools {
		wrapper := &toolWrapper{tool: t}
		result = append(result, wrapper)
	}
	return result
}
