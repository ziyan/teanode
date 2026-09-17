package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A goal on a conversation makes the agent take turns of its own in it.
// What each turn decides is the goal tool's last call, and these are the
// four things it can leave behind: a note with a time, a wait for the
// person, the word that it is met, and nothing at all.

// The rounds of the fake model: the goal tool, then words.
const (
	goalNoteRound = `{"id":"g1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"goal","arguments":"{\"action\":\"note\",\"text\":\"two of five filed\",\"minutes\":45}"}}]},"finish_reason":"tool_calls"}]}
{"id":"g1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

	goalWaitRound = `{"id":"g2","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","function":{"name":"goal","arguments":"{\"action\":\"wait\",\"text\":\"two drafts are ready; say send or edit\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"g2","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

	goalMetRound = `{"id":"g3","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_3","function":{"name":"goal","arguments":"{\"action\":\"met\",\"text\":\"all five are filed\"}"}}]},"finish_reason":"tool_calls"}]}
{"id":"g3","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

	goalSilentRound = `{"id":"g4","model":"m","choices":[{"delta":{"content":"Nothing to do yet."},"finish_reason":"stop"}]}
{"id":"g4","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5}}`
)

// goalWorld is a person with an agent, a mailbox to send from, and a
// conversation carrying a goal that is already due.
type goalWorld struct {
	database     db.Database
	worker       *agent.Agent
	owner        *models.User
	found        *models.Agent
	conversation *models.AgentConversation
	sender       *fakeMailer
	requests     *[]map[string]any
	close        func()
}

func startGoalWorld(t *testing.T, script []string, goal string) *goalWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	model, requests := fakeModel(t, script)

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	// The night is not what this is about, and whether one is due depends
	// on the wall clock; left on it would answer the canned script.
	off := false
	configuration.Agent.Features.Dreaming = &off
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	sender := &fakeMailer{}
	worker.SetMailer(sender)
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	world := &goalWorld{database: database, worker: worker, sender: sender, requests: requests}
	world.close = func() {
		worker.Stop()
		model.Close()
		closeDatabase()
	}
	due := time.Now().Add(-time.Minute)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if world.owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example", Email: "alice@example.net", Timezone: "Europe/Berlin", TimezoneMode: "fixed"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if world.found, err = tx.CreateAgent(&models.Agent{UserID: world.owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		// A granted mailbox with an address, which is what the agent
		// sends the person's notice from.
		domain, err := tx.CreateDomain(&models.Domain{Domain: "example.com"})
		if err != nil {
			t.Fatalf("CreateDomain: %s", err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: world.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if _, err := tx.CreateAlias(&models.Alias{DomainID: domain.ID, Pattern: "alice", Kind: models.AliasKindMailbox, MailboxID: mailbox.ID}); err != nil {
			t.Fatalf("CreateAlias: %s", err)
		}
		if world.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: world.found.ID, Kind: models.AgentConversationMain, LastAt: time.Now(),
			Goal: goal, GoalState: models.GoalWorking, GoalNextAt: &due,
		}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	return world
}

// runGoalTurn ticks the worker and waits for the goal job to finish.
func (self *goalWorld) runGoalTurn(t *testing.T, jobs int) {
	t.Helper()
	if err := self.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	self.worker.Wait()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var done int
		dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
			found, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: self.found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}, Statuses: []models.AgentJobStatus{models.AgentJobDone}}, nil)
			done = len(found)
		})
		if done >= jobs || time.Now().After(deadline) {
			if done < jobs {
				t.Fatalf("the goal job should have finished: %d of %d", done, jobs)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (self *goalWorld) read(t *testing.T) *models.AgentConversation {
	t.Helper()
	var after *models.AgentConversation
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if after, err = tx.GetAgentConversation(self.conversation.ID); err != nil {
			t.Fatalf("GetAgentConversation: %s", err)
		}
	})
	return after
}

// A goal that is due takes a turn of the agent's own in the person's own
// conversation: the check-in arrives marked as what it is, the note the
// turn left is on the conversation, and the next turn is when the tool
// asked for, within the bounds.
func TestGoalTurnNotesWhereItIsAndSchedulesTheNext(t *testing.T) {
	world := startGoalWorld(t, []string{goalNoteRound, answerRound}, "file the invoices from last quarter")
	defer world.close()

	before := time.Now()
	world.runGoalTurn(t, 1)
	after := world.read(t)
	if after.GoalState != models.GoalWorking {
		t.Fatalf("a note leaves the goal working: %+v", after)
	}
	if after.GoalNote != "two of five filed" {
		t.Fatalf("the note should be the tool's text: %q", after.GoalNote)
	}
	if after.GoalNextAt == nil {
		t.Fatal("a note schedules the next turn")
	}
	if wait := after.GoalNextAt.Sub(before); wait < 40*time.Minute || wait > 50*time.Minute {
		t.Fatalf("the next turn should be the forty-five minutes it asked for, got %s", wait)
	}

	// The check-in is a turn of the conversation, and it says so: the
	// dashboard tells one from the person's own words by the marker.
	var messages []*models.AgentMessage
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		messages, _ = tx.ListAgentMessages(world.conversation.ID, nil)
	})
	if len(messages) == 0 || messages[0].Role != "user" || !strings.HasPrefix(messages[0].Content, models.GoalCheckInMarker) {
		t.Fatalf("the check-in should open the turn, marked: %+v", messages)
	}
	if !strings.Contains(messages[0].Content, "file the invoices from last quarter") {
		t.Fatalf("the check-in should carry the goal: %q", messages[0].Content)
	}
	// And the prompt the model was given says what the goal is and where
	// it stands, so a turn that lost the thread can find it again.
	system := (*world.requests)[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "file the invoices from last quarter") || !strings.Contains(system, "working") {
		t.Fatalf("the prompt should carry the goal and its state: %s", system)
	}
}

