package apigraph

import (
	"bytes"
	"encoding/json"
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

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

const linkPreviewFields = `url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels`

// linkPreviewSite is a web site that counts the visits it gets and remembers
// the last address asked for.
type linkPreviewSite struct {
	*httptest.Server
	visits    atomic.Int32
	lastQuery atomic.Value
}

func newLinkPreviewSite(t *testing.T) *linkPreviewSite {
	t.Helper()
	site := &linkPreviewSite{}
	site.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/chair.png":
			canvas := image.NewRGBA(image.Rect(0, 0, 900, 600))
			for row := 0; row < 600; row++ {
				for column := 0; column < 900; column++ {
					canvas.Set(column, row, color.RGBA{R: uint8(column), G: uint8(row), B: 40, A: 255})
				}
			}
			var encoded bytes.Buffer
			_ = png.Encode(&encoded, canvas)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(encoded.Bytes())
		case "/missing":
			http.NotFound(response, request)
		case "/bare":
			site.visits.Add(1)
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<head><title>A bare page</title></head>`))
		default:
			site.visits.Add(1)
			site.lastQuery.Store(request.URL.RawQuery)
			response.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(response, `<head><meta property="og:title" content="Garden Chair"><meta property="og:description" content="A chair for the garden."><meta property="og:site_name" content="Example Shop"><meta property="og:image" content="/chair.png"></head>`)
		}
	}))
	t.Cleanup(site.Close)
	return site
}

// useLinkPreviewSite makes the graph fetch with the site's own client, which
// reaches the loopback address that the guarded one refuses, and makes a person
// with a conversation whose first message, the person's own, holds the links.
func (self *avatarWorld) useLinkPreviewSite(t *testing.T, site *linkPreviewSite, links ...string) {
	t.Helper()
	self.graph.linkPreviews.fetcher = linkpreview.NewFetcherWith(site.Client())
	self.router.Path(api.PathLinkPreviewImage).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.graph.linkPreviewImageView)
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		asker, err := tx.CreateUser(&models.User{Username: "link-asker"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		grantAgentUse(t, tx, asker)
		agent, err := tx.CreateAgent(&models.Agent{UserID: asker.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		self.linkAsker, self.linkConversationID = asker, conversation.ID
	})
	if len(links) > 0 {
		self.sayInLinkConversation(t, "user", strings.Join(links, " "))
	}
}

// sayInLinkConversation adds a message to the conversation of the link tests.
func (self *avatarWorld) sayInLinkConversation(t *testing.T, role, content string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: self.linkConversationID, Role: role, Name: map[string]string{"tool": "web_search"}[role], ToolCallID: map[string]string{"tool": "call_1"}[role], Content: content}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
	})
}

func (self *avatarWorld) readLinkPreviews(t *testing.T, addresses ...string) []map[string]any {
	t.Helper()
	quoted := make([]string, len(addresses))
	for index, address := range addresses {
		quoted[index] = fmt.Sprintf("%q", address)
	}
	data, failures := self.ask(t, self.linkAsker, fmt.Sprintf(`query { ReadLinkPreviews(conversationId: %q, urls: [%s]) { %s } }`, self.linkConversationID, strings.Join(quoted, ","), linkPreviewFields))
	if len(failures) > 0 {
		t.Fatalf("ReadLinkPreviews: %v", failures)
	}
	var previews []map[string]any
	for _, each := range data["ReadLinkPreviews"].([]any) {
		previews = append(previews, each.(map[string]any))
	}
	return previews
}

func TestAPageIsFetchedCleanedAndItsPictureServedFromHere(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/shop/chair?color=blue&gclid=abc123&utm_source=mail#reviews")

	written := site.URL + "/shop/chair?color=blue&gclid=abc123&utm_source=mail#reviews"
	previews := world.readLinkPreviews(t, written)
	if len(previews) != 1 {
		t.Fatalf("got %d previews, want 1", len(previews))
	}
	preview := previews[0]
	if preview["url"] != written || preview["cleanedUrl"] != site.URL+"/shop/chair?color=blue#reviews" {
		t.Errorf("the link came back as %v, cleaned %v", preview["url"], preview["cleanedUrl"])
	}
	if !strings.HasSuffix(preview["displayText"].(string), "/shop/chair?color=blue") {
		t.Errorf("displayText = %v", preview["displayText"])
	}
	if preview["isAvailable"] != true || preview["title"] != "Garden Chair" || preview["description"] != "A chair for the garden." || preview["siteName"] != "Example Shop" {
		t.Errorf("the preview is %v", preview)
	}
	if got := site.lastQuery.Load(); got != "color=blue" {
		t.Errorf("the page was asked for ?%v: the tracking parameters were sent on", got)
	}
	if preview["imageWidthPixels"] != float64(600) || preview["imageHeightPixels"] != float64(400) {
		t.Errorf("the picture is %v by %v, want 600 by 400", preview["imageWidthPixels"], preview["imageHeightPixels"])
	}

	address, _ := preview["imageUrl"].(string)
	if !strings.HasPrefix(address, "/api/v1/link-preview/image/") || strings.Contains(address, site.URL) {
		t.Fatalf("imageUrl = %q, want an address on this server", address)
	}
	served := world.fetch(world.person, address)
	if served.Code != http.StatusOK || served.Header().Get("Content-Type") != "image/jpeg" || served.Body.Len() == 0 {
		t.Errorf("the picture answered %d as %q", served.Code, served.Header().Get("Content-Type"))
	}
	if again := world.fetch(world.other, address, "If-None-Match", served.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Errorf("a picture asked for again with its tag answered %d, want 304", again.Code)
	}
}

func TestAPictureIsServedOnlyToASignedInReaderAndOnlyByAKeptName(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page")
	address := world.readLinkPreviews(t, site.URL+"/page")[0]["imageUrl"].(string)

	signedOut := httptest.NewRecorder()
	world.router.ServeHTTP(signedOut, httptest.NewRequest(http.MethodGet, address, nil))
	if signedOut.Code != http.StatusUnauthorized {
		t.Errorf("a picture asked for by nobody answered %d, want 401", signedOut.Code)
	}
	// A file of the store that is not a link preview's is not served from
	// here, whatever the name.
	if err := world.files.PutFile(t.Context(), "avatar-user-someone-1", []byte("not a link preview")); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	if other := world.fetch(world.person, "/api/v1/link-preview/image/avatar-user-someone-1"); other.Code != http.StatusNotFound {
		t.Errorf("another file of the store answered %d, want 404", other.Code)
	}
}

func TestAKeptPreviewIsNotFetchedAgainUntilItIsOld(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page", site.URL+"/page#section")
	address := site.URL + "/page"

	world.readLinkPreviews(t, address)
	world.readLinkPreviews(t, address+"?gclid=1", address+"#section")
	if visits := site.visits.Load(); visits != 1 {
		t.Fatalf("the page was fetched %d times, want once for the same page asked for three ways", visits)
	}

	// A day later it is asked again, and the picture it had is replaced.
	var firstImageFileId string
	if err := world.database.Transaction(func(tx db.Transaction) error {
		kept, err := tx.ListLinkPreviews([]string{address})
		if err != nil {
			return err
		}
		firstImageFileId = kept[address].ImageFileID
		kept[address].FetchedAt = time.Now().Add(-25 * time.Hour)
		_, err = tx.SaveLinkPreview(kept[address])
		return err
	}); err != nil {
		t.Fatalf("aging the row: %s", err)
	}
	world.readLinkPreviews(t, address)
	if visits := site.visits.Load(); visits != 2 {
		t.Errorf("an old preview was fetched %d times in all, want 2", visits)
	}
	if _, err := world.files.GetFile(t.Context(), firstImageFileId); err == nil {
		t.Errorf("the picture the new one replaced is still in the store")
	}
}

func TestAPageThatCannotBeReadIsRememberedAsUnavailable(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/missing")

	missing := world.readLinkPreviews(t, site.URL+"/missing")[0]
	if missing["isAvailable"] != false || missing["title"] != "" || missing["imageUrl"] != nil {
		t.Errorf("a missing page came back as %v", missing)
	}
	if missing["displayText"] == "" || missing["cleanedUrl"] == "" {
		t.Errorf("an unavailable preview lost its link: %v", missing)
	}
	// Remembered: an hour is not up.
	world.readLinkPreviews(t, site.URL+"/missing")
	var failure string
	if err := world.database.Transaction(func(tx db.Transaction) error {
		kept, err := tx.ListLinkPreviews([]string{site.URL + "/missing"})
		if err == nil && kept[site.URL+"/missing"] != nil {
			failure = kept[site.URL+"/missing"].FetchFailure
		}
		return err
	}); err != nil || failure != "unreachable" {
		t.Errorf("the failure kept is %q (%v), want unreachable", failure, err)
	}
}

func TestAPageWithoutAPictureStillHasACard(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/bare")
	bare := world.readLinkPreviews(t, site.URL+"/bare")[0]
	if bare["isAvailable"] != true || bare["title"] != "A bare page" || bare["imageUrl"] != nil {
		t.Errorf("a page with only a title came back as %v", bare)
	}
	if bare["siteName"] != "127.0.0.1" {
		t.Errorf("a page that names no site is called %v, want its host", bare["siteName"])
	}
}

func TestWhatIsNotAWebAddressIsNotFetched(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site)

	previews := world.readLinkPreviews(t, "mailto:someone@example.com", "ftp://example.com/file", "javascript:alert(1)")
	for _, preview := range previews {
		if preview["isAvailable"] != false {
			t.Errorf("%v was previewed", preview["url"])
		}
	}
	if visits := site.visits.Load(); visits != 0 {
		t.Errorf("the site was visited %d times", visits)
	}
}

// The guarded fetcher is the default, and it refuses an address inside the
// network this server sits in, so a link to one has no preview.
func TestAnAddressInsideTheNetworkIsRefusedByDefault(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page")
	// What useLinkPreviewSite put there is the guarded fetcher's to refuse.
	world.graph.linkPreviews.fetcher = nil

	preview := world.readLinkPreviews(t, site.URL+"/page")[0]
	if preview["isAvailable"] != false {
		t.Errorf("a loopback address was previewed: %v", preview)
	}
	if visits := site.visits.Load(); visits != 0 {
		t.Errorf("the guarded fetcher reached the loopback %d times", visits)
	}
}

func TestLinkPreviewQuestionsAreBounded(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site)

	head := fmt.Sprintf(`query { ReadLinkPreviews(conversationId: %q, urls: `, world.linkConversationID)
	var many []string
	for index := 0; index < 11; index++ {
		many = append(many, fmt.Sprintf("%q", fmt.Sprintf("%s/page-%d", site.URL, index)))
	}
	if _, failures := world.ask(t, world.linkAsker, head+`[`+strings.Join(many, ",")+`]) { url } }`); len(failures) == 0 {
		t.Error("eleven links were accepted")
	}
	if _, failures := world.ask(t, world.linkAsker, head+`[]) { url } }`); len(failures) == 0 {
		t.Error("no links were accepted")
	}
	if _, failures := world.ask(t, world.linkAsker, head+`["  "]) { url } }`); len(failures) == 0 {
		t.Error("a blank link was accepted")
	}
	if visits := site.visits.Load(); visits != 0 {
		t.Errorf("a refused question fetched %d pages", visits)
	}
}

func TestOldLinkPreviewsAreRemovedWithTheirPictures(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page", site.URL+"/other-page")
	address := site.URL + "/page"
	world.readLinkPreviews(t, address)

	var imageFileId string
	if err := world.database.Transaction(func(tx db.Transaction) error {
		kept, err := tx.ListLinkPreviews([]string{address})
		if err != nil {
			return err
		}
		imageFileId = kept[address].ImageFileID
		kept[address].FetchedAt = time.Now().Add(-40 * 24 * time.Hour)
		_, err = tx.SaveLinkPreview(kept[address])
		return err
	}); err != nil {
		t.Fatalf("aging the row: %s", err)
	}

	// Another link is fetched later than an hour after the last removal.
	world.graph.linkPreviews.prunedAt = time.Now().Add(-2 * time.Hour)
	world.readLinkPreviews(t, site.URL+"/other-page")
	if err := world.database.Transaction(func(tx db.Transaction) error {
		kept, err := tx.GetLinkPreviewByImageFile(imageFileId)
		if kept != nil {
			t.Errorf("a row past its month is still kept")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := world.files.GetFile(t.Context(), imageFileId); err == nil {
		t.Errorf("the picture of a removed row is still in the store")
	}
}

// The command line asks with a document of its own and the links as a
// variable, as the dashboard and the iPhone app do; the server takes it.
func TestTheDocumentTheClientsSendIsAcceptedWithTheLinksAsAVariable(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page?gclid=1")

	body, err := json.Marshal(graphRequest{Query: client.DocumentReadLinkPreviews, Variables: map[string]any{"conversationId": world.linkConversationID, "urls": []string{site.URL + "/page?gclid=1"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, world.linkAsker.Username)
	request.Header.Set(api.CredentialKindHeader, string(models.CredentialSession))
	response := httptest.NewRecorder()
	world.graph.graphView(response, request)

	var answer struct {
		Data struct {
			ReadLinkPreviews []client.LinkPreview `json:"ReadLinkPreviews"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil || len(answer.Errors) > 0 {
		t.Fatalf("the answer is %s (%v)", response.Body.String(), err)
	}
	if len(answer.Data.ReadLinkPreviews) != 1 || !answer.Data.ReadLinkPreviews[0].IsAvailable || answer.Data.ReadLinkPreviews[0].Title != "Garden Chair" {
		t.Errorf("the previews are %+v", answer.Data.ReadLinkPreviews)
	}
	if answer.Data.ReadLinkPreviews[0].ImageWidthPixels == nil || *answer.Data.ReadLinkPreviews[0].ImageWidthPixels != 600 {
		t.Errorf("the picture's width is %v", answer.Data.ReadLinkPreviews[0].ImageWidthPixels)
	}
}

