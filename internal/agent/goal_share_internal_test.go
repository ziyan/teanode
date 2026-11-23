package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The goals at their own pace spend at most their share of the person's day
// between them: what goal turns spent since midnight counts, what the person's
// own conversation spent does not, and a server with no daily budget has no
// share to run out of.
func TestTheGoalsSpendTheirShareOfTheDay(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	owner := &models.User{Timezone: "Europe/London"}
	var agent *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "alice", Timezone: owner.Timezone})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if agent, err = tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	today := DayStart(owner, time.Now())
	configuration := config.Default()
	configuration.Agent.Limits.DailyTokensPerAgent = 100000

	spend := func(kind string, tokens uint64) {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if err := tx.PutAgentUsage(&db.AgentUsage{
				AgentID: agent.ID, Model: "fake:thinker", Kind: kind, At: today.Add(time.Minute),
				Values: []uint64{tokens, 0, 0, 0, 1},
			}); err != nil {
				t.Fatalf("PutAgentUsage: %s", err)
			}
		})
	}
	isSpent := func(configuration *config.Configuration) bool {
		var isShareSpent bool
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if isShareSpent, err = goalShareSpent(tx, configuration, agent, today); err != nil {
				t.Fatalf("goalShareSpent: %s", err)
			}
		})
		return isShareSpent
	}

	spend("ask", 90000)
	spend(string(models.AgentJobGoal), 40000)
	if isSpent(configuration) {
		t.Fatal("forty thousand of goal turns is under half of a hundred thousand, and the conversation's is not the goals'")
	}
	spend(string(models.AgentJobGoal), 10000)
	if !isSpent(configuration) {
		t.Fatal("fifty thousand of goal turns is half the day, which is the goals' share")
	}
	configuration.Agent.Limits.GoalShare = 0.8
	if isSpent(configuration) {
		t.Fatal("an operator who gives the goals more of the day lets them go on")
	}
	configuration.Agent.Limits.DailyTokensPerAgent = 0
	if isSpent(configuration) {
		t.Fatal("a day with no budget has no share to spend")
	}
}
