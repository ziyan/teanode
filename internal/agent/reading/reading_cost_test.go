package reading_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What the reading has cost, and what the rest of it comes to.
//
// The row already guessed at hours. Hours are not what somebody watching
// a hundred and fifty thousand documents go by is deciding about; the bill
// is, and the same rows that say how fast the night reads say what the
// reading cost.
func TestTheReadingSaysWhatItCostAndWhatIsLeftToSpend(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Currency = "eur"
	// A million prompt tokens costs 2, a million completion 10.
	configuration.Agent.Providers = []config.AgentProvider{
		{Name: "one", Kind: "openai", BaseURL: "http://x.example", APIKey: "k",
			Pricing: config.AgentPricing{Input: 2, Output: 10}},
	}

	var owner *models.User
	var person *models.Agent
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}

		// One night, two hours long, that read a hundred documents.
		started := time.Now().Add(-3 * time.Hour)
		finished := started.Add(2 * time.Hour)
		dream, err := tx.StartAgentDream(&models.AgentDream{AgentID: person.ID, StartedAt: started})
		if err != nil {
			test.Fatalf("StartAgentDream: %s", err)
		}
		dream.FinishedAt = &finished
		dream.Digested = 100
		if err := tx.FinishAgentDream(dream); err != nil {
			test.Fatalf("FinishAgentDream: %s", err)
		}

		// What that night spent: a million prompt tokens and a hundred
		// thousand completion, which comes to 2 + 1 = 3.
		if err := tx.PutAgentUsage(&db.AgentUsage{
			AgentID: person.ID, Model: "one:thinker", Kind: string(models.AgentJobDream),
			At: started.Add(time.Hour), Values: []uint64{1_000_000, 100_000, 0, 0, 1},
		}); err != nil {
			test.Fatalf("PutAgentUsage: %s", err)
		}
		// And what the person's own conversations cost in the same window,
		// which is not the reading and must not be priced into it.
		if err := tx.PutAgentUsage(&db.AgentUsage{
			AgentID: person.ID, Model: "one:thinker", Kind: "ask",
			At: started.Add(time.Hour), Values: []uint64{5_000_000, 500_000, 0, 0, 9},
		}); err != nil {
			test.Fatalf("PutAgentUsage: %s", err)
		}

		// A hundred documents read and fifty still waiting, so the rest
		// costs half of what has been spent.
		writeDocuments(test, tx, person.ID, 100, true)
		writeDocuments(test, tx, person.ID, 50, false)
	})

	var progress *reading.Progress
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if progress, err = reading.For(tx, configuration, person, owner); err != nil {
			test.Fatalf("reading.For: %s", err)
		}
	})

	if progress.Read != 100 || progress.Waiting != 50 {
		test.Fatalf("the counts are 100 read and 50 waiting, not %d and %d", progress.Read, progress.Waiting)
	}
	if math.Abs(progress.Spent-3) > 0.001 {
		test.Fatalf("the night cost 3 and not %v -- the person's own turns must not be in it", progress.Spent)
	}
	// Three for a hundred documents is 0.03 each, and fifty of those is 1.5.
	if math.Abs(progress.CostLeft-1.5) > 0.001 {
		test.Fatalf("the rest comes to 1.5 at that price, not %v", progress.CostLeft)
	}
	if progress.Currency != "eur" {
		test.Fatalf("said in the operator's currency, not %q", progress.Currency)
	}
	// And the row can say what it measured. "The dreams behind that pace"
	// named a window without saying there was one, so a person meeting it
	// could not tell whether it meant today, last night, or all of them.
	if progress.Dreams != 1 {
		test.Fatalf("one night was counted, and it says %d", progress.Dreams)
	}
}

