package agentloop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	gobash "github.com/mark3labs/go-bash"
	"github.com/mark3labs/go-bash/command"
	"github.com/mark3labs/go-bash/fs/rwfs"
	"github.com/mark3labs/go-bash/network"

	"github.com/curtisnewbie/miso/errs"
)

type BashCommand = command.Command

// bashSandboxHolder lazily holds the per-session *gobash.Bash sandbox instance used
// by the bash tool, created on first use and reused for subsequent bash tool calls
// within the same Execute() call. Held via AgentContext.bash (unexported), so a fresh
// holder — and thus a fresh sandbox — is created for each agent session.
type bashSandboxHolder struct {
	mu      sync.Mutex
	sandbox *gobash.Bash
}

// BashArgs are the arguments accepted by the bash tool.
type BashArgs struct {
	Script string `json:"script"`
}

// bashToolConfig holds configuration for the built-in bash tool.
type bashToolConfig struct {
	timeout         time.Duration
	networkPrefixes []string
	customCommands  []BashCommand
}

// BashToolOption configures the built-in bash tool. See [WithBashTimeout] and
// [WithBashNetwork].
type BashToolOption func(*bashToolConfig)

// WithBashTimeout sets the maximum duration a single bash tool call may run before
// being cancelled. Default: 30 seconds.
func WithBashTimeout(d time.Duration) BashToolOption {
	return func(cfg *bashToolConfig) {
		cfg.timeout = d
	}
}

// WithBashNetwork enables outbound network access for the bash tool's sandbox,
// restricted to the given URL prefixes (scheme+host[+port][+path prefix]). Private IP
// ranges are always denied. When not called, network access is disabled entirely.
func WithBashNetwork(urlPrefixes ...string) BashToolOption {
	return func(cfg *bashToolConfig) {
		cfg.networkPrefixes = urlPrefixes
	}
}

// WithBashCustomCommands registers additional commands available to bash scripts,
// on top of go-bash's built-in command set. A custom command overrides a built-in
// of the same name. Use [github.com/mark3labs/go-bash/command.Define] to build a
// Command:
//
//	cmd := command.Define("mytool", func(ctx context.Context, args []string, c *command.Context) command.Result {
//	    return command.Result{Stdout: "...", ExitCode: 0}
//	})
func WithBashCustomCommands(cmds ...BashCommand) BashToolOption {
	return func(cfg *bashToolConfig) {
		cfg.customCommands = cmds
	}
}

const bashToolDescription = `Execute a bash script in a sandboxed environment.

When the agent's file store backs a real directory (i.e. it implements
DirBackedFileStore), the sandbox shares that directory's filesystem, so files
written via write_file are visible to bash (e.g. "cat /foo/bar.txt"), and files
created by bash scripts are visible to read_file/list_directory. Otherwise the
sandbox uses an isolated in-memory filesystem.

Network access is disabled by default; commands like curl will fail unless the
tool has been explicitly configured with allowed network destinations.

Each call runs with a bounded timeout; scripts that run too long are aborted.`

// NewBashTool creates the built-in "bash" tool, which executes a bash script inside a
// sandboxed environment (github.com/mark3labs/go-bash). By default the sandbox has no
// network access and a 30 second execution timeout; use [WithBashTimeout],
// [WithBashNetwork], and [WithBashCustomCommands] to change these defaults.
func NewBashTool(opts ...BashToolOption) Tool {
	cfg := bashToolConfig{
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return NewTypedCtxAwareToolFunc(
		"bash",
		bashToolDescription,
		map[string]*schema.ParameterInfo{
			"script": StringParam("Bash script to execute", true),
		},
		func(ctx context.Context, agentCtx AgentContext, args BashArgs) (string, error) {
			if args.Script == "" {
				return "", errs.NewErrf("script must not be empty")
			}

			sandbox, err := getOrCreateBashSandbox(agentCtx, cfg)
			if err != nil {
				return "", err
			}

			execCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
			defer cancel()

			result, err := sandbox.Exec(execCtx, args.Script, gobash.ExecOptions{})
			if err != nil {
				return "", errs.Wrapf(err, "bash execution failed")
			}

			return fmt.Sprintf("stdout:\n%s\nstderr:\n%s\nexit_code: %d", result.Stdout, result.Stderr, result.ExitCode), nil
		},
	)
}

// getOrCreateBashSandbox returns the cached *gobash.Bash sandbox for the current
// session, constructing and caching a new one on first use. agentCtx.bash is always
// set by Agent.Execute (the only way to reach this code, since agentCtxKey is
// unexported and external callers cannot inject their own AgentContext).
func getOrCreateBashSandbox(agentCtx AgentContext, cfg bashToolConfig) (*gobash.Bash, error) {
	holder := agentCtx.bash
	holder.mu.Lock()
	defer holder.mu.Unlock()
	if holder.sandbox != nil {
		return holder.sandbox, nil
	}

	bashOpts := gobash.BashOptions{
		Cwd: "/",
	}

	if dirStore, ok := agentCtx.Store.(DirBackedFileStore); ok {
		dir, err := dirStore.RootDir()
		if err != nil {
			return nil, errs.Wrapf(err, "failed to resolve file store directory for bash sandbox")
		}
		fsys, err := rwfs.New(rwfs.Options{Root: dir})
		if err != nil {
			return nil, errs.Wrapf(err, "failed to initialize bash sandbox filesystem")
		}
		bashOpts.FS = fsys
	}

	if len(cfg.networkPrefixes) > 0 {
		entries := make([]network.AllowedURLEntry, 0, len(cfg.networkPrefixes))
		for _, prefix := range cfg.networkPrefixes {
			entries = append(entries, network.AllowedURLEntry{URL: prefix})
		}
		bashOpts.Network = &network.Config{
			AllowedURLPrefixes: entries,
			DenyPrivateRanges:  true,
		}
	}

	bashOpts.CustomCommands = cfg.customCommands

	sandbox, err := gobash.New(bashOpts)
	if err != nil {
		return nil, errs.Wrapf(err, "failed to initialize bash sandbox")
	}

	holder.sandbox = sandbox
	return sandbox, nil
}
