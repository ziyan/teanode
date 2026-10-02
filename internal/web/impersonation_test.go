package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/web"
)

// cookiesOf is the cookies a response set, by name.
func cookiesOf(recorder *httptest.ResponseRecorder) map[string]*http.Cookie {
	cookies := map[string]*http.Cookie{}
	for _, cookie := range recorder.Result().Cookies() {
		cookies[cookie.Name] = cookie
	}
	return cookies
}

// requestWith is a request carrying the given cookies.
func requestWith(cookies ...*http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	for _, cookie := range cookies {
		if cookie != nil && cookie.MaxAge >= 0 && cookie.Value != "" {
			request.AddCookie(cookie)
		}
	}
	return request
}

// impersonate signs the operator whose cookie this is in as username, and
// returns the new session cookie and the cookie keeping theirs.
func impersonate(t *testing.T, authenticator web.Authenticator, own *http.Cookie, username string) (*http.Cookie, *http.Cookie) {
	t.Helper()
	recorder := httptest.NewRecorder()
	if _, err := authenticator.StartImpersonation(recorder, requestWith(own), username); err != nil {
		t.Fatalf("StartImpersonation: %s", err)
	}
	cookies := cookiesOf(recorder)
	if cookies[web.SessionCookieName] == nil || cookies[web.ReturnCookieName] == nil {
		t.Fatalf("both cookies are set: %v", cookies)
	}
	return cookies[web.SessionCookieName], cookies[web.ReturnCookieName]
}

// An operator signed in as somebody is that person to every request, named
// as the one behind it, for no longer than an hour; ending it puts the
// operator's own session back, and the impersonation stops working.
func TestImpersonationActsAsThePersonAndReturnsTheOperator(t *testing.T) {
	t.Parallel()
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "bob", "hunter2")))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	own := login(t, authenticator, "admin", "hunter2")
	impersonation, returning := impersonate(t, authenticator, own, "bob")
	if returning.Value != own.Value {
		t.Fatal("the cookie kept aside is the operator's own")
	}

	identity, ok := authenticator.AuthenticateIdentity(requestWith(impersonation))
	if !ok || identity.Username != "bob" || identity.ImpersonatorUsername != "admin" {
		t.Fatalf("the request is bob's, with admin behind it: %+v %v", identity, ok)
	}
	if identity.ImpersonationEndsAt.After(time.Now().Add(web.ImpersonationLifetime + time.Minute)) {
		t.Fatalf("it ends within the hour: %s", identity.ImpersonationEndsAt)
	}
	sessions, err := authenticator.ListSessions("bob", false)
	if err != nil || len(sessions) != 1 || !sessions[0].IsImpersonation() {
		t.Fatalf("bob sees the session in his own list: %+v %v", sessions, err)
	}

	recorder := httptest.NewRecorder()
	restored, err := authenticator.EndImpersonation(recorder, requestWith(impersonation, returning))
	if err != nil || !restored {
		t.Fatalf("EndImpersonation: %v %v", restored, err)
	}
	cookies := cookiesOf(recorder)
	if cookies[web.SessionCookieName].Value != own.Value || cookies[web.ReturnCookieName].MaxAge >= 0 {
		t.Fatalf("the operator's cookie is back and the kept one cleared: %+v", cookies)
	}
	if _, ok := authenticator.AuthenticateIdentity(requestWith(impersonation)); ok {
		t.Fatal("an ended impersonation still authenticated")
	}
	if identity, ok := authenticator.AuthenticateIdentity(requestWith(own)); !ok || identity.Username != "admin" || identity.ImpersonatorUsername != "" {
		t.Fatalf("the operator is themselves again: %+v %v", identity, ok)
	}
}

