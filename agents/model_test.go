package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var errFakeModel = errors.New("fake model failure")

// failingChatModel is a model.ToolCallingChatModel whose Generate and Stream
// always fail. It counts invocations so retry behaviour can be asserted at the
// model boundary. A real HTTP server cannot be used for the already-done-context
// case: the request never leaves the client, so no hit can be observed there.
type failingChatModel struct {
	generateCalls int
	streamCalls   int
}

func (f *failingChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.generateCalls++
	return nil, errFakeModel
}

func (f *failingChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.streamCalls++
	return nil, errFakeModel
}

func (f *failingChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

func TestContextAwareRetry(t *testing.T) {
	// A dead context must fail fast: the retry gap function refuses to retry when
	// ctx is already done, so exactly one attempt is made and no backoff is slept.
	// The first backoff the old code would have slept is 1s, so anything well under
	// that proves the gating works.
	const cancelledMaxElapsed = 200 * time.Millisecond

	tests := []struct {
		name       string
		cancelCtx  bool
		invoke     func(r *retryChatModel, ctx context.Context) error
		calls      func(f *failingChatModel) int
		wantCalls  int
		maxElapsed time.Duration
	}{
		{
			name:      "generate with already-done context attempts once",
			cancelCtx: true,
			invoke: func(r *retryChatModel, ctx context.Context) error {
				_, err := r.Generate(ctx, nil)
				return err
			},
			calls:      func(f *failingChatModel) int { return f.generateCalls },
			wantCalls:  1,
			maxElapsed: cancelledMaxElapsed,
		},
		{
			name:      "stream with already-done context attempts once",
			cancelCtx: true,
			invoke: func(r *retryChatModel, ctx context.Context) error {
				// Stream now delivers failures on the returned stream, not synchronously.
				sr, err := r.Stream(ctx, nil)
				if err != nil {
					return err
				}
				_, err = readStream(sr)
				return err
			},
			calls:      func(f *failingChatModel) int { return f.streamCalls },
			wantCalls:  1,
			maxElapsed: cancelledMaxElapsed,
		},
		{
			name:      "generate with live context still retries once",
			cancelCtx: false,
			invoke: func(r *retryChatModel, ctx context.Context) error {
				_, err := r.Generate(ctx, nil)
				return err
			},
			calls:     func(f *failingChatModel) int { return f.generateCalls },
			wantCalls: 2,
		},
		{
			name:      "stream with live context still retries once",
			cancelCtx: false,
			invoke: func(r *retryChatModel, ctx context.Context) error {
				// Stream now delivers failures on the returned stream, not synchronously.
				sr, err := r.Stream(ctx, nil)
				if err != nil {
					return err
				}
				_, err = readStream(sr)
				return err
			},
			calls:     func(f *failingChatModel) int { return f.streamCalls },
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &failingChatModel{}
			r := &retryChatModel{retry: 1, c: fake}

			ctx := context.Background()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // already done before the call
			}

			start := time.Now()
			err := tt.invoke(r, ctx)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !errors.Is(err, errFakeModel) {
				t.Fatalf("expected the final attempt's error %v, got %v", errFakeModel, err)
			}
			if got := tt.calls(fake); got != tt.wantCalls {
				t.Fatalf("expected exactly %d model invocation(s), got %d", tt.wantCalls, got)
			}
			if tt.maxElapsed > 0 && elapsed >= tt.maxElapsed {
				t.Fatalf("expected the call to return promptly (< %v), took %v", tt.maxElapsed, elapsed)
			}
		})
	}
}

// --- Streaming retry tests -------------------------------------------------
//
// These drive the public constructor against a local SSE server, so the real
// streaming client and the real drain path are exercised. The per-attempt model
// timeout is sub-second, keeping the fail-then-succeed cases fast.

// sseChunkLine is one OpenAI-compatible stream chunk (delta.content) as an SSE frame.
func sseChunkLine(content string) string {
	return fmt.Sprintf("data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", content)
}

// sseFinishLine ends a stream normally: a stop chunk followed by [DONE].
func sseFinishLine() string {
	return "data: {\"id\":\"2\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
}

