package agent_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

// feedPictureSite is a web site whose pages each name a picture, counting the
// pages read.
type feedPictureSite struct {
	*httptest.Server
	pageVisits atomic.Int32
}

func newFeedPictureSite(t *testing.T) *feedPictureSite {
	t.Helper()
	site := &feedPictureSite{}
	site.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, ".png"):
			canvas := image.NewRGBA(image.Rect(0, 0, 1600, 900))
			for row := 0; row < 900; row++ {
				for column := 0; column < 1600; column++ {
					canvas.Set(column, row, color.RGBA{R: uint8(column), G: uint8(row), B: 90, A: 255})
				}
			}
			var encoded bytes.Buffer
			_ = png.Encode(&encoded, canvas)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(encoded.Bytes())
		case request.URL.Path == "/plain":
			site.pageVisits.Add(1)
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<head><title>A page with no picture</title></head>`))
		default:
			site.pageVisits.Add(1)
			response.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(response, `<head><meta property="og:title" content="A story"><meta property="og:site_name" content="Example News"><meta property="og:image" content="/lead.png"></head>`)
		}
	}))
	t.Cleanup(site.Close)
	previous := agent.SetFeedPictureFetcher(linkpreview.NewFetcherWith(site.Client()))
	t.Cleanup(func() { agent.SetFeedPictureFetcher(previous) })
	return site
}

// feedRun is a run conversation whose tool answered with the given text.
func feedRun(t *testing.T, world *speakFirstWorld, toolAnswer string) *models.AgentConversation {
	t.Helper()
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: world.agent.ID, Kind: models.AgentConversationRun, Title: "A run", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "tool", Name: "web_search", Content: toolAnswer}); err != nil {
			t.Fatal(err)
		}
	})
	return conversation
}

func readFeedItem(t *testing.T, world *speakFirstWorld, feedItemId string) *models.AgentFeedItem {
	t.Helper()
	var item *models.AgentFeedItem
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if item, err = tx.GetAgentFeedItem(feedItemId); err != nil {
			t.Fatal(err)
		}
	})
	return item
}

// A news card posted with only a source link shows the picture of that page,
// fetched by the server, kept scaled down, and named with the page's site; a
// page a tool never answered with is not fetched at all.
func TestANewsCardShowsThePictureOfItsPageOnlyWhenAToolGaveThePage(t *testing.T) {
	world := newSpeakFirstWorld(t)
	site := newFeedPictureSite(t)
	story := site.URL + "/story"
	run := feedRun(t, world, "Results: "+story+" and nothing else")

	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
		Title: "A story", RunConversationID: run.ID, FeedItemKind: models.FeedItemNews,
		SourceLinks: []models.FeedSourceLink{{SourceURL: story}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.FeedMedia) != 1 || !posted.FeedMedia[0].IsPictureWaiting() {
		t.Fatalf("the card was posted with %+v, expected one picture waiting for its page", posted.FeedMedia)
	}
	world.worker.WaitFeedPictures()
	stored := readFeedItem(t, world, posted.ID)
	if len(stored.FeedMedia) != 1 || !stored.FeedMedia[0].IsShown() {
		t.Fatalf("the card has %+v, expected the page's picture", stored.FeedMedia)
	}
	picture := stored.FeedMedia[0]
	if picture.SiteName != "Example News" || picture.WidthPixels != 1200 || picture.HeightPixels != 675 {
		t.Errorf("the picture is %+v, expected Example News at 1200 by 675", picture)
	}

	// A page whose address the model wrote itself could carry what it read;
	// it is never asked for, and the card shows no picture.
	visitsBefore := site.pageVisits.Load()
	composed, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
		Title: "Another story", RunConversationID: run.ID, FeedItemKind: models.FeedItemNews,
		SourceLinks: []models.FeedSourceLink{{SourceURL: site.URL + "/story?read=something"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	world.worker.WaitFeedPictures()
	if got := site.pageVisits.Load(); got != visitsBefore {
		t.Errorf("the site was asked for %d pages more, expected none", got-visitsBefore)
	}
	stored = readFeedItem(t, world, composed.ID)
	if len(stored.FeedMedia) != 1 || stored.FeedMedia[0].IsShown() || !stored.FeedMedia[0].IsPictureMissing {
		t.Errorf("the card has %+v, expected a picture marked missing", stored.FeedMedia)
	}
}

// A list card's linked rows get a picture each, and a page without one leaves
// its row without a thumbnail.
func TestAListCardsLinkedRowsGetThumbnails(t *testing.T) {
	world := newSpeakFirstWorld(t)
	site := newFeedPictureSite(t)
	first, plain := site.URL+"/first", site.URL+"/plain"
	run := feedRun(t, world, first+"\n"+plain)

	posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
		Title: "Top stories", RunConversationID: run.ID, FeedCardLayout: models.FeedLayoutList,
		LayoutContent: fmt.Sprintf(`{"listRows":[{"rowTitle":"First","rowUrl":%q},{"rowTitle":"No link"},{"rowTitle":"Plain","rowUrl":%q}]}`, first, plain),
	}})
	if err != nil {
		t.Fatal(err)
	}
	world.worker.WaitFeedPictures()
	stored := readFeedItem(t, world, posted.ID)
	shown := map[int]bool{}
	for _, entry := range stored.FeedMedia {
		if entry.ListRowIndex == nil {
			t.Errorf("%+v is a picture of the whole card, expected only row thumbnails", entry)
			continue
		}
		shown[*entry.ListRowIndex] = entry.IsShown()
	}
	if len(shown) != 2 || !shown[0] || shown[2] {
		t.Errorf("the rows' pictures are %v, expected row 0 shown and row 2 without one", shown)
	}
}

// A card replaced by a later post about the same page keeps the picture it
// already has instead of fetching it again, and a card changed to show other
// pictures throws the old ones' files away.
func TestAReplacedCardKeepsItsPictureAndAChangedOneDropsTheOld(t *testing.T) {
	world := newSpeakFirstWorld(t)
	site := newFeedPictureSite(t)
	story, other := site.URL+"/story", site.URL+"/other"
	run := feedRun(t, world, story+" "+other)
	post := func(title, link string) *models.AgentFeedItem {
		posted, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: &models.AgentFeedItem{
			Title: title, RunConversationID: run.ID, CoalesceKey: "the-story", SourceLinks: []models.FeedSourceLink{{SourceURL: link}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		world.worker.WaitFeedPictures()
		return readFeedItem(t, world, posted.ID)
	}
	first := post("The story", story)
	visits := site.pageVisits.Load()
	again := post("The story, updated", story)
	if site.pageVisits.Load() != visits {
		t.Error("the page was read again for a card that already had its picture")
	}
	if again.FeedMedia[0].ImageFileID != first.FeedMedia[0].ImageFileID {
		t.Errorf("the replaced card's picture is %s, expected the one it had, %s", again.FeedMedia[0].ImageFileID, first.FeedMedia[0].ImageFileID)
	}

	changed, err := world.worker.UpdateFeedItem(context.Background(), world.agent.ID, again.ID, run.ID, func(item *models.AgentFeedItem) error {
		item.FeedMedia = []models.FeedMedia{{PageURL: other}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	world.worker.WaitFeedPictures()
	changed = readFeedItem(t, world, changed.ID)
	if len(changed.FeedMedia) != 1 || !changed.FeedMedia[0].IsShown() || changed.FeedMedia[0].PageURL != other {
		t.Fatalf("the changed card has %+v, expected the other page's picture", changed.FeedMedia)
	}
	if _, err := world.store().GetFile(context.Background(), first.FeedMedia[0].ImageFileID); err == nil {
		t.Error("the old picture's file is still kept")
	}
}

// A post naming a photo that is not the person's is refused, and media that
// cannot be shown are refused with words the model can act on.
func TestAPostWithMediaThatCannotBeShownIsRefused(t *testing.T) {
	world := newSpeakFirstWorld(t)
	rowIndex := 3
	for _, test := range []struct {
		name  string
		item  *models.AgentFeedItem
		words string
	}{
		{"an unknown photo", &models.AgentFeedItem{Title: "A photo", FeedMedia: []models.FeedMedia{{PhotoID: "no-such-photo"}}}, "no photo"},
		{"both kinds at once", &models.AgentFeedItem{Title: "Both", FeedMedia: []models.FeedMedia{{PhotoID: "a", PageURL: "https://example.com/"}}}, "not both"},
		{"not a web page", &models.AgentFeedItem{Title: "A file", FeedMedia: []models.FeedMedia{{PageURL: "file:///etc/hosts"}}}, "not a page"},
		{"a row of a text card", &models.AgentFeedItem{Title: "Text", FeedMedia: []models.FeedMedia{{PageURL: "https://example.com/", ListRowIndex: &rowIndex}}}, "list card"},
		{"too many", &models.AgentFeedItem{Title: "Many", FeedMedia: []models.FeedMedia{
			{PageURL: "https://example.com/1"}, {PageURL: "https://example.com/2"}, {PageURL: "https://example.com/3"}, {PageURL: "https://example.com/4"},
			{PageURL: "https://example.com/5"}, {PageURL: "https://example.com/6"}, {PageURL: "https://example.com/7"},
		}}, "at most 6"},
	} {
		_, _, err := world.worker.PostFeedItem(context.Background(), &agent.FeedPost{AgentID: world.agent.ID, Item: test.item})
		if err == nil || !strings.Contains(err.Error(), test.words) {
			t.Errorf("%s: posting said %v, expected a refusal saying %q", test.name, err, test.words)
		}
	}
}
