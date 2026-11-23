package apigraph

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A page's picture on a card is served to the card's person, from the address
// the card names, and to nobody else: another person and a request that is not
// signed in are turned away, and a picture not fetched yet has no address.
func TestACardsPictureIsServedOnlyToItsPerson(t *testing.T) {
	world := newAvatarWorld(t)
	world.router.Path(api.PathFeedMedia).Methods(http.MethodGet, http.MethodHead).HandlerFunc(world.graph.feedMediaView)
	if err := world.files.PutFile(t.Context(), "feed-picture-test", []byte("a kept picture")); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	var item *models.AgentFeedItem
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agent, err := tx.CreateAgent(&models.Agent{UserID: world.person.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if item, _, err = tx.UpsertAgentFeedItem(&models.AgentFeedItem{
			AgentID: agent.ID, UserID: world.person.ID, Title: "A story", FeedItemKind: models.FeedItemNews, FeedSourceKind: models.FeedSourceAgent,
			FeedMedia: []models.FeedMedia{
				{MediaKey: "fetched", FeedMediaKind: models.FeedMediaPicture, PageURL: "https://example.com/story", ImageFileID: "feed-picture-test", WidthPixels: 1200, HeightPixels: 675},
				{MediaKey: "waiting", FeedMediaKind: models.FeedMediaPicture, PageURL: "https://example.com/other"},
			},
		}, time.Now()); err != nil {
			t.Fatalf("UpsertAgentFeedItem: %s", err)
		}
	})
	views := feedMediaViews(item)
	if len(views) != 1 || views[0].ImageURL != api.FeedMediaPath(item.ID, "fetched") || views[0].LinkURL != "https://example.com/story" {
		t.Fatalf("the card's media read %+v, expected only the fetched picture with its page", views)
	}

	served := world.fetch(world.person, views[0].ImageURL)
	if served.Code != http.StatusOK || served.Body.String() != "a kept picture" || served.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("the card's person was answered %d %q", served.Code, served.Body.String())
	}
	if other := world.fetch(world.other, views[0].ImageURL); other.Code != http.StatusNotFound {
		t.Errorf("another person was answered %d, want 404", other.Code)
	}
	signedOut := httptest.NewRecorder()
	world.router.ServeHTTP(signedOut, httptest.NewRequest(http.MethodGet, views[0].ImageURL, nil))
	if signedOut.Code != http.StatusUnauthorized {
		t.Errorf("a request not signed in was answered %d, want 401", signedOut.Code)
	}
	if waiting := world.fetch(world.person, api.FeedMediaPath(item.ID, "waiting")); waiting.Code != http.StatusNotFound {
		t.Errorf("a picture not fetched yet was answered %d, want 404", waiting.Code)
	}
}
