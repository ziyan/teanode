package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Thinking with something to hand.
//
// Sorting a message and drafting an answer to it were one call each: one
// prompt, one reply, and no way to ask a question first. So the sorting run
// could not tell whether this sender had ever written before, and the draft
// that said "Thursday works" had not looked at Thursday.
//
// Both are runs of the conversation loop now, with a small read-only set of
// tools and a cap on how many turns they may take -- the shape research has
// used since it was written. What is different about these two is that their
// answer is a JSON object rather than prose, so each of them reads the last
// thing the loop said. There is no other way to ask: every model call in
// this program is a run of the loop, one round and no tools where that is
// all it needs, so that every one of them is a transcript the person can
// open.

// thought is what a thinking run produced: the last thing it said, and the
// transcript to point the insight or the reply at.
type thought struct {
	Conversation *models.AgentConversation
	Text         string
	// Usage is what the turn cost, every round added up, for a caller
	// that keeps a budget of its own.
	Usage llm.Usage
}

// triageTools is what a sorting run may reach.
//
// Four, and the shortness is the point twice over: this runs on every message
// that arrives, so every tool definition is paid for on every message, and a
// tool the model has to read about is a tool it may be tempted to use. What
// is here is what sorting actually turns on -- the rest of the conversation,
// whether this sender has written before, who they are, and what day it is.
//
// Not the calendar: whether Thursday is free does not change what a message
// is. Not memory: what the person told the agent about sorting is already in
// the prompt, from the layer that carries their memories.
var triageTools = map[string]bool{
	"mail_read": true, "mail_search": true, "contact_book": true, "datetime": true,
}

// replyTools is what a drafting run may reach: everything sorting has, the
// diary, memory, and the web where the operator allows it. Answering somebody
// is where looking things up earns its keep -- "are you free Thursday" cannot
// be answered without the diary.
var replyTools = map[string]bool{
	"mail_read": true, "mail_search": true, "contact_book": true,
	"calendar": true, "memory": true, "datetime": true,
	"web_fetch": true, "web_search": true,
}

// think runs one headless, read-only turn with the given tools and returns
// the last thing it said.
//
// Read-only is not a detail: a sorting run that could write would be a
// stranger's message deciding what happens to a mailbox. The loop strips
// every tool that changes anything and refuses any call whose action turns
// out not to be a read, which is what lets the calendar be in the set at all.
//
// This is the one way any headless work asks a model anything. A call
// that needs no tools passes an empty allow set and one round, and gets a
// transcript of its prompt, its answer and what it cost, like every other
// run; the kind of work chooses the model, so the dream runs on the scan
// model and sorting on the triage one. The ask feature, which is the
// person's own chat, does not gate this: the feature that owns the work
// does, and the caller has checked it.
// thinkResultCharacters bounds what one lookup brings back into a
// headless run. A page of a hundred facts is twenty thousand characters,
// and a run given twenty documents to read that fetched three such pages
// overflowed a small model's window and lost the documents to the
// compaction; a lookup is for checking what is known, and this much of a
// page says it.
const thinkResultCharacters = 6000

func (self *Agent) think(ctx context.Context, run *Run, title, prompt string, allow map[string]bool, rounds int, kind models.AgentJobKind, work config.AgentWork) (*thought, error) {
	if self.operations == nil {
		return nil, fmt.Errorf("no way to act as the person")
	}
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		mailboxId := ""
		if run.Mailbox != nil {
			mailboxId = run.Mailbox.ID
		}
		// A run made in a request or inside a turn has no job; its kind
		// still names it in the activity table.
		jobId, subjectId := "", run.Subject
		if run.Job != nil {
			jobId, subjectId = run.Job.ID, run.Job.SubjectID
		}
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: run.Agent.ID, MailboxID: mailboxId, Kind: models.AgentConversationRun,
			Title: title, JobID: jobId, JobKind: string(kind), SubjectID: subjectId,
			Surface: string(kind), LastAt: time.Now(),
		})
		return err
	}); err != nil {
		return nil, err
	}
	operations, err := self.operations(ctx, run.Owner)
	if err != nil {
		return nil, err
	}
	turn, err := self.Ask(&AskSettings{
		Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation,
		Message: prompt, Surface: string(kind), ReadOnly: true, Short: true,
		Allow: allow, Headless: true, MaxRounds: rounds, UsageKind: string(kind), Work: work,
		ReadThenAnswer: true, ResultCharacters: thinkResultCharacters,
	})
	if err != nil {
		return nil, err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	said, failure := "", ""
	for event := range events {
		// The job's own deadline, not the agent's: a turn past its time is
		// stopped here, before another instance is handed the job.
		if ctx.Err() != nil {
			turn.Stop()
			break
		}
		switch event.Kind {
		case EventMessage:
			said = event.Text
		case EventError:
			failure = event.Error
		}
	}
	if failure != "" {
		// The run comes back with the error, so a caller can say on its
		// transcript what became of it.
		return &thought{Conversation: conversation, Usage: turn.Usage()}, fmt.Errorf("the %s turn failed: %s", kind, failure)
	}
	return &thought{Conversation: conversation, Text: strings.TrimSpace(said), Usage: turn.Usage()}, nil
}

// retitle says what the run turned out to be, now that it is over. The
// conversation was named before the answer existed, because the transcript
// has to exist for the loop to write into.
func (self *Agent) retitle(ctx context.Context, run *Run, conversation *models.AgentConversation, title string) {
	if conversation == nil || strings.TrimSpace(title) == "" {
		return
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := tx.UpdateAgentConversation(conversation.ID, func(found *models.AgentConversation) error {
			found.Title = title
			return nil
		})
		return err
	}); err != nil {
		log.Debugf("cannot retitle the run %q: %s", conversation.ID, err)
	}
}

