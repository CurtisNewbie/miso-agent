package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/agentloop/types"
)

// Wrapper wraps a types.Tool into an Eino BaseTool.
type Wrapper struct {
	tool types.Tool
}

// NewWrapper creates a new tool wrapper.
func NewWrapper(tool types.Tool) *Wrapper {
	return &Wrapper{
		tool: tool,
	}
}

// Info returns the tool info for Eino.
func (w *Wrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	// Convert our ParameterInfo to schema.ParameterInfo
	params := make(map[string]*schema.ParameterInfo)
	for key, val := range w.tool.Parameters() {
		params[key] = &schema.ParameterInfo{
			Type: schema.DataType(val.Type),
			Desc: val.Description,
		}
	}

	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

// Invokable executes the tool.
func (w *Wrapper) Invokable(ctx context.Context, args map[string]any) (any, error) {
	return w.tool.Execute(ctx, args)
}

// Stream is not supported.
func (w *Wrapper) Stream(ctx context.Context, args map[string]any) (*schema.StreamReader[any], error) {
	return nil, fmt.Errorf("stream not supported")
}

// ConvertToEinoTools converts a list of types.Tool to Eino BaseTools.
func ConvertToEinoTools(tools []types.Tool) ([]tool.BaseTool, error) {
	result := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		result[i] = NewWrapper(t)
	}
	return result, nil
}

// ConvertRegistryToBaseTools converts a registry to Eino BaseTools.
func ConvertRegistryToBaseTools(registry *Registry) ([]tool.BaseTool, error) {
	tools := registry.List()
	return ConvertToEinoTools(tools)
}