// A reply of the agent's can be made to carry what it read in a link's query,
// and drawing the card would send that to the link's site with no click: a link
// is previewed only if somebody other than the agent wrote it.
func TestALinkOnlyTheAgentWroteIsNeverFetched(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site)
	injected := site.URL + "/collect?data=secret"
	world.sayInLinkConversation(t, "assistant", "Here you go: "+injected)

	preview := world.readLinkPreviews(t, injected)[0]
	if preview["isAvailable"] != false || preview["title"] != "" {
		t.Errorf("a link only the agent wrote has the preview %v", preview)
	}
	if preview["cleanedUrl"] != injected {
		t.Errorf("the link came back as %v", preview["cleanedUrl"])
	}
	if visits := site.visits.Load(); visits != 0 {
		t.Errorf("the site was visited %d times", visits)
	}
	// Not the start of a link a person or a page wrote, with the rest added.
	world.sayInLinkConversation(t, "tool", "a page said "+site.URL+"/collect")
	if previewed := world.readLinkPreviews(t, injected)[0]; previewed["isAvailable"] != false || site.visits.Load() != 0 {
		t.Errorf("a link that only starts as one a tool showed was previewed: %v", previewed)
	}
}

func TestACachedPreviewIsNotGivenForALinkOnlyTheAgentWrote(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page")
	if world.readLinkPreviews(t, site.URL+"/page")[0]["isAvailable"] != true {
		t.Fatal("the page the person wrote has no preview, so the test says nothing")
	}
	// Another conversation of the same person, where only the agent wrote it.
	var otherId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		agent, err := tx.GetAgentByUser(world.linkAsker.ID)
		if err != nil || agent == nil {
			t.Fatalf("GetAgentByUser: %v %v", agent, err)
		}
		other, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		otherId = other.ID
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: other.ID, Role: "assistant", Content: site.URL + "/page"}); err != nil {
			t.Fatal(err)
		}
	})
	world.linkConversationID = otherId
	if preview := world.readLinkPreviews(t, site.URL+"/page")[0]; preview["isAvailable"] != false {
		t.Errorf("a cached preview was given for a link only the agent wrote: %v", preview)
	}
}

