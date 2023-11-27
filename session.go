// Package session provides helpers for managing user sessions.
//
// At a high level, SessionManager manages the creation of time-bounded Session
// instances, which are in turn stored to a SessionStore. The Session is imbued
// with an arbitrary Data payload, which can be used to store user session
// details (e.g. identity).
//
// At Session creation, SessionManager will set the SID cookie to refer to the
// Session ID.
//
// Sessions also contain an assocated CSRF token, which can be used in CSRF
// protections (e.g., hidden form fields).
//
// The general principle is that HTTP handlers that must be Session-aware will
// use the Manage middleware. The latter ensures that a Session always exists,
// and defaults to a pre-session - i.e., one with nil associated Data. This
// ensures that CSRF protection is always possible.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/swfrench/simple-session/internal/token"
	"github.com/swfrench/simple-session/store"
	"golang.org/x/exp/slog"
)

const (
	defaultSessionTTL         = 30 * time.Minute
	sessionStorageGracePeriod = 10 * time.Minute
	sessionCookieGracePeriod  = 10 * time.Minute
	defaultIDLen              = 16 // bytes
	defaultSessionCookieName  = "session"
)

// contextKey is the type used to represent keys identifying values stored in
// the request Context.
type contextKey string

const contextKeySession = contextKey("session-id")

// Session represents a user session.
type Session[D any] struct {
	// ID is a random unique identifier (authenticated).
	ID string `json:"id"`
	// Data is an arbitrary data payload. Type D must meet any requirements of
	// the chosen SessionStore (e.g., must marshal to / from JSON).
	Data *D `json:"data"`
	// Expiration is the time after which this session is no longer valid.
	Expiration time.Time `json:"expiration"`
	// CSRFToken is a random identifier (authenticated) bound to this session,
	// suitable for, e.g., embedding in a hidden form field.
	CSRFToken string `json:"csrf_token"`
}

// Options represents tunable knobs that control the behavior of SessionManager.
type Options struct {
	// TTL is the duration that any given session is valid. Note that there is
	// no facility for session extension at this time.
	// Default if unspecified: 30m
	TTL time.Duration
	// IDLen is the length of random portion of user-facing identifiers (i.e.,
	// SIDs and CSRF tokens). Note that the full identifier will be extended
	// with its HMAC (32 bytes) and base64url enconded.
	// Default if unspecified: 16 bytes
	IDLen int
	// SessionCookieName is the name of the session ID cookie set by
	// SessionManager. For example, together with a suitable definition of
	// CreateCookie (see below), this can be used to configure a secure cookie
	// name prefix (e.g., "__Host-").
	// Default if unspecified: "session"
	SessionCookieName string
	// CreateCookie is a user-supplied factory for creating session ID cookies
	// with the provided name, value, and expiration. This is provided as a
	// convenience for granular control of cookie attributes, such as Path.
	// Default if unspecified: CreateStrictCookie
	CreateCookie func(name, value string, expires time.Time) *http.Cookie
}

