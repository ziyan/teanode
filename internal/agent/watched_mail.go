package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// Watched mail: what arrives in a mailbox this server does not host,
// noticed through the installed skill that reads it. A look searches for
// what arrived since the last one, reads each new message with the
// skill's own read, sorts it with the prompt hosted mail is sorted with,
// and makes an alert candidate of what the sorting says the person should
// hear about. Nothing else follows from the sorting: the rules, replies
// and lookups that follow it for hosted mail act on a message this server
// holds, and this one it does not.
//
// The look runs with nobody present, which a turn may not do on the
// person's computer (computer.Of). It is code, not a turn: it runs only
// the skill's search and read, never anything that sends, drafts or
// labels. docs/planning/watched-mail-execplan.md is the design.

const (
	// gmailSkillName is the skill the watch reads Gmail through, and
	// gmailSearchTool and gmailReadTool the two tools of it it runs.
	gmailSkillName  = "gmail"
	gmailSearchTool = "gmail_search"
	gmailReadTool   = "gmail_read"

	// watchedMailboxName is what the sorting is told the mailbox is.
	watchedMailboxName = "Gmail"

	// watchEvery is how often a look is queued for each person.
	watchEvery = 10 * time.Minute

	// watchOverlap is how far before the newest message already looked at
	// a look starts, for a message the mailbox indexed late; the row per
	// message keeps one from being sorted twice.
	watchOverlap = 10 * time.Minute

	// watchFirstLookBack is how far back the first look reaches. Older
	// mail is not news, and sorting a day of it at once would be a burst
	// of calls the person did not ask for.
	watchFirstLookBack = 2 * time.Hour

	// watchMessagesAtOnce bounds the messages one look sorts. The oldest
	// go first, so what is left is found by the next look.
	watchMessagesAtOnce = 25

	// watchedMessageCharacters is how much of a message a candidate keeps
	// for the alert job to read.
	watchedMessageCharacters = 4000

	// watchedMailKept is how long the record of a message looked at is
	// kept: far longer than any look reaches back.
	watchedMailKept = 30 * 24 * time.Hour
)

// gmailWatchQuery is the search a look sends: everything that arrived
// since the moment given, archived or not, except what the person sent or
// is writing, what Gmail already filed as spam or trash, and the
// promotions and social tabs, which the sorting would leave alone anyway.
func gmailWatchQuery(since time.Time) string {
	return fmt.Sprintf("after:%d -in:sent -in:drafts -in:chats -in:spam -in:trash -category:promotions -category:social", since.Unix())
}

// watchSince is where a look starts: shortly before the newest message
// already looked at, or a little while back when nothing has been, and
// never further back than an alert could still be news.
func watchSince(latest *time.Time, now time.Time) time.Time {
	since := now.Add(-watchFirstLookBack)
	if latest != nil {
		since = latest.Add(-watchOverlap)
	}
	if earliest := now.Add(-alertFreshness); since.Before(earliest) {
		since = earliest
	}
	return since
}

// gmailThread is one thread the search listed.
type gmailThread struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Labels       []string `json:"labels"`
	MessageCount int      `json:"messageCount"`
}

// gmailThreadMessage is one message of a thread, as the thread read lists
// it: enough to say whether it is new and whether the person wrote it.
type gmailThreadMessage struct {
	ID           string   `json:"id"`
	InternalDate string   `json:"internalDate"`
	LabelIDs     []string `json:"labelIds"`
}

// gmailMessage is one message as the message read prints it: its headers,
// its body as the sender wrote it, and Gmail's own record of it.
type gmailMessage struct {
	Body    string             `json:"body"`
	Headers map[string]string  `json:"headers"`
	Message gmailThreadMessage `json:"message"`
}

// skillText is what a skill's command printed, read as the JSON it was
// asked for. A command that ended badly prints its complaint instead,
// which is said as the error.
func skillText(answer map[string]any) (string, error) {
	text, _ := answer["text"].(string)
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return "", fmt.Errorf("the skill answered something other than JSON: %s", cutRunes(text, 300))
	}
	return text, nil
}

// parseGmailSearch reads the threads a search listed.
func parseGmailSearch(answer map[string]any) ([]gmailThread, error) {
	text, err := skillText(answer)
	if err != nil {
		return nil, err
	}
	var listed struct {
		Threads []gmailThread `json:"threads"`
	}
	if err := json.Unmarshal([]byte(text), &listed); err != nil {
		return nil, fmt.Errorf("the search's answer cannot be read: %w", err)
	}
	return listed.Threads, nil
}

