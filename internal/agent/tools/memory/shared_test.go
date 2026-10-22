package memory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// sharingRun is a run that can read what another person shared: a page of
// theirs, a root they listed, a hit a search found. The reading itself is
// internal/agent's and is tested there; what is tested here is what the tool
// does with it.
type sharingRun struct {
	*fakeRun
	can   bool
	roots []*models.SharedRoot
	pages map[string]*models.SharedPage
	hits  []*models.SharedHit

	// sharedWords is what shared pages showed the conversation, for
	// RestsOnSharedPages; the matching itself is internal/agent's.
	sharedWords []string

	operations *routedOperations
}

func (self *sharingRun) CanReadSharedPages() bool { return self.can }

func (self *sharingRun) SharedPages(context.Context) ([]*models.SharedRoot, error) {
	return self.roots, nil
}

func (self *sharingRun) ReadSharedPage(_ context.Context, ownerUserId, path string) (*models.SharedPage, error) {
	return self.pages[ownerUserId+"|"+path], nil
}

func (self *sharingRun) SearchSharedPages(context.Context, string, int) ([]*models.SharedHit, error) {
	return self.hits, nil
}

func (self *sharingRun) RestsOnSharedPages(_ context.Context, text string) bool {
	for _, words := range self.sharedWords {
		if strings.Contains(strings.ToLower(words), strings.ToLower(strings.TrimSuffix(text, "."))) {
			return true
		}
	}
	return false
}

func (self *sharingRun) Operations() tools.Operations { return self.operations }

// routedOperations answers each API operation with a canned answer and keeps
// what it was sent.
type routedOperations struct {
	answers map[string]string
	sent    map[string]map[string]any
}

func (self *routedOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}, {Permission: models.PermissionCircleUse}})
}

func (self *routedOperations) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	data := map[string]json.RawMessage{}
	for operation, answer := range self.answers {
		if strings.Contains(document, operation+"(") || strings.Contains(document, operation+" ") {
			data[operation] = json.RawMessage(answer)
			if self.sent == nil {
				self.sent = map[string]map[string]any{}
			}
			self.sent[operation] = variables
		}
	}
	encoded, _ := json.Marshal(data)
	return json.Unmarshal(encoded, result)
}

func sharedPageFixture() *models.SharedPage {
	return &models.SharedPage{
		OwnerUserID: "ada-id", OwnerUsername: "ada", OwnerName: "Ada Example",
		Path: "people/a-grandparent", Name: "A Grandparent", NodeKind: models.NodePerson,
		Opening: "Lives by the sea.",
		Facts: []*models.SharedFact{
			{Number: 3, FactKind: models.FactPlain, Text: "Next appointment is Tuesday at three.", EvidenceSource: "from Ada Example's mail"},
		},
		Edges:    []*models.SharedEdge{{FromPath: "people/a-grandparent", ToPath: "people/a-grandparent/doctor", Relation: models.EdgeKnows}},
		Children: []*models.SharedChild{{Path: "people/a-grandparent/doctor", Name: "The doctor", NodeKind: models.NodePerson, Opening: "Sees them in spring"}},
	}
}

func aSharingRun(t *testing.T, username string) (*sharingRun, func()) {
	t.Helper()
	base, _, closeDatabase := person(t, username)
	run := &sharingRun{
		fakeRun: base, can: true,
		roots: []*models.SharedRoot{{
			OwnerUserID: "ada-id", OwnerUsername: "ada", OwnerName: "Ada Example",
			Path: "people/a-grandparent", Name: "A Grandparent", Opening: "Lives by the sea",
		}},
		pages:      map[string]*models.SharedPage{"ada-id|people/a-grandparent": sharedPageFixture()},
		operations: &routedOperations{answers: map[string]string{}},
	}
	return run, closeDatabase
}

func callMemory(t *testing.T, run tools.Run, arguments string) (*tools.Result, error) {
	t.Helper()
	return find(t, "memory").Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: []byte(arguments)})
}

