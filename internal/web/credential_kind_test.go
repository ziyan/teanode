package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/web"
)

// A request says what it signed in with, because the operations that only a
// person's tap may make (lending money, approving a call that spends it)
// have to tell the dashboard's session and the phone app from a command line
// token, which is what an agent holds when it drives the person's computer. A
// token is the phone app's only when its row says so; a client that was
// authorized, or one minted by hand, is a token like any other.
func TestARequestSaysWhatItSignedInWith(t *testing.T) {
	t.Parallel()

	user := newUser(t, "someone", "a-password-for-a-test")
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(user))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	handByHand, byHand, err := authenticator.IssueToken("someone", "laptop", time.Hour)
	if err != nil || handByHand == nil {
		t.Fatalf("IssueToken: %v", err)
	}
	_, byProgram, _, err := authenticator.IssueAuthorizedToken(user.ID, "a program", "a-client", "/api/v1/mcp", "", time.Hour)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}
	_, byUnmarkedApp, _, err := authenticator.IssueAuthorizedToken(user.ID, "an app", "an-app", "", "", time.Hour)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}
	_, byApp, _, err := authenticator.IssueAuthorizedToken(user.ID, "an app", "an-app", "", models.ClientKindApp, time.Hour)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}
	cookie := login(t, authenticator, "someone", "a-password-for-a-test")

	bearer := func(value string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, api.PathAgentMCP, nil)
		request.Header.Set("Authorization", "Bearer "+value)
		return request
	}
	graph := func(value string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
		request.Header.Set("Authorization", "Bearer "+value)
		return request
	}
	for name, test := range map[string]struct {
		request  *http.Request
		expected models.CredentialKind
	}{
		"a token minted by hand":                  {graph(byHand), models.CredentialToken},
		"a token a program was authorized for":    {bearer(byProgram), models.CredentialToken},
		"an app's token with no marker":           {graph(byUnmarkedApp), models.CredentialToken},
		"an app's token the app's sign-in marked": {graph(byApp), models.CredentialApp},
		"the dashboard's session":                 {requestWith(cookie), models.CredentialSession},
	} {
		identity, ok := authenticator.AuthenticateIdentity(test.request)
		if !ok || identity.Username != "someone" || identity.CredentialKind != test.expected {
			t.Errorf("%s: %+v (%v), expected %q", name, identity, ok, test.expected)
		}
	}
}

// What the middleware tells the handlers is the credential it established, and
// a client cannot say it for itself.
func TestTheMiddlewareSetsTheCredentialKindAndStripsAForgedOne(t *testing.T) {
	t.Parallel()

	user := newUser(t, "someone", "a-password-for-a-test")
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(user))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	_, byHand, err := authenticator.IssueToken("someone", "laptop", time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	_, byApp, _, err := authenticator.IssueAuthorizedToken(user.ID, "an app", "an-app", "", models.ClientKindApp, time.Hour)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}
	cookie := login(t, authenticator, "someone", "a-password-for-a-test")

	var seen models.CredentialKind
	handler := web.MakeAuthenticationMiddleware(authenticator, "", nil)(
		http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			seen = api.CredentialKindFromRequest(request)
		}),
	)
	forged := func(request *http.Request) *http.Request {
		request.Header.Set(api.CredentialKindHeader, string(models.CredentialApp))
		return request
	}
	withToken := func(value string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, nil)
		request.Header.Set("Authorization", "Bearer "+value)
		return request
	}
	for name, test := range map[string]struct {
		request  *http.Request
		expected models.CredentialKind
	}{
		"a command line token claiming to be the app": {forged(withToken(byHand)), models.CredentialToken},
		"the app":                          {withToken(byApp), models.CredentialApp},
		"a session claiming to be the app": {forged(requestWith(cookie)), models.CredentialSession},
		"nobody claiming to be the app":    {forged(httptest.NewRequest(http.MethodGet, api.PathGraphQL, nil)), ""},
	} {
		seen = "unset"
		handler.ServeHTTP(httptest.NewRecorder(), test.request)
		if seen != test.expected {
			t.Errorf("%s: the handler saw %q, expected %q", name, seen, test.expected)
		}
	}
}
