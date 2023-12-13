// Package session provides helpers for managing user sessions.
//
// At a high level, Manager manages the creation of time-bounded Session
// instances, which are in turn stored to a SessionStore. The Session is imbued
// with an arbitrary Data payload, which can be used to store user session
// details (e.g., identity).
//
// At Session creation, Manager will set a session token cookie to that refers
// to the Session ID.
//
// Sessions also contain an assocated CSRF token, which can be used in CSRF
// protections (e.g., hidden form fields).
//
// Both tokens (session ID and CSRF) are authenticated (currently HMAC-SHA256)
// using separate keys.
//
// The general principle is that HTTP handlers that must be Session-aware will
// use the Manage middleware. The latter ensures that a Session always exists,
// and defaults to a pre-session - i.e., one with nil associated Data. This
// ensures that CSRF protection is always possible.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/swfrench/simple-session/internal/retry"
	"github.com/swfrench/simple-session/internal/token"
	"github.com/swfrench/simple-session/store"
	"golang.org/x/crypto/hkdf"
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

const contextKeySession = contextKey("session")

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

// Options represents tunable knobs that control the behavior of Manager.
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
	// SessionCookieName is the name of the session ID cookie set by Manager.
	// For example, together with a suitable definition of CreateCookie (see
	// below), this can be used to configure a secure cookie name prefix (e.g.,
	// "__Host-").
	// Default if unspecified: "session"
	SessionCookieName string
	// CreateCookie is a user-supplied factory for creating session ID cookies
	// with the provided name, value, and expiration. This is provided as a
	// convenience for granular control of cookie attributes, such as Path.
	// Default if unspecified: CreateStrictCookie
	CreateCookie func(name, value string, expires time.Time) *http.Cookie
	// OnCreate is a user-supplied callback invoked on session creation, with
	// the associated ResponseWriter instance and newly created Session. May be
	// used, e.g., to inject a CSRF Token cookie into the response.
	// Default if unspecified: nil, in which case OnCreate is not invoked.
	OnCreate func(w http.ResponseWriter, session any)
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

// Manager manages user sessions (i.e., Session instances).
type Manager[D any] struct {
	// Clock can be used to override measurement of time in tests.
	Clock func() time.Time
	store store.SessionStore[Session[D]]
	opts  *Options
	sta   *token.Authenticator
	cta   *token.Authenticator
}

func deriveKeys(ikm []byte, infos []string) ([][]byte, error) {
	var keys [][]byte
	prk := hkdf.Extract(sha256.New, ikm, nil)
	for _, info := range infos {
		key := make([]byte, 32)
		if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, []byte(info)), key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// NewManager returns a new Manager using the provided store for session storage
// and respecting the provided options.
// Session IDs and associated CSRF tokens are authenticated (HMAC-SHA256) using
// keys derived from the provided initial key.
func NewManager[D any](s store.SessionStore[Session[D]], key []byte, opts *Options) (*Manager[D], error) {
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
	keys, err := deriveKeys(key, []string{"session-token", "csrf-token"})
	if err != nil {
		return nil, err
	}
	return &Manager[D]{
		Clock: func() time.Time { return time.Now() },
		store: s,
		opts:  opts,
		sta:   token.NewAuthenticator(keys[0]),
		cta:   token.NewAuthenticator(keys[1]),
	}, nil
}

var errExpiredSession = errors.New("expired session")

func (m *Manager[D]) lookup(ctx context.Context, sid string) (*Session[D], error) {
	s, err := m.store.Get(ctx, sid)
	if err != nil {
		return nil, err
	}
	if s.Expiration.Before(m.Clock()) {
		return nil, errExpiredSession
	}
	return s, nil
}

func (m *Manager[D]) createID() ([]byte, error) {
	data := make([]byte, m.opts.IDLen)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	return data, nil
}

func (m *Manager[D]) createSessionToken() (string, error) {
	data, err := m.createID()
	if err != nil {
		return "", err
	}
	return m.sta.Create(data), nil
}

func (m *Manager[D]) createCSRFToken() (string, error) {
	data, err := m.createID()
	if err != nil {
		return "", err
	}
	return m.cta.Create(data), nil
}

// Create creates a new Session with the provided Data payload, storing the
// session to the SessionStore and setting the associated SID cookie.
func (m *Manager[D]) Create(ctx context.Context, w http.ResponseWriter, data *D) (*Session[D], error) {
	var s *Session[D]
	fn := func(rctx *retry.RetryContext) {
		// create(Session|CSRF)Token may fail if there is insufficient entropy
		// available, in which case, it makes sense to backoff and retry.
		id, err := m.createSessionToken()
		if err != nil {
			slog.Error("Failed to generate Session ID token", "error", err)
			return
		}
		csrf, err := m.createCSRFToken()
		if err != nil {
			slog.Error("Failed to generate CSRF token", "error", err)
			return
		}
		snew := &Session[D]{
			ID:         id,
			Data:       data,
			Expiration: m.Clock().Add(m.opts.TTL),
			CSRFToken:  csrf,
		}
		// Set may fail if there is a session collision, the backing store is
		// unavailable, or snew cannot be marshalled for storage.
		if err := m.store.Set(ctx, id, snew, m.opts.TTL+sessionStorageGracePeriod); err != nil {
			if !errors.Is(err, store.ErrSessionExists) {
				slog.Error("Failed to store new Session", "error", err)
			}
			if errors.Is(err, store.ErrInvalidSessionData) {
				// This suggests that type D cannot be marshalled by the
				// underlying store, which retry cannot address.
				rctx.Abort()
			}
			return
		}
		s = snew
		rctx.Done()
	}
	// Max 4 attempts, with inter-attempt delay ~100ms, ~200ms, ~400ms (+/- 20%).
	err := retry.Backoff{Base: 100 * time.Millisecond, Growth: 2.0, Jitter: 0.2}.Do(fn, 4)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %v", err)
	}
	m.setSIDCookie(w, s.ID)
	if m.opts.OnCreate != nil {
		m.opts.OnCreate(w, s)
	}
	return s, nil
}

