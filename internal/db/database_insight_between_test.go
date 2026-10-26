package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The messages said with the agent in a day are counted by conversation, the
// way the journal shows them: only the person's own conversations, only what
// was said (not a tool's result), and only inside the stretch asked for.
func TestAgentConversationActivityBetweenCountsWhatWasSaidInTheStretch(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var agentId, mainId, namedId, runId, firstMainMessageId string
	messageTimes := map[string]time.Time{}
	zone := time.FixedZone("test", -5*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "activity-ada")
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agentId = agent.ID
		make := func(kind models.AgentConversationKind, title string) string {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: kind, Title: title, LastAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			return conversation.ID
		}
		mainId, namedId, runId = make(models.AgentConversationMain, ""), make(models.AgentConversationNamed, "Trip plans"), make(models.AgentConversationRun, "A run")
		say := func(conversationId, role, content string, when time.Time) {
			message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversationId, Role: role, Content: content})
			if err != nil {
				t.Fatal(err)
			}
			messageTimes[message.ID] = when
			if content == "Good morning" {
				firstMainMessageId = message.ID
			}
		}
		say(mainId, "user", "Good morning", at(22, 8, 5))
		say(mainId, "assistant", "Good morning, Ada.", at(22, 8, 6))
		say(mainId, "tool", "{\"result\":1}", at(22, 8, 6))
		say(mainId, "assistant", "", at(22, 8, 6))
		say(mainId, "user", "Late one", at(22, 23, 40))
		say(mainId, "user", "Next day", at(23, 0, 10))
		say(namedId, "user", "Where should we stay?", at(22, 13, 0))
		say(runId, "assistant", "A run says things nobody read.", at(22, 9, 0))
		say(mainId, "user", "Day before", at(21, 23, 59))
	})
	for messageId, when := range messageTimes {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_message" SET "created_at" = '%s' WHERE "id" = '%s'`, when.UTC().Format(time.RFC3339), messageId))
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		activity, err := tx.ListAgentConversationActivityBetween(agentId, at(22, 0, 0), at(23, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		if len(activity) != 2 {
			t.Fatalf("two conversations had something said: %+v", activity)
		}
		if activity[0].ConversationID != mainId || activity[0].MessageCount != 3 {
			t.Fatalf("the main conversation had three things said, not a tool result or a bare call: %+v", activity[0])
		}
		if activity[0].FirstMessageID != firstMainMessageId {
			t.Fatalf("the first message of the day opens the conversation: %q, want %q", activity[0].FirstMessageID, firstMainMessageId)
		}
		if !activity[0].FirstAt.Equal(at(22, 8, 5)) || !activity[0].LastAt.Equal(at(22, 23, 40)) {
			t.Fatalf("first and last times of the day: %+v", activity[0])
		}
		if activity[1].ConversationID != namedId || activity[1].Title != "Trip plans" || activity[1].MessageCount != 1 {
			t.Fatalf("the named conversation: %+v", activity[1])
		}
		for _, found := range activity {
			if found.ConversationID == runId {
				t.Fatal("a run's transcript is not something the person said")
			}
		}
		if _, err := tx.ListAgentConversationActivityBetween(agentId, at(23, 0, 0), at(22, 0, 0)); err == nil {
			t.Fatal("a stretch that ends before it starts is refused")
		}
	})
}