// CreateStrictCookie returns an http.Cookie with strict defaults, with the
// provided name, value, and expiration. The resulting cookie is marked Secure,
// HttpOnly, and SameSite Strict, with no Domain or Path attribute.
// Consider using this as a base for your own implementation of CreateCookie.
func CreateStrictCookie(name, value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Expires:  expires,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// SessionManager manages user sessions (i.e., Session instances).
type SessionManager[D any] struct {
	// Clock can be used to override measurement of time in tests.
	Clock func() time.Time
	store store.SessionStore[Session[D]]
	opts  *Options
	ta    *token.TokenAuthenticator
}

// NewSessionManager returns a new SessionManager using the provided store for
// session storage and respecting the provided options.
// Session IDs and associated CSRF tokens are authenticated with HMAC-SHA256
// using the provided key.
func NewSessionManager[D any](s store.SessionStore[Session[D]], key []byte, opts *Options) *SessionManager[D] {
	if opts.TTL == time.Duration(0) {
		opts.TTL = defaultSessionTTL
	}
	if opts.IDLen == 0 {
		opts.IDLen = defaultIDLen
	}
	if opts.SessionCookieName == "" {
		opts.SessionCookieName = defaultSessionCookieName
	}
	if opts.CreateCookie == nil {
		opts.CreateCookie = CreateStrictCookie
	}
	return &SessionManager[D]{
		Clock: func() time.Time { return time.Now() },
		store: s,
		opts:  opts,
		ta:    token.NewTokenAuthenticator(key),
	}
}

// GetSession returns the Session object instance from the provided Context -
// i.e., previously stored there via the Manage middleware.
func (sm *SessionManager[D]) GetSession(ctx context.Context) *Session[D] {
	s := ctx.Value(contextKeySession)
	if s == nil {
		return nil
	}
	return s.(*Session[D])
}

var errExpiredSession = errors.New("expired session")

func (sm *SessionManager[D]) lookup(ctx context.Context, sid string) (*Session[D], error) {
	s, err := sm.store.Get(ctx, sid)
	if err != nil {
		return nil, err
	}
	if s.Expiration.Before(sm.Clock()) {
		return nil, errExpiredSession
	}
	return s, nil
}

func (sm *SessionManager[D]) createToken() (string, error) {
	data := make([]byte, sm.opts.IDLen)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return sm.ta.Create(data), nil
}

// Create creates a new Session with the provided Data payload, storing the
// session to the SessionStore and setting the associated SID cookie.
func (sm *SessionManager[D]) Create(ctx context.Context, w http.ResponseWriter, data *D) (*Session[D], error) {
	// TODO: Consider whether jittered backoff would make sense here, possibly
	// dependent on the type of SessionStore and error (e.g., Redis errors).
	attempts := 0
	var s *Session[D]
	for s == nil {
		attempts += 1
		id, err := sm.createToken()
		if err != nil {
			return nil, err
		}
		csrf, err := sm.createToken()
		if err != nil {
			return nil, err
		}
		s = &Session[D]{
			ID:         id,
			Data:       data,
			Expiration: sm.Clock().Add(sm.opts.TTL),
			CSRFToken:  csrf,
		}
		if err := sm.store.Set(ctx, id, s, sm.opts.TTL+sessionStorageGracePeriod); err != nil {
			if !errors.Is(err, store.ErrSessionExists) {
				slog.Error("Failed to store new session", "error", err)
			}
			if attempts == 3 {
				return nil, fmt.Errorf("failed to create session in %d attempts, latest error: %v", attempts, err)
			}
			s = nil
		}
	}
	sm.setSIDCookie(w, s.ID)
	return s, nil
}

// Clear creates a new pre-session (i.e., a Session with no Data payload) and
// attempts to delete the prior session from the SessionStore. The former is
// stored to the SessionStore and its ID set in the SID cookie, and it is also
// returned. Deletion of the old session is considered non-critical (i.e.,
// unexpected errors are merely logged).
func (sm *SessionManager[D]) Clear(ctx context.Context, w http.ResponseWriter, sid string) (*Session[D], error) {
	ps, err := sm.Create(ctx, w, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create new pre-session: %w", err)
	}
	if err := sm.store.Del(ctx, sid); err != nil && !errors.Is(err, store.ErrSessionNotFound) {
		slog.Error("Failed to delete data for session", "sid", sid)
	}
	return ps, nil
}

func (sm *SessionManager[D]) setSIDCookie(w http.ResponseWriter, sid string) {
	expires := sm.Clock().Add(sm.opts.TTL + sessionCookieGracePeriod)
	http.SetCookie(w, sm.opts.CreateCookie(sm.opts.SessionCookieName, sid, expires))
}

var errNoSIDCookie = errors.New("no SID cookie")

// getSIDCookie fetches the SID cookie from the provided request and verifies its authenticity.
func (sm *SessionManager[D]) getSIDCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(sm.opts.SessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", errNoSIDCookie
		}
		return "", err
	}
	if _, err := sm.ta.Verify(c.Value); err != nil {
		return "", err
	}
	return c.Value, nil
}

// VerifySessionCSRFToken verifies the authenticity of the provided CSRF token
// and that it matches the expected value for the provided Session.
func (sm *SessionManager[D]) VerifySessionCSRFToken(token string, s *Session[D]) error {
	if _, err := sm.ta.Verify(token); err != nil {
		return fmt.Errorf("failed to validate CSRF token: %w", err)
	}
	if token != s.CSRFToken {
		return fmt.Errorf("CSRF token %q does not match session-bound token %q", token, s.CSRFToken)
	}
	return nil
}

func (sm *SessionManager[D]) wrapHandler(w http.ResponseWriter, r *http.Request, next http.Handler) {
	var s *Session[D]
	sid, err := sm.getSIDCookie(r)
	if err != nil {
		// Regardless of the error reason, we'll create a pre-session below.
		if !errors.Is(err, errNoSIDCookie) {
			slog.Error("Failed to extract session cookie", "error", err)
		}
	} else if cs, err := sm.lookup(r.Context(), sid); err != nil {
		slog.Debug("Failed to look up session for SID", "sid", sid, "error", err)
	} else {
		s = cs
	}
	if s == nil {
		ps, err := sm.Create(r.Context(), w, nil)
		if err != nil {
			slog.Error("Failed to create session", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s = ps
	}
	ctx := context.WithValue(r.Context(), contextKeySession, s)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// Manage is a chi-compatible middleware that validates the session cookie,
// looks up the associated session data, and stores it to the request Context
// (which can be retrieved via GetSession).
// If no session cookie is present, a pre-session (i.e., one with nil Data
// payload) will be created. In other words, Manage ensures a session always
// exists (with an associated CSRF token).
func (sm *SessionManager[D]) Manage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.wrapHandler(w, r, next)
	})
}
