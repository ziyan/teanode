package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// document files a document of the owner's under a source of this kind.
func (self *sharedWorld) document(t *testing.T, sourceKind models.AgentKnowledgeKind, documentKind models.AgentDocumentKind, externalId string) *models.AgentDocument {
	t.Helper()
	var document *models.AgentDocument
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: self.ownerAgent.ID, Kind: sourceKind, Name: "source of " + externalId, Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/records", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		if document, err = tx.PutAgentDocument(&models.AgentDocument{
			AgentID: self.ownerAgent.ID, SourceID: source.ID, ExternalID: externalId, Kind: documentKind,
			Title: "A document", Hash: externalId, Private: true,
		}); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
	})
	return document
}

// calendarEvent files an event of the owner's, marked private or not.
func (self *sharedWorld) calendarEvent(t *testing.T, isPrivate bool) *models.CalendarObject {
	t.Helper()
	class := "PUBLIC"
	if isPrivate {
		class = "PRIVATE"
	}
	startsAt := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	var object *models.CalendarObject
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		owned, err := tx.CreateCalendar(&models.Calendar{UserID: self.owner.ID, Name: "Home " + class, CalendarKind: models.CalendarEvents})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		data := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//example//test//EN\r\nBEGIN:VEVENT\r\nUID:event-" + class + "@example.test\r\n" +
			"DTSTAMP:20260901T090000Z\r\nDTSTART:20260901T090000Z\r\nDTEND:20260901T100000Z\r\nSUMMARY:A visit\r\nCLASS:" + class + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
		if object, err = tx.PutCalendarObject(&models.CalendarObject{
			CalendarID: owned.ID, UID: "event-" + class + "@example.test", ETag: "e", Data: data, Summary: "A visit",
			StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour),
		}, nil); err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
	})
	return object
}

// The words of a recording are other speakers' words: they are never quoted
// to a reader, whatever the owner chose about quotes, and the fact is named as
// read in recordings.
func TestAFactReadInARecordingIsNeverQuoted(t *testing.T) {
	world := newSharedWorld(t)
	recordingDocument := world.document(t, models.SourceRecording, models.DocumentJournal, "recording-passage")
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Brings the green folder on Tuesday.",
		Evidence: []models.Evidence{
			{Kind: models.EvidenceRecordingPassage, ID: "a-recording", Quote: "bring the green folder, said Cora"},
			{Kind: models.EvidenceMail, ID: "a-message", Quote: "the folder is green"},
		},
	})
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Keeps bees.",
		Evidence: []models.Evidence{
			{Kind: models.EvidenceDocument, ID: recordingDocument.ID, Quote: "I keep bees, said Cora"},
			{Kind: models.EvidenceMail, ID: "a-message", Quote: "the bees are fine"},
		},
	})
	world.fact(t, "people/a", &models.AgentFact{
		Text:     "Has a boat.",
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message", Quote: "the boat is in the harbor"}},
	})
	world.share(t, "people/a", true)

	page := world.read(t, world.reader, "people/a")
	if page == nil || len(page.Facts) != 3 {
		t.Fatalf("three facts: %+v", page)
	}
	for _, fact := range page.Facts[:2] {
		if fact.EvidenceQuote != "" {
			t.Errorf("a quote from a recording: %q", fact.EvidenceQuote)
		}
		if fact.EvidenceSource != "from Ada Example's recordings and mail" && fact.EvidenceSource != "from Ada Example's mail and recordings" {
			t.Errorf("the source of a recording is named as the owner's recordings: %q", fact.EvidenceSource)
		}
	}
	if page.Facts[2].EvidenceQuote != "the boat is in the harbor" {
		t.Errorf("a quote from mail is kept when quotes are shared: %q", page.Facts[2].EvidenceQuote)
	}
	serialized, _ := json.Marshal(page)
	for _, leak := range []string{"said Cora", recordingDocument.ID, "a-recording"} {
		if strings.Contains(string(serialized), leak) {
			t.Errorf("the view leaks %q: %s", leak, serialized)
		}
	}
}

// A summary that quotes somebody is copied from the facts it was written
// from, so a page with a fact read in a recording leaves it out, as it does
// for a chat, even when the owner shares quotes.
func TestASummaryQuotingARecordingIsLeftOutEvenWhenQuotesAreShared(t *testing.T) {
	world := newSharedWorld(t)
	quoting := "Lives by the sea. Somebody said \"we meet at the old pier\" last week."
	world.setSummary(t, "people/a", quoting)
	world.setSummary(t, "people/a/doctor", quoting)
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Keeps bees.",
		Evidence: []models.Evidence{
			{Kind: models.EvidenceRecordingPassage, ID: "a-recording", Quote: "I keep bees, said Cora"},
			{Kind: models.EvidenceMail, ID: "a-message", Quote: "the bees are fine"},
		},
	})
	world.share(t, "people/a", true)

	if page := world.read(t, world.reader, "people/a"); page == nil || page.Opening != "" {
		t.Errorf("a summary quoting people, on a page read in a recording: %+v", page)
	}
	if doctor := world.read(t, world.reader, "people/a/doctor"); doctor == nil || doctor.Opening != quoting {
		t.Errorf("a quoting summary on a page with no recording: %+v", doctor)
	}
}

