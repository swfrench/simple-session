package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore is a Redis-based session store for session data of type S,
// implementing the SessionStore interface. S must be marshallable to JSON.
type RedisStore[S any] struct {
	rc     *redis.Client
	prefix string
}

// NewRedisStore returns a new RedisStore using the provided client. Keys will
// be stored with the provided prefix.
func NewRedisStore[S any](rc *redis.Client, prefix string) *RedisStore[S] {
	return &RedisStore[S]{rc: rc, prefix: prefix}
}

func (rs *RedisStore[S]) sessionKey(sid string) string {
	return fmt.Sprintf("%s:%s", rs.prefix, sid)
}

// Get returns the session data for the provided SID.
func (rs *RedisStore[S]) Get(ctx context.Context, sid string) (*S, error) {
	val, err := rs.rc.Get(ctx, rs.sessionKey(sid)).Result()
	if err == redis.Nil {
		return nil, ErrSessionNotFound
	}
	s := new(S)
	if err := json.Unmarshal([]byte(val), s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data from Redis (error: %v): %w", err, ErrInvalidStoredSessionData)
	}
	return s, nil
}

// Set stores the provided session data associated with the provided SID. Note
// that if a session is already associated with the latter, ErrSessionExists is
// returned.
func (rs *RedisStore[S]) Set(ctx context.Context, sid string, s *S, ttl time.Duration) error {
	val, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal session data (error: %v): %w", err, ErrInvalidSessionData)
	}
	set, err := rs.rc.SetNX(ctx, rs.sessionKey(sid), val, ttl).Result()
	if err != nil {
		return fmt.Errorf("failed to store session info to Redis: %w", err)
	}
	if !set {
		return ErrSessionExists
	}
	return nil
}

// Del deletes the session data associated with the provided SID.
func (rs *RedisStore[S]) Del(ctx context.Context, sid string) error {
	r := rs.rc.Del(ctx, rs.sessionKey(sid))
	if err := r.Err(); err != nil {
		return err
	}
	if r.Val() != 1 {
		return ErrSessionNotFound
	}
	return nil
}
