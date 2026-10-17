package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// pageShareWorld is a server with four people who can use circles and their
// agents' memory: ada and ben in one circle, cora in none of it, and eve, who
// can use memory but not circles. Ada keeps people/a, a page under it, a page
// whose name only starts like it, and a private one.
type pageShareWorld struct {
	*avatarWorld
	ada, ben, cora, eve *models.User
	home                string
}

func newPageShareWorld(t *testing.T) *pageShareWorld {
	t.Helper()
	world := &pageShareWorld{avatarWorld: newAvatarWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: strings.ToUpper(username[:1]) + username[1:]})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			if _, err := tx.EnsureAgent(user.ID); err != nil {
				t.Fatalf("EnsureAgent: %s", err)
			}
			return user
		}
		both := []models.Permission{models.PermissionCircleUse, models.PermissionAgentUse}
		world.ada, world.ben, world.cora = create("page-ada", both...), create("page-ben", both...), create("page-cora", both...)
		world.eve = create("page-eve", models.PermissionAgentUse)

		adaAgent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || adaAgent == nil {
			t.Fatalf("GetAgentByUser: %v %v", adaAgent, err)
		}
		if err := tx.EnsureAgentRoots(adaAgent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		for path, summary := range map[string]string{
			"people/a": "Lives by the sea.", "people/a/doctor": "Sees the doctor in spring.",
			"people/ab": "Not the same person.", "projects/private": "Nobody else's business.",
		} {
			node, err := tx.PutAgentNode(&models.AgentNode{AgentID: adaAgent.ID, Path: path, Kind: models.NodeTopic, Name: path, Summary: summary})
			if err != nil {
				t.Fatalf("PutAgentNode: %s", err)
			}
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: adaAgent.ID, NodeID: node.ID, Kind: models.FactPlain, Text: "A fact of " + path + ".", Confidence: 1,
				Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-private-message", Quote: "words from a private mail"}},
			}); err != nil {
				t.Fatalf("AddAgentFact: %s", err)
			}
		}
	})
	data := world.call(t, world.ada, `mutation { CreateCircle(name: "Home", circleKind: "family") { id } }`)
	world.home = data["CreateCircle"].(map[string]any)["id"].(string)
	world.call(t, world.ada, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, world.home, world.ben.Username))
	invitations := world.call(t, world.ben, `query { ListCircleInvitations { id circleId } }`)
	for _, each := range invitations["ListCircleInvitations"].([]any) {
		world.call(t, world.ben, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, each.(map[string]any)["id"]))
	}
	return world
}

func (self *pageShareWorld) call(t *testing.T, user *models.User, query string) map[string]any {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) > 0 {
		t.Fatalf("%s: %v\n%s", user.Username, failures, query)
	}
	return data
}

// refused returns what a refusal said, and fails when there was none.
func (self *pageShareWorld) refused(t *testing.T, user *models.User, query string) string {
	t.Helper()
	data, failures := self.ask(t, user, query)
	if len(failures) == 0 {
		t.Fatalf("%s was not refused: %v\n%s", user.Username, data, query)
	}
	return strings.Join(failures, "; ")
}

