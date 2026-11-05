package agent_test

import (
	"context"
	"encoding/json"
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

// startBackgroundGoal is the goal world with no goal of its own, and a
// goal started through StartGoal.
func startBackgroundGoal(t *testing.T, script []string) (*goalWorld, *models.AgentConversation) {
	t.Helper()
	world := startGoalWorld(t, script, "")
	goal, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.StartGoal(tx, world.found, "Invoices filed", "File the invoices from last quarter, five in all.", world.conversation.ID)
	})
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

	told, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.TellGoal(tx, world.found, goal.ID, "send both")
	})
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
	dropped, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, goal.ID, models.GoalDropped)
	})
	if err != nil || dropped.GoalState != models.GoalDropped || dropped.GoalNextAt != nil {
		t.Fatalf("dropped: %+v %v", dropped, err)
	}
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.TellGoal(tx, world.found, goal.ID, "anything")
	}); err == nil {
		t.Fatal("a dropped goal was told something")
	}
	reopened, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, goal.ID, models.GoalWorking)
	})
	if err != nil || reopened.GoalState != models.GoalWorking || reopened.GoalNextAt == nil {
		t.Fatalf("reopened: %+v %v", reopened, err)
	}
	if _, activity := readGoal(t, world, goal.ID); len(activity) != 3 || activity[1].GoalActivityKind != models.GoalActivityDropped {
		t.Fatalf("the goal's log: %+v", activity)
	}
	// And a goal is not put on a conversation the person chats in.
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.conversation.ID, models.GoalMet)
	}); err == nil {
		t.Fatal("the main conversation was treated as a goal")
	}
}

// A goal with a schedule runs on it: moving one to the goal stops the
// goal's own turns, a turn it was owed is not taken, and a goal whose
// schedule is switched off is given a turn again.
func TestAGoalWithAScheduleRunsOnIt(t *testing.T) {
	world, goal := startBackgroundGoal(t, nil)
	defer world.close()
	var schedule *models.AgentSchedule
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		next := time.Now().Add(time.Hour)
		var err error
		if schedule, err = tx.CreateAgentSchedule(&models.AgentSchedule{
			AgentID: world.found.ID, Name: "Save the photos", Cron: "0 */3 * * *", Prompt: "run the sync",
			Deliver: models.AgentDeliverMail, ConversationID: world.conversation.ID, Enabled: true, NextRunAt: &next,
		}); err != nil {
			t.Fatalf("CreateAgentSchedule: %s", err)
		}
	})

	moved, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentSchedule, error) {
		return world.worker.MoveScheduleToGoal(tx, world.found, schedule.ID, goal.ID)
	})
	if err != nil || moved.ConversationID != goal.ID || moved.Deliver != models.AgentDeliverDrawer {
		t.Fatalf("moved: %+v %v", moved, err)
	}
	after, activity := readGoal(t, world, goal.ID)
	if after.GoalNextAt != nil || activity[0].ActivityHeadline != `Runs on the schedule "Save the photos"` {
		t.Fatalf("a goal on a schedule books no turn of its own: %+v %+v", after, activity[0])
	}

	// A tick finds it without a turn but with a schedule: nothing queued.
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		jobs, _ = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: world.found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
	})
	if len(jobs) != 0 {
		t.Fatalf("a goal on a schedule took a turn of its own: %+v", jobs)
	}

	// Switched off, the schedule no longer keeps it: it gets a turn.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgentSchedule(schedule.ID, func(changing *models.AgentSchedule) error {
			changing.Enabled = false
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentSchedule: %s", err)
		}
	})
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if woken, _ := readGoal(t, world, goal.ID); woken.GoalNextAt == nil {
		t.Fatalf("a goal whose schedule is off has no turn: %+v", woken)
	}
}

