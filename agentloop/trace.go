package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/flow"
)

// ToolEventKind identifies the kind of tool event emitted during agent execution.
type ToolEventKind string

const (
	// ToolEventKindCall fires when the LLM invokes a tool, before execution begins.
	ToolEventKindCall ToolEventKind = "call"
	// ToolEventKindResult fires after a tool finishes execution, with its result.
	ToolEventKindResult ToolEventKind = "result"
)

// tokenAccumulator collects cumulative token usage across all LLM calls in one execution.
type tokenAccumulator struct {
	mu               sync.Mutex
	promptTokens     int
	completionTokens int
	cachedTokens     int
}

func (a *tokenAccumulator) add(prompt, completion, cached int) {
	a.mu.Lock()
	a.promptTokens += prompt
	a.completionTokens += completion
	a.cachedTokens += cached
	a.mu.Unlock()
}

func (a *tokenAccumulator) snapshot() TokenUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return TokenUsage{
		PromptTokens:     a.promptTokens,
		CompletionTokens: a.completionTokens,
		CachedTokens:     a.cachedTokens,
	}
}

// ToolEvent is emitted during agent execution for each tool invocation.
// If ToolEventCallback is set in AgentConfig, it is called synchronously for each event.
type ToolEvent struct {
	Kind ToolEventKind
	Name string // tool name
	Args string // raw JSON args string
}

// withAgentTraceCallback builds a trace callback for the AgentLoop graph.
// It extends the generic graph.WithTraceCallback with tool-specific logging:
// file paths for read/write/edit/list_directory/add_artifact, glob patterns,
// and todo item details for add_todo/update_todo/delete_todo.
func withAgentTraceCallback(name string, ops agentOps, acc *tokenAccumulator) compose.Option {
	return compose.WithCallbacks(buildTraceHandler(name, ops, acc))
}

func buildTraceHandler(name string, ops agentOps, acc *tokenAccumulator) callbacks.Handler {
	b := callbacks.NewHandlerBuilder()
	if ops.logOnStart || ops.toolEventCallback != nil {
		b = b.OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
			rail := flow.NewRail(ctx)
			if ri.Component == "Tool" {
				if ops.logOnStart {
					logToolStart(rail, name, ri, in)
				}
				if ops.toolEventCallback != nil {
					ci := einotool.ConvCallbackInput(in)
					args := ""
					if ci != nil {
						args = ci.ArgumentsInJSON
					}
					ops.toolEventCallback(ToolEvent{
						Kind: ToolEventKindCall,
						Name: ri.Name,
						Args: args,
					})
					return context.WithValue(ctx, toolArgsCtxKey, args)
				}
			} else if ri.Component == "ChatModel" {
				if ri.Name == "" {
					// skip inner component-level callback; node-level fires separately
					return ctx
				}
				if ops.logInputs {
					logChatModelInput(rail, name, ri, in)
				} else {
					rail.Infof("[%v] %v/%v start", name, ri.Component, ri.Name)
				}

			} else {
				if ops.logInputs {
					rail.Infof("Graph exec %v start, name: %v, type: %v, component: %v, input: %v", name, ri.Name, ri.Type, ri.Component, in)
				} else {
					rail.Infof("Graph exec %v start, name: %v, type: %v, component: %v", name, ri.Name, ri.Type, ri.Component)
				}
			}
			return ctx
		})
	}
	if ops.toolEventCallback != nil || ops.logOnEnd || acc != nil {
		b = b.OnEndFn(func(ctx context.Context, ri *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			if ops.toolEventCallback != nil && ri.Component == "Tool" {
				args, _ := ctx.Value(toolArgsCtxKey).(string)
				ops.toolEventCallback(ToolEvent{
					Kind: ToolEventKindResult,
					Name: ri.Name,
					Args: args,
				})
			}
			if ri.Component == "ChatModel" {
				if ri.Name == "" {
					// skip inner component-level callback; node-level fires separately
					return ctx
				}
				inToken, outToken, cachedToken, ok := agentTokenUsage(output)
				if ok && acc != nil {
					acc.add(inToken, outToken, cachedToken)
				}
				if ops.logOnEnd {
					rail := flow.NewRail(ctx)
					if ok {
						msg := fmt.Sprintf("[%v] %v/%v — in: %v tokens", name, ri.Component, ri.Name, inToken)
						if ops.maxTokens > 0 {
							msg += fmt.Sprintf(" (ctx: %.1f%%, max: %v)", float64(inToken)*100.0/float64(ops.maxTokens), ops.maxTokens)
						}
						msg += fmt.Sprintf(", out: %v tokens", outToken)
						if cachedToken > 0 && inToken > 0 {
							msg += fmt.Sprintf(", cache hit: %v (%.1f%%)", cachedToken, float64(cachedToken)*100.0/float64(inToken))
						}
						rail.Infof("%s", msg)
					} else {
						rail.Infof("[%v] %v/%v done", name, ri.Component, ri.Name)
					}
				}
				if ops.logOnEnd && ops.logOutputs {
					msg := agentExtractMessage(output)
					if msg != nil {
						rail := flow.NewRail(ctx)
						if msg.Content != "" {
							rail.Infof("[%v] %v/%v output: %v", name, ri.Component, ri.Name, msg.Content)
						}
						if msg.ReasoningContent != "" {
							rail.Infof("[%v] %v/%v reasoning:\n%v", name, ri.Component, ri.Name, msg.ReasoningContent)
						}
					}
				}
			}
			return ctx
		})
	}
	return b.Build()
}

