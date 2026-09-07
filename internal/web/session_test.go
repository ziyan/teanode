package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
	"github.com/ziyan/teanode/internal/web"
)

// newStore writes a configuration with the given dashboard users and opens a
// store over it.
// testPasswordHash is a bcrypt hash of a password nothing logs in with. It
// exists because the configuration refuses a user whose hash is not one.
var testPasswordHash = func() string {
	hash, err := security.HashPassword("a-password")
	if err != nil {
		panic(err)
	}
	return string(hash)
}()

// identifierOf is what the credential store keys by. The tests name accounts
// by username, because that is what a person does; the store stores the
// identifier, because a username can change.
func identifierOf(t *testing.T, credentials *memoryStore, username string) string {
	t.Helper()

	user, err := credentials.GetUserByUsername(username)
	if err != nil || user == nil {
		t.Fatalf("no account called %q", username)
	}
	return user.ID
}

func newStore(t *testing.T) config.Store {
	t.Helper()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "teanode1.key"), []byte("not a real key"), 0o600); err != nil {
		t.Fatalf("failed to write the key file: %s", err)
	}

	configuration := config.Example()
	configuration.Server.DataDirectory = directory

	store := config.NewMemoryStore(configuration)
	t.Cleanup(func() { _ = store.Close() })

	// The server does this on startup; the session key lives in the
	// configuration rather than in a file of its own.
	if err := config.EnsureSecrets(store); err != nil {
		t.Fatalf("failed to generate secrets: %s", err)
	}
	return store
}

func newUser(t *testing.T, username, password string) *models.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash the password: %s", err)
	}
	return &models.User{ID: config.NewID(), Username: username, PasswordHash: string(hash)}
}

// login performs a login and returns the session cookie it set.
func login(t *testing.T, authenticator web.Authenticator, username, password string) *http.Cookie {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	if err := authenticator.Login(recorder, request, username, password); err != nil {
		t.Fatalf("failed to log in: %s", err)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == web.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("no session cookie was set")
	return nil
}

func TestLoginAndAuthenticate(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	if !authenticator.Required() {
		t.Error("authentication should be required when a user is configured")
	}

	// Without a cookie there is no session.
	if _, ok := authenticator.Authenticate(httptest.NewRequest(http.MethodGet, "/api/mail", nil)); ok {
		t.Error("a request with no cookie authenticated")
	}

	cookie := login(t, authenticator, "admin", "hunter2")

	request := httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	request.AddCookie(cookie)
	username, ok := authenticator.Authenticate(request)
	if !ok {
		t.Fatal("a request with a valid session did not authenticate")
	}
	if username != "admin" {
		t.Errorf("authenticated as %q, want admin", username)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	tests := []struct{ name, username, password string }{
		{"wrong password", "admin", "wrong"},
		{"unknown user", "somebody", "hunter2"},
		{"empty username", "", "hunter2"},
		{"empty password", "admin", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			if err := authenticator.Login(recorder, request, test.username, test.password); err == nil {
				t.Error("login succeeded when it should not have")
			}
			if len(recorder.Result().Cookies()) != 0 {
				t.Error("a cookie was set for a failed login")
			}
		})
	}
}

// TestSessionCannotBeForged is the property the whole scheme rests on.
//
// It used to be about editing: the cookie carried a username and an expiry, so
// the test proved neither could be rewritten. The cookie names a row now, so
// there is nothing in it to edit into another identity — and what has to hold
// instead is that a value nobody issued cannot be made to name one.
func TestSessionCannotBeForged(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	credentials := newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "other", "hunter2"))
	authenticator, err := web.NewAuthenticator(store, credentials)
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	cookie := login(t, authenticator, "admin", "hunter2")
	if !strings.HasPrefix(cookie.Value, web.SessionPrefix) {
		t.Fatalf("a session cookie should be recognisable as one: %q", cookie.Value)
	}

	// The identifier is the readable half, and is not a secret: it appears in
	// the log and in the session list. Knowing it must not be enough.
	sessions, err := credentials.ListSessions(identifierOf(t, credentials, "admin"), nil)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("expected one session, got %v (%v)", sessions, err)
	}
	id := sessions[0].ID

	forgeries := map[string]string{
		"a changed character":     tamper(cookie.Value),
		"the identifier alone":    web.SessionPrefix + id,
		"the identifier and junk": web.SessionPrefix + id + strings.Repeat("a", 32),
		"no prefix":               strings.TrimPrefix(cookie.Value, web.SessionPrefix),
		"another prefix":          web.TokenPrefix + strings.TrimPrefix(cookie.Value, web.SessionPrefix),
		"empty":                   "",
		"rubbish":                 "not-a-session",
	}
	for name, value := range forgeries {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
			request.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: value})
			if username, ok := authenticator.Authenticate(request); ok {
				t.Errorf("a forged session authenticated as %q", username)
			}
		})
	}

	// And the real one still works, so the checks above are refusing
	// forgeries rather than everything.
	request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	request.AddCookie(cookie)
	if username, ok := authenticator.Authenticate(request); !ok || username != "admin" {
		t.Errorf("the issued session did not authenticate: %q, %v", username, ok)
	}
}

