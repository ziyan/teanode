package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// goalMigrationId is the migration that turned schedules into goals.
const goalMigrationId = "0281_goal_takes_schedules"

func goalMigration(t *testing.T) migrations.Migration {
	t.Helper()
	for _, migration := range migrations.Migrations() {
		if migration.ID == goalMigrationId {
			return migration
		}
	}
	t.Fatalf("the migration %s is missing", goalMigrationId)
	return migrations.Migration{}
}

// Every schedule becomes a goal. The ones a goal owned are its times, their
// words kept in its description under their names, and it is due at the
// soonest of them; a second mail schedule of the same goal keeps only its
// words. A schedule of its own is a goal of its own, with the schedule's id:
// on a cron line it repeats, at a moment it is that moment, on mail it waits
// for mail, switched off it is paused, and a brief its switch made before
// that was recorded is TeaNode's. The cards a schedule posted are its goal's,
// a schedule's queued run is gone, and so is the table. Back again, each
// time is a schedule of its goal's once more.
func TestSchedulesBecomeGoals(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	var agentId, userId, mainId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "keeper", Name: "Example Keeper"})
		if err != nil {
			t.Fatal(err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		main, err := tx.MainAgentConversation(found.ID, "drawer")
		if err != nil {
			t.Fatal(err)
		}
		agentId, userId, mainId = found.ID, owner.ID, main.ID
	})

	migration := goalMigration(t)
	execMigrationSQL(t, database, migration.ReverseSQL)
	soon := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	execMigrationSQL(t, database, `
		INSERT INTO "agent_conversation" ("id", "created_at", "modified_at", "agent_id", "kind", "title", "last_at", "goal", "goal_title", "goal_state")
		VALUES ('goal-tax', now(), now(), '`+agentId+`', 'goal', 'Property tax', now(), 'Track the property tax bill.', 'Property tax', 'working');
		INSERT INTO "agent_schedule" ("id", "created_at", "modified_at", "agent_id", "name", "cron", "prompt", "deliver", "enabled", "next_run_at", "written_by", "conversation_id", "mail_conditions")
		VALUES
			('weekly', now(), now(), '`+agentId+`', 'Look for the bill', '0 9 * * 1', 'Look for the bill in the mail.', 'drawer', true, '`+later+`', 'agent', 'goal-tax', NULL),
			('reminder', now() + interval '1 second', now(), '`+agentId+`', 'A week before', '@at 2030-12-11 09:00', 'Remind me it is due in a week.', 'drawer', true, '`+soon+`', 'agent', 'goal-tax', NULL),
			('tax-mail', now() + interval '2 seconds', now(), '`+agentId+`', 'When the bill comes', '', 'Read the bill.', 'drawer', true, NULL, 'agent', 'goal-tax', '[{"field":"from","operator":"contains","value":"tax@city.example"}]'),
			('tax-mail-again', now() + interval '3 seconds', now(), '`+agentId+`', 'When a notice comes', '', 'Read the notice.', 'drawer', true, NULL, 'agent', 'goal-tax', '[{"field":"any"}]'),
			('morning', now(), now(), '`+agentId+`', 'Morning', '0 8 * * 1-5', 'Tell me what is on today.', 'feed', true, '`+soon+`', 'person', '`+mainId+`', NULL),
			('brief', now(), now(), '`+agentId+`', 'Daily brief', '30 7 * * 1-5', 'Tell me what today holds.', 'feed', true, '`+soon+`', 'person', '', NULL),
			('once', now(), now(), '`+agentId+`', 'Call back', '@at 2030-01-02 15:00', 'Remind me to call back.', 'drawer', true, '`+soon+`', 'agent', '', NULL),
			('off', now(), now(), '`+agentId+`', 'Pond', '0 12 * * *', 'Look at the pond.', 'drawer', false, '`+soon+`', 'agent', '', NULL),
			('pictures', now(), now(), '`+agentId+`', 'School pictures', '', 'Save the pictures.', 'drawer', true, NULL, 'agent', '', '[{"field":"any"}]');
		INSERT INTO "agent_feed_item" ("id", "agent_id", "user_id", "feed_item_kind", "feed_source_kind", "feed_source_id", "created_at", "updated_at")
		VALUES
			('card-weekly', '`+agentId+`', '`+userId+`', 'report', 'schedule', 'weekly', now(), now()),
			('card-morning', '`+agentId+`', '`+userId+`', 'report', 'schedule', 'morning', now(), now());
		INSERT INTO "agent_job" ("id", "created_at", "agent_id", "kind", "subject_id", "status")
		VALUES ('queued-run', now(), '`+agentId+`', 'schedule', 'morning', 'queued');
	`)
	execMigrationSQL(t, database, migration.SQL)

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		read := func(goalId string) *models.AgentConversation {
			t.Helper()
			goal, err := tx.GetAgentConversation(goalId)
			if err != nil || goal == nil || goal.Kind != models.AgentConversationGoal {
				t.Fatalf("goal %s: %+v %v", goalId, goal, err)
			}
			return goal
		}

		tax := read("goal-tax")
		if strings.Join(tax.GoalTimes, " | ") != "0 9 * * 1 | @at 2030-12-11 09:00" {
			t.Errorf("the goal's times are its schedules': %q", tax.GoalTimes)
		}
		if len(tax.GoalMailConditions) != 1 || tax.GoalMailConditions[0].Value != "tax@city.example" {
			t.Errorf("the goal waits for its first mail schedule's mail: %+v", tax.GoalMailConditions)
		}
		for _, words := range []string{"Track the property tax bill.", "Look for the bill (0 9 * * 1): Look for the bill in the mail.",
			"A week before (@at 2030-12-11 09:00): Remind me it is due in a week.", "When the bill comes (when the mail it waits for arrives): Read the bill.",
			"When a notice comes (when the mail it waits for arrives): Read the notice."} {
			if !strings.Contains(tax.Goal, words) {
				t.Errorf("the description keeps %q:\n%s", words, tax.Goal)
			}
		}
		if tax.GoalNextAt == nil || tax.GoalNextAt.After(time.Now().Add(2*time.Hour)) || tax.GoalWrittenBy != models.WrittenByAgent {
			t.Errorf("the goal is due at its soonest schedule's time, and is the agent's: %v %q", tax.GoalNextAt, tax.GoalWrittenBy)
		}

		morning := read("morning")
		if morning.GoalTitle != "Morning" || morning.Goal != "Tell me what is on today." || strings.Join(morning.GoalTimes, "") != "0 8 * * 1-5" ||
			morning.GoalState != models.GoalWorking || morning.GoalWrittenBy != models.WrittenByPerson || morning.GoalOriginConversationID != mainId || morning.GoalNextAt == nil {
			t.Errorf("a schedule of its own is a goal of its own: %+v", morning)
		}
		if brief := read("brief"); brief.GoalWrittenBy != models.WrittenByBuiltIn {
			t.Errorf("a brief its switch made is TeaNode's: %q", brief.GoalWrittenBy)
		}
		if once := read("once"); strings.Join(once.GoalTimes, "") != "@at 2030-01-02 15:00" || once.GoalState != models.GoalWorking {
			t.Errorf("a moment is the goal's one time: %+v", once)
		}
		if off := read("off"); off.GoalState != models.GoalPaused || off.GoalNextAt != nil {
			t.Errorf("a schedule that was off is a paused goal: %+v", off)
		}
		if pictures := read("pictures"); len(pictures.GoalTimes) != 0 || len(pictures.GoalMailConditions) != 1 || pictures.GoalNextAt != nil {
			t.Errorf("a schedule on mail is a goal waiting for mail: %+v", pictures)
		}

		weekly, err := tx.GetAgentFeedItem("card-weekly")
		if err != nil || weekly.FeedSourceKind != models.FeedSourceGoal || weekly.FeedSourceID != "goal-tax" {
			t.Errorf("a card of a goal's schedule is the goal's: %+v %v", weekly, err)
		}
		if card, err := tx.GetAgentFeedItem("card-morning"); err != nil || card.FeedSourceKind != models.FeedSourceGoal || card.FeedSourceID != "morning" {
			t.Errorf("a card of a schedule of its own is its goal's: %+v %v", card, err)
		}
		if job, _ := tx.GetAgentJob("queued-run"); job != nil {
			t.Errorf("a schedule's queued run is gone: %+v", job)
		}
	})
	if exists := rawQueryString(t, database, `SELECT COALESCE(to_regclass('agent_schedule')::text, '')`); exists != "" {
		t.Errorf("the schedule table is still there")
	}

	execMigrationSQL(t, database, migration.ReverseSQL)
	if count := rawQueryString(t, database, `SELECT count(*)::text FROM "agent_schedule" WHERE "conversation_id" = 'goal-tax'`); count != "3" {
		t.Errorf("back again, the goal's two times and its mail are three schedules: %s", count)
	}
	if state := rawQueryString(t, database, `SELECT "goal_state" FROM "agent_conversation" WHERE "id" = 'off'`); state != "waiting" {
		t.Errorf("back again, a paused goal waits: %s", state)
	}
	execMigrationSQL(t, database, migration.SQL)
}
