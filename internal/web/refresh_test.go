package web_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/web"
)

// Two renewals of the same token: exactly one retires it.
//
// Checking the refresh secret and retiring the token are two steps, so a
// retry, or a stolen copy racing the real program, could both pass the check.
// Whoever retires the token is the one that goes on; the other has to be told
// it lost, which it can only be if retiring says who did it.
func TestOnlyOneRenewalRetiresAToken(t *testing.T) {
	t.Parallel()

	user := newUser(t, "someone", "a-password-for-a-test")
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(user))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	token, _, _, err := authenticator.IssueAuthorizedToken(user.ID, "a program", "a-client", "/api/v1/mcp", time.Hour)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}

	var retired atomic.Int32
	var waiting sync.WaitGroup
	for range 8 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if did, err := authenticator.RevokeTokenByID(token.ID); err == nil && did {
				retired.Add(1)
			}
		}()
	}
	waiting.Wait()
	if count := retired.Load(); count != 1 {
		t.Errorf("%d callers retired the same token", count)
	}
}

// A token whose access has run out can still be renewed, and a retired one
// cannot.
//
// Many programs renew only after a request has been refused, so refusing an
// expired token here meant approving the program again every month.
func TestAnExpiredTokenCanBeRenewedAndARetiredOneCannot(t *testing.T) {
	t.Parallel()

	user := newUser(t, "someone", "a-password-for-a-test")
	authenticator, err := web.NewAuthenticator(newStore(t), newMemoryStore(user))
	if err != nil {
		t.Fatalf("NewAuthenticator: %s", err)
	}
	token, _, refresh, err := authenticator.IssueAuthorizedToken(user.ID, "a program", "a-client", "/api/v1/mcp", time.Nanosecond)
	if err != nil {
		t.Fatalf("IssueAuthorizedToken: %s", err)
	}
	time.Sleep(time.Millisecond)

	renewed, err := authenticator.RedeemRefresh(refresh)
	if err != nil || renewed == nil || renewed.ID != token.ID {
		t.Fatalf("an expired token could not be renewed: %v", err)
	}

	if _, err := authenticator.RevokeTokenByID(token.ID); err != nil {
		t.Fatalf("RevokeTokenByID: %s", err)
	}
	if _, err := authenticator.RedeemRefresh(refresh); err == nil {
		t.Error("a retired token was renewed")
	}
}
