package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func aPersonPage(t *testing.T, tx db.Transaction, agent *models.Agent, path string) *models.AgentNode {
	t.Helper()
	node, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: path, Kind: models.NodePerson, Name: "Fixture Person"})
	if err != nil {
		t.Fatalf("PutAgentNode(%s): %s", path, err)
	}
	return node
}

// The same page, record and relation said twice is one link; a different
// relation, or a different passage of one recording, is another.
func TestRecordLinkUniqueKey(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		page := aPersonPage(t, tx, agent, "people/fixture-one")
		base := models.AgentRecordLink{AgentID: agent.ID, NodeID: page.ID, RecordKind: models.RecordMail, RecordID: "item-1", RecordRelation: models.RecordRelationWrote}
		for count := 0; count < 2; count++ {
			link := base
			if _, err := tx.PutAgentRecordLink(&link); err != nil {
				t.Fatalf("PutAgentRecordLink: %s", err)
			}
		}
		other := base
		other.RecordRelation = models.RecordRelationReceived
		if _, err := tx.PutAgentRecordLink(&other); err != nil {
			t.Fatal(err)
		}
		links, err := tx.ListAgentRecordLinks(agent.ID, page.ID)
		if err != nil || len(links) != 2 {
			t.Fatalf("two relations to one mail are two links, and the repeat is not a third: %d %v", len(links), err)
		}

		passage := models.AgentRecordLink{AgentID: agent.ID, NodeID: page.ID, RecordKind: models.RecordRecordingPassage, RecordID: "recording-1", RecordRelation: models.RecordRelationSpokeIn}
		for _, span := range [][2]int64{{1000, 2000}, {5000, 6000}, {1000, 2000}} {
			link := passage
			link.PassageStartMS, link.PassageEndMS = span[0], span[1]
			if _, err := tx.PutAgentRecordLink(&link); err != nil {
				t.Fatal(err)
			}
		}
		links, _ = tx.ListAgentRecordLinks(agent.ID, page.ID)
		if len(links) != 4 {
			t.Fatalf("two passages of one recording are two links: %d", len(links))
		}
	})
}

// A link is checked before it is stored, and a page that is not the
// agent's cannot be linked to by it.
func TestRecordLinkIsValidatedAndScopedToTheAgent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mine, theirs := graphAgent(t, tx), graphAgent(t, tx)
		myPage := aPersonPage(t, tx, mine, "people/fixture-mine")
		theirPage := aPersonPage(t, tx, theirs, "people/fixture-theirs")

		for name, link := range map[string]models.AgentRecordLink{
			"unknown kind":               {AgentID: mine.ID, NodeID: myPage.ID, RecordKind: "diary", RecordID: "x", RecordRelation: models.RecordRelationAbout},
			"relation that does not fit": {AgentID: mine.ID, NodeID: myPage.ID, RecordKind: models.RecordFinanceTransaction, RecordID: "x", RecordRelation: models.RecordRelationSpokeIn},
			"no record":                  {AgentID: mine.ID, NodeID: myPage.ID, RecordKind: models.RecordMail, RecordRelation: models.RecordRelationWrote},
			"range on a mail":            {AgentID: mine.ID, NodeID: myPage.ID, RecordKind: models.RecordMail, RecordID: "x", RecordRelation: models.RecordRelationWrote, PassageEndMS: 5},
			"unknown status":             {AgentID: mine.ID, NodeID: myPage.ID, RecordKind: models.RecordMail, RecordID: "x", RecordRelation: models.RecordRelationWrote, LinkStatus: "certain"},
		} {
			link := link
			if _, err := tx.PutAgentRecordLink(&link); err == nil {
				t.Errorf("%s was stored", name)
			}
		}

		// The other agent's page, named by the first agent.
		_, err := tx.PutAgentRecordLink(&models.AgentRecordLink{AgentID: mine.ID, NodeID: theirPage.ID, RecordKind: models.RecordMail, RecordID: "x", RecordRelation: models.RecordRelationWrote})
		if err == nil {
			t.Errorf("a link was written onto somebody else's page")
		}
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{AgentID: theirs.ID, NodeID: theirPage.ID, RecordKind: models.RecordMail, RecordID: "shared-id", RecordRelation: models.RecordRelationWrote}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{AgentID: mine.ID, NodeID: myPage.ID, RecordKind: models.RecordMail, RecordID: "shared-id", RecordRelation: models.RecordRelationReceived}); err != nil {
			t.Fatal(err)
		}
		// Each agent sees only the pages of its own graph for one record.
		back, err := tx.ListAgentRecordBacklinks(mine.ID, models.RecordMail, "shared-id", 0, 0, 10)
		if err != nil || len(back) != 1 || back[0].NodePath != "people/fixture-mine" {
			t.Fatalf("backlinks crossed agents: %v %v", back, err)
		}
		if links, _ := tx.ListAgentRecordLinks(mine.ID, theirPage.ID); len(links) != 0 {
			t.Errorf("another agent's page listed links to this agent: %v", links)
		}
		removed, err := tx.DeleteAgentRecordLink(mine.ID, theirPage.ID, models.RecordMail, "shared-id", "", 0, 0)
		if err != nil || removed != 0 {
			t.Errorf("a link on another agent's page was removed: %d %v", removed, err)
		}
	})
}

