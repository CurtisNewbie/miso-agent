package agentloop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/curtisnewbie/miso/flow"
)

// TestAgentRetryCount verifies that AgentConfig.ModelRetry is plumbed through to the
// model's retry option. The model endpoint is a stub that fails every request with
// HTTP 500, so each attempt is a distinct observable HTTP request and the retry layer
// is the only thing that can issue more than one.
func TestAgentRetryCount(t *testing.T) {
	t.Run("explicit ModelRetry of 1 gives exactly one retry", func(t *testing.T) {
		var requests int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requests, 1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"stub model failure"}}`))
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		rail := flow.NewRail(ctx)

		agent, err := NewAgent(AgentConfig{
			Name:       "retry-explicit-one",
			ModelName:  "qwen3-max",
			ApiKey:     "test-key",
			ApiUrl:     srv.URL,
			ModelRetry: 1,
		}, ctx)
		if err != nil {
			t.Fatalf("NewAgent() returned err: %v", err)
		}

		start := time.Now()
		if _, err := agent.Execute(rail, AgentRequest{UserInput: "hello"}); err == nil {
			t.Fatalf("expected Execute() to fail because the model endpoint always errors")
		}
		t.Logf("ModelRetry=1: finished in %v after %d model request(s)", time.Since(start), atomic.LoadInt32(&requests))

		if got := atomic.LoadInt32(&requests); got != 2 {
			t.Fatalf("expected exactly 2 model requests (1 initial + 1 retry), got %d", got)
		}
	})

	t.Run("unset ModelRetry keeps the library default", func(t *testing.T) {
		var requests int32
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Once 3 attempts have been observed, the default retry budget (5) is
			// provably in force (> 2 attempts). Cancelling here avoids sleeping
			// through the whole 1s/2s/4s/8s/10s backoff schedule.
			if atomic.AddInt32(&requests, 1) >= 3 {
				cancel()
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"stub model failure"}}`))
		}))
		defer srv.Close()

		rail := flow.NewRail(ctx)
		agent, err := NewAgent(AgentConfig{
			Name:      "retry-default",
			ModelName: "qwen3-max",
			ApiKey:    "test-key",
			ApiUrl:    srv.URL,
			// ModelRetry deliberately left unset: the library default applies.
		}, ctx)
		if err != nil {
			t.Fatalf("NewAgent() returned err: %v", err)
		}

		start := time.Now()
		_, _ = agent.Execute(rail, AgentRequest{UserInput: "hello"})
		t.Logf("ModelRetry unset: finished in %v after %d model request(s)", time.Since(start), atomic.LoadInt32(&requests))

		if got := atomic.LoadInt32(&requests); got <= 2 {
			t.Fatalf("expected the default retry budget to exceed 2 attempts, got %d", got)
		}
	})
}
