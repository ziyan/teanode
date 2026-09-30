package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
	"github.com/ziyan/teanode/internal/models"
)

// An idea keeps what became of it however often the catalog is read, and
// is done when the goal of the conversation carrying it out is met,
// whoever says so: the rule is kept where every goal is written, so no
// caller can forget it.
func TestAnIdeaKeepsItsStatusAndFinishesWithItsGoal(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		offer := &models.AgentIdea{
			AgentID: agent.ID, IdeaKey: "bread", IdeaKind: models.IdeaCatalog, IdeaCategory: "home", Emoji: "🏠",
			Headline: "Tell me the flour. I'll plan the bake.", NeededToolNames: []string{"calendar"},
		}
		kept, err := tx.UpsertAgentIdea(offer)
		if err != nil || kept.IdeaStatus != models.IdeaOpen {
			t.Fatalf("kept open: %+v %v", kept, err)
		}
		if _, err := tx.UpsertAgentIdea(&models.AgentIdea{AgentID: agent.ID, IdeaKey: "bread", IdeaKind: models.IdeaCatalog, IdeaCategory: "rumor", Headline: "x"}); err == nil {
			t.Fatal("an idea in no category is refused")
		}

		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if _, err := tx.UpdateAgentIdea(agent.ID, kept.ID, func(idea *models.AgentIdea) error {
			idea.IdeaStatus, idea.StartedAt, idea.StartedConversationID = models.IdeaStarted, &now, conversation.ID
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		offer.Headline = "Tell me the flour and the day. I'll plan the bake."
		refreshed, err := tx.UpsertAgentIdea(offer)
		if err != nil || refreshed.IdeaStatus != models.IdeaStarted || refreshed.Headline != offer.Headline {
			t.Fatalf("the words refresh, the status stays: %+v %v", refreshed, err)
		}

		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changing *models.AgentConversation) error {
			changing.Goal, changing.GoalState = "bake on Saturday", models.GoalWorking
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if still, _ := tx.GetAgentIdea(agent.ID, kept.ID); still.IdeaStatus != models.IdeaStarted {
			t.Fatalf("a goal set is not a goal met: %+v", still)
		}
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changing *models.AgentConversation) error {
			changing.GoalState = models.GoalMet
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		done, _ := tx.GetAgentIdea(agent.ID, kept.ID)
		if done.IdeaStatus != models.IdeaDone || done.ClosedAt == nil {
			t.Fatalf("done with its goal: %+v", done)
		}

		// "Met" taken back: the goal set again, and the idea is started again.
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changing *models.AgentConversation) error {
			changing.GoalState = models.GoalWorking
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if reopened, _ := tx.GetAgentIdea(agent.ID, kept.ID); reopened.IdeaStatus != models.IdeaStarted || reopened.ClosedAt != nil {
			t.Fatalf("started again with its goal: %+v", reopened)
		}
		inProgress, err := tx.ListAgentGoalsInProgress(agent.ID)
		if err != nil || len(inProgress) != 1 || inProgress[0].ID != conversation.ID {
			t.Fatalf("the goal is in progress again: %+v %v", inProgress, err)
		}
		archived := time.Now()
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changing *models.AgentConversation) error {
			changing.ArchivedAt = &archived
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if inProgress, _ := tx.ListAgentGoalsInProgress(agent.ID); len(inProgress) != 1 {
			t.Fatalf("a goal in an archived conversation still runs, so it is still listed: %+v", inProgress)
		}
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changing *models.AgentConversation) error {
			changing.GoalState = models.GoalMet
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err := tx.MarkAgentIdeasShown(agent.ID, []string{kept.ID}, now); err != nil {
			t.Fatal(err)
		}
		listed, err := tx.ListAgentIdeas(agent.ID, []models.AgentIdeaStatus{models.IdeaDone}, []models.AgentIdeaKind{models.IdeaCatalog})
		if err != nil || len(listed) != 1 || listed[0].ShownAt == nil {
			t.Fatalf("listed by status and kind, shown: %+v %v", listed, err)
		}
	})
}

