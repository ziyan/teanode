package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// MailInsight is what the agent worked out about one message for one
// mailbox: its category and priority, whether it needs an answer, a line
// of summary, the things it asks for. Rules read it in their second phase
// and the list shows it as chips. Two people who received the same message
// each have their own, because their agents were told different things.
type MailInsight struct {
	MailID    string `json:"mailId"`
	MailboxID string `json:"mailboxId"`
	AgentID   string `json:"agentId"`

	Category   string `json:"category"`
	Priority   string `json:"priority"` // high | normal | low
	NeedsReply bool   `json:"needsReply"`

	// ResearchAsked says triage thought a lookup would help; the research
	// run, when it exists, writes Notes.
	ResearchAsked bool `json:"researchAsked"`

	Summary     string   `json:"summary"`
	ActionItems []string `json:"actionItems"`

	// Notes are what research found, shown above the message and read by
	// the reply run. NotesRunID is the run that wrote them.
	Notes      string `json:"notes,omitempty"`
	NotesRunID string `json:"notesRunId,omitempty"`

	// ExtractAsked says triage thought the message carries an appointment or
	// somebody's details in its words; the extract run, when it exists,
	// writes Proposals.
	ExtractAsked bool `json:"extractAsked"`

	// Proposals are what was found in the words and is offered in the
	// reader. Nothing here has been written anywhere.
	Proposals []MailProposal `json:"proposals"`

	// AlertSignal is whether the sorting thought the person should hear
	// about this message without opening their mail: AlertSignalNone,
	// AlertSignalSoon or AlertSignalNow. AlertReason is its line on why.
	AlertSignal string `json:"alertSignal"`
	AlertReason string `json:"alertReason,omitempty"`

	Model     string    `json:"model"`
	RunID     string    `json:"runId"`
	CreatedAt time.Time `json:"createdAt"`
}