// Clear creates a new pre-session (i.e., a Session with no Data payload) and
// attempts to delete the prior session from the SessionStore. The former is
// stored to the SessionStore and its ID set in the SID cookie, and it is also
// returned. Deletion of the old session is considered non-critical (i.e.,
// unexpected errors are merely logged).
func (m *Manager[D]) Clear(ctx context.Context, w http.ResponseWriter, sid string) (*Session[D], error) {
	ps, err := m.Create(ctx, w, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create new pre-session: %w", err)
	}
	if err := m.store.Del(ctx, sid); err != nil && !errors.Is(err, store.ErrSessionNotFound) {
		slog.Error("Failed to delete data for session", "sid", sid)
	}
	return ps, nil
}

func (m *Manager[D]) setSIDCookie(w http.ResponseWriter, sid string) {
	expires := m.Clock().Add(m.opts.TTL + sessionCookieGracePeriod)
	http.SetCookie(w, m.opts.CreateCookie(m.opts.SessionCookieName, sid, expires))
}

var errNoSIDCookie = errors.New("no SID cookie")

// getSIDCookie fetches the SID cookie from the provided request and verifies its authenticity.
func (m *Manager[D]) getSIDCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(m.opts.SessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", errNoSIDCookie
		}
		return "", err
	}
	if _, err := m.sta.Verify(c.Value); err != nil {
		return "", err
	}
	return c.Value, nil
}

// VerifySessionCSRFToken verifies the authenticity of the provided CSRF token
// and that it matches the expected value for the provided Session.
func (m *Manager[D]) VerifySessionCSRFToken(token string, s *Session[D]) error {
	if _, err := m.cta.Verify(token); err != nil {
		return fmt.Errorf("failed to validate CSRF token: %w", err)
	}
	if token != s.CSRFToken {
		return fmt.Errorf("CSRF token %q does not match session-bound token %q", token, s.CSRFToken)
	}
	return nil
}

func (m *Manager[D]) wrapHandler(w http.ResponseWriter, r *http.Request, next http.Handler) {
	var s *Session[D]
	sid, err := m.getSIDCookie(r)
	if err != nil {
		// Regardless of the error reason, we'll create a pre-session below.
		if !errors.Is(err, errNoSIDCookie) {
			slog.Error("Failed to extract session cookie", "error", err)
		}
	} else if cs, err := m.lookup(r.Context(), sid); err != nil {
		slog.Debug("Failed to look up session for SID", "sid", sid, "error", err)
	} else {
		s = cs
	}
	if s == nil {
		ps, err := m.Create(r.Context(), w, nil)
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
// (which can be retrieved via Get).
// If no session cookie is present, a pre-session (i.e., one with nil Data
// payload) will be created. In other words, Manage ensures a session always
// exists (with an associated CSRF token).
func (m *Manager[D]) Manage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.wrapHandler(w, r, next)
	})
}

// Get returns the Session object instance stored in the provided request
// Context by the Manage middleware.
func Get[D any](ctx context.Context) *Session[D] {
	s := ctx.Value(contextKeySession)
	if s == nil {
		return nil
	}
	return s.(*Session[D])
}
