package agentloop

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/cloudwego/eino/components/model"
)

// AgentConfig is the configuration for creating an agent.
type AgentConfig struct {
	// Model is the LLM model to use.
	Model model.ToolCallingChatModel

	// MaxRunSteps limits the number of graph steps before the agent terminates.
	// If 0 or negative, the agent runs without a step limit.
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

	// TokenizerModelName is the name of the model used for token counting.
	// This is used to select the appropriate tiktoken encoding (e.g., cl100k_base for gpt-3.5-turbo, o200k_base for gpt-4o).
	// If empty, defaults to "gpt-3.5-turbo".
	// Common values: "gpt-3.5-turbo", "gpt-4", "gpt-4o", "qwen-plus", "deepseek-chat"
	TokenizerModelName string

	// EvictToolResultsThreshold is the maximum token count for tool results before eviction.
	// Tool results exceeding this threshold are evicted to the filesystem and replaced with a reference.
	// If 0 or negative, no eviction is performed.
	// Default: 0 (no eviction)
	// Recommended: 1000-2000 tokens for most use cases
	EvictToolResultsThreshold int

	// EvictToolResultsKeepPreview is the number of tokens to keep as a preview in the reference message.
	// Allows the agent to see context without loading the full content.
	// If 0, no preview is kept (only metadata).
	// Default: 0 (no preview)
	EvictToolResultsKeepPreview int

	// EnableFileTool enables the built-in file tools: read_file, write_file, edit_file,
	// list_directory, glob, and add_artifact. When false, these tools are not registered.
	// Default: false
	EnableFileTool bool
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