// parseGmailThread reads the messages a thread read listed.
func parseGmailThread(answer map[string]any) ([]gmailThreadMessage, error) {
	text, err := skillText(answer)
	if err != nil {
		return nil, err
	}
	var read struct {
		Thread struct {
			Messages []gmailThreadMessage `json:"messages"`
		} `json:"thread"`
	}
	if err := json.Unmarshal([]byte(text), &read); err != nil {
		return nil, fmt.Errorf("the thread's answer cannot be read: %w", err)
	}
	return read.Thread.Messages, nil
}

// parseGmailMessage reads one message.
func parseGmailMessage(answer map[string]any) (*gmailMessage, error) {
	text, err := skillText(answer)
	if err != nil {
		return nil, err
	}
	var read gmailMessage
	if err := json.Unmarshal([]byte(text), &read); err != nil {
		return nil, fmt.Errorf("the message's answer cannot be read: %w", err)
	}
	if read.Message.ID == "" {
		return nil, fmt.Errorf("the message's answer has no message in it")
	}
	return &read, nil
}

// at is when Gmail received the message, or the zero time when it does
// not say.
func (self *gmailThreadMessage) at() time.Time {
	milliseconds, err := strconv.ParseInt(self.InternalDate, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds)
}

// isWrittenByThePerson says the person sent the message or is still
// writing it: nothing to tell them about.
func (self *gmailThreadMessage) isWrittenByThePerson() bool {
	return slices.Contains(self.LabelIDs, "SENT") || slices.Contains(self.LabelIDs, "DRAFT")
}

// watchedMessageContext is a message as the sorting reads it, with what
// this server can say about where it came from in place of the checks it
// makes on mail it receives itself.
func watchedMessageContext(message *gmailMessage, maximumCharacters int) *MessageContext {
	header := func(name string) string { return strings.TrimSpace(message.Headers[name]) }
	text := message.Body
	if strings.Contains(strings.ToLower(text), "<html") || strings.Contains(strings.ToLower(text), "<body") || strings.Contains(strings.ToLower(text), "<div") {
		text = tools.HTMLToText(text)
	}
	text = strings.TrimSpace(text)
	isTruncated := false
	if maximumCharacters > 0 && len([]rune(text)) > maximumCharacters {
		text, isTruncated = cutRunes(text, maximumCharacters), true
	}
	facts := []string{"read from the person's Gmail through the gmail skill; this server did not receive it, so it has no authentication results of its own"}
	if labels := message.Message.LabelIDs; len(labels) > 0 {
		facts = append(facts, "Gmail labels: "+strings.Join(labels, ", "))
		if !slices.Contains(labels, "INBOX") {
			facts = append(facts, "not in the inbox: archived, or filed by a filter of the person's")
		}
	}
	return &MessageContext{
		MailID: message.Message.ID, From: header("from"), To: header("to"), Cc: header("cc"),
		Date: header("date"), Subject: header("subject"), Text: text, Truncated: isTruncated, Facts: facts,
	}
}

// queueWatching queues a look for each person whose mail can be watched:
// the skill is installed and on, they want alerts, and a computer of
// theirs is attached for the skill to run on.
func (self *Agent) queueWatching(ctx context.Context, now time.Time) {
	if now.Sub(self.lastWatch) < watchEvery {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "skills") || !FeatureAllowed(configuration, "triage") || !self.canThink(configuration) {
		return
	}
	self.lastWatch = now
	var agents []*models.Agent
	isInstalled := false
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		installed, err := tx.ListAgentSkills()
		if err != nil {
			return err
		}
		for _, row := range installed {
			isInstalled = isInstalled || (row.Enabled && row.Name == gmailSkillName)
		}
		if !isInstalled {
			return nil
		}
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot say whose mail to watch: %s", err)
		return
	}
	for _, agent := range agents {
		if !isAlertingAllowed(configuration, agent, nil) || len(self.computersFor(agent.ID)) == 0 {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobWatch, agent.ID, "", gmailSkillName)
			return err
		}); err != nil {
			log.Warningf("cannot queue a look at the mail of agent %q: %s", agent.ID, err)
		}
	}
}

// watchedSkill is the installed skill a look reads through, or nil when it
// is gone or switched off.
func watchedSkill(tx db.Transaction, skillName string) (*skills.Skill, error) {
	installed, err := tx.ListAgentSkills()
	if err != nil {
		return nil, err
	}
	for _, row := range installed {
		if row.Name != skillName || !row.Enabled {
			continue
		}
		return skills.Parse([]byte(row.Content))
	}
	return nil, nil
}

