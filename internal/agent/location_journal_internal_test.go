package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func journalTime(day, hour, minute int) *time.Time {
	return journalTimeIn(time.UTC, day, hour, minute)
}

func journalTimeIn(zone *time.Location, day, hour, minute int) *time.Time {
	moment := time.Date(2026, 3, day, hour, minute, 0, 0, zone)
	return &moment
}

// A day lists its stays in order with the place's name, what kind of place
// OpenStreetMap says it is in plain words, and the person's own label. With a
// home place, the first line says how long they were away from it.
func TestLocationJournalDayTextListsStaysInOrder(t *testing.T) {
	home := &models.Place{ID: "place-home", PlaceName: "Elm Street 4", PlaceLabel: models.PlaceLabelHome}
	park := &models.Place{ID: "place-park", PlaceName: "Riverside Park", OSMCategory: "leisure=park"}
	office := &models.Place{ID: "place-office", PlaceName: "Harbor Office", PlaceLabel: models.PlaceLabelWork, OSMCategory: "office=company"}
	visits := []*models.PlaceVisit{
		// Listed newest first, as the database answers.
		{PlaceID: home.ID, Place: home, ArrivedAt: journalTime(4, 18, 0), DepartedAt: nil},
		{PlaceID: park.ID, Place: park, ArrivedAt: journalTime(4, 12, 0), DepartedAt: journalTime(4, 12, 45)},
		{PlaceID: office.ID, Place: office, ArrivedAt: journalTime(4, 9, 40), DepartedAt: journalTime(4, 11, 30)},
		{PlaceID: home.ID, Place: home, ArrivedAt: journalTime(3, 19, 0), DepartedAt: journalTime(4, 8, 0)},
		// Another day entirely.
		{PlaceID: park.ID, Place: park, ArrivedAt: journalTime(6, 9, 0), DepartedAt: journalTime(6, 10, 0)},
	}
	dayStart := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)

	got := locationDayText(visits, dayStart, true)
	want := strings.Join([]string{
		"Places: 3, time away from home: 2 h 35 min",
		"00:00 to 08:00 (8 h): Elm Street 4, labeled home, there since the day before",
		"09:40 to 11:30 (1 h 50 min): Harbor Office, labeled work",
		"12:00 to 12:45 (45 min): Riverside Park (park)",
		"18:00 to 24:00 (6 h): Elm Street 4, labeled home, still there at midnight",
	}, "\n")
	if got != want {
		t.Fatalf("the day reads\n%s\nwant\n%s", got, want)
	}

	// Without a home place there is nothing to be away from.
	if without := locationDayText(visits, dayStart, false); strings.Contains(without, "away from home") || !strings.HasPrefix(without, "Places: 3\n") {
		t.Errorf("no home place, no time away: %s", without)
	}
	// A day with no stays has no document.
	if empty := locationDayText(visits, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), true); empty != "" {
		t.Errorf("a day with no stays is empty: %q", empty)
	}
}

// A departure that iOS reported without an arrival is a moment, and a place
// with no name is still a line.
func TestLocationJournalDayTextKeepsADepartureWithoutAnArrival(t *testing.T) {
	visits := []*models.PlaceVisit{
		{PlaceID: "place-a", Place: &models.Place{ID: "place-a"}, DepartedAt: journalTime(4, 7, 5)},
	}
	got := locationDayText(visits, time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), false)
	if got != "Places: 1\n07:05 (left): an unnamed place" {
		t.Errorf("the day reads %q", got)
	}
}

func TestLocationJournalDurationText(t *testing.T) {
	for duration, want := range map[time.Duration]string{
		0: "0 min", 45 * time.Minute: "45 min", 2 * time.Hour: "2 h", 110 * time.Minute: "1 h 50 min", 90*time.Minute + 59*time.Second: "1 h 30 min",
	} {
		if got := locationDurationText(duration); got != want {
			t.Errorf("%s reads %q, want %q", duration, got, want)
		}
	}
}

