package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// documentEvidence is a place a fact was read in, numbered.
func documentEvidence(number int) models.Evidence {
	return models.Evidence{Kind: models.EvidenceDocument, ID: fmt.Sprintf("document-%02d", number), Quote: fmt.Sprintf("line %d", number)}
}

// A full fact told the same thing again keeps where it came from and
// what it was just told, and the same place merged again changes nothing.
func TestMergedEvidenceKeepsTheFirstAndTheNewest(t *testing.T) {
	var full []models.Evidence
	for number := 1; number <= models.EvidenceCount; number++ {
		full = append(full, documentEvidence(number))
	}
	added := documentEvidence(99)
	merged := mergedEvidence(full, []models.Evidence{added})
	if len(merged) != models.EvidenceCount {
		t.Fatalf("%d entries, not %d", len(merged), models.EvidenceCount)
	}
	if merged[0] != full[0] || merged[len(merged)-1] != added {
		t.Errorf("the first or the newest went: first %v, last %v", merged[0], merged[len(merged)-1])
	}
	if again := mergedEvidence(merged, []models.Evidence{added, added}); len(again) != len(merged) || again[len(again)-1] != added || again[0] != merged[0] {
		t.Errorf("merging the same place again changed the evidence: %v", again)
	}
	if twice := mergedEvidence([]models.Evidence{documentEvidence(1)}, []models.Evidence{documentEvidence(1), documentEvidence(2)}); len(twice) != 2 {
		t.Errorf("a place listed twice: %v", twice)
	}
}

// A date taken from another saying keeps the precision it was given in,
// and a fact that already has a date keeps its own.
func TestTakingADateKeepsItsPrecision(t *testing.T) {
	first := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	for _, each := range []struct {
		happenedPrecision string
		happenedText      string
	}{
		{models.HappenedDay, "1 May 2026"},
		{models.HappenedMonth, "May 2026"},
		{models.HappenedYear, "2026"},
	} {
		fact := &models.AgentFact{Text: "The pier was repainted."}
		takeTheDateOf(fact, &models.AgentFact{HappenedAt: &first, HappenedPrecision: each.happenedPrecision})
		if got := fact.HappenedText(); got != each.happenedText || fact.HappenedPrecision != each.happenedPrecision {
			t.Errorf("a date given to the %s reads %q (%q)", each.happenedPrecision, got, fact.HappenedPrecision)
		}
	}
	later := first.AddDate(0, 1, 0)
	fact := &models.AgentFact{HappenedAt: &first, HappenedPrecision: models.HappenedYear}
	takeTheDateOf(fact, &models.AgentFact{HappenedAt: &later, HappenedPrecision: models.HappenedDay})
	if !fact.HappenedAt.Equal(first) || fact.HappenedPrecision != models.HappenedYear {
		t.Errorf("a dated fact took another date: %v %q", fact.HappenedAt, fact.HappenedPrecision)
	}
}

// Through the database: an inferred fact with full evidence, said again
// as stated from a new place, stands as stated on that place, and learns
// the month it happened as a month.
func TestAFoldKeepsTheEvidenceItRisesOnAndTheDatesPrecision(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	_, run := digestSplitWorldWith(t, database, "http://127.0.0.1:1", nil)
	var full []models.Evidence
	for number := 1; number <= models.EvidenceCount; number++ {
		full = append(full, documentEvidence(number))
	}
	// In the zone the database reads times back in, so the month is May
	// wherever the test runs.
	may := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.Local)
	added := documentEvidence(99)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		page, err := tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent.ID, Path: "projects/pier", Kind: models.NodeProject, Name: "Pier"})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		standing, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "The pier was repainted.",
			Confidence: 0.5, Inferred: true, Evidence: full,
		})
		if err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		said := &models.AgentFact{
			AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactPlain, Text: "The pier was repainted.",
			Confidence: 1, Evidence: []models.Evidence{added}, HappenedAt: &may, HappenedPrecision: models.HappenedMonth,
		}
		for range 2 {
			if _, err := takeTheEvidenceOf(tx, standing, said); err != nil {
				t.Fatalf("takeTheEvidenceOf: %s", err)
			}
		}
		stored, err := tx.GetAgentFacts(run.Agent.ID, []string{standing.ID})
		if err != nil || len(stored) != 1 {
			t.Fatalf("GetAgentFacts: %v %s", stored, err)
		}
		fact := stored[0]
		if fact.Inferred || fact.Confidence != 1 {
			t.Errorf("the fact stands at %v, inferred %v", fact.Confidence, fact.Inferred)
		}
		if len(fact.Evidence) != models.EvidenceCount || fact.Evidence[0] != full[0] || fact.Evidence[len(fact.Evidence)-1] != added {
			t.Errorf("the evidence it rose on is not kept: %v", fact.Evidence)
		}
		if fact.HappenedText() != "May 2026" || fact.HappenedPrecision != models.HappenedMonth {
			t.Errorf("the date reads %q (%q)", fact.HappenedText(), fact.HappenedPrecision)
		}
	})
}
