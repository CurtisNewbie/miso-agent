package agentloop

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/cloudwego/eino/schema"
	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/util/hash"
)

// HitlState holds the persisted agent state at the point of interruption.
// Saved by the agent when interrupted; loaded by Resume to continue execution.
type HitlState struct {
	Messages            []*schema.Message
	CompactionSummary   string
	OutputCheckAttempts int
	InterruptReason     string
}

// HitlStore persists and retrieves HITL state between an interrupt and a resume.
// Keyed by SessionId. Implementations must be safe for concurrent use.
type HitlStore interface {
	// Save persists the HITL state for the given session.
	Save(ctx context.Context, sessionId string, state HitlState) error
	// Load retrieves the HITL state for the given session. existed=false if not found.
	Load(ctx context.Context, sessionId string) (state HitlState, existed bool, err error)
	// Delete removes the HITL state for the given session.
	Delete(ctx context.Context, sessionId string) error
}

// NewMemHitlStore returns a simple in-memory HitlStore suitable for testing and
// single-process use. State is lost when the process exits.
func NewMemHitlStore() HitlStore {
	return &memHitlStore{states: hash.NewStrRWMap[[]byte]()}
}

type memHitlStore struct {
	states *hash.StrRWMap[[]byte]
}

func (s *memHitlStore) Save(_ context.Context, sessionId string, state HitlState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return errs.Wrapf(err, "marshal HitlState")
	}
	s.states.Put(sessionId, b)
	return nil
}

func (s *memHitlStore) Load(_ context.Context, sessionId string) (HitlState, bool, error) {
	b, ok := s.states.Get(sessionId)
	if !ok {
		return HitlState{}, false, nil
	}
	var state HitlState
	if err := json.Unmarshal(b, &state); err != nil {
		return HitlState{}, false, errs.Wrapf(err, "unmarshal HitlState")
	}
	return state, true, nil
}

func (s *memHitlStore) Delete(_ context.Context, sessionId string) error {
	s.states.Del(sessionId)
	return nil
}

// hitlSignal carries the interrupt reason set by a tool.
type hitlSignal struct {
	Reason string
}

// hitlHolder is a thread-safe container for a pending HITL interrupt signal.
// It is stored as a pointer field in AgentContext so mutations are visible
// across all copies of AgentContext produced by ctx.Value.
type hitlHolder struct {
	mu     sync.Mutex
	signal *hitlSignal
}

// set sets the interrupt reason. Only the first call takes effect.
func (h *hitlHolder) set(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.signal == nil {
		h.signal = &hitlSignal{Reason: reason}
	}
}

// get returns the pending signal, or nil if none has been set.
func (h *hitlHolder) get() *hitlSignal {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.signal
}

// RequestHitlInterrupt signals a HITL interrupt from within a tool implementation.
// The tool should also return a descriptive message to the LLM explaining what
// the user needs to do. The agent loop will pause after the current tool batch
// completes, persist its state, and return TaskOutput.Interrupted = true.
//
// This function is a no-op if HitlStore is not configured on the current agent.
func RequestHitlInterrupt(agentCtx AgentContext, reason string) {
	agentCtx.hitl.set(reason)
}