// And where nothing prices the models, the row says nothing about money
// rather than saying it is free. A model somebody runs themselves has no
// price, and that is the ordinary case for the reading.
func TestReadingWithNoPricesSaysNothingAboutMoney(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// A provider with no pricing at all, which is what a local model is.
	configuration.Agent.Providers = []config.AgentProvider{
		{Name: "one", Kind: "openai", BaseURL: "http://x.example", APIKey: "k"},
	}

	var owner *models.User
	var person *models.Agent
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if person, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(person.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		started := time.Now().Add(-3 * time.Hour)
		finished := started.Add(2 * time.Hour)
		dream, err := tx.StartAgentDream(&models.AgentDream{AgentID: person.ID, StartedAt: started})
		if err != nil {
			test.Fatalf("StartAgentDream: %s", err)
		}
		dream.FinishedAt = &finished
		dream.Digested = 100
		if err := tx.FinishAgentDream(dream); err != nil {
			test.Fatalf("FinishAgentDream: %s", err)
		}
		if err := tx.PutAgentUsage(&db.AgentUsage{
			AgentID: person.ID, Model: "one:thinker", Kind: string(models.AgentJobDream),
			At: started.Add(time.Hour), Values: []uint64{1_000_000, 100_000, 0, 0, 1},
		}); err != nil {
			test.Fatalf("PutAgentUsage: %s", err)
		}
		writeDocuments(test, tx, person.ID, 100, true)
		writeDocuments(test, tx, person.ID, 50, false)
	})

	var progress *reading.Progress
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if progress, err = reading.For(tx, configuration, person, owner); err != nil {
			test.Fatalf("reading.For: %s", err)
		}
	})
	if progress.Spent != 0 || progress.CostLeft != 0 {
		test.Fatalf("an unpriced model costs nothing anybody can state: %v and %v", progress.Spent, progress.CostLeft)
	}
	if progress.PerHour <= 0 {
		test.Fatal("and the pace is still measured, which is the half of this that does not need prices")
	}
}

// writeDocuments files documents for an agent, read or waiting.
func writeDocuments(test *testing.T, tx db.Transaction, agentId string, count int, read bool) {
	test.Helper()
	source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: agentId, Name: "notes", Kind: models.SourceComputer,
		Specification: models.AgentKnowledgeSpecification{Path: "/notes", Computer: "gen7"},
	})
	if err != nil {
		test.Fatalf("PutAgentSource: %s", err)
	}
	for number := 0; number < count; number++ {
		metadata := map[string]any{}
		if read {
			metadata["digested"] = time.Now().Format(time.RFC3339)
		}
		if _, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: agentId, SourceID: source.ID,
			ExternalID: externalName(read, number), Kind: models.DocumentFile,
			Title: externalName(read, number), Bytes: 4096, Metadata: metadata,
		}); err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}
	}
}

func externalName(read bool, number int) string {
	if read {
		return "read-" + itoa(number)
	}
	return "waiting-" + itoa(number)
}

func itoa(number int) string {
	if number == 0 {
		return "0"
	}
	digits := ""
	for number > 0 {
		digits = string(rune('0'+number%10)) + digits
		number /= 10
	}
	return digits
}

// The estimate says what the budget comes to, where the budget is what the
// reading is waiting for.
//
// The hours are what it would take dreaming without pause. An agent with a
// daily budget stops when the budget is spent, and where the rest costs
// several days of it the two answers differ by days: a person told "about
// eleven hours left" on a reading with four days of budget to go has been
// misled by a number that looks precise. This is the night that ran out at
// $70.01 of a $70 budget and reported it as a model that did not answer.
func TestTheEstimateSaysWhatTheBudgetComesTo(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		what     string
		costLeft float64
		budget   float64
		days     int
		says     bool
	}{
		{"four days of budget", 280, 70, 4, true},
		{"a part of a day counts as a day", 71, 70, 2, true},
		{"exactly a day's budget is not days", 70, 70, 0, false},
		{"what fits inside a day", 12, 70, 0, false},
		{"no budget at all", 280, 0, 0, false},
	} {
		progress := &reading.Progress{Waiting: 1000, Read: 1000, PerHour: 100, HoursLeft: 10, CostLeft: each.costLeft, Currency: "usd"}
		if each.budget > 0 && each.costLeft > each.budget {
			progress.DaysAtBudget = int(math.Ceil(each.costLeft / each.budget))
		}
		if progress.DaysAtBudget != each.days {
			test.Errorf("%s: %d days", each.what, progress.DaysAtBudget)
		}
		line := progress.Describe()
		if said := strings.Contains(line, "daily budget spreads over"); said != each.says {
			test.Errorf("%s: the line says it=%v: %q", each.what, said, line)
		}
	}
}
