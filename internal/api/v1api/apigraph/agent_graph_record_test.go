package apigraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type recordWorld struct {
	owner, stranger          *models.User
	agent, strangerAgent     *models.Agent
	page, strangerPage       *models.AgentNode
	ownCard, closedCard      *models.Contact
	strangerCard             *models.Contact
	ownContext, otherContext func(db.Transaction) context.Context
}

func aRecordWorld(t *testing.T, tx db.Transaction) *recordWorld {
	t.Helper()
	world := &recordWorld{}
	build := func(username string) (*models.User, *models.Agent, *models.AgentNode) {
		owner, err := tx.CreateUser(&models.User{Username: username, Name: "Fixture Person"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			t.Fatal(err)
		}
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "people/fixture-friend", Kind: models.NodePerson, Name: "Fixture Friend"})
		if err != nil {
			t.Fatal(err)
		}
		return owner, agent, page
	}
	world.owner, world.agent, world.page = build("record-owner")
	world.stranger, world.strangerAgent, world.strangerPage = build("record-stranger")
	card := func(user *models.User, name string, isGranted bool) *models.Contact {
		book, err := tx.CreateAddressBook(&models.AddressBook{UserID: user.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if isGranted {
			book.AgentGranted = true
			if _, err := tx.UpdateAddressBook(book); err != nil {
				t.Fatal(err)
			}
		}
		contact, err := tx.PutContact(&models.Contact{AddressBookID: book.ID, UID: name, ETag: "e", Card: "x", Name: name + " Card", Emails: []string{name + "@example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		return contact
	}
	world.ownCard = card(world.owner, "Granted", true)
	world.closedCard = card(world.owner, "Closed", false)
	world.strangerCard = card(world.stranger, "Strangers", true)
	contextFor := func(user *models.User) func(db.Transaction) context.Context {
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
		return func(tx db.Transaction) context.Context {
			return api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)
		}
	}
	world.ownContext, world.otherContext = contextFor(world.owner), contextFor(world.stranger)
	return world
}

// The person links a page to their own record, sees it on the page with a
// title, may do it for a book the agent was not given, and cannot link
// anybody else's record or one that is not there.
func TestLinkAgentRecordIsTheirOwnRecordsOnly(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)

		view, err := resolver.LinkAgentRecord(ctx, LinkAgentRecordArguments{
			Path: "people/fixture-friend", RecordKind: "contact", RecordID: world.closedCard.ID, RecordRelation: "is_contact",
		})
		if err != nil {
			t.Fatalf("a card in a book the agent was not given is still theirs to link: %s", err)
		}
		if view.LinkStatus != models.EdgeStated || view.RecordTitle != "Closed Card" || view.IsUnavailable {
			t.Errorf("the person's own link reads whole: %+v", view)
		}

		for name, arguments := range map[string]LinkAgentRecordArguments{
			"a stranger's card":        {Path: "people/fixture-friend", RecordKind: "contact", RecordID: world.strangerCard.ID, RecordRelation: "is_contact"},
			"a card that is gone":      {Path: "people/fixture-friend", RecordKind: "contact", RecordID: "missing", RecordRelation: "is_contact"},
			"a kind that is not":       {Path: "people/fixture-friend", RecordKind: "diary", RecordID: "x", RecordRelation: "about"},
			"a relation that fits not": {Path: "people/fixture-friend", RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "spoke_in"},
			"a page that is not":       {Path: "people/nobody", RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "is_contact"},
		} {
			if _, err := resolver.LinkAgentRecord(ctx, arguments); err == nil {
				t.Errorf("linking %s was allowed", name)
			}
		}
		// Another person's page, named by this one's path, is not theirs.
		if links, _ := tx.ListAgentRecordLinks(world.strangerAgent.ID, world.strangerPage.ID); len(links) != 0 {
			t.Errorf("a link landed on the stranger's page: %v", links)
		}
	})
}