// inTransaction runs one call in a transaction of its own and commits it,
// as a GraphQL request would.
func inTransaction[T any](t *testing.T, database db.Database, run func(tx db.Transaction) (T, error)) (T, error) {
	t.Helper()
	var result T
	err := database.TransactionContext(context.Background(), func(tx db.Transaction) (err error) {
		result, err = run(tx)
		return err
	})
	return result, err
}

// The answers and edges of a goal on a schedule: an answer still earns a
// turn, a mailed schedule is no clock for the goal, and the schedule of a
// goal that is done does not run.
func TestAGoalsScheduleEdges(t *testing.T) {
	world, goal := startBackgroundGoal(t, nil)
	defer world.close()
	addSchedule := func(deliver string, nextRunAt time.Time) *models.AgentSchedule {
		t.Helper()
		var schedule *models.AgentSchedule
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if schedule, err = tx.CreateAgentSchedule(&models.AgentSchedule{
				AgentID: world.found.ID, Name: "Look again", Cron: "0 */3 * * *", Prompt: "look",
				Deliver: deliver, ConversationID: goal.ID, Enabled: true, NextRunAt: &nextRunAt,
			}); err != nil {
				t.Fatalf("CreateAgentSchedule: %s", err)
			}
		})
		return schedule
	}

	// A mailed schedule is no clock: the goal keeps its own turns.
	mailed := addSchedule(models.AgentDeliverMail, time.Now().Add(time.Hour))
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return tx.UpdateAgentConversation(goal.ID, func(changing *models.AgentConversation) error {
			changing.GoalNextAt = nil
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if woken, _ := readGoal(t, world, goal.ID); woken.GoalNextAt == nil && woken.GoalState == models.GoalWorking {
		t.Fatalf("a goal whose only schedule mails was left without a turn: %+v", woken)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.DeleteAgentSchedule(mailed.ID); err != nil {
			t.Fatal(err)
		}
	})

	// With a schedule it answers in, an answer still earns a turn.
	addSchedule(models.AgentDeliverDrawer, time.Now().Add(time.Hour))
	told, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.TellGoal(tx, world.found, goal.ID, "yes, go on")
	})
	if err != nil || told.GoalNextAt == nil {
		t.Fatalf("an answer to a goal on a schedule earns a turn: %+v %v", told, err)
	}

	// Done, its schedule does not run when it comes due.
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, goal.ID, models.GoalMet)
	}); err != nil {
		t.Fatal(err)
	}
	before := len(messagesOf(t, world, goal.ID))
	addSchedule(models.AgentDeliverDrawer, time.Now().Add(-time.Minute))
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if after := len(messagesOf(t, world, goal.ID)); after != before {
		t.Fatalf("the schedule of a goal that is done ran: %d messages, were %d", after, before)
	}
}

// A goal a turn finished with nobody there is said in the main conversation
// once, with what came of it; another sweep says nothing more. The person
// hears it there, rather than having to open the Goals tab to find out.
func TestABackgroundGoalFinishedAloneIsSaidOnce(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{goalMetRound, answerRound})
	defer world.close()

	world.runGoalTurn(t, 1)
	for range 2 {
		if err := world.worker.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		world.worker.Wait()
	}
	main := messagesOf(t, world, world.conversation.ID)
	if len(main) != 2 || !strings.HasPrefix(main[0].Content, models.GoalDoneMarker+" "+goal.ID+" Invoices filed") || main[1].Role != "assistant" ||
		!strings.Contains(main[1].Content, "Invoices filed") || !strings.Contains(main[1].Content, "all five are filed") {
		t.Fatalf("the main conversation should hear once how the goal ended: %+v", main)
	}
	if !models.IsOwnTurn(main[0].Content) {
		t.Fatalf("what the person hears must not read as their own words: %q", main[0].Content)
	}
	if done, _ := readGoal(t, world, goal.ID); done.GoalState != models.GoalMet || done.GoalSurfacedAt == nil {
		t.Fatalf("the goal is met, said: %+v", done)
	}
}

