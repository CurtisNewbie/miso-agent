package agentloop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
)

// Agent represents a ReAct agent with skills and tools.
type Agent struct {
	config      AgentConfig
	skills      *Skills
	tools       *ToolRegistry
	todoManager *TodoManager
	tokenizer   *Tokenizer
	graph       compose.Runnable[taskInput, finalOutput]
}

// NewAgent creates a new ReAct agent.
func NewAgent(config AgentConfig) (*Agent, error) {
	// Set defaults
	if config.MaxSteps <= 0 {
		config.MaxSteps = 100
	}
	if config.Language == "" {
		config.Language = "English"
	}

	// Initialize tokenizer for accurate token counting
	tokenizer, err := NewTokenizer(config.TokenizerModelName)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to initialize tokenizer for model %s", config.TokenizerModelName)
	}

	// Initialize backend
	if config.Backend == nil {
		config.Backend = NewMemFileStore()
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
	skillsMiddleware := NewSkills(config.Backend)
	if len(config.Skills) > 0 {
		ctx := context.Background()
		if err := skillsMiddleware.Load(ctx, config.Skills); err != nil {
			return nil, errs.Wrapf(err, "failed to load skills")
		}
	}

	// Initialize tools
	toolRegistry := NewToolRegistry()

	// Create todo manager
	todoManager := NewTodoManager()

	// Add built-in tools (including todo tools)
	builtinTools := BuiltinTools(config.Backend, todoManager)
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
		tokenizer:   tokenizer,
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
func (a *Agent) Execute(rail flow.Rail, userInput string) (string, error) {
	// Prepare input
	taskInput := taskInput{
		task: userInput,
	}

	// Execute graph
	result, err := a.graph.Invoke(rail, taskInput)
	if err != nil {
		return "", errs.Wrapf(err, "failed to execute graph")
	}

	return result.response, nil
}

// evictToolResult stores a large tool result to the backend and returns a reference message.
// The agent can use read_file with offset/limit to retrieve specific sections of the evicted content.
func (a *Agent) evictToolResult(msg *schema.Message) *schema.Message {
	// Generate unique reference ID
	refID := fmt.Sprintf("tool-result-%s", msg.ToolCallID)

	// Store full content to backend
	filePath := fmt.Sprintf("/tool-results/%s.txt", refID)
	if err := a.config.Backend.WriteFile(context.Background(), filePath, []byte(msg.Content)); err != nil {
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
func (a *Agent) evictLargeToolResults(messages []*schema.Message) []*schema.Message {
	result := make([]*schema.Message, len(messages))
	copy(result, messages)

	for i, msg := range result {
		if msg.Role == schema.Tool && len(msg.ToolCallID) > 0 && i != len(result)-1 {
			// Check if this result should be evicted
			if a.shouldEvictToolResult(msg) {
				result[i] = a.evictToolResult(msg)
			}
		}
	}

	return result
}
