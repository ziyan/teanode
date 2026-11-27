package agent

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A night has no share of the day of its own: the daily limit is what
// binds it, less what the day has already spent in conversation and in
// earlier nights.
func TestANightGetsWhatTheDailyLimitHasLeft(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Limits.DailyTokensPerAgent = 200000
	agent := &models.Agent{}

	first := newDreamBudget(configuration, agent, 0)
	if first.allowed != 200000 {
		t.Fatalf("the first night of the day has the whole limit: %d", first.allowed)
	}
	second := newDreamBudget(configuration, agent, 150000)
	if second.allowed != 50000 {
		t.Fatalf("the second has what the day left: %d", second.allowed)
	}
	if !second.left() {
		t.Fatal("and may still spend it")
	}
	// A day with nothing left has nothing, which is not the same number as
	// a night nothing caps.
	third := newDreamBudget(configuration, agent, 200000)
	if third.left() {
		t.Fatal("a night on a day that is spent may not call a model")
	}
	// And a deployment that caps nothing still caps nothing, however much
	// the day has spent.
	free := config.Default()
	free.Agent.Limits.DailyTokensPerAgent = 0
	if uncapped := newDreamBudget(free, agent, 500000); !uncapped.left() {
		t.Fatal("no daily limit still means no cap")
	}

	// An agent's own limit is what binds where the operator set one.
	own := newDreamBudget(configuration, &models.Agent{DailyTokens: 10000}, 1000)
	if own.allowed != 9000 {
		t.Fatalf("the person's own limit binds first: %d", own.allowed)
	}
}

// What a night is held to is the whole day's spending, every kind of work,
// from the owner's own midnight.
func TestTheDaysSpendIsReadBackWhateverTheKind(t *testing.T) {
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
			// Placed against the owner's own day rather than the clock of
			// whoever runs the test, so that the hours after the owner's
			// midnight do not change the answer.
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
		spent, _, err := SumSpend(tx, config.Default(), agentId, today)
		if err != nil {
			t.Fatalf("SumSpend: %s", err)
		}
		// The conversation's ninety thousand counts, and yesterday's night
		// does not.
		if spent != 135000 {
			t.Fatalf("today spent 135000, not %d", spent)
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

// A night whose provider will not bill the account stops, rather than
// asking again for every page, every document and every person left. The
// budget is where it stops, because every stage of the night already asks
// the budget whether to go on.
func TestARefusedAccountEndsTheNight(t *testing.T) {
	budget := &dreamBudget{allowed: 1000000}
	if !budget.left() {
		t.Fatal("a fresh night has room")
	}
	budget.refuse()
	if budget.left() {
		t.Fatal("the night goes on after the provider refused to bill")
	}
	if budget.reserve() {
		t.Fatal("and it claimed the cost of another call")
	}
}

// The night's row said the model did not answer whenever the reading gave
// up, including when what stopped it was the day's allowance. The two look
// the same from the reading's side: a turn the allowance stopped is noted
// and ends without an error, so the batch comes back unanswered either way.
func TestTheRowSaysWhatStoppedTheReading(t *testing.T) {
	quiet := readingStopReason(nil)
	if quiet != "the model did not answer; the reading stops here" {
		t.Errorf("with no budget to read: %q", quiet)
	}
	if got := readingStopReason(&Budget{Limit: 1000, Used: 10}); got != quiet {
		t.Errorf("with room left: %q", got)
	}
	spent := readingStopReason(&Budget{CostLimit: 70, Cost: 70.01, Currency: "USD"})
	if !strings.Contains(spent, "is used up") || strings.Contains(spent, "the model did not answer") {
		t.Errorf("with the day's money gone: %q", spent)
	}
	if !strings.HasSuffix(spent, "; the reading stops here") {
		t.Errorf("and it still says the reading stops: %q", spent)
	}
}

// Catching up switches itself off when a night finds nothing left to read.
// A night that could not read is not that night, and the difference is the
// whole of what keeps a backlog moving: one that read nothing and reported
// nothing wrong ended catching up with fifty thousand documents waiting,
// because a provider had refused every call and the night said so nowhere.
func TestANightThatCouldNotReadHasNotCaughtUp(t *testing.T) {
	nothingWaiting := &models.AgentDream{Backlog: 40, Digested: 40}
	if !caughtUp(nothingWaiting) {
		t.Error("a night that read everything waiting has caught up")
	}
	nothingToRead := &models.AgentDream{Backlog: 0, Digested: 0}
	if !caughtUp(nothingToRead) {
		t.Error("a night with nothing to read has caught up")
	}
	stillGoing := &models.AgentDream{Backlog: 5000, Digested: 40}
	if caughtUp(stillGoing) {
		t.Error("a night that left a backlog has not caught up")
	}
	couldNotRead := &models.AgentDream{Backlog: 51854, Digested: 0, LastError: "the provider would not bill this account; the reading stops here"}
	if caughtUp(couldNotRead) {
		t.Error("a night that could not read has not caught up, whatever it digested")
	}
}

// And the night says which of the two stopped it, since one is the person's
// own cap and the other is the account behind the provider.
func TestTheBudgetSaysWhichOneStoppedTheReading(t *testing.T) {
	spent := (&dreamBudget{}).whyItStopped()
	if !strings.Contains(spent, "what the day allows") {
		t.Errorf("an allowance that ran out: %q", spent)
	}
	refused := &dreamBudget{}
	refused.refuse()
	if got := refused.whyItStopped(); !strings.Contains(got, "would not bill") {
		t.Errorf("a provider that refused: %q", got)
	}
}