func TestALinkARepliesToolShowedTheAgentIsPreviewed(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site)
	shown := site.URL + "/shop/chair?color=blue"
	world.sayInLinkConversation(t, "tool", `[{"title":"Garden Chair","url":"`+shown+`"}]`)
	world.sayInLinkConversation(t, "assistant", "I found "+shown)
	if preview := world.readLinkPreviews(t, shown)[0]; preview["isAvailable"] != true || preview["title"] != "Garden Chair" {
		t.Errorf("a link a tool showed has the preview %v", preview)
	}
}

func TestALinkWithTrackingIsPreviewedWhenItsCleanedFormWasWritten(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page")
	if preview := world.readLinkPreviews(t, site.URL+"/page?gclid=1&utm_source=x")[0]; preview["isAvailable"] != true {
		t.Errorf("the tracked form of a written link has the preview %v", preview)
	}
}

func TestAConversationOfSomebodyElseIsNotFound(t *testing.T) {
	world := newAvatarWorld(t)
	site := newLinkPreviewSite(t)
	world.useLinkPreviewSite(t, site, site.URL+"/page")
	// Somebody who may use an agent of their own, so that it is the
	// conversation that is refused and not the person.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		grantAgentUse(t, tx, world.person)
		if _, err := tx.CreateAgent(&models.Agent{UserID: world.person.ID, Enabled: true, Name: "Other"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	for _, conversationId := range []string{"no-such-conversation", world.linkConversationID} {
		asker := world.person
		if conversationId == "no-such-conversation" {
			asker = world.linkAsker
		}
		_, failures := world.ask(t, asker, fmt.Sprintf(`query { ReadLinkPreviews(conversationId: %q, urls: [%q]) { url } }`, conversationId, site.URL+"/page"))
		if len(failures) == 0 {
			t.Errorf("a conversation %q of nobody's, or somebody else's, was read by %s", conversationId, asker.Username)
		}
	}
	if visits := site.visits.Load(); visits != 0 {
		t.Errorf("a refused question fetched %d pages", visits)
	}
}