// An impersonation answers to a live operator: it stops the moment their
// own session ends, or their account goes.
func TestImpersonationEndsWithTheOperator(t *testing.T) {
	t.Parallel()
	credentials := newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "bob", "hunter2"), newUser(t, "carol", "hunter2"))
	authenticator, err := web.NewAuthenticator(newStore(t), credentials)
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}

	own := login(t, authenticator, "admin", "hunter2")
	impersonation, _ := impersonate(t, authenticator, own, "bob")
	sessions, err := authenticator.ListSessions("admin", false)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions: %v %v", sessions, err)
	}
	if err := authenticator.RevokeSession("admin", sessions[0].ID); err != nil {
		t.Fatalf("RevokeSession: %s", err)
	}
	if _, ok := authenticator.AuthenticateIdentity(requestWith(impersonation)); ok {
		t.Fatal("the impersonation outlived the operator's own session")
	}

	own = login(t, authenticator, "admin", "hunter2")
	impersonation, _ = impersonate(t, authenticator, own, "carol")
	credentials.removeUser("admin")
	if _, ok := authenticator.AuthenticateIdentity(requestWith(impersonation)); ok {
		t.Fatal("the impersonation outlived the operator's account")
	}
}

// Only an ordinary session of the operator's own starts one: not an
// impersonation already, not a token, not nobody, and not oneself.
func TestImpersonationStartsOnlyFromTheOperatorsOwnSession(t *testing.T) {
	t.Parallel()
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "bob", "hunter2"), newUser(t, "carol", "hunter2")))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	own := login(t, authenticator, "admin", "hunter2")
	impersonation, _ := impersonate(t, authenticator, own, "bob")

	if _, err := authenticator.StartImpersonation(httptest.NewRecorder(), requestWith(impersonation), "carol"); err == nil {
		t.Fatal("an impersonation started another")
	}
	if _, err := authenticator.StartImpersonation(httptest.NewRecorder(), requestWith(), "carol"); err == nil {
		t.Fatal("a request with no session started one")
	}
	withToken := requestWith(own)
	withToken.Header.Set("Authorization", "Bearer something")
	if _, err := authenticator.StartImpersonation(httptest.NewRecorder(), withToken, "carol"); err == nil {
		t.Fatal("a request with a token started one")
	}
	if _, err := authenticator.StartImpersonation(httptest.NewRecorder(), requestWith(own), "admin"); err == nil {
		t.Fatal("the operator signed in as themselves")
	}
}

// Signing out of somebody else's account is coming back to one's own.
func TestLoggingOutOfAnImpersonationReturnsTheOperator(t *testing.T) {
	t.Parallel()
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "bob", "hunter2")))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	own := login(t, authenticator, "admin", "hunter2")
	impersonation, returning := impersonate(t, authenticator, own, "bob")

	recorder := httptest.NewRecorder()
	authenticator.Logout(recorder, requestWith(impersonation, returning))
	if cookie := cookiesOf(recorder)[web.SessionCookieName]; cookie == nil || cookie.Value != own.Value {
		t.Fatalf("the operator's cookie is back: %+v", cookie)
	}
	if _, ok := authenticator.AuthenticateIdentity(requestWith(impersonation)); ok {
		t.Fatal("the impersonation still authenticated after logging out of it")
	}
	if _, ok := authenticator.AuthenticateIdentity(requestWith(own)); !ok {
		t.Fatal("the operator's own session was ended with it")
	}
}

// The middleware hands the operator behind an impersonation to the API in a
// header of its own, and a client cannot set that header itself.
func TestTheMiddlewareNamesTheOperatorAndStripsAForgedOne(t *testing.T) {
	t.Parallel()
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "bob", "hunter2")))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	var seenUsername, seenImpersonator string
	handler := web.MakeAuthenticationMiddleware(authenticator, "", nil)(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			seenUsername = request.Header.Get(api.AuthenticatedUsernameHeader)
			seenImpersonator = request.Header.Get(api.ImpersonatorUsernameHeader)
		}),
	)
	own := login(t, authenticator, "admin", "hunter2")
	impersonation, _ := impersonate(t, authenticator, own, "bob")

	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	request.AddCookie(impersonation)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if seenUsername != "bob" || seenImpersonator != "admin" {
		t.Fatalf("the request is bob's with admin behind it: %q %q", seenUsername, seenImpersonator)
	}

	forged := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
	forged.AddCookie(own)
	forged.Header.Set(api.ImpersonatorUsernameHeader, "bob")
	handler.ServeHTTP(httptest.NewRecorder(), forged)
	if seenUsername != "admin" || seenImpersonator != "" {
		t.Fatalf("a client-supplied operator header survived: %q %q", seenUsername, seenImpersonator)
	}
}
