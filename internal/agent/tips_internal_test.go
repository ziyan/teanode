package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// With two features, one used, the tip is about the other and is recorded;
// it is not given twice, not to somebody still typing, and not a second
// time the same day.
func TestATipIsAboutWhatThePersonHasNotTried(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	saved := tipCatalog
	defer func() { tipCatalog = saved }()
	tipCatalog = []*Tip{
		{TipKey: "kite_flying", Feature: "Flying kites.", Where: "the kite page", IsUsed: func(db.Transaction, *models.Agent, *models.User) (bool, error) { return true, nil }},
		{TipKey: "bread_baking", Feature: "Baking bread.", Where: "the bread page", IsUsed: func(db.Transaction, *models.Agent, *models.User) (bool, error) { return false, nil }},
	}
	now := time.Now()
	reason := worker.tipReason()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.MarkAgentSpokeFirst(run.Agent.ID, nil, &now, nil); err != nil {
			t.Fatal(err)
		}
		agent, _ := tx.GetAgent(run.Agent.ID)
		if isDue, err := reason.isDue(t.Context(), tx, agent, run.Owner, time.Minute, now); err != nil || isDue {
			t.Fatalf("not while they are busy: %v %v", isDue, err)
		}
		if isDue, err := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); err != nil || !isDue {
			t.Fatalf("a quiet person with a feature to try: %v %v", isDue, err)
		}
		message, err := reason.checkIn(t.Context(), tx, agent, run.Owner, now)
		if err != nil || !strings.Contains(message, "Baking bread.") || strings.Contains(message, "kite") || !strings.HasPrefix(message, models.SpeakFirstMarker) {
			t.Fatalf("the tip is about bread: %q %v", message, err)
		}
		tips, _ := tx.ListAgentTips(agent.ID)
		if len(tips) != 1 || tips[0].TipKey != "bread_baking" {
			t.Fatalf("recorded: %+v", tips)
		}
		if isDue, _ := reason.isDue(t.Context(), tx, agent, run.Owner, 10*time.Minute, now); isDue {
			t.Fatal("nothing left to tell")
		}
	})

	// The daily limit: with another tip to give, the agent that spoke first
	// an hour ago stays quiet.
	tipCatalog = append(tipCatalog, &Tip{TipKey: "tea_making", Feature: "Making tea.", Where: "the kettle", IsUsed: func(db.Transaction, *models.Agent, *models.User) (bool, error) { return false, nil }})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		spoke := now.Add(-time.Hour)
		if err := tx.MarkAgentSpokeFirst(run.Agent.ID, &spoke, nil, nil); err != nil {
			t.Fatal(err)
		}
		agent, _ := tx.GetAgent(run.Agent.ID)
		if reason, err := worker.dueSpeakFirstReason(t.Context(), tx, agent, 10*time.Minute, now); err != nil || reason != "" {
			t.Fatalf("one a day: %q %v", reason, err)
		}
		if reason, err := worker.dueSpeakFirstReason(t.Context(), tx, agent, 10*time.Minute, now.Add(25*time.Hour)); err != nil || reason != SpeakFirstTip {
			t.Fatalf("the next day, a tip: %q %v", reason, err)
		}
	})
}
