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
// becomes theirs when they save it with its prompt (changed or not); the on and
// off switch, which sends no prompt, leaves it the agent's.
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
		if got := writer(ctx, tx, agentsId); got != models.WrittenByPerson {
			t.Errorf("pressing Save on the agent's schedule, even with its prompt sent back unchanged, makes it theirs: %q", got)
		}
	})
	// The on and off switch sends no prompt: the person has not read what the
	// schedule says, so it stays the agent's.
	var switchedId string
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		created, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: fixture.ownerAgent.ID, Name: "Switch", Cron: "0 12 * * *", Prompt: "Look at the pond.", WrittenBy: models.WrittenByAgent, Deliver: "drawer", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		switchedId = created.ID
		off := false
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: switchedId, Enabled: &off}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, switchedId); got != models.WrittenByAgent {
			t.Errorf("switching the agent's schedule off with a tap, without its prompt, made it theirs: %q", got)
		}
		on := true
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: theirsId, Enabled: &on}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, theirsId); got != models.WrittenByPerson {
			t.Errorf("switching the person's own schedule with a tap changed the writer to %q", got)
		}
	})
	// A schedule the agent wrote is also theirs when they save a new prompt.
	var otherAgentsId string
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		created, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: fixture.ownerAgent.ID, Name: "Other", Cron: "0 10 * * *", Prompt: "Look at the shed.", WrittenBy: models.WrittenByAgent, Deliver: "drawer", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		otherAgentsId = created.ID
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: otherAgentsId, Prompt: "Look at the shed and tell me what is waiting."}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, otherAgentsId); got != models.WrittenByPerson {
			t.Errorf("a prompt the person saved is the person's: %q", got)
		}
	})
	// The same save from the command line, with the prompt unchanged, does not
	// make a schedule theirs, nor does renaming it; changing it makes it the agent's.
	var agentsFromLineId string
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		created, err := tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: fixture.ownerAgent.ID, Name: "Line", Cron: "0 11 * * *", Prompt: "Look at the garden.", WrittenBy: models.WrittenByAgent, Deliver: "drawer", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agentsFromLineId = created.ID
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: agentsFromLineId, Name: "Garden", Prompt: "Look at the garden."}); err != nil {
			t.Fatal(err)
		}
		if got := writer(ctx, tx, agentsFromLineId); got != models.WrittenByAgent {
			t.Errorf("saving the same prompt from the command line made it theirs: %q", got)
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
	// Turning the person's schedule off from the command line takes it from
	// them, as rewording it does.
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		off := false
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: theirsId, Enabled: &off}); err != nil {
			t.Fatal(err)
		}
		if stored, _ := tx.GetAgentSchedule(theirsId); stored.WrittenBy != models.WrittenByAgent {
			t.Errorf("switching a schedule off from the command line left the writer %q, want the agent", stored.WrittenBy)
		}
	})
}

// Anything a caller that is not a tap changes about when a schedule runs,
// where its answer goes, whether it runs or what it says makes the schedule the
// agent's, as the agent's schedule tool does; the same changes by a tap leave
// the person's schedule theirs.
func TestAnyChangeToAScheduleFromTheCommandLineMakesItTheAgents(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	off := false
	for name, change := range map[string]SaveAgentScheduleArguments{
		"its time":         {Cron: "30 7 * * *"},
		"where it answers": {Deliver: "drawer"},
		"whether it runs":  {Enabled: &off},
		"its words":        {Prompt: "Read me the shed note."},
	} {
		var scheduleId string
		fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
			created, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{Name: name, Cron: "0 8 * * *", Prompt: "Tell me what is on today.", Deliver: "drawer"})
			if err != nil {
				t.Fatal(err)
			}
			scheduleId = created.ID
			change.ScheduleID = scheduleId
			if _, err := fixture.resolver.SaveAgentSchedule(ctx, change); err != nil {
				t.Fatal(err)
			}
			if stored, _ := tx.GetAgentSchedule(scheduleId); stored.WrittenBy != models.WrittenByPerson {
				t.Errorf("the person changing %s on a screen made the writer %q", name, stored.WrittenBy)
			}
		})
		fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
			change.ScheduleID = scheduleId
			if change.Prompt != "" {
				change.Prompt += " Then the weather."
			}
			if _, err := fixture.resolver.SaveAgentSchedule(ctx, change); err != nil {
				t.Fatal(err)
			}
			if stored, _ := tx.GetAgentSchedule(scheduleId); stored.WrittenBy != models.WrittenByAgent {
				t.Errorf("changing %s from the command line left the writer %q, want the agent", name, stored.WrittenBy)
			}
		})
	}
	// Renaming it is not one of those changes.
	var scheduleId string
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		created, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{Name: "Morning", Cron: "0 8 * * *", Prompt: "Tell me what is on today."})
		if err != nil {
			t.Fatal(err)
		}
		scheduleId = created.ID
	})
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.SaveAgentSchedule(ctx, SaveAgentScheduleArguments{ScheduleID: scheduleId, Name: "Early"}); err != nil {
			t.Fatal(err)
		}
		if stored, _ := tx.GetAgentSchedule(scheduleId); stored.WrittenBy != models.WrittenByPerson {
			t.Errorf("renaming from the command line made the writer %q", stored.WrittenBy)
		}
	})
}