// A turn that needs the person stops the turns, tells them by mail, and
// their next turn in the conversation starts it again.
func TestGoalThatWaitsResumesOnThePersonsTurn(t *testing.T) {
	world := startGoalWorld(t, []string{goalWaitRound, answerRound}, "draft a reply to the newest mail and wait for me to say send")
	defer world.close()

	world.runGoalTurn(t, 1)
	waiting := world.read(t)
	if waiting.GoalState != models.GoalWaiting {
		t.Fatalf("the goal should be waiting for the person: %+v", waiting)
	}
	if waiting.GoalNextAt != nil {
		t.Fatalf("nothing is scheduled while it waits: %s", waiting.GoalNextAt)
	}
	if waiting.GoalNote != "two drafts are ready; say send or edit" {
		t.Fatalf("the note says what it needs: %q", waiting.GoalNote)
	}
	// Told, because the drawer may well be shut.
	world.sender.mutex.Lock()
	sent := len(world.sender.sent)
	var subject, body string
	if sent > 0 {
		subject, body = world.sender.sent[0].Subject, world.sender.sent[0].Text
	}
	world.sender.mutex.Unlock()
	if sent != 1 || !strings.HasPrefix(subject, "Goal: ") || !strings.Contains(body, "two drafts are ready") {
		t.Fatalf("a waiting goal mails the person: %d %q %q", sent, subject, body)
	}
	// A second tick queues nothing: a goal that waits is not due.
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		jobs, _ = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: world.found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
	})
	if len(jobs) != 1 {
		t.Fatalf("a waiting goal is not queued again: %+v", jobs)
	}

	// The person answers, and the goal goes back to work a minute later.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	run, err := world.worker.Ask(&agent.AskSettings{Agent: world.found, Owner: world.owner, Operations: operations, Conversation: world.conversation, Message: "send both", Surface: "drawer"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collect(run)
	resumed := world.read(t)
	if resumed.GoalState != models.GoalWorking || resumed.GoalNextAt == nil {
		t.Fatalf("the person's turn should start the goal again: %+v", resumed)
	}
	if wait := time.Until(*resumed.GoalNextAt); wait <= 0 || wait > 2*time.Minute {
		t.Fatalf("it should go back to work a minute from now, got %s", wait)
	}
}

// A turn that never calls the tool said nothing about when to look again,
// so the next one is twice as far off as the last.
func TestGoalTurnThatSaysNothingWaitsTwiceAsLong(t *testing.T) {
	world := startGoalWorld(t, []string{goalSilentRound}, "tell me when the build is green")
	defer world.close()

	before := time.Now()
	world.runGoalTurn(t, 1)
	after := world.read(t)
	if after.GoalState != models.GoalWorking || after.GoalNextAt == nil {
		t.Fatalf("a silent turn leaves the goal working: %+v", after)
	}
	// A goal just set has no interval behind it, so the default stands in
	// for the last one: thirty minutes doubled.
	if wait := after.GoalNextAt.Sub(before); wait < 55*time.Minute || wait > 65*time.Minute {
		t.Fatalf("the next turn should be twice the default away, got %s", wait)
	}
}

// The turns a conversation may take in a day are counted from the job
// rows. At the cap the goal goes on tomorrow instead, and says so.
func TestGoalStopsAtTheDayCap(t *testing.T) {
	world := startGoalWorld(t, []string{goalNoteRound, answerRound}, "watch the deploy")
	defer world.close()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for turn := 0; turn < 48; turn++ {
			job, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: world.found.ID, Kind: models.AgentJobGoal, SubjectID: world.conversation.ID})
			if err != nil {
				t.Fatalf("EnqueueAgentJob: %s", err)
			}
			if err := tx.FinishAgentJob(job.ID, "", models.AgentJobDone, "", nil); err != nil {
				t.Fatalf("FinishAgentJob: %s", err)
			}
		}
	})
	world.runGoalTurn(t, 49)

	after := world.read(t)
	if after.GoalState != models.GoalWorking {
		t.Fatalf("the cap postpones the goal, it does not end it: %+v", after)
	}
	if !strings.Contains(after.GoalNote, "48 turns today") {
		t.Fatalf("the note should say why it stopped: %q", after.GoalNote)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	local := time.Now().In(berlin)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, berlin).AddDate(0, 0, 1)
	if after.GoalNextAt == nil || !after.GoalNextAt.Equal(midnight) {
		t.Fatalf("it should go on at the person's own midnight (%s), got %v", midnight, after.GoalNextAt)
	}
	// And the model was never asked: the cap comes before the turn.
	if len(*world.requests) != 0 {
		t.Fatalf("a capped goal asks the model nothing: %d requests", len(*world.requests))
	}
}

