package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
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

		if err := tx.MarkAgentIdeasShown(agent.ID, []string{kept.ID}, now); err != nil {
			t.Fatal(err)
		}
		listed, err := tx.ListAgentIdeas(agent.ID, []models.AgentIdeaStatus{models.IdeaDone}, []models.AgentIdeaKind{models.IdeaCatalog})
		if err != nil || len(listed) != 1 || listed[0].ShownAt == nil {
			t.Fatalf("listed by status and kind, shown: %+v %v", listed, err)
		}
	})
}