// TestASessionCookieIsNotAToken covers the domain separation in the signature.
// The two are the same shape, and a session that could be presented as a token
// would be a way around whatever either is allowed to do.
func TestASessionCookieIsNotAToken(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	authenticator, err := web.NewAuthenticator(store, newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	cookie := login(t, authenticator, "admin", "hunter2")
	presented := web.TokenPrefix + strings.TrimPrefix(cookie.Value, web.SessionPrefix)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	request.Header.Set("Authorization", "Bearer "+presented)
	if username, ok := authenticator.Authenticate(request); ok {
		t.Errorf("a session cookie was accepted as an API token, as %q", username)
	}
}

// TestRevokingASessionEndsIt is what the table is for: one browser can be
// signed out without touching the others.
func TestRevokingASessionEndsIt(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	credentials := newMemoryStore(newUser(t, "admin", "hunter2"))
	authenticator, err := web.NewAuthenticator(store, credentials)
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	one := login(t, authenticator, "admin", "hunter2")
	two := login(t, authenticator, "admin", "hunter2")

	sessions, err := authenticator.ListSessions("admin", false)
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %d", len(sessions))
	}

	// Ending the first must leave the second alone, which is the whole
	// difference from rotating a signing key.
	if err := authenticator.RevokeSession("admin", sessions[1].ID); err != nil {
		t.Fatalf("RevokeSession: %s", err)
	}

	for name, expected := range map[*http.Cookie]bool{one: false, two: true} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
		request.AddCookie(name)
		if _, ok := authenticator.Authenticate(request); ok != expected {
			t.Errorf("session authenticated=%v, want %v", ok, expected)
		}
	}

	// The revoked row stays, so the list can say what happened to it.
	withRevoked, err := authenticator.ListSessions("admin", true)
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(withRevoked) != 2 {
		t.Errorf("expected the revoked session to still be listed, got %d", len(withRevoked))
	}
}

// TestRevokingAnotherAccountsSessionIsRefused: a session identifier is not a
// secret, so naming one must not be enough to end it.
func TestRevokingAnotherAccountsSessionIsRefused(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	authenticator, err := web.NewAuthenticator(store, newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "other", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	cookie := login(t, authenticator, "admin", "hunter2")
	sessions, err := authenticator.ListSessions("admin", false)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("expected one session, got %v (%v)", sessions, err)
	}

	if err := authenticator.RevokeSession("other", "01nope"); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("revoking an unknown session: got %v, want ErrNotFound", err)
	}
	if err := authenticator.RevokeSession("other", sessions[0].ID); !errors.Is(err, api.ErrNotFound) {
		t.Error("one account ended another's session")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	request.AddCookie(cookie)
	if _, ok := authenticator.Authenticate(request); !ok {
		t.Error("the session was ended by an account that does not own it")
	}
}

