package models

import "time"

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

// The kinds: the one continuous conversation, a named one kept apart, and
// the transcript of a run with nobody present.
const (
	AgentConversationMain  AgentConversationKind = "main"
	AgentConversationNamed AgentConversationKind = "named"
	AgentConversationRun   AgentConversationKind = "run"
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
}

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
}

// AgentReference is a thread or message the person pointed the agent at
// with a turn: what the dashboard had open, named so that "this" keeps
// meaning it after the page moves on.
type AgentReference struct {
	ItemID   string `json:"itemId,omitempty" graphapi:"nullable"`
	ThreadID string `json:"threadId,omitempty" graphapi:"nullable"`
	Subject  string `json:"subject,omitempty" graphapi:"nullable"`
	From     string `json:"from,omitempty" graphapi:"nullable"`
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