// The page shows every link to the person, a record that is gone reads as
// unavailable and can still be removed, and a stranger asking about the
// same record is told of nothing.
func TestAPageShowsItsRecordLinksAndTheirBacklinks(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)
		for _, recordId := range []string{world.ownCard.ID, world.closedCard.ID} {
			if _, err := resolver.LinkAgentRecord(ctx, LinkAgentRecordArguments{Path: "people/fixture-friend", RecordKind: "contact", RecordID: recordId, RecordRelation: "about"}); err != nil {
				t.Fatal(err)
			}
		}
		// Written by an automatic pass or by the agent, for a record that
		// has since gone.
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
			AgentID: world.agent.ID, NodeID: world.page.ID, RecordKind: models.RecordMail, RecordID: "item-gone",
			RecordRelation: models.RecordRelationReceived, LinkStatus: models.EdgeProposed,
		}); err != nil {
			t.Fatal(err)
		}

		page, err := resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: "people/fixture-friend"})
		if err != nil {
			t.Fatal(err)
		}
		if page.RecordLinkCount != 3 || len(page.RecordLinks) != 3 {
			t.Fatalf("the page carries its links: %d %d", page.RecordLinkCount, len(page.RecordLinks))
		}
		var titles []string
		for _, view := range page.RecordLinks {
			if view.RecordKind == models.RecordMail {
				if !view.IsUnavailable || view.RecordTitle != "" || view.LinkStatus != models.EdgeProposed {
					t.Errorf("a record that is gone is unavailable, and still a proposal: %+v", view)
				}
				continue
			}
			titles = append(titles, view.RecordTitle)
			if view.DashboardPath == "" {
				t.Errorf("a record that is there opens: %+v", view)
			}
		}
		if len(titles) != 2 {
			t.Errorf("both cards, granted to the agent or not, are read by the person: %v", titles)
		}

		listed, err := resolver.ListAgentRecordLinks(ctx, ListAgentRecordLinksArguments{Path: "people/fixture-friend", First: 2})
		if err != nil || len(listed.RecordLinks) != 2 || listed.NextOffset != 2 || listed.RecordLinkCount != 3 {
			t.Fatalf("a part of the links and where the next starts: %+v %v", listed, err)
		}
		rest, _ := resolver.ListAgentRecordLinks(ctx, ListAgentRecordLinksArguments{Path: "people/fixture-friend", First: 2, Offset: 2})
		if len(rest.RecordLinks) != 1 || rest.NextOffset != 0 {
			t.Errorf("the last part ends the list: %+v", rest)
		}

		back, err := resolver.ReadRecordMemoryLinks(ctx, ReadRecordMemoryLinksArguments{RecordKind: "contact", RecordID: world.ownCard.ID})
		if err != nil || len(back) != 1 || back[0].NodePath != "people/fixture-friend" || back[0].RecordTitle != "Granted Card" {
			t.Fatalf("the pages that link to a card: %+v %v", back, err)
		}
		// The stranger has their own page and their own graph, and asking
		// for the same record finds none of this person's links.
		strangers, err := resolver.ReadRecordMemoryLinks(world.otherContext(tx), ReadRecordMemoryLinksArguments{RecordKind: "contact", RecordID: world.ownCard.ID})
		if err != nil || len(strangers) != 0 {
			t.Errorf("another person asking about this person's record: %+v %v", strangers, err)
		}
		if _, err := resolver.ListAgentRecordLinks(world.otherContext(tx), ListAgentRecordLinksArguments{Path: "people/fixture-friend"}); err != nil {
			t.Errorf("a stranger reads the links of their own page of that name: %v", err)
		}
		if _, err := resolver.ReadRecordMemoryLinks(ctx, ReadRecordMemoryLinksArguments{RecordKind: "diary", RecordID: "x"}); err == nil {
			t.Errorf("an unknown kind was answered")
		}

		removed, err := resolver.UnlinkAgentRecord(ctx, UnlinkAgentRecordArguments{Path: "people/fixture-friend", RecordKind: "mail", RecordID: "item-gone"})
		if err != nil || !removed {
			t.Errorf("a link to a record that is gone can be removed: %v %v", removed, err)
		}
		removed, _ = resolver.UnlinkAgentRecord(world.otherContext(tx), UnlinkAgentRecordArguments{Path: "people/fixture-friend", RecordKind: "contact", RecordID: world.ownCard.ID})
		if removed {
			t.Errorf("a stranger removed this person's link")
		}
	})
}

