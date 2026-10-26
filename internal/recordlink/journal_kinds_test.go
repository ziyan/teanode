package recordlink_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// Every kind a journal day links to is found again for its owner and for
// nobody else, and a record that is gone reads as unavailable.

func TestAPhotoAndAVisitAreTheOwnersAlone(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var alice, bob *person
	var photoId, visitId string
	takenAt := time.Date(2026, 10, 22, 23, 30, 0, 0, time.UTC)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob = aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob")
		photo, _, err := tx.CreatePhoto(&models.Photo{UserID: alice.user.ID, ClientPhotoID: "harbor", PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine, TakenAt: &takenAt, PlaceName: "Harbor"})
		if err != nil {
			t.Fatal(err)
		}
		photoId = photo.ID
		digest := sha256.Sum256([]byte("blob of harbor"))
		blob := hex.EncodeToString(digest[:])
		if _, err := tx.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: blob, BlobSizeBytes: 100}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetPhotoFile(alice.user.ID, photo.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: blob, FileSizeBytes: 100, ContentType: "image/jpeg"}, 0); err != nil {
			t.Fatal(err)
		}
		place, err := tx.CreatePlace(alice.user.ID, &models.Place{PlaceName: "Harbor Cafe", LatitudeDegrees: 10.5, LongitudeDegrees: 20.5})
		if err != nil {
			t.Fatal(err)
		}
		arrived := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
		visit := &models.PlaceVisit{PlaceID: place.ID, ClientVisitKey: "visit-one", VisitSource: models.VisitSourceVisit, ArrivedAt: &arrived, LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, AccuracyMeters: 20}
		if _, err := tx.PutPlaceVisit(alice.user.ID, visit); err != nil {
			t.Fatal(err)
		}
		found, err := tx.GetPlaceVisitByClientKey(alice.user.ID, "visit-one")
		if err != nil || found == nil {
			t.Fatalf("the visit: %v %v", found, err)
		}
		visitId = found.ID
	})
	dbtest.MarkPhotoReady(t, database, photoId)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, reader := range []recordlink.Reader{alice.person, alice.robot} {
			photo := resolve(t, tx, reader, models.RecordPhoto, photoId)
			if !photo.IsReadable() || photo.Title != "Photo at Harbor" || photo.DashboardPath != "/photos/"+photoId || photo.HappenedAt == nil || !photo.HappenedAt.Equal(takenAt) {
				t.Errorf("the owner's photo: %+v", photo)
			}
			visit := resolve(t, tx, reader, models.RecordPlaceVisit, visitId)
			if !visit.IsReadable() || visit.Title != "Visit to Harbor Cafe" || !strings.HasPrefix(visit.DashboardPath, "/location?day=") {
				t.Errorf("the owner's visit: %+v", visit)
			}
		}
		for _, reader := range []recordlink.Reader{bob.person, bob.robot} {
			if resolved := resolve(t, tx, reader, models.RecordPhoto, photoId); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("another person's photo is not found: %+v", resolved)
			}
			if resolved := resolve(t, tx, reader, models.RecordPlaceVisit, visitId); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("another person's visit is not found: %+v", resolved)
			}
		}
		if resolved := resolve(t, tx, alice.person, models.RecordPhoto, "missing"); !resolved.IsUnavailable {
			t.Errorf("a photo that is gone: %+v", resolved)
		}
	})
}

