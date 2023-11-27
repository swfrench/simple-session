package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	session "github.com/swfrench/simple-session"
	"github.com/swfrench/simple-session/internal/testutil"
	"github.com/swfrench/simple-session/store"
)

// fakeSessionData is the fake data payload type used in tests below.
type fakeSessionData struct {
	Greeting string `json:"greeting"`
}

// stubStore is a stub implementation of the SessionStore interface.
type stubStore[S any] struct {
	sessions map[string]*S
	getErr   func() error
	setErr   func() error
	delErr   func() error
}

func newStubStore[S any]() *stubStore[S] {
	return &stubStore[S]{
		sessions: make(map[string]*S),
		getErr:   func() error { return nil },
		setErr:   func() error { return nil },
		delErr:   func() error { return nil },
	}
}

func (s *stubStore[S]) Get(ctx context.Context, sid string) (*S, error) {
	if err := s.getErr(); err != nil {
		return nil, err
	}
	sess, ok := s.sessions[sid]
	if !ok {
		return nil, store.ErrSessionNotFound
	}
	return sess, nil
}

func (s *stubStore[S]) Set(ctx context.Context, sid string, sess *S, ttl time.Duration) error {
	if err := s.setErr(); err != nil {
		return err
	}
	if _, ok := s.sessions[sid]; ok {
		return store.ErrSessionExists
	}
	s.sessions[sid] = sess
	return nil
}

func (s *stubStore[S]) Del(ctx context.Context, sid string) error {
	if err := s.delErr(); err != nil {
		return err
	}
	delete(s.sessions, sid)
	return nil
}

// Secure must be false, as we do not configure TLS on our httptest.Server.
func createNotSecureCookie(name, value string, expires time.Time) *http.Cookie {
	base := session.CreateStrictCookie(name, value, expires)
	base.Secure = false
	return base
}

func sessionOptions() *session.Options {
	opts := &session.Options{}
	opts.CreateCookie = createNotSecureCookie
	return opts
}

type sessionRunner struct {
	store      *stubStore[session.Session[fakeSessionData]]
	sm         *session.SessionManager[fakeSessionData]
	ctxSession *session.Session[fakeSessionData]
	srv        *httptest.Server
	srvURL     *url.URL
	jar        http.CookieJar
	client     *http.Client
	handler    http.HandlerFunc
}

func mustCreateSessionRunner(t *testing.T, opts *session.Options) *sessionRunner {
	sr := new(sessionRunner)
	sr.store = newStubStore[session.Session[fakeSessionData]]()
	k := testutil.MustDecodeBase64(t, "W+HdoO687DHK7p/Uk933ojArElzkEMtRebhW07NFTgU=")
	sr.sm = session.NewSessionManager[fakeSessionData](sr.store, k, opts)
	sr.srv = httptest.NewServer(sr.sm.Manage(http.HandlerFunc(sr.handle)))
	var err error
	sr.srvURL, err = url.Parse(sr.srv.URL)
	if err != nil {
		t.Fatalf("url.Parse() returned unexpected error: %v", err)
	}
	sr.jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New() returned unexpected error: %v", err)
	}
	sr.client = &http.Client{Jar: sr.jar}
	return sr
}

func (sr *sessionRunner) close() {
	sr.srv.Close()
}

func (sr *sessionRunner) handle(w http.ResponseWriter, r *http.Request) {
	sr.ctxSession = sr.sm.GetSession(r.Context())
	if sr.handler != nil {
		sr.handler(w, r)
	}
	w.WriteHeader(http.StatusTeapot)
}

func (sr *sessionRunner) run(t *testing.T, h http.HandlerFunc) *http.Response {
	sr.handler = h
	r, err := http.NewRequest("GET", sr.srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() returned unexpected error: %v", err)
	}
	resp, err := sr.client.Do(r)
	if err != nil {
		t.Fatalf("Client.Do() returned unexpected error: %v", err)
	}
	return resp
}

