package agentloop

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/flow"
)

// withAgentTraceCallback builds a trace callback for the AgentLoop graph.
// It extends the generic graph.WithTraceCallback with tool-specific logging:
// file paths for read/write/edit/list_directory/add_artifact, glob patterns,
// and todo item details for add_todo/update_todo/delete_todo.
func withAgentTraceCallback(name string, ops agentOps) compose.Option {
	b := callbacks.NewHandlerBuilder()
	if ops.logOnStart {
		b = b.OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
			rail := flow.NewRail(ctx)
			if ri.Component == "Tool" {
				logToolStart(rail, name, ri, in)
			} else if ri.Component == "ChatModel" {
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
	if ops.logOnEnd {
		b = b.OnEndFn(func(ctx context.Context, ri *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			rail := flow.NewRail(ctx)
			inToken, outToken, ok := agentTokenUsage(output)
			if ok {
				rail.Infof("[%v] %v/%v — in: %v tokens, out: %v tokens", name, ri.Component, ri.Name, inToken, outToken)
			}
			if ops.logOutputs {
				if ri.Component == "ChatModel" {
					msg := agentExtractMessage(output)
					if msg != nil {
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
	return compose.WithCallbacks(b.Build())
}

// logChatModelInput logs each input message sent to the ChatModel.
// Tool-call-only messages (empty content) are summarized as "<tool_calls: name1, name2>".
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
	for i, msg := range ci.Messages {
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
		rail.Infof("[%v] %v/%v input[%v] [%v]: %v", graphName, ri.Component, ri.Name, i, msg.Role, content)
	}
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

	case "think_tool":
		reflection := extractJSONStringField(argsJSON, "reflection")
		rail.Infof("[%v] Tool/think_tool — reflection: %v", graphName, reflection)

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
func agentTokenUsage(in callbacks.CallbackOutput) (_in int, _out int, ok bool) {
	switch m := in.(type) {
	case *model.CallbackOutput:
		if m.TokenUsage != nil {
			return m.TokenUsage.PromptTokens, m.TokenUsage.CompletionTokens, true
		}
	}
	return 0, 0, false
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
