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