// MailProposal is something a message carries that belongs somewhere else: an
// appointment in its words, or a person's details in a signature.
//
// It is an offer and never a write. Putting an appointment in somebody's
// diary because a stranger's message mentioned a day is how a calendar stops
// being trusted, so this is what the reader draws a card from and nothing
// happens until the person presses something.
type MailProposal struct {
	// Kind is "event" or "contact".
	Kind string `json:"kind"`

	// Because is the line of the message it was read out of, quoted, so the
	// person can see what the agent thought it saw.
	Because string `json:"because,omitempty"`

	// Status is "" while it is still an offer, then "accepted" or
	// "dismissed". A dismissed proposal is not offered again.
	Status string `json:"status,omitempty"`

	// An event: what it is, when, and where.
	Summary  string `json:"summary,omitempty"`
	Starts   string `json:"starts,omitempty"`
	Ends     string `json:"ends,omitempty"`
	Location string `json:"location,omitempty"`
	AllDay   bool   `json:"allDay,omitempty"`

	// A person: what the signature said. ContactID names the contact this
	// would change rather than add, when the address book already holds
	// somebody with one of these addresses.
	Name         string   `json:"name,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Title        string   `json:"title,omitempty"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`
	Note         string   `json:"note,omitempty"`
	ContactID    string   `json:"contactId,omitempty"`
}

// The kinds of proposal, and the states one can be in.
const (
	MailProposalEvent   = "event"
	MailProposalContact = "contact"

	MailProposalOffered   = ""
	MailProposalAccepted  = "accepted"
	MailProposalDismissed = "dismissed"
)

// ThreadSummary is what the agent wrote about a conversation for one
// mailbox, and how far into the conversation it read. It is rewritten from
// the previous one when the conversation grows, so a long conversation is
// never sent whole twice.
type ThreadSummary struct {
	ThreadID  string `json:"threadId"`
	MailboxID string `json:"mailboxId"`
	AgentID   string `json:"agentId"`

	Summary string `json:"summary"`

	// ThroughMailID is the newest message the summary covers; a
	// conversation whose newest message is another one has outgrown it.
	ThroughMailID string `json:"throughMailId"`
	MessageCount  int    `json:"messageCount"`

	Model     string    `json:"model"`
	RunID     string    `json:"runId"`
	CreatedAt time.Time `json:"createdAt"`
}

// AgentConversationKind tells a person's conversations from a run's record.
type AgentConversationKind string

// The kinds: the one continuous conversation, a named one kept apart, the
// transcript of a run with nobody present, and a goal's own conversation,
// where the turns the agent takes toward it run out of the person's sight.
const (
	AgentConversationMain  AgentConversationKind = "main"
	AgentConversationNamed AgentConversationKind = "named"
	AgentConversationRun   AgentConversationKind = "run"
	AgentConversationGoal  AgentConversationKind = "goal"
)

// AgentConversation is a conversation or a run transcript.
type AgentConversation struct {
	ID         string                `json:"id"`
	CreatedAt  time.Time             `json:"createdAt"`
	ModifiedAt time.Time             `json:"modifiedAt"`
	AgentID    string                `json:"agentId"`
	MailboxID  string                `json:"mailboxId,omitempty"`
	Kind       AgentConversationKind `json:"kind"`
	Title      string                `json:"title"`

	// Summary is a line on what the conversation is about, rewritten by
	// the model as it goes; TitledBy is "person" once they named it
	// themselves, after which the model leaves the title alone.
	Summary  string `json:"summary,omitempty"`
	TitledBy string `json:"titledBy,omitempty"`

	// DescribedAt is when the title and summary were last written; a
	// conversation with something said since, once it has gone quiet, is
	// described again.
	DescribedAt *time.Time `json:"describedAt,omitempty"`

	// RememberedThrough is the last message a run that files what the
	// conversation taught has read, and RememberedAt when it last ran.
	// The identifier rather than the time, so a run that dies re-reads
	// from where it was rather than skipping what arrived meanwhile.
	RememberedThrough string     `json:"-"`
	RememberedAt      *time.Time `json:"rememberedAt,omitempty"`

	// JobID, JobKind and SubjectID say which run this is the record of.
	JobID     string `json:"jobId,omitempty"`
	JobKind   string `json:"jobKind,omitempty"`
	SubjectID string `json:"subjectId,omitempty"`

	// Surface is where a conversation is held: drawer, page, cli, api, mail.
	Surface string `json:"surface,omitempty"`

	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	LastAt     time.Time  `json:"lastAt"`

	// CompactedThrough is the last message the latest compaction note
	// stands in for; the verbatim transcript resumes after it. The note
	// itself is a later message with the compaction role.
	CompactedThrough string `json:"compactedThrough,omitempty"`

	// Goal is the standing instruction on this conversation, in the
	// person's words: what the agent keeps working toward across turns of
	// its own until it is met or they clear it. Empty means there is
	// none, and GoalState is empty with it.
	//
	// GoalNote is the agent's last word on where it is -- a sentence or
	// two, shown beside the conversation and, while it waits, above the
	// composer. GoalNextAt is when the agent takes its next turn on its
	// own; nothing is scheduled while it waits for the person or once the
	// goal is met.
	Goal       string         `json:"goal,omitempty"`
	GoalTitle  string         `json:"goalTitle,omitempty"`
	GoalState  AgentGoalState `json:"goalState,omitempty"`
	GoalNote   string         `json:"goalNote,omitempty"`
	GoalNextAt *time.Time     `json:"goalNextAt,omitempty"`
	// GoalTitle is what a goal is called in a list: a few words. Goal is
	// then the description, what it is for and what done looks like.
	//
	// GoalSetAt is when the goal was set, for the panel that says since
	// when the agent has been at it.
	GoalSetAt *time.Time `json:"goalSetAt,omitempty"`

	// GoalSurfacedAt is when a goal that came to need the person was said
	// in the main conversation; nil while it waits to be, and for a goal
	// that never needed them.
	GoalSurfacedAt *time.Time `json:"goalSurfacedAt,omitempty"`

	// GoalOriginConversationID is the conversation the goal was asked for
	// in, when it was asked for in one.
	GoalOriginConversationID string `json:"goalOriginConversationId,omitempty"`

	// BackgroundWakeCount is how many turns ended background commands and
	// finished background work have woken here since the person last
	// wrote, which bounds a chain of them.
	BackgroundWakeCount int `json:"backgroundWakeCount,omitempty"`
}

// AgentGoalState is where a conversation's goal stands.
type AgentGoalState string

// The states a goal is in. A goal that is working takes turns of the
// agent's own; one that is waiting takes none until the person writes
// again, and their next turn puts it back to working; one that is met is
// done, and its text stays on the conversation until they clear it; one
// that is dropped is one the person stopped.
const (
	GoalWorking AgentGoalState = "working"
	GoalWaiting AgentGoalState = "waiting"
	GoalMet     AgentGoalState = "met"
	GoalDropped AgentGoalState = "dropped"
)

// IsAgentGoalState says whether a state is one of the four.
func IsAgentGoalState(goalState AgentGoalState) bool {
	switch goalState {
	case GoalWorking, GoalWaiting, GoalMet, GoalDropped:
		return true
	}
	return false
}

// IsGoal says whether a conversation is a goal's own.
func (self *AgentConversation) IsGoal() bool {
	return self != nil && self.Kind == AgentConversationGoal
}

// AgentGoalActivityKind is what happened on a goal.
type AgentGoalActivityKind string

// What an activity row says happened: the goal was started; a turn of its
// own did something worth reading; it came to need the person; the person
// answered and it went back to work; it was met or dropped; it stopped
// after too many turns alone; or a turn failed.
const (
	GoalActivityStarted  AgentGoalActivityKind = "started"
	GoalActivityProgress AgentGoalActivityKind = "progress"
	GoalActivityWaiting  AgentGoalActivityKind = "waiting"
	GoalActivityResumed  AgentGoalActivityKind = "resumed"
	GoalActivityMet      AgentGoalActivityKind = "met"
	GoalActivityDropped  AgentGoalActivityKind = "dropped"
	GoalActivityStalled  AgentGoalActivityKind = "stalled"
	GoalActivityFailed   AgentGoalActivityKind = "failed"
)

// AgentGoalActivity is one thing that happened on a goal, as the person
// reads it in the goal's log.
type AgentGoalActivity struct {
	ID               string                `json:"id"`
	AgentID          string                `json:"agentId"`
	ConversationID   string                `json:"conversationId"`
	CreatedAt        time.Time             `json:"createdAt"`
	GoalActivityKind AgentGoalActivityKind `json:"goalActivityKind"`
	ActivityHeadline string                `json:"activityHeadline"`
	ActivityDetail   string                `json:"activityDetail,omitempty"`
}

// AgentGoalArtifactKind is what sort of thing a goal made.
type AgentGoalArtifactKind string

// The things a goal made that carry no conversation of their own; its
// schedules and background work are found by the conversation instead.
const (
	GoalArtifactMailRule  AgentGoalArtifactKind = "mail_rule"
	GoalArtifactReminder  AgentGoalArtifactKind = "reminder"
	GoalArtifactAlertMute AgentGoalArtifactKind = "alert_mute"
)

// AgentGoalArtifact is one thing a goal made, by its own reference: a mail
// rule's name, a reminder's id, an alert mute's id.
type AgentGoalArtifact struct {
	ID                string                `json:"id"`
	AgentID           string                `json:"agentId"`
	ConversationID    string                `json:"conversationId"`
	CreatedAt         time.Time             `json:"createdAt"`
	GoalArtifactKind  AgentGoalArtifactKind `json:"goalArtifactKind"`
	ArtifactReference string                `json:"artifactReference"`
	ArtifactTitle     string                `json:"artifactTitle"`
}

// GoalCheckInMarker begins the message a goal turn is given, so that
// everything reading the transcript can tell the agent's own check-in from
// the person's words: the dashboard draws such a message as a muted line
// rather than as a person's bubble.
//
// Named here rather than in the agent package because the API hands the
// same transcript to the dashboard, and a marker only one side knows is a
// marker that drifts.
const GoalCheckInMarker = "[goal check-in]"

// BackgroundCommandMarker begins the message the agent is woken with when a
// command it left running in the background on the person's computer
// ends. Like the goal's marker, it tells the transcript's readers that the
// person did not write it.
const BackgroundCommandMarker = "[background command]"

// BackgroundWorkMarker begins the message the agent is woken with when a
// survey or a subagent it left running in the background finishes, and
// each finished piece of work within that message.
const BackgroundWorkMarker = "[background work]"

// ScheduleMarker begins the message a schedule's turn is given when it
// answers in a conversation: the agent's own turn, at a time somebody set,
// and not the person's words.
const ScheduleMarker = "[schedule]"

// OwnTurnMarkers are the markers of every turn the agent takes on its own
// in a person's conversation: what anything looking for the person's own
// last word must pass over.
var OwnTurnMarkers = []string{GoalCheckInMarker, BackgroundCommandMarker, BackgroundWorkMarker, ScheduleMarker, SpeakFirstMarker, AlertMarker, GoalNeedsYouMarker, HerdrQuestionMarker, HerdrSessionMarker}

// SpeakFirstMarker begins the message a turn the agent starts on its own
// is given: an introduction, a memory check, an idea. Nobody wrote it; the
// agent is about to speak first.
const SpeakFirstMarker = "[speaking first]"

// AlertMarker begins the message an alert is written under in the main
// conversation: the agent telling the person, unasked, what their mail
// showed. Nobody wrote it and no turn ran; the agent's words follow it.
const AlertMarker = "[alert]"

// GoalNeedsYouMarker begins the line a goal is written under in the main
// conversation when it comes to need the person: the goal's id follows it,
// then its title. The agent's sentence saying what it needs comes after,
// as an alert's words do. It is the only thing a goal says there.
const GoalNeedsYouMarker = "[goal needs you]"

// HerdrQuestionMarker begins the line a question from one of the person's
// herdr coding sessions is written under in the main conversation, followed
// by the computer, the pane and the question's fingerprint, so the drawer
// can show its options and the next turn can pass the person's answer on.
const HerdrQuestionMarker = "[herdr question]"

// HerdrSessionMarker begins the message the agent is woken with when a
// herdr coding session it watched has finished.
const HerdrSessionMarker = "[herdr session]"

// GoalRelayMarker begins the message the person's answer is written into a
// goal's conversation as, when they gave it in the main conversation: their
// words, carried over by the agent.
const GoalRelayMarker = "[from the person, in the main conversation]"

// AgentMessage is one turn, tool call or result in a conversation.
type AgentMessage struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	ConversationID string    `json:"conversationId"`

	Role       string          `json:"role"` // system | user | assistant | tool | note
	Content    string          `json:"content"`
	ToolCalls  []AgentToolCall `json:"toolCalls,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Name       string          `json:"name,omitempty"`
	Usage      *AgentUsageNote `json:"usage,omitempty"`

	// Attachments are the files that came with a person's turn, and
	// References the threads they pointed at when they wrote it.
	Attachments []AgentAttachment `json:"attachments,omitempty"`
	References  []AgentReference  `json:"references,omitempty"`
}

