package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/curtisnewbie/miso/flow"
)

// missingArgumentsErrorBody is the gateway rejection from the incident: a replayed
// assistant tool call lost its "function.arguments" key through omitempty.
const missingArgumentsErrorBody = `{"error":{"message":"If tool_calls are present in the message, function.arguments must be defined.","type":"invalid_request_error"}}`

// orphanToolResultErrorBody is the gateway rejection for a "tool" message that
// answers no preceding assistant tool call. Deliberately distinct from
// missingArgumentsErrorBody so a failure names the invariant that broke.
const orphanToolResultErrorBody = `{"error":{"message":"messages with role 'tool' must be a response to a preceding message with 'tool_calls'","type":"invalid_request_error"}}`

// toolCallsMissingArguments validates a request body the way the real gateway does:
// every message's every tool call must carry a "function.arguments" key.
func toolCallsMissingArguments(body string) []string {
	var req struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return []string{"unparseable request body: " + err.Error()}
	}
	var missing []string
	for i, msg := range req.Messages {
		raw, ok := msg["tool_calls"]
		if !ok {
			continue
		}
		var calls []struct {
			ID       string                     `json:"id"`
			Function map[string]json.RawMessage `json:"function"`
		}
		if err := json.Unmarshal(raw, &calls); err != nil {
			missing = append(missing, fmt.Sprintf("messages[%d]: unparseable tool_calls", i))
			continue
		}
		for _, call := range calls {
			if _, hasArgs := call.Function["arguments"]; !hasArgs {
				missing = append(missing, fmt.Sprintf("messages[%d] tool_call %q has no function.arguments", i, call.ID))
			}
		}
	}
	return missing
}

// orphanToolResults validates the other gateway invariant: a "tool" message must
// answer a tool call declared by a PRECEDING assistant message in the same body.
// A tool result whose call was dropped on replay is an orphan and is rejected.
func orphanToolResults(body string) []string {
	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		return []string{"unparseable request body: " + err.Error()}
	}
	declared := map[string]struct{}{}
	var orphans []string
	for i, m := range req.Messages {
		if m.Role == "assistant" {
			for _, call := range m.ToolCalls {
				declared[call.ID] = struct{}{}
			}
		}
		if m.Role != "tool" {
			continue
		}
		if _, ok := declared[m.ToolCallID]; !ok {
			orphans = append(orphans, fmt.Sprintf("messages[%d] tool result for %q has no preceding assistant tool call", i, m.ToolCallID))
		}
	}
	return orphans
}

