package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	//
	// And no more than a call holds: messages are shown whole now, so a
	// run stops where they would pass digestBatchRunes, and the rest wait
	// for the next run as the rest of sixty do.
	backlog := 0
	if len(unread) > rememberMessages {
		backlog = len(unread) - rememberMessages
		unread = unread[:rememberMessages]
	}
	shownRunes := 0
	for index, message := range unread {
		shownRunes += utf8.RuneCountInString(shownText(message))
		if index > 0 && shownRunes > digestBatchRunes {
			backlog += len(unread) - index
			unread = unread[:index]
			break
		}
	}

	// The same window again, with the commands run in it, for what the work
	// taught. Read before the filing, because the mark the filing moves is
	// the lessons' mark too: a window past it is never read again. When a
	// part of a long window was not answered, the filing stops where the
	// lessons stopped, and the rest of the window waits for the next run
	// as a backlog does.
	unread, read, backlog, err := self.readLessonsOf(ctx, run, conversation, messages, unread, backlog)
	if err != nil {
		return err
	}
	said, transcript, err := self.askWhatWasLearned(ctx, run, conversation, unread)
	if err != nil {
		return err
	}
	if !said.IsValid {
		if err := self.anUnreadableAnswer(ctx, run, conversation, transcript, read, len(unread), said.Problem); err != nil {
			return err
		}
		return deferTheBacklog(backlog)
	}
	answer := &said.Value
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
				things := "things"
				if filed.Filed == 1 {
					things = "thing"
				}
				note = fmt.Sprintf("Filed %d %s from %s", filed.Filed, things, chatName(conversation))
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
	return deferTheBacklog(backlog)
}

// readLessonsOf reads the lessons of the window the filing is about to
// cover, and gives back the messages to file, the last of them and the
// backlog, cut back to what the lessons read when a part was not answered.
// When not even the first part was answered nothing is filed: the run
// fails and is tried again with the mark where it was, as it does when the
// filing's own call fails. Lessons a retry reads again are the same in
// meaning as those filed and are not filed twice.
func (self *Agent) readLessonsOf(ctx context.Context, run *Run, conversation *models.AgentConversation, messages, unread []*models.AgentMessage, backlog int) ([]*models.AgentMessage, *models.AgentMessage, int, error) {
	read := unread[len(unread)-1]
	window := lessonWindowOf(messages, conversation.RememberedThrough, read)
	filedCount, readCount, err := self.readLessons(ctx, run, conversation, window)
	if filedCount > 0 {
		log.Debugf("filed %d lesson(s) from conversation %s", filedCount, conversation.ID)
	}
	if err == nil {
		return unread, read, backlog, nil
	}
	isRead := make(map[string]bool, readCount)
	for _, message := range window[:readCount] {
		isRead[message.ID] = true
	}
	keptCount := 0
	for keptCount < len(unread) && isRead[unread[keptCount].ID] {
		keptCount++
	}
	if keptCount > 0 {
		log.Warningf("cannot read the rest of the lessons from conversation %s, filing %d of %d messages and leaving the rest for the next run: %s",
			conversation.ID, keptCount, len(unread), err)
		return unread[:keptCount], unread[keptCount-1], backlog + len(unread) - keptCount, nil
	}
	return nil, nil, 0, fmt.Errorf("reading the lessons: %w", err)
}

// lessonWindowOf is every message the filing above covered, tool calls
// and results included, which the filing left out: from just after the
// previous mark, not from the first message worth filing, so commands run
// before it in the window are read; and through the results answering the
// calls of the last message read, which come after it and the next window
// would start past.
func lessonWindowOf(messages []*models.AgentMessage, previousMark string, last *models.AgentMessage) []*models.AgentMessage {
	start, end := 0, -1
	for index, message := range messages {
		if previousMark != "" && message.ID == previousMark {
			start = index + 1
		}
		if message.ID == last.ID {
			end = index
		}
	}
	if end < start {
		return nil
	}
	for end+1 < len(messages) && messages[end+1].Role == string(llm.RoleTool) {
		end++
	}
	return messages[start : end+1]
}

// deferTheBacklog puts the job straight back into the queue when there is
// more of the conversation to read, rather than waiting for the sweep to
// offer it again, which it does once a minute.
//
// A deferral and not another Enqueue: one job per agent, kind and subject
// is open at a time, and this job is the open one, so an Enqueue from
// inside it hands back the row it is already running and queues nothing
// at all.
func deferTheBacklog(backlog int) error {
	if backlog <= 0 {
		return nil
	}
	return &Deferral{
		Until:  time.Now(),
		Reason: fmt.Sprintf("%d more message(s) of this conversation are unread", backlog),
	}
}

// unreadableRememberAnswer begins the error a filing run fails with when
// its answer could not be read, which is how the next try knows it is
// the second.
const unreadableRememberAnswer = "the filing run's answer could not be read"

// anUnreadableAnswer is what a filing run does when the model's answer
// could not be read. The first time, the job fails and is tried again,
// and the mark stays where it was. The second time in a row the window is
// let go, because a window that blocks blocks the whole conversation --
// but said: the run's title names how many messages were skipped and
// why, where the person reading the runs will see it.
func (self *Agent) anUnreadableAnswer(ctx context.Context, run *Run, conversation, transcript *models.AgentConversation, read *models.AgentMessage, messageCount int, problem string) error {
	if !strings.HasPrefix(run.Job.Error, unreadableRememberAnswer) {
		self.retitle(ctx, run, transcript, fmt.Sprintf("Filing %s: the answer could not be read, to be tried again", chatName(conversation)))
		return fmt.Errorf("%s: %s", unreadableRememberAnswer, problem)
	}
	log.Warningf("skipping %d messages of conversation %s: the answer could not be read twice: %s", messageCount, conversation.ID, problem)
	self.retitle(ctx, run, transcript, fmt.Sprintf("Skipped %d messages of %s: the answer could not be read twice (%s)",
		messageCount, chatName(conversation), cutRunes(problem, 160)))
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkAgentConversationRemembered(conversation.ID, read.ID, time.Now())
	})
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

// chatName is how a run names the conversation it read. The main chat is
// shown as the main chat wherever the person sees it, and the title it
// carries is whatever it was called before it became the main chat.
func chatName(conversation *models.AgentConversation) string {
	switch {
	case conversation.Kind == models.AgentConversationMain:
		return "the main chat"
	case strings.TrimSpace(conversation.Title) == "":
		return "an untitled chat"
	}
	return strconv.Quote(conversation.Title)
}
