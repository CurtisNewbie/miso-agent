package types

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/curtisnewbie/miso-agent/agentloop/backend"
)

// AgentConfig is the configuration for creating an agent.
type AgentConfig struct {
	// Model is the LLM model to use.
	Model model.ToolCallingChatModel

	// Skills is a list of skill paths to load.
	// Skills are loaded from the backend and injected into the system prompt.
	Skills []string

	// Tools is a list of tools available to the agent.
	// If nil, built-in tools will be used.
	Tools []Tool

	// TaskPrompt is the main task prompt for the agent.
	TaskPrompt string

	// SystemPrompt is an optional custom system prompt.
	// If provided, it will be prepended to the base system prompt.
	SystemPrompt string

	// Backend is the file storage backend.
	// If nil, a new MemFileBackend will be created.
	Backend backend.FileBackendProtocol

	// MaxSteps is the maximum number of steps in the ReAct loop.
	MaxSteps int

	// Language is the language for the agent (default: "English").
	Language string

	// Timezone is the timezone offset in hours for time display.
	Timezone float64
}

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

// DefaultConfig returns a default agent configuration.
func DefaultConfig() AgentConfig {
	return AgentConfig{
		MaxSteps: 100,
		Language: "English",
		Timezone: 0,
	}
}
