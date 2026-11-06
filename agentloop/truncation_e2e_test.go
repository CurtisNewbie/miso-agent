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

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/flow"
)

// countingTool records executions so the test can prove a truncated tool call was
// never dispatched.
type countingTool struct {
	mu    sync.Mutex
	calls int
}

func (c *countingTool) Name() string        { return "counter" }
func (c *countingTool) Description() string { return "counts executions" }
func (c *countingTool) Parameters() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"x": {Type: schema.Integer, Desc: "ignored", Required: false},
	}
}
func (c *countingTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return "executed", nil
}

func (c *countingTool) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestTruncatedToolCallsAreNotExecuted drives the real graph end to end: the first
// provider response carries a tool call with finish_reason "length", the second one
// answers normally. It asserts (a) the tool never ran, (b) the retry actually happened,
// and (c) the transcript keeps the assistant tool_calls paired with an error tool result,
// which is what the provider requires and what tells the model to re-issue the call.
func TestTruncatedToolCallsAreNotExecuted(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		requests++
		n := requests
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		var frames []string
		if n == 1 {
			// Truncated tool call: arguments stop mid-JSON, finish_reason "length".
			frames = []string{
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"counter","arguments":"{\"x\":"}}]},"finish_reason":null}]}`,
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			}
		} else {
			frames = []string{
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"recovered"},"finish_reason":null}]}`,
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				`{"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22}}`,
			}
		}
		for _, f := range frames {
			fmt.Fprintf(w, "data: %s\n\n", f)
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rail := flow.NewRail(ctx)

	tool := &countingTool{}
	agent, err := NewAgent(AgentConfig{
		Name:           "truncation-e2e",
		ModelName:      "gpt-4o-mini",
		ApiKey:         "k",
		ApiUrl:         srv.URL,
		MaxRunSteps:    5,
		EnableFileTool: new(bool),
		Tools:          []Tool{tool},
	}, ctx)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	out, err := agent.Execute(rail, AgentRequest{UserInput: "do the thing"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := tool.count(); got != 0 {
		t.Fatalf("truncated tool call must not execute, tool ran %d time(s)", got)
	}
	if out.Response != "recovered" {
		t.Fatalf("expected the retried response, got %q", out.Response)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("expected exactly 2 provider requests (truncated + retry), got %d", requests)
	}

	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    any    `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &req); err != nil {
		t.Fatalf("unmarshal retry request: %v", err)
	}

	var assistantCallID string
	var toolResultContent string
	for _, m := range req.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			assistantCallID = m.ToolCalls[0].ID
		}
		if m.Role == "tool" {
			toolResultContent, _ = m.Content.(string)
			if m.ToolCallID != assistantCallID {
				t.Fatalf("tool result %q does not match assistant tool call %q", m.ToolCallID, assistantCallID)
			}
		}
	}
	if assistantCallID != "call_1" {
		t.Fatalf("expected the assistant tool call to be replayed, got id %q", assistantCallID)
	}
	if !strings.Contains(toolResultContent, "was not executed") || !strings.Contains(toolResultContent, `"counter"`) {
		t.Fatalf("expected an actionable error tool result naming the tool, got %q", toolResultContent)
	}
}

// TestRepeatedTruncationStopsAtMaxRunSteps proves a model that keeps producing
// truncated tool calls cannot spin forever: refusals keep the loop going but
// MaxRunSteps still bounds it, and no tool is ever executed.
func TestRepeatedTruncationStopsAtMaxRunSteps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"counter\",\"arguments\":\"{\\\"x\\\":\"}}]},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rail := flow.NewRail(ctx)

	tool := &countingTool{}
	agent, err := NewAgent(AgentConfig{
		Name:           "truncation-loop",
		ModelName:      "gpt-4o-mini",
		ApiKey:         "k",
		ApiUrl:         srv.URL,
		MaxRunSteps:    2,
		EnableFileTool: new(bool),
		Tools:          []Tool{tool},
	}, ctx)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	if _, err := agent.Execute(rail, AgentRequest{UserInput: "loop"}); err == nil {
		t.Fatal("expected Execute to fail once MaxRunSteps is exhausted")
	} else if !strings.Contains(err.Error(), "max run steps") {
		t.Fatalf("expected a max run steps error, got: %v", err)
	}
	if got := tool.count(); got != 0 {
		t.Fatalf("truncated tool calls must never execute, tool ran %d time(s)", got)
	}
}

// TestRateLimitStillRetries proves the new non-retryable classification did not swallow
// transient failures: a 429 must still be retried and the run must succeed.
func TestRateLimitStillRetries(t *testing.T) {
	var mu sync.Mutex
	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()

		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit reached for requests","type":"requests","code":"rate_limit_exceeded"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rail := flow.NewRail(ctx)

	agent, err := NewAgent(AgentConfig{
		Name:           "rate-limit-retry",
		ModelName:      "gpt-4o-mini",
		ApiKey:         "k",
		ApiUrl:         srv.URL,
		MaxRunSteps:    2,
		EnableFileTool: new(bool),
	}, ctx)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	out, err := agent.Execute(rail, AgentRequest{UserInput: "hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Response != "ok" {
		t.Fatalf("expected the retried response %q, got %q", "ok", out.Response)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("expected 1 rate-limited request + 1 retry, got %d", requests)
	}
}