// AgentAttachment is a file a person handed their agent: the row, with the
// bytes in the spool under the same id. Text is what could be read out of
// it as text, for the model; empty for a picture or a video.
type AgentAttachment struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	AgentID        string    `json:"agentId"`
	ConversationID string    `json:"conversationId,omitempty"`
	MessageID      string    `json:"messageId,omitempty"`
	Name           string    `json:"name"`
	ContentType    string    `json:"contentType"`
	Size           int64     `json:"size"`
	Text           string    `json:"-"`

	// FinanceTransactionID is the finance transaction the file was
	// uploaded to as its receipt, empty for any other file.
	FinanceTransactionID string `json:"financeTransactionId,omitempty" graphapi:"nullable"`
}

// AgentReference is a thread or message the person pointed the agent at
// with a turn: what the dashboard had open, named so that "this" keeps
// meaning it after the page moves on.
type AgentReference struct {
	ItemID   string `json:"itemId,omitempty" graphapi:"nullable"`
	ThreadID string `json:"threadId,omitempty" graphapi:"nullable"`
	Subject  string `json:"subject,omitempty" graphapi:"nullable"`
	From     string `json:"from,omitempty" graphapi:"nullable"`

	// Path and Name point at a page of the agent's own memory instead of
	// a message: the person pressed Ask on a page of the graph, and wants
	// the agent to dig into it, or to change what it says and links to.
	Path string `json:"path,omitempty" graphapi:"nullable"`
	Name string `json:"name,omitempty" graphapi:"nullable"`

	// FinanceTransactionID points at one of the agent's finance
	// transactions instead: the person pressed Ask in its details. The
	// fields after it are what the chip shows, named as on the finance
	// transaction; the server fills them in from the stored row whatever
	// the dashboard sent, and drops the reference when the finance
	// transaction is not the agent's.
	FinanceTransactionID string `json:"financeTransactionId,omitempty" graphapi:"nullable"`
	PostedOn             string `json:"postedOn,omitempty" graphapi:"nullable"`
	Amount               string `json:"amount,omitempty" graphapi:"nullable"`
	CurrencyCode         string `json:"currencyCode,omitempty" graphapi:"nullable"`
	MerchantName         string `json:"merchantName,omitempty" graphapi:"nullable"`
	Description          string `json:"description,omitempty" graphapi:"nullable"`

	// FinanceTransactionContext is what the model is told about the
	// finance transaction in the turn it was pointed at: read from the
	// database when the turn is kept, and never stored, so an earlier
	// turn names it by its id alone and the finance tool reads it again.
	FinanceTransactionContext string `json:"-"`
}

