package agentloop

import (
	"context"
	"embed"
	"io/fs"

	"github.com/cloudwego/eino/components/model"
)

// OutputCheckFunc is a callback invoked on each final assistant response before the agent
// accepts it as the output.
//
// agentCtx provides access to the current execution context (session ID, user input, file store,
// todos, artifacts, metadata), enabling checks that inspect or update agent state.
//
// attempt is the 1-based invocation count for the current execution, so the callback can
// apply different logic on the first check versus subsequent retries (e.g. give up after N attempts).
//
// Return values:
//   - ok=true: output is accepted; agent proceeds to final_output.
//   - ok=false: output is rejected; hint is inserted as a user message and the agent retries.
//   - err!=nil: unexpected failure (e.g. network error); the agent aborts immediately.
//
// OutputCheckFunc may be used for any per-response validation: output format compliance,
// quality assessment, security screening, and so on.
type OutputCheckFunc func(ctx context.Context, agentCtx AgentContext, attempt int, output string) (hint string, ok bool, err error)

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
	// Internally it is multiplied by the number of nodes in the compiled graph to derive the Eino
	// step budget (each node may execute at most MaxRunSteps times).
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

	// PreloadedSkills is a map of file paths to content that will be written to the backend
	// before loading skills. This is useful for predefining skills when using TmpFileStore.
	// All skill paths must be under /skills/, e.g. {"/skills/web-research/SKILL.md": "# Web Research\n\n..."}.
	// Skills are automatically discovered from the /skills/ directory on each execution.
	//
	// See [BuildPreloadedSkills]
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

	// SystemPrompt is an optional task prompt.
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

	// OutputCheck is an optional callback invoked on each plain-text (non-tool-call) assistant
	// response before the agent accepts it as the final output. If OutputCheck returns a non-nil
	// error, its message is inserted into the conversation as a user message and the chat model
	// is called again so it can self-correct.
	//
	// OutputCheck can be used for output format validation, quality checks, security screening,
	// or any other per-response review. If nil, no check is performed.
	OutputCheck OutputCheckFunc

	// EnableTrace enables per-node execution tracing. When true, each graph node's input and
	// output are JSON-marshaled and collected in TaskOutput.TraceLogs. ChatModel entries include
	// the full message history per call, so TraceLogs can grow large on long multi-turn runs.
	// If nil, defaults to false.
	EnableTrace *bool
}

// BuildPreloadedSkills builds a PreloadedSkills map from an embedded filesystem.
// The efs root must contain skill directories directly (each with a SKILL.md file).
// If skillNames are provided, only skills with matching directory names are included;
// if no skillNames are given, all top-level skill directories are included.
// Resulting paths are prefixed with /skills/ to satisfy the store convention.
//
// Example:
//
//	//go:embed all:*
//	var skillsFS embed.FS
//
//	// Load all skills
//	preloaded := BuildPreloadedSkills(skillsFS)
//	// Returns: map[string]string{
//	//   "/skills/humanizer/SKILL.md": "...",
//	//   "/skills/web-research/SKILL.md": "...",
//	// }
//
//	// Load only specific skills
//	preloaded := BuildPreloadedSkills(skillsFS, "humanizer")
//	// Returns: map[string]string{
//	//   "/skills/humanizer/SKILL.md": "...",
//	// }
//
// The returned map is passed to [AgentConfig.PreloadedSkills]. Skills are
// automatically discovered from the /skills/ directory on each execution.
func BuildPreloadedSkills(efs embed.FS, skills ...string) map[string]string {
	result := make(map[string]string)

	nameSet := make(map[string]bool, len(skills))
	for _, n := range skills {
		nameSet[n] = true
	}

	entries, err := fs.ReadDir(efs, ".")
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(nameSet) > 0 && !nameSet[name] {
			continue
		}

		_ = fs.WalkDir(efs, name, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			content, err := efs.ReadFile(path)
			if err != nil {
				return nil
			}
			result["/skills/"+path] = string(content)
			return nil
		})
	}

	return result
}
