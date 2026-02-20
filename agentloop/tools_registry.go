package agentloop

import (
	"context"

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
	tool Tool
}

func (w *toolWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(w.tool.Parameters()),
	}, nil
}

func (w *toolWrapper) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
	// Parse input as JSON
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

	return w.tool.Execute(ctx, args)
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
