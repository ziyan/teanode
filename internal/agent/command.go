package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

// A word to a turn from wherever the person is: stop it, answer what it
// asked, decide a card. The turn runs on one instance; the person's
// browser may be talking to another, which sees the turn through the
// feed. The word is applied here when the run is here, and said to the
// other instances otherwise, as a notification the running one applies.
//
// An instance remembers the turns it has heard of through the feed — the
// run and its conversation — so it can tell whose a run is before it
// forwards a word about it.

// RunCommand is one word to a run.
type RunCommand struct {
	// Instance is who said it, filled in on the way out.
	Instance string `json:"instance"`
	RunID    string `json:"runId"`
	// Action is "stop", "answer" or "resolve".
	Action  string `json:"action"`
	CallID  string `json:"callId,omitempty"`
	Answer  string `json:"answer,omitempty"`
	Approve bool   `json:"approve,omitempty"`
}

const (
	CommandStop    = "stop"
	CommandAnswer  = "answer"
	CommandResolve = "resolve"
)

// foreignRunFor is how long a run heard of through the feed is
// remembered after its last event.
const foreignRunFor = time.Hour

type foreignRun struct {
	conversationId string
	heard          time.Time
}

// Apply gives the run the word here. It says whether anything was
// waiting for it: a stop always is.
func (self *Agent) Apply(run *AskRun, command RunCommand) bool {
	switch command.Action {
	case CommandStop:
		run.Stop()
		return true
	case CommandAnswer:
		return run.Answer(command.CallID, command.Answer)
	case CommandResolve:
		return run.Resolve(command.CallID, command.Approve)
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
func (self *Agent) ForeignRun(runId string) (string, bool) {
	self.foreignMutex.Lock()
	defer self.foreignMutex.Unlock()
	known, ok := self.foreignRuns[runId]
	return known.conversationId, ok
}

// noteForeignRun remembers a run heard of through the feed, and forgets
// the ones not heard of for a while.
func (self *Agent) noteForeignRun(runId, conversationId string) {
	self.foreignMutex.Lock()
	defer self.foreignMutex.Unlock()
	if self.foreignRuns == nil {
		self.foreignRuns = map[string]foreignRun{}
	}
	now := time.Now()
	for id, known := range self.foreignRuns {
		if now.Sub(known.heard) > foreignRunFor {
			delete(self.foreignRuns, id)
		}
	}
	self.foreignRuns[runId] = foreignRun{conversationId: conversationId, heard: now}
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
