package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