// TestLogoutEndsTheSession: clearing the cookie is not enough on its own,
// because whoever has a copy of it would still be signed in.
func TestLogoutEndsTheSession(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	authenticator, err := web.NewAuthenticator(store, newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	cookie := login(t, authenticator, "admin", "hunter2")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	request.AddCookie(cookie)
	authenticator.Logout(httptest.NewRecorder(), request)

	// The same cookie, presented again as a copy would be.
	replay := httptest.NewRequest(http.MethodGet, "/api/v1/graphql", nil)
	replay.AddCookie(cookie)
	if username, ok := authenticator.Authenticate(replay); ok {
		t.Errorf("a logged-out session still authenticated as %q", username)
	}
}

func TestSessionExpires(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Session.Lifetime = config.Duration(time.Second)
		return nil
	}); err != nil {
		t.Fatalf("failed to shorten the session lifetime: %s", err)
	}

	authenticator, err := web.NewAuthenticator(store, newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}
	cookie := login(t, authenticator, "admin", "hunter2")

	// Rewrite the expiry to the past and re-sign nothing: an expired session
	// with a valid signature must still be refused.
	request := httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	request.AddCookie(cookie)
	if _, ok := authenticator.Authenticate(request); !ok {
		t.Fatal("a fresh session did not authenticate")
	}

	time.Sleep(1100 * time.Millisecond)
	if _, ok := authenticator.Authenticate(request); ok {
		t.Error("an expired session still authenticated")
	}
}

// TestSessionStopsWhenUserRemoved covers revocation: taking a user out of the
// configuration has to end their session at once, not when the cookie expires.
func TestSessionStopsWhenUserRemoved(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	credentials := newMemoryStore(newUser(t, "admin", "hunter2"), newUser(t, "leaver", "hunter2"))
	authenticator, err := web.NewAuthenticator(store, credentials)
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	cookie := login(t, authenticator, "leaver", "hunter2")
	request := httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	request.AddCookie(cookie)
	if _, ok := authenticator.Authenticate(request); !ok {
		t.Fatal("the session did not authenticate before removal")
	}

	credentials.removeUser("leaver")

	if _, ok := authenticator.Authenticate(request); ok {
		t.Error("a removed user's session still authenticated")
	}
}

// TestNoUsersConfiguredIsOpen documents the deliberate escape hatch: with no
// dashboard users the API is open, which is only reasonable bound to
// localhost, and the server warns about it at startup.
func TestNoUsersConfiguredIsOpen(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore())
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}
	if authenticator.Required() {
		t.Error("authentication should not be required with no users configured")
	}
	if _, ok := authenticator.Authenticate(httptest.NewRequest(http.MethodGet, "/api/mail", nil)); !ok {
		t.Error("a request was refused with no users configured")
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	t.Parallel()

	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(newUser(t, "admin", "hunter2")))
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	recorder := httptest.NewRecorder()
	authenticator.Logout(recorder, httptest.NewRequest(http.MethodPost, "/api/logout", nil))

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name != web.SessionCookieName {
			continue
		}
		if cookie.Value != "" || cookie.MaxAge >= 0 {
			t.Errorf("logout did not clear the cookie: value=%q maxAge=%d", cookie.Value, cookie.MaxAge)
		}
		return
	}
	t.Error("logout set no cookie")
}