// wireToolCalls returns the raw "tool_calls" value of every request message, nil
// where the message carries no "tool_calls" key at all.
func wireToolCalls(t *testing.T, body string) []json.RawMessage {
	t.Helper()
	var req struct {
		Messages []struct {
			Role      string          `json:"role"`
			ToolCalls json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	calls := make([]json.RawMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		calls = append(calls, m.ToolCalls)
	}
	return calls
}

// writeSSEStream emits frames as SSE data events followed by the [DONE] sentinel.
func writeSSEStream(w http.ResponseWriter, frames []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	for _, f := range frames {
		fmt.Fprintf(w, "data: %s\n\n", f)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// TestToolCallArgumentsReplay proves on the wire that a replayed assistant tool call
// keeps a "function.arguments" field, and that a nameless call is never replayed.
// The provider is a real SSE endpoint that validates each request like the gateway
// that produced the incident: it rejects with the incident's 400 when any replayed
// tool call lacks "function.arguments", and otherwise answers normally.
func TestToolCallArgumentsReplay(t *testing.T) {
	tests := []struct {
		name string
		// firstDelta is the tool_calls element streamed in request 1.
		firstDelta string
		// check asserts on the request-2 (post tool result) body.
		check func(t *testing.T, body string)
	}{
		{
			name:       "missing arguments is filled with an empty object",
			firstDelta: `{"index":0,"id":"call_1","type":"function","function":{"name":"query_faqs"}}`,
			check: func(t *testing.T, body string) {
				t.Helper()
				var calls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string  `json:"name"`
						Arguments *string `json:"arguments"`
					} `json:"function"`
				}
				for _, raw := range wireToolCalls(t, body) {
					if raw == nil {
						continue
					}
					if calls != nil {
						t.Fatalf("expected tool_calls on a single message, got more: %s", body)
					}
					if err := json.Unmarshal(raw, &calls); err != nil {
						t.Fatalf("unmarshal tool_calls: %v", err)
					}
				}
				if len(calls) != 1 {
					t.Fatalf("expected exactly 1 replayed tool call, got %d (%s)", len(calls), body)
				}
				call := calls[0]
				if call.ID != "call_1" || call.Type != "function" || call.Function.Name != "query_faqs" {
					t.Fatalf("expected the id/type/name preserved, got %+v", call)
				}
				if call.Function.Arguments == nil {
					t.Fatalf("replayed tool call has no function.arguments: %s", body)
				}
				if *call.Function.Arguments != "{}" {
					t.Fatalf("expected arguments %q, got %q", "{}", *call.Function.Arguments)
				}
				if !strings.Contains(body, `"arguments":"{}"`) {
					t.Fatalf("expected the wire body to carry \"arguments\":\"{}\", got %s", body)
				}
			},
		},
		{
			name:       "nameless tool call is not replayed",
			firstDelta: `{"index":0,"id":"call_2","type":"function","function":{"name":""}}`,
			check: func(t *testing.T, body string) {
				t.Helper()
				for i, raw := range wireToolCalls(t, body) {
					if raw != nil {
						t.Fatalf("messages[%d] still carries tool_calls %s: %s", i, raw, body)
					}
				}
				// The graph's synthetic result for the dropped call must be gone too,
				// otherwise the gateway rejects the orphan tool message.
				var req struct {
					Messages []struct {
						Role       string `json:"role"`
						ToolCallID string `json:"tool_call_id"`
						Content    string `json:"content"`
					} `json:"messages"`
				}
				if err := json.Unmarshal([]byte(body), &req); err != nil {
					t.Fatalf("unmarshal request: %v", err)
				}
				for i, m := range req.Messages {
					if m.Role == "tool" || m.ToolCallID == "call_2" {
						t.Fatalf("messages[%d] still carries the dropped call's tool result (%+v): %s", i, m, body)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies []string

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				bodies = append(bodies, string(body))
				n := len(bodies)
				mu.Unlock()

				if n == 1 {
					// The incident shape: the tool call delta has no arguments key at all,
					// and the tool name is not registered.
					writeSSEStream(w, []string{
						fmt.Sprintf(`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[%s]},"finish_reason":null}]}`, tt.firstDelta),
						`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
					})
					return
				}

				if missing := toolCallsMissingArguments(string(body)); len(missing) > 0 {
					t.Logf("provider rejected request %d (missing arguments): %v", n, missing)
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(missingArgumentsErrorBody))
					return
				}
				if orphans := orphanToolResults(string(body)); len(orphans) > 0 {
					t.Logf("provider rejected request %d (orphan tool result): %v", n, orphans)
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(orphanToolResultErrorBody))
					return
				}

				writeSSEStream(w, []string{
					`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"recovered"},"finish_reason":null}]}`,
					`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				})
			}))
			defer srv.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			rail := flow.NewRail(ctx)

			agent, err := NewAgent(AgentConfig{
				Name:           "tool-call-arguments-e2e",
				ModelName:      "gpt-4o-mini",
				ApiKey:         "k",
				ApiUrl:         srv.URL,
				MaxRunSteps:    5,
				EnableFileTool: new(bool),
				Tools:          []Tool{&countingTool{}},
			}, ctx)
			if err != nil {
				t.Fatalf("NewAgent: %v", err)
			}

			out, err := agent.Execute(rail, AgentRequest{UserInput: "do the thing"})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if out.Response != "recovered" {
				t.Fatalf("expected the recovered response, got %q", out.Response)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != 2 {
				t.Fatalf("expected exactly 2 provider requests (tool call + follow-up, no retry), got %d", len(bodies))
			}
			t.Logf("request 2 body: %s", bodies[1])
			tt.check(t, bodies[1])
		})
	}
}
