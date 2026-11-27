package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
)

// A conversation is compacted while it is quiet, not while somebody waits
// on it. A turn whose history passed the soft limit asks for it, and once no
// turn has run in the conversation for a while the older turns are read
// into a note; the next turn starts compacted. Only past the hard limit does
// a turn compact before its round, on the reply's path, because the
// request would otherwise not fit.
//
// The soft limit is a size rather than a share of the window: a round costs
// what it sends and the person waits for all of it, so a model that can
// read half a million tokens is no reason to send it that many each time.
const (
	// askSoftHistoryTokens is the history past which a quiet conversation
	// is compacted, when the window is large enough to hold more.
	askSoftHistoryTokens = 100000

	// askHardWindowFifths is the part of a known window, in fifths, the
	// history may fill before a turn compacts it before its round: the rest
	// is the prompt, the tools and the answer, and the estimate is rough.
	askHardWindowFifths = 4

	// idleCompactionAttempts is how many times a compaction waits for the
	// conversation to fall quiet before giving up until the next turn asks.
	idleCompactionAttempts = 8

	// idleCompactionTimeout bounds one compaction, the notes of every chunk
	// included, and is how long another instance leaves it to this one.
	idleCompactionTimeout = 5 * time.Minute
)

// idleCompactionDelay is how long a conversation is quiet before it is
// compacted: past the moment a person reads the answer and writes again
// at once. A variable for the tests.
var idleCompactionDelay = 45 * time.Second

// softHistoryLimit is the history past which a conversation is compacted
// once it is quiet, for a model whose window is contextLength tokens, or
// zero when it is not known.
func softHistoryLimit(contextLength int) int {
	return min(askSoftHistoryTokens, historyLimit(contextLength))
}

// hardHistoryLimit is the history past which a turn compacts before its
// round, on the reply's path: four fifths of a known window, and
// askHistoryTokens when the window is not known, as before there was a
// soft limit.
func hardHistoryLimit(contextLength int) int {
	return max(askHistoryTokens, contextLength*askHardWindowFifths/5)
}

// isIdleCompactionDue says the turn that just ended left a history worth
// compacting while the conversation is quiet. A chat's turn, a member's turn
// and a read-then-answer run carry history of their own making and are not
// compacted.
func (self *AskRun) isIdleCompactionDue() bool {
	settings := self.settings
	if settings.Conversation == nil || settings.Conversation.ID == "" || settings.Chat != nil || settings.isMemberTurn() || settings.ReadThenAnswer {
		return false
	}
	return self.lastHistoryTokens > softHistoryLimit(self.lastContextLength)
}

// compactWhenIdle compacts the conversation of a finished turn once no turn
// has run in it for idleCompactionDelay, when the turn asked for it.
func (self *Agent) compactWhenIdle(run *AskRun) {
	if self.settings.Database == nil || !run.isIdleCompactionDue() {
		return
	}
	conversationId := run.settings.Conversation.ID
	go func() {
		for attempt := 0; attempt < idleCompactionAttempts; attempt++ {
			timer := time.NewTimer(idleCompactionDelay)
			select {
			case <-timer.C:
			case <-self.ctx.Done():
				timer.Stop()
				return
			}
			// A turn running now compacts what it needs itself; this one
			// waits for the quiet after it.
			if self.isTurnRunning(conversationId) {
				continue
			}
			self.waitGroup.Add(1)
			err := run.compactQuietly()
			self.waitGroup.Done()
			if err != nil {
				log.Warningf("cannot compact the quiet conversation %q: %s", conversationId, err)
			}
			return
		}
	}()
}

// compactQuietly compacts the conversation of this finished turn as it now
// stands, saying nothing to anyone watching: nobody is waiting on it. One
// instance does it at a time, and a conversation another turn has brought
// back under the soft limit is left alone.
func (self *AskRun) compactQuietly() error {
	ctx, cancel := context.WithTimeout(self.agent.ctx, idleCompactionTimeout)
	defer cancel()
	isTaken := false
	var history []llm.ChatMessage
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		conversation, err := tx.GetAgentConversation(self.settings.Conversation.ID)
		if err != nil || conversation == nil {
			return err
		}
		if isTaken, err = tx.TakeThrottle("compact:"+conversation.ID, 2*idleCompactionTimeout, time.Now()); err != nil || !isTaken {
			return err
		}
		// Where the last compaction left off, which a turn since may have
		// moved: the history is read from there.
		self.settings.Conversation.CompactedThrough = conversation.CompactedThrough
		history, err = self.loadHistory(tx)
		return err
	}); err != nil || !isTaken {
		return err
	}
	if llm.EstimateTokens(renderHistory(history)) <= softHistoryLimit(self.lastContextLength) {
		return nil
	}
	self.isQuietCompaction = true
	_, err := self.compact(ctx, nil, "", "", history, askTailMessages)
	return err
}
