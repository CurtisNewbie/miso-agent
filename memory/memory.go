package memory

import (
	"fmt"
	"slices"
	"time"

	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso/middleware/redis"
	"github.com/curtisnewbie/miso/miso"
	"github.com/curtisnewbie/miso/util/async"
	"github.com/curtisnewbie/miso/util/atom"
	"github.com/curtisnewbie/miso/util/strutil"
)

type Conversation struct {
	Time      atom.Time `json:"time"`
	User      string    `json:"user"`
	Assistant string    `json:"assistant"`
}

type shortTermTempMemory struct {
	keyPat string
}

func newShortTermTempMemory() *shortTermTempMemory {
	return &shortTermTempMemory{
		keyPat: "miso-agent:memory:short-term:%v",
	}
}

func (s *shortTermTempMemory) Load(rail miso.Rail, key string) ([]Conversation, error) {
	loaded, ok, err := redis.GetJson[[]Conversation](rail, fmt.Sprintf(s.keyPat, key))
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Conversation{}, nil
	}
	return loaded, nil
}

func (s *shortTermTempMemory) Store(rail miso.Rail, key string, v []Conversation, ttl time.Duration) error {
	return redis.SetJson(rail, fmt.Sprintf(s.keyPat, key), v, ttl)
}

type longTermTempMemory struct {
	keyPat string
}

func newLongTermTempMemory() *longTermTempMemory {
	return &longTermTempMemory{
		keyPat: "miso-agent:memory:long-term:%v",
	}
}

func (s *longTermTempMemory) Load(rail miso.Rail, key string) (string, error) {
	loaded, ok, err := redis.Get(rail, fmt.Sprintf(s.keyPat, key))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return loaded, nil
}

func (s *longTermTempMemory) Store(rail miso.Rail, key string, value string, ttl time.Duration) error {
	return redis.Set(rail, fmt.Sprintf(s.keyPat, key), value, ttl)
}

type TempMemory struct {
	key            string
	lockPat        string
	compactLockPat string

	shortTerm *shortTermTempMemory
	longTerm  *longTermTempMemory
	agent     *agents.MemorySummarizer

	compactThreshold      int
	compactTokenThreshold int
	longTermMemoryTTL     time.Duration
	shortTermMemoryTTL    time.Duration
}

func (m *TempMemory) LoadLocked(rail miso.Rail) (_longTerm string, shortTerm []Conversation, _err error) {
	lk := redis.NewRLockf(rail, m.lockPat, m.key)
	if err := lk.Lock(); err != nil {
		return "", nil, err
	}
	defer lk.Unlock()

	return m.Load(rail)
}

func (m *TempMemory) Load(rail miso.Rail) (_longTerm string, shortTerm []Conversation, _err error) {
	shortTerm, err := m.shortTerm.Load(rail, m.key)
	if err != nil {
		return "", nil, err
	}
	slices.Reverse(shortTerm)

	longTerm, err := m.longTerm.Load(rail, m.key)
	if err != nil {
		return "", nil, err
	}
	return longTerm, shortTerm, nil
}

func (m *TempMemory) LoadFormatted(rail miso.Rail, lock bool) (_longTerm string, _shortTerm string, _err error) {
	var (
		longTerm  string
		shortTerm []Conversation
		err       error
	)
	if lock {
		longTerm, shortTerm, err = m.LoadLocked(rail)
	} else {
		longTerm, shortTerm, err = m.Load(rail)
	}
	if err != nil {
		return "", "", err
	}

	shortTermFmt := strutil.NewBuilder()
	for _, t := range shortTerm {
		if shortTermFmt.Len() > 0 {
			shortTermFmt.WriteRune('\n')
		}
		shortTermFmt.Printlnf("%v", t.Time.FormatStdLocale())
		shortTermFmt.Printlnf("User: %v", t.User)
		shortTermFmt.Printlnf("Assistant: %v", t.Assistant)
	}
	return longTerm, shortTermFmt.String(), nil
}

// countConversationTokens approximates the token count of a Conversation
// using the same 4-chars-per-token heuristic as the agentloop Tokenizer.
func countConversationTokens(c Conversation) int {
	return (len(c.User) + len(c.Assistant)) / 4
}

