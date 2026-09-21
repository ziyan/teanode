package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// TestSessionRoundTrip covers what the authenticator does on every request,
// against the real column types rather than a map.
func TestSessionRoundTrip(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	ziyan := dbtest.CreateUser(t, database, "ziyan")

	expires := time.Now().Add(720 * time.Hour)
	created, err := database.CreateSession(&models.Session{
		ID:        security.NewULID(),
		UserID:    ziyan,
		ExpiresAt: expires,
		UsedAt:    time.Now(),
		IP:        "192.0.2.1",
		UserAgent: "a browser",
	}, "a-key-hash")
	if err != nil {
		t.Fatalf("CreateSession: %s", err)
	}

	found, keyHash, err := database.GetSession(created.ID)
	if err != nil {
		t.Fatalf("GetSession: %s", err)
	}
	if found == nil {
		t.Fatal("the session was not stored")
	}
	if keyHash != "a-key-hash" {
		t.Errorf("the key hash came back as %q", keyHash)
	}
	if found.UserID != ziyan || found.IP != "192.0.2.1" || found.UserAgent != "a browser" {
		t.Errorf("the session came back changed: %+v", found)
	}
	// Compared to the second: PostgreSQL keeps microseconds and the value
	// went through a time zone on the way.
	if found.ExpiresAt.Sub(expires) > time.Second || expires.Sub(found.ExpiresAt) > time.Second {
		t.Errorf("the expiry came back as %s, want %s", found.ExpiresAt, expires)
	}
	if !found.Active(time.Now()) {
		t.Error("a session that has not expired should be active")
	}

	// An identifier nobody issued is not an error, it is an absence — the
	// authenticator asks about whatever was in a cookie.
	missing, _, err := database.GetSession(security.NewULID())
	if err != nil {
		t.Errorf("looking up an unknown session should not be an error: %s", err)
	}
	if missing != nil {
		t.Error("an unknown identifier returned a session")
	}
}