// logChatModelInput logs each input message sent to the ChatModel.
// Tool-call-only messages (empty content) are summarized as "<tool_calls: name1, name2>".
// Tool result messages (role=tool) are trimmed to first/last 30 runes with total length.
func logChatModelInput(rail flow.Rail, graphName string, ri *callbacks.RunInfo, in callbacks.CallbackInput) {
	ci := model.ConvCallbackInput(in)
	if ci == nil {
		rail.Infof("[%v] %v/%v start", graphName, ri.Component, ri.Name)
		return
	}
	if len(ci.Messages) == 0 {
		rail.Infof("[%v] %v/%v start (no messages)", graphName, ri.Component, ri.Name)
		return
	}
	parts := make([]string, 0, len(ci.Messages))
	for _, msg := range ci.Messages {
		content := msg.Content
		if len(msg.ToolCalls) > 0 {
			tcNames := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				tcNames = append(tcNames, tc.Function.Name)
			}
			toolSummary := "<tool_calls: " + strings.Join(tcNames, ", ") + ">"
			if content == "" {
				content = toolSummary
			} else {
				content = content + " " + toolSummary
			}
		}
		if msg.Role == schema.Tool {
			content = trimLogContent(content, 30)
		}
		parts = append(parts, fmt.Sprintf("<%v>\n%v\n</%v>", msg.Role, content, msg.Role))
	}
	rail.Infof("[%v] %v/%v inputs:\n%v", graphName, ri.Component, ri.Name, strings.Join(parts, "\n"))
}

// trimLogContent trims s to at most head+tail runes, inserting a middle summary.
// Format: "<first head runes>...<last tail runes> (len=N)"
func trimLogContent(s string, n int) string {
	runes := []rune(s)
	total := len(runes)
	if total <= n*2 {
		return s
	}
	head := string(runes[:n])
	tail := string(runes[total-n:])
	return head + "..." + tail + " (len=" + fmt.Sprintf("%d", total) + ")"
}