func flushSSE(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// serveSSECompletion serves a complete, successful streamed completion.
func serveSSECompletion(t *testing.T, w http.ResponseWriter, parts ...string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flushSSE(w)
	for _, p := range parts {
		_, _ = io.WriteString(w, sseChunkLine(p))
		flushSSE(w)
	}
	_, _ = io.WriteString(w, sseFinishLine())
	flushSSE(w)
}

// serveSSEStall sends response headers, then holds the body open with no chunk
// until the client's per-attempt timeout cancels the request. This is the
// production shape: the call is created, then the body read times out.
func serveSSEStall(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flushSSE(w)
	<-r.Context().Done()
}

// serveSSEChunkThenError delivers one chunk and then fails the stream in-band,
// deterministically (no TCP race), so the "failure after a delivered chunk" case
// is stable.
func serveSSEChunkThenError(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flushSSE(w)
	_, _ = io.WriteString(w, sseChunkLine(content))
	flushSSE(w)
	_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"mid-stream failure\",\"type\":\"server_error\"}}\n\n")
	flushSSE(w)
}

// readStream drains a message stream and returns the concatenated content. A
// non-EOF receive error is returned to the caller.
func readStream(sr *schema.StreamReader[*schema.Message]) (string, error) {
	defer sr.Close()
	var msgs []*schema.Message
	for {
		chunk, err := sr.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		msgs = append(msgs, chunk)
	}
	if len(msgs) == 0 {
		return "", nil
	}
	msg, err := schema.ConcatMessages(msgs)
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}

func invokeStream(ctx context.Context, m *OpenAIChatModel) (string, error) {
	sr, err := m.Stream(ctx, nil)
	if err != nil {
		return "", err
	}
	return readStream(sr)
}

func invokeGenerate(ctx context.Context, m *OpenAIChatModel) (string, error) {
	msg, err := m.Generate(ctx, nil)
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}

func TestStreamRetry(t *testing.T) {
	const attemptTimeout = 300 * time.Millisecond

	tests := []struct {
		name         string
		handler      func(t *testing.T, w http.ResponseWriter, r *http.Request, n int)
		invoke       func(ctx context.Context, m *OpenAIChatModel) (string, error)
		wantErr      bool
		wantContent  string
		wantRequests int
	}{
		{
			name: "fail before first chunk then succeed",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, n int) {
				if n == 1 {
					serveSSEStall(t, w, r)
					return
				}
				serveSSECompletion(t, w, "Hello", " World")
			},
			invoke:       invokeStream,
			wantContent:  "Hello World",
			wantRequests: 2,
		},
		{
			name: "fail after a chunk is not retried",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, n int) {
				if n == 1 {
					serveSSEChunkThenError(t, w, "Hello")
					return
				}
				serveSSECompletion(t, w, "Hello", " World")
			},
			invoke:       invokeStream,
			wantErr:      true,
			wantRequests: 1,
		},
		{
			name: "fail at creation is retried",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, n int) {
				if n == 1 {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				serveSSECompletion(t, w, "Hello", " World")
			},
			invoke:       invokeStream,
			wantContent:  "Hello World",
			wantRequests: 2,
		},
		{
			name: "generate routes through the stream retry path",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, n int) {
				if n == 1 {
					serveSSEStall(t, w, r)
					return
				}
				serveSSECompletion(t, w, "Hello", " World")
			},
			invoke:       invokeGenerate,
			wantContent:  "Hello World",
			wantRequests: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.handler(t, w, r, int(n.Add(1)))
			}))
			t.Cleanup(srv.Close)

			m, err := NewOpenAIChatModel("test-model", "sk-test",
				WithBaseURL(srv.URL),
				WithModelTimeout(attemptTimeout),
				WithRetry(1),
			)
			if err != nil {
				t.Fatalf("NewOpenAIChatModel: %v", err)
			}

			content, err := tt.invoke(context.Background(), m)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got content %q", content)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if content != tt.wantContent {
					t.Fatalf("expected content %q, got %q", tt.wantContent, content)
				}
			}
			if got := int(n.Load()); got != tt.wantRequests {
				t.Fatalf("expected %d completion request(s), got %d", tt.wantRequests, got)
			}
		})
	}
}

// scriptedChatModel hands the driver a caller-supplied stream so the driver's
// guards can be exercised directly.
type scriptedChatModel struct {
	stream func() (*schema.StreamReader[*schema.Message], error)
}

func (s *scriptedChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return nil, errFakeModel
}

func (s *scriptedChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return s.stream()
}