// A goal that is met is over: nothing is scheduled, the person is told,
// and the words stay on the conversation until they clear them.
func TestGoalThatIsMetEndsTheTurns(t *testing.T) {
	world := startGoalWorld(t, []string{goalMetRound, answerRound}, "file the invoices from last quarter")
	defer world.close()

	world.runGoalTurn(t, 1)
	after := world.read(t)
	if after.GoalState != models.GoalMet || after.GoalNextAt != nil {
		t.Fatalf("a met goal schedules nothing more: %+v", after)
	}
	if after.Goal == "" || after.GoalNote != "all five are filed" {
		t.Fatalf("the goal and its closing word stay: %+v", after)
	}
	world.sender.mutex.Lock()
	sent := len(world.sender.sent)
	subject := ""
	if sent > 0 {
		subject = world.sender.sent[0].Subject
	}
	world.sender.mutex.Unlock()
	// Not mailed about: a goal that is met is read in the drawer when the
	// person next looks, and the maintainer asked not to be written to.
	if sent != 0 {
		t.Fatalf("a met goal is not mailed about: %d %q", sent, subject)
	}
	if err := world.worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	world.worker.Wait()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		jobs, _ = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: world.found.ID, Kinds: []models.AgentJobKind{models.AgentJobGoal}}, nil)
	})
	if len(jobs) != 1 {
		t.Fatalf("a met goal is never queued again: %+v", jobs)
	}
}

// A goal nobody can meet answers "look again" for ever; after a run of
// turns with no word from the person it stops and waits for them, and
// they are told, so the worst case is a bounded number of turns rather
// than the day's cap every day.
func TestGoalStopsAfterTurnsAlone(t *testing.T) {
	world := startGoalWorld(t, []string{goalNoteRound, answerRound}, "watch the deploy")
	defer world.close()

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for turn := 0; turn < 24; turn++ {
			job, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: world.found.ID, Kind: models.AgentJobGoal, SubjectID: world.conversation.ID})
			if err != nil {
				t.Fatalf("EnqueueAgentJob: %s", err)
			}
			if err := tx.FinishAgentJob(job.ID, "", models.AgentJobDone, "", nil); err != nil {
				t.Fatalf("FinishAgentJob: %s", err)
			}
		}
	})
	world.runGoalTurn(t, 25)

	after := world.read(t)
	if after.GoalState != models.GoalWaiting || after.GoalNextAt != nil {
		t.Fatalf("the bound stops the goal and waits for the person: %+v", after)
	}
	if !strings.HasPrefix(after.GoalNote, "Goal stalled: 24 turns since you last wrote") {
		t.Fatalf("the note should say why it stopped: %q", after.GoalNote)
	}
	// And the transcript says so where the person reads, since no turn
	// ran to say it there.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(world.conversation.ID, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		last := messages[len(messages)-1]
		if last.Role != models.AgentMessageNote || last.Content != after.GoalNote {
			t.Fatalf("the stall should be the transcript's last line: %+v", last)
		}
	})
	if len(*world.requests) != 0 {
		t.Fatalf("a goal at the bound asks the model nothing: %d requests", len(*world.requests))
	}
	world.sender.mutex.Lock()
	sent := len(world.sender.sent)
	world.sender.mutex.Unlock()
	if sent != 1 {
		t.Fatalf("the person is told once that it waits: %d", sent)
	}
	world.sender.mutex.Lock()
	subject := world.sender.sent[0].Subject
	world.sender.mutex.Unlock()
	if !strings.HasPrefix(subject, "Goal stalled: ") {
		t.Fatalf("the mail says it stalled: %q", subject)
	}

	// Their own turn starts the count again: with a word from them after
	// the twenty-four, the next turn of the agent's own runs.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.conversation.ID, Role: "user", Content: "keep going"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		due := time.Now().Add(-time.Minute)
		if _, err := tx.UpdateAgentConversation(world.conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState, conversation.GoalNextAt = models.GoalWorking, &due
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}
	})
	world.runGoalTurn(t, 26)
	if len(*world.requests) == 0 {
		t.Fatalf("after the person wrote, the goal takes its turn again")
	}
	if after := world.read(t); after.GoalState != models.GoalWorking {
		t.Fatalf("the turn after the person's word goes on working: %+v", after)
	}
}
