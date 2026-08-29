package web_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/web"
)

// login attempts once from the given address and returns what came back.
func attemptLogin(t *testing.T, authenticator web.Authenticator, address, password string) error {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	request.RemoteAddr = address + ":40000"
	return authenticator.Login(httptest.NewRecorder(), request, "ziyan", password)
}

// Guessing has to stop being free. Without a limit, verifying a password is
// the only cost, and the attacker pays it in parallel while the server pays it
// serially.
func TestGuessingIsRefusedAfterTheBurst(t *testing.T) {
	t.Parallel()
	store := newStore(t, &config.User{ID: config.NewID(), Username: "ziyan", PasswordHash: testPasswordHash})
	authenticator, err := web.NewAuthenticator(store, newMemoryStore())
	if err != nil {
		t.Fatalf("failed to build an authenticator: %s", err)
	}

	var refused error
	for attempt := 1; attempt <= 200; attempt++ {
		err := attemptLogin(t, authenticator, "203.0.113.7", "not-the-password")
		if errors.Is(err, web.ErrTooManyAttempts) {
			refused = err
			t.Logf("refused after %d attempts", attempt)
			break
		}
		if !errors.Is(err, web.ErrInvalidCredentials) {
			t.Fatalf("attempt %d returned %v, want invalid credentials", attempt, err)
		}
	}
	if refused == nil {
		t.Fatal("two hundred wrong passwords from one address were all answered; nothing is limiting it")
	}
}

// The limit is per address, so one attacker cannot lock out everybody else.
func TestOneAddressBeingLimitedDoesNotLockOutAnother(t *testing.T) {
	t.Parallel()
	store := newStore(t, &config.User{ID: config.NewID(), Username: "ziyan", PasswordHash: testPasswordHash})
	authenticator, err := web.NewAuthenticator(store, newMemoryStore())
	if err != nil {
		t.Fatalf("failed to build an authenticator: %s", err)
	}

	for attempt := 1; attempt <= 200; attempt++ {
		if errors.Is(attemptLogin(t, authenticator, "203.0.113.7", "wrong"), web.ErrTooManyAttempts) {
			break
		}
	}
	if err := attemptLogin(t, authenticator, "203.0.113.8", "wrong"); errors.Is(err, web.ErrTooManyAttempts) {
		t.Error("a second address was refused because the first one was guessing")
	}
}

// The port varies per connection, so keying on it would count every attempt
// separately and limit nothing.
func TestThePortIsNotPartOfTheKey(t *testing.T) {
	t.Parallel()
	store := newStore(t, &config.User{ID: config.NewID(), Username: "ziyan", PasswordHash: testPasswordHash})
	authenticator, err := web.NewAuthenticator(store, newMemoryStore())
	if err != nil {
		t.Fatalf("failed to build an authenticator: %s", err)
	}

	var refused bool
	for attempt := 1; attempt <= 200; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
		request.RemoteAddr = fmt.Sprintf("203.0.113.9:%d", 40000+attempt)
		if errors.Is(authenticator.Login(httptest.NewRecorder(), request, "ziyan", "wrong"), web.ErrTooManyAttempts) {
			refused = true
			break
		}
	}
	if !refused {
		t.Error("changing the source port evaded the limit")
	}
}