func (self *pageShareWorld) share(t *testing.T, path string, grantee *models.User, isQuoteShared bool) string {
	t.Helper()
	data := self.call(t, self.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: %q, granteeUserId: %q, isQuoteShared: %v) { id path granteeUsername isQuoteShared } }`,
		path, grantee.ID, isQuoteShared))
	return data["ShareAgentPage"].(map[string]any)["id"].(string)
}

func (self *pageShareWorld) read(user *models.User, ownerId, path string, t *testing.T) (map[string]any, []string) {
	t.Helper()
	return self.ask(t, user, fmt.Sprintf(`query { ReadSharedAgentPage(ownerUserId: %q, path: %q) {
		ownerUsername ownerName path name opening isQuoteShared
		facts { number text evidenceSource evidenceQuote }
		edges { fromPath toPath }
		children { path }
	} }`, ownerId, path))
}

// A page that is not shared with the caller is not found, and so is a page
// beside one that is, and a page under one is found.
func TestASharedPageIsFoundOnlyWhereItIsShared(t *testing.T) {
	world := newPageShareWorld(t)
	world.share(t, "people/a", world.ben, false)

	for path, want := range map[string]bool{
		"people/a": true, "people/a/doctor": true,
		"people/ab": false, "projects/private": false, "people": false, "people/a/nobody": false,
	} {
		data, failures := world.read(world.ben, world.ada.ID, path, t)
		if got := len(failures) == 0; got != want {
			t.Errorf("ben reads %q: %v (%v), want %v", path, got, failures, want)
		}
		if !want && len(failures) > 0 && !strings.Contains(strings.ToLower(failures[0]), "not found") {
			t.Errorf("a refusal says not found, not why: %v", failures)
		}
		if want {
			page := data["ReadSharedAgentPage"].(map[string]any)
			if page["ownerUsername"] != world.ada.Username {
				t.Errorf("the owner is named: %v", page)
			}
		}
	}

	// And not by anybody it is not shared with.
	for _, other := range []*models.User{world.cora, world.eve} {
		if _, failures := world.read(other, world.ada.ID, "people/a", t); len(failures) == 0 {
			t.Errorf("%s reads a page not shared with them", other.Username)
		}
		listed, _ := world.ask(t, other, `query { ListSharedAgentPages { path } }`)
		if rows, _ := listed["ListSharedAgentPages"].([]any); len(rows) != 0 {
			t.Errorf("%s is shown shared pages: %v", other.Username, rows)
		}
	}
	// The owner reads their own through their own operations, not this one.
	if _, failures := world.read(world.ada, world.ada.ID, "people/a", t); len(failures) == 0 {
		t.Error("the owner is not a reader of their own share")
	}
}

// The quotes are left out of what a reader is shown unless the owner shared
// them, and the source is the owner's.
func TestTheReaderSeesNoQuoteUnlessItIsShared(t *testing.T) {
	world := newPageShareWorld(t)
	world.share(t, "people/a", world.ben, false)
	data, _ := world.read(world.ben, world.ada.ID, "people/a", t)
	fact := data["ReadSharedAgentPage"].(map[string]any)["facts"].([]any)[0].(map[string]any)
	if fact["evidenceQuote"] != "" || fact["evidenceSource"] != "from Page-ada's mail" {
		t.Errorf("no quote, and the source as the owner's: %v", fact)
	}
	if strings.Contains(fmt.Sprint(data), "private mail") || strings.Contains(fmt.Sprint(data), "a-private-message") {
		t.Errorf("the mail leaks: %v", data)
	}
	world.share(t, "people/a", world.ben, true)
	data, _ = world.read(world.ben, world.ada.ID, "people/a", t)
	fact = data["ReadSharedAgentPage"].(map[string]any)["facts"].([]any)[0].(map[string]any)
	if fact["evidenceQuote"] != "words from a private mail" {
		t.Errorf("the quote once it is shared: %v", fact)
	}
}

// Who may share, list and take back: the owner of a page, with a person they
// share a circle with, and nobody else.
func TestOnlyTheOwnerSharesAndTakesBack(t *testing.T) {
	world := newPageShareWorld(t)
	shareId := world.share(t, "people/a", world.ben, false)

	// Not with someone in no circle in common, nor in a circle the owner is
	// not in, nor for a page the owner does not have: not found.
	for _, query := range []string{
		fmt.Sprintf(`mutation { ShareAgentPage(path: "people/a", granteeUserId: %q) { id } }`, world.cora.ID),
		fmt.Sprintf(`mutation { ShareAgentPage(path: "people/nobody", granteeUserId: %q) { id } }`, world.ben.ID),
		`mutation { ShareAgentPage(path: "people/a", granteeUserId: "nobody-has-this-id") { id } }`,
		`mutation { ShareAgentPage(path: "people/a", granteeCircleId: "nobody-has-this-id") { id } }`,
	} {
		if said := world.refused(t, world.ada, query); !strings.Contains(strings.ToLower(said), "not found") {
			t.Errorf("a refusal says not found: %q for %s", said, query)
		}
	}
	// Neither a person and a circle at once nor nobody.
	world.refused(t, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/a", granteeUserId: %q, granteeCircleId: %q) { id } }`, world.ben.ID, world.home))
	world.refused(t, world.ada, `mutation { ShareAgentPage(path: "people/a") { id } }`)

	// Ben cannot share ada's page, list her shares, or take hers back.
	world.refused(t, world.ben, fmt.Sprintf(`mutation { UnshareAgentPage(shareId: %q) }`, shareId))
	listed := world.call(t, world.ben, `query { ListAgentPageShares { id } }`)
	if rows := listed["ListAgentPageShares"].([]any); len(rows) != 0 {
		t.Errorf("ben sees ada's shares: %v", rows)
	}
	// Someone who cannot use circles cannot share at all.
	world.refused(t, world.eve, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/a", granteeUserId: %q) { id } }`, world.ben.ID))

	listed = world.call(t, world.ada, `query { ListAgentPageShares { id path pageName granteeUsername isQuoteShared } }`)
	rows := listed["ListAgentPageShares"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["granteeUsername"] != world.ben.Username || rows[0].(map[string]any)["path"] != "people/a" {
		t.Fatalf("ada's share: %v", rows)
	}

	// Ben reads it; ada takes it back; ben does not.
	if _, failures := world.read(world.ben, world.ada.ID, "people/a", t); len(failures) > 0 {
		t.Fatalf("ben reads it: %v", failures)
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareAgentPage(shareId: %q) }`, shareId))
	if _, failures := world.read(world.ben, world.ada.ID, "people/a", t); len(failures) == 0 {
		t.Error("ben still reads a page that was taken back")
	}
	world.refused(t, world.ada, fmt.Sprintf(`mutation { UnshareAgentPage(shareId: %q) }`, shareId))
}

// A circle's share reaches its members, and leaving the circle ends the
// reading at once.
func TestACircleShareEndsWhenTheReaderLeaves(t *testing.T) {
	world := newPageShareWorld(t)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareAgentPage(path: "people/a", granteeCircleId: %q) { id } }`, world.home))
	if _, failures := world.read(world.ben, world.ada.ID, "people/a", t); len(failures) > 0 {
		t.Fatalf("a member reads it: %v", failures)
	}
	world.call(t, world.ben, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))
	if _, failures := world.read(world.ben, world.ada.ID, "people/a", t); len(failures) == 0 {
		t.Error("ben left the circle and still reads the page")
	}
}