// A person confirming a proposal states it, and a proposal made again
// never takes a statement back.
func TestRecordLinkStatementIsNotTakenBack(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		page := aPersonPage(t, tx, agent, "people/fixture-status")
		put := func(status models.AgentEdgeStatus, note string) *models.AgentRecordLink {
			stored, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
				AgentID: agent.ID, NodeID: page.ID, RecordKind: models.RecordContact, RecordID: "card-1",
				RecordRelation: models.RecordRelationIsContact, LinkStatus: status, Note: note,
			})
			if err != nil {
				t.Fatal(err)
			}
			return stored
		}
		if stored := put(models.EdgeProposed, "same address"); !stored.Proposed() {
			t.Fatalf("a proposal is stored as proposed: %s", stored.LinkStatus)
		}
		if stored := put(models.EdgeStated, ""); stored.Proposed() || stored.Note != "same address" {
			t.Fatalf("stating a proposal confirms it and keeps the note: %+v", stored)
		}
		if stored := put(models.EdgeProposed, ""); stored.Proposed() {
			t.Fatalf("a proposal made again took back what was stated")
		}
	})
}

// Which pages link to a moment of a recording is a range overlap.
func TestRecordBacklinksOverlapARecordingRange(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		early, late := aPersonPage(t, tx, agent, "people/fixture-early"), aPersonPage(t, tx, agent, "people/fixture-late")
		for page, span := range map[*models.AgentNode][2]int64{early: {1000, 2000}, late: {9000, 10000}} {
			if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
				AgentID: agent.ID, NodeID: page.ID, RecordKind: models.RecordRecordingPassage, RecordID: "recording-1",
				RecordRelation: models.RecordRelationSpokeIn, PassageStartMS: span[0], PassageEndMS: span[1],
			}); err != nil {
				t.Fatal(err)
			}
		}
		paths := func(startMS, endMS int64) string {
			links, err := tx.ListAgentRecordBacklinks(agent.ID, models.RecordRecordingPassage, "recording-1", startMS, endMS, 10)
			if err != nil {
				t.Fatal(err)
			}
			text := ""
			for _, link := range links {
				text += link.NodePath + ";"
			}
			return text
		}
		if got := paths(1500, 1600); got != "people/fixture-early;" {
			t.Errorf("inside the early passage: %q", got)
		}
		if got := paths(1900, 9100); got != "people/fixture-early;people/fixture-late;" {
			t.Errorf("a range that touches both: %q", got)
		}
		if got := paths(3000, 4000); got != "" {
			t.Errorf("a range between them: %q", got)
		}
		if got := paths(0, 0); got != "people/fixture-early;people/fixture-late;" {
			t.Errorf("no range is the whole recording: %q", got)
		}
		if _, err := tx.ListAgentRecordBacklinks(agent.ID, models.RecordRecordingPassage, "recording-1", 5, 1, 10); err == nil {
			t.Errorf("a range that ends before it starts was accepted")
		}
	})
}

// Folding one page into another brings its links, and the card it names,
// and deleting a page takes its links.
func TestRecordLinksFollowAMergeAndGoWithAPage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		from, into := aPersonPage(t, tx, agent, "people/fixture-from"), aPersonPage(t, tx, agent, "people/fixture-into")
		for _, link := range []models.AgentRecordLink{
			{NodeID: from.ID, RecordKind: models.RecordMail, RecordID: "item-1", RecordRelation: models.RecordRelationWrote},
			{NodeID: from.ID, RecordKind: models.RecordMail, RecordID: "item-2", RecordRelation: models.RecordRelationWrote},
			{NodeID: into.ID, RecordKind: models.RecordMail, RecordID: "item-2", RecordRelation: models.RecordRelationWrote},
		} {
			link.AgentID = agent.ID
			if _, err := tx.PutAgentRecordLink(&link); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.SetAgentNodeContact(agent.ID, from.ID, "card-1"); err != nil {
			t.Fatal(err)
		}
		merged, err := tx.MergeAgentNodes(agent.ID, from.Path, into.Path)
		if err != nil {
			t.Fatal(err)
		}
		if merged.ContactID != "card-1" {
			t.Errorf("the survivor did not take the card: %q", merged.ContactID)
		}
		links, _ := tx.ListAgentRecordLinks(agent.ID, into.ID)
		if len(links) != 2 {
			t.Errorf("the survivor has the union of the links, once each: %d", len(links))
		}
		if _, err := tx.DeleteAgentNode(agent.ID, into.Path); err != nil {
			t.Fatal(err)
		}
		back, _ := tx.ListAgentRecordBacklinks(agent.ID, models.RecordMail, "item-2", 0, 0, 10)
		if len(back) != 0 {
			t.Errorf("a deleted page's links outlived it: %d", len(back))
		}
	})
}

