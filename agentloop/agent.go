package agentloop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso-agent/graph"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
)

type ctxKey int

var (
	agentCtxKey ctxKey
)

// Agent is a ReAct (Reasoning + Acting) agent that can process tasks using tools and skills.
// The graph is compiled once and can be reused across multiple Execute calls for efficiency.
// Each Execute call receives fresh skills and backend via taskInput, allowing for stateful backends.
// Capabilities:
//   - Tool calling with automatic backend injection
//   - Skills system with progressive disclosure
//   - Token-aware message pruning
//   - Tool result eviction for large outputs
//   - Support for finish_tool to signal task completion
type Agent struct {
	config    AgentConfig
	tools     *ToolRegistry
	tokenizer *Tokenizer
	graph     compose.Runnable[taskInput, taskOutput]
}

// NewAgent creates a new ReAct agent.
func NewAgent(config AgentConfig) (*Agent, error) {
	if config.GenericOps == nil {
		config.GenericOps = graph.NewGenericOps()
	}

	// Set defaults
	if config.GenericOps.MaxRunSteps <= 0 {
		config.GenericOps.MaxRunSteps = 100
	}
	if config.Language == "" {
		config.Language = "English"
	}

	// Initialize tokenizer for accurate token counting
	tokenizer, err := NewTokenizer(config.TokenizerModelName)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to initialize tokenizer for model %s", config.TokenizerModelName)
	}

	// Initialize tools
	toolRegistry := NewToolRegistry()

	// Add built-in tools (will receive backend and todoManager via context)
	builtinTools := BuiltinTools(
		WithEnableFileTool(config.EnableFileTool),
		WithEnableFinishTool(config.EnableFinishTool),
	)
	toolRegistry.Merge(builtinTools)

	// Add custom tools
	for _, t := range config.Tools {
		toolRegistry.Register(t)
	}

	agent := &Agent{
		config:    config,
		tools:     toolRegistry,
		tokenizer: tokenizer,
	}

	// Build the Eino graph (compiled once)
	graph, err := buildGraph(agent)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to build graph")
	}
	agent.graph = graph

	return agent, nil
}

type AgentContext struct {
	Store     FileStore
	Todos     *TodoManager
	Artifacts *ArtifactManager
}

// AgentRequest represents a request to execute an agent
type AgentRequest struct {
	UserInput        string
	ArtifactCallback func(store FileStore, artifacts []Artifact) error // Optional callback for artifacts
}

// Execute runs the agent with the given request.
func (a *Agent) Execute(rail flow.Rail, req AgentRequest) (TaskOutput, error) {
	// Initialize backend (fresh on each execution)
	var backend FileStore
	if a.config.BackendFactory != nil {
		backend = a.config.BackendFactory()
	}
	if backend == nil {
		backend = NewMemFileStore()
	}

	// Write preloaded skills into the backend
	if len(a.config.PreloadedSkills) > 0 {
		ctx := context.Background()
		for path, content := range a.config.PreloadedSkills {
			if err := backend.WriteFile(ctx, path, []byte(content)); err != nil {
				return TaskOutput{}, errs.Wrapf(err, "failed to write preloaded skill %s", path)
			}
		}
	}

	// Initialize skills middleware with fresh backend
	skills := NewSkills(backend)
	if len(a.config.Skills) > 0 {
		ctx := context.Background()
		if err := skills.Load(ctx, a.config.Skills); err != nil {
			return TaskOutput{}, errs.Wrapf(err, "failed to load skills")
		}
	}

	// Prepare input with backend and skills
	taskInput := taskInput{
		task:   req.UserInput,
		skills: skills,
		store:  backend,
	}

	// Initialize todo manager (fresh on each execution)
	todoManager := NewTodoManager()

	// Initialize artifact manager (fresh on each execution)
	artifactManager := NewArtifactManager()

	// Propagate stateful components via context
	rail = rail.WithCtxVal(agentCtxKey, AgentContext{
		Store:     backend,
		Todos:     todoManager,
		Artifacts: artifactManager,
	})

	// Execute graph
	result, err := graph.InvokeGraph(rail, a.config.GenericOps, a.graph, "AgentLoop", taskInput)
	if err != nil {
		return TaskOutput{}, errs.Wrapf(err, "failed to execute graph")
	}

	// Call ArtifactCallback if provided
	if req.ArtifactCallback != nil && len(result.Artifacts) > 0 {
		if err := req.ArtifactCallback(backend, result.Artifacts); err != nil {
			return result, errs.Wrapf(err, "artifact callback failed")
		}
	}

	return result, nil
}