// A shared page is read with the owner named, the facts numbered as the
// owner numbers them, the marker on top so the filing run can tell it from the
// person's own pages, the words cited as data, and the form to cite it in.
func TestASharedPageIsReadWithItsOwner(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "reading-shared")
	defer closeDatabase()

	result, err := callMemory(t, run, `{"action":"get","path":"people/a-grandparent","owner":"ada"}`)
	if err != nil {
		t.Fatalf("get: %s", err)
	}
	if !strings.HasPrefix(result.Content, tools.SharedPageMarker+"\n") || !result.Untrusted {
		t.Errorf("marked, and data: %q (untrusted %v)", result.Content, result.Untrusted)
	}
	for _, want := range []string{
		"people/a-grandparent - A Grandparent (person), shared by Ada Example (owner:ada), read only",
		"Lives by the sea.",
		"#3 Next appointment is Tuesday at three.  (from Ada Example's mail)",
		"→ knows people/a-grandparent/doctor",
		"under it: doctor",
		"people/a-grandparent#3 (shared by Ada Example)",
		"[name](memory:people/a-grandparent#N@ada)",
	} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("the page lacks %q:\n%s", want, result.Content)
		}
	}
	if strings.Contains(result.Content, "quote:") {
		t.Errorf("no quote was shared:\n%s", result.Content)
	}
	if got := tools.TextFromSharedPages(result.Content); !strings.Contains(got, "Next appointment is Tuesday") {
		t.Errorf("the filing run finds the words after the marker: %q", got)
	}

	// The owner's username is matched without regard to case, and a quote
	// shown only when the share carried it.
	page := run.pages["ada-id|people/a-grandparent"]
	page.Facts[0].EvidenceQuote = "see you Tuesday at three"
	result, err = callMemory(t, run, `{"action":"get","path":"people/a-grandparent","owner":"ADA"}`)
	if err != nil || !strings.Contains(result.Content, `quote: "see you Tuesday at three"`) {
		t.Errorf("the quote once it is shared: %v\n%v", err, result)
	}
}

// A page that is not shared is not there, and the answer says only what is.
func TestAPageNotSharedIsNotThere(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "unshared-page")
	defer closeDatabase()

	_, err := callMemory(t, run, `{"action":"get","path":"people/a-grandparent-private","owner":"ada"}`)
	if err == nil || !strings.Contains(err.Error(), "shared no page at people/a-grandparent-private") || !strings.Contains(err.Error(), "What they shared: people/a-grandparent") {
		t.Errorf("a page beside a shared one: %v", err)
	}
	// And the same words for somebody who shared nothing as for no account.
	for _, owner := range []string{"nobody", "cora"} {
		_, err = callMemory(t, run, `{"action":"get","path":"people/a-grandparent","owner":"`+owner+`"}`)
		if err == nil || !strings.Contains(err.Error(), `nobody called "`+owner+`" has shared pages`) {
			t.Errorf("an owner who shared nothing: %v", err)
		}
	}
}

// A run that may not read shared pages (a dream, a run nobody is at) is
// refused, rather than answered as if nothing were shared.
func TestARunThatMayNotReadSharedPagesIsRefused(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "dream-shared")
	defer closeDatabase()
	run.can = false
	for _, arguments := range []string{
		`{"action":"get","path":"people/a-grandparent","owner":"ada"}`,
		`{"action":"index","owner":"ada"}`,
		`{"action":"search","query":"appointment","owner":"ada"}`,
		`{"action":"shared"}`,
	} {
		if _, err := callMemory(t, run, arguments); err == nil || !strings.Contains(err.Error(), "pages other people shared are read only in a one-to-one conversation") {
			t.Errorf("%s: %v", arguments, err)
		}
	}
	// Its own search is its own search, with nothing shared added.
	run.hits = []*models.SharedHit{{OwnerUsername: "ada", OwnerName: "Ada Example", Path: "people/a-grandparent", FactNumber: 3, Text: "Tuesday"}}
	result, err := callMemory(t, run, `{"action":"search","query":"appointment"}`)
	if err != nil || strings.Contains(result.Content, "Ada Example") || strings.Contains(result.Content, tools.SharedPageMarker) {
		t.Errorf("a search with nothing shared in it: %v %v", err, result)
	}
}

