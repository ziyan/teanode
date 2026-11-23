package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A goal that waits for mail is set off by a message that passes its
// conditions as it arrives, and not by one that does not or by one the
// filters put in Junk. The tick gives it a turn; the turn is told what came,
// with the sender quoted as unverified, and forgets the message afterwards
// while the goal stays on, waiting for the next.
func TestAGoalTakesATurnWhenItsMailArrives(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{`{"id":"s1","model":"m","choices":[{"delta":{"content":"Saved the pictures."},"finish_reason":"stop"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":5}}`})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	off := false
	configuration.Agent.Features.Dreaming = &off
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) { return operations, nil })

	var found *models.Agent
	var mailbox *models.Mailbox
	var goal *models.AgentConversation
	var inbox, junk *models.MailboxFolder
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var owner *models.User
		owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example", Timezone: "Europe/Berlin", TimezoneMode: "fixed"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if inbox, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind inbox: %v %s", inbox, err)
		}
		if junk, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindJunk); err != nil || junk == nil {
			t.Fatalf("GetFolderByKind junk: %v %s", junk, err)
		}
		conditions := []models.MailboxRuleCondition{{Field: "from", Operator: "contains", Value: "news@school.example"}}
		if goal, err = worker.StartGoal(tx, found, owner, &agent.GoalStart{GoalTitle: "School pictures", GoalDescription: "Save the pictures it links.",
			GoalMailConditions: conditions, GoalWrittenBy: models.WrittenByPerson}); err != nil {
			t.Fatalf("StartGoal: %s", err)
		}
	})
	deliver := func(folder *models.MailboxFolder, from, subject string) *models.MailboxItem {
		var item *models.MailboxItem
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			mail, err := tx.CreateMail(&models.Mail{Subject: subject, From: from, Kind: models.MailKindIncoming}, nil)
			if err != nil {
				t.Fatalf("CreateMail: %s", err)
			}
			if item, err = tx.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				t.Fatalf("AddItem: %s", err)
			}
			worker.OnMailboxDelivery(tx, mailbox, item, mail)
		})
		return item
	}
	deliver(inbox, "someone@elsewhere.example", "Lunch?")
	deliver(junk, "news@school.example", "Pictures from today, again")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		waiting, _ := tx.GetAgentConversation(goal.ID)
		if len(waiting.GoalMailArrivals) != 0 || waiting.GoalNextAt != nil {
			t.Fatalf("mail that does not pass, or that went to Junk, set the goal off: %+v", waiting)
		}
	})
	item := deliver(inbox, "School News <news@school.example>", "Pictures from today")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		waiting, _ := tx.GetAgentConversation(goal.ID)
		if len(waiting.GoalMailArrivals) != 1 || waiting.GoalMailArrivals[0].MailboxItemID != item.ID || waiting.GoalNextAt == nil {
			t.Fatalf("the message should be noted and the goal due: %+v", waiting)
		}
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var done bool
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
			done = len(jobs) == 1 && jobs[0].Status == models.AgentJobDone
		})
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
		if len(jobs) != 1 || jobs[0].Status != models.AgentJobDone {
			t.Fatalf("the goal should have taken one turn: %+v", jobs)
		}
		after, _ := tx.GetAgentConversation(goal.ID)
		if after == nil || after.GoalState != models.GoalWorking || len(after.GoalMailArrivals) != 0 || after.GoalNextAt != nil {
			t.Fatalf("the turn should forget the message and leave the goal on, waiting: %+v", after)
		}
	})
	if len(*requests) == 0 {
		t.Fatal("the run took no turn")
	}
	turn := ""
	for _, message := range (*requests)[0]["messages"].([]any) {
		if content, _ := message.(map[string]any)["content"].(string); strings.Contains(content, models.GoalCheckInMarker) {
			turn = content
		}
	}
	for _, want := range []string{"when mail it waits for arrives", `"Pictures from today"`, `"School News <news@school.example>"`, "item_id " + item.ID, "not verified", "Save the pictures it links."} {
		if !strings.Contains(turn, want) {
			t.Errorf("the turn should say %q:\n%s", want, turn)
		}
	}
	if strings.Contains(turn, "Lunch?") || strings.Contains(turn, "today, again") {
		t.Errorf("the turn was told about mail that did not set it off:\n%s", turn)
	}
}

// A goal's mail conditions are those a message can be judged by as it
// arrives, each with what it needs to be judged.
func TestAGoalsMailConditionsAreValidated(t *testing.T) {
	for _, test := range []struct {
		name       string
		conditions []models.MailboxRuleCondition
		isValid    bool
	}{
		{"on mail", []models.MailboxRuleCondition{{Field: "subject", Operator: "matches", Value: "^Pictures"}}, true},
		{"any message", []models.MailboxRuleCondition{{Field: "any"}}, true},
		{"none", nil, true},
		{"a field known only after sorting", []models.MailboxRuleCondition{{Field: "category", Operator: "equals", Value: "receipt"}}, false},
		{"a broken pattern", []models.MailboxRuleCondition{{Field: "subject", Operator: "matches", Value: "("}}, false},
		{"no value", []models.MailboxRuleCondition{{Field: "from", Operator: "contains"}}, false},
		{"a header without its name", []models.MailboxRuleCondition{{Field: "header", Operator: "contains", Value: "x"}}, false},
	} {
		if err := models.ValidateGoalMailConditions(test.conditions); (err == nil) != test.isValid {
			t.Errorf("%s: said %v, want valid %v", test.name, err, test.isValid)
		}
	}
}
