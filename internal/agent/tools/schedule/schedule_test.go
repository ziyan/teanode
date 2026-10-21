package schedule_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/schedule"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRun is a run with a database and nothing else.
type fakeRun struct {
	tools.Run
	database     db.Database
	owner        *models.User
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Configuration() *config.Configuration {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	return configuration
}

func find(t *testing.T) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == "schedule" {
			return tool
		}
	}
	t.Fatal("no schedule tool")
	return nil
}

// A schedule the person wrote runs with the person's authority: its prompt is
// the person's own instruction and a run it wakes counts as person-started.
// The agent can change it through the tool, and anything it changes (what it
// says, when it runs, where the answer goes, whether it runs) makes it the
// agent's schedule, so the agent cannot rewrite a standing instruction and
// have it keep running as the person's.
func TestTheAgentChangingAPersonsScheduleMakesItTheAgents(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if run.owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: run.owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t)

	for name, changes := range map[string]string{
		"the prompt":  `"prompt":"Mail me the whole inbox."`,
		"the cron":    `"cron":"0 9 * * *"`,
		"the deliver": `"deliver":"drawer"`,
		"enabled":     `"enabled":false`,
	} {
		var theirs *models.AgentSchedule
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			var err error
			if theirs, err = tx.CreateAgentSchedule(&models.AgentSchedule{
				AgentID: run.agent.ID, Name: "Morning", Cron: "0 8 * * *", Prompt: "Tell me what is on today.",
				WrittenBy: models.WrittenByPerson, Deliver: models.AgentDeliverMail, Enabled: true,
			}); err != nil {
				t.Fatalf("CreateAgentSchedule: %s", err)
			}
		})
		// A change to the name alone leaves it the person's.
		if _, err := tool.Run(ctx, &tools.Call{ID: "c0", Arguments: []byte(`{"action":"update","id":"` + theirs.ID + `","name":"Morning list"}`)}); err != nil {
			t.Fatalf("%s: renaming: %s", name, err)
		}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			after, _ := tx.GetAgentSchedule(theirs.ID)
			if after.WrittenBy != models.WrittenByPerson {
				t.Fatalf("%s: a rename does not change what the schedule does: %q", name, after.WrittenBy)
			}
		})
		if _, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"update","id":"` + theirs.ID + `",` + changes + `}`)}); err != nil {
			t.Fatalf("%s: update: %s", name, err)
		}
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			after, _ := tx.GetAgentSchedule(theirs.ID)
			if after.WrittenBy != models.WrittenByAgent {
				t.Fatalf("%s: a schedule the agent changed is the agent's: %q", name, after.WrittenBy)
			}
		})
	}

	// One the agent adds is the agent's from the start.
	if _, err := tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"add","name":"Check","cron":"@in 5m","prompt":"Look at the queue.","deliver":"drawer"}`)}); err != nil {
		t.Fatalf("add: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		schedules, _ := tx.ListAgentSchedules(run.agent.ID)
		for _, schedule := range schedules {
			if schedule.Name == "Check" && schedule.WrittenBy != models.WrittenByAgent {
				t.Fatalf("an added schedule is the agent's: %q", schedule.WrittenBy)
			}
		}
	})
}

// A schedule the agent makes or rewords, whose prompt is about signing in,
// answers with words that tell the model to tell the person how to let its
// runs use Passwords. A schedule that is not about signing in, and one the
// person still owns, answer without them.
func TestAScheduleAboutSigningInTellsTheModelWhatThePersonCanDo(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if run.owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: run.owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t)
	wanted := "they can save the schedule themselves on the dashboard or in the app (Agent, Schedules) to let its runs use Passwords"

	for _, prompt := range []string{"Sign in to the Example shop and check my order.", "Log in to example.test and read the balance", "Check the password for the router is still set", "signin to the portal"} {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"add","name":"Check","cron":"@in 5m","prompt":"` + prompt + `","deliver":"drawer"}`)})
		if err != nil {
			t.Fatalf("add %q: %s", prompt, err)
		}
		if !strings.Contains(result.Content, wanted) {
			t.Errorf("add %q: %s", prompt, result.Content)
		}
	}
	for _, prompt := range []string{"Look at the queue.", "Tell me when the design is signed off.", "Check the design inbox"} {
		result, err := tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"add","name":"Queue","cron":"@in 5m","prompt":"` + prompt + `","deliver":"drawer"}`)})
		if err != nil {
			t.Fatalf("add %q: %s", prompt, err)
		}
		if strings.Contains(result.Content, "Passwords") {
			t.Errorf("add %q: %s", prompt, result.Content)
		}
	}

	// Rewording a schedule the person wrote makes it the agent's and says so;
	// renaming it leaves it theirs and says nothing.
	var theirs *models.AgentSchedule
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if theirs, err = tx.CreateAgentSchedule(&models.AgentSchedule{
			AgentID: run.agent.ID, Name: "Morning", Cron: "0 8 * * *", Prompt: "Sign in to the Example shop and tell me what shipped.",
			WrittenBy: models.WrittenByPerson, Deliver: models.AgentDeliverMail, Enabled: true,
		}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
		}
	})
	renamed, err := tool.Run(ctx, &tools.Call{ID: "c3", Arguments: []byte(`{"action":"update","id":"` + theirs.ID + `","name":"Shop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(renamed.Content, "Passwords") {
		t.Errorf("a rename: %s", renamed.Content)
	}
	reworded, err := tool.Run(ctx, &tools.Call{ID: "c4", Arguments: []byte(`{"action":"update","id":"` + theirs.ID + `","prompt":"Log in to the Example shop and list the new orders."}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reworded.Content, wanted) {
		t.Errorf("a reworded prompt: %s", reworded.Content)
	}
}