// Every action that writes, and every read of the person's own pages that is
// not get, index or search, refuses an owner who is not the person, and
// writes nothing.
func TestWritesRefuseAnOwnerWhoIsNotThePerson(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "writing-shared")
	defer closeDatabase()
	database := run.database
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatal(err)
		}
	})
	for _, arguments := range []string{
		`{"action":"note","path":"people/a-grandparent","text":"Has an appointment on Tuesday.","owner":"ada"}`,
		`{"action":"note","path":"people/a-grandparent","number":3,"text":"Corrected.","owner":"ada"}`,
		`{"action":"page","path":"people/a-grandparent","summary":"Mine now.","owner":"ada"}`,
		`{"action":"link","path":"people/a-grandparent","to":"people/b","relation":"knows","owner":"ada"}`,
		`{"action":"unlink","path":"people/a-grandparent","to":"people/b","relation":"knows","owner":"ada"}`,
		`{"action":"move","path":"people/a-grandparent","to":"people/b","owner":"ada"}`,
		`{"action":"merge","path":"people/a-grandparent","to":"people/b","owner":"ada"}`,
		`{"action":"forget","path":"people/a-grandparent","number":3,"owner":"ada"}`,
		`{"action":"history","path":"people/a-grandparent","owner":"ada"}`,
		`{"action":"batch","items":[{"action":"note","path":"people/a-grandparent","text":"Copied.","owner":"ada"}]}`,
	} {
		result, err := callMemory(t, run, arguments)
		refusal := ""
		if err != nil {
			refusal = err.Error()
		} else {
			refusal = result.Content
		}
		if !strings.Contains(refusal, "pages another person shared are read only") {
			t.Errorf("%s was not refused: %q", arguments, refusal)
		}
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if node, err := tx.GetAgentNode(run.agent.ID, "people/a-grandparent"); err != nil || node != nil {
			t.Errorf("a refused write made a page: %v %v", node, err)
		}
	})

	// The person's own username is the person's own graph.
	if _, err := callMemory(t, run, `{"action":"note","path":"things/marigold","kind":"thing","name":"Marigold","text":"A boat.","owner":"`+run.owner.Username+`"}`); err != nil {
		t.Errorf("a write that names the person: %v", err)
	}
}

// A note that repeats what a shared page showed the conversation is refused,
// with the words that say to cite it; a note of something else is written.
func TestANoteOfWhatASharedPageSaidIsRefused(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "noting-shared")
	defer closeDatabase()
	run.sharedWords = []string{"Next appointment is Tuesday at three."}
	_, err := callMemory(t, run, `{"action":"note","path":"people/a-grandparent","text":"Next appointment is Tuesday at three."}`)
	if err == nil || !strings.Contains(err.Error(), tools.SharedPageRefusal) {
		t.Fatalf("the copy was not refused: %v", err)
	}
	dbtest.RunTransactionOn(t, run.database, func(tx db.Transaction) {
		if node, err := tx.GetAgentNode(run.agent.ID, "people/a-grandparent"); err != nil || node != nil {
			t.Errorf("a refused note made a page: %v %v", node, err)
		}
	})
	if _, err := callMemory(t, run, `{"action":"note","path":"things/marigold","kind":"thing","name":"Marigold","text":"A boat moored in the harbour."}`); err != nil {
		t.Errorf("a note of the person's own: %v", err)
	}
}

