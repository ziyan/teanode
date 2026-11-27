package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// An embedding of a question is a call that costs, so it is made only while
// the person's daily limit has room; past it every caller compares by words
// alone, as where there is no embedding model.
func TestEmbeddingWaitsForTheDailyLimit(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{saidByModel("ok")})
	defer provider.Close()
	worker, run := digestSplitWorldWith(t, database, provider.URL, func(configuration *config.Configuration) {
		configuration.Agent.Limits.DailyTokensPerAgent = 1000
	})

	if !worker.isBudgetLeftToEmbed(t.Context(), run.Agent.ID) {
		t.Fatal("a day with room is let through")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.PutAgentUsage(&db.AgentUsage{
			AgentID: run.Agent.ID, Model: "p:thinker", Kind: "ask",
			At: DayStart(run.Owner, time.Now()).Add(time.Minute), Values: []uint64{1000, 0, 0, 0, 1},
		}); err != nil {
			t.Fatalf("PutAgentUsage: %s", err)
		}
	})
	if worker.isBudgetLeftToEmbed(t.Context(), run.Agent.ID) {
		t.Fatal("a day with nothing left is not")
	}
	if _, _, isEmbedded := worker.embed(t.Context(), run.Agent.ID, "ask", []string{"a question"}); isEmbedded {
		t.Fatal("nothing is embedded past the limit")
	}
	if !worker.isBudgetLeftToEmbed(t.Context(), "an-agent-that-is-not-stored") {
		t.Fatal("an agent that is not stored has nothing to bill")
	}
}