// The preview is what the share shows, for a person the owner could share
// with, and nothing for anybody else.
func TestThePreviewIsExactlyWhatTheShareShows(t *testing.T) {
	world := newPageShareWorld(t)
	selection := `page { path name opening isQuoteShared facts { number text evidenceSource evidenceQuote } edges { fromPath toPath } children { path } } reachedPageCount isReachedPageCountCut`
	previewed := world.call(t, world.ada, fmt.Sprintf(`query { PreviewAgentPageShare(path: "people/a", granteeUserId: %q) { %s } }`, world.ben.ID, selection))
	preview := previewed["PreviewAgentPageShare"].(map[string]any)
	if preview["reachedPageCount"].(float64) != 2 || preview["isReachedPageCountCut"] != false {
		t.Errorf("the share reaches people/a and the page under it: %v", preview)
	}
	if fact := preview["page"].(map[string]any)["facts"].([]any)[0].(map[string]any); fact["evidenceQuote"] != "" {
		t.Errorf("no quote in a preview without quotes: %v", fact)
	}
	// Nothing is shared by previewing.
	if _, failures := world.read(world.ben, world.ada.ID, "people/a", t); len(failures) == 0 {
		t.Fatal("a preview is not a share")
	}

	world.share(t, "people/a", world.ben, false)
	data, _ := world.read(world.ben, world.ada.ID, "people/a", t)
	actual := data["ReadSharedAgentPage"].(map[string]any)
	expected := preview["page"].(map[string]any)
	for _, key := range []string{"path", "name", "opening", "isQuoteShared"} {
		if fmt.Sprint(actual[key]) != fmt.Sprint(expected[key]) {
			t.Errorf("the preview's %s was %v and the share shows %v", key, expected[key], actual[key])
		}
	}
	if fmt.Sprint(actual["facts"]) != fmt.Sprint(expected["facts"]) || fmt.Sprint(actual["children"]) != fmt.Sprint(expected["children"]) {
		t.Errorf("the preview differs from the share:\n%v\n%v", expected, actual)
	}

	// For somebody the owner cannot share with, a page she does not have,
	// and a circle she is not in: not found.
	world.refused(t, world.ada, fmt.Sprintf(`query { PreviewAgentPageShare(path: "people/a", granteeUserId: %q) { reachedPageCount } }`, world.cora.ID))
	world.refused(t, world.ada, fmt.Sprintf(`query { PreviewAgentPageShare(path: "people/nobody", granteeUserId: %q) { reachedPageCount } }`, world.ben.ID))
	world.refused(t, world.ada, `query { PreviewAgentPageShare(path: "people/a", granteeCircleId: "nobody-has-this-id") { reachedPageCount } }`)
	// Ben has no pages of his own to preview.
	world.refused(t, world.ben, fmt.Sprintf(`query { PreviewAgentPageShare(path: "people/a", granteeUserId: %q) { reachedPageCount } }`, world.ada.ID))
}

// The reader lists the roots shared with them, grouped by who shared them.
func TestTheReaderListsWhatIsSharedWithThem(t *testing.T) {
	world := newPageShareWorld(t)
	world.share(t, "people/a", world.ben, false)
	world.share(t, "projects/private", world.ben, false)
	data := world.call(t, world.ben, `query { ListSharedAgentPages { ownerUserId ownerUsername ownerName ownerAvatarColorIndex path name opening } }`)
	rows := data["ListSharedAgentPages"].([]any)
	if len(rows) != 2 {
		t.Fatalf("two roots: %v", rows)
	}
	first := rows[0].(map[string]any)
	if first["ownerUserId"] != world.ada.ID || first["ownerName"] != "Page-ada" || first["path"] != "people/a" || first["opening"] != "Lives by the sea" {
		t.Errorf("a root with its owner and opening: %v", first)
	}
}