func (s *scriptedChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

// TestStreamRetry_PanicPropagates proves a panic inside the driver surfaces to
// the consumer as a stream error rather than a silent io.EOF.
func TestStreamRetry_PanicPropagates(t *testing.T) {
	r := &retryChatModel{retry: 1, c: &scriptedChatModel{
		stream: func() (*schema.StreamReader[*schema.Message], error) {
			panic("boom")
		},
	}}

	sr, err := r.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream returned a synchronous error: %v", err)
	}
	_, err = readStream(sr)
	if err == nil {
		t.Fatal("expected a panic-derived error on the stream, got a clean end of stream")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected the panic to be surfaced, got %v", err)
	}
}

// TestStreamRetry_ConsumerStopsReading proves the driver releases the abandoned
// inner reader when the consumer stops reading, so no goroutine is leaked.
func TestStreamRetry_ConsumerStopsReading(t *testing.T) {
	inner, innerW := schema.Pipe[*schema.Message](0)
	released := make(chan struct{})
	go func() {
		defer innerW.Close()
		for {
			if closed := innerW.Send(&schema.Message{Content: "x"}, nil); closed {
				close(released)
				return
			}
		}
	}()

	r := &retryChatModel{retry: 1, c: &scriptedChatModel{
		stream: func() (*schema.StreamReader[*schema.Message], error) { return inner, nil },
	}}

	sr, err := r.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream returned a synchronous error: %v", err)
	}
	if _, err := sr.Recv(); err != nil {
		t.Fatalf("first Recv: %v", err)
	}
	sr.Close() // consumer stops reading; the driver must release the inner reader

	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("driver did not release the abandoned reader when the consumer stopped")
	}
}

// --- Coverage-gap tests (ticket 04) ----------------------------------------

// TestRetryGapFunc_BackoffCap calls gapFunc as a pure function (no sleeping) to
// cover the 10s backoff cap and both doRetry outcomes, including the ctx guard.
func TestRetryGapFunc_BackoffCap(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name      string
		retry     int
		attempt   int
		cancelCtx bool
		wantWait  time.Duration
		wantRetry bool
	}{
		{name: "attempt below cap yields uncapped exponential wait", retry: 5, attempt: 4, wantWait: 8 * time.Second, wantRetry: true},
		{name: "attempt above cap yields capped wait", retry: 5, attempt: 5, wantWait: 10 * time.Second, wantRetry: true},
		{name: "capped wait with budget exhausted", retry: 5, attempt: 6, wantWait: 10 * time.Second, wantRetry: false},
		{name: "budget exhausted no retry", retry: 1, attempt: 2, wantWait: 2 * time.Second, wantRetry: false},
		{name: "done context no retry at low index", retry: 5, attempt: 1, cancelCtx: true, wantWait: time.Second, wantRetry: false},
		{name: "done context no retry above cap", retry: 5, attempt: 4, cancelCtx: true, wantWait: 8 * time.Second, wantRetry: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &retryChatModel{retry: tt.retry}
			ctx := context.Background()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			wait, doRetry := r.gapFunc(ctx, tt.attempt, errBoom)
			if wait != tt.wantWait {
				t.Fatalf("expected wait %v, got %v", tt.wantWait, wait)
			}
			if doRetry != tt.wantRetry {
				t.Fatalf("expected doRetry %v, got %v", tt.wantRetry, doRetry)
			}
		})
	}
}

