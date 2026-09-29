package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// callsRound is a round in which the model asks for several calls at once,
// each named by its id and made to the tool given, with no arguments.
func callsRound(calls ...[2]string) string {
	parts := make([]string, 0, len(calls))
	for index, call := range calls {
		parts = append(parts, fmt.Sprintf(`{"index":%d,"id":%q,"function":{"name":%q,"arguments":"{}"}}`, index, call[0], call[1]))
	}
	return `{"choices":[{"delta":{"tool_calls":[` + strings.Join(parts, ",") + `]},"finish_reason":"tool_calls"}]}`
}

// callTimes is when each call of a test's tools started and ended, by id.
type callTimes struct {
	mutex    sync.Mutex
	started  map[string]time.Time
	ended    map[string]time.Time
	canceled map[string]bool
}

func newCallTimes() *callTimes {
	return &callTimes{started: map[string]time.Time{}, ended: map[string]time.Time{}, canceled: map[string]bool{}}
}

func (self *callTimes) start(id string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.started[id] = time.Now()
}

func (self *callTimes) end(id string, isCanceled bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.ended[id] = time.Now()
	self.canceled[id] = isCanceled
}

func (self *callTimes) startedCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.started)
}

// slowTool is a tool that takes duration, or until the turn is stopped,
// and answers with the id of its call.
func slowTool(name string, risk tools.Risk, duration time.Duration, times *callTimes) *Tool {
	return &Tool{
		Name: name, Family: FamilyGeneral, Core: true, Risk: risk,
		Description: "a tool for a test that takes its time",
		Parameters:  tools.Object(map[string]any{}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			times.start(call.ID)
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
				times.end(call.ID, false)
				return tools.TextResult("answered %s", call.ID), nil
			case <-ctx.Done():
				times.end(call.ID, true)
				return nil, ctx.Err()
			}
		},
	}
}

// batchWorld is a turn's world with the given tools in the catalog, a
// model answering from rounds, and a conversation to talk in.
func batchWorld(t *testing.T, database db.Database, rounds []string, extraTools ...*Tool) (*Agent, *Run, *models.AgentConversation) {
	t.Helper()
	provider := scriptedProvider(rounds)
	t.Cleanup(provider.Close)
	worker, run := digestSplitWorld(t, database, provider.URL)
	for _, tool := range extraTools {
		worker.catalog.Register(tool)
	}
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	return worker, run, conversation
}

// storedToolAnswers is the tool messages of a conversation, by call id, in
// the order they were kept.
func storedToolAnswers(t *testing.T, database db.Database, conversationId string) []string {
	t.Helper()
	var answers []string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, err := tx.ListAgentMessages(conversationId, nil)
		if err != nil {
			t.Fatalf("ListAgentMessages: %s", err)
		}
		for _, message := range messages {
			if message.Role == string(llm.RoleTool) {
				answers = append(answers, message.ToolCallID+"="+message.Content)
			}
		}
	})
	return answers
}

// Three reads asked for at once take about as long as one, and what they
// answered is kept in the order the model asked for them.
func TestAskRunsARoundsReadsTogether(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const readDuration = 400 * time.Millisecond
	times := newCallTimes()
	worker, run, conversation := batchWorld(t, database,
		[]string{callsRound([2]string{"call_a", "kettle_look"}, [2]string{"call_b", "kettle_look"}, [2]string{"call_c", "kettle_look"}), saidByModel("All three are warm.")},
		slowTool("kettle_look", RiskRead, readDuration, times))

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "look at the kettles", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	events := collectEvents(turn)

	var firstStart, lastEnd time.Time
	for _, id := range []string{"call_a", "call_b", "call_c"} {
		started, ended := times.started[id], times.ended[id]
		if started.IsZero() || ended.IsZero() {
			t.Fatalf("the call %s ran: %v", id, times.started)
		}
		if firstStart.IsZero() || started.Before(firstStart) {
			firstStart = started
		}
		if ended.After(lastEnd) {
			lastEnd = ended
		}
	}
	if took := lastEnd.Sub(firstStart); took > readDuration*2 {
		t.Fatalf("three reads of %s each took %s together, which is one after another", readDuration, took)
	}
	answers := storedToolAnswers(t, database, conversation.ID)
	if strings.Join(answers, " ") != "call_a=answered call_a call_b=answered call_b call_c=answered call_c" {
		t.Fatalf("the answers are kept in the model's order: %v", answers)
	}
	// The drawer is told each call started, in the model's order, and
	// each one's result.
	var started, resulted []string
	for _, event := range events {
		switch event.Kind {
		case EventToolCall:
			started = append(started, event.CallID)
		case EventToolResult:
			resulted = append(resulted, event.CallID)
		}
	}
	if strings.Join(started, " ") != "call_a call_b call_c" || len(resulted) != 3 {
		t.Fatalf("each call is said to start in order and to answer: %v %v", started, resulted)
	}
}