// Naming the card a person's page is about writes the column and an
// is_contact link, takes only their own cards, refuses the self page, and
// clearing it takes the link with it. Nothing makes a card.
func TestSetAgentNodeContact(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)

		if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: "people/fixture-friend", ContactID: world.strangerCard.ID}); err == nil {
			t.Errorf("a stranger's card was named")
		}
		if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: models.PathSelf, ContactID: world.ownCard.ID}); err == nil {
			t.Errorf("the self page takes its card from SetMyContact")
		}
		if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: "people/fixture-friend", ContactID: world.ownCard.ID}); err != nil {
			t.Fatal(err)
		}
		page, _ := resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: "people/fixture-friend"})
		if page.Node.ContactID != world.ownCard.ID || page.Contact == nil || page.Contact.ID != world.ownCard.ID {
			t.Errorf("the page names the card and reads it: %+v", page.Node)
		}
		if page.RecordLinkCount != 1 || page.RecordLinks[0].RecordRelation != models.RecordRelationIsContact {
			t.Errorf("an is_contact link was written alongside: %+v", page.RecordLinks)
		}
		// Naming another card moves the link.
		if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: "people/fixture-friend", ContactID: world.closedCard.ID}); err != nil {
			t.Fatal(err)
		}
		page, _ = resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: "people/fixture-friend"})
		if page.RecordLinkCount != 1 || page.RecordLinks[0].RecordID != world.closedCard.ID {
			t.Errorf("the link followed the card: %+v", page.RecordLinks)
		}
		if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: "people/fixture-friend"}); err != nil {
			t.Fatal(err)
		}
		page, _ = resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: "people/fixture-friend"})
		if page.Node.ContactID != "" || page.Contact != nil || page.RecordLinkCount != 0 {
			t.Errorf("clearing takes the card and its link: %+v %d", page.Node, page.RecordLinkCount)
		}
	})
}

// A fact whose evidence names a record carries the record, found, so that
// its source opens it; and evidence that names a record that is gone says so.
func TestFactEvidenceNamesTheRecordItWasReadFrom(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)
		recording, err := tx.CreateRecording(&models.Recording{UserID: world.owner.ID, ClientRecordingID: "fixture", RecordingTitle: "Fixture review", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: world.agent.ID, NodeID: world.page.ID, Kind: models.FactPlain, Text: "Prefers the early slot.",
			Evidence: []models.Evidence{
				{Kind: models.EvidenceRecordingPassage, ID: recording.ID, TranscriptRunID: "run-1", StartMS: 1500, EndMS: 4000},
				{Kind: models.EvidenceContact, ID: "contact-gone"},
				{Kind: models.EvidencePerson},
			},
		}); err != nil {
			t.Fatal(err)
		}
		page, err := resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: "people/fixture-friend"})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.EvidenceRecords) != 2 {
			t.Fatalf("the two evidences that name records are resolved, the person's word is not: %+v", page.EvidenceRecords)
		}
		passage, gone := page.EvidenceRecords[0], page.EvidenceRecords[1]
		if passage.EvidenceIndex != 0 || passage.RecordTitle != "Fixture review" || passage.PassageStartMS != 1500 ||
			passage.DashboardPath != "/recordings/"+recording.ID+"?endMS=4000&startMS=1500&version=run-1" {
			t.Errorf("a passage opens the recording at the range: %+v", passage)
		}
		if gone.EvidenceIndex != 1 || !gone.IsUnavailable {
			t.Errorf("a contact that is gone: %+v", gone)
		}
	})
}

// Confirming a proposed is_contact link names the card on the page, as the
// memory tool does, and removing that link takes the card away and is
// remembered as the person's doing. Removing some other link to the same
// card leaves both the card and its link where they were.
func TestConfirmingAndRemovingAContactLinkMovesThePagesCard(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)
		pagePath := "people/fixture-friend"
		if _, err := tx.PutAgentRecordLink(&models.AgentRecordLink{
			AgentID: world.agent.ID, NodeID: world.page.ID, RecordKind: models.RecordContact, RecordID: world.ownCard.ID,
			RecordRelation: models.RecordRelationIsContact, LinkStatus: models.EdgeProposed,
		}); err != nil {
			t.Fatal(err)
		}
		page, err := resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: pagePath})
		if err != nil || page.Node.ContactID != "" {
			t.Fatalf("a guess names no card: %+v %v", page.Node, err)
		}

		if _, err := resolver.LinkAgentRecord(ctx, LinkAgentRecordArguments{
			Path: pagePath, RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "is_contact",
		}); err != nil {
			t.Fatal(err)
		}
		page, _ = resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: pagePath})
		if page.Node.ContactID != world.ownCard.ID || page.Contact == nil {
			t.Fatalf("confirming the link names the card on the page: %+v", page.Node)
		}

		// Another relation to the same card is not the card's link.
		if _, err := resolver.LinkAgentRecord(ctx, LinkAgentRecordArguments{
			Path: pagePath, RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "about",
		}); err != nil {
			t.Fatal(err)
		}
		removed, err := resolver.UnlinkAgentRecord(ctx, UnlinkAgentRecordArguments{
			Path: pagePath, RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "about",
		})
		if err != nil || !removed {
			t.Fatalf("the about link goes: %v %v", removed, err)
		}
		page, _ = resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: pagePath})
		if page.Node.ContactID != world.ownCard.ID || page.RecordLinkCount != 1 {
			t.Errorf("removing an about link leaves the card: %+v %d", page.Node, page.RecordLinkCount)
		}

		removed, err = resolver.UnlinkAgentRecord(ctx, UnlinkAgentRecordArguments{
			Path: pagePath, RecordKind: "contact", RecordID: world.ownCard.ID, RecordRelation: "is_contact",
		})
		if err != nil || !removed {
			t.Fatalf("the is_contact link goes: %v %v", removed, err)
		}
		page, _ = resolver.AgentGraphPage(ctx, AgentGraphPageReadArguments{Path: pagePath})
		if page.Node.ContactID != "" || page.Contact != nil || page.RecordLinkCount != 0 {
			t.Errorf("removing the is_contact link takes the card: %+v %d", page.Node, page.RecordLinkCount)
		}
		dismissed, err := tx.ListAgentRecordLinkDismissals(world.agent.ID)
		key := models.RecordLinkKey(world.page.ID, models.RecordContact, world.ownCard.ID, models.RecordRelationIsContact, 0, 0)
		if err != nil || !dismissed[key] {
			t.Errorf("the person's removal is remembered: %v %v", dismissed, err)
		}
	})
}

