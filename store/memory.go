package store

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

type eviction struct {
	expires time.Time
	sid     string
}

type evictionQueue []*eviction

func (eq evictionQueue) Len() int {
	return len(eq)
}

func (eq evictionQueue) Less(i, j int) bool {
	return eq[i].expires.Before(eq[j].expires)
}

func (eq evictionQueue) Swap(i, j int) {
	eq[i], eq[j] = eq[j], eq[i]
}

func (eq *evictionQueue) Push(e any) {
	*eq = append(*eq, e.(*eviction))
}

func (eq *evictionQueue) Pop() any {
	n := len(*eq)
	e := (*eq)[n-1]
	(*eq)[n-1] = nil
	*eq = (*eq)[:n-1]
	return e
}

func (eq *evictionQueue) Peek() *eviction {
	n := len(*eq)
	return (*eq)[n-1]
}

// MemoryStore is a simple in-memory session store, for use in tests or where an
// external store is not available.
//
// Notably, it _does not_ "store" sessions as values in serialized form, but
// simply stores pointers to the original objects passed to it (i.e., if you
// mutate said objects, the objects in the store are also mutated, since they
// are one and the same).
//
// Eviction: Expired sessions are garbage collected on entry to any MemoryStore
// method.
type MemoryStore[S any] struct {
	// Clock can be overridden in tests (e.g., to test eviciton logic).
	Clock     func() time.Time
	mu        sync.Mutex
	items     map[string]*S
	evictions *evictionQueue
}

// NewMemoryStore returns a new MemoryStore instance.
func NewMemoryStore[S any]() *MemoryStore[S] {
	eq := &evictionQueue{}
	heap.Init(eq)
	ms := &MemoryStore[S]{
		Clock:     func() time.Time { return time.Now() },
		items:     make(map[string]*S),
		evictions: eq,
	}
	return ms
}

func (ms *MemoryStore[S]) evict(t time.Time) {
	for ms.evictions.Len() > 0 && ms.evictions.Peek().expires.Before(t) {
		e := heap.Pop(ms.evictions).(*eviction)
		delete(ms.items, e.sid)
	}
}

// Get retrieves the session associated with the provided SID from the store,
// returning ErrSessionNotFound if none exists.
func (ms *MemoryStore[S]) Get(ctx context.Context, sid string) (*S, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.evict(ms.Clock())
	s, ok := ms.items[sid]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// Set stores the provided session (keyed on the associated SID) in the store,
// with the provided TTL. Returns ErrSessionExists if a session associated with
// SID already exists.
func (ms *MemoryStore[S]) Set(ctx context.Context, sid string, s *S, ttl time.Duration) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	t := ms.Clock()
	ms.evict(t)
	if _, ok := ms.items[sid]; ok {
		return ErrSessionExists
	}
	ms.items[sid] = s
	heap.Push(ms.evictions, &eviction{
		expires: t.Add(ttl),
		sid:     sid,
	})
	return nil
}

// Del deletes the session associated with the provided SID from the store,
// returning ErrSessionNotFound if none exists.
func (ms *MemoryStore[S]) Del(ctx context.Context, sid string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.evict(ms.Clock())
	if _, ok := ms.items[sid]; !ok {
		return ErrSessionNotFound
	}
	// Note: We let the evictions entry get cleaned up lazily.
	delete(ms.items, sid)
	return nil
}