// A fact whose evidence is all finance, recordings, the location journal or
// an event marked private is not shown: those have a sharing of their own. A
// fact with any other evidence, or one the person stated, stays, and the
// quote shown is never the one of such evidence.
func TestAFactReadOnlyInPrivateKindsIsNotShared(t *testing.T) {
	world := newSharedWorld(t)
	recordingDocument := world.document(t, models.SourceRecording, models.DocumentJournal, "recording-passage")
	locationDocument := world.document(t, models.SourceLocation, models.DocumentJournal, "location-day")
	privateEvent := world.calendarEvent(t, true)
	publicEvent := world.calendarEvent(t, false)
	for _, fact := range []*models.AgentFact{
		{Text: "Paid the boatyard on the first.", Evidence: []models.Evidence{{Kind: models.EvidenceFinanceTransaction, ID: "a-transaction", Quote: "BOATYARD 120.00"}}},
		{Text: "Has a receipt for paint.", Evidence: []models.Evidence{{Kind: models.EvidenceFinanceReceipt, ID: "a-receipt"}, {Kind: models.EvidenceFinanceTransaction, ID: "a-transaction"}}},
		{Text: "Said the harbor opens early.", Evidence: []models.Evidence{{Kind: models.EvidenceRecordingPassage, ID: "a-recording", Quote: "early, said Cora"}}},
		{Text: "Said the pier is closed.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: recordingDocument.ID}}},
		{Text: "Went to the quay on Monday.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: locationDocument.ID, Quote: "the quay at nine"}}},
		{Text: "Has a visit on the first.", Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: privateEvent.ID}}},

		{Text: "Has a dentist on the first.", Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: publicEvent.ID}}},
		{Text: "Paid for the boat repair.", Evidence: []models.Evidence{
			{Kind: models.EvidenceFinanceTransaction, ID: "a-transaction", Quote: "BOATYARD 120.00"},
			{Kind: models.EvidenceMail, ID: "a-message", Quote: "the boat repair is paid"},
		}},
		{Text: "Prefers the morning.", Evidence: []models.Evidence{{Kind: models.EvidencePerson, Quote: "I prefer the morning"}}},
		{Text: "Has no evidence."},
	} {
		world.fact(t, "people/a", fact)
	}
	world.share(t, "people/a", true)

	page := world.read(t, world.reader, "people/a")
	if page == nil {
		t.Fatal("the page is not shared")
	}
	var texts []string
	for _, fact := range page.Facts {
		texts = append(texts, fact.Text)
	}
	expected := []string{"Has a dentist on the first.", "Paid for the boat repair.", "Prefers the morning.", "Has no evidence."}
	if strings.Join(texts, "|") != strings.Join(expected, "|") {
		t.Errorf("the facts shown: %q, expected %q", texts, expected)
	}
	if repair := page.Facts[1]; repair.EvidenceQuote != "the boat repair is paid" {
		t.Errorf("the quote of a fact with other evidence is the other evidence's: %q", repair.EvidenceQuote)
	}
	serialized, _ := json.Marshal(page)
	for _, leak := range []string{"boatyard", "BOATYARD", "quay", "harbor opens", "pier is closed"} {
		if strings.Contains(strings.ToLower(string(serialized)), strings.ToLower(leak)) {
			t.Errorf("the view leaks %q: %s", leak, serialized)
		}
	}

	// A search finds the words of a fact that is shown and not of one that is not.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		hits, err := SearchSharedPages(tx, world.reader.ID, "boatyard quay harbor dentist", 20)
		if err != nil {
			t.Fatalf("SearchSharedPages: %s", err)
		}
		if len(hits) != 1 || hits[0].Text != "Has a dentist on the first." {
			t.Errorf("a search of facts: %+v", hits)
		}
		// The owner is told how many the share keeps back.
		count, err := CountWithheldFacts(tx, world.owner.ID, "people/a")
		if err != nil || count != 6 {
			t.Errorf("the facts withheld: %d %v", count, err)
		}
		if count, err = CountWithheldFacts(tx, world.owner.ID, "people/a/doctor"); err != nil || count != 0 {
			t.Errorf("the facts withheld on a page with none: %d %v", count, err)
		}
	})
}
