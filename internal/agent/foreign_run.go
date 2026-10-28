package agent

import (
	"time"
)

// A run's events can be read from any instance, not only the one running it:
// the command line polls a run, and a load balancer sends each poll where it
// likes. Each instance keeps, for the runs it hears of through the feed, the
// events it heard, bounded the way a run here keeps its own, and hands them
// to a reader as a run here would. What another instance heard is joined and
// trimmed for the relay, so its pieces of text are fewer and its sequences
// sparse; a reader asks for what follows a sequence, which holds for both.

const (
	// foreignRunsKeptMost bounds the runs of other instances whose events
	// are kept, the least recently heard going first.
	foreignRunsKeptMost = 256

	// foreignRunBytesMost bounds what is kept of one run of another
	// instance. A run here keeps its first 500 events whatever their size;
	// one kept here for a reader who may never come is kept smaller, since
	// every instance keeps every other instance's runs.
	foreignRunBytesMost = 256 << 10

	// foreignRunDoneFor is how long the events of a finished run of another
	// instance are kept for a reader who comes late, as forgetRun keeps a
	// run of this one.
	foreignRunDoneFor = 10 * time.Minute
)

// foreignRunLog is what this instance heard of one run of another.
type foreignRunLog struct {
	events []Event
	// keptBytes is about how much the kept events hold.
	keptBytes      int
	isDone         bool
	subscribers    map[int]chan Event
	nextSubscriber int
}

// keepForeignEvent keeps one event of another instance's run and hands it
// to whoever reads that run here. Called with foreignMutex held.
func (self *Agent) keepForeignEvent(known *foreignRun, event Event) {
	if known.log == nil {
		known.log = &foreignRunLog{subscribers: map[int]chan Event{}}
	}
	runLog := known.log
	if runLog.isDone {
		return
	}
	eventBytes := len(event.Text) + len(event.Arguments) + len(event.Note) + len(event.NoteDetail) + len(event.Error) + 256
	if len(runLog.events) < askEventBacklog && runLog.keptBytes+eventBytes <= foreignRunBytesMost || event.Kind == EventDone {
		runLog.events = append(runLog.events, event)
		runLog.keptBytes += eventBytes
	}
	for _, channel := range runLog.subscribers {
		select {
		case channel <- event:
		default:
			// A reader who stopped reading misses this one, as a reader of
			// a run here does.
		}
	}
	if event.Kind == EventDone {
		runLog.isDone = true
		for id, channel := range runLog.subscribers {
			delete(runLog.subscribers, id)
			close(channel)
		}
	}
}

// SubscribeForeignRun reads a run another instance is running, as heard
// here: what was heard so far, then what follows, closed once it is done.
// ok is false for a run this instance has not heard of.
func (self *Agent) SubscribeForeignRun(runId string) (conversationId string, events <-chan Event, unsubscribe func(), ok bool) {
	self.foreignMutex.Lock()
	defer self.foreignMutex.Unlock()
	known, isKnown := self.foreignRuns[runId]
	if !isKnown || known.log == nil {
		return "", nil, nil, false
	}
	runLog := known.log
	channel := make(chan Event, askEventBacklog+16)
	for _, event := range runLog.events {
		channel <- event
	}
	if runLog.isDone {
		close(channel)
		return known.conversationId, channel, func() {}, true
	}
	id := runLog.nextSubscriber
	runLog.nextSubscriber++
	runLog.subscribers[id] = channel
	return known.conversationId, channel, func() {
		self.foreignMutex.Lock()
		defer self.foreignMutex.Unlock()
		if _, isSubscribed := runLog.subscribers[id]; isSubscribed {
			delete(runLog.subscribers, id)
			close(channel)
		}
	}, true
}

// forgetForeignRuns drops the runs of other instances not heard of for
// foreignRunFor, the finished ones after foreignRunDoneFor, and the least
// recently heard beyond foreignRunsKeptMost. Called with foreignMutex held.
func (self *Agent) forgetForeignRuns(now time.Time) {
	for id, known := range self.foreignRuns {
		isDone := known.log != nil && known.log.isDone
		if now.Sub(known.heard) > foreignRunFor || isDone && now.Sub(known.heard) > foreignRunDoneFor {
			self.dropForeignRun(id)
		}
	}
	for len(self.foreignRuns) > foreignRunsKeptMost {
		oldestId, oldestHeard := "", now
		for id, known := range self.foreignRuns {
			if known.heard.Before(oldestHeard) || oldestId == "" {
				oldestId, oldestHeard = id, known.heard
			}
		}
		self.dropForeignRun(oldestId)
	}
}

// dropForeignRun forgets one, ending its readers. Called with foreignMutex
// held.
func (self *Agent) dropForeignRun(runId string) {
	known, isKnown := self.foreignRuns[runId]
	if !isKnown {
		return
	}
	if known.log != nil {
		for id, channel := range known.log.subscribers {
			delete(known.log.subscribers, id)
			close(channel)
		}
	}
	delete(self.foreignRuns, runId)
}
