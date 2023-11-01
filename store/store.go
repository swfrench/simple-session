package store

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrSessionNotFound indicates that the provided SID does not map to any
	// session.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionExists indicates that the provided SID already maps to a
	// session.
	ErrSessionExists = errors.New("session exists")
	// ErrInvalidSessionData indicates that the provided session data is
	// invalid, and cannot be used. For example, this may occur if it cannot be
	// successfully marshalled to JSON.
	ErrInvalidSessionData = errors.New("invalid session data")
	// ErrInvalidStoredSessionData indicates that the session data fetched from
	// storage is invalid, and cannot be used. For example, this may occur if it
	// cannot be successfully unmarshalled.
	ErrInvalidStoredSessionData = errors.New("invalid stored session data")
)

// SessionStore represents an abstract Session storage object.
type SessionStore[S any] interface {
	Get(context.Context, string) (*S, error)
	Set(context.Context, string, *S, time.Duration) error
	Del(context.Context, string) error
}
