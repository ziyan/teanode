package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds.
const (
	// rememberQuiet is how long a conversation rests before it is read.
	// Long enough that somebody thinking between messages is not filed
	// mid-thought; short enough that what they said this morning is on
	// the pages by lunch.
	rememberQuiet = 5 * time.Minute

	// rememberBatch is how many conversations one sweep queues.
	rememberBatch = 20

	// rememberEvery is how often the sweep looks.
	rememberEvery = time.Minute

	// rememberFacts is how many facts one run may file, and
	// rememberMessages how many messages it reads.
	rememberFacts    = 15
	rememberMessages = 60

	// rememberMessageCharacters is how much of one message goes in. A
	// tool's answer can be a page of JSON; what it taught is in the first
	// part of it.
	rememberMessageCharacters = 1500

	// rememberIndexTokens is how much of the index the run is shown, so
	// it files onto a page that exists rather than making a second one
	// beside it.
	rememberIndexTokens = 3000

	// rememberPages is how many of the pages the conversation already
	// touched are shown in full.
	rememberPages = 6

	// alreadySaidCandidates is how much of a page is read back to see
	// whether it already states a sentence. The same bound the nightly
	// pass reads a page with (firstSayingIt), because the two are asking
	// the same question at either end of the night.
	alreadySaidCandidates = 500
)

// queueRemembering queues a remember job for every conversation that has
// gone quiet with something in it nobody has filed.
//
// Queued from the sweep rather than at the end of a turn so that a burst
// of messages coalesces into one run, and so that a turn started from a
// chat app, a terminal or the drawer is treated the same way.
func (self *Agent) queueRemembering(ctx context.Context, now time.Time) {
	if now.Sub(self.lastRemember) < rememberEvery || self.settings.Registry == nil {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "remember") || !FeatureAllowed(configuration, "ask") {
		return
	}
	self.lastRemember = now

	var due []*models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		due, err = tx.ListAgentConversationsToRemember(now.Add(-rememberQuiet), rememberBatch)
		return err
	}); err != nil {
		log.Warningf("cannot list the conversations to file: %s", err)
		return
	}
	for _, conversation := range due {
		if ctx.Err() != nil {
			return
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobRemember, conversation.AgentID, "", conversation.ID)
			return err
		}); err != nil {
			log.Warningf("cannot queue the filing of conversation %q: %s", conversation.ID, err)
		}
	}
}

// runRemember is the handler for a remember job; its subject is the
// conversation.
func (self *Agent) runRemember(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "remember") {
		return nil
	}
	if !self.canThink(configuration) {
		return fmt.Errorf("no way to act as the person")
	}

	var conversation *models.AgentConversation
	var messages []*models.AgentMessage
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		found, err := tx.GetAgentConversation(run.Job.SubjectID)
		if err != nil || found == nil {
			return err
		}
		if found.AgentID != run.Agent.ID {
			return nil
		}
		conversation = found
		messages, err = tx.ListAgentMessages(found.ID, nil)
		return err
	}); err != nil {
		return err
	}
	if conversation == nil {
		return nil // gone since it was queued
	}

	// Everything after the mark, which is where the last run stopped.
	unread := messages
	if conversation.RememberedThrough != "" {
		for index, message := range messages {
			if message.ID == conversation.RememberedThrough {
				unread = messages[index+1:]
				break
			}
		}
	}
	unread = worthReading(unread)
	if len(unread) == 0 {
		// Nothing to file, but the mark still moves: otherwise a
		// conversation of nothing but tool chatter comes round every
		// minute for ever.
		return self.markRemembered(ctx, conversation, messages)
	}
	// The oldest sixty, not the newest sixty. Cutting from the end and
	// then moving the mark to the last message of the whole list said
	// that everything in between had been filed, and nothing ever came
	// back for it: a conversation with two hundred unread messages had
	// its first hundred and forty marked read without being read. A
	// backlog is worked through oldest first, sixty at a time, over as
	// many runs as it takes.
	backlog := 0
	if len(unread) > rememberMessages {
		backlog = len(unread) - rememberMessages
		unread = unread[:rememberMessages]
	}

	answer, transcript, err := self.askWhatWasLearned(ctx, run, conversation, unread)
	if err != nil {
		return err
	}
	theirWords := make(map[string]bool, len(unread))
	// What the run put in front of the model, which is the only thing a
	// fact from it may cite and the only words it may quote.
	shown := make(map[string]string, len(unread))
	for _, message := range unread {
		if message.Role == string(llm.RoleUser) {
			theirWords[message.ID] = true
		}
		shown[message.ID] = shownText(message)
	}
	// The last message this run was actually given, so the mark never
	// stands past something nobody read.
	read := unread[len(unread)-1]
	// The mark moves in the same transaction as the writes it is a
	// promise about. It says that everything behind it has been filed,
	// and it was moved separately and unconditionally: a fact whose write
	// failed was logged and stepped over, the run reported success, and
	// the mark went past the whole window. Those messages were never read
	// again. A window now lands whole or not at all, and a run that
	// cannot write leaves the mark where it was for the next one -- the
	// job is queued again, and the deferral below drains what is left.
	//
	// The loop wrote the transcript as it went; what is left is to say
	// what the run turned out to be.
	filed, err := self.fileWhatWasLearned(ctx, run, answer, theirWords, models.EvidenceConversation, shown,
		func(tx db.Transaction, filed whatWasFiled) error {
			note := "Filed nothing from this conversation"
			if filed.Filed > 0 {
				note = fmt.Sprintf("Filed %d thing(s) from %q", filed.Filed, conversation.Title)
			}
			// How much of what it filed could not be shown to have been
			// said. On the row rather than in a log line, because the
			// person reading the runs is the one who would want to know.
			if checked := filed.Describe(); checked != "" {
				note += ", " + checked
			}
			if transcript != nil {
				if _, err := tx.UpdateAgentConversation(transcript.ID, func(found *models.AgentConversation) error {
					found.Title = note
					return nil
				}); err != nil {
					return err
				}
			}
			return tx.MarkAgentConversationRemembered(conversation.ID, read.ID, time.Now())
		})
	if err != nil {
		return err
	}
	if filed.Filed > 0 {
		log.Debugf("filed %d fact(s) from conversation %s", filed.Filed, conversation.ID)
	}
	if backlog > 0 {
		// Straight back into the queue rather than waiting for the sweep
		// to offer the conversation again, which it does once a minute.
		//
		// A deferral and not another Enqueue: one job per agent, kind and
		// subject is open at a time, and this job is the open one, so an
		// Enqueue from inside it hands back the row it is already running
		// and queues nothing at all.
		return &Deferral{
			Until:  time.Now(),
			Reason: fmt.Sprintf("%d more message(s) of this conversation are unread", backlog),
		}
	}
	return nil
}

// markRemembered moves the mark without filing anything.
func (self *Agent) markRemembered(ctx context.Context, conversation *models.AgentConversation, messages []*models.AgentMessage) error {
	if len(messages) == 0 {
		return nil
	}
	last := messages[len(messages)-1]
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentConversationRemembered(conversation.ID, last.ID, time.Now())
	})
}

// worthReading is the messages a filing run has any use for: what the
// person said and what the agent answered. A tool's call and its result
// are left out -- they are how the answer was arrived at, not what was
// learned, and they are most of the tokens.
func worthReading(messages []*models.AgentMessage) []*models.AgentMessage {
	kept := make([]*models.AgentMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case string(llm.RoleUser), string(llm.RoleAssistant):
			if strings.TrimSpace(message.Content) != "" {
				kept = append(kept, message)
			}
		}
	}
	return kept
}