// TestCreateFirstUserClaimsTheServer covers the first run: a server with no
// account lets the first arrival choose one, and then never again.
func TestCreateFirstUserClaimsTheServer(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	credentials := newMemoryStore()
	authenticator, err := web.NewAuthenticator(store, credentials)
	if err != nil {
		t.Fatalf("failed to create the authenticator: %s", err)
	}

	if authenticator.Required() {
		t.Fatal("a fresh server should not require authentication yet")
	}

	if err := authenticator.CreateFirstUser(context.Background(), "ziyan", "a-proper-long-password"); err != nil {
		t.Fatalf("failed to create the first account: %s", err)
	}

	if !authenticator.Required() {
		t.Error("the server should require authentication once claimed")
	}

	// The account has to reach the store, or it is lost on restart. That it
	// survives being written and read back is covered where that actually
	// happens, in internal/db.
	stored, err := credentials.GetUserByUsername("ziyan")
	if err != nil || stored == nil {
		t.Fatalf("the account was not stored: %v", err)
	}
	if stored.PasswordHash == "a-proper-long-password" {
		t.Fatal("the password was stored in the clear")
	}
	// And the first person is an administrator: the roles and groups were
	// seeded around them.
	if len(credentials.groups) == 0 || len(credentials.groups[0].UserIDs) == 0 {
		t.Error("claiming the server did not make the first account an administrator")
	}

	// It has to actually work for logging in.
	cookie := login(t, authenticator, "ziyan", "a-proper-long-password")
	request := httptest.NewRequest(http.MethodGet, "/api/mail", nil)
	request.AddCookie(cookie)
	if username, ok := authenticator.Authenticate(request); !ok || username != "ziyan" {
		t.Errorf("the new account cannot log in: ok=%v username=%q", ok, username)
	}

	// A second claim must be refused, or anybody could add themselves.
	if err := authenticator.CreateFirstUser(context.Background(), "attacker", "another-long-password"); !errors.Is(err, web.ErrAccountExists) {
		t.Errorf("a second claim returned %v, want ErrAccountExists", err)
	}
}

func TestCreateFirstUserRejectsUnusableAccounts(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, username, password string }{
		{"no username", "", "a-password"},
		{"username with a space", "two words", "a-password"},
		{"no password", "ziyan", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore())
			if err != nil {
				t.Fatalf("failed to create the authenticator: %s", err)
			}
			if err := authenticator.CreateFirstUser(context.Background(), test.username, test.password); !errors.Is(err, web.ErrInvalidAccount) {
				t.Errorf("got %v, want ErrInvalidAccount", err)
			}
			if authenticator.Required() {
				t.Error("a rejected account should leave the server unclaimed")
			}
		})
	}
}