// A search without an owner also searches what is shared, after the person's
// own results, each hit marked with whose it is.
func TestASearchAlsoSearchesWhatIsShared(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "searching-shared")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, run.database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(run.agent.ID); err != nil {
			t.Fatal(err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.agent.ID, Path: "things/appointments", Kind: models.NodeThing, Name: "Appointments"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{AgentID: run.agent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: "My own appointment is Monday.", Confidence: 1}); err != nil {
			t.Fatal(err)
		}
	})
	run.hits = []*models.SharedHit{
		{OwnerUsername: "ada", OwnerName: "Ada Example", Path: "people/a-grandparent", Name: "A Grandparent", Opening: "Lives by the sea"},
		{OwnerUsername: "ada", OwnerName: "Ada Example", Path: "people/a-grandparent", FactNumber: 3, Text: "Next appointment is Tuesday at three."},
	}
	result, err := callMemory(t, run, `{"action":"search","query":"appointment"}`)
	if err != nil {
		t.Fatalf("search: %s", err)
	}
	own := strings.Index(result.Content, "My own appointment is Monday.")
	marker := strings.Index(result.Content, tools.SharedPageMarker)
	shared := strings.Index(result.Content, "people/a-grandparent#3 (shared by Ada Example) Next appointment is Tuesday at three.")
	if own < 0 || marker < own || shared < marker {
		t.Errorf("own results, then the marker, then the shared ones:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "people/a-grandparent - A Grandparent: Lives by the sea (shared by Ada Example, owner:ada)") {
		t.Errorf("a shared page is marked with its owner:\n%s", result.Content)
	}
	// The person's own results do not come after the marker.
	if tools.TextFromSharedPages(result.Content) == "" || strings.Contains(tools.TextFromSharedPages(result.Content), "My own appointment") {
		t.Errorf("only the shared part is after the marker:\n%s", result.Content)
	}

	// Nothing of their own, and the shared hits stand alone.
	result, err = callMemory(t, run, `{"action":"search","query":"zebra"}`)
	if err != nil || !strings.HasPrefix(result.Content, tools.SharedPageMarker) {
		t.Errorf("only shared hits: %v %q", err, result.Content)
	}
	// With an owner, only that owner's.
	run.hits[1].OwnerUsername = "cora"
	result, err = callMemory(t, run, `{"action":"search","query":"appointment","owner":"ada"}`)
	if err != nil || strings.Contains(result.Content, "Tuesday") || !strings.Contains(result.Content, "A Grandparent") {
		t.Errorf("one owner's: %v %v", err, result)
	}
}

// The index of what one person shared, and of all of it, says how many more
// there are and the call that reads on.
func TestTheIndexOfSharedPagesSaysHowManyMore(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "index-shared")
	defer closeDatabase()
	for _, path := range []string{"projects/garden", "projects/boat"} {
		run.roots = append(run.roots, &models.SharedRoot{OwnerUserID: "ada-id", OwnerUsername: "ada", OwnerName: "Ada Example", Path: path, Name: path})
	}
	result, err := callMemory(t, run, `{"action":"index","owner":"ada","limit":2}`)
	if err != nil {
		t.Fatalf("index: %s", err)
	}
	if !strings.Contains(result.Content, "… 1 more; index again with offset: 2 and owner: ada") || !strings.HasPrefix(result.Content, tools.SharedPageMarker) {
		t.Errorf("the rest is counted:\n%s", result.Content)
	}
	result, err = callMemory(t, run, `{"action":"shared","limit":1}`)
	if err != nil || !strings.Contains(result.Content, "(shared by Ada Example, owner:ada)") || !strings.Contains(result.Content, "… 2 more; shared again with offset: 1") {
		t.Errorf("everything shared: %v\n%v", err, result)
	}
	// And the pages under a shared page.
	result, err = callMemory(t, run, `{"action":"index","owner":"ada","path":"people/a-grandparent"}`)
	if err != nil || !strings.Contains(result.Content, "  people/a-grandparent/doctor - The doctor: Sees them in spring") {
		t.Errorf("under a page: %v\n%v", err, result)
	}
}