// evictToolResult stores a large tool result to the backend and returns a reference message.
// The agent can use read_file with offset/limit to retrieve specific sections of the evicted content.
func (a *Agent) evictToolResult(backend FileStore, msg *schema.Message) *schema.Message {
	// Generate unique reference ID
	refID := fmt.Sprintf("tool-result-%s", msg.ToolCallID)

	// Store full content to backend
	filePath := fmt.Sprintf("/tool-results/%s.txt", refID)
	if err := backend.WriteFile(context.Background(), filePath, []byte(msg.Content)); err != nil {
		// If write fails, return original message (better than losing data)
		return msg
	}

	// Calculate token count
	totalTokens := a.tokenizer.CountTokens(msg.Content)

	// Generate preview if configured
	preview := ""
	if a.config.EvictToolResultsKeepPreview > 0 {
		// Read first N tokens as preview
		lines := strings.Split(msg.Content, "\n")
		previewTokens := 0
		var previewLines []string

		for _, line := range lines {
			lineTokens := a.tokenizer.CountTokens(line)
			if previewTokens+lineTokens > a.config.EvictToolResultsKeepPreview {
				break
			}
			previewTokens += lineTokens
			previewLines = append(previewLines, line)
		}

		preview = strings.Join(previewLines, "\n")
		if len(previewLines) < len(lines) {
			preview += "\n... [truncated]"
		}
	}

	// Create reference message with instructions for chunked reading
	referenceContent := fmt.Sprintf(
		"[Tool result evicted to: %s]\n"+
			"Tokens: %d\n"+
			"Preview:\n%s\n\n"+
			"Use read_file with offset/limit to read specific sections:\n"+
			"  - read_file(path='%s', offset=0, limit=100)  # Read first 100 lines\n"+
			"  - read_file(path='%s', offset=100, limit=100) # Read next 100 lines\n",
		filePath,
		totalTokens,
		preview,
		filePath,
		filePath,
	)

	return &schema.Message{
		Role:       msg.Role,
		ToolCallID: msg.ToolCallID,
		Content:    referenceContent,
	}
}

// shouldEvictToolResult checks if a tool result should be evicted based on configuration.
// Returns true if the tool result should be evicted.
func (a *Agent) shouldEvictToolResult(msg *schema.Message) bool {
	// Check if eviction is enabled
	if a.config.EvictToolResultsThreshold <= 0 {
		return false
	}

	// Check if this is a tool result message
	if msg.Role != schema.Tool || len(msg.ToolCallID) == 0 {
		return false
	}

	// Check token count
	resultTokens := a.tokenizer.CountTokens(msg.Content)
	return resultTokens > a.config.EvictToolResultsThreshold
}

// evictLargeToolResults processes messages and evicts large tool results.
func (a *Agent) evictLargeToolResults(backend FileStore, messages []*schema.Message) []*schema.Message {
	result := make([]*schema.Message, len(messages))
	copy(result, messages)

	for i, msg := range result {
		if msg.Role == schema.Tool && len(msg.ToolCallID) > 0 && i != len(result)-1 {
			// Check if this result should be evicted
			if a.shouldEvictToolResult(msg) {
				result[i] = a.evictToolResult(backend, msg)
			}
		}
	}

	return result
}