// roundsFor is how many turns a thinking run may take, from the operator's
// limits with the plan's defaults behind them.
func roundsFor(configuration *config.Configuration, kind models.AgentJobKind) int {
	limits := configuration.Agent.Limits
	switch kind {
	case models.AgentJobTriage:
		if limits.MaxRoundsPerTriage > 0 {
			return limits.MaxRoundsPerTriage
		}
		return 3
	case models.AgentJobReply:
		if limits.MaxRoundsPerReply > 0 {
			return limits.MaxRoundsPerReply
		}
		return 6
	case models.AgentJobDream, models.AgentJobIngest:
		if limits.MaxRoundsPerDream > 0 {
			return limits.MaxRoundsPerDream
		}
		return 4
	}
	return 4
}

// noTools is the allow set of a call that needs none: one round, one
// answer, and the transcript a run gets.
var noTools = map[string]bool{}

// oneShot is a run that needs no tools: one round, one answer, and the
// transcript every run gets -- the prompt, the answer and what it cost.
func (self *Agent) oneShot(ctx context.Context, run *Run, title, prompt string, kind models.AgentJobKind, work config.AgentWork) (*thought, error) {
	return self.think(ctx, run, title, prompt, noTools, 1, kind, work)
}

// runFor is a run for work that no job queued: a draft asked for from the
// composer, a conversation being titled, a turn compacting its history.
func (self *Agent) runFor(agent *models.Agent, owner *models.User, mailbox *models.Mailbox, subject string) *Run {
	return &Run{Agent: agent, Owner: owner, Mailbox: mailbox, Subject: subject, Now: time.Now(), settings: self.settings}
}

// noteRun is a run that made no call at all, kept for what it says: a
// reply that was refused before any model was asked has a line in the
// activity table saying why.
func noteRun(tx db.Transaction, run *Run, note string) (*models.AgentConversation, error) {
	conversation, err := tx.CreateAgentConversation(&models.AgentConversation{
		AgentID: run.Agent.ID, MailboxID: run.Job.MailboxID, Kind: models.AgentConversationRun,
		Title: note, JobID: run.Job.ID, JobKind: string(run.Job.Kind), SubjectID: run.Job.SubjectID,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: models.AgentMessageNote, Content: note}); err != nil {
		return nil, err
	}
	return conversation, nil
}

// canThink says whether a run may use the loop at all: somebody to act as.
func (self *Agent) canThink(configuration *config.Configuration) bool {
	return self.operations != nil
}

// sortWithTools is the sorting run as a turn of the loop. It answers with the
// insight, or with nothing at all when the model did not end with the object
// the prompt asked for.
func (self *Agent) sortWithTools(ctx context.Context, run *Run, mail *models.Mail, prompt string) (*models.MailInsight, func(db.Transaction) (string, error), error) {
	thinking, err := self.think(ctx, run, fmt.Sprintf("Sorting %q", mail.Subject), prompt,
		triageTools, roundsFor(run.Configuration(), models.AgentJobTriage), models.AgentJobTriage, config.AgentWorkTriage)
	if err != nil {
		return nil, nil, err
	}
	answer, err := llm.Extract[TriageAnswer](thinking.Text)
	if err != nil {
		log.Warningf("the sorting run for %q did not answer with an object: %s", mail.ID, err)
		return nil, nil, nil
	}
	insight, err := InterpretTriage(&answer, run.Agent)
	if err != nil {
		return nil, nil, err
	}
	// The model this run used is named on the usage rows the loop wrote;
	// what goes on the insight is what the operator chose for sorting.
	insight.Model = run.Configuration().Agent.Models.ForWork(config.AgentWorkTriage)
	self.retitle(ctx, run, thinking.Conversation, sortingNote(mail, insight))
	return insight, func(db.Transaction) (string, error) { return thinking.Conversation.ID, nil }, nil
}

// draftWithTools is the answering run as a turn of the loop: the same object
// the single call answers with, written by a run that could read the thread,
// look the sender up and open the diary first.
//
// It hands back the answer and a way to record the run, or nothing at all
// when the model ended with something other than the object.
func (self *Agent) draftWithTools(ctx context.Context, run *Run, mail *models.Mail, prompt string) (*ReplyAnswer, func(db.Transaction, string) (string, error), error) {
	thinking, err := self.think(ctx, run, fmt.Sprintf("Answering %q", mail.Subject), prompt,
		replyTools, roundsFor(run.Configuration(), models.AgentJobReply), models.AgentJobReply, config.AgentWorkReply)
	if err != nil {
		return nil, nil, err
	}
	answer, err := llm.Extract[ReplyAnswer](thinking.Text)
	if err != nil {
		log.Warningf("the answering run for %q did not answer with an object: %s", mail.ID, err)
		return nil, nil, nil
	}
	return &answer, func(tx db.Transaction, note string) (string, error) {
		// The loop wrote the transcript as it went; what is left is to say
		// what the run turned out to be. In the caller's own transaction,
		// so that the title and the reply row are written together -- and
		// never in a transaction of its own, which would be a second one
		// opened inside the first.
		if _, err := tx.UpdateAgentConversation(thinking.Conversation.ID, func(found *models.AgentConversation) error {
			found.Title = note
			return nil
		}); err != nil {
			return "", err
		}
		return thinking.Conversation.ID, nil
	}, nil
}