// Sharing is a granting call, taking back is a write, and listing is a read;
// a batch is as risky as its riskiest item.
func TestSharingIsAGrantingCall(t *testing.T) {
	tool := find(t, "memory")
	for arguments, want := range map[string]tools.Risk{
		`{"action":"share","path":"people/a","with":"ben"}`:                                         tools.RiskGranting,
		`{"action":"unshare","share_id":"x"}`:                                                       tools.RiskWrite,
		`{"action":"shares"}`:                                                                       tools.RiskRead,
		`{"action":"shared"}`:                                                                       tools.RiskRead,
		`{"action":"preview","path":"people/a","with":"ben"}`:                                       tools.RiskRead,
		`{"action":"get","path":"people/a","owner":"ada"}`:                                          tools.RiskRead,
		`{"action":"batch","items":[{"action":"get","path":"a"},{"action":"share","path":"a"}]}`:    tools.RiskGranting,
		`{"action":"batch","items":[{"action":"share","path":"a"},{"action":"note","text":"x"}]}`:   tools.RiskGranting,
		`{"action":"batch","items":[{"action":"share","path":"a"},{"action":"forget","path":"x"}]}`: tools.RiskDestructive,
		`{"action":"batch","items":[{"action":"get","path":"a"},{"action":"shares"}]}`:              tools.RiskRead,
	} {
		if got := tool.RiskFor([]byte(arguments)); got != want {
			t.Errorf("%s is %s, want %s", arguments, got, want)
		}
	}
}

// Sharing goes through the same operations the dashboard and the command line
// use, resolving a username or a circle's id.
func TestSharingGoesThroughTheAPI(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "sharing-api")
	defer closeDatabase()
	run.operations.answers = map[string]string{
		"ListCirclePeople": `[{"userId":"ben-id","username":"ben"}]`,
		"ListCircles":      `[{"id":"home-id","name":"Home"}]`,
		"ShareAgentPage":   `{"id":"share-1","isQuoteShared":false}`,
		"UnshareAgentPage": `true`,
		"ListAgentPageShares": `[{"id":"share-1","path":"people/a","pageName":"A","granteeUsername":"ben","granteeCircleName":"","isQuoteShared":false},
			{"id":"share-2","path":"people/b","pageName":"B","granteeUsername":"","granteeCircleName":"Home","isQuoteShared":true}]`,
	}
	result, err := callMemory(t, run, `{"action":"share","path":"/People/A/","with":"Ben"}`)
	if err != nil || !strings.Contains(result.Content, `"with":"ben"`) {
		t.Fatalf("share with a person: %v %v", err, result)
	}
	if sent := run.operations.sent["ShareAgentPage"]; sent["granteeUserId"] != "ben-id" || sent["granteeCircleId"] != nil || sent["path"] != "people/a" || sent["isQuoteShared"] != false {
		t.Errorf("what was sent: %v", sent)
	}
	if _, err := callMemory(t, run, `{"action":"share","path":"people/a","with":"home-id","is_quote_shared":true}`); err != nil {
		t.Fatalf("share with a circle: %s", err)
	}
	if sent := run.operations.sent["ShareAgentPage"]; sent["granteeCircleId"] != "home-id" || sent["granteeUserId"] != nil || sent["isQuoteShared"] != true {
		t.Errorf("what was sent for a circle: %v", sent)
	}
	if _, err := callMemory(t, run, `{"action":"share","path":"people/a","with":"stranger"}`); err == nil || !strings.Contains(err.Error(), "neither the username of somebody the person shares a circle with nor the name or id of a circle") {
		t.Errorf("somebody in no circle: %v", err)
	}
	if _, err := callMemory(t, run, `{"action":"share","path":"people/a"}`); err == nil {
		t.Error("with whom")
	}
	if _, err := callMemory(t, run, `{"action":"share","path":"people/a","with":"Home"}`); err != nil {
		t.Errorf("a circle is shared with by its name: %s", err)
	}
	if sent := run.operations.sent["ShareAgentPage"]; sent["granteeCircleId"] != "home-id" {
		t.Errorf("a circle by name: %v", sent)
	}
	result, err = callMemory(t, run, `{"action":"shares"}`)
	if err != nil || !strings.Contains(result.Content, `"with":"the circle \"Home\""`) || !strings.Contains(result.Content, `"with":"ben"`) {
		t.Errorf("the shares: %v %v", err, result)
	}
	if _, err := callMemory(t, run, `{"action":"unshare","share_id":"share-1"}`); err != nil {
		t.Errorf("unshare: %s", err)
	}
	if run.operations.sent["UnshareAgentPage"]["shareId"] != "share-1" {
		t.Errorf("what was sent: %v", run.operations.sent["UnshareAgentPage"])
	}
	if _, err := callMemory(t, run, `{"action":"unshare"}`); err == nil {
		t.Error("which share")
	}
}