// Every finished day with stays becomes one document, today does not, a run
// again writes nothing, and a stay whose departure is reported late rewrites
// its day.
func TestLocationJournalWritesFinishedDaysAndRewritesAChangedOne(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	provider, _ := tinyProvider()
	defer provider.Close()
	worker, run := digestSplitWorldWith(t, database, provider.URL, nil)

	zone := Location(run.Owner)
	now := time.Date(2026, 3, 6, 15, 0, 0, 0, zone)
	var parkID string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.InsertLocationPoints(run.Owner.ID, []*models.LocationPoint{{LatitudeDegrees: 10.5, LongitudeDegrees: 20.5, AccuracyMeters: 5, MeasuredAt: now, LocationSource: models.LocationSourcePhone}}); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}
		home, err := tx.CreatePlace(run.Owner.ID, &models.Place{PlaceName: "Elm Street 4", PlaceLabel: models.PlaceLabelHome, LatitudeDegrees: 10.5, LongitudeDegrees: 20.5})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		park, err := tx.CreatePlace(run.Owner.ID, &models.Place{PlaceName: "Riverside Park", OSMCategory: "leisure=park", LatitudeDegrees: 10.6, LongitudeDegrees: 20.6})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		parkID = park.ID
		for key, visit := range map[string]*models.PlaceVisit{
			"home-yesterday": {PlaceID: home.ID, ArrivedAt: journalTimeIn(zone, 5, 7, 0), DepartedAt: journalTimeIn(zone, 5, 9, 0)},
			"park-yesterday": {PlaceID: park.ID, ArrivedAt: journalTimeIn(zone, 5, 9, 30), DepartedAt: journalTimeIn(zone, 5, 10, 30)},
			"park-today":     {PlaceID: park.ID, ArrivedAt: journalTimeIn(zone, 6, 9, 0), DepartedAt: journalTimeIn(zone, 6, 10, 0)},
			"park-earlier":   {PlaceID: park.ID, ArrivedAt: journalTimeIn(zone, 3, 9, 0), DepartedAt: journalTimeIn(zone, 3, 10, 0)},
		} {
			visit.ClientVisitKey, visit.VisitSource = key, models.VisitSourceVisit
			visit.LatitudeDegrees, visit.LongitudeDegrees = 10.5, 20.5
			if _, err := tx.PutPlaceVisit(run.Owner.ID, visit); err != nil {
				t.Fatalf("PutPlaceVisit(%s): %s", key, err)
			}
		}
	})

	writtenCount, err := worker.writeLocationJournal(t.Context(), run.Agent, run.Owner, now)
	if err != nil || writtenCount != 2 {
		t.Fatalf("the 3rd and the 5th are written: %d, %v", writtenCount, err)
	}
	documents := locationJournalDocuments(t, database, run)
	if len(documents) != 2 || documents["location:2026-03-05"] == nil || documents["location:2026-03-03"] == nil {
		t.Fatalf("one document for each finished day with stays, not today: %v", documents)
	}
	yesterday := documents["location:2026-03-05"]
	if yesterday.Title != "Where they went on Thursday 5 March 2026" || yesterday.URL != "/location?day=2026-03-05" || !yesterday.Private || yesterday.Kind != models.DocumentJournal {
		t.Errorf("yesterday is %+v", yesterday)
	}
	text := locationJournalText(t, database, run, yesterday)
	if !strings.HasPrefix(text, "Places: 2, time away from home: 1 h\n") || !strings.Contains(text, "09:30 to 10:30 (1 h): Riverside Park (park)") {
		t.Errorf("yesterday reads %s", text)
	}

	if writtenCount, err = worker.writeLocationJournal(t.Context(), run.Agent, run.Owner, now); err != nil || writtenCount != 0 {
		t.Fatalf("a second run writes nothing: %d, %v", writtenCount, err)
	}

	// The park visit's departure arrives again, later than first reported.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.PutPlaceVisit(run.Owner.ID, &models.PlaceVisit{
			PlaceID: parkID, ClientVisitKey: "park-yesterday", VisitSource: models.VisitSourceVisit,
			DepartedAt: journalTimeIn(zone, 5, 11, 30), LatitudeDegrees: 10.5, LongitudeDegrees: 20.5,
		}); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
	})
	if writtenCount, err = worker.writeLocationJournal(t.Context(), run.Agent, run.Owner, now); err != nil || writtenCount != 1 {
		t.Fatalf("only the changed day is rewritten: %d, %v", writtenCount, err)
	}
	if text := locationJournalText(t, database, run, locationJournalDocuments(t, database, run)["location:2026-03-05"]); !strings.Contains(text, "09:30 to 11:30 (2 h): Riverside Park (park)") {
		t.Errorf("yesterday now reads %s", text)
	}

	// The next day's run adds the 6th and leaves the rest.
	if writtenCount, err = worker.writeLocationJournal(t.Context(), run.Agent, run.Owner, now.AddDate(0, 0, 1)); err != nil || writtenCount != 1 {
		t.Fatalf("the 6th is written: %d, %v", writtenCount, err)
	}
}

// A person who never reported a position gets no source and no document.
func TestLocationJournalIsNotMadeForAPersonWithNoPositions(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	provider, _ := tinyProvider()
	defer provider.Close()
	worker, run := digestSplitWorldWith(t, database, provider.URL, nil)

	worker.queueLocationJournals(t.Context(), time.Date(2026, 3, 6, 15, 0, 0, 0, time.UTC))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		sources, err := tx.ListAgentSources(run.Agent.ID)
		if err != nil {
			t.Fatalf("ListAgentSources: %s", err)
		}
		for _, source := range sources {
			if source.Kind == models.SourceLocation {
				t.Errorf("a person with no positions has a location source: %+v", source)
			}
		}
	})
}

func locationJournalDocuments(t *testing.T, database db.Database, run *Run) map[string]*models.AgentDocument {
	t.Helper()
	found := map[string]*models.AgentDocument{}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		sources, err := tx.ListAgentSources(run.Agent.ID)
		if err != nil {
			t.Fatalf("ListAgentSources: %s", err)
		}
		for _, source := range sources {
			if source.Kind != models.SourceLocation {
				continue
			}
			for day := 1; day <= 8; day++ {
				externalID := time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC).Format("location:2006-01-02")
				document, err := tx.GetAgentDocumentByExternal(source.ID, externalID)
				if err != nil {
					t.Fatalf("GetAgentDocumentByExternal: %s", err)
				}
				if document != nil {
					found[externalID] = document
				}
			}
		}
	})
	return found
}

func locationJournalText(t *testing.T, database db.Database, run *Run, document *models.AgentDocument) string {
	t.Helper()
	var text string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		chunks, err := tx.ListAgentChunks(run.Agent.ID, document.ID)
		if err != nil || len(chunks) != 1 {
			t.Fatalf("ListAgentChunks: %v, %d chunks", err, len(chunks))
		}
		text = chunks[0].Text
	})
	return text
}
