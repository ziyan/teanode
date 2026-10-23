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

// A summary is copied from the facts it was written from, so a page with a
// fact the reader is not shown, or one read in a chat or a recording, leaves
// its summary out even when it quotes nobody, and a search does not find the
// page by a word on it. A page whose facts are all shown keeps its summary.
func TestASummaryIsLeftOutWhenAFactOnThePageIsWithheld(t *testing.T) {
	world := newSharedWorld(t)
	chatDocument := world.document(t, models.SourceArchive, models.DocumentChat, "chat-day")
	world.setSummary(t, "people/a", "Paid the boatyard on the first. Keeps the lighthouse key.")
	world.setSummary(t, "people/a/doctor", "Watches the quay gate. Keeps the lighthouse lamp.")
	world.setSummary(t, "projects/garden", "Keeps bees and the lighthouse hive.")
	world.fact(t, "people/a", &models.AgentFact{
		Text:     "Paid the boatyard on the first.",
		Evidence: []models.Evidence{{Kind: models.EvidenceFinanceTransaction, ID: "a-transaction"}},
	})
	world.fact(t, "people/a/doctor", &models.AgentFact{
		Text:     "Said the pier is closed.",
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}, {Kind: models.EvidenceDocument, ID: chatDocument.ID}},
	})
	world.fact(t, "projects/garden", &models.AgentFact{
		Text:     "Keeps bees.",
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
	})
	world.share(t, "people/a", true)
	world.share(t, "projects/garden", true)

	if page := world.read(t, world.reader, "people/a"); page == nil || page.Opening != "" {
		t.Errorf("a summary on a page with a withheld fact: %+v", page)
	}
	if doctor := world.read(t, world.reader, "people/a/doctor"); doctor == nil || doctor.Opening != "" {
		t.Errorf("a summary on a page with a fact read in a chat: %+v", doctor)
	}
	if garden := world.read(t, world.reader, "projects/garden"); garden == nil || garden.Opening != "Keeps bees and the lighthouse hive." {
		t.Errorf("a summary on a page whose facts are all shown: %+v", garden)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		roots, err := SharedPagesFor(tx, world.reader.ID)
		if err != nil || len(roots) != 2 {
			t.Fatalf("the roots: %+v %v", roots, err)
		}
		for _, root := range roots {
			expected := ""
			if root.Path == "projects/garden" {
				expected = "Keeps bees and the lighthouse hive"
			}
			if root.Opening != expected {
				t.Errorf("the opening of %s: %q", root.Path, root.Opening)
			}
		}
		// The same word is on three summaries and only the shown one finds its page.
		hits, err := SearchSharedPages(tx, world.reader.ID, "lighthouse", 20)
		if err != nil {
			t.Fatalf("SearchSharedPages: %s", err)
		}
		if len(hits) != 1 || hits[0].Path != "projects/garden" {
			t.Errorf("a word on a left-out summary finds %+v", hits)
		}
		for _, word := range []string{"boatyard", "quay"} {
			if hits, err = SearchSharedPages(tx, world.reader.ID, word, 20); err != nil || len(hits) != 0 {
				t.Errorf("%s finds %+v %v", word, hits, err)
			}
		}
	})
}

// A summary may have been written while a fact now dormant or replaced was on
// the page, so it can carry that fact's words: the page's summary is left out
// for a withheld fact that is no longer live, though the fact is not shown.
func TestASummaryIsLeftOutForADormantOrSupersededWithheldFact(t *testing.T) {
	world := newSharedWorld(t)
	world.setSummary(t, "people/a", "Paid the boatyard on the first. Keeps the lighthouse key.")
	world.setSummary(t, "people/a/doctor", "Watches the quay gate. Keeps the lighthouse lamp.")
	world.setSummary(t, "projects/garden", "Keeps bees and the lighthouse hive.")
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Paid the boatyard on the first.", Dormant: true,
		Evidence: []models.Evidence{{Kind: models.EvidenceFinanceTransaction, ID: "a-transaction"}},
	})
	var replacedId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		replacement, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: world.ownerAgent.ID, NodeID: world.pages["people/a/doctor"].ID, Kind: models.FactPlain, Confidence: 1,
			Text: "Watches the harbor gate.", Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
		})
		if err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		replacedId = replacement.ID
	})
	world.fact(t, "people/a/doctor", &models.AgentFact{
		Text: "Watches the quay gate.", SupersededBy: replacedId,
		Evidence: []models.Evidence{{Kind: models.EvidenceRecordingPassage, ID: "a-recording"}},
	})
	world.fact(t, "projects/garden", &models.AgentFact{
		Text:     "Keeps bees.",
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
	})
	world.share(t, "people/a", true)
	world.share(t, "projects/garden", true)

	if page := world.read(t, world.reader, "people/a"); page == nil || page.Opening != "" || len(page.Facts) != 0 {
		t.Errorf("a summary on a page whose withheld fact is dormant: %+v", page)
	}
	if doctor := world.read(t, world.reader, "people/a/doctor"); doctor == nil || doctor.Opening != "" || len(doctor.Facts) != 1 {
		t.Errorf("a summary on a page whose recording fact was replaced: %+v", doctor)
	}
	if garden := world.read(t, world.reader, "projects/garden"); garden == nil || garden.Opening != "Keeps bees and the lighthouse hive." {
		t.Errorf("a summary on a page whose facts are all shown: %+v", garden)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		hits, err := SearchSharedPages(tx, world.reader.ID, "lighthouse", 20)
		if err != nil {
			t.Fatalf("SearchSharedPages: %s", err)
		}
		if len(hits) != 1 || hits[0].Path != "projects/garden" {
			t.Errorf("a word on a left-out summary finds %+v", hits)
		}
		// Pages are decided as the search finds them, so a short list is
		// not spent on pages whose summary is left out.
		if hits, err = SearchSharedPages(tx, world.reader.ID, "lighthouse", 1); err != nil || len(hits) != 1 || hits[0].Path != "projects/garden" {
			t.Errorf("a list of one finds %+v %v", hits, err)
		}
	})
}

