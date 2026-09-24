package apigraph

import (
	"net/http/httptest"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The language the dashboard is shown in wins over the browser's own, and
// a change of language or zone is kept at once rather than an hour later.
func TestTheAccountLearnsTheLanguageTheDashboardIsShownIn(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var user *models.User
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if user, err = tx.CreateUser(&models.User{Username: "location-reader", Name: "Alice Example"}); err != nil {
			t.Fatal(err)
		}
	})
	resolver := &graph{database: database}
	visit := func(language, acceptLanguage, timezone string) {
		request := httptest.NewRequest("POST", "/api/v1/graphql", nil)
		request.Header.Set(api.AuthenticatedUsernameHeader, user.Username)
		if language != "" {
			request.Header.Set(LanguageHeader, language)
		}
		request.Header.Set("Accept-Language", acceptLanguage)
		request.Header.Set(TimezoneHeader, timezone)
		resolver.touchLocation(request)
	}
	seen := func() *models.User {
		var found *models.User
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if found, err = tx.GetUserByUsername(user.Username); err != nil {
				t.Fatal(err)
			}
		})
		return found
	}

	visit("zh", "en-US,en;q=0.9", "America/New_York")
	if found := seen(); found.LocaleSeen != "zh" || found.Timezone != "America/New_York" {
		t.Fatalf("want zh in America/New_York, got %q in %q", found.LocaleSeen, found.Timezone)
	}
	visit("ja", "en-US,en;q=0.9", "Asia/Tokyo")
	if found := seen(); found.LocaleSeen != "ja" || found.Timezone != "Asia/Tokyo" {
		t.Fatalf("a change should be kept at once: got %q in %q", found.LocaleSeen, found.Timezone)
	}
	// A client that does not say its language falls back to the browser's.
	visit("", "fr-FR,fr;q=0.9", "Asia/Tokyo")
	if found := seen(); found.LocaleSeen != "fr-fr" {
		t.Fatalf("want the Accept-Language fallback, got %q", found.LocaleSeen)
	}
}
