package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/periodic"
)

// Only the instance holding the websocket of a person's computer can read it,
// but any instance may claim a job that needs the computer. Each instance
// therefore says in agent_computer_attachment which computers it holds, and
// says every so often that it still does. An instance that claimed a job for
// a computer it does not hold looks there: when another instance that has
// been heard of lately holds it, the job waits a moment for that instance
// instead of being counted as waiting for a computer that is offline.

const (
	// computerElsewhereRetry is how long a job waits when its computer is
	// held by another instance.
	computerElsewhereRetry = 20 * time.Second

	// computerAttachmentTimeout bounds the writes that follow an attach or a
	// detach, which a websocket handler waits for.
	computerAttachmentTimeout = 10 * time.Second
)

// startComputerAttachments forgets what this instance said it held before it
// last stopped, says again what it holds now, and begins saying every
// turnHeartbeatEvery that it still holds it. Nothing else can be running under
// this instance's name: the server holds the name while it runs.
func (self *Agent) startComputerAttachments() {
	if self.settings.Database == nil || self.settings.Instance == "" {
		return
	}
	ctx, cancel := context.WithTimeout(self.ctx, computerAttachmentTimeout)
	defer cancel()
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.DeleteAgentComputerAttachmentsOfInstance(self.settings.Instance)
	}); err != nil {
		log.Warningf("cannot forget the computers this instance held before it restarted: %s", err)
	}
	// A computer that attached before this started was not recorded by
	// anything else.
	for agentId, computerNames := range self.attachedComputerNames() {
		for _, computerName := range computerNames {
			self.recordComputerAttachment(agentId, computerName)
		}
	}
	periodic.New(self.ctx, &self.waitGroup, func(ctx context.Context) error {
		return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.HeartbeatAgentComputerAttachments(self.settings.Instance, time.Now())
		})
	}, &periodic.Settings{Interval: turnHeartbeatEvery, Name: "agent:computer-heartbeat", SkipInitialRun: true}).Start()
}

// attachedComputerNames is the names of the computers attached here, by agent.
func (self *Agent) attachedComputerNames() map[string][]string {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	names := map[string][]string{}
	for agentId, computers := range self.computers {
		for computerName := range computers {
			names[agentId] = append(names[agentId], computerName)
		}
	}
	return names
}

// isComputerAttachedHere says a computer of that exact name is attached to
// this instance.
func (self *Agent) isComputerAttachedHere(agentId, computerName string) bool {
	self.computersMutex.Lock()
	defer self.computersMutex.Unlock()
	return self.computers[agentId][computerName] != nil
}

// recordComputerAttachment makes the database say what is true here of a
// computer: held by this instance when it is attached, and not held by it
// when it is not. What is true is read again after the write, so that a
// computer detaching and attaching again at once, from two connections, is not
// left unrecorded by the write of whichever finished last.
func (self *Agent) recordComputerAttachment(agentId, computerName string) {
	if self.settings == nil || self.settings.Database == nil || self.settings.Instance == "" || self.ctx == nil {
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		isAttached := self.isComputerAttachedHere(agentId, computerName)
		// Not cancelled with the server: an instance stopping lets go of
		// its computers, rather than leave them to go stale.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), computerAttachmentTimeout)
		err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			if isAttached {
				return tx.PutAgentComputerAttachment(agentId, computerName, self.settings.Instance, time.Now())
			}
			return tx.DeleteAgentComputerAttachment(agentId, computerName, self.settings.Instance)
		})
		cancel()
		if err != nil {
			log.Warningf("cannot say whether this instance holds the computer %q of agent %q: %s", computerName, agentId, err)
			return
		}
		if self.isComputerAttachedHere(agentId, computerName) == isAttached {
			return
		}
	}
}

// computerElsewhere is the instance that holds a computer other than this
// one, or empty when none that has been heard of lately does. An empty
// computerName is any computer of the agent.
func (self *Agent) computerElsewhere(ctx context.Context, agentId, computerName string) string {
	if self.settings.Database == nil || self.settings.Instance == "" {
		return ""
	}
	var instance string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		instance, err = tx.FindAgentComputerAttachment(agentId, computerName, time.Now().Add(-turnStaleAfter))
		return err
	}); err != nil {
		log.Warningf("cannot look for the instance holding the computer %q of agent %q: %s", computerName, agentId, err)
		return ""
	}
	if instance == self.settings.Instance {
		return ""
	}
	return instance
}

// deferredToComputerInstance is a Deferral for a job whose computer another
// instance holds. It is an expected hop between instances, not a job that
// went wrong, so it is not reported at the level of one (see isQuietDeferral).
type deferredToComputerInstance struct {
	Deferral
}

func (self *deferredToComputerInstance) Unwrap() error { return &self.Deferral }

// elsewhereDeferral is what a job returns when its computer is held by the
// named instance.
func elsewhereDeferral(instance string) error {
	return &deferredToComputerInstance{Deferral{
		Until:  time.Now().Add(computerElsewhereRetry),
		Reason: fmt.Sprintf("the computer is held by instance %q", instance),
	}}
}

// isQuietDeferral says the deferral is one that comes of the instances taking
// turns, and so is not worth a line in the log each time.
func isQuietDeferral(err error) bool {
	var elsewhere *deferredToComputerInstance
	return errors.As(err, &elsewhere)
}