// AgentToolCall is a tool the model asked for, as recorded.
type AgentToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentUsageNote is what one model call cost, on the message it produced.
type AgentUsageNote struct {
	Model            string `json:"model"`
	Kind             string `json:"kind"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	CacheReadTokens  int    `json:"cacheReadTokens"`
	CacheWriteTokens int    `json:"cacheWriteTokens"`

	// Cost is what the call cost by the provider's pricing, in the
	// operator's currency; zero when there is no pricing.
	Cost float64 `json:"cost,omitempty"`
}

// AgentMessageNote is the role of a message that is neither side of the
// conversation: what a run did, in its own words, for the activity view.
const AgentMessageNote = "note"

// AgentNoteKind names a note, so that a client words it in the person's
// language. A note of a kind keeps the kind in the message's Name and only
// its detail in Content: the goal, the reason, the count. A note without
// one is prose, shown as written.
type AgentNoteKind string

// The kinds of note a conversation carries.
const (
	NoteQueued          AgentNoteKind = "queued"           // the turn waits behind the one before it
	NoteStopped         AgentNoteKind = "stopped"          // stopped, by the person or for the detail's reason
	NoteFailed          AgentNoteKind = "failed"           // the turn failed: the error
	NoteCallUnreadable  AgentNoteKind = "call_unreadable"  // a tool call the server could not read
	NoteRepeatedFailure AgentNoteKind = "repeated_failure" // the same call failed three times
	NoteRoundLimit      AgentNoteKind = "round_limit"      // the most rounds a turn may take
	NoteCompacting      AgentNoteKind = "compacting"       // the earlier conversation is being folded into a note
	NoteCompacted       AgentNoteKind = "compacted"        // it was: the detail is the note
	NoteDepth           AgentNoteKind = "depth"            // looked into carefully: the detail is why
	NoteHerdrAnswered   AgentNoteKind = "herdr_answered"   // a coding session's question went: the computer, pane and fingerprint
	// The goal notes below are written no longer, since goals have their
	// own conversations and activity; they are kept so that transcripts
	// written before still read.
	NoteGoalSet      AgentNoteKind = "goal_set" // the detail is the goal
	NoteGoalSetAgain AgentNoteKind = "goal_set_again"
	NoteGoalChanged  AgentNoteKind = "goal_changed"
	NoteGoalCleared  AgentNoteKind = "goal_cleared"
	NoteGoalMet      AgentNoteKind = "goal_met"     // the detail is what was said of it, or the goal
	NoteGoalStalled  AgentNoteKind = "goal_stalled" // the detail is how many turns went by alone
)

// noteEnglish is each kind in English, for whoever reads a note without
// the kinds: the command line, a chat channel, an old client.
var noteEnglish = map[AgentNoteKind]string{
	NoteQueued:          "queued behind the turn before it",
	NoteStopped:         "stopped",
	NoteFailed:          "failed",
	NoteCallUnreadable:  "a tool call the server could not read; asked again",
	NoteRepeatedFailure: "stopped: the same call failed three times",
	NoteRoundLimit:      "stopped after the most rounds a turn may take",
	NoteCompacting:      "compacting the earlier conversation into a note",
	NoteCompacted:       "the earlier conversation was compacted into a note",
	NoteDepth:           "looking into this carefully",
	NoteHerdrAnswered:   "a coding session's question was answered",
	NoteGoalSet:         "Goal set",
	NoteGoalSetAgain:    "Goal set again",
	NoteGoalChanged:     "Goal changed",
	NoteGoalCleared:     "Goal cleared",
	NoteGoalMet:         "Goal met",
	NoteGoalStalled:     "Goal stalled",
}

// NoteText is a note in English: the kind's words, then its detail. The
// compaction's detail is the whole note, which is not repeated here.
func NoteText(kind AgentNoteKind, detail string) string {
	english, ok := noteEnglish[kind]
	if !ok {
		return detail
	}
	switch {
	case kind == NoteGoalStalled:
		return fmt.Sprintf("Goal stalled: %s turns since you last wrote and it is not met. Write to keep going, or clear or change it.", detail)
	case detail == "" || kind == NoteCompacted:
		return english
	}
	return english + ": " + detail
}

// NewAgentNote is a note of a kind for a conversation.
func NewAgentNote(conversationId string, kind AgentNoteKind, detail string) *AgentMessage {
	return &AgentMessage{ConversationID: conversationId, Role: AgentMessageNote, Name: string(kind), Content: detail}
}

// NeedsInsight says whether a rule has a condition only the agent's
// insight can answer, which is why such a rule runs in the second phase.
func (self *MailboxRule) NeedsInsight() bool {
	for _, condition := range self.Conditions {
		if condition.NeedsInsight() {
			return true
		}
	}
	return false
}

// NeedsInsight says whether a condition reads the agent's insight.
func (self *MailboxRuleCondition) NeedsInsight() bool {
	return self.Field == "category" || self.Field == "priority" || self.Field == "needs-reply"
}

// AgentReplyStatus is where a reply the agent wrote stands.
type AgentReplyStatus string

// The statuses: held in Drafts until the hold passes; sent; cancelled by
// the person (or taken over by an edit); refused by the ladder before or
// at sending, with the reason; failed to send after the ladder let it go.
const (
	AgentReplyHeld      AgentReplyStatus = "held"
	AgentReplySending   AgentReplyStatus = "sending"
	AgentReplySent      AgentReplyStatus = "sent"
	AgentReplyCancelled AgentReplyStatus = "cancelled"
	AgentReplyRefused   AgentReplyStatus = "refused"
	AgentReplyFailed    AgentReplyStatus = "failed"
)

// AgentReply is a reply the agent wrote on the person's behalf: to which
// message, held as which draft, and what became of it.
type AgentReply struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	AgentID   string `json:"agentId"`
	MailboxID string `json:"mailboxId"`

	// MailID is the message answered; ThreadID its conversation.
	MailID   string `json:"mailId"`
	ThreadID string `json:"threadId,omitempty"`

	// DraftItemID is the draft in Drafts while the reply is held.
	DraftItemID string `json:"draftItemId,omitempty"`
	RunID       string `json:"runId,omitempty"`

	Status AgentReplyStatus `json:"status"`
	Reason string           `json:"reason,omitempty"`

	// What is sent: the subject, the address it goes from — the one the
	// message was written to — the address it goes to, and the text.
	Subject string `json:"subject"`
	From    string `json:"from"`
	To      string `json:"to"`
	Text    string `json:"text"`

	// SendAfter is when the hold ends; SentMailID and SentAt what was sent
	// and when.
	SendAfter  *time.Time `json:"sendAfter,omitempty"`
	SentMailID string     `json:"sentMailId,omitempty"`
	SentAt     *time.Time `json:"sentAt,omitempty"`
}

// InteractionKind is what the agent stopped to ask the person for.
type InteractionKind string

// The kinds: an answer to a question, or the word to go ahead with a
// call that needs it.
const (
	InteractionQuestion InteractionKind = "question"
	InteractionApproval InteractionKind = "approval"
)

// The answers an approval can have, and the one a card gets when the
// turn waiting on it was stopped.
const (
	InteractionApproved = "approved"
	InteractionDeclined = "declined"
	InteractionStopped  = "stopped"
)

// AgentInteraction is a question or an approval card, kept until the
// person answers it. See docs/planning/durable-interactions-execplan.md.
type AgentInteraction struct {
	ID              string          `json:"id"`
	AgentID         string          `json:"agentId"`
	ConversationID  string          `json:"conversationId"`
	RunID           string          `json:"runId"`
	CallID          string          `json:"callId"`
	InteractionKind InteractionKind `json:"interactionKind"`
	ToolName        string          `json:"toolName"`
	ToolArguments   string          `json:"toolArguments"`

	// InteractionText is the question, or the approval card's line.
	InteractionText    string   `json:"interactionText"`
	InteractionChoices []string `json:"interactionChoices"`
	ToolRisk           string   `json:"toolRisk"`

	CreatedAt         time.Time  `json:"createdAt"`
	ResolvedAt        *time.Time `json:"resolvedAt,omitempty" graphapi:"nullable"`
	InteractionAnswer string     `json:"interactionAnswer"`
}

// suggestedRepliesPattern is the hidden line a dashboard answer may end
// with, offering replies the person can send with a click, or the start of
// one still being written.
var suggestedRepliesPattern = regexp.MustCompile(`\n?<!--suggestions:(?:\[[^\]]*\]-->|[^>]*)\s*$`)

// StripSuggestedReplies takes the hidden suggested replies off an answer,
// for anywhere but the dashboard, which reads them: a chat app, a mail, a
// terminal. See web/src/suggestions.ts.
func StripSuggestedReplies(text string) string {
	return strings.TrimRight(suggestedRepliesPattern.ReplaceAllString(text, ""), " \n")
}