// watchComputer is the computer a look runs the skill on: the one the
// person chose for the skill, or else the only one attached. Nil when
// neither is attached.
func (self *Agent) watchComputer(ctx context.Context, agentId, skillName string) *attachedComputer {
	computers := self.computersFor(agentId)
	chosen := self.reachOf(ctx, agentId, models.AgentReachSkill, skillName)
	for _, computer := range computers {
		if chosen != "" && strings.EqualFold(computer.name, chosen) {
			return computer
		}
	}
	if chosen == "" && len(computers) == 1 {
		return computers[0]
	}
	return nil
}

// watchedMessageReference is a message a look found that may be new.
type watchedMessageReference struct {
	watchedMessageId string
	watchedMessageAt time.Time
}

// runWatch is the handler for a watch job: one look.
func (self *Agent) runWatch(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	skillName := run.Job.SubjectID
	if skillName != gmailSkillName || !FeatureAllowed(configuration, "skills") || !isAlertingAllowed(configuration, run.Agent, nil) {
		return nil
	}
	if !self.canThink(configuration) {
		return fmt.Errorf("no way to act as the person")
	}
	now := time.Now()
	var skill *skills.Skill
	var latest *time.Time
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, now); err != nil {
			return err
		}
		if skill, err = watchedSkill(tx, skillName); err != nil || skill == nil {
			return err
		}
		if err := tx.DeleteAgentWatchedMailBefore(run.Agent.ID, skillName, now.Add(-watchedMailKept)); err != nil {
			return err
		}
		latest, err = tx.LatestAgentWatchedMailAt(run.Agent.ID, skillName)
		return err
	}); err != nil {
		return err
	}
	if skill == nil {
		return nil // uninstalled or switched off since it was queued
	}
	computer := self.watchComputer(ctx, run.Agent.ID, skillName)
	if computer == nil {
		return nil // the next look, once one is attached
	}
	running := &skills.Running{Shell: &computerShell{attached: computer}}
	since := watchSince(latest, now)

	searched, err := skill.Run(ctx, gmailSearchTool, map[string]any{"action": "search", "query": gmailWatchQuery(since)}, running)
	if err != nil {
		return fmt.Errorf("the %s skill's search on %s: %w", skillName, computer.name, err)
	}
	threads, err := parseGmailSearch(searched)
	if err != nil {
		return fmt.Errorf("the %s skill's search on %s: %w", skillName, computer.name, err)
	}
	var found []watchedMessageReference
	for _, thread := range threads {
		if thread.MessageCount <= 1 {
			// A thread of one message is known by that message's id.
			found = append(found, watchedMessageReference{watchedMessageId: thread.ID})
			continue
		}
		// A reply in an older thread: which of its messages are new is
		// only in the thread.
		read, err := skill.Run(ctx, gmailReadTool, map[string]any{"action": "thread", "id": thread.ID}, running)
		if err != nil {
			return fmt.Errorf("the %s skill's read of a thread on %s: %w", skillName, computer.name, err)
		}
		messages, err := parseGmailThread(read)
		if err != nil {
			return fmt.Errorf("the %s skill's read of a thread on %s: %w", skillName, computer.name, err)
		}
		for _, message := range messages {
			if at := message.at(); message.ID != "" && !message.isWrittenByThePerson() && !at.Before(since) {
				found = append(found, watchedMessageReference{watchedMessageId: message.ID, watchedMessageAt: at})
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	var looked map[string]bool
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		ids := make([]string, 0, len(found))
		for _, reference := range found {
			ids = append(ids, reference.watchedMessageId)
		}
		looked, err = tx.ListAgentWatchedMailLooked(run.Agent.ID, skillName, ids)
		return err
	}); err != nil {
		return err
	}
	var fresh []watchedMessageReference
	for _, reference := range found {
		if !looked[reference.watchedMessageId] {
			looked[reference.watchedMessageId] = true
			fresh = append(fresh, reference)
		}
	}
	// The search lists the newest first; the oldest are sorted first, so
	// that a look cut short leaves the newest for the next one, which
	// starts from the newest message this one looked at.
	slices.Reverse(fresh)
	if len(fresh) > watchMessagesAtOnce {
		fresh = fresh[:watchMessagesAtOnce]
	}
	for _, reference := range fresh {
		if err := self.watchOne(ctx, run, skill, running, reference, now); err != nil {
			return err
		}
	}
	return nil
}

