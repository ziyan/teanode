package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A sign-in begun in one transaction is found by another with its code, and an
// expired one is removed when the next begins.
func TestAgentProviderSignInCreateGetDelete(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	if err := database.SetSecret([]byte("a-server-secret-for-the-test")); err != nil {
		t.Fatalf("SetSecret: %s", err)
	}
	now := time.Now()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, signIn := range []*db.AgentProviderSignIn{
			{ID: "expired-sign-in", ProviderName: "plan", UserCode: "AAAA-0000", DeviceAuthID: "an-old-device-id", ExpiresAt: now.Add(-time.Minute)},
			{ID: "waiting-sign-in", ProviderName: "plan", UserCode: "BBBB-1111", VerificationAddress: "https://sign-in.example/device",
				DeviceAuthID: "a-device-id", PollInterval: 5 * time.Second, ExpiresAt: now.Add(10 * time.Minute)},
		} {
			if err := tx.CreateAgentProviderSignIn(signIn, now.Add(-time.Hour)); err != nil {
				t.Fatalf("CreateAgentProviderSignIn: %s", err)
			}
		}
	})
	// Beginning another removes the one that expired.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.CreateAgentProviderSignIn(&db.AgentProviderSignIn{ID: "next-sign-in", ProviderName: "plan", UserCode: "CCCC-2222", DeviceAuthID: "another-device-id", ExpiresAt: now.Add(time.Minute)}, now); err != nil {
			t.Fatalf("CreateAgentProviderSignIn: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.GetAgentProviderSignIn("expired-sign-in"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("an expired sign-in is removed, got %v", err)
		}
		found, err := tx.GetAgentProviderSignIn("waiting-sign-in")
		if err != nil {
			t.Fatalf("GetAgentProviderSignIn: %s", err)
		}
		if found.UserCode != "BBBB-1111" || found.DeviceAuthID != "a-device-id" || found.PollInterval != 5*time.Second ||
			found.VerificationAddress != "https://sign-in.example/device" || found.ProviderName != "plan" {
			t.Errorf("found %+v", found)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.DeleteAgentProviderSignIn("waiting-sign-in"); err != nil {
			t.Fatalf("DeleteAgentProviderSignIn: %s", err)
		}
		if err := tx.DeleteAgentProviderSignIn("waiting-sign-in"); err != nil {
			t.Errorf("deleting twice: %s", err)
		}
		if _, err := tx.GetAgentProviderSignIn("waiting-sign-in"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a deleted sign-in is found, got %v", err)
		}
	})
}

// The access token stored for a provider is read back by another transaction,
// replaced by the next, kept apart from the sign-ins waiting for a code, and
// not removed with the expired ones.
func TestAgentProviderAccessTokenPutGet(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	now := time.Now()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.GetAgentProviderAccessToken("plan"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("none stored yet, got %v", err)
		}
		for _, accessToken := range []string{"the-first-access-token", "the-second-access-token"} {
			if err := tx.PutAgentProviderAccessToken(&db.AgentProviderAccessToken{
				ProviderName: "plan", AccessToken: accessToken, ForRefreshToken: "a-refresh-token", ExpiresAt: now.Add(-time.Hour),
			}); err != nil {
				t.Fatalf("PutAgentProviderAccessToken: %s", err)
			}
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		// Beginning a sign-in removes the expired ones, which this is not.
		if err := tx.CreateAgentProviderSignIn(&db.AgentProviderSignIn{ID: "a-sign-in", ProviderName: "plan", UserCode: "DDDD-3333", DeviceAuthID: "a-device-id", ExpiresAt: now.Add(time.Minute)}, now); err != nil {
			t.Fatalf("CreateAgentProviderSignIn: %s", err)
		}
		found, err := tx.GetAgentProviderAccessToken("plan")
		if err != nil {
			t.Fatalf("GetAgentProviderAccessToken: %s", err)
		}
		if found.AccessToken != "the-second-access-token" || found.ForRefreshToken != "a-refresh-token" || found.ExpiresAt.Unix() != now.Add(-time.Hour).Unix() {
			t.Errorf("found %+v", found)
		}
		if _, err := tx.GetAgentProviderAccessToken("another-plan"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("another provider's is found, got %v", err)
		}
	})
}
