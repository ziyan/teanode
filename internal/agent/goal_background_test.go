package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
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
		return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{GoalTitle: "Invoices filed", GoalDescription: "File the invoices from last quarter, five in all.", OriginConversationID: world.conversation.ID})
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

// For a person whose agent tells them things in the feed, a goal that came
// to need them is a card of the feed, with a way to answer, and not a message
// in the main conversation; posted once however many sweeps find it.
func TestAGoalThatWaitsIsACardOfTheFeed(t *testing.T) {
	world, goal := startBackgroundGoal(t, []string{goalWaitRound, answerRound})
	defer world.close()
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.Agent, error) {
		return tx.UpdateAgent(world.found.ID, func(changing *models.Agent) error {
			changing.FeedDestination = models.FeedDestinationFeed
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}

	world.runGoalTurn(t, 1)
	for range 3 {
		if err := world.worker.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		world.worker.Wait()
	}
	if main := messagesOf(t, world, world.conversation.ID); len(main) != 0 {
		t.Fatalf("the main conversation hears nothing when the feed is where things are told: %+v", main)
	}
	var cards []*models.AgentFeedItem
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		all, _, err := tx.ListAgentFeedItems(world.owner.ID, nil, 50, false, time.Now())
		if err != nil {
			t.Fatalf("ListAgentFeedItems: %s", err)
		}
		for _, card := range all {
			if card.FeedSourceID == goal.ID && card.CoalesceKey == "goal:"+goal.ID+":waiting" {
				cards = append(cards, card)
			}
		}
	})
	if len(cards) != 1 || cards[0].FeedItemKind != models.FeedItemDecision || cards[0].Title != "Invoices filed" ||
		!strings.Contains(cards[0].Body, "two drafts are ready") || !cards[0].HasChoiceRole(models.FeedChoiceDiscuss) {
		t.Fatalf("the goal's call should be one card with a way to answer: %+v", cards)
	}
	if waiting, _ := readGoal(t, world, goal.ID); waiting.GoalSurfacedAt == nil {
		t.Fatalf("the goal is marked said: %+v", waiting)
	}
}

// The person drops a goal and takes it up again; a dropped goal takes no
// turns and cannot be told anything.
func TestAPersonDropsAndReopensAGoal(t *testing.T) {
	world, goal := startBackgroundGoal(t, nil)
	defer world.close()
	dropped, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalDropped)
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
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalWorking)
	})
	if err != nil || reopened.GoalState != models.GoalWorking || reopened.GoalNextAt == nil {
		t.Fatalf("reopened: %+v %v", reopened, err)
	}
	if _, activity := readGoal(t, world, goal.ID); len(activity) != 3 || activity[1].GoalActivityKind != models.GoalActivityDropped {
		t.Fatalf("the goal's log: %+v", activity)
	}
	// And a goal is not put on a conversation the person chats in.
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, world.conversation.ID, models.GoalMet)
	}); err == nil {
		t.Fatal("the main conversation was treated as a goal")
	}
}

// A goal on set times takes its turns at them: it is due at the soonest
// time, not at once; after a turn its next time is the next of them,
// whatever minutes the turn asked for; paused it takes no turn, and taken up
// again it is due at its next time.
func TestAGoalOnSetTimesRunsAtThem(t *testing.T) {
	world := startGoalWorld(t, []string{goalProgressRound}, "")
	defer world.close()
	goal, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{
			GoalTitle: "Invoices", GoalDescription: "File the day's invoices.", GoalTimes: []string{"0 8 * * *"},
		})
	})
	if err != nil {
		t.Fatalf("StartGoal: %s", err)
	}
	location := agent.Location(world.owner)
	nextEight := func(after time.Time) time.Time {
		local := after.In(location)
		at := time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, location)
		if !at.After(after) {
			at = at.AddDate(0, 0, 1)
		}
		return at
	}
	if goal.GoalNextAt == nil || !goal.GoalNextAt.Equal(nextEight(time.Now())) {
		t.Fatalf("a goal on set times is due at its time, not at once: %v", goal.GoalNextAt)
	}

	// Due now, it takes its turn, and the turn's sixty minutes are not used.
	makeDue := func() {
		if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
			return tx.UpdateAgentConversation(goal.ID, func(changing *models.AgentConversation) error {
				past := time.Now().Add(-time.Minute)
				changing.GoalNextAt = &past
				return nil
			})
		}); err != nil {
			t.Fatal(err)
		}
	}
	makeDue()
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	waitForGoalJobs(t, world, 1)
	after, _ := readGoal(t, world, goal.ID)
	if after.GoalNextAt == nil || !after.GoalNextAt.Equal(nextEight(time.Now())) || after.GoalNote != "three of five invoices filed" {
		t.Fatalf("after its turn it is due at its next time: %v %q", after.GoalNextAt, after.GoalNote)
	}

	// Paused, a time that comes takes no turn.
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalPaused)
	}); err != nil {
		t.Fatal(err)
	}
	makeDue()
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	waitForGoalJobs(t, world, 1)

	// Taken up again, it is due at its next time.
	resumed, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalWorking)
	})
	if err != nil || resumed.GoalNextAt == nil || !resumed.GoalNextAt.Equal(nextEight(time.Now())) {
		t.Fatalf("taken up again, it is due at its next time: %+v %v", resumed, err)
	}
}