// logToolStart logs tool-specific details for known builtin tools.
func logToolStart(rail flow.Rail, graphName string, ri *callbacks.RunInfo, in callbacks.CallbackInput) {
	ci := einotool.ConvCallbackInput(in)
	if ci == nil {
		rail.Infof("[%v] Tool/%v called", graphName, ri.Name)
		return
	}
	argsJSON := ci.ArgumentsInJSON

	switch ri.Name {
	case "read_file":
		path := extractJSONStringField(argsJSON, "path")
		offset := extractJSONIntField(argsJSON, "offset")
		limit := extractJSONIntField(argsJSON, "limit")
		switch {
		case offset > 0 && limit > 0:
			rail.Infof("[%v] Tool/read_file — path: %v, offset: %v, limit: %v", graphName, path, offset, limit)
		case offset > 0:
			rail.Infof("[%v] Tool/read_file — path: %v, offset: %v", graphName, path, offset)
		case limit > 0:
			rail.Infof("[%v] Tool/read_file — path: %v, limit: %v", graphName, path, limit)
		default:
			rail.Infof("[%v] Tool/read_file — path: %v", graphName, path)
		}

	case "write_file":
		path := extractJSONStringField(argsJSON, "path")
		contentLen := extractJSONStringLen(argsJSON, "content")
		rail.Infof("[%v] Tool/write_file — path: %v, content_len: %v", graphName, path, contentLen)

	case "edit_file":
		path := extractJSONStringField(argsJSON, "path")
		replaceAll := extractJSONBoolField(argsJSON, "replace_all")
		if replaceAll {
			rail.Infof("[%v] Tool/edit_file — path: %v, replace_all: true", graphName, path)
		} else {
			rail.Infof("[%v] Tool/edit_file — path: %v", graphName, path)
		}

	case "list_directory", "add_artifact":
		path := extractJSONStringField(argsJSON, "path")
		rail.Infof("[%v] Tool/%v — path: %v", graphName, ri.Name, path)

	case "glob":
		pattern := extractJSONStringField(argsJSON, "pattern")
		rail.Infof("[%v] Tool/glob — pattern: %v", graphName, pattern)

	case "add_todo":
		tasks := extractTodoTasks(argsJSON)
		rail.Infof("[%v] Tool/add_todo — todos: [%v]", graphName, strings.Join(tasks, ", "))

	case "update_todo":
		id := extractJSONStringField(argsJSON, "id")
		status := extractJSONStringField(argsJSON, "status")
		rail.Infof("[%v] Tool/update_todo — id: %v, status: %v", graphName, id, status)

	case "delete_todo":
		ids := extractJSONStringSliceField(argsJSON, "ids")
		rail.Infof("[%v] Tool/delete_todo — ids: [%v]", graphName, strings.Join(ids, ", "))

	case "task":
		agentName := extractJSONStringField(argsJSON, "agent_name")
		task := trimLogContent(extractJSONStringField(argsJSON, "task"), 80)
		rail.Infof("[%v] Tool/task — agent: %v, task: %v", graphName, agentName, task)

	case "think_tool":
		reflection := extractJSONStringField(argsJSON, "reflection")
		rail.Infof("[%v] Tool/think_tool — reflection: %v", graphName, reflection)

	case "transform_csv_lua":
		inputPath := extractJSONStringField(argsJSON, "input_path")
		script := extractJSONStringField(argsJSON, "script")
		outputPath := extractJSONStringField(argsJSON, "output_path")
		if outputPath != "" {
			rail.Infof("[%v] Tool/transform_csv_lua — input_path: %v, output_path: %v, script: \n%v\n", graphName, inputPath, outputPath, script)
		} else {
			rail.Infof("[%v] Tool/transform_csv_lua — input_path: %v, script: \n%v\n", graphName, inputPath, script)
		}

	case "tavily_search":
		query := extractJSONStringField(argsJSON, "query")
		timeRange := extractJSONStringField(argsJSON, "time_range")
		topic := extractJSONStringField(argsJSON, "topic")
		msg := fmt.Sprintf("[%v] Tool/tavily_search — query: %v", graphName, query)
		if timeRange != "" {
			msg += fmt.Sprintf(", time_range: %v", timeRange)
		}
		if topic != "" {
			msg += fmt.Sprintf(", topic: %v", topic)
		}
		rail.Infof("%s", msg)

	case "tavily_extract":
		urls := extractJSONStringSliceField(argsJSON, "urls")
		query := extractJSONStringField(argsJSON, "query")
		msg := fmt.Sprintf("[%v] Tool/tavily_extract — urls: [%v]", graphName, strings.Join(urls, ", "))
		if query != "" {
			msg += fmt.Sprintf(", query: %v", query)
		}
		rail.Infof("%s", msg)

	default:
		rail.Infof("[%v] Tool/%v called", graphName, ri.Name)
	}
}

// extractJSONStringField extracts a top-level string field from a JSON object string.
// Returns empty string if parsing fails or field is missing.
func extractJSONStringField(jsonStr, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// extractJSONStringSliceField extracts a top-level []string field from a JSON object string.
func extractJSONStringSliceField(jsonStr, field string) []string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return nil
	}
	raw, ok := m[field]
	if !ok {
		return nil
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err != nil {
		return nil
	}
	return ss
}

// extractJSONIntField extracts a top-level int field from a JSON object string.
// Returns 0 if parsing fails or field is missing.
func extractJSONIntField(jsonStr, field string) int {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return 0
	}
	raw, ok := m[field]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// extractJSONBoolField extracts a top-level bool field from a JSON object string.
// Returns false if parsing fails or field is missing.
func extractJSONBoolField(jsonStr, field string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return false
	}
	raw, ok := m[field]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// extractJSONStringLen extracts the byte length of a top-level string field
// from a JSON object string. Returns 0 if parsing fails or field is missing.
func extractJSONStringLen(jsonStr, field string) int {
	return len(extractJSONStringField(jsonStr, field))
}

// extractTodoTasks parses the add_todo args JSON and returns task names.
func extractTodoTasks(jsonStr string) []string {
	var args struct {
		Todos []struct {
			Task string `json:"task"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &args); err != nil {
		return nil
	}
	tasks := make([]string, 0, len(args.Todos))
	for _, t := range args.Todos {
		tasks = append(tasks, t.Task)
	}
	return tasks
}

// agentTokenUsage extracts token usage from a callback output.
func agentTokenUsage(in callbacks.CallbackOutput) (_in int, _out int, _cached int, ok bool) {
	switch m := in.(type) {
	case *model.CallbackOutput:
		if m.TokenUsage != nil {
			return m.TokenUsage.PromptTokens, m.TokenUsage.CompletionTokens, m.TokenUsage.PromptTokenDetails.CachedTokens, true
		}
	}
	return 0, 0, 0, false
}

// agentExtractMessage extracts a schema.Message from a callback output.
func agentExtractMessage(in callbacks.CallbackOutput) *schema.Message {
	switch m := in.(type) {
	case *model.CallbackOutput:
		if m == nil {
			return nil
		}
		return m.Message
	case *schema.Message:
		return m
	}
	return nil
}
