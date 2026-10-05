package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A goal kept in the background: its turns run in a conversation of its
// own, what happened on it is its activity, and the person's main
// conversation hears from it only when it needs them.

// goalProgressRound is a turn on a goal that did something worth logging.
const goalProgressRound = `{"id":"g5","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_5","function":{"name":"goal","arguments":"{\"action\":\"note\",\"text\":\"three of five invoices filed\",\"activity\":\"Filed the March invoices\",\"minutes\":60}"}}]},"finish_reason":"tool_calls"}]}
{"id":"g5","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

// startBackgroundGoal is the goal world with its main conversation left
// without a goal, and a goal started in the background.
func startBackgroundGoal(t *testing.T, script []string) (*goalWorld, *models.AgentConversation) {
	t.Helper()
	world := startGoalWorld(t, script, "")
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgentConversation(world.conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState, conversation.GoalNextAt = "", nil
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}
	})
	goal, err := world.worker.StartGoal(context.Background(), world.found, "Invoices filed", "File the invoices from last quarter, five in all.", world.conversation.ID)
	if err != nil {
		t.Fatalf("StartGoal: %s", err)
	}
	return world, goal
}

func readGoal(t *testing.T, world *goalWorld, goalId string) (*models.AgentConversation, []*models.AgentGoalActivity) {
	t.Helper()
	var goal *models.AgentConversation
	var activity []*models.AgentGoalActivity
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if goal, err = tx.GetAgentConversation(goalId); err != nil {
			t.Fatalf("GetAgentConversation: %s", err)
		}
		if activity, err = tx.ListAgentGoalActivity(world.found.ID, goalId, 50); err != nil {
			t.Fatalf("ListAgentGoalActivity: %s", err)
		}
	})
	return goal, activity
}

func messagesOf(t *testing.T, world *goalWorld, conversationId string) []*models.AgentMessage {
	t.Helper()
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		messages, _ = tx.ListAgentMessages(conversationId, nil)
	})
	return messages
}

// A started goal has a conversation of its own and a first row in its log;
// its turn runs there, a note that says something happened adds a row, and
// the main conversation hears nothing.
func TestABackgroundGoalRunsOutOfTheMainConversation(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{goalProgressRound, answerRound})
	defer world.close()
	if goal.Kind != models.AgentConversationGoal || goal.GoalTitle != "Invoices filed" || goal.GoalState != models.GoalWorking {
		t.Fatalf("the goal is %+v", goal)
	}

	world.runGoalTurn(t, 1)
	after, activity := readGoal(t, world, goal.ID)
	if after.GoalNote != "three of five invoices filed" || after.GoalState != models.GoalWorking {
		t.Fatalf("the goal after its turn: %+v", after)
	}
	if len(activity) != 2 || activity[0].GoalActivityKind != models.GoalActivityProgress || activity[0].ActivityHeadline != "Filed the March invoices" ||
		activity[1].GoalActivityKind != models.GoalActivityStarted {
		t.Fatalf("the goal's log: %+v", activity)
	}
	if turn := messagesOf(t, world, goal.ID); len(turn) == 0 || !strings.HasPrefix(turn[0].Content, models.GoalCheckInMarker) || !strings.Contains(turn[0].Content, "do not read this conversation") {
		t.Fatalf("the turn ran in the goal's own conversation: %+v", turn)
	}
	if main := messagesOf(t, world, world.conversation.ID); len(main) != 0 {
		t.Fatalf("the main conversation heard from a goal that did not need the person: %+v", main)
	}
}

// A goal that comes to need the person is said in the main conversation
// once, as the marker line and what it needs; another sweep says nothing
// more. The person's answer, passed on, puts it back to work.
func TestABackgroundGoalThatWaitsIsSaidOnceAndAnswered(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{goalWaitRound, answerRound})
	defer world.close()

	world.runGoalTurn(t, 1)
	// The sweep that says it runs on the worker's tick.
	for range 2 {
		if err := world.worker.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		world.worker.Wait()
	}
	main := messagesOf(t, world, world.conversation.ID)
	if len(main) != 2 || !strings.HasPrefix(main[0].Content, models.GoalNeedsYouMarker+" "+goal.ID) || main[1].Role != "assistant" ||
		!strings.Contains(main[1].Content, "Invoices filed") || !strings.Contains(main[1].Content, "two drafts are ready") {
		t.Fatalf("the main conversation should hear once what the goal needs: %+v", main)
	}
	waiting, activity := readGoal(t, world, goal.ID)
	if waiting.GoalState != models.GoalWaiting || waiting.GoalSurfacedAt == nil || activity[0].GoalActivityKind != models.GoalActivityWaiting {
		t.Fatalf("the goal waits, said: %+v %+v", waiting, activity)
	}

	told, err := world.worker.TellGoal(context.Background(), world.found, goal.ID, "send both")
	if err != nil {
		t.Fatalf("TellGoal: %s", err)
	}
	if told.GoalState != models.GoalWorking || told.GoalNextAt == nil || time.Until(*told.GoalNextAt) > 2*time.Minute {
		t.Fatalf("a goal told goes back to work in a minute: %+v", told)
	}
	turn := messagesOf(t, world, goal.ID)
	if last := turn[len(turn)-1]; last.Role != "user" || last.Content != models.GoalRelayMarker+" send both" {
		t.Fatalf("the person's words should reach the goal's conversation: %+v", last)
	}
	if _, activity := readGoal(t, world, goal.ID); activity[0].GoalActivityKind != models.GoalActivityResumed || activity[0].ActivityDetail != "send both" {
		t.Fatalf("the answer should be in the goal's log: %+v", activity[0])
	}
}

// The person drops a goal and takes it up again; a dropped goal takes no
// turns and cannot be told anything.
func TestAPersonDropsAndReopensAGoal(t *testing.T) {
	world, goal := startBackgroundGoal(t, nil)
	defer world.close()
	dropped, err := world.worker.SetGoalState(context.Background(), world.found, goal.ID, models.GoalDropped)
	if err != nil || dropped.GoalState != models.GoalDropped || dropped.GoalNextAt != nil {
		t.Fatalf("dropped: %+v %v", dropped, err)
	}
	if _, err := world.worker.TellGoal(context.Background(), world.found, goal.ID, "anything"); err == nil {
		t.Fatal("a dropped goal was told something")
	}
	reopened, err := world.worker.SetGoalState(context.Background(), world.found, goal.ID, models.GoalWorking)
	if err != nil || reopened.GoalState != models.GoalWorking || reopened.GoalNextAt == nil {
		t.Fatalf("reopened: %+v %v", reopened, err)
	}
	if _, activity := readGoal(t, world, goal.ID); len(activity) != 3 || activity[1].GoalActivityKind != models.GoalActivityDropped {
		t.Fatalf("the goal's log: %+v", activity)
	}
	// And a goal is not put on a conversation the person chats in.
	if _, err := world.worker.SetGoalState(context.Background(), world.found, world.conversation.ID, models.GoalMet); err == nil {
		t.Fatal("the main conversation was treated as a goal")
	}
}
