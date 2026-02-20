package agentloop

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/llm"
)

// Wrapper wraps a Tool into an Eino BaseTool.
type Wrapper struct {
	tool Tool
}

// NewWrapper creates a new tool wrapper.
func NewWrapper(tool Tool) *Wrapper {
	return &Wrapper{
		tool: tool,
	}
}

// Info returns the tool info for Eino.
func (w *Wrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(w.tool.Parameters()),
	}, nil
}

// InvokableRun executes the tool.
func (w *Wrapper) InvokableRun(ctx context.Context, argsInJSON string, opts ...tool.Option) (string, error) {
	var args map[string]any
	if argsInJSON != "" {
		parsedArgs, err := llm.ParseLLMJsonAs[map[string]any](argsInJSON)
		if err != nil {
			return "", err
		}
		args = parsedArgs
	}
	result, err := w.tool.Execute(ctx, args)
	if err != nil {
		return "", err
	}
	// Convert result to string
	switch v := any(result).(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case interface{}:
		// Try to convert to JSON
		if b, err := json.Marshal(v); err == nil {
			return string(b), nil
		}
		return "", errs.NewErrf("unsupported result type: %T", result)
	default:
		return "", errs.NewErrf("unsupported result type: %T", result)
	}
}

// ConvertToEinoTools converts a list of Tool to Eino BaseTools.
func ConvertToEinoTools(tools []Tool) ([]tool.BaseTool, error) {
	result := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		result[i] = NewWrapper(t)
	}
	return result, nil
}

// ConvertRegistryToBaseTools converts a registry to Eino BaseTools.
func ConvertRegistryToBaseTools(registry *ToolRegistry) ([]tool.BaseTool, error) {
	tools := registry.List()
	return ConvertToEinoTools(tools)
}
