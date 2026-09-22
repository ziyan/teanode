package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/decide"
)

// A decider that answers from a script, for asking what this path does
// with each kind of answer.
// The real path asks several deciders at once, so this is asked from
// several goroutines at once and has to count under a lock.
type scriptedDecider struct {
	answers map[string]float64
	err     error

	mutex sync.Mutex
	asked int
}

func (self *scriptedDecider) Decide(_ context.Context, state string, _ map[string]decide.Question) (decide.Answers, error) {
	self.mutex.Lock()
	self.asked++
	self.mutex.Unlock()
	if self.err != nil {
		return nil, self.err
	}
	yes, ok := self.answers[state]
	if !ok {
		return nil, errors.New("nothing scripted for that state")
	}
	return decide.Answers{"worth_opening": {Yes: yes}}, nil
}

func (self *scriptedDecider) timesAsked() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.asked
}

// A file is opened when the answer is strongly yes and passed over when it
// is not, and the line between them sits above the middle.
//
// Above the middle because the mistake is not symmetric: opening one costs
// a picture call now, and passing one over costs nothing, since a later
// night offers it again.
func TestWhatIsWorthOpeningIsDecidedAboveTheMiddle(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		yes  float64
		open bool
	}{
		{0.99, true},
		{worthOpeningFloor, true},
		{0.59, false},
		{0.5, false},
		{0.01, false},
	} {
		if open := each.yes >= worthOpeningFloor; open != each.open {
			test.Errorf("an answer of %v opened=%v", each.yes, open)
		}
	}
	if worthOpeningFloor <= 0.5 {
		test.Errorf("the line is at or below the middle: %v", worthOpeningFloor)
	}
}

// One file the decider could not answer for sends the whole batch to a
// model, rather than the batch coming back with that file missing.
//
// This is the property that matters most here. The caller declines
// everything not in the map it gets, and a declined file is passed over by
// every night afterwards, so a network error that came back as a partial
// answer would retire files for good on the strength of a timeout. The
// same mistake the model path already guards against, where an error
// object decoded into an empty list.
func TestOneFailureSendsTheWholeBatchToAModel(test *testing.T) {
	test.Parallel()

	decider := &scriptedDecider{err: errors.New("the service is down")}
	chosen, answered := decideWith(context.Background(), decider, []string{"a", "b", "c"})
	if answered {
		test.Error("a failure was reported as a decision")
	}
	if chosen != nil {
		test.Errorf("a failure came back with files chosen: %v", chosen)
	}
}

// Everything answered is a decision, and only what reads as worth opening
// is in it.
func TestOnlyWhatReadsAsWorthOpeningIsChosen(test *testing.T) {
	test.Parallel()

	decider := &scriptedDecider{answers: map[string]float64{
		"a": 0.95,
		"b": 0.10,
		"c": 0.72,
	}}
	chosen, answered := decideWith(context.Background(), decider, []string{"a", "b", "c"})
	if !answered {
		test.Fatal("an answer for every file was not a decision")
	}
	if len(chosen) != 2 || chosen["a"] == "" || chosen["c"] == "" {
		test.Fatalf("chosen: %v", chosen)
	}
	if _, wanted := chosen["b"]; wanted {
		test.Error("a file that read as furniture was chosen")
	}
	// The reason says how sure it was, since the row shows a reason
	// either way and "a model said so" is not one.
	if chosen["a"] == chosen["c"] {
		test.Errorf("both reasons read the same: %q", chosen["a"])
	}
	if asked := decider.timesAsked(); asked != 3 {
		test.Errorf("it asked %d times for three files", asked)
	}
}

// decideWith runs the real deciding over states given directly, so the
// behaviour under test is the code that ships and not a copy of it.
func decideWith(ctx context.Context, decider *scriptedDecider, states []string) (map[string]string, bool) {
	items := make([]decideItem, 0, len(states))
	for _, state := range states {
		items = append(items, decideItem{id: state, state: state})
	}
	return decideWorthOpening(ctx, decider, items)
}