// The card for sharing a page says the page goes with everything under it,
// who it is for and how many people a circle is, and whether the quotes go.
func TestTheCardForSharingAPageSaysWhatIsGranted(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "sharing-card")
	defer closeDatabase()
	run.operations.answers = map[string]string{
		"ListCirclePeople": `[{"userId":"ben-id","username":"ben"}]`,
		"ListCircles":      `[{"id":"home-id","name":"Home","members":[{"userId":"a"},{"userId":"b"},{"userId":"c"}]}]`,
	}
	ctx := tools.WithRun(context.Background(), run)
	memory := find(t, "memory")
	for arguments, want := range map[string]string{
		`{"action":"share","path":"people/a","with":"ben"}`:                         `Share "people/a" and everything under it with ben, read only, without the quotes each fact was read from`,
		`{"action":"share","path":"people/a","with":"home","is_quote_shared":true}`: `Share "people/a" and everything under it with the circle "Home" (3 people), read only, with the quotes each fact was read from`,
	} {
		if got := memory.PreviewLine(ctx, []byte(arguments)); got != want {
			t.Errorf("%s\n got %q\nwant %q", arguments, got, want)
		}
	}
}

// Previewing a share shows what it would show and how far it reaches, through
// the operation the dashboard and the command line use, and shares nothing.
func TestPreviewingAShareSharesNothing(t *testing.T) {
	run, closeDatabase := aSharingRun(t, "sharing-preview")
	defer closeDatabase()
	run.operations.answers = map[string]string{
		"ListCirclePeople": `[{"userId":"ben-id","username":"ben"}]`,
		"ListCircles":      `[]`,
		"PreviewAgentPageShare": `{"page":{"path":"people/a","name":"A","nodeKind":"person","opening":"Lives by the sea.",
			"facts":[{"number":1,"text":"Likes tea","evidenceQuote":"I like tea"}],"edges":[{"relation":"knows"}],"children":[{"path":"people/a/b"}]},
			"reachedPageCount":4,"isReachedPageCountCut":false,
			"reachedPagePaths":["people/a","people/a/b"],"withheldFactCount":3}`,
	}
	result, err := callMemory(t, run, `{"action":"preview","path":"/People/A/","with":"ben","is_quote_shared":true}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{`"reached_page_count":4`, `"Likes tea"`, `"quote":"I like tea"`, `"with":"ben"`,
		`"reached_page_paths":["people/a","people/a/b"]`, `added under the shared page later is shared too`, `"withheld_fact_count":3`, `3 facts from finance, recordings or location stay private`} {
		if !strings.Contains(result.Content, wanted) {
			t.Errorf("the preview lacks %s:\n%s", wanted, result.Content)
		}
	}
	if sent := run.operations.sent["PreviewAgentPageShare"]; sent["granteeUserId"] != "ben-id" || sent["path"] != "people/a" || sent["isQuoteShared"] != true {
		t.Errorf("what was sent: %v", sent)
	}
	if run.operations.sent["ShareAgentPage"] != nil {
		t.Error("a preview shared the page")
	}
}
