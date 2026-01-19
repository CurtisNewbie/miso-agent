package memory

import (
	"fmt"
	"slices"
	"time"

	"github.com/curtisnewbie/miso-agent/agents"
	"github.com/curtisnewbie/miso/middleware/redis"
	"github.com/curtisnewbie/miso/miso"
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
	return redis.Set(rail, key, fmt.Sprintf(s.keyPat, key), ttl)
}

type TempMemory struct {
	key     string
	lockPat string

	shortTerm *shortTermTempMemory
	longTerm  *longTermTempMemory
	agent     *agents.MemorySummarizer

	compactThreshold   int
	compactCount       int
	longTermMemoryTTL  time.Duration
	shortTermMemoryTTL time.Duration
}

func (m *TempMemory) Load(rail miso.Rail) (_longTerm string, shortTerm []Conversation, _err error) {
	lk := redis.NewRLockf(rail, m.lockPat, m.key)
	if err := lk.Lock(); err != nil {
		return "", nil, err
	}
	defer lk.Unlock()

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

func (m *TempMemory) LoadFormatted(rail miso.Rail) (_longTerm string, _shortTerm string, _err error) {
	longTerm, shortTerm, err := m.Load(rail)
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

	longTerm, err := m.longTerm.Load(rail, m.key)
	if err != nil {
		return err
	}

	shortTerm = append(shortTerm, c)
	if len(shortTerm) >= m.compactThreshold {
		trimmed := shortTerm[:m.compactCount]
		shortTerm = shortTerm[m.compactCount:]

		slices.Reverse(trimmed)
		trimmedstr := strutil.NewBuilder()
		for _, t := range trimmed {
			if trimmedstr.Len() > 0 {
				trimmedstr.WriteRune('\n')
			}
			trimmedstr.Printlnf("%v", t.Time.FormatStdLocale())
			trimmedstr.Printlnf("User: %v", t.User)
			trimmedstr.Printlnf("Assistant: %v", t.Assistant)
		}
		summarizerd, err := m.agent.Execute(rail, agents.MemorySummarizerInput{
			LongTermMemory:     longTerm,
			RecentConversation: trimmedstr.String(),
		})
		if err != nil {
			return err
		}
		longTerm = summarizerd.Summary
		if err := m.longTerm.Store(rail, m.key, longTerm, m.longTermMemoryTTL); err != nil {
			return err
		}
	}

	return m.shortTerm.Store(rail, m.key, shortTerm, m.shortTermMemoryTTL)
}

type memoryConfig struct {
	compactThreshold   int
	compactCount       int
	longTermMemoryTTL  time.Duration
	shortTermMemoryTTL time.Duration
}

// Trigger memory compaction when the number of conversations is greater than or equal to n.
//
// Remove the earliest n / 2 conversations (CompactCount), summarize them, and merge them to long term memory.
func WithCompactThreshold(n int) memoryConfigFunc {
	return func(mc *memoryConfig) {
		if n < 2 {
			n = 2
		}
		mc.compactThreshold = n
		mc.compactCount = n / 2
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

// Create TempMemory.
//
// By default, CompactThreshold is set to 4, CompactCount is set to 2, and both the long term memory and short term memory are set to 30 days.
func NewTempMemory(key string, agent *agents.MemorySummarizer, ops ...memoryConfigFunc) *TempMemory {
	m := &memoryConfig{
		compactThreshold:   4,
		compactCount:       2,
		longTermMemoryTTL:  time.Hour * 24 * 30,
		shortTermMemoryTTL: time.Hour * 24 * 30,
	}
	for _, op := range ops {
		op(m)
	}
	return &TempMemory{
		agent:              agent,
		key:                key,
		lockPat:            "miso-agent:memory:memory-store:%v",
		compactThreshold:   m.compactThreshold,
		compactCount:       m.compactCount,
		shortTerm:          newShortTermTempMemory(),
		longTerm:           newLongTermTempMemory(),
		longTermMemoryTTL:  m.longTermMemoryTTL,
		shortTermMemoryTTL: m.shortTermMemoryTTL,
	}
}