// The person marking a goal done needs no telling.
func TestAGoalThePersonMarkedDoneIsNotSaid(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{answerRound})
	defer world.close()

	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, goal.ID, models.GoalMet)
	}); err != nil {
		t.Fatalf("SetGoalState: %s", err)
	}
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if main := messagesOf(t, world, world.conversation.ID); len(main) != 0 {
		t.Fatalf("the person who said it is done was told it is done: %+v", main)
	}
}

// A goal taken up again and finished alone is news again, though the first
// ending was said.
func TestAGoalReopenedAndFinishedAloneIsSaidAgain(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{goalMetRound, answerRound, goalMetRound, answerRound})
	defer world.close()

	world.runGoalTurn(t, 1)
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, goal.ID, models.GoalWorking)
	}); err != nil {
		t.Fatalf("SetGoalState: %s", err)
	}
	world.runGoalTurn(t, 2)
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	if main := messagesOf(t, world, world.conversation.ID); len(main) != 4 {
		t.Fatalf("both endings should be said: %+v", main)
	}
}

// A goal finished before this was said, or long enough ago to be old news,
// stays on the Goals tab: a first start must not announce the past.
func TestAGoalMetLongAgoIsNotSaid(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{answerRound})
	defer world.close()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgentConversation(goal.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState, conversation.GoalNextAt, conversation.GoalSurfacedAt = models.GoalMet, nil, nil
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}
		found, err := tx.ListAgentGoalsToSurface(time.Now().Add(time.Hour), 10)
		if err != nil || len(found) != 0 {
			t.Fatalf("a goal met before the limit was listed: %+v %v", found, err)
		}
		found, err = tx.ListAgentGoalsToSurface(time.Now().Add(-time.Hour), 10)
		if err != nil || len(found) != 1 || found[0].ID != goal.ID {
			t.Fatalf("a goal met within the limit was not listed: %+v %v", found, err)
		}
	})
}

// A real turn is sent what is going on for the person, after the history,
// read once however many rounds the turn takes.
func TestATurnIsToldWhereThePersonIsAndWhatIsOpen(t *testing.T) {
	world, _ := startBackgroundGoal(t, []string{goalProgressRound, answerRound})
	defer world.close()

	arrivedAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	world.operations.answers = map[string]string{
		"GetLocation": `{"GetLocation":{"latestPoint":null,"pause":null,"openVisit":{"arrivedAt":"` + arrivedAt + `","place":{"placeName":"","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":40,"longitudeDegrees":-75,"radiusMeters":100,"osmName":"Pine Hollow Cabins","addressText":"12 Ridge Road"}}}}`,
	}
	dueOn := time.Now().AddDate(0, 0, 3).Format(time.DateOnly)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, _, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
			AgentID: world.found.ID, FollowUpKind: models.AgentFollowUpBill, FollowUpKey: "water", FollowUpTitle: "Water bill",
			Amount: "84.00", CurrencyCode: "USD", DueOn: dueOn, FollowUpState: models.AgentFollowUpOpen, SourceItemIDs: []string{},
		}); err != nil {
			t.Fatalf("UpsertAgentFollowUp: %s", err)
		}
	})

	world.runGoalTurn(t, 1)
	if len(*world.requests) < 2 {
		t.Fatalf("the turn should have taken two rounds: %d", len(*world.requests))
	}
	for round, request := range *world.requests {
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("Marshal: %s", err)
		}
		body := string(raw)
		if !strings.Contains(body, "right_now") || !strings.Contains(body, "At Pine Hollow Cabins (12 Ridge Road) since") || !strings.Contains(body, "bill: Water bill, 84.00 USD, due") {
			t.Fatalf("round %d was not told what is going on: %s", round, body)
		}
	}
	reads := 0
	for _, document := range world.operations.documents {
		if strings.Contains(document, "GetLocation") {
			reads++
		}
	}
	if reads != 1 {
		t.Fatalf("the location should be read once a turn, not %d times", reads)
	}
}
