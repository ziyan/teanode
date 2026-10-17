package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func locationKnowledgeAgent(t *testing.T, tx db.Transaction, username string) *models.Agent {
	t.Helper()
	user, err := tx.CreateUser(&models.User{Username: username})
	if err != nil {
		t.Fatalf("CreateUser(%s): %s", username, err)
	}
	agent, err := tx.EnsureAgent(user.ID)
	if err != nil {
		t.Fatalf("EnsureAgent: %s", err)
	}
	return agent
}

// A person's agent has one location source, switched on, made the first time
// it is asked for and found again after that. It has no schedule, because
// nothing reads it from anywhere.
func TestLocationSourceIsMadeOncePerAgent(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := locationKnowledgeAgent(t, tx, "location-source")
		other := locationKnowledgeAgent(t, tx, "location-source-other")

		source, err := tx.EnsureLocationSource(agent.ID)
		if err != nil || source == nil || source.Kind != models.SourceLocation || !source.Enabled || source.Cron != "" || source.NextRunAt != nil {
			t.Fatalf("the first call makes a source that is on and has no schedule: %+v, %v", source, err)
		}
		if got := source.Describe(); got != "where they went, one day at a time" {
			t.Errorf("the source is described as %q", got)
		}
		again, err := tx.EnsureLocationSource(agent.ID)
		if err != nil || again.ID != source.ID {
			t.Fatalf("the second call finds the same source: %+v, %v", again, err)
		}
		otherSource, err := tx.EnsureLocationSource(other.ID)
		if err != nil || otherSource.ID == source.ID || otherSource.AgentID != other.ID {
			t.Fatalf("another agent has a source of its own: %+v, %v", otherSource, err)
		}
		sources, err := tx.ListAgentSources(agent.ID)
		if err != nil || len(sources) != 1 {
			t.Fatalf("the agent has one source: %d, %v", len(sources), err)
		}
		if _, err := tx.EnsureLocationSource(""); err == nil {
			t.Error("a source needs an agent")
		}
	})
}

// A finished day is one private journal document with its words in one
// chunk. The same words again change nothing, so the day is not read twice;
// different words replace the chunk and the hash.
func TestLocationDayDocumentIsWrittenOnceAndRewrittenWhenItChanges(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := locationKnowledgeAgent(t, tx, "location-day")
		source, err := tx.EnsureLocationSource(agent.ID)
		if err != nil {
			t.Fatalf("EnsureLocationSource: %s", err)
		}
		zone := time.FixedZone("test", 9*3600)
		day := time.Date(2026, 3, 4, 0, 0, 0, 0, zone)

		if latest, err := tx.GetLatestLocationDayStart(source.ID); latest != nil || err != nil {
			t.Fatalf("no day is filed yet: %v, %v", latest, err)
		}
		isChanged, err := tx.PutLocationDayDocument(agent.ID, source.ID, day, "Places: 1\n09:00 to 10:00 (1 h): Riverside Park (park)")
		if err != nil || !isChanged {
			t.Fatalf("the first write changes: %v, %v", isChanged, err)
		}
		document, err := tx.GetAgentDocumentByExternal(source.ID, "location:2026-03-04")
		if err != nil || document == nil {
			t.Fatalf("the day is filed under its date: %v, %v", document, err)
		}
		if document.Kind != models.DocumentJournal || !document.Private || document.Title != "Where they went on Wednesday 4 March 2026" ||
			document.URL != "/location?day=2026-03-04" || document.HappenedAt == nil || !document.HappenedAt.Equal(day) || document.Hash == "" {
			t.Errorf("the document is %+v", document)
		}
		chunks, err := tx.ListAgentChunks(agent.ID, document.ID)
		if err != nil || len(chunks) != 1 || !strings.Contains(chunks[0].Text, "Riverside Park") {
			t.Fatalf("the words are one chunk: %+v, %v", chunks, err)
		}

		isChanged, err = tx.PutLocationDayDocument(agent.ID, source.ID, day, "Places: 1\n09:00 to 10:00 (1 h): Riverside Park (park)")
		if err != nil || isChanged {
			t.Fatalf("the same words change nothing: %v, %v", isChanged, err)
		}

		isChanged, err = tx.PutLocationDayDocument(agent.ID, source.ID, day, "Places: 1\n09:00 to 11:30 (2 h 30 min): Riverside Park (park)")
		if err != nil || !isChanged {
			t.Fatalf("different words rewrite the day: %v, %v", isChanged, err)
		}
		rewritten, err := tx.GetAgentDocumentByExternal(source.ID, "location:2026-03-04")
		if err != nil || rewritten.ID != document.ID || rewritten.Hash == document.Hash {
			t.Fatalf("the same document with a new hash: %+v, %v", rewritten, err)
		}
		chunks, err = tx.ListAgentChunks(agent.ID, document.ID)
		if err != nil || len(chunks) != 1 || !strings.Contains(chunks[0].Text, "2 h 30 min") {
			t.Fatalf("the chunk was replaced, not added to: %+v, %v", chunks, err)
		}

		nextDay := day.AddDate(0, 0, 1)
		if _, err := tx.PutLocationDayDocument(agent.ID, source.ID, nextDay, "Places: 1\n08:00 to 09:00 (1 h): Home"); err != nil {
			t.Fatalf("PutLocationDayDocument: %s", err)
		}
		latest, err := tx.GetLatestLocationDayStart(source.ID)
		if err != nil || latest == nil || !latest.Equal(nextDay) {
			t.Fatalf("the newest day is the 5th: %v, %v", latest, err)
		}

		for name, call := range map[string]func() (bool, error){
			"no words":  func() (bool, error) { return tx.PutLocationDayDocument(agent.ID, source.ID, day, "") },
			"no source": func() (bool, error) { return tx.PutLocationDayDocument(agent.ID, "", day, "words") },
			"no day":    func() (bool, error) { return tx.PutLocationDayDocument(agent.ID, source.ID, time.Time{}, "words") },
		} {
			if _, err := call(); err == nil {
				t.Errorf("%s is refused", name)
			}
		}
	})
}