func (sr *sessionRunner) getSessionCookieByName(name string) *http.Cookie {
	for _, cookie := range sr.jar.Cookies(sr.srvURL) {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func (sr *sessionRunner) getSessionCookie() *http.Cookie {
	return sr.getSessionCookieByName("session")
}

func TestCreatesPreSession(t *testing.T) {
	// Emulate the recommended pattern of overriding CreateCookie to inject
	// customized attributes, to verify they're respected.
	opts := sessionOptions()
	opts.CreateCookie = func(name, value string, expires time.Time) *http.Cookie {
		base := createNotSecureCookie(name, value, expires)
		base.Path = "/"
		return base
	}
	sr := mustCreateSessionRunner(t, opts)
	defer sr.close()

	resp := sr.run(t, nil)

	// Verify that the (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	// Verify that the session cookie was provided to the client and is
	// consistent with the context session.
	sc := sr.getSessionCookie()
	if sc == nil {
		t.Fatal("Session cookie missing from response")
	}
	if got, want := sc.Value, sr.ctxSession.ID; got != want {
		t.Errorf("Expected session cookie value to match Context session SID - got: %q want: %q", got, want)
	}

	// Verify that the Set-Cookie response header has the appropriate attributes
	// set. Note: Jar does not preserve fields other than Name and Value.
	sch := resp.Header.Get("Set-Cookie")
	for _, attr := range []string{"HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(sch, attr) {
			t.Errorf("Expected Set-Cookie response header to include the %s attribute, got: %q", attr, sch)
		}
	}
}

func TestCustomSessionCookieName(t *testing.T) {
	opts := sessionOptions()
	opts.SessionCookieName = "a-very-good-cookie-name"
	sr := mustCreateSessionRunner(t, opts)
	defer sr.close()

	sr.run(t, nil)

	// Verify that the (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	// Verify that the session cookie was provided to the client with the
	// correct name and is consistent with the context session.
	sc := sr.getSessionCookieByName("a-very-good-cookie-name")
	if sc == nil {
		t.Fatal("Session cookie missing from response")
	}
	if got, want := sc.Value, sr.ctxSession.ID; got != want {
		t.Errorf("Expected session cookie value to match Context session SID - got: %q want: %q", got, want)
	}
}

func TestOnCreateCallback(t *testing.T) {
	createdSID := new(string)
	opts := sessionOptions()
	opts.OnCreate = func(w http.ResponseWriter, s any) {
		w.Header().Add("X-The-Cow-Says", "moo")
		*createdSID = s.(*session.Session[fakeSessionData]).ID
	}
	sr := mustCreateSessionRunner(t, opts)
	defer sr.close()

	resp := sr.run(t, nil)

	// Verify that the session cookie was provided to the client, with a value
	// matching that captured by the OnCreate callback.
	sc := sr.getSessionCookie()
	if sc == nil {
		t.Fatal("Session cookie missing from response")
	}
	if got, want := sc.Value, *createdSID; got != want {
		t.Errorf("Expected session cookie value to match Context session SID - got: %q want: %q", got, want)
	}

	// Verify that response header modifications by the OnCreate callback were
	// retained.
	if got, want := resp.Header.Get("X-The-Cow-Says"), "moo"; got != want {
		t.Errorf("Expected custom header set by OnCreate to be preserved - got: %q want: %q", got, want)
	}
}

func TestCreatesPreSessionOnce(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	// Verify that the session cookie does not change on the next request, and
	// further that there was no duplicate Set-Cookie header.
	resp := sr.run(t, nil)
	sc2 := sr.getSessionCookie()
	if got, want := sc2.Value, sc1.Value; got != want {
		t.Errorf("Unexpected change in session cookie value on second request - got: %q want: %q", got, want)
	}
	sch := resp.Header.Get("Set-Cookie")
	if got, want := sch, ""; got != want {
		t.Errorf("Set-Cookie header unexpectedly present on second request: %s", sch)
	}
}

func TestCreatesNewPreSessionWhenNotFound(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.store.getErr = func() error { return store.ErrSessionNotFound }

	// Verify that the session cookie changes on the next request (i.e., a new
	// session is created).
	sr.run(t, nil)
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Unexpected value for session cookie - got %q again", sc1.Value)
	}
}

func TestCreatesNewPreSessionWhenLookupFails(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.store.getErr = func() error { return errors.New("badger") }

	// Verify that the session cookie changes on the next request (i.e., a new
	// session is created).
	sr.run(t, nil)
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Unexpected value for session cookie - got %q again", sc1.Value)
	}
}

