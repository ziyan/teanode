package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The person editing a schedule on the dashboard or the command line is the
// person writing it. A schedule they wrote stays theirs through any edit. One
// the agent wrote becomes theirs only when they save a different prompt;
// turning it off, or saving the same prompt again, leaves it the agent's.
func TestEditingAScheduleKeepsOrTakesItsWriter(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	var theirsId, agentsId string
	fixture.as(t, fixture.owner, func(ctx context.Context, tx db.Transaction) {
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
	fixture.as(t, fixture.owner, func(ctx context.Context, tx db.Transaction) {
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
	fixture.as(t, fixture.owner, func(ctx context.Context, tx db.Transaction) {
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
