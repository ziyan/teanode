package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// TestAuthenticationMiddleware covers what the middleware must let through and
// what it must stop. Getting the exemptions wrong either locks the operator
// out of their own login form or leaves the API open.
func TestAuthenticationMiddleware(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	var reachedHandler bool
	handler := web.MakeAuthenticationMiddleware(authenticator, "/.well-known/acme-challenge/")(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			reachedHandler = true
			// Echo back whoever the middleware says this is.
			_, _ = response.Write([]byte(request.Header.Get(api.AuthenticatedUsernameHeader)))
		}),
	)

	tests := []struct {
		name           string
		path           string
		withSession    bool
		wantStatusCode int
		wantHandler    bool
		wantUsername   string
	}{
		{
			name: "the session endpoint is reachable without logging in",
			path: api.PathGraphQL, wantStatusCode: http.StatusOK, wantHandler: true,
		},
		{
			// An application sends with an SMTP credential, which the handler
			// checks itself; it has no session and never will.
			name: "the send endpoint is reachable without logging in",
			path: api.Prefix + "/send/example.com/welcome", wantStatusCode: http.StatusOK, wantHandler: true,
		},
		{
			// A certificate authority arrives with no session and no cookie.
			name: "the acme challenge is reachable without logging in",
			path: "/.well-known/acme-challenge/sometoken", wantStatusCode: http.StatusOK, wantHandler: true,
		},
		{
			name: "the api is refused without a session",
			path: api.Prefix + "/nonsense", wantStatusCode: http.StatusUnauthorized, wantHandler: false,
		},
		{
			// The dashboard itself has to load so it can show a login form.
			name: "the dashboard loads without a session",
			path: "/mail", wantStatusCode: http.StatusOK, wantHandler: true,
		},
		{
			name: "the api is allowed with a session",
			path: api.Prefix + "/nonsense", withSession: true,
			wantStatusCode: http.StatusOK, wantHandler: true, wantUsername: "admin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reachedHandler = false

			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.withSession {
				request.AddCookie(login(t, authenticator, "admin", "hunter2"))
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatusCode {
				t.Errorf("status is %d, want %d", recorder.Code, test.wantStatusCode)
			}
			if reachedHandler != test.wantHandler {
				t.Errorf("reached handler = %v, want %v", reachedHandler, test.wantHandler)
			}
			if test.wantHandler && recorder.Body.String() != test.wantUsername {
				t.Errorf("handler saw username %q, want %q", recorder.Body.String(), test.wantUsername)
			}
		})
	}
}

// TestAuthenticationMiddlewareStripsForgedHeader is the reason the header is
// deleted on the way in: without that, anybody could simply send it.
func TestAuthenticationMiddlewareStripsForgedHeader(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	var seen string
	handler := web.MakeAuthenticationMiddleware(authenticator, "")(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			seen = request.Header.Get(api.AuthenticatedUsernameHeader)
		}),
	)

	// A public path, so the request is not refused and reaches the handler.
	request := httptest.NewRequest(http.MethodGet, api.PathGraphQL, nil)
	request.Header.Set(api.AuthenticatedUsernameHeader, "admin")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if seen != "" {
		t.Errorf("a client-supplied identity header survived as %q", seen)
	}
}

// TestAPIRepliesAreNotCacheable covers a bug that made a development server
// unusable: the accounts were cleared, and an iPhone went on showing a login
// form for a server that had none, because it had cached the session reply and
// never asked again. A 200 with no Cache-Control is heuristically cacheable,
// so the browser was within its rights.
func TestAPIRepliesAreNotCacheable(t *testing.T) {
	handler := web.NoStoreMiddleware(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		path string
		want string
	}{
		// Who you are, and everything that can change it.
		{api.PathGraphQL, "no-store"},
		{api.Prefix + "/send/example.com/welcome", "no-store"},
		// The dashboard's own assets are cached deliberately: the HTML is
		// no-store already, and the bundles are content hashed and immutable.
		// This middleware must not interfere with either.
		{"/", ""},
		{"/teanode.abcdef.js", ""},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))

			if got := recorder.Header().Get("Cache-Control"); got != test.want {
				t.Errorf("Cache-Control is %q, want %q", got, test.want)
			}
		})
	}
}

// TestAPublicPathStillIdentifiesTheCaller is the regression that broke logging
// in entirely.
//
// Making the GraphQL endpoint public — necessary, because logging in happens
// there — was done with an early return, which skipped establishing who the
// caller was as well as skipping the refusal. Every resolver behind it was
// then told nobody was there, so a reader who had just signed in successfully
// was answered "not logged in" by everything they clicked.
//
// Identifying the caller and deciding whether to refuse them are separate
// questions, and this pins them apart.
func TestAPublicPathStillIdentifiesTheCaller(t *testing.T) {
	store := newStore(t)
	authenticator, err := web.NewAuthenticator(store, newMemoryStore(&models.User{Username: "ziyan", PasswordHash: testPasswordHash}))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}

	var seen string
	handler := web.MakeAuthenticationMiddleware(authenticator, "")(
		http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			seen = request.Header.Get(api.AuthenticatedUsernameHeader)
		}),
	)

	// A session cookie, the way a browser would have one after logging in.
	recorder := httptest.NewRecorder()
	if err := authenticator.Login(recorder, httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil), "ziyan", "a-password"); err != nil {
		t.Fatalf("Login: %s", err)
	}

	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	for _, cookie := range recorder.Result().Cookies() {
		request.AddCookie(cookie)
	}
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if seen != "ziyan" {
		t.Errorf("the resolvers were told the caller is %q, want %q", seen, "ziyan")
	}

	// And without a cookie the same path is still reachable — that is what
	// makes it public — but carries nobody, so the resolvers refuse.
	seen = "unset"
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil))
	if seen != "" {
		t.Errorf("an anonymous caller was identified as %q", seen)
	}
}
