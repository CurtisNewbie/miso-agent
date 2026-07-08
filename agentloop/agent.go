package agentloop

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/compose"
	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	"github.com/curtisnewbie/miso/util/idutil"
	"github.com/curtisnewbie/miso/util/slutil"
)

// agentOps holds the resolved operational configuration for the agent loop.
// It is built from AgentConfig flat fields in NewAgent and used internally
// for graph compilation and trace callbacks.
type agentOps struct {
	maxRunSteps                  int
	language                     string
	logOnStart                   bool
	logOnEnd                     bool
	logInputs                    bool
	logOutputs                   bool
	toolEventCallback            func(event ToolEvent)
	compaction                   bool
	compactPreserveRecentTokens  int
	compactBuffer                int // derived from compactionThreshold * MaxTokens
	maxTokens                    int // model context window size (0 = unknown)
	toolOffloadTokenLimit        int // 0 = disabled
	toolOffloadResultsPathPrefix string
	enableFileTool               bool
	enableTodoTool               bool
	enableToolOffload            bool
}

type ctxKey int

var (
	agentCtxKey    ctxKey = 0
	toolArgsCtxKey ctxKey = 1
)

// Agent is a ReAct (Reasoning + Acting) agent that can process tasks using tools and skills.
// The graph is compiled once and can be reused across multiple Execute calls for efficiency.
// Each Execute call receives fresh skills and backend via taskInput, allowing for stateful backends.
// Capabilities:
//   - Tool calling with automatic backend injection
//   - Skills system with progressive disclosure
//   - Token-aware message pruning
type Agent struct {
	config        AgentConfig
	ops           agentOps
	tools         *ToolRegistry
	tokenizer     Tokenizer
	graph         compose.Runnable[taskInput, taskOutput]
	middleware    []Middleware
	logPromptOnce sync.Once
}

// boolOrDefault returns *p if p is non-nil, otherwise returns def.
func boolOrDefault(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}

