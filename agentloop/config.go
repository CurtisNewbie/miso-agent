package agentloop

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/cloudwego/eino/components/model"
)

// AgentConfig is the configuration for creating an agent.
type AgentConfig struct {
	// Name is an optional identifier for the agent used in logs.
	// If empty, defaults to "AgentLoop".
	Name string

	// Model is the LLM model to use.
	//
	// Retry behavior is controlled by the model wrapper, not the agent itself.
	// Use [agents.NewOpenAIChatModel] with [agents.WithRetry] to configure retry count and
	// backoff. The default model uses exponential backoff (1s, 2s, 4s, capped at 5s) with
	// 5 retries; 429 (rate limit) errors skip directly to the 5s cap.
	Model model.ToolCallingChatModel

	// MaxRunSteps limits the maximum number of ReAct rounds (tool-call cycles) the agent may execute.
	// Each round corresponds to one tool-calling iteration; the value is multiplied by 5 internally
	// to derive the actual Eino graph step budget (1 round ≈ 4 graph steps, ×5 includes a safety margin).
	// If 0 or negative, defaults to 5.
	MaxRunSteps int

	// Language specifies the language for agent responses.
	// If empty, defaults to "English".
	Language string

	// LogOnStart controls whether the agent logs when it starts processing.
	// If nil, defaults to true.
	LogOnStart *bool

	// LogOnEnd controls whether the agent logs token stats when it finishes.
	// If nil, defaults to true.
	LogOnEnd *bool

	// LogInputs controls whether the agent logs input messages to the model.
	// If nil, defaults to false.
	LogInputs *bool

	// LogOutputs controls whether the agent logs model output content.
	// If nil, defaults to true.
	LogOutputs *bool

	// Skills is a list of skill paths to load.
	// Skills are loaded from the backend and injected into the system prompt.
	Skills []string

	// PreloadedSkills is a map of file paths to content that will be written to the backend
	// before loading skills. This is useful for predefining skills when using TmpFileStore.
	// Example: {"/skills/web-research/SKILL.md": "# Web Research\n\n..."}
	//
	// See [BuildPreloadedSkills], [BuildPreloadedSkillsWithFilter]
	PreloadedSkills map[string]string

	// Tools is a list of custom tools to add to the built-in tools.
	// Built-in tools are always registered; this field adds additional tools.
	//
	// Create custom tools using helper functions:
	//   - [NewToolFunc] - for simple tools with map-based arguments
	//   - [NewCtxAwareToolFunc] - for tools needing AgentContext (Store, Todos)
	//   - [NewTypedToolFunc] - for tools with typed struct arguments
	//   - [NewTypedCtxAwareToolFunc] - for tools with typed arguments and AgentContext
	Tools []Tool

	// TaskPrompt is the main task prompt for the agent.
	TaskPrompt string

	// SystemPrompt is an optional custom system prompt.
	// If provided, it will be prepended to the base system prompt.
	SystemPrompt string

	// BackendFactory is a factory function that creates a fresh FileStore for each execution.
	// If nil, a new TmpFileStore will be created for each execution.
	// This allows stateful backends to be created fresh per execution.
	BackendFactory func() FileStore

	// Timezone is the timezone offset in hours for time display (default: 0, UTC).
	Timezone float64

	// MaxTokens is the maximum number of tokens allowed in the conversation history.
	// When exceeded, old messages will be pruned (except system messages).
	// If 0 or negative, no token limit is enforced.
	// Default: 0 (no limit)
	MaxTokens int

	// EnableModelsFetch enables runtime fetching of model context window sizes
	// from models.dev when the model is not found in the build-time generated map.
	// Fetched results are cached in memory for the process lifetime.
	EnableModelsFetch bool

	// EnableFileTool enables the built-in file tools: read_file, write_file, edit_file,
	// list_directory, glob, and add_artifact. When false, these tools are not registered.
	// If nil, defaults to true.
	EnableFileTool *bool

	// EnableTodoTool enables the built-in todo tools: add_todo, update_todo, list_todos,
	// delete_todo. When false, these tools are not registered.
	// If nil, defaults to false.
	EnableTodoTool *bool

	// ToolEventCallback is called synchronously for each tool invocation during execution.
	// Receives a ToolEvent with the tool name and raw JSON args before the tool runs.
	// Must not block for long — it runs within the agent graph execution.
	// If nil, no events are emitted.
	ToolEventCallback func(event ToolEvent)

	// Compaction enables LLM-based context compaction when the conversation history
	// approaches MaxTokens. Older messages are summarized into a structured checkpoint;
	// recent messages are kept verbatim. Requires MaxTokens to be set.
	// If nil, defaults to false.
	Compaction *bool

	// CompactPreserveRecentTokens is the token budget for the verbatim recent tail kept after compaction.
	// Messages within this budget (newest first) are sent to the model as-is; older messages are summarized.
	// When MaxTokens is known, defaults to max(2000, min(8000, MaxTokens * 0.25)) — i.e., 25% of the
	// context window, clamped between 2k and 8k tokens. Set explicitly to override.
	CompactPreserveRecentTokens int

	// ToolOffloadTokenLimit is the token threshold above which a tool result is
	// offloaded to the backend store and replaced with a short preview + file pointer.
	// The agent can recover the full content by calling the read_file tool on the
	// saved path. nil uses the default threshold of 20,000 tokens. Set to a non-nil
	// pointer to 0 to disable offloading entirely.
	//
	// The following tools are never offloaded regardless of size: read_file,
	// write_file, edit_file, list_directory, glob, grep, delete.
	ToolOffloadTokenLimit *int

	// ToolOffloadResultsPathPrefix is the FileStore path prefix for offloaded tool
	// results. Each offloaded result is written to:
	//   {ToolOffloadResultsPathPrefix}/{sanitized_tool_call_id}
	// Defaults to "/large_tool_results" when empty.
	ToolOffloadResultsPathPrefix string

	// EnableToolOffload controls whether large tool results are offloaded to the
	// backend store and replaced with a preview + file pointer.
	// If nil, defaults to true. Set to a non-nil pointer to false to disable.
	EnableToolOffload *bool

	// Middleware is an ordered list of middleware to apply to the agent loop.
	// Middlewares are called in registration order for BeforeAgent, AfterAgent,
	// SystemPromptFragment, and Tools; and composed into chains for WrapModelCall
	// and WrapToolCall. Middleware cannot be registered after NewAgent() returns.
	Middleware []Middleware
}