func TestADocumentARecordingAnIdeaAndAGoalBelongToTheirAgent(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob")
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: alice.agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/records", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatal(err)
		}
		happenedAt := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
		page, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: alice.agent.ID, SourceID: source.ID, ExternalID: "page-one", Kind: models.DocumentPage,
			Title: "Trip plan", Hash: "hash-one", HappenedAt: &happenedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		dayFile, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: alice.agent.ID, SourceID: source.ID, ExternalID: "location:2026-10-22", Kind: models.DocumentJournal,
			Title: "Where they went", URL: "/location?day=2026-10-22", Hash: "hash-two", HappenedAt: &happenedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		recording, err := tx.CreateRecording(&models.Recording{UserID: alice.user.ID, ClientRecordingID: "standup", RecordingTitle: "Standup", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			t.Fatal(err)
		}
		idea, err := tx.UpsertAgentIdea(&models.AgentIdea{AgentID: alice.agent.ID, IdeaKey: "bread", IdeaKind: models.IdeaCatalog, IdeaCategory: "home", Headline: "Plan the bake"})
		if err != nil {
			t.Fatal(err)
		}
		goal, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: alice.agent.ID, Kind: models.AgentConversationGoal, Title: "Goal conversation", GoalTitle: "Learn to bake", LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		named, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: alice.agent.ID, Kind: models.AgentConversationNamed, Title: "Named", LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}

		for _, reader := range []recordlink.Reader{alice.person, alice.robot} {
			document := resolve(t, tx, reader, models.RecordAgentDocument, page.ID)
			if !document.IsReadable() || document.Title != "Trip plan" || document.DashboardPath != "/knowledge?documents="+url.QueryEscape("Trip plan") || !document.HappenedAt.Equal(happenedAt) {
				t.Errorf("a document: %+v", document)
			}
			if document := resolve(t, tx, reader, models.RecordAgentDocument, dayFile.ID); document.DashboardPath != "/location?day=2026-10-22" {
				t.Errorf("a document that names a page opens it: %+v", document)
			}
			if resolved := resolve(t, tx, reader, models.RecordRecording, recording.ID); !resolved.IsReadable() || resolved.Title != "Standup" || resolved.DashboardPath != "/recordings/"+recording.ID {
				t.Errorf("a recording: %+v", resolved)
			}
			if resolved := resolve(t, tx, reader, models.RecordIdea, idea.ID); !resolved.IsReadable() || resolved.Title != "Plan the bake" {
				t.Errorf("an idea: %+v", resolved)
			}
			if resolved := resolve(t, tx, reader, models.RecordGoal, goal.ID); !resolved.IsReadable() || resolved.Title != "Learn to bake" || resolved.DashboardPath != "/settings/agent/goals?goal="+goal.ID {
				t.Errorf("a goal: %+v", resolved)
			}
			if resolved := resolve(t, tx, reader, models.RecordGoal, named.ID); !resolved.IsUnavailable {
				t.Errorf("a conversation that is not a goal is not one: %+v", resolved)
			}
		}
		for _, reader := range []recordlink.Reader{bob.person, bob.robot} {
			for _, found := range []struct {
				recordKind models.AgentRecordKind
				recordId   string
			}{
				{models.RecordAgentDocument, page.ID}, {models.RecordRecording, recording.ID},
				{models.RecordIdea, idea.ID}, {models.RecordGoal, goal.ID},
			} {
				if resolved := resolve(t, tx, reader, found.recordKind, found.recordId); !resolved.IsUnavailable || resolved.Title != "" {
					t.Errorf("another person's %s is not found: %+v", found.recordKind, resolved)
				}
			}
		}
	})
}

// A chat message is read by a member for the time they were in the chat, by
// their agent only where they let it remember the chat, and by nobody else.
func TestAChatMessageIsReadByAMemberWhileTheyWereInTheChat(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob, carol := aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob"), aPerson(t, tx, "journal-carol")
		chat, err := tx.OpenDirectChat(alice.user.ID, bob.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		message, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: bob.user.ID, AuthorName: "Ben", Body: "Lunch at noon?"})
		if err != nil {
			t.Fatal(err)
		}

		resolved := resolve(t, tx, alice.person, models.RecordChatMessage, message.ID)
		if !resolved.IsReadable() || resolved.Title != "Ben: Lunch at noon?" || !strings.HasPrefix(resolved.DashboardPath, "/chat?conversation=") || !strings.HasSuffix(resolved.DashboardPath, "&message="+message.ID) {
			t.Errorf("a member reads what was said in their chat: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordChatMessage, message.ID); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("the agent reads a chat only where it was let remember it: %+v", resolved)
		}
		for _, reader := range []recordlink.Reader{carol.person, carol.robot} {
			if resolved := resolve(t, tx, reader, models.RecordChatMessage, message.ID); !resolved.IsUnavailable || resolved.Title != "" {
				t.Errorf("somebody outside the chat reads nothing: %+v", resolved)
			}
		}
		if _, err := tx.DeleteChatMessage(message.ID); err != nil {
			t.Fatal(err)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordChatMessage, message.ID); !resolved.IsUnavailable {
			t.Errorf("a deleted message is gone: %+v", resolved)
		}
	})
}

