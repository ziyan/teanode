package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// Live Activities: a turn the person started in the app and left running is
// shown on the lock screen and in the Dynamic Island until it ends. The app
// starts the activity, registers its push token with the relay and gives the
// sealed registration to the run, which pushes the turn's state each time it
// changes. An activity is shown readable by Apple and the relay, so it is
// told one word (push.Delivery.ActivityState) and never what the turn says.

// liveActivitiesMost bounds the activities one run pushes to: one per phone.
const liveActivitiesMost = 4

// liveActivities are the Live Activities following one run.
type liveActivities struct {
	mutex         sync.Mutex
	registrations []string
	// activityState is the state last pushed, so an unchanged one is not
	// pushed again; isEnded says the end was pushed.
	activityState string
	isEnded       bool
	// pending is the newest state not yet sent, by registration. One
	// sender per run sends them, one at a time, so each activity is told
	// its states in the order they happened: sent from a goroutine apiece,
	// a "waiting" could reach Apple after the "working" that followed it.
	// A state superseded while the one before it is on its way is never
	// sent at all. isSending says the sender is running.
	pending   map[string]liveActivityUpdate
	isSending bool
}

// liveActivityUpdate is one state on its way to one Live Activity.
type liveActivityUpdate struct {
	activityState string
	isEnded       bool
	changedAt     time.Time
}

// activityStateOf is the state an event puts a turn in, and whether it ends
// it; false when the event changes nothing a Live Activity shows.
func activityStateOf(kind EventKind) (string, bool, bool) {
	switch kind {
	case EventConfirmation, EventQuestion, EventLocate:
		return "waiting", false, true
	case EventText, EventMessage, EventToolCall, EventToolResult:
		return "working", false, true
	case EventDone:
		return "done", true, true
	case EventError:
		return "failed", true, true
	default:
		return "", false, false
	}
}

// RegisterLiveActivity has the run push its state to a Live Activity, from
// now on and at once.
func (self *AskRun) RegisterLiveActivity(pushRegistration string) error {
	if pushRegistration == "" {
		return fmt.Errorf("agent: a Live Activity needs its registration")
	}
	self.mutex.Lock()
	isFinished := self.finished
	self.mutex.Unlock()
	if isFinished {
		return fmt.Errorf("agent: the turn has ended")
	}
	self.live.mutex.Lock()
	defer self.live.mutex.Unlock()
	if len(self.live.registrations) >= liveActivitiesMost {
		return fmt.Errorf("agent: this turn follows %d Live Activities already", liveActivitiesMost)
	}
	self.live.registrations = append(self.live.registrations, pushRegistration)
	// A turn that has said nothing yet is working.
	if self.live.activityState == "" {
		self.live.activityState = "working"
	}
	self.queueLiveActivityLocked(pushRegistration, liveActivityUpdate{activityState: self.live.activityState, isEnded: self.live.isEnded, changedAt: time.Now()})
	return nil
}

// followLiveActivities pushes the state an event puts the turn in, when it
// changes, to every Live Activity following the run.
func (self *AskRun) followLiveActivities(kind EventKind, at time.Time) {
	activityState, isEnding, ok := activityStateOf(kind)
	if !ok {
		return
	}
	self.live.mutex.Lock()
	defer self.live.mutex.Unlock()
	if self.live.isEnded || (activityState == self.live.activityState && !isEnding) {
		return
	}
	self.live.activityState = activityState
	self.live.isEnded = isEnding
	for _, registration := range self.live.registrations {
		self.queueLiveActivityLocked(registration, liveActivityUpdate{activityState: activityState, isEnded: isEnding, changedAt: at})
	}
}

// queueLiveActivityLocked puts a state in line for one activity, replacing
// any not sent yet, and starts the run's sender if it is not running. The
// caller holds live.mutex.
func (self *AskRun) queueLiveActivityLocked(pushRegistration string, update liveActivityUpdate) {
	if self.live.pending == nil {
		self.live.pending = map[string]liveActivityUpdate{}
	}
	self.live.pending[pushRegistration] = update
	if self.live.isSending {
		return
	}
	self.live.isSending = true
	self.agent.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.agent.waitGroup.Done()
		self.sendLiveActivities()
	}()
}

// sendLiveActivities is the run's sender: it sends what is pending until
// nothing is.
func (self *AskRun) sendLiveActivities() {
	for {
		self.live.mutex.Lock()
		pending := self.live.pending
		self.live.pending = nil
		if len(pending) == 0 {
			self.live.isSending = false
			self.live.mutex.Unlock()
			return
		}
		self.live.mutex.Unlock()
		sender, err := self.agent.phoneSender()
		if err != nil || sender == nil {
			continue
		}
		for registration, update := range pending {
			self.agent.sendLiveActivity(sender, registration, update)
		}
	}
}

// sendLiveActivity pushes one state to one Live Activity and waits for the
// relay to take it.
func (self *Agent) sendLiveActivity(sender push.Sender, pushRegistration string, update liveActivityUpdate) {
	ctx, cancel := context.WithTimeout(self.ctx, phonePushTimeout)
	defer cancel()
	delivery := &push.Delivery{ActivityState: update.activityState, IsActivityEnded: update.isEnded, ActivityChangedAt: update.changedAt}
	if err := sender.Send(ctx, pushRegistration, delivery); err != nil {
		log.Noticef("a Live Activity was not told %s: %s", update.activityState, err)
	}
}
