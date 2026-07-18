package agentloop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/curtisnewbie/miso/flow"
	"github.com/mark3labs/go-bash/command"
)

func TestBashTool_BasicExecution(t *testing.T) {
	ctx := context.Background()
	tool := NewBashTool()

	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "echo hello"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("bash execution failed: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected output to contain %q, got %q", "hello", result)
	}
	if !strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected exit_code: 0, got %q", result)
	}
}

func TestBashTool_NonZeroExit(t *testing.T) {
	ctx := context.Background()
	tool := NewBashTool()

	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "exit 1"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("expected no Go error for non-zero exit script, got: %v", err)
	}
	if !strings.Contains(result, "exit_code: 1") {
		t.Errorf("expected exit_code: 1, got %q", result)
	}
}

func TestBashTool_SharedFileStore(t *testing.T) {
	ctx := context.Background()
	store := newTestMemFileStore()
	defer store.OnSessionEnd(flow.NewRail(ctx))

	if err := store.WriteFile(ctx, "/greeting.txt", []byte("hi there")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	tool := NewBashTool()
	agentCtx := AgentContext{Store: store, Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "cat /greeting.txt"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("bash execution failed: %v", err)
	}
	if !strings.Contains(result, "hi there") {
		t.Errorf("expected output to contain %q, got %q", "hi there", result)
	}
}

func TestBashTool_SandboxReusedAcrossCallsInSession(t *testing.T) {
	ctx := context.Background()
	tool := NewBashTool()

	// Setting the unexported bash holder directly (same package as tests) mirrors
	// what Agent.Execute does — a single holder shared across bash tool calls within
	// one session, enabling the sandbox (and its in-memory FS) to persist between calls.
	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	writeArgs, _ := json.Marshal(map[string]interface{}{"script": "mkdir -p /work && echo persisted > /work/f.txt"})
	if _, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(writeArgs)); err != nil {
		t.Fatalf("first bash call failed: %v", err)
	}

	readArgs, _ := json.Marshal(map[string]interface{}{"script": "cat /work/f.txt"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(readArgs))
	if err != nil {
		t.Fatalf("second bash call failed: %v", err)
	}
	if !strings.Contains(result, "persisted") {
		t.Errorf("expected sandbox to be reused across calls (file from first call visible), got %q", result)
	}
}

func TestBashTool_NetworkDisabledByDefault(t *testing.T) {
	ctx := context.Background()
	tool := NewBashTool()

	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "curl https://example.com"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("bash execution failed: %v", err)
	}
	if !strings.Contains(result, "network disabled") {
		t.Errorf("expected output to indicate network disabled, got %q", result)
	}
	if !strings.Contains(result, "exit_code: 2") {
		t.Errorf("expected exit_code: 2, got %q", result)
	}
}

func TestBashTool_TimeoutEnforcement(t *testing.T) {
	ctx := context.Background()
	tool := NewBashTool(WithBashTimeout(200 * time.Millisecond))

	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "sleep 5"})

	start := time.Now()
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("expected timeout to bound execution under 2s, took %v", elapsed)
	}
	// go-bash's `sleep` observes ctx cancellation and returns exit code 130
	// (like a SIGINT) as a normal command result rather than a Go error.
	if err == nil && !strings.Contains(result, "exit_code: 130") {
		t.Errorf("expected either an error or exit_code: 130 indicating timeout, got err=%v result=%q", err, result)
	}
}

func TestBashTool_CustomCommand(t *testing.T) {
	ctx := context.Background()
	greet := command.Define("greet", func(ctx context.Context, args []string, c *command.Context) command.Result {
		return command.Result{Stdout: "hi from custom command\n", ExitCode: 0}
	})
	tool := NewBashTool(WithBashCustomCommands(greet))

	agentCtx := AgentContext{Metadata: NewMetadataStore(), bash: &bashSandboxHolder{}}
	ctx = context.WithValue(ctx, agentCtxKey, agentCtx)

	args, _ := json.Marshal(map[string]interface{}{"script": "greet"})
	result, err := tool.(SelfInvokeTool).ExecuteJson(ctx, string(args))
	if err != nil {
		t.Fatalf("bash execution failed: %v", err)
	}
	if !strings.Contains(result, "hi from custom command") {
		t.Errorf("expected custom command output, got %q", result)
	}
	if !strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected exit_code: 0, got %q", result)
	}
}

func TestTmpFileStore_PathMirroring(t *testing.T) {
	ctx := context.Background()
	store := newTestMemFileStore()
	defer store.OnSessionEnd(flow.NewRail(ctx))

	if err := store.WriteFile(ctx, "/a/b.txt", []byte("data")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	dir, err := store.RootDir()
	if err != nil {
		t.Fatalf("RootDir failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "a/b.txt"))
	if err != nil {
		t.Fatalf("failed to read mirrored file: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("expected %q, got %q", "data", string(got))
	}
}
