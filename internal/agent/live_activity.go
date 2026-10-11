package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ziyan/teanode/internal/push"
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
	if len(self.live.registrations) >= liveActivitiesMost {
		self.live.mutex.Unlock()
		return fmt.Errorf("agent: this turn follows %d Live Activities already", liveActivitiesMost)
	}
	self.live.registrations = append(self.live.registrations, pushRegistration)
	// A turn that has said nothing yet is working.
	if self.live.activityState == "" {
		self.live.activityState = "working"
	}
	activityState := self.live.activityState
	isEnded := self.live.isEnded
	self.live.mutex.Unlock()
	self.agent.sendLiveActivity(pushRegistration, activityState, isEnded)
	return nil
}

// followLiveActivities pushes the state an event puts the turn in, when it
// changes, to every Live Activity following the run.
func (self *AskRun) followLiveActivities(kind EventKind) {
	activityState, isEnding, ok := activityStateOf(kind)
	if !ok {
		return
	}
	self.live.mutex.Lock()
	if self.live.isEnded || (activityState == self.live.activityState && !isEnding) {
		self.live.mutex.Unlock()
		return
	}
	self.live.activityState = activityState
	self.live.isEnded = isEnding
	registrations := append([]string(nil), self.live.registrations...)
	self.live.mutex.Unlock()
	for _, registration := range registrations {
		self.agent.sendLiveActivity(registration, activityState, isEnding)
	}
}

// sendLiveActivity pushes one state to one Live Activity in the background.
func (self *Agent) sendLiveActivity(pushRegistration, activityState string, isEnded bool) {
	sender, err := self.phoneSender()
	if err != nil || sender == nil {
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer self.waitGroup.Done()
		ctx, cancel := context.WithTimeout(self.ctx, phonePushTimeout)
		defer cancel()
		delivery := &push.Delivery{ActivityState: activityState, IsActivityEnded: isEnded}
		if err := sender.Send(ctx, pushRegistration, delivery); err != nil {
			log.Noticef("a Live Activity was not told %s: %s", activityState, err)
		}
	}()
}