// An occurrence is named by its event and its start; a start the event does
// not have, or a calendar the agent was not given, reads as such.
func TestACalendarOccurrenceIsNamedByItsEventAndStart(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob")
		first := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
		second := first.AddDate(0, 0, 7)
		put := func(granted bool, uid string) *models.CalendarObject {
			calendar := aCalendar(t, tx, alice.user, "Calendar "+uid, models.CalendarEvents, granted)
			object, err := tx.PutCalendarObject(&models.CalendarObject{
				CalendarID: calendar.ID, UID: uid, ETag: "e", Data: "x", Summary: "Weekly sync " + uid,
				StartsAt: first, EndsAt: first.Add(time.Hour), Recurring: true,
			}, []models.Occurrence{{StartsAt: first, EndsAt: first.Add(time.Hour)}, {StartsAt: second, EndsAt: second.Add(time.Hour)}})
			if err != nil {
				t.Fatal(err)
			}
			return object
		}
		granted, ungranted := put(true, "granted"), put(false, "ungranted")

		reference := recordlink.FormatOccurrenceReference(granted.ID, second)
		resolved := resolve(t, tx, alice.robot, models.RecordCalendarOccurrence, reference)
		if !resolved.IsReadable() || resolved.Title != "Weekly sync granted" || !resolved.HappenedAt.Equal(second) ||
			resolved.DashboardPath != "/calendar?event="+url.QueryEscape(granted.ID)+"&on=2026-10-29" {
			t.Errorf("an occurrence in a granted calendar: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordCalendarOccurrence, recordlink.FormatOccurrenceReference(ungranted.ID, first)); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("an occurrence in a calendar that was not given: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordCalendarOccurrence, recordlink.FormatOccurrenceReference(ungranted.ID, first)); !resolved.IsReadable() {
			t.Errorf("the person reads their own calendar: %+v", resolved)
		}
		for _, reference := range []string{
			recordlink.FormatOccurrenceReference(granted.ID, first.Add(time.Hour)),
			granted.ID, granted.ID + "@not-a-time",
		} {
			if resolved := resolve(t, tx, alice.person, models.RecordCalendarOccurrence, reference); !resolved.IsUnavailable {
				t.Errorf("%q is not an occurrence the event has: %+v", reference, resolved)
			}
		}
		if resolved := resolve(t, tx, bob.person, models.RecordCalendarOccurrence, reference); !resolved.IsUnavailable {
			t.Errorf("another person's occurrence is not found: %+v", resolved)
		}
		eventId, startsAt, err := recordlink.ParseOccurrenceReference(reference)
		if err != nil || eventId != granted.ID || !startsAt.Equal(second) {
			t.Errorf("an occurrence reference reads back: %q %s %v", eventId, startsAt, err)
		}
	})
}