// TestTouchSessionIsThrottled: the guard is in the WHERE clause, so that two
// instances touching the same session do not both write and a flush that lost
// a race cannot move the column backwards.
func TestTouchSessionIsThrottled(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	ziyan := dbtest.CreateUser(t, database, "ziyan")

	created, err := database.CreateSession(&models.Session{
		ID:        security.NewULID(),
		UserID:    ziyan,
		ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash")
	if err != nil {
		t.Fatalf("CreateSession: %s", err)
	}

	now := time.Now()
	if err := database.TouchSession(created.ID, now, "192.0.2.1", "one"); err != nil {
		t.Fatalf("TouchSession: %s", err)
	}

	// A second touch a moment later is inside the window and must not write.
	if err := database.TouchSession(created.ID, now.Add(time.Second), "192.0.2.9", "two"); err != nil {
		t.Fatalf("TouchSession: %s", err)
	}
	found, _, err := database.GetSession(created.ID)
	if err != nil {
		t.Fatalf("GetSession: %s", err)
	}
	if found.UserAgent != "one" {
		t.Errorf("a touch inside the window wrote anyway: %q", found.UserAgent)
	}

	// Past the window it writes.
	if err := database.TouchSession(created.ID, now.Add(2*db.TouchInterval), "192.0.2.9", "two"); err != nil {
		t.Fatalf("TouchSession: %s", err)
	}
	found, _, err = database.GetSession(created.ID)
	if err != nil {
		t.Fatalf("GetSession: %s", err)
	}
	if found.UserAgent != "two" {
		t.Errorf("a touch past the window did not write: %q", found.UserAgent)
	}
}

// TestRevokeAndList covers the list the settings page shows, including that a
// revoked session stays visible for a while rather than vanishing.
func TestRevokeAndList(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	ziyan := dbtest.CreateUser(t, database, "ziyan")
	somebodyElse := dbtest.CreateUser(t, database, "somebody-else")

	var ids []string
	for index := 0; index < 3; index++ {
		created, err := database.CreateSession(&models.Session{
			ID:        security.NewULID(),
			UserID:    ziyan,
			ExpiresAt: time.Now().Add(time.Hour),
		}, "a-key-hash")
		if err != nil {
			t.Fatalf("CreateSession: %s", err)
		}
		ids = append(ids, created.ID)
	}
	// Another account's session, which must never appear in the list above.
	if _, err := database.CreateSession(&models.Session{
		ID: security.NewULID(), UserID: somebodyElse, ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateSession: %s", err)
	}

	if err := database.RevokeSession(ids[0], time.Now()); err != nil {
		t.Fatalf("RevokeSession: %s", err)
	}

	active, err := database.ListSessions(ziyan, nil)
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(active) != 2 {
		t.Errorf("expected two active sessions, got %d", len(active))
	}

	all, err := database.ListSessions(ziyan, &db.SessionOptions{IncludeRevoked: true})
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(all) != 3 {
		t.Errorf("expected the revoked one to still be listed, got %d", len(all))
	}

	// Ending them all, except the one making the request.
	ended, err := database.RevokeSessionsByUser(ziyan, time.Now(), ids[1])
	if err != nil {
		t.Fatalf("RevokeSessionsByUser: %s", err)
	}
	if ended != 1 {
		t.Errorf("expected to end one, ended %d", ended)
	}
	remaining, err := database.ListSessions(ziyan, nil)
	if err != nil {
		t.Fatalf("ListSessions: %s", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[1] {
		t.Errorf("the kept session is wrong: %+v", remaining)
	}
}

// TestScavenge removes what nobody is looking at any more, and keeps what
// somebody might be.
func TestScavenge(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	ziyan := dbtest.CreateUser(t, database, "ziyan")

	longGone := security.NewULID()
	if _, err := database.CreateSession(&models.Session{
		ID: longGone, UserID: ziyan, ExpiresAt: time.Now().Add(-30 * 24 * time.Hour),
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateSession: %s", err)
	}
	// Expired, but only just: kept for a day so somebody logged out by an
	// expiry can see that is what happened.
	recent := security.NewULID()
	if _, err := database.CreateSession(&models.Session{
		ID: recent, UserID: ziyan, ExpiresAt: time.Now().Add(-time.Minute),
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateSession: %s", err)
	}
	live := security.NewULID()
	if _, err := database.CreateSession(&models.Session{
		ID: live, UserID: ziyan, ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash"); err != nil {
		t.Fatalf("CreateSession: %s", err)
	}

	removed, err := database.ScavengeSessions(time.Now())
	if err != nil {
		t.Fatalf("ScavengeSessions: %s", err)
	}
	if removed != 1 {
		t.Errorf("expected to remove one, removed %d", removed)
	}

	for id, want := range map[string]bool{longGone: false, recent: true, live: true} {
		found, _, err := database.GetSession(id)
		if err != nil {
			t.Fatalf("GetSession: %s", err)
		}
		if (found != nil) != want {
			t.Errorf("session %s present=%v, want %v", id, found != nil, want)
		}
	}
}

// TestTokenRoundTrip is the same for tokens, which differ in having a name and
// in usually having no expiry at all.
func TestTokenRoundTrip(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	ziyan := dbtest.CreateUser(t, database, "ziyan")

	created, err := database.CreateToken(&models.Token{
		ID:     security.NewULID(),
		UserID: ziyan,
		Name:   "laptop",
	}, "a-key-hash")
	if err != nil {
		t.Fatalf("CreateToken: %s", err)
	}

	found, keyHash, err := database.GetToken(created.ID)
	if err != nil {
		t.Fatalf("GetToken: %s", err)
	}
	if found == nil || found.Name != "laptop" || keyHash != "a-key-hash" {
		t.Fatalf("the token came back wrong: %+v (%q)", found, keyHash)
	}
	if !found.ExpiresAt.IsZero() {
		t.Errorf("a token with no expiry should come back with none, got %s", found.ExpiresAt)
	}
	if !found.Active(time.Now()) {
		t.Error("a token with no expiry should be active")
	}
	if !found.UsedAt.IsZero() {
		t.Errorf("a token that has never been used should say so, got %s", found.UsedAt)
	}

	if err := database.RevokeToken(created.ID, time.Now()); err != nil {
		t.Fatalf("RevokeToken: %s", err)
	}
	found, _, err = database.GetToken(created.ID)
	if err != nil {
		t.Fatalf("GetToken: %s", err)
	}
	if found.Active(time.Now()) {
		t.Error("a revoked token is not active")
	}
}