// TestIsNonRetryableError pins the error classification: permanent provider failures
// must not be retried, transient ones (including 429 rate limits and transport timeouts)
// must be.
func TestIsNonRetryableError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantReason  string
		wantNoRetry bool
	}{
		{name: "nil", err: nil, wantNoRetry: false},
		{name: "401 status code", err: errors.New("error, status code: 401, status: , message: nope"), wantReason: "unauthorized", wantNoRetry: true},
		{name: "401 unauthorized text", err: errors.New("401 Unauthorized"), wantReason: "unauthorized", wantNoRetry: true},
		{name: "invalid api key", err: errors.New("error, status code: 401, message: invalid_api_key"), wantReason: "invalid api key", wantNoRetry: true},
		{name: "403", err: errors.New("error, status code: 403, status: , message: forbidden"), wantReason: "forbidden", wantNoRetry: true},
		{name: "404", err: errors.New("error, status code: 404, status: , message: not found"), wantReason: "not found", wantNoRetry: true},
		{name: "400", err: errors.New("error, status code: 400, status: , message: bad request"), wantReason: "bad request", wantNoRetry: true},
		{name: "400 missing function.arguments", err: errors.New("error, status code: 400, status: 400 Bad Request, message: If tool_calls are present in the message, function.arguments must be defined."), wantReason: "bad request", wantNoRetry: true},
		{name: "insufficient quota", err: errors.New(`{"error":{"code":"insufficient_quota"}}`), wantReason: "quota exhausted", wantNoRetry: true},
		{name: "exceeded current quota", err: errors.New("You exceeded your current quota, please check your plan"), wantReason: "quota exhausted", wantNoRetry: true},
		{name: "billing", err: errors.New("billing hard limit has been reached"), wantReason: "billing issue", wantNoRetry: true},
		{name: "account deactivated", err: errors.New("account_deactivated"), wantReason: "account deactivated", wantNoRetry: true},
		{name: "context length exceeded", err: errors.New("context_length_exceeded: too many tokens"), wantReason: "context length exceeded", wantNoRetry: true},
		{name: "maximum context length", err: errors.New("This model's maximum context length is 32768 tokens"), wantReason: "context length exceeded", wantNoRetry: true},
		{name: "context canceled", err: fmt.Errorf("request failed: %w", context.Canceled), wantReason: "context canceled", wantNoRetry: true},
		{name: "context deadline exceeded", err: fmt.Errorf("request failed: %w", context.DeadlineExceeded), wantReason: "context deadline exceeded", wantNoRetry: true},
		{name: "per-attempt client timeout stays retryable", err: fmt.Errorf("context deadline exceeded (Client.Timeout or context cancellation while reading body): %w", context.DeadlineExceeded), wantNoRetry: false},
		{name: "429 rate limit", err: errors.New("error, status code: 429, status: , message: Rate limit reached for requests"), wantNoRetry: false},
		{name: "500", err: errors.New("error, status code: 500, status: , message: internal error"), wantNoRetry: false},
		{name: "502", err: errors.New("error, status code: 502, status: , message: bad gateway"), wantNoRetry: false},
		{name: "EOF", err: io.EOF, wantNoRetry: false},
		{name: "timeout", err: errors.New("dial tcp 127.0.0.1:443: i/o timeout"), wantNoRetry: false},
		{name: "generic", err: errors.New("connection reset by peer"), wantNoRetry: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, noRetry := isNonRetryableError(tt.err)
			if noRetry != tt.wantNoRetry {
				t.Fatalf("isNonRetryableError() noRetry = %v, want %v", noRetry, tt.wantNoRetry)
			}
			if noRetry && reason != tt.wantReason {
				t.Fatalf("isNonRetryableError() reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

// TestNonRetryableError_HTTP401NoRetry proves a 401 is issued exactly once: the retry
// layer must not burn its budget on a permanent auth failure.
func TestNonRetryableError_HTTP401NoRetry(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"invalid_request_error","code":"invalid_api_key"}}`))
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIChatModel("test-model", "sk-test",
		WithBaseURL(srv.URL),
		WithRetry(5),
	)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if _, err := invokeGenerate(ctx, m); err == nil {
		t.Fatal("expected the 401 to surface as an error")
	}
	t.Logf("401: finished in %v after %d request(s)", time.Since(start), requests.Load())

	if got := int(requests.Load()); got != 1 {
		t.Fatalf("expected exactly 1 provider request for a 401, got %d", got)
	}
}

// TestNonRetryableError_HTTP400NoRetry proves the incident's deterministic 400 is
// attempted once: retrying it would burn the budget and ~7s of sleeps, and the body
// cannot change, since the request that produced it is already fixed on the wire.
func TestNonRetryableError_HTTP400NoRetry(t *testing.T) {
	const rejection = `{"error":{"message":"If tool_calls are present in the message, function.arguments must be defined.","type":"invalid_request_error"}}`

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(rejection))
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIChatModel("gpt-4o-mini", "k",
		WithBaseURL(srv.URL),
		WithRetry(3),
	)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err = m.Generate(ctx, []*schema.Message{schema.UserMessage("hi")})
	if err == nil {
		t.Fatal("expected the 400 to surface as an error")
	}
	if !strings.Contains(err.Error(), "function.arguments") {
		t.Fatalf("expected the provider message to surface, got %v", err)
	}
	t.Logf("400: finished in %v after %d request(s)", time.Since(start), requests.Load())

	if got := int(requests.Load()); got != 1 {
		t.Fatalf("expected exactly 1 provider request for a deterministic 400, got %d", got)
	}
}

// errorStream returns a reader that yields err before any chunk.
func errorStream(err error) *schema.StreamReader[*schema.Message] {
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(nil, err)
	sw.Close()
	return sr
}

// drainFailChatModel creates the stream successfully every time, but the stream
// itself fails before delivering any chunk. Creations are counted so the number
// of attempts is observable.
type drainFailChatModel struct {
	err         error
	streamCalls int
}

func (f *drainFailChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return nil, f.err
}

func (f *drainFailChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.streamCalls++
	return errorStream(f.err), nil
}

func (f *drainFailChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return f, nil
}

// TestStreamRetry_DrainBudgetExhausted covers a pre-delivery drain failure whose
// retried attempt also fails before any chunk: the budget is exhausted and the
// error must reach the consumer instead of being swallowed as a clean end.
func TestStreamRetry_DrainBudgetExhausted(t *testing.T) {
	errDrain := errors.New("drain failure before any chunk")
	fake := &drainFailChatModel{err: errDrain}
	r := &retryChatModel{retry: 1, c: fake}

	sr, err := r.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream returned a synchronous error: %v", err)
	}
	content, err := readStream(sr)
	if err == nil {
		t.Fatal("expected the exhausted-budget drain error, got a clean end of stream")
	}
	if !errors.Is(err, errDrain) {
		t.Fatalf("expected the drain error to be surfaced, got %v", err)
	}
	if content != "" {
		t.Fatalf("expected no content, got %q", content)
	}
	if fake.streamCalls != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", fake.streamCalls)
	}
}

// panicOnRecvReader returns a reader whose Recv panics, so the panic is raised
// while draining an already-open reader rather than at stream creation.
func panicOnRecvReader() *schema.StreamReader[*schema.Message] {
	inner, innerW := schema.Pipe[*schema.Message](1)
	innerW.Send(&schema.Message{Content: "x"}, nil)
	innerW.Close()
	return schema.StreamReaderWithConvert(inner, func(*schema.Message) (*schema.Message, error) {
		panic("drain boom")
	})
}

// TestStreamRetry_PanicWhileDraining proves a panic raised while draining an open
// reader reaches the consumer as a stream error, not a clean end of stream.
func TestStreamRetry_PanicWhileDraining(t *testing.T) {
	r := &retryChatModel{retry: 1, c: &scriptedChatModel{
		stream: func() (*schema.StreamReader[*schema.Message], error) {
			return panicOnRecvReader(), nil
		},
	}}

	sr, err := r.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream returned a synchronous error: %v", err)
	}
	_, err = readStream(sr)
	if err == nil {
		t.Fatal("expected a panic-derived error from the drain, got a clean end of stream")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected the panic to be surfaced, got %v", err)
	}
}

// TestStreamRetry_AttemptCounterSharedAcrossPhases drives a creation-time retry
// followed by a pre-delivery drain retry through one shared budget of two: three
// provider requests are made before the final attempt succeeds.
func TestStreamRetry_AttemptCounterSharedAcrossPhases(t *testing.T) {
	const attemptTimeout = 300 * time.Millisecond

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch int(n.Add(1)) {
		case 1:
			http.Error(w, "boom", http.StatusInternalServerError) // fail at creation
		case 2:
			serveSSEStall(t, w, r) // creation ok, then fail before any chunk
		default:
			serveSSECompletion(t, w, "Hello", " World")
		}
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIChatModel("test-model", "sk-test",
		WithBaseURL(srv.URL),
		WithModelTimeout(attemptTimeout),
		WithRetry(2),
	)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	content, err := invokeStream(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "Hello World" {
		t.Fatalf("expected content %q, got %q", "Hello World", content)
	}
	if got := int(n.Load()); got != 3 {
		t.Fatalf("expected exactly 3 provider requests (shared budget of 2), got %d", got)
	}
}

// TestStreamRetry_AttemptCounterSharedAcrossPhases_Exhausts proves the retry
// budget is shared across phases. A creation failure consumes one attempt, then
// two pre-delivery drain failures consume the rest, so the run ends with the
// error surfaced after exactly three provider requests. A counter that reset per
// phase (each phase getting its own budget) would instead let the third drain
// failure retry once more and succeed, which is why the error outcome, not just
// the request count, is the discriminating assertion.
func TestStreamRetry_AttemptCounterSharedAcrossPhases_Exhausts(t *testing.T) {
	const attemptTimeout = 300 * time.Millisecond

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch int(n.Add(1)) {
		case 1:
			http.Error(w, "boom", http.StatusInternalServerError) // fail at attempt 1: creation
		case 2, 3:
			serveSSEStall(t, w, r) // attempts 2 and 3: created, then fail before any chunk
		default:
			// Only reachable if the budget were per-phase rather than shared.
			serveSSECompletion(t, w, "Hello", " World")
		}
	}))
	t.Cleanup(srv.Close)

	m, err := NewOpenAIChatModel("test-model", "sk-test",
		WithBaseURL(srv.URL),
		WithModelTimeout(attemptTimeout),
		WithRetry(2),
	)
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	content, err := invokeStream(context.Background(), m)
	if err == nil {
		t.Fatalf("expected the exhausted-budget error, got a clean end with content %q", content)
	}
	if content != "" {
		t.Fatalf("expected no content, got %q", content)
	}
	if got := int(n.Load()); got != 3 {
		t.Fatalf("expected exactly 3 provider requests (shared budget of 2), got %d", got)
	}
}

// assertJSONEqual compares two JSON documents semantically so tests do not pin
// key order, while still catching a missing, added or altered field.
func assertJSONEqual(t *testing.T, want string, got []byte) {
	t.Helper()
	var wantAny, gotAny any
	if err := json.Unmarshal([]byte(want), &wantAny); err != nil {
		t.Fatalf("invalid expected JSON: %v", err)
	}
	if err := json.Unmarshal(got, &gotAny); err != nil {
		t.Fatalf("modifier produced invalid JSON: %v (%s)", err, got)
	}
	if !reflect.DeepEqual(wantAny, gotAny) {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// TestEnsureMessageContent verifies the request payload modifier injects the
// fields providers require and drops tool calls that cannot be addressed.
func TestEnsureMessageContent(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		want          string
		wantUnchanged bool
	}{
		{
			name: "missing arguments becomes an empty object",
			body: `{"messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq"}}]}]}`,
			want: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]}]}`,
		},
		{
			name: "empty arguments becomes an empty object",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":""}}]}]}`,
			want: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]}]}`,
		},
		{
			name:          "non-empty arguments is left untouched",
			body:          `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{\"query\":\"x\"}"}}]}]}`,
			want:          `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{\"query\":\"x\"}"}}]}]}`,
			wantUnchanged: true,
		},
		{
			name: "tool call without a function name is dropped",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"arguments":"{}"}}]}]}`,
			want: `{"messages":[{"role":"assistant","content":""}]}`,
		},
		{
			name: "tool call with an empty function name is dropped",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}}]}]}`,
			want: `{"messages":[{"role":"assistant","content":""}]}`,
		},
		{
			name: "tool call without a function object is dropped",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function"}]}]}`,
			want: `{"messages":[{"role":"assistant","content":""}]}`,
		},
		{
			name: "mixed message keeps only the addressable call",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":"","arguments":"{}"}}]}]}`,
			want: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]}]}`,
		},
		{
			name:          "message without tool_calls is untouched",
			body:          `{"messages":[{"role":"user","content":"hi"}]}`,
			want:          `{"messages":[{"role":"user","content":"hi"}]}`,
			wantUnchanged: true,
		},
		{
			name: "tool result of a dropped nameless call is dropped too",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c2","type":"function","function":{"name":""}}]},{"role":"tool","tool_call_id":"c2","content":"tool \"\" does not exist"}]}`,
			want: `{"messages":[{"role":"assistant","content":""}]}`,
		},
		{
			name: "tool result of a kept call survives a dropped sibling",
			body: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":""}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`,
			want: `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`,
		},
		{
			name:          "tool results are untouched when no call is dropped",
			body:          `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`,
			want:          `{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"query_faq","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}]}`,
			wantUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ensureMessageContent(context.Background(), nil, []byte(tt.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertJSONEqual(t, tt.want, got)
			if tt.wantUnchanged && string(got) != tt.body {
				t.Fatalf("expected the original body, got %s", got)
			}
			if tt.name == "non-empty arguments is left untouched" &&
				!strings.Contains(string(got), `"arguments":"{\"query\":\"x\"}"`) {
				t.Fatalf("arguments were re-encoded instead of preserved: %s", got)
			}
		})
	}
}
