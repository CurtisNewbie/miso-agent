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
	"github.com/curtisnewbie/miso-agent/graph"
	"github.com/curtisnewbie/miso/flow"
)

// withAgentTraceCallback builds a trace callback for the AgentLoop graph.
// It extends the generic graph.WithTraceCallback with tool-specific logging:
// file paths for read/write/edit/list_directory/add_artifact, glob patterns,
// and todo item details for add_todo/update_todo/delete_todo.
func withAgentTraceCallback(name string, genops *graph.GenericOps) compose.Option {
	b := callbacks.NewHandlerBuilder()
	if genops.LogOnStart {
		b = b.OnStartFn(func(ctx context.Context, ri *callbacks.RunInfo, in callbacks.CallbackInput) context.Context {
			rail := flow.NewRail(ctx)
			if ri.Component == "Tool" {
				logToolStart(rail, name, ri, in)
			} else {
				if genops.LogInputs {
					rail.Infof("Graph exec %v start, name: %v, type: %v, component: %v, input: %v", name, ri.Name, ri.Type, ri.Component, in)
				} else {
					rail.Infof("Graph exec %v start, name: %v, type: %v, component: %v", name, ri.Name, ri.Type, ri.Component)
				}
			}
			return ctx
		})
	}
	if genops.LogOnEnd {
		b = b.OnEndFn(func(ctx context.Context, ri *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			rail := flow.NewRail(ctx)
			inToken, outToken, ok := agentTokenUsage(output)
			if ok {
				rail.Infof("[%v] %v/%v — in: %v tokens, out: %v tokens", name, ri.Component, ri.Name, inToken, outToken)
			}
			if genops.LogOutputs {
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

// logToolStart logs tool-specific details for known builtin tools.
func logToolStart(rail flow.Rail, graphName string, ri *callbacks.RunInfo, in callbacks.CallbackInput) {
	ci := einotool.ConvCallbackInput(in)
	if ci == nil {
		rail.Infof("[%v] Tool/%v called", graphName, ri.Name)
		return
	}
	argsJSON := ci.ArgumentsInJSON

	switch ri.Name {
	case "read_file", "write_file", "edit_file", "list_directory", "add_artifact":
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