// totalTokens returns the total approximate token count across all conversations.
func totalTokens(convs []Conversation) int {
	total := 0
	for _, c := range convs {
		total += countConversationTokens(c)
	}
	return total
}

func (m *TempMemory) Append(rail miso.Rail, c Conversation) error {
	lk := redis.NewRLockf(rail, m.lockPat, m.key)
	if err := lk.Lock(); err != nil {
		return err
	}
	defer lk.Unlock()

	shortTerm, err := m.shortTerm.Load(rail, m.key)
	if err != nil {
		return err
	}

	shortTerm = append(shortTerm, c)
	if err := m.shortTerm.Store(rail, m.key, shortTerm, m.shortTermMemoryTTL); err != nil {
		return err
	}
	if m.shouldCompact(shortTerm) {
		async.Fire(rail.NewCtx().NextSpanId(), func() error { return m.compactMemory(rail) })
	}
	return nil
}

// shouldCompact returns true when short-term memory exceeds the compaction threshold.
// If a token threshold is configured it takes precedence; otherwise falls back to the round threshold.
func (m *TempMemory) shouldCompact(shortTerm []Conversation) bool {
	// Require at least 3 conversations before compacting: selectCompactCount keeps
	// the last 2, so we need at least 1 eligible for compaction.
	if len(shortTerm) < 3 {
		return false
	}
	if m.compactTokenThreshold > 0 {
		return totalTokens(shortTerm) >= m.compactTokenThreshold
	}
	return len(shortTerm) >= m.compactThreshold
}

// selectCompactCount returns how many conversations (from the oldest end) to compact.
// Always compacts all but the two most recent conversations.
func (m *TempMemory) selectCompactCount(shortTerm []Conversation) int {
	max := len(shortTerm) - 2
	if max <= 0 {
		return 0
	}
	return max
}

func (m *TempMemory) compactMemory(rail miso.Rail) error {
	// Compaction lock: held for the full duration including the LLM call.
	// Uses a separate key from the data lock so Append is never blocked.
	lkc := redis.NewRLockf(rail, m.compactLockPat, m.key)
	ok, err := lkc.TryLock(redis.WithBackoff(time.Second * 3))
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	defer lkc.Unlock()

	shortTerm, err := m.shortTerm.Load(rail, m.key)
	if err != nil {
		return err
	}
	if !m.shouldCompact(shortTerm) {
		return nil
	}
	if m.compactTokenThreshold > 0 {
		rail.Infof("ShortTermMemory exceeds token threshold (%v), compacting memory: %v conversations, ~%v tokens",
			m.compactTokenThreshold, len(shortTerm), totalTokens(shortTerm))
	} else {
		rail.Infof("ShortTermMemory exceeds round threshold (%v), compacting memory: %v conversations",
			m.compactThreshold, len(shortTerm))
	}

	longTerm, err := m.longTerm.Load(rail, m.key)
	if err != nil {
		return err
	}
	compactedFirst := shortTerm[0] // identity of the first entry we are about to compact

	compactCount := m.selectCompactCount(shortTerm)
	if compactCount <= 0 {
		return nil
	}
	trimmed := shortTerm[:compactCount]
	slices.Reverse(trimmed)
	trimBuilder := strutil.NewBuilder()
	for _, t := range trimmed {
		if trimBuilder.Len() > 0 {
			trimBuilder.WriteRune('\n')
		}
		trimBuilder.Printlnf("%v", t.Time.FormatStdLocale())
		trimBuilder.Printlnf("User: %v", t.User)
		trimBuilder.Printlnf("Assistant: %v", t.Assistant)
	}
	trimmedstr := trimBuilder.String()
	summarizerd, err := m.agent.Execute(rail, agents.MemorySummarizerInput{
		LongTermMemory:     longTerm,
		RecentConversation: trimmedstr,
	})
	if err != nil {
		return err
	}
	longTerm = summarizerd.Summary
	if longTerm == "" { // failed for whatever reason?
		rail.Warnf("Failed to compact memory, summarized memory is empty, conversation: '%v'", trimmedstr)
		return nil
	}

	// Acquire lock to write. Re-load shortTerm to capture any appends that
	// happened while the LLM was running; only discard the compacted prefix.
	lkw := redis.NewRLockf(rail, m.lockPat, m.key)
	if err := lkw.Lock(); err != nil {
		return err
	}
	defer lkw.Unlock()

	shortTerm, err = m.shortTerm.Load(rail, m.key)
	if err != nil {
		return err
	}

	// Another compaction ran while the LLM was working and already trimmed our
	// entries — the front of shortTerm no longer matches what we summarized.
	// Abort to avoid trimming entries that were never incorporated into longTerm.
	if len(shortTerm) < compactCount ||
		shortTerm[0].Time.Compare(compactedFirst.Time) != 0 ||
		shortTerm[0].User != compactedFirst.User ||
		shortTerm[0].Assistant != compactedFirst.Assistant {
		rail.Infof("ShortTermMemory already compacted by another goroutine, skipping write")
		return nil
	}

	shortTerm = shortTerm[compactCount:]
	if err := m.longTerm.Store(rail, m.key, longTerm, m.longTermMemoryTTL); err != nil {
		return err
	}
	return m.shortTerm.Store(rail, m.key, shortTerm, m.shortTermMemoryTTL)
}