func TestCreatePreSessionFailsOnPersistentSetError(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.store.setErr = func() error { return errors.New("gremlins") }

	// Verify that persistent Set errors result in failed session creation and a
	// 503 response. Further, no cookie is provided to the client.
	resp := sr.run(t, nil)
	if got, want := resp.StatusCode, http.StatusInternalServerError; got != want {
		t.Errorf("Session creation under persistent Set error returned incorrect status code - got: %d want: %d", got, want)
	}
	if got := sr.getSessionCookie(); got != nil {
		t.Errorf("Session creation under persistent Set error produced a session cooke - got: %v want: %v", got, nil)
	}
}

func TestCreatePreSessionSucceedsOnTransientSetError(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	setErr := errors.New("gremlins")
	sr.store.setErr = func() error {
		var err error
		err, setErr = setErr, nil
		return err
	}

	// Verify that a transient Set error results in successful session creation.
	// Further, the session cookie is provided to the client as expected.
	resp := sr.run(t, nil)
	if got, want := resp.StatusCode, http.StatusTeapot; got != want {
		t.Errorf("Session creation under transient Set error returned incorrect status code - got: %d want: %d", got, want)
	}
	sc := sr.getSessionCookie()
	if sc == nil {
		t.Error("Session cookie missing from response")
	}
}

func TestCreatesPreSessionWhenSessionCookieIsInvalid(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	// Pre-populate the cookie jar with an invalid session cookie.
	const invalid = "nope"
	c := sessionOptions().CreateCookie("session", invalid, time.Now().Add(time.Hour))
	sr.jar.SetCookies(sr.srvURL, []*http.Cookie{c})

	sr.run(t, nil)

	// Grab the session cookie known to the client.
	sc := sr.getSessionCookie()
	if sc == nil {
		t.Fatal("Session cookie missing from response")
	}

	// Verify that the session cookie is not our invalid one.
	if sc.Value == invalid {
		t.Errorf("Unexpected value for session cookie - got %q again", invalid)
	}
}

func TestReplacePreSession(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	data := &fakeSessionData{Greeting: "hola"}

	// Run the next request, now creating a new session with a distinct data
	// payload.
	var cs *session.Session[fakeSessionData]
	sr.run(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := sr.sm.Create(context.Background(), w, data)
		if err != nil {
			t.Errorf("Create() returned unexpected error: %v", err)
			return
		}
		cs = s
	}))
	if cs == nil {
		t.Fatal("No session cookie created")
	}

	// Verify that the new session cookie was provided to the client.
	sc2 := sr.getSessionCookie()
	if got, want := sc2.Value, cs.ID; got != want {
		t.Errorf("Unexpected session cookie value on second request - got: %q want: %q", got, want)
	}
	if sc1.Value == sc2.Value {
		t.Errorf("Expected new session to produce a new SID - got %q again", sc1.Value)
	}

	// Verify that the session cookie does not change on the next request, and
	// further that there was no duplicate Set-Cookie header.
	resp := sr.run(t, nil)
	sc3 := sr.getSessionCookie()
	if got, want := sc3.Value, sc2.Value; got != want {
		t.Errorf("Unexpected change in session cookie value on second request - got: %q want: %q", got, want)
	}
	sch := resp.Header.Get("Set-Cookie")
	if got, want := sch, ""; got != want {
		t.Errorf("Set-Cookie header unexpectedly present on second request: %s", sch)
	}

	// Finally, verify that the most recently observed context session data
	// returns the expected value.
	if diff := cmp.Diff(data, sr.ctxSession.Data); diff != "" {
		t.Errorf("Context session did not reproduce the expected session data payload (+got, -want):\n%s", diff)
	}
}

func TestClearSession(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	// Run the next request, clearing the prior session.
	sr.run(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := sr.sm.GetSession(r.Context())
		if _, err := sr.sm.Clear(r.Context(), w, s.ID); err != nil {
			t.Errorf("Clear() returned unexpected error: %v", err)
		}
	}))

	// Verify that the new session cookie was provided to the client.
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Expected new session to produce a new SID - got %q again", sc1.Value)
	}
}