// TestBearerTokenAuthentication covers the credential the command line client
// uses, both kinds: one stored in the token table and one minted from the
// server secret.
func TestBearerTokenAuthentication(t *testing.T) {
	store := newStore(t)
	if err := store.Update(func(configuration *config.Configuration) error {
		configuration.Server.Secret = "a-server-secret-long-enough"
		return nil
	}); err != nil {
		t.Fatalf("Update: %s", err)
	}

	credentials := newMemoryStore()
	authenticator, err := web.NewAuthenticator(store, credentials)
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	if err := authenticator.CreateFirstUser(context.Background(), "ziyan", "a-password"); err != nil {
		t.Fatalf("CreateFirstUser: %s", err)
	}
	credentials.addUser(&models.User{Username: "temporary", PasswordHash: testPasswordHash})

	_, ownedSecret, err := authenticator.IssueToken("ziyan", "laptop", 0)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	_, expiredSecret, err := authenticator.IssueToken("ziyan", "old", time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	// Wound back so it is already past, which the store lets a test do and
	// the API does not.
	expireNow(t, credentials, identifierOf(t, credentials, "ziyan"), "old")

	// A token belonging to an account that is later removed has to stop
	// working, which used to follow from tokens living inside the account and
	// now has to be checked for.
	_, orphanedSecret, err := authenticator.IssueToken("temporary", "gone", 0)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}

	revoked, revokedSecret, err := authenticator.IssueToken("ziyan", "revoked", 0)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	if err := authenticator.RevokeToken("ziyan", revoked.ID); err != nil {
		t.Fatalf("RevokeToken: %s", err)
	}
	// A token that is not there, and one that is somebody else's, are the
	// same "not found": revoking must not be a way of listing identifiers.
	if err := authenticator.RevokeToken("ziyan", "01nope"); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("revoking an unknown token: got %v, want ErrNotFound", err)
	}
	orphaned, _, err := authenticator.IssueToken("temporary", "theirs", 0)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	if err := authenticator.RevokeToken("ziyan", orphaned.ID); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("revoking somebody else's token: got %v, want ErrNotFound", err)
	}

	local, err := store.Current().MintLocalToken(time.Minute)
	if err != nil {
		t.Fatalf("MintLocalToken: %s", err)
	}

	tests := []struct {
		name         string
		header       string
		wantUsername string
		wantOk       bool
	}{
		{"a token belonging to an account acts as them", "Bearer " + ownedSecret, "ziyan", true},
		{"the scheme is case insensitive", "bearer " + ownedSecret, "ziyan", true},
		{"a token acts as the account holding it", "Bearer " + orphanedSecret, "temporary", true},
		{"a locally minted token acts as the console", "Bearer " + local, config.LocalUsername, true},
		{"an expired token is refused", "Bearer " + expiredSecret, "", false},
		{"a revoked token is refused", "Bearer " + revokedSecret, "", false},

		{"a tampered secret is refused", "Bearer " + tamper(ownedSecret), "", false},
		{"an unknown identifier is refused", "Bearer " + web.TokenPrefix + "nosuchid_secret", "", false},
		{"a malformed token is refused", "Bearer not-a-token", "", false},
		{"another scheme is refused", "Basic " + ownedSecret, "", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
			request.Header.Set("Authorization", test.header)

			username, ok := authenticator.Authenticate(request)
			if ok != test.wantOk {
				t.Fatalf("authenticated = %v, want %v", ok, test.wantOk)
			}
			if username != test.wantUsername {
				t.Errorf("username is %q, want %q", username, test.wantUsername)
			}
		})
	}
}

// TestBearerTokenDoesNotFallBackToTheSession makes sure a client that sent a
// token is told the token is wrong, rather than being quietly served by
// whoever's session cookie happened to be on the request.
func TestBearerTokenDoesNotFallBackToTheSession(t *testing.T) {
	store := newStore(t)
	authenticator, err := web.NewAuthenticator(store, newMemoryStore())
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	if err := authenticator.CreateFirstUser(context.Background(), "ziyan", "a-password"); err != nil {
		t.Fatalf("CreateFirstUser: %s", err)
	}

	recorder := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	if err := authenticator.Login(recorder, loginRequest, "ziyan", "a-password"); err != nil {
		t.Fatalf("Login: %s", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	for _, cookie := range recorder.Result().Cookies() {
		request.AddCookie(cookie)
	}
	request.Header.Set("Authorization", "Bearer "+web.TokenPrefix+"nosuchid_secret")

	if username, ok := authenticator.Authenticate(request); ok {
		t.Errorf("a bad token was accepted as %q because a session cookie was present", username)
	}
}

// tamper changes the last character of a credential, which has to be enough to
// make it fail: the signature covers the whole of it.
func tamper(value string) string {
	if value == "" {
		return value
	}
	last := value[len(value)-1]
	replacement := byte('a')
	if last == 'a' {
		replacement = 'b'
	}
	return value[:len(value)-1] + string(replacement)
}

// expireNow winds a token's expiry into the past, which the store allows and
// the API deliberately does not.
func expireNow(t *testing.T, store *memoryStore, userId, name string) {
	t.Helper()

	tokens, err := store.ListTokens(userId, nil)
	if err != nil {
		t.Fatalf("ListTokens: %s", err)
	}
	for _, token := range tokens {
		if token.Name != name {
			continue
		}
		store.mutex.Lock()
		store.tokens[token.ID].token.ExpiresAt = time.Now().Add(-time.Hour)
		store.mutex.Unlock()
		return
	}
	t.Fatalf("no token called %q", name)
}