// NewAgent creates a new ReAct agent.
//
// Retry on model errors is not handled by the agent — configure it on the model itself.
// See [agents.NewOpenAIChatModel] and [agents.WithRetry].
func NewAgent(config AgentConfig, optCtx ...context.Context) (*Agent, error) {
	rail := flow.NewRail(slutil.VarArgAny(optCtx, func() context.Context { return context.Background() }))

	// Set defaults
	if config.Name == "" {
		config.Name = "AgentLoop"
	}
	if config.Language == "" {
		config.Language = "English"
	}

	if config.MaxRunSteps <= 0 {
		config.MaxRunSteps = 5
	}

	// Convert MaxRunSteps (rounds) to Eino graph steps.
	// Each round ≈ 4 graph steps; multiply by 5 to include a safety margin.
	maxGraphSteps := config.MaxRunSteps * 5

	// Build ops from individual config fields
	ops := agentOps{
		maxRunSteps:                 maxGraphSteps,
		language:                    config.Language,
		logOnStart:                  boolOrDefault(config.LogOnStart, true),
		logOnEnd:                    boolOrDefault(config.LogOnEnd, true),
		logInputs:                   boolOrDefault(config.LogInputs, false),
		logOutputs:                  boolOrDefault(config.LogOutputs, true),
		toolEventCallback:           config.ToolEventCallback,
		compaction:                  boolOrDefault(config.Compaction, false),
		compactPreserveRecentTokens: config.CompactPreserveRecentTokens,
	}

	// Auto-detect MaxTokens from model name if not explicitly set.
	if config.MaxTokens < 1 {
		if namer, ok := config.Model.(agents.ModelNamer); ok {
			if ctx, found := agents.LookupModelContextWindow(rail, namer.ModelName(), config.EnableModelsFetch); found {
				config.MaxTokens = ctx
				rail.Infof("Model %v max token detected: %v", namer.ModelName(), config.MaxTokens)
			}
		}
	}

	// Propagate MaxTokens into ops so trace callbacks can report context occupation.
	ops.maxTokens = config.MaxTokens

	// Derive compactBuffer and compactPreserveRecentTokens from MaxTokens.
	// Both are percentage-based to stay consistent regardless of model context size.
	if config.MaxTokens > 0 {
		ops.compactBuffer = int(float64(config.MaxTokens) * 0.2)
		if ops.compactPreserveRecentTokens <= 0 {
			// Keep 25% of context as recent verbatim tail.
			// Matches opencode's preserveRecentBudget: max(2000, min(8000, usable * 0.25)).
			ops.compactPreserveRecentTokens = max(2000, min(8000, int(float64(config.MaxTokens)*0.25)))
		}
	}

	// Tool result offloading
	const defaultToolTokenLimit = 20000
	if config.ToolOffloadTokenLimit == nil {
		ops.toolOffloadTokenLimit = defaultToolTokenLimit
	} else {
		ops.toolOffloadTokenLimit = *config.ToolOffloadTokenLimit
	}
	ops.toolOffloadResultsPathPrefix = config.ToolOffloadResultsPathPrefix
	ops.enableFileTool = boolOrDefault(config.EnableFileTool, true)
	ops.enableTodoTool = boolOrDefault(config.EnableTodoTool, false)

	// Disable offloading when file tools are unavailable (read_file would be inaccessible).
	ops.enableToolOffload = boolOrDefault(config.EnableToolOffload, true)
	if ops.toolOffloadTokenLimit < 1 {
		ops.enableToolOffload = false
	}
	if ops.enableToolOffload && !ops.enableFileTool {
		rail.Warnf("tool result offloading disabled: EnableFileTool is false")
		ops.enableToolOffload = false
	}

	// Warn if compaction is enabled but MaxTokens is not set; the compaction path
	// is guarded by MaxTokens > 0, so it would be silently skipped.
	if ops.compaction && config.MaxTokens <= 0 {
		rail.Warnf(
			"NewAgent %q: compaction is enabled but MaxTokens is not set; compaction will be silently skipped",
			config.Name,
		)
	}

	// Warn if compactPreserveRecentTokens is so large that selectForCompaction will never
	// find messages to summarize — compaction will silently do nothing.
	if config.MaxTokens > 0 && ops.compaction && ops.compactPreserveRecentTokens >= config.MaxTokens-ops.compactBuffer {
		rail.Warnf(
			"NewAgent %q: compactPreserveRecentTokens (%d) >= MaxTokens-compactBuffer (%d); compaction will never select messages to summarize — increase MaxTokens or reduce CompactPreserveRecentTokens",
			config.Name, ops.compactPreserveRecentTokens, config.MaxTokens-ops.compactBuffer,
		)
	}

	// Initialize tokenizer for token counting
	tokenizer := NewTokenizer()

	// Initialize tools
	toolRegistry := NewToolRegistry()

	// Add built-in tools (will receive backend and todoManager via context)
	builtinTools := BuiltinTools(
		WithEnableFileTool(ops.enableFileTool),
		WithEnableTodoTool(ops.enableTodoTool),
	)
	toolRegistry.Merge(builtinTools)

	// Add custom tools
	for _, t := range config.Tools {
		toolRegistry.Register(t)
	}

	// Register tools from middleware. Panic on name collision.
	for _, m := range config.Middleware {
		for _, t := range m.Tools() {
			if _, exists := toolRegistry.Get(t.Name()); exists {
				return nil, errs.NewErrf("middleware %q: tool name collision %q", m.Name(), t.Name())
			}
			toolRegistry.Register(t)
		}
	}

	names := make([]string, 0, len(toolRegistry.List()))
	for _, t := range toolRegistry.List() {
		names = append(names, t.Name())
	}
	rail.Infof("NewAgent %q tools: %v", config.Name, names)

	agent := &Agent{
		config:     config,
		ops:        ops,
		tools:      toolRegistry,
		tokenizer:  tokenizer,
		middleware: config.Middleware,
	}

	// Build the Eino graph (compiled once)
	graph, err := buildGraph(agent)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to build graph")
	}
	agent.graph = graph

	return agent, nil
}

// AgentContext holds per-execution stateful components.
// Accessible from tool and middleware callbacks via the context.
type AgentContext struct {
	SessionId string
	UserInput string
	Store     FileStore
	Todos     *TodoManager
	Artifacts *ArtifactManager
	Metadata  *MetadataStore
}