func TestClearSessionSucceedsOnDelError(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.store.delErr = func() error { return errors.New("gremlins") }

	// Run the next request, attempting to clear the prior session.
	sr.run(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := sr.sm.GetSession(r.Context())
		if _, err := sr.sm.Clear(r.Context(), w, s.ID); err != nil {
			t.Errorf("Clear() returned unexpected error: %v", err)
		}
	}))

	// Verify that the new session cookie was provided to the client.
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Expected new session to produce a new SID - got %q again", sc1.Value)
	}
}

func TestClearSessionFailsOnSetError(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.store.setErr = func() error { return errors.New("gremlins") }

	// Run the next request, attempting to clear the prior session.
	sr.run(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := sr.sm.GetSession(r.Context())
		if _, err := sr.sm.Clear(r.Context(), w, s.ID); err == nil {
			t.Errorf("Clear() should have returned error - got: %v", err)
		}
	}))

	// Verify that no new session cookie was provided to the client.
	if got, want := sr.getSessionCookie().Value, sc1.Value; got != want {
		t.Errorf("Expected no change in session cookie SID - got: %q want: %q", got, want)
	}
}

func TestClearSessionSucceedsOnSessionNotFound(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.store.delErr = func() error { return store.ErrSessionNotFound }

	// Run the next request, clearing the prior session.
	sr.run(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := sr.sm.GetSession(r.Context())
		if _, err := sr.sm.Clear(r.Context(), w, s.ID); err != nil {
			t.Errorf("Clear() returned unexpected error: %v", err)
		}
	}))

	// Verify that the new session cookie was provided to the client.
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Expected new session to produce a new SID - got %q again", sc1.Value)
	}
}

func TestExpiredSession(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	now := time.Now()

	sr.sm.Clock = func() time.Time { return now }

	sr.run(t, nil)

	// Verify that the session cookie was provided to the client.
	sc1 := sr.getSessionCookie()
	if sc1 == nil {
		t.Fatal("Session cookie missing from response")
	}

	sr.sm.Clock = func() time.Time { return now.Add(time.Hour) }

	sr.run(t, nil)

	// Verify that the new session cookie was provided to the client, and it was
	// seen in the request context.
	sc2 := sr.getSessionCookie()
	if got, want := sc2.Value, sr.ctxSession.ID; got != want {
		t.Errorf("Unexpected session cookie value on second request - got: %q want: %q", got, want)
	}
	if sc1.Value == sc2.Value {
		t.Errorf("Expected session expiration to produce a new SID - got %q again", sc1.Value)
	}
}

func TestVerifySessionCSRFToken(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	now := time.Now()

	sr.sm.Clock = func() time.Time { return now }

	sr.run(t, nil)

	// Verify that the (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	token1 := sr.ctxSession.CSRFToken

	// Advance the clock to the point where the previous session will have
	// expired, such that a new one is created (yielding a new CSRF token).
	sr.sm.Clock = func() time.Time { return now.Add(time.Hour) }

	sr.run(t, nil)

	// Verify that the new (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	// Verify that the new token is indeed new.
	token2 := sr.ctxSession.CSRFToken
	if token1 == token2 {
		t.Fatalf("Expected session expiration to produce a new CSRF token - got %q again", token1)
	}

	testCases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:  "succeeds",
			token: sr.ctxSession.CSRFToken,
		},
		{
			name:    "structurally invalid token",
			token:   "nope",
			wantErr: true,
		},
		{
			name:    "structurally valid unauthenticated token",
			token:   "u0h0nzzuYTZwZfZ3p/FvSbDSYZ37Ihd2Q0hVfbanQSQ=.KtiBFS1u64w=",
			wantErr: true,
		},
		{
			name:    "stale token",
			token:   token1,
			wantErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := sr.sm.VerifySessionCSRFToken(tc.token, sr.ctxSession)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("VerifySessionCSRFToken() returned incorrect error - got: %t want: %t (error: %v)", gotErr, tc.wantErr, err)
			}
		})
	}
}

func TestGetSessionWithEmptyContext(t *testing.T) {
	sr := mustCreateSessionRunner(t, sessionOptions())
	defer sr.close()

	if got, want := sr.sm.GetSession(context.Background()), (*session.Session[fakeSessionData])(nil); got != want {
		t.Errorf("GetSession() returned unexpected value for empty context - got: %v want: %v", got, want)
	}
}