// Naming a card on a page writes that one column and refuses a page that
// is not the agent's.
func TestSetAgentNodeContactIsScopedToTheAgent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mine, theirs := graphAgent(t, tx), graphAgent(t, tx)
		page := aPersonPage(t, tx, mine, "people/fixture-card")
		other := aPersonPage(t, tx, theirs, "people/fixture-other")
		if err := tx.SetAgentNodeContact(mine.ID, other.ID, "card-1"); err == nil {
			t.Errorf("a card was named on another agent's page")
		}
		if err := tx.SetAgentNodeContact(mine.ID, page.ID, "card-1"); err != nil {
			t.Fatal(err)
		}
		if found, _ := tx.GetAgentNodeByID(mine.ID, page.ID); found.ContactID != "card-1" {
			t.Errorf("the card was not named: %q", found.ContactID)
		}
		if err := tx.SetAgentNodeContact(mine.ID, page.ID, ""); err != nil {
			t.Fatal(err)
		}
		if found, _ := tx.GetAgentNodeByID(mine.ID, page.ID); found.ContactID != "" {
			t.Errorf("the card was not cleared: %q", found.ContactID)
		}
	})
}

// A link taken away by hand is remembered for the automatic passes, making
// it by hand again forgets that, and a link made while dreaming does not.
func TestRemovedRecordLinksAreRememberedUntilMadeByHand(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		page := aPersonPage(t, tx, agent, "people/fixture-one")
		link := models.AgentRecordLink{AgentID: agent.ID, NodeID: page.ID, RecordKind: models.RecordRecordingPassage, RecordID: "recording-1",
			PassageStartMS: 1000, PassageEndMS: 2000, RecordRelation: models.RecordRelationSpokeIn, LinkStatus: models.EdgeProposed}
		key := models.RecordLinkKey(page.ID, link.RecordKind, link.RecordID, link.RecordRelation, 1000, 2000)
		if _, err := tx.PutAgentRecordLink(&link); err != nil {
			t.Fatal(err)
		}
		if dismissed, err := tx.ListAgentRecordLinkDismissals(agent.ID); err != nil || len(dismissed) != 0 {
			t.Fatalf("nothing is dismissed by making a link: %v %v", dismissed, err)
		}
		if removed, err := tx.DeleteAgentRecordLink(agent.ID, page.ID, link.RecordKind, link.RecordID, "", 0, 0); err != nil || removed != 1 {
			t.Fatalf("remove: %d %v", removed, err)
		}
		if dismissed, _ := tx.ListAgentRecordLinkDismissals(agent.ID); !dismissed[key] {
			t.Fatalf("the removal is remembered with the link's key: %v", dismissed)
		}

		dreaming := link
		tx.AsActor(models.ActorDream)
		if _, err := tx.PutAgentRecordLink(&dreaming); err != nil {
			t.Fatal(err)
		}
		if dismissed, _ := tx.ListAgentRecordLinkDismissals(agent.ID); !dismissed[key] {
			t.Fatalf("a dream does not undo a removal: %v", dismissed)
		}

		tx.AsActor(models.ActorPerson)
		byHand := link
		byHand.LinkStatus = models.EdgeStated
		if _, err := tx.PutAgentRecordLink(&byHand); err != nil {
			t.Fatal(err)
		}
		if dismissed, _ := tx.ListAgentRecordLinkDismissals(agent.ID); len(dismissed) != 0 {
			t.Fatalf("making it by hand forgets the removal: %v", dismissed)
		}
		statuses, err := tx.ListAgentRecordLinkStatuses(agent.ID)
		if err != nil || statuses[key] != models.EdgeStated {
			t.Fatalf("statuses: %v %v", statuses, err)
		}
	})
}

// Newest first by the message's own time needs no other option to say so.
func TestItemsByReceivedTimeNeedNoOtherOption(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "fixture-received", Name: "Fixture Person"})
		if err != nil {
			t.Fatal(err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatal(err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("no Inbox: %v", err)
		}
		var newest string
		for _, age := range []time.Duration{3 * time.Hour, time.Hour, 2 * time.Hour} {
			mail, err := tx.CreateMail(&models.Mail{Subject: "Fixture", Kind: models.MailKindIncoming, ReceivedAt: time.Now().Add(-age)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			item, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatal(err)
			}
			if age == time.Hour {
				newest = item.ID
			}
		}
		items, err := tx.ListItems(inbox.ID, &db.ItemOptions{ByReceived: true, Limit: 2})
		if err != nil || len(items) != 2 || items[0].ID != newest {
			t.Fatalf("the newest message first: %v %v", items, err)
		}
	})
}