// AgentRequest represents a request to execute an agent
type AgentRequest struct {
	// SessionId is an optional identifier for this execution.
	// If empty, a unique ID is generated automatically with the prefix "sess_".
	SessionId           string
	UserInput           string
	PreloadBackendFiles func(store FileStore) error                       // Optional callback to preload files into the backend before execution
	ArtifactCallback    func(store FileStore, artifacts []Artifact) error // Optional callback for artifacts
}

// Execute runs the agent with the given request.
func (a *Agent) Execute(rail flow.Rail, req AgentRequest) (TaskOutput, error) {
	rail = rail.NextSpanId()
	if req.SessionId == "" {
		req.SessionId = idutil.Id("sess_")
	}
	rail.Infof("Execute agent %q, SessionId: %q, UserInput: %q", a.config.Name, req.SessionId, req.UserInput)

	// Initialize backend (fresh on each execution)
	var backend FileStore
	if a.config.BackendFactory != nil {
		backend = a.config.BackendFactory()
	}
	if backend == nil {
		backend = NewTmpFileStore()
	}

	// Session lifecycle: notify the backend that the session is starting.
	if sa, ok := backend.(SessionAware); ok {
		if err := sa.OnSessionStart(rail); err != nil {
			return TaskOutput{}, errs.Wrapf(err, "failed to start session")
		}
		defer func() {
			if err := sa.OnSessionEnd(rail); err != nil {
				rail.Errorf("failed to end session: %v", err)
			}
		}()
	}

	// Preload backend files if callback provided (runs after session start, before skills are loaded)
	if req.PreloadBackendFiles != nil {
		if err := req.PreloadBackendFiles(backend); err != nil {
			return TaskOutput{}, errs.Wrapf(err, "failed to preload backend files")
		}
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

	// Initialize metadata store (fresh on each execution)
	metadataStore := NewMetadataStore()

	agentCtxVal := AgentContext{
		SessionId: req.SessionId,
		UserInput: req.UserInput,
		Store:     backend,
		Todos:     todoManager,
		Artifacts: artifactManager,
		Metadata:  metadataStore,
	}
	rail = rail.WithCtxVal(agentCtxKey, agentCtxVal)

	// Call BeforeAgent on each middleware. Any error aborts execution.
	for _, m := range a.middleware {
		if err := m.BeforeAgent(agentCtxVal); err != nil {
			return TaskOutput{}, errs.Wrapf(err, "middleware %q BeforeAgent failed", m.Name())
		}
	}

	// Execute graph with agent-specific trace callback (always registered to collect token usage)
	acc := &tokenAccumulator{}
	invokeOpts := []compose.Option{withAgentTraceCallback(a.config.Name, a.ops, acc)}
	result, err := a.graph.Invoke(rail, taskInput, invokeOpts...)
	if err != nil {
		for _, m := range a.middleware {
			if afterErr := m.AfterAgent(agentCtxVal, nil, err); afterErr != nil {
				rail.Errorf("middleware %q AfterAgent error: %v", m.Name(), afterErr)
			}
		}
		return TaskOutput{}, errs.Wrapf(err, "failed to execute graph")
	}
	result.TokenUsage = acc.snapshot()
	tu := result.TokenUsage
	if tu.PromptTokens > 0 {
		msg := fmt.Sprintf("[%v] total — in: %v tokens, out: %v tokens", a.config.Name, tu.PromptTokens, tu.CompletionTokens)
		if tu.CachedTokens > 0 {
			msg += fmt.Sprintf(", cache hit: %v (%.1f%%)", tu.CachedTokens, float64(tu.CachedTokens)*100.0/float64(tu.PromptTokens))
		}
		rail.Info(msg)
	}

	// Call AfterAgent on each middleware.
	for _, m := range a.middleware {
		if afterErr := m.AfterAgent(agentCtxVal, &result, nil); afterErr != nil {
			rail.Errorf("middleware %q AfterAgent error: %v", m.Name(), afterErr)
		}
	}

	// Call ArtifactCallback if provided
	if req.ArtifactCallback != nil && len(result.Artifacts) > 0 {
		if err := req.ArtifactCallback(backend, result.Artifacts); err != nil {
			return result, errs.Wrapf(err, "artifact callback failed")
		}
	}

	return result, nil
}
