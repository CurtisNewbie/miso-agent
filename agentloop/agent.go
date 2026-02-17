package agentloop

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso-agent/agentloop/backend"
	"github.com/curtisnewbie/miso-agent/agentloop/skills"
	"github.com/curtisnewbie/miso-agent/agentloop/tools"
	"github.com/curtisnewbie/miso-agent/agentloop/types"
	"github.com/curtisnewbie/miso/errs"
)

// Agent represents a ReAct agent with skills and tools.
type Agent struct {
	config      types.AgentConfig
	skills      *skills.Middleware
	tools       *tools.Registry
	todoManager *tools.TodoManager
	graph       compose.Runnable[TaskInput, finalOutput]
}

// NewAgent creates a new ReAct agent.
func NewAgent(config types.AgentConfig) (*Agent, error) {
	// Set defaults
	if config.MaxSteps <= 0 {
		config.MaxSteps = 100
	}
	if config.Language == "" {
		config.Language = "English"
	}

	// Initialize backend
	if config.Backend == nil {
		config.Backend = backend.NewMemFileBackend()
	}

	// Write preloaded skills into the backend
	if len(config.PreloadedSkills) > 0 {
		ctx := context.Background()
		for path, content := range config.PreloadedSkills {
			if err := config.Backend.WriteFile(ctx, path, []byte(content)); err != nil {
				return nil, errs.Wrapf(err, "failed to write preloaded skill %s", path)
			}
		}
	}

	// Initialize skills middleware
	skillsMiddleware := skills.NewMiddleware(config.Backend, config.Skills)

	// Initialize tools
	toolRegistry := tools.NewRegistry()

	// Create todo manager
	todoManager := tools.NewTodoManager()

	// Add built-in tools (including todo tools)
	builtinTools := tools.BuiltinTools(config.Backend, todoManager)
	toolRegistry.Merge(builtinTools)

	// Add custom tools
	for _, t := range config.Tools {
		toolRegistry.Register(t)
	}

	// Load skills
	if len(config.Skills) > 0 {
		if err := skillsMiddleware.Load(context.Background(), config.Skills); err != nil {
			return nil, errs.Wrapf(err, "failed to load skills")
		}
	}

	agent := &Agent{
		config:      config,
		skills:      skillsMiddleware,
		tools:       toolRegistry,
		todoManager: todoManager,
	}

	// Build the Eino graph
	graph, err := buildGraph(agent)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to build graph")
	}
	agent.graph = graph

	return agent, nil
}

// Execute runs the agent with the given user input.
func (a *Agent) Execute(ctx context.Context, userInput string) (string, error) {
	// Prepare input
	taskInput := TaskInput{
		task: userInput,
	}

	// Execute graph
	result, err := a.graph.Invoke(ctx, taskInput)
	if err != nil {
		return "", errs.Wrapf(err, "failed to execute graph")
	}

	return result.response, nil
}

// GetTools returns the agent's tool registry.
func (a *Agent) GetTools() *tools.Registry {
	return a.tools
}

// GetSkills returns the agent's skills middleware.
func (a *Agent) GetSkills() *skills.Middleware {
	return a.skills
}

// GetTodoManager returns the agent's todo manager.
func (a *Agent) GetTodoManager() *tools.TodoManager {
	return a.todoManager
}