// A moment given now that has passed, or that is no moment at all, is
// refused rather than dropped: dropped, it would leave a goal with no times
// that runs at once. A goal whose one moment passed while it was paused
// goes on at a pace of its own when taken up again, rather than waiting
// for a time that never comes.
func TestAGoalsMomentsAreChecked(t *testing.T) {
	world := startGoalWorld(t, []string{goalProgressRound}, "")
	defer world.close()
	for _, given := range []string{"@at 2020-01-01 09:00", "@at tomorrow 9"} {
		if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
			return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{
				GoalTitle: "Call back", GoalDescription: "Remind them to call back.", GoalTimes: []string{given},
			})
		}); err == nil {
			t.Errorf("a goal was started at %q", given)
		}
	}
	later := time.Now().Add(2 * time.Hour).In(agent.Location(world.owner)).Format("2006-01-02 15:04")
	goal, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{
			GoalTitle: "Call back", GoalDescription: "Remind them to call back.", GoalTimes: []string{"@at " + later},
		})
	})
	if err != nil {
		t.Fatalf("StartGoal: %s", err)
	}
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalPaused)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return tx.UpdateAgentConversation(goal.ID, func(changing *models.AgentConversation) error {
			changing.GoalTimes = []string{"@at 2020-01-01 09:00"}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	resumed, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalWorking)
	})
	if err != nil || resumed.GoalNextAt == nil || time.Until(*resumed.GoalNextAt) > time.Minute || len(resumed.GoalTimes) != 0 {
		t.Fatalf("taken up again with every time passed, it goes on alone from now: %+v %v", resumed, err)
	}
}

// waitForGoalJobs waits until the agent has as many goal jobs as wanted, all
// finished, and fails when it has more.
func waitForGoalJobs(t *testing.T, world *goalWorld, wanted int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var jobs []*models.AgentJob
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			jobs, _ = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: world.found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
		})
		finished := 0
		for _, job := range jobs {
			if job.Status == models.AgentJobDone {
				finished++
			}
		}
		if len(jobs) > wanted {
			t.Fatalf("%d goal turns, wanted %d: %+v", len(jobs), wanted, jobs)
		}
		if finished == wanted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d goal turns finished: %+v", finished, wanted, jobs)
		}
		time.Sleep(50 * time.Millisecond)
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

// An answer to a goal on set times still earns a turn a minute later; the
// times take over again after it.
func TestAnAnswerToAGoalOnSetTimesEarnsATurn(t *testing.T) {
	world := startGoalWorld(t, nil, "")
	defer world.close()
	goal, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{
			GoalTitle: "Look again", GoalDescription: "Look at the build.", GoalTimes: []string{"0 */3 * * *"},
		})
	})
	if err != nil {
		t.Fatalf("StartGoal: %s", err)
	}
	told, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
		return world.worker.TellGoal(tx, world.found, goal.ID, "yes, go on")
	})
	if err != nil || told.GoalNextAt == nil || told.GoalNextAt.After(time.Now().Add(2*time.Minute)) {
		t.Fatalf("an answer to a goal on set times earns a turn soon: %+v %v", told, err)
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
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalMet)
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
		return world.worker.SetGoalState(tx, world.found, world.owner, goal.ID, models.GoalWorking)
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

// The person moving the times of a built-in goal (the daily brief's) leaves
// it built in: written by the person, with times and no mail conditions, it
// would count as their own and may use Passwords, while the brief reads mail
// that strangers wrote. A goal the agent wrote still becomes the person's.
func TestAPersonEditingABuiltInGoalLeavesItBuiltIn(t *testing.T) {
	world := startGoalWorld(t, nil, "")
	defer world.close()
	start := func(title, writer string) *models.AgentConversation {
		goal, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
			return world.worker.StartGoal(tx, world.found, world.owner, &agent.GoalStart{
				GoalTitle: title, GoalDescription: "Tell me about today.", GoalTimes: []string{"30 7 * * 1-5"}, GoalWrittenBy: writer,
			})
		})
		if err != nil {
			t.Fatalf("StartGoal: %s", err)
		}
		return goal
	}
	edit := func(goal *models.AgentConversation) *models.AgentConversation {
		times := []string{"0 9 * * 1-5"}
		edited, err := inTransaction(t, world.database, func(tx db.Transaction) (*models.AgentConversation, error) {
			return world.worker.UpdateGoal(tx, world.found, world.owner, goal.ID, &agent.GoalChange{GoalTimes: &times, GoalWrittenBy: models.WrittenByPerson})
		})
		if err != nil {
			t.Fatalf("UpdateGoal: %s", err)
		}
		return edited
	}
	brief := edit(start("Daily brief", models.WrittenByBuiltIn))
	if brief.GoalWrittenBy != models.WrittenByBuiltIn || len(brief.GoalTimes) != 1 || brief.GoalTimes[0] != "0 9 * * 1-5" {
		t.Errorf("the edited brief is written by %q at %v, want built_in at the new time", brief.GoalWrittenBy, brief.GoalTimes)
	}
	if agent.IsPersonsOwnGoalForTest(brief) {
		t.Errorf("the edited brief counts as the person's own")
	}
	if theirs := edit(start("Refund", models.WrittenByAgent)); theirs.GoalWrittenBy != models.WrittenByPerson {
		t.Errorf("an agent's goal the person edited is written by %q, want person", theirs.GoalWrittenBy)
	}
}