// watchOne reads one new message, sorts it, and records that it was
// looked at, with the candidate the sorting makes of it, in one
// transaction.
func (self *Agent) watchOne(ctx context.Context, run *Run, skill *skills.Skill, running *skills.Running, reference watchedMessageReference, now time.Time) error {
	configuration := run.Configuration()
	read, err := skill.Run(ctx, gmailReadTool, map[string]any{"action": "message", "id": reference.watchedMessageId}, running)
	if err != nil {
		return fmt.Errorf("the %s skill's read of a message: %w", skill.Name, err)
	}
	message, err := parseGmailMessage(read)
	if err != nil {
		return fmt.Errorf("the %s skill's read of a message: %w", skill.Name, err)
	}
	watchedMessageAt := message.Message.at()
	if watchedMessageAt.IsZero() {
		watchedMessageAt = reference.watchedMessageAt
	}
	if watchedMessageAt.IsZero() {
		watchedMessageAt = now
	}
	watched := &models.AgentWatchedMail{
		AgentID: run.Agent.ID, SkillName: skill.Name, WatchedMessageID: reference.watchedMessageId,
		WatchedMessageAt: watchedMessageAt, LookedAt: now,
	}
	if message.Message.isWrittenByThePerson() {
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.AddAgentWatchedMail(watched)
		})
	}
	messageContext := watchedMessageContext(message, configuration.Agent.Limits.MaxBodyCharacters)

	var memories, corrections []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if err := RequireBudget(tx, configuration, run.Agent, run.Owner, time.Now()); err != nil {
			return err
		}
		if memories, err = memoryLines(tx, run.Agent.ID, models.AudienceTriage, promptRunMemories, false); err != nil {
			return err
		}
		corrections, err = correctionLines(tx, run.Agent.ID, []models.AgentFeedbackKind{models.FeedbackFiled, models.FeedbackSorted})
		return err
	}); err != nil {
		return err
	}
	prompt, err := TriagePrompt(&TriageInput{
		Configuration: configuration, Agent: run.Agent, Owner: run.Owner, Mailbox: &models.Mailbox{Name: watchedMailboxName},
		Message: messageContext, Memories: memories, Corrections: corrections,
	})
	if err != nil {
		return err
	}
	thinking, err := self.oneShot(ctx, run, fmt.Sprintf("Sorting %s %q", watchedMailboxName, messageContext.Subject), prompt[1].Content, models.AgentJobWatch, config.AgentWorkTriage)
	if err != nil {
		return err
	}
	answer, err := llm.Extract[TriageAnswer](thinking.Text)
	if err != nil {
		return fmt.Errorf("the sorting of a %s message did not answer with an object: %w", watchedMailboxName, err)
	}
	insight, err := InterpretTriage(&answer, run.Agent)
	if err != nil {
		return err
	}
	self.retitle(ctx, run, thinking.Conversation, fmt.Sprintf("Sorted %s %q: %s, %s priority, alert %s. %s",
		watchedMailboxName, messageContext.Subject, insight.Category, insight.Priority, insight.AlertSignal, insight.Summary))
	watched.AlertSignal = insight.AlertSignal
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.AddAgentWatchedMail(watched); err != nil {
			return err
		}
		return noteWatchedCandidate(tx, run.Agent.ID, skill.Name, watched, messageContext, insight, now)
	})
}

// noteWatchedCandidate makes the candidate a watched message's sorting
// gives rise to, if any, and queues the job that decides on it. A message
// the person muted the sender, the domain or the kind of is written down
// and dropped at once, as hosted mail's is.
func noteWatchedCandidate(tx db.Transaction, agentId, skillName string, watched *models.AgentWatchedMail, messageContext *MessageContext, insight *models.MailInsight, now time.Time) error {
	if insight.AlertSignal != models.AlertSignalSoon && insight.AlertSignal != models.AlertSignalNow {
		return nil
	}
	if now.Sub(watched.WatchedMessageAt) > alertFreshness {
		return nil
	}
	watchedMessageAt := watched.WatchedMessageAt
	created, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{
		AgentID: agentId, CandidateKind: models.AlertCandidateWatched,
		AlertSignal: insight.AlertSignal, CandidateReason: insight.AlertReason,
		WatchedSkillName: skillName, WatchedMessageID: watched.WatchedMessageID,
		WatchedSender: messageContext.From, WatchedSubject: messageContext.Subject, WatchedMailCategory: insight.Category,
		WatchedMessageAt: &watchedMessageAt, WatchedMessageText: cutRunes(messageContext.Render(), watchedMessageCharacters),
	})
	if err != nil {
		return err
	}
	mutes, err := tx.ListAgentAlertMutes(agentId)
	if err != nil {
		return err
	}
	if mutedBy(mutes, candidateFacts(created, "", "")) != nil {
		return tx.DropAgentAlertCandidates([]string{created.ID}, alertMutedReason, now)
	}
	return queueAlert(tx, agentId, insight.AlertSignal == models.AlertSignalNow, now)
}
