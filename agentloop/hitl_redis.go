package agentloop

import (
	"context"
	"fmt"
	"time"

	"github.com/curtisnewbie/miso/errs"
	"github.com/curtisnewbie/miso/flow"
	mredis "github.com/curtisnewbie/miso/middleware/redis"
)

const defaultRedisHitlKeyPat = "miso-agent:hitl:%v"

// RedisHitlStoreOption configures a RedisHitlStore.
type RedisHitlStoreOption func(*redisHitlStore)

// WithRedisHitlKeyPattern overrides the Redis key pattern.
// The pattern must contain one %v placeholder for the session ID.
// Default: "miso-agent:hitl:%v".
func WithRedisHitlKeyPattern(pat string) RedisHitlStoreOption {
	return func(s *redisHitlStore) { s.keyPat = pat }
}

// WithRedisHitlTTL sets an expiry on persisted HITL state.
// Use 0 (the default) for no expiry.
func WithRedisHitlTTL(ttl time.Duration) RedisHitlStoreOption {
	return func(s *redisHitlStore) { s.ttl = ttl }
}

// NewRedisHitlStore returns a HitlStore backed by Redis.
// Requires miso Redis middleware to be bootstrapped before use.
func NewRedisHitlStore(opts ...RedisHitlStoreOption) HitlStore {
	s := &redisHitlStore{keyPat: defaultRedisHitlKeyPat}
	for _, o := range opts {
		o(s)
	}
	return s
}

type redisHitlStore struct {
	keyPat string
	ttl    time.Duration
}

func (s *redisHitlStore) key(sessionId string) string {
	return fmt.Sprintf(s.keyPat, sessionId)
}

func (s *redisHitlStore) Save(ctx context.Context, sessionId string, state HitlState) error {
	if err := mredis.SetJson(flow.NewRail(ctx), s.key(sessionId), state, s.ttl); err != nil {
		return errs.Wrapf(err, "save HITL state for session %q", sessionId)
	}
	return nil
}

func (s *redisHitlStore) Load(ctx context.Context, sessionId string) (HitlState, bool, error) {
	state, ok, err := mredis.GetJson[HitlState](flow.NewRail(ctx), s.key(sessionId))
	if err != nil {
		return HitlState{}, false, errs.Wrapf(err, "load HITL state for session %q", sessionId)
	}
	return state, ok, nil
}

func (s *redisHitlStore) Delete(ctx context.Context, sessionId string) error {
	if err := mredis.GetRedis().Del(ctx, s.key(sessionId)).Err(); err != nil {
		return errs.Wrapf(err, "delete HITL state for session %q", sessionId)
	}
	return nil
}
