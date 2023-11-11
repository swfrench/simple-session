// Package memory provides an in-memory SessionStore.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/swfrench/simple-session/store"
)

// Store is a simple in-memory session store, for use in tests or where an
// external store is not available.
//
// Notably, it _does not_ "store" sessions as values in serialized form, but
// simply stores pointers to the original objects passed to it (i.e., if you
// mutate said objects, the objects in the store are also mutated, since they
// are one and the same).
//
// Eviction: Expired sessions are garbage collected on entry to any Store
// method.
type Store[S any] struct {
	// Clock can be overridden in tests (e.g., to test eviciton logic).
	Clock     func() time.Time
	mu        sync.Mutex
	items     map[string]*S
	evictions *evictionQueue
}

// New returns a new Store instance.
func New[S any]() *Store[S] {
	ms := &Store[S]{
		Clock:     func() time.Time { return time.Now() },
		items:     make(map[string]*S),
		evictions: newEvictionQueue(),
	}
	return ms
}

func (ms *Store[S]) evict(t time.Time) {
	for ms.evictions.Len() > 0 && ms.evictions.Peek().expires.Before(t) {
		delete(ms.items, ms.evictions.Pop().key)
	}
}

// Get returns the stored session data associated with the provided SID, or
// ErrSessionNotFound if no stored session exists.
func (ms *Store[S]) Get(ctx context.Context, sid string) (*S, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.evict(ms.Clock())
	s, ok := ms.items[sid]
	if !ok {
		return nil, store.ErrSessionNotFound
	}
	return s, nil
}

// Set stores the provided session data associated with the provided SID and
// TTL, returning ErrSessionExists if a session is already associated with the
// former.
func (ms *Store[S]) Set(ctx context.Context, sid string, s *S, ttl time.Duration) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	t := ms.Clock()
	ms.evict(t)
	if _, ok := ms.items[sid]; ok {
		return store.ErrSessionExists
	}
	ms.items[sid] = s
	ms.evictions.Push(sid, t.Add(ttl))
	return nil
}

// Del deletes the stored session data associated with the provided SID,
// returning ErrSessionNotFound if no stored session exists.
func (ms *Store[S]) Del(ctx context.Context, sid string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.evict(ms.Clock())
	if _, ok := ms.items[sid]; !ok {
		return store.ErrSessionNotFound
	}
	// Note: We let the evictions entry get cleaned up lazily.
	delete(ms.items, sid)
	return nil
}
