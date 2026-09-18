package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The share belongs to the day, not to each night.
//
// A night runs as often as every six hours, so four of them in a day each
// starting from the whole thirty percent spend the day's budget twice
// over and leave nothing of what the share was protecting. What the day's
// earlier nights spent is read back from the usage rows and comes off
// what this one may have.
func TestASecondNightOfTheDayGetsWhatIsLeft(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Limits.DailyTokensPerAgent = 200000
	agent := &models.Agent{}

	first := newDreamBudget(configuration, agent, 0.3, 0)
	if first.allowed != 60000 {
		t.Fatalf("the first night of the day has the whole share: %d", first.allowed)
	}
	second := newDreamBudget(configuration, agent, 0.3, 45000)
	if second.allowed != 15000 {
		t.Fatalf("the second has what the first left: %d", second.allowed)
	}
	if !second.left() {
		t.Fatal("and may still spend it")
	}
	// A night whose share is gone has nothing, which is not the same
	// number as a night nothing caps.
	third := newDreamBudget(configuration, agent, 0.3, 60000)
	if third.left() {
		t.Fatal("a night whose share today is spent may not call a model")
	}
	// And a deployment that caps nothing still caps nothing, however
	// much the day has spent.
	free := config.Default()
	free.Agent.Limits.DailyTokensPerAgent = 0
	if uncapped := newDreamBudget(free, agent, 0.3, 500000); !uncapped.left() {
		t.Fatal("no daily limit still means no cap")
	}

	// An agent's own limit is what binds where the operator set one.
	own := newDreamBudget(configuration, &models.Agent{DailyTokens: 10000}, 0.3, 1000)
	if own.allowed != 2000 {
		t.Fatalf("the person's own limit binds first: %d", own.allowed)
	}
}

// What a night reads back is the nights' own spending and nobody else's,
// bounded by the person's own day.
//
// The whole of the fix above rests on this number, and a kind spelled one
// way where it is written and another where it is read would make the
// share fresh again without anything failing.
func TestTheDaysDreamSpendIsReadBackByKind(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	owner := &models.User{Timezone: "Europe/London"}
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "alice", Timezone: owner.Timezone})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = found.ID
	})
	today := DayStart(owner, time.Now())
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, each := range []struct {
			kind   string
			at     time.Time
			tokens uint64
		}{
			// Placed against the owner's own day rather than the
			// clock of whoever runs the test. The owner keeps London
			// time; a machine in New York is five hours behind it, so
			// for the two hours after London midnight -- between
			// seven and nine in the evening there -- an hour ago is
			// yesterday to the owner, and rows meant for today fell
			// outside the sum. The test failed for two hours a day
			// and passed for twenty-two.
			{string(models.AgentJobDream), today.Add(time.Minute), 40000},
			{string(models.AgentJobDream), today.Add(2 * time.Minute), 5000},
			{"ask", today.Add(time.Minute), 90000},
			{string(models.AgentJobDream), today.Add(-3 * time.Hour), 70000},
		} {
			if err := tx.PutAgentUsage(&db.AgentUsage{
				AgentID: agentId, Model: "fake:thinker", Kind: each.kind, At: each.at,
				Values: []uint64{each.tokens, 0, 0, 0, 1},
			}); err != nil {
				t.Fatalf("PutAgentUsage: %s", err)
			}
		}
		spent, err := SumSpendOfKind(tx, agentId, string(models.AgentJobDream), today)
		if err != nil {
			t.Fatalf("SumSpendOfKind: %s", err)
		}
		// The conversation's ninety thousand is not the night's, and
		// yesterday's night is not today's.
		if spent != 45000 {
			t.Fatalf("today's nights spent 45000, not %d", spent)
		}
	})
}

// Three batches reading at once cannot each be told the whole remainder
// is free.
//
// The check and the spending used to be two steps with a model call
// between them, so every batch in flight was answered from the same
// number and the night spent as many times its allowance as it had
// batches running. A call claims an estimate before it is made and
// corrects it with what it really cost when it comes back.
func TestConcurrentCallsCannotEachSpendTheRemainder(t *testing.T) {
	budget := &dreamBudget{allowed: 2 * dreamCallEstimate}

	var granted int
	var mutex sync.Mutex
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if !budget.reserve() {
				return
			}
			mutex.Lock()
			granted++
			mutex.Unlock()
		}()
	}
	group.Wait()
	if granted != 2 {
		t.Fatalf("two estimates fit in two estimates' worth of budget, not %d", granted)
	}

	// What they really cost replaces what they were assumed to cost, so
	// a night of cheap calls is not stopped by its own estimates.
	budget.settle(llm.Usage{PromptTokens: 100, CompletionTokens: 20})
	budget.settle(llm.Usage{PromptTokens: 100, CompletionTokens: 20})
	if budget.spent != 240 || budget.reserved != 0 {
		t.Fatalf("spent %d with %d still reserved", budget.spent, budget.reserved)
	}
	if !budget.left() {
		t.Fatal("and the night reads on")
	}
}