// A Health day exists where the phone sent a summary for it, and the agent
// reads it only when the person let it read Health.
func TestAHealthDayNeedsASummaryAndTheAgentsSwitch(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob")
		sum := 8200.0
		if _, err := tx.UpsertHealthDaySummaries(alice.user.ID, []*models.HealthDaySummary{
			{HealthType: "stepCount", SummaryDay: "2026-10-22", QuantityUnit: "count", SumValue: &sum, SampleCount: 1, WorkedOutAt: time.Now()},
		}); err != nil {
			t.Fatal(err)
		}
		reference := recordlink.FormatHealthDayReference("stepCount", "2026-10-22")

		resolved := resolve(t, tx, alice.person, models.RecordHealthDay, reference)
		if !resolved.IsReadable() || resolved.Title != "Steps, 2026-10-22" || resolved.DashboardPath != "/health?type=stepCount" {
			t.Errorf("a Health day for its owner: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordHealthDay, reference); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("the agent reads no Health until the person lets it: %+v", resolved)
		}
		if err := tx.SetCanAgentReadHealth(alice.user.ID, true); err != nil {
			t.Fatal(err)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordHealthDay, reference); !resolved.IsReadable() {
			t.Errorf("the agent reads Health once the person lets it: %+v", resolved)
		}
		for _, missing := range []string{"stepCount@2026-10-23", "stepCount", "stepCount@yesterday", "bad type@2026-10-22"} {
			if resolved := resolve(t, tx, alice.person, models.RecordHealthDay, missing); !resolved.IsUnavailable {
				t.Errorf("%q has no summary: %+v", missing, resolved)
			}
		}
		if resolved := resolve(t, tx, bob.person, models.RecordHealthDay, reference); !resolved.IsUnavailable {
			t.Errorf("another person's Health day is not found: %+v", resolved)
		}
	})
}

// A note is found by its identifier in the person's mailboxes, as its
// newest version that is not deleted, and the agent reads it only from a
// mailbox it was given.
func TestANoteIsItsNewestVersionInAGrantedMailbox(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, bob := aPerson(t, tx, "journal-alice"), aPerson(t, tx, "journal-bob")
		write := func(grant *models.AgentMailbox, identifier, subject string) *models.MailboxItem {
			mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: alice.user.ID, Name: "Notes of " + identifier, Agent: grant})
			if err != nil {
				t.Fatal(err)
			}
			folder, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
			if err != nil || folder == nil {
				t.Fatalf("an inbox: %v %v", folder, err)
			}
			mail, err := tx.CreateMail(&models.Mail{Subject: subject, Kind: models.MailKindNote, NoteIdentifier: identifier, ReceivedAt: time.Now()}, nil)
			if err != nil {
				t.Fatal(err)
			}
			item, err := tx.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{})
			if err != nil {
				t.Fatal(err)
			}
			return item
		}
		granted := write(&models.AgentMailbox{Granted: true}, "note-granted", "Packing list")
		write(nil, "note-closed", "Private thoughts")

		resolved := resolve(t, tx, alice.robot, models.RecordNote, "note-granted")
		if !resolved.IsReadable() || resolved.Title != "Packing list" || resolved.DashboardPath != "/mailbox/"+granted.FolderID+"/"+granted.ID {
			t.Errorf("a note in a granted mailbox: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.robot, models.RecordNote, "note-closed"); !resolved.IsWithheld || resolved.Title != "" {
			t.Errorf("a note in a mailbox that was not given: %+v", resolved)
		}
		if resolved := resolve(t, tx, alice.person, models.RecordNote, "note-closed"); !resolved.IsReadable() || resolved.Title != "Private thoughts" {
			t.Errorf("the person reads their own notes: %+v", resolved)
		}
		for _, reader := range []recordlink.Reader{bob.person, bob.robot} {
			if resolved := resolve(t, tx, reader, models.RecordNote, "note-granted"); !resolved.IsUnavailable {
				t.Errorf("another person's note is not found: %+v", resolved)
			}
		}
		if resolved := resolve(t, tx, alice.person, models.RecordNote, "note-missing"); !resolved.IsUnavailable {
			t.Errorf("a note that is gone: %+v", resolved)
		}
	})
}

// A shopping trip cannot be linked yet: its tables are not here.
func TestAShoppingTripIsNotALinkableKindYet(t *testing.T) {
	if models.IsAgentRecordKind(models.RecordShoppingTrip) {
		t.Fatal("a shopping trip has no resolver, so it is not in the list of kinds a page can link")
	}
}