type memoryConfig struct {
	compactThreshold      int
	compactTokenThreshold int
	longTermMemoryTTL     time.Duration
	shortTermMemoryTTL    time.Duration
}

// WithCompactThreshold triggers memory compaction when the number of conversations
// is greater than or equal to n. On compaction all but the two most recent
// conversations are summarised and merged into long-term memory.
func WithCompactThreshold(n int) memoryConfigFunc {
	return func(mc *memoryConfig) {
		if n < 2 {
			n = 2
		}
		mc.compactThreshold = n
	}
}

// WithCompactTokenThreshold triggers memory compaction when the total approximate
// token count of short-term memory reaches or exceeds n tokens. When triggered,
// the oldest conversations totaling approximately n/2 tokens are summarized and
// merged into long-term memory.
//
// This option takes precedence over [WithCompactThreshold] when both are set.
func WithCompactTokenThreshold(n int) memoryConfigFunc {
	return func(mc *memoryConfig) {
		if n < 1 {
			return
		}
		mc.compactTokenThreshold = n
	}
}

// Set long term memory TTL to v.
func WithLongTermMemoryTTL(v time.Duration) memoryConfigFunc {
	return func(mc *memoryConfig) {
		mc.longTermMemoryTTL = v
	}
}

// Set short term memory TTL to v.
func WithShortTermMemoryTTL(v time.Duration) memoryConfigFunc {
	return func(mc *memoryConfig) {
		mc.shortTermMemoryTTL = v
	}
}

type memoryConfigFunc func(*memoryConfig)

// NewTempMemory creates a TempMemory backed by Redis.
//
// Default compaction policy: token-based at 4000 tokens (~4–6 typical customer-support
// exchanges). This threshold keeps recent detail without bloating the model's context
// window. On compaction the oldest conversations totalling ~2000 tokens are summarised
// and merged into long-term memory, leaving ~2000 tokens of recent context intact.
//
// Override with [WithCompactTokenThreshold] (recommended) or [WithCompactThreshold]
// (round-based, legacy). Both the short-term and long-term stores default to a 30-day TTL.
func NewTempMemory(key string, agent *agents.MemorySummarizer, ops ...memoryConfigFunc) *TempMemory {
	m := &memoryConfig{
		compactTokenThreshold: 4000,
		compactThreshold:      6,
		longTermMemoryTTL:     time.Hour * 24 * 30,
		shortTermMemoryTTL:    time.Hour * 24 * 30,
	}
	for _, op := range ops {
		op(m)
	}
	return &TempMemory{
		agent:                 agent,
		key:                   key,
		lockPat:               "miso-agent:memory:memory-store:%v",
		compactLockPat:        "miso-agent:memory:compacting:%v",
		compactThreshold:      m.compactThreshold,
		compactTokenThreshold: m.compactTokenThreshold,
		shortTerm:             newShortTermTempMemory(),
		longTerm:              newLongTermTempMemory(),
		longTermMemoryTTL:     m.longTermMemoryTTL,
		shortTermMemoryTTL:    m.shortTermMemoryTTL,
	}
}