// Swapping a page's card for another is not the person turning the old card
// down, so the old card may be proposed again.
func TestSwappingAPagesCardDoesNotDismissTheOldOne(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)
		for _, card := range []*models.Contact{world.ownCard, world.closedCard} {
			if _, err := resolver.SetAgentNodeContact(ctx, SetAgentNodeContactArguments{Path: "people/fixture-friend", ContactID: card.ID}); err != nil {
				t.Fatal(err)
			}
		}
		dismissed, err := tx.ListAgentRecordLinkDismissals(world.agent.ID)
		if err != nil || len(dismissed) != 0 {
			t.Errorf("a swap dismisses nothing: %v %v", dismissed, err)
		}
	})
}

// A link to a passage of a recording may name only a transcript run of that
// recording, of a sane length.
func TestLinkingAPassageChecksItsTranscriptRun(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := aRecordWorld(t, tx)
		ctx := world.ownContext(tx)
		runOf := func(clientRecordingId string) (*models.Recording, *models.RecordingTranscriptRun) {
			recording, err := tx.CreateRecording(&models.Recording{UserID: world.owner.ID, ClientRecordingID: clientRecordingId, RecordingTitle: clientRecordingId, RecordingOrigin: models.RecordingOriginAudioImport})
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("audio of " + clientRecordingId)
			digest := sha256.Sum256(content)
			digestText := hex.EncodeToString(digest[:])
			media, err := tx.CreateRecordingMedia(world.owner.ID, &models.RecordingMedia{
				RecordingID: recording.ID, ClientMediaID: clientRecordingId + "-media", OriginalFilename: "original.wav",
				OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestText,
			}, 1024, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.PutRecordingMediaPart(world.owner.ID, &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: 0, PartSizeBytes: int64(len(content)), PartSHA256: digestText}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := tx.CompleteRecordingMedia(world.owner.ID, media.ID, digestText); err != nil {
				t.Fatal(err)
			}
			transcriptRun, err := tx.CreateRecordingTranscriptRun(world.owner.ID, media.ID, world.agent.ID, clientRecordingId+"-run", "fixture:diarize", false)
			if err != nil {
				t.Fatal(err)
			}
			return recording, transcriptRun
		}
		recording, transcriptRun := runOf("first-fixture")
		_, otherRun := runOf("second-fixture")

		link := func(transcriptRunId string) error {
			_, err := resolver.LinkAgentRecord(ctx, LinkAgentRecordArguments{
				Path: "people/fixture-friend", RecordKind: "recording_passage", RecordID: recording.ID, RecordRelation: "spoke_in",
				TranscriptRunID: transcriptRunId, PassageStartMS: 1000, PassageEndMS: 2000,
			})
			return err
		}
		for name, transcriptRunId := range map[string]string{
			"a run of another recording": otherRun.ID,
			"a run that is not there":    "missing-run",
			"a run too long to be one":   strings.Repeat("r", 200),
		} {
			if err := link(transcriptRunId); err == nil {
				t.Errorf("%s was accepted", name)
			}
		}
		if err := link(transcriptRun.ID); err != nil {
			t.Errorf("a run of the recording is accepted: %s", err)
		}
		if err := link(""); err != nil {
			t.Errorf("no run is accepted: %s", err)
		}
	})
}
