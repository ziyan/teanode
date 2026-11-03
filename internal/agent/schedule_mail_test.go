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

// A schedule that waits for mail is set off by a message that passes its
// conditions as it arrives, and not by one that does not or by one the
// filters put in Junk. The tick runs it; its turn is told what came, with the
// sender quoted as unverified, and the run forgets the message afterwards
// while the schedule stays on, waiting for the next.
func TestAScheduleRunsWhenItsMailArrives(t *testing.T) {
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
	var schedule *models.AgentSchedule
	var inbox, junk *models.MailboxFolder
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example", Timezone: "Europe/Berlin", TimezoneMode: "fixed"})
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
		if schedule, err = tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: found.ID, Name: "School pictures", MailConditions: conditions, Prompt: "Save the pictures it links.", Deliver: "drawer", Enabled: true}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
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
		waiting, _ := tx.GetAgentSchedule(schedule.ID)
		if len(waiting.MailArrivals) != 0 || waiting.NextRunAt != nil {
			t.Fatalf("mail that does not pass, or that went to Junk, set the schedule off: %+v", waiting)
		}
	})
	item := deliver(inbox, "School News <news@school.example>", "Pictures from today")
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		waiting, _ := tx.GetAgentSchedule(schedule.ID)
		if len(waiting.MailArrivals) != 1 || waiting.MailArrivals[0].MailboxItemID != item.ID || waiting.NextRunAt == nil {
			t.Fatalf("the message should be noted and the schedule due: %+v", waiting)
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
			jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobSchedule}}, nil)
			done = len(jobs) == 1 && jobs[0].Status == models.AgentJobDone
		})
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Kinds: []models.AgentJobKind{models.AgentJobSchedule}}, nil)
		if len(jobs) != 1 || jobs[0].Status != models.AgentJobDone {
			t.Fatalf("the schedule should have run once: %+v", jobs)
		}
		after, _ := tx.GetAgentSchedule(schedule.ID)
		if after == nil || !after.Enabled || len(after.MailArrivals) != 0 || after.NextRunAt != nil || after.LastRunAt == nil {
			t.Fatalf("the run should forget the message and leave the schedule on, waiting: %+v", after)
		}
	})
	if len(*requests) == 0 {
		t.Fatal("the run took no turn")
	}
	turn := ""
	for _, message := range (*requests)[0]["messages"].([]any) {
		if content, _ := message.(map[string]any)["content"].(string); strings.Contains(content, models.ScheduleMarker) {
			turn = content
		}
	}
	for _, want := range []string{"mail it waits for arrived", `"Pictures from today"`, `"School News <news@school.example>"`, "item_id " + item.ID, "not verified", "Save the pictures it links."} {
		if !strings.Contains(turn, want) {
			t.Errorf("the turn should say %q:\n%s", want, turn)
		}
	}
	if strings.Contains(turn, "Lunch?") || strings.Contains(turn, "again") {
		t.Errorf("the turn was told about mail that did not set it off:\n%s", turn)
	}
}

// A schedule runs at set times or on arriving mail, never both, and its mail
// conditions are those a message can be judged by as it arrives.
func TestAScheduleOnMailIsValidated(t *testing.T) {
	for _, test := range []struct {
		name     string
		schedule models.AgentSchedule
		isValid  bool
	}{
		{"on mail", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "subject", Operator: "matches", Value: "^Pictures"}}}, true},
		{"any message", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "any"}}}, true},
		{"both", models.AgentSchedule{Name: "n", Prompt: "p", Cron: "0 8 * * *", MailConditions: []models.MailboxRuleCondition{{Field: "any"}}}, false},
		{"neither", models.AgentSchedule{Name: "n", Prompt: "p"}, false},
		{"a field known only after sorting", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "category", Operator: "equals", Value: "receipt"}}}, false},
		{"a broken pattern", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "subject", Operator: "matches", Value: "("}}}, false},
		{"no value", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "from", Operator: "contains"}}}, false},
		{"a header without its name", models.AgentSchedule{Name: "n", Prompt: "p", MailConditions: []models.MailboxRuleCondition{{Field: "header", Operator: "contains", Value: "x"}}}, false},
	} {
		if err := test.schedule.Validate(); (err == nil) != test.isValid {
			t.Errorf("%s: Validate said %v, want valid %v", test.name, err, test.isValid)
		}
	}
}