// Deleting the conversation an idea was started in puts the idea back on
// offer, since nothing carries it out any more; one already done stays
// done and only loses the link to a conversation that is gone.
func TestDeletingItsConversationPutsAStartedIdeaBackOnOffer(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		startIn := func(ideaKey string, status models.AgentIdeaStatus) *models.AgentIdea {
			kept, err := tx.UpsertAgentIdea(&models.AgentIdea{
				AgentID: agent.ID, IdeaKey: ideaKey, IdeaKind: models.IdeaCatalog, IdeaCategory: "home", Emoji: "🏠",
				Headline: "Tell me the plants. I'll plan the watering.",
			})
			if err != nil {
				t.Fatal(err)
			}
			started, err := tx.UpdateAgentIdea(agent.ID, kept.ID, func(idea *models.AgentIdea) error {
				idea.IdeaStatus, idea.StartedAt, idea.StartedConversationID = status, &now, conversation.ID
				if status == models.IdeaDone {
					idea.ClosedAt = &now
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			return started
		}
		started := startIn("watering", models.IdeaStarted)
		done := startIn("pruning", models.IdeaDone)

		if err := tx.DeleteAgentConversation(conversation.ID); err != nil {
			t.Fatal(err)
		}
		reopened, _ := tx.GetAgentIdea(agent.ID, started.ID)
		if reopened.IdeaStatus != models.IdeaOpen || reopened.StartedConversationID != "" || reopened.StartedAt != nil {
			t.Fatalf("on offer again, with no conversation: %+v", reopened)
		}
		listed, err := tx.ListAgentIdeas(agent.ID, []models.AgentIdeaStatus{models.IdeaOpen}, nil)
		if err != nil || len(listed) != 1 || listed[0].ID != started.ID {
			t.Fatalf("listed among the open ideas: %+v %v", listed, err)
		}
		finished, _ := tx.GetAgentIdea(agent.ID, done.ID)
		if finished.IdeaStatus != models.IdeaDone || finished.StartedConversationID != "" || finished.ClosedAt == nil {
			t.Fatalf("still done, without the link: %+v", finished)
		}
	})
}

// Ideas whose conversation was deleted before deleting one cleared the link
// are put right by a migration, by the same rule: started goes back on
// offer, done stays done, and a link to a conversation that is still there
// is left alone.
func TestTheMigrationClearsLinksToDeletedConversations(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	var agentId, startedId, doneId, livingId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agentId = agent.ID
		living, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		keep := func(ideaKey string, status models.AgentIdeaStatus, conversationId string) string {
			kept, err := tx.UpsertAgentIdea(&models.AgentIdea{
				AgentID: agent.ID, IdeaKey: ideaKey, IdeaKind: models.IdeaCatalog, IdeaCategory: "home", Emoji: "🏠",
				Headline: "Tell me the plants. I'll plan the watering.",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.UpdateAgentIdea(agent.ID, kept.ID, func(idea *models.AgentIdea) error {
				idea.IdeaStatus, idea.StartedAt, idea.StartedConversationID = status, &now, conversationId
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return kept.ID
		}
		startedId = keep("watering", models.IdeaStarted, "gone-conversation")
		doneId = keep("pruning", models.IdeaDone, "gone-conversation")
		livingId = keep("repotting", models.IdeaStarted, living.ID)
	})
	isFound := false
	for _, migration := range migrations.Migrations() {
		if migration.ID == "0128_agent_idea_deleted_conversation" {
			dbtest.Exec(t, database, migration.SQL)
			isFound = true
		}
	}
	if !isFound {
		t.Fatal("the migration is missing")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if reopened, _ := tx.GetAgentIdea(agentId, startedId); reopened.IdeaStatus != models.IdeaOpen || reopened.StartedConversationID != "" {
			t.Fatalf("on offer again: %+v", reopened)
		}
		if finished, _ := tx.GetAgentIdea(agentId, doneId); finished.IdeaStatus != models.IdeaDone || finished.StartedConversationID != "" {
			t.Fatalf("still done, without the link: %+v", finished)
		}
		if living, _ := tx.GetAgentIdea(agentId, livingId); living.IdeaStatus != models.IdeaStarted || living.StartedConversationID == "" {
			t.Fatalf("a living conversation keeps its idea: %+v", living)
		}
	})
}
