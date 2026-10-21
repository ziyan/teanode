package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// asCredential runs one step as the person, signed in with the credential
// kind given, in a transaction of its own: the dashboard's session is a tap on
// a screen, a command line token is not.
func (self *financeFixture) asCredential(test *testing.T, credentialKind models.CredentialKind, run func(ctx context.Context, tx db.Transaction)) {
	test.Helper()
	principal := &api.Principal{User: self.owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}}), CredentialKind: credentialKind}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx), tx)
	})
}

// The person editing a schedule on the dashboard is the person writing it. A
// schedule they wrote stays theirs through any edit. One the agent wrote
// becomes theirs only when they save a different prompt; turning it off, or
// saving the same prompt again, leaves it the agent's.
func TestEditingAScheduleKeepsOrTakesItsWriter(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	var theirsId, agentsId string
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		theirs, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: fixture.ownerAgent.ID, Name: "Morning", Cron: "0 8 * * *", Prompt: "Tell me what is on today.", WrittenBy: models.WrittenByPerson, Deliver: "drawer", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agents, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: fixture.ownerAgent.ID, Name: "Check", Cron: "0 9 * * *", Prompt: "Look at the queue.", WrittenBy: models.WrittenByAgent, Deliver: "drawer", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		theirsId, agentsId = theirs.ID, agents.ID
	})
	writer := func(ctx context.Context, tx db.Transaction, scheduleId string) string {
		schedule, err := tx.GetAgentSchedule(scheduleId)
		if err != nil || schedule == nil {
			t.Fatalf("GetAgentSchedule: %v %v", schedule, err)
		}
		return schedule.WrittenBy
	}
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		off := false
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: theirsId, Prompt: "Tell me what is on today and tomorrow.", Enabled: &off}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, theirsId); got != models.WrittenByPerson {
			t.Errorf("a schedule the person wrote stays theirs when they edit it: %q", got)
		}
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: agentsId, Prompt: "Look at the queue.", Enabled: &off}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, agentsId); got != models.WrittenByAgent {
			t.Errorf("switching the agent's schedule off, with its prompt sent back unchanged, does not make it theirs: %q", got)
		}
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: agentsId, Prompt: "Look at the queue and tell me what is waiting."}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, agentsId); got != models.WrittenByPerson {
			t.Errorf("a prompt the person saved is the person's: %q", got)
		}
	})
}

// The schedule the daily brief's switch writes is recorded as built in, not as
// the person's: it reads the inbox, so a run it wakes does not count as the
// person asking for Passwords (it is still handed to the run as their words).
// The person rewriting its prompt makes it theirs.
func TestTheBriefSwitchWritesABuiltInSchedule(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		brief, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: true})
		if err != nil || brief == nil {
			t.Fatalf("SetAgentBrief: %v %v", brief, err)
		}
		if brief.WrittenBy != models.WrittenByBuiltIn {
			t.Errorf("the brief is recorded as written by %q", brief.WrittenBy)
		}
		stored, err := tx.GetAgentSchedule(brief.ID)
		if err != nil || stored == nil || stored.WrittenBy != models.WrittenByBuiltIn {
			t.Fatalf("the stored brief: %+v %v", stored, err)
		}
		// Turning it off and on again, or moving its time, leaves the writer.
		if _, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: true, At: "08:00"}); err != nil {
			t.Fatal(err)
		}
		if stored, _ = tx.GetAgentSchedule(brief.ID); stored.WrittenBy != models.WrittenByBuiltIn {
			t.Errorf("moving the time changed the writer to %q", stored.WrittenBy)
		}
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: brief.ID, Prompt: "Read me my bank balance first thing."}); err != nil {
			t.Fatal(err)
		}
		if stored, _ = tx.GetAgentSchedule(brief.ID); stored.WrittenBy != models.WrittenByPerson {
			t.Errorf("a prompt the person wrote is theirs: %q", stored.WrittenBy)
		}
	})
}

// A schedule made or reworded from the command line is the agent's: the
// agent's own shell holds the same token, so a schedule from there cannot be
// told from one the agent made, and its runs must not use Passwords. A tap on
// the dashboard or in the phone app is the person's.
func TestAScheduleFromTheCommandLineIsTheAgents(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	var theirsId string
	for name, test := range map[string]struct {
		credentialKind models.CredentialKind
		want           string
	}{
		"the dashboard":       {models.CredentialSession, models.WrittenByPerson},
		"the phone app":       {models.CredentialApp, models.WrittenByPerson},
		"the command line":    {models.CredentialToken, models.WrittenByAgent},
		"a request with none": {"", models.WrittenByAgent},
	} {
		fixture.asCredential(t, test.credentialKind, func(ctx context.Context, tx db.Transaction) {
			created, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{Name: name, Cron: "0 8 * * *", Prompt: "Tell me what is on today."})
			if err != nil {
				t.Fatal(err)
			}
			if created.WrittenBy != test.want {
				t.Errorf("a schedule made from %s is written by %q, want %q", name, created.WrittenBy, test.want)
			}
			if test.credentialKind == models.CredentialSession {
				theirsId = created.ID
			}
		})
	}
	// Rewording the person's schedule from the command line takes it from
	// them; turning it off does not.
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		off := false
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: theirsId, Enabled: &off}); err != nil {
			t.Fatal(err)
		}
		if stored, _ := tx.GetAgentSchedule(theirsId); stored.WrittenBy != models.WrittenByPerson {
			t.Errorf("switching a schedule off from the command line changed the writer to %q", stored.WrittenBy)
		}
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: theirsId, Prompt: "Read me the shed note."}); err != nil {
			t.Fatal(err)
		}
		if stored, _ := tx.GetAgentSchedule(theirsId); stored.WrittenBy != models.WrittenByAgent {
			t.Errorf("a prompt sent from the command line is written by %q, want the agent", stored.WrittenBy)
		}
	})
}
