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
	// Use full schema for proper nested structure conversion
	fullSchema := w.tool.FullSchema()
	params := make(map[string]*schema.ParameterInfo)
	for name, paramSchema := range fullSchema {
		if paramMap, ok := paramSchema.(map[string]interface{}); ok {
			params[name] = convertSchemaToParameterInfo(paramMap)
		}
	}

	return &schema.ToolInfo{
		Name:        w.tool.Name(),
		Desc:        w.tool.Description(),
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

// convertSchemaToParameterInfo recursively converts schema map to Eino ParameterInfo
func convertSchemaToParameterInfo(paramSchema map[string]interface{}) *schema.ParameterInfo {
	info := &schema.ParameterInfo{
		Type: schema.DataType(getString(paramSchema, "type")),
		Desc: getString(paramSchema, "description"),
	}

	// Handle enum values
	if enumVal, ok := paramSchema["enum"].([]interface{}); ok {
		enum := make([]string, 0, len(enumVal))
		for _, e := range enumVal {
			if s, ok := e.(string); ok {
				enum = append(enum, s)
			}
		}
		info.Enum = enum
	}

	// Handle required field
	if required, ok := paramSchema["required"].(bool); ok {
		info.Required = required
	}

	// Handle array type - convert items to ElemInfo
	if info.Type == schema.Array {
		if items, ok := paramSchema["items"].(map[string]interface{}); ok {
			info.ElemInfo = convertSchemaToParameterInfo(items)
		}
	}

	// Handle object type - convert properties to SubParams
	if info.Type == schema.Object {
		if properties, ok := paramSchema["properties"].(map[string]interface{}); ok {
			subParams := make(map[string]*schema.ParameterInfo)
			for propName, propSchema := range properties {
				if propMap, ok := propSchema.(map[string]interface{}); ok {
					subParams[propName] = convertSchemaToParameterInfo(propMap)
				}
			}
			info.SubParams = subParams
		}

		// Handle required fields array - check []string first
		if stringFields, ok := paramSchema["required"].([]string); ok {
			if info.SubParams != nil {
				for _, req := range stringFields {
					if param, exists := info.SubParams[req]; exists {
						param.Required = true
					}
				}
			}
		} else if requiredFields, ok := paramSchema["required"].([]interface{}); ok {
			if info.SubParams != nil {
				for _, req := range requiredFields {
					if reqName, ok := req.(string); ok {
						if param, exists := info.SubParams[reqName]; exists {
							param.Required = true
						}
					}
				}
			}
		}
	}

	return info
}

func (w *toolWrapper) InvokableRun(ctx context.Context, input string, opts ...tool.Option) (string, error) {
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

	if st, ok := ctx.Value(fileStoreCtxKey).(FileStore); ok && st != nil {
		args[ArgKeyAgentLoopFileStore] = st
	}

	if tm, ok := ctx.Value(todoManagerCtxKey).(*TodoManager); ok && tm != nil {
		args[ArgKeyAgentLoopTodoManager] = tm
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
