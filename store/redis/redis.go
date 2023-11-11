// Package redis provides a Redis-backed SessionStore.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/swfrench/simple-session/store"
)

// Store is a Redis-based store for session data of type S, implementing the
// store.SessionStore interface. S must be marshallable to JSON.
type Store[S any] struct {
	rc     *goredis.Client
	prefix string
}

// New returns a new Store using the provided Redis client. Keys will be stored
// with the provided prefix.
func New[S any](rc *goredis.Client, prefix string) *Store[S] {
	return &Store[S]{rc: rc, prefix: prefix}
}

func (rs *Store[S]) sessionKey(sid string) string {
	return fmt.Sprintf("%s:%s", rs.prefix, sid)
}

// Get returns the stored session data associated with the provided SID, or
// ErrSessionNotFound if no stored session exists.
func (rs *Store[S]) Get(ctx context.Context, sid string) (*S, error) {
	val, err := rs.rc.Get(ctx, rs.sessionKey(sid)).Result()
	if err == goredis.Nil {
		return nil, store.ErrSessionNotFound
	}
	s := new(S)
	if err := json.Unmarshal([]byte(val), s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data from Redis (error: %v): %w", err, store.ErrInvalidStoredSessionData)
	}
	return s, nil
}

// Set stores the provided session data associated with the provided SID and
// TTL, returning ErrSessionExists if a session is already associated with the
// former.
func (rs *Store[S]) Set(ctx context.Context, sid string, s *S, ttl time.Duration) error {
	val, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal session data (error: %v): %w", err, store.ErrInvalidSessionData)
	}
	set, err := rs.rc.SetNX(ctx, rs.sessionKey(sid), val, ttl).Result()
	if err != nil {
		return fmt.Errorf("failed to store session info to Redis: %w", err)
	}
	if !set {
		return store.ErrSessionExists
	}
	return nil
}

// Del deletes the stored session data associated with the provided SID,
// returning ErrSessionNotFound if no stored session exists.
func (rs *Store[S]) Del(ctx context.Context, sid string) error {
	r := rs.rc.Del(ctx, rs.sessionKey(sid))
	if err := r.Err(); err != nil {
		return err
	}
	if r.Val() != 1 {
		return store.ErrSessionNotFound
	}
	return nil
}