// A page shows its reader only its live facts: one gone dormant, or replaced
// by a correction, is not shown, though nothing it cites is withheld.
func TestADormantOrSupersededFactIsNotShown(t *testing.T) {
	world := newSharedWorld(t)
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Used to row on Sundays.", Dormant: true,
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
	})
	var correctionId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		correction, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: world.ownerAgent.ID, NodeID: world.pages["people/a"].ID, Kind: models.FactPlain, Confidence: 1,
			Text: "Rows on Saturdays.", Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
		})
		if err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		correctionId = correction.ID
	})
	world.fact(t, "people/a", &models.AgentFact{
		Text: "Rows on Fridays.", SupersededBy: correctionId,
		Evidence: []models.Evidence{{Kind: models.EvidenceMail, ID: "a-message"}},
	})
	world.share(t, "people/a", true)

	page := world.read(t, world.reader, "people/a")
	if page == nil {
		t.Fatal("the shared page is not read")
	}
	shownTexts := []string{}
	for _, fact := range page.Facts {
		shownTexts = append(shownTexts, fact.Text)
	}
	if len(shownTexts) != 1 || shownTexts[0] != "Rows on Saturdays." {
		t.Errorf("the facts shown are %q, want only the live one", shownTexts)
	}
}

// A fact that cites an event or a document that cannot be found is not shown
// to a reader, since it may have been a private event or a recording: an
// event deleted since the fact was filed, a document gone from the archive,
// and an event in a circle's calendar, which is not the owner's own.
func TestAFactCitingSomethingThatCannotBeFoundIsNotShared(t *testing.T) {
	world := newSharedWorld(t)
	var circleEvent *models.CalendarObject
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		circleCalendar, err := tx.CreateCalendar(&models.Calendar{CircleID: world.circle.ID, Name: "Home", CalendarKind: models.CalendarEvents})
		if err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		startsAt := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
		if circleEvent, err = tx.PutCalendarObject(&models.CalendarObject{
			CalendarID: circleCalendar.ID, UID: "circle-event@example.test", ETag: "e", Summary: "A visit", StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour),
			Data: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//example//test//EN\r\nBEGIN:VEVENT\r\nUID:circle-event@example.test\r\n" +
				"DTSTAMP:20260901T090000Z\r\nDTSTART:20260901T090000Z\r\nDTEND:20260901T100000Z\r\nSUMMARY:A visit\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		}, nil); err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
	})
	publicEvent := world.calendarEvent(t, false)
	privateEvent := world.calendarEvent(t, true)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.DeleteCalendarObject(privateEvent.CalendarID, privateEvent.ID); err != nil {
			t.Fatalf("DeleteCalendarObject: %s", err)
		}
	})
	otherDocument := world.document(t, models.SourceArchive, models.DocumentJournal, "plain-document")
	for _, fact := range []*models.AgentFact{
		{Text: "Had a private visit.", Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: privateEvent.ID}}},
		{Text: "Has a visit in the circle calendar.", Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: circleEvent.ID}}},
		{Text: "Read a document that is gone.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: "a-document-that-is-gone"}}},
		{Text: "Has a dentist on the first.", Evidence: []models.Evidence{{Kind: models.EvidenceCalendarEvent, ID: publicEvent.ID}}},
		{Text: "Read a plain document.", Evidence: []models.Evidence{{Kind: models.EvidenceDocument, ID: otherDocument.ID}}},
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
	expected := []string{"Has a dentist on the first.", "Read a plain document."}
	if strings.Join(texts, "|") != strings.Join(expected, "|") {
		t.Errorf("the facts shown: %q, expected %q", texts, expected)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		count, err := CountWithheldFacts(tx, world.owner.ID, "people/a")
		if err != nil || count != 3 {
			t.Errorf("the facts withheld: %d %v", count, err)
		}
	})
}
