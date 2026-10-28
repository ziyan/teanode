package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

// A word to a turn from wherever the person is: stop it, answer what it
// asked, decide a card. The turn runs on one instance; the person's
// browser may be talking to another, which sees the turn through the
// feed. The word is applied here when the run is here, and said to the
// other instances otherwise, as a notification the running one applies.
//
// An instance remembers the turns it has heard of through the feed, the
// run and its conversation, so it can tell whose a run is before it
// forwards a word about it; one it has not heard of is in the record of the
// turns that are running (turn_record.go).

// RunCommand is one word to a run.
type RunCommand struct {
	// Instance is who said it, filled in on the way out.
	Instance string `json:"instance"`
	RunID    string `json:"runId"`
	// Action is "stop", "answer", "resolve", "locate" or "liveActivity".
	Action  string `json:"action"`
	CallID  string `json:"callId,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Approve bool   `json:"approve,omitempty"`
	// PushRegistration is the Live Activity a liveActivity command has the
	// run follow (see RegisterLiveActivity).
	PushRegistration string `json:"pushRegistration,omitempty"`
}

const (
	CommandStop    = "stop"
	CommandAnswer  = "answer"
	CommandResolve = "resolve"
	// CommandLocate carries the browser's answer to a location call in
	// Answer, as JSON (see AnswerLocation).
	CommandLocate = "locate"
	// CommandLiveActivity has the run push its state to the Live Activity
	// in PushRegistration. The app registers the activity wherever its
	// request lands, which need not be the instance running the turn.
	CommandLiveActivity = "liveActivity"
)

// foreignRunFor is how long a run heard of through the feed is
// remembered after its last event.
const foreignRunFor = time.Hour

type foreignRun struct {
	conversationId string
	heard          time.Time
	// log is the run's events as heard here (foreign_run.go).
	log *foreignRunLog
}

// Apply gives the run the word here. It says whether anything was
// waiting for it: a stop always is.
func (self *Agent) Apply(run *AskRun, command RunCommand) bool {
	switch command.Action {
	case CommandStop:
		run.StopByPerson()
		return true
	case CommandAnswer:
		return run.Answer(command.CallID, command.Answer)
	case CommandResolve:
		return run.Resolve(command.CallID, command.Approve)
	case CommandLocate:
		return run.AnswerLocation(command.CallID, command.Answer)
	case CommandLiveActivity:
		if err := run.RegisterLiveActivity(command.PushRegistration); err != nil {
			log.Noticef("a Live Activity was not followed: %s", err)
			return false
		}
		return true
	}
	return false
}

// Forward says the word to the other instances, for the one running the
// turn to apply. Whether anything was waiting for it is not known here.
func (self *Agent) Forward(command RunCommand) error {
	if self.settings.Database == nil {
		return fmt.Errorf("agent: no way to reach the instance running the turn")
	}
	command.Instance = self.settings.Instance
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	return self.settings.Database.NotifyAgentCommand(string(payload))
}

// ForeignRun is the conversation of a run another instance is running,
// as heard through the feed, and whether it is known at all.
//
// A run this instance has not heard of, because it began before this
// instance started or has been quiet, is found in the record of the turns
// that are running.
func (self *Agent) ForeignRun(runId string) (string, bool) {
	self.foreignMutex.Lock()
	known := self.foreignRuns[runId]
	self.foreignMutex.Unlock()
	if known != nil {
		return known.conversationId, true
	}
	return self.recordedRunElsewhere(runId)
}

// noteForeignEvent remembers a run heard of through the feed, with the
// event heard, and forgets the ones not heard of for a while.
func (self *Agent) noteForeignEvent(event Event) {
	self.foreignMutex.Lock()
	defer self.foreignMutex.Unlock()
	if self.foreignRuns == nil {
		self.foreignRuns = map[string]*foreignRun{}
	}
	now := time.Now()
	self.forgetForeignRuns(now)
	known := self.foreignRuns[event.RunID]
	if known == nil {
		known = &foreignRun{conversationId: event.ConversationID}
		self.foreignRuns[event.RunID] = known
	}
	known.heard = now
	self.keepForeignEvent(known, event)
}

// listenCommands applies the words the other instances say about the
// runs that are here.
func (self *Agent) listenCommands() {
	payloads, err := self.settings.Database.ListenAgentCommands(self.ctx)
	if err != nil {
		log.Warningf("the agent cannot hear commands from the other instances: %s", err)
		return
	}
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		for payload := range payloads {
			var command RunCommand
			if err := json.Unmarshal([]byte(payload), &command); err != nil || command.Instance == self.settings.Instance {
				continue
			}
			if run := self.FindRun(command.RunID); run != nil {
				self.Apply(run, command)
			}
		}
	}()
}
