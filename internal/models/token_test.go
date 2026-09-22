package models

import (
	"testing"
	"time"
)

// A program's token can be renewed after its access runs out, for a while.
//
// Many programs only renew once a request has been refused, so refusing to
// renew an expired token meant approving the program again every month. One
// nobody has run for the whole window has been forgotten and is not renewed.
func TestATokenCanBeRenewedForAWhileAfterItsAccessRunsOut(test *testing.T) {
	now := time.Now()
	for _, check := range []struct {
		describe string
		token    Token
		want     bool
	}{
		{"still active", Token{ExpiresAt: now.Add(time.Hour)}, true},
		{"expired an hour ago", Token{ExpiresAt: now.Add(-time.Hour)}, true},
		{"expired just inside the window", Token{ExpiresAt: now.Add(-RefreshWindow + time.Hour)}, true},
		{"expired beyond the window", Token{ExpiresAt: now.Add(-RefreshWindow - time.Hour)}, false},
		{"revoked", Token{ExpiresAt: now.Add(time.Hour), RevokedAt: now.Add(-time.Minute)}, false},
		{"never expires", Token{}, true},
	} {
		if got := check.token.Refreshable(now); got != check.want {
			test.Errorf("%s: renewable was %v", check.describe, got)
		}
	}
}