// A change between two reads is a barrier: it starts after the read before
// it has answered, and the read after it starts once it has.
func TestAskRunsAChangeBetweenReadsAlone(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	const duration = 200 * time.Millisecond
	times := newCallTimes()
	worker, run, conversation := batchWorld(t, database,
		[]string{callsRound([2]string{"call_before", "shelf_look"}, [2]string{"call_change", "shelf_tidy"}, [2]string{"call_after", "shelf_look"}), saidByModel("Tidied.")},
		slowTool("shelf_look", RiskRead, duration, times), slowTool("shelf_tidy", RiskWrite, duration, times))

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "tidy the shelf", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collectEvents(turn)

	if !times.started["call_change"].After(times.ended["call_before"]) {
		t.Fatal("the change starts after the read before it has answered")
	}
	if !times.started["call_after"].After(times.ended["call_change"]) {
		t.Fatal("the read after the change starts once the change has answered")
	}
	answers := storedToolAnswers(t, database, conversation.ID)
	if strings.Join(answers, " ") != "call_before=answered call_before call_change=answered call_change call_after=answered call_after" {
		t.Fatalf("the answers are kept in the model's order: %v", answers)
	}
}

// Stop cancels every call of the batch that is running, and the turn ends
// without waiting them out.
func TestStopCancelsTheWholeBatch(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	times := newCallTimes()
	worker, run, conversation := batchWorld(t, database,
		[]string{callsRound([2]string{"call_a", "pond_look"}, [2]string{"call_b", "pond_look"}, [2]string{"call_c", "pond_look"}), saidByModel("Never said.")},
		slowTool("pond_look", RiskRead, time.Minute, times))

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "watch the pond", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for times.startedCount() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the three calls started together: %d did", times.startedCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
	stoppedAt := time.Now()
	turn.StopByPerson()
	select {
	case <-turn.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn ends when stopped, without waiting for its calls")
	}
	if took := time.Since(stoppedAt); took > 2*time.Second {
		t.Fatalf("the turn took %s to stop", took)
	}
	for _, id := range []string{"call_a", "call_b", "call_c"} {
		if !times.canceled[id] {
			t.Fatalf("the call %s was canceled: %v", id, times.canceled)
		}
	}
}

