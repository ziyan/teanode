package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// sameAnswerTool is a tool that answers the same words every time it runs,
// and counts its runs.
func sameAnswerTool(name string, risk tools.Risk, runCount *atomic.Int32) *Tool {
	return &Tool{
		Name: name, Family: FamilyGeneral, Core: true, Risk: risk,
		Description: "a tool for a test that always answers the same",
		Parameters:  tools.Object(map[string]any{}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			runCount.Add(1)
			return tools.TextResult(`{"is_kept": true}`), nil
		},
	}
}

// A change sent again and again with the same answer runs twice: the second
// answer says it is done, the third call is turned back without running.
func TestTheSameChangeAnsweredTheSameRunsTwice(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var runCount atomic.Int32
	worker, run, conversation := batchWorld(t, database,
		[]string{
			callsRound([2]string{"call_a", "note_keep"}),
			callsRound([2]string{"call_b", "note_keep"}),
			callsRound([2]string{"call_c", "note_keep"}),
			saidByModel("Kept."),
		},
		sameAnswerTool("note_keep", RiskWrite, &runCount))

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "keep the note", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collectEvents(turn)

	if runCount.Load() != 2 {
		t.Fatalf("the change ran %d times, not 2", runCount.Load())
	}
	answers := storedToolAnswers(t, database, conversation.ID)
	if len(answers) != 3 {
		t.Fatalf("three answers are kept: %v", answers)
	}
	if answers[0] != `call_a={"is_kept": true}` {
		t.Errorf("the first answer is the tool's own: %q", answers[0])
	}
	if !strings.HasPrefix(answers[1], `call_b={"is_kept": true}`) || !strings.Contains(answers[1], "repeated_call: this exact call already ran") {
		t.Errorf("the second answer says the change is done: %q", answers[1])
	}
	if answers[2] != "call_c="+repeatedCallRefusal {
		t.Errorf("the third call is turned back: %q", answers[2])
	}
}

// A change whose answer moves each time is a new step each time, and a read
// may be asked as often as the model likes: neither is held back.
func TestRepeatsThatAnswerDifferentlyOrOnlyReadRun(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	times := newCallTimes()
	var readCount atomic.Int32
	worker, run, conversation := batchWorld(t, database,
		[]string{
			callsRound([2]string{"call_a", "page_next"}, [2]string{"call_b", "shelf_look"}),
			callsRound([2]string{"call_c", "page_next"}, [2]string{"call_d", "shelf_look"}),
			callsRound([2]string{"call_e", "page_next"}, [2]string{"call_f", "shelf_look"}),
			saidByModel("Three pages on."),
		},
		slowTool("page_next", RiskWrite, time.Millisecond, times),
		sameAnswerTool("shelf_look", RiskRead, &readCount))

	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: &digestSplitOperations{}, Conversation: conversation, Message: "go three pages on", Surface: "cli", Short: true})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	collectEvents(turn)

	if times.startedCount() != 3 {
		t.Errorf("a change answering differently each time ran %d times, not 3", times.startedCount())
	}
	if readCount.Load() != 3 {
		t.Errorf("a read answering the same ran %d times, not 3", readCount.Load())
	}
	for _, answer := range storedToolAnswers(t, database, conversation.ID) {
		if strings.Contains(answer, "repeated_call") {
			t.Errorf("nothing was held back: %q", answer)
		}
	}
}

// The same arguments written with other spacing or key order are the same
// call.
func TestRepeatedCallKeySettlesTheArguments(t *testing.T) {
	first := repeatedCallKey("note_keep", []byte(`{"day": "2026-01-05", "text": "a"}`))
	second := repeatedCallKey("note_keep", []byte(`{"text":"a","day":"2026-01-05"}`))
	if first != second {
		t.Errorf("%q and %q are one call", first, second)
	}
	if repeatedCallKey("note_keep", []byte(`{"text":"b"}`)) == repeatedCallKey("note_keep", []byte(`{"text":"a"}`)) {
		t.Error("other words are another call")
	}
}