// BuildPreloadedSkills builds a PreloadedSkills map from an embedded filesystem.
// The baseDirs are the root directories within the embedded FS to start from.
// File paths in the returned map will be relative to the baseDir and prefixed with '/'.
//
// Example:
//
//	//go:embed skills/*
//	var skillsFS embed.FS
//
//	preloaded := BuildPreloadedSkills(skillsFS, "skills")
//	// Returns: map[string]string{
//	//   "/skills/web-research/SKILL.md": "...",
//	//   "/skills/code-analysis/SKILL.md": "...",
//	// }
//
// Multiple base dirs:
//
//	preloaded := BuildPreloadedSkills(skillsFS, "skills", "templates")
func BuildPreloadedSkills(efs embed.FS, baseDirs ...string) map[string]string {
	result := make(map[string]string)

	for _, baseDir := range baseDirs {
		// Ensure baseDir doesn't have trailing slash
		baseDir = strings.TrimSuffix(baseDir, "/")

		// Walk through the embedded filesystem
		err := fs.WalkDir(efs, baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files that can't be accessed
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Read file content
			content, err := efs.ReadFile(path)
			if err != nil {
				return nil // Skip files that can't be read
			}

			// Build the virtual path (key for PreloadedSkills)
			// Convert "skills/web-research/SKILL.md" to "/skills/web-research/SKILL.md"
			virtualPath := "/" + path
			result[virtualPath] = string(content)

			return nil
		})

		if err != nil {
			// If walk fails, return whatever we've collected
			return result
		}
	}

	return result
}

// BuildPreloadedSkillsWithFilter builds a PreloadedSkills map from an embedded filesystem
// with a custom filter function. The filter function receives the file path and should return
// true if the file should be included in the result.
//
// Example:
//
//	//go:embed skills/*
//	var skillsFS embed.FS
//
//	// Only include SKILL.md files
//	preloaded := BuildPreloadedSkillsWithFilter(skillsFS, func(path string) bool {
//	    return strings.HasSuffix(path, "SKILL.md")
//	}, "skills")
//
// Multiple base dirs:
//
//	preloaded := BuildPreloadedSkillsWithFilter(skillsFS, func(path string) bool {
//	    return strings.HasSuffix(path, "SKILL.md")
//	}, "skills", "templates")
func BuildPreloadedSkillsWithFilter(efs embed.FS, filter func(path string) bool, baseDirs ...string) map[string]string {
	result := make(map[string]string)

	for _, baseDir := range baseDirs {
		// Ensure baseDir doesn't have trailing slash
		baseDir = strings.TrimSuffix(baseDir, "/")

		// Walk through the embedded filesystem
		err := fs.WalkDir(efs, baseDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip files that can't be accessed
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Apply filter
			if filter != nil && !filter(path) {
				return nil
			}

			// Read file content
			content, err := efs.ReadFile(path)
			if err != nil {
				return nil // Skip files that can't be read
			}

			// Build the virtual path (key for PreloadedSkills)
			virtualPath := "/" + path
			result[virtualPath] = string(content)

			return nil
		})

		if err != nil {
			// If walk fails, return whatever we've collected
			return result
		}
	}

	return result
}
