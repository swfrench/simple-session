package session_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/redis/go-redis/v9"
	session "github.com/swfrench/simple-session"
	"github.com/swfrench/simple-session/internal/testutil"
	"github.com/swfrench/simple-session/store"
)

type fakeSessionData struct {
	Greeting string `json:"greeting"`
}

type sessionRunner struct {
	sm         *session.SessionManager[fakeSessionData]
	ctxSession *session.Session[fakeSessionData]
	srv        *httptest.Server
	srvURL     *url.URL
	jar        http.CookieJar
	client     *http.Client
	handler    http.HandlerFunc
}

func mustCreateSessionRunner(t *testing.T, rc *redis.Client) *sessionRunner {
	sr := &sessionRunner{}
	rs := store.NewRedisStore[session.Session[fakeSessionData]](rc, "session")
	k := testutil.MustDecodeBase64(t, "W+HdoO687DHK7p/Uk933ojArElzkEMtRebhW07NFTgU=")
	opts := &session.Options{} // use default options
	sr.sm = session.NewSessionManager[fakeSessionData](rs, k, opts)
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

func (sr *sessionRunner) getSessionCookie() *http.Cookie {
	for _, cookie := range sr.jar.Cookies(sr.srvURL) {
		if cookie.Name == "session" {
			return cookie
		}
	}
	return nil
}

func TestCreatesPreSession(t *testing.T) {
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
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
	for _, attr := range []string{"HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(sch, attr) {
			t.Errorf("Expected Set-Cookie response header to include the %s attribute, got: %q", attr, sch)
		}
	}
}

func TestCreatesPreSessionOnce(t *testing.T) {
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
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

func TestReplacePreSession(t *testing.T) {
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
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
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
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
			t.Errorf("Create() returned unexpected error: %v", err)
		}
	}))

	// Verify that the new session cookie was provided to the client.
	sc2 := sr.getSessionCookie()
	if sc1.Value == sc2.Value {
		t.Errorf("Expected new session to produce a new SID - got %q again", sc1.Value)
	}
}

func TestExpiredSession(t *testing.T) {
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
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
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
	defer sr.close()

	now := time.Now()

	sr.sm.Clock = func() time.Time { return now }

	sr.run(t, nil)

	// Verify that the (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	token1 := sr.ctxSession.CSRFToken

	sr.sm.Clock = func() time.Time { return now.Add(time.Hour) }

	sr.run(t, nil)

	// Verify that the new (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	// Verify that the new token is indeed new.
	token2 := sr.ctxSession.CSRFToken
	if token1 == token2 {
		t.Errorf("Expected session expiration to produce a new CSRF token - got %q again", token1)
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
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
	defer sr.close()

	if got, want := sr.sm.GetSession(context.Background()), (*session.Session[fakeSessionData])(nil); got != want {
		t.Errorf("GetSession() returned unexpected value for empty context - got: %v want: %v", got, want)
	}
}

func TestUnkownSession(t *testing.T) {
	rb := testutil.MustCreateRedisBundle(t)
	defer rb.Close()

	sr := mustCreateSessionRunner(t, rb.Client())
	defer sr.close()

	sr.run(t, nil)

	// Verify that the (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	sid := sr.ctxSession.ID

	// Make Redis forget about the current session.
	rb.Flush()

	sr.run(t, nil)

	// Verify that the new (pre)session was provided to the request context.
	if sr.ctxSession == nil {
		t.Fatal("GetSession() returned nil Session within handler")
	}

	if sid == sr.ctxSession.ID {
		t.Errorf("Expected unknown session to produce a new SID - got %q again", sid)
	}
}