// collectEvents follows a turn to its end and gives what it emitted.
func collectEvents(turn *AskRun) []Event {
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

// batchRun is a turn put together without a database, for the parts of a
// round that need none: which calls run together, and cards.
func batchRun() *AskRun {
	run := &AskRun{
		ID:            "run-batch",
		settings:      &AskSettings{Conversation: &models.AgentConversation{ID: "conversation-batch"}, Surface: "drawer"},
		agent:         &Agent{},
		subscribers:   map[int]chan Event{},
		confirmations: map[string]chan bool{},
		loaded:        map[string]bool{},
		done:          make(chan struct{}),
	}
	run.ctx, run.cancel = context.WithCancel(context.Background())
	return run
}

// Which calls run together: reads that follow each other, and nothing
// that changes, raises a card, drives the browser or waits on a judgement.
func TestRoundBatchesKeepReadsTogetherAndEverythingElseAlone(t *testing.T) {
	run := batchRun()
	defer run.cancel()
	configuration := config.Default()
	configuration.Agent.Tools.Confirm = []string{"garden_count"}
	sent := []*Tool{
		{Name: "garden_look", Family: FamilyGeneral, Risk: RiskRead},
		{Name: "garden_water", Family: FamilyGeneral, Risk: RiskWrite},
		{Name: "garden_count", Family: FamilyGeneral, Risk: RiskRead},
		{Name: "garden_page", Family: FamilyBrowser, Risk: RiskRead},
		{Name: "garden_command", Family: FamilyComputer, Risk: RiskRead, JudgedCall: func(json.RawMessage) string { return "count the pots" }},
		{Name: "garden_note", Family: FamilyGeneral, Risk: RiskWrite, RiskOf: func(arguments json.RawMessage) tools.Risk {
			if strings.Contains(string(arguments), "read") {
				return RiskRead
			}
			return RiskWrite
		}},
	}
	call := func(id, name, arguments string) llm.ToolCall {
		return llm.ToolCall{ID: id, Name: name, Arguments: arguments}
	}
	batches := run.roundBatches(configuration, sent, []llm.ToolCall{
		call("1", "garden_look", "{}"),
		call("2", "garden_note", `{"action":"read"}`),
		call("3", "garden_missing", "{}"),
		call("4", "garden_water", "{}"),
		call("5", "garden_look", "{}"),
		call("6", "garden_count", "{}"),
		call("7", "garden_page", "{}"),
		call("8", "garden_command", "{}"),
		call("9", "garden_look", "{}"),
		call("10", "garden_look", "{}"),
	})
	var said []string
	for _, batch := range batches {
		var ids []string
		for _, toolCall := range batch {
			ids = append(ids, toolCall.ID)
		}
		said = append(said, strings.Join(ids, ","))
	}
	if strings.Join(said, " ") != "1,2,3 4 5 6 7 8 9,10" {
		t.Fatalf("the batches: %v", said)
	}
}

// Two subagents in one batch whose work each reaches a call that asks put
// their cards in the parent's conversation one at a time, and both go on
// once answered.
func TestSubagentCardsInOneBatchAreAskedOneAtATime(t *testing.T) {
	parent := batchRun()
	defer parent.cancel()
	demolish := &Tool{Name: "shed_demolish", Family: FamilyGeneral, Risk: RiskDestructive}
	// What runSubagent does for a card, without the model: a run of its
	// own whose confirmations go to the parent.
	subagentLike := &Tool{
		Name: "helper", Family: FamilyGeneral, Risk: RiskRead,
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			child := &AskRun{settings: &AskSettings{Conversation: &models.AgentConversation{ID: "subagent-" + call.ID}, confirmVia: parent}}
			isApproved, err := child.confirm(ctx, demolish, &Call{ID: "inner-" + call.ID, Arguments: json.RawMessage(`{}`)})
			if err != nil {
				return nil, err
			}
			return tools.TextResult("approved=%v", isApproved), nil
		},
	}
	events, unsubscribe := parent.Subscribe()
	defer unsubscribe()
	answered := make(chan []string, 1)
	failures := make(chan string, 4)
	go func() {
		var cardIds []string
		for event := range events {
			if event.Kind != EventConfirmation {
				continue
			}
			parent.mutex.Lock()
			openCount := len(parent.confirmations)
			parent.mutex.Unlock()
			if openCount != 1 {
				failures <- fmt.Sprintf("%d cards were open at once", openCount)
			}
			cardIds = append(cardIds, event.CallID)
			// Long enough for a second card to have come up, were it
			// going to.
			time.Sleep(100 * time.Millisecond)
			parent.mutex.Lock()
			openCount = len(parent.confirmations)
			parent.mutex.Unlock()
			if openCount != 1 {
				failures <- fmt.Sprintf("%d cards were open while one waited", openCount)
			}
			parent.Resolve(event.CallID, true)
			if len(cardIds) == 2 {
				answered <- cardIds
				return
			}
		}
	}()
	outcomes := parent.runBatch(parent.ctx, config.Default(), []*Tool{subagentLike}, nil, []llm.ToolCall{
		{ID: "call_first", Name: "helper", Arguments: "{}"},
		{ID: "call_second", Name: "helper", Arguments: "{}"},
	})
	select {
	case failure := <-failures:
		t.Fatal(failure)
	default:
	}
	select {
	case cardIds := <-answered:
		if len(cardIds) != 2 || cardIds[0] == cardIds[1] {
			t.Fatalf("both cards were put: %v", cardIds)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("both cards were put and answered")
	}
	for index, outcome := range outcomes {
		if outcome.content != "approved=true" {
			t.Fatalf("the call %d went on once its card was answered: %q", index, outcome.content)
		}
	}
}

// A card waiting for its turn behind another gives up when the turn is
// stopped, instead of being put after the stop.
func TestACardWaitingBehindAnotherEndsWithTheTurn(t *testing.T) {
	parent := batchRun()
	release, err := parent.takeCardSlot(parent.ctx)
	if err != nil {
		t.Fatalf("takeCardSlot: %s", err)
	}
	defer release()
	waited := make(chan error, 1)
	go func() {
		_, err := parent.confirmWaiting(parent.ctx, &Tool{Name: "shed_demolish", Risk: RiskDestructive}, &Call{ID: "call_waiting", Arguments: json.RawMessage(`{}`)}, false)
		waited <- err
	}()
	time.Sleep(50 * time.Millisecond)
	parent.cancel()
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("a card stopped before it was put is not approved")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting card gave up when the turn was stopped")
	}
	for _, event := range parent.events {
		if event.Kind == EventConfirmation {
			t.Fatal("no card is put after the stop")
		}
	}
}
