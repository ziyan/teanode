package models

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

// AgentAudience names the kinds of run a memory is addressed to.
type AgentAudience string

// The audiences: the conversation, and each kind of run that reads
// memories.
const (
	AudienceAsk       AgentAudience = "ask"
	AudienceTriage    AgentAudience = "triage"
	AudienceResearch  AgentAudience = "research"
	AudienceReply     AgentAudience = "reply"
	AudienceSummaries AgentAudience = "summaries"
)

// AgentAudiences is every audience, for validation and the prompts.
var AgentAudiences = []AgentAudience{AudienceAsk, AudienceTriage, AudienceResearch, AudienceReply, AudienceSummaries}

// AgentMemory is a durable fact about the person that outlives a
// conversation — "signs as Z.", "the accountant is Maria", "never answer
// the landlord automatically" — addressed to the runs that should read it.
// Per person, per server; never shared.
type AgentMemory struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	AgentID    string    `json:"agentId"`

	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`

	// AppliesTo is which runs read it; empty means the conversation only.
	AppliesTo []AgentAudience `json:"appliesTo"`

	// Pinned memories come first in every prompt.
	Pinned bool `json:"pinned"`

	// UsedAt is when a prompt last carried it or a search last found it,
	// which is what orders the rest.
	UsedAt *time.Time `json:"usedAt,omitempty"`

	// Vector is what the memory means, for finding it by meaning rather
	// than by the words it happens to use, and VectorModel is what said
	// so: two vectors are comparable only when one model made them both.
	Vector      []float32 `json:"-"`
	VectorModel string    `json:"-"`
}

// Validate reports everything wrong with a memory.
func (self *AgentMemory) Validate() error {
	var errors ValidationErrors
	if strings.TrimSpace(self.Title) == "" {
		errors.add("title", "required")
	}
	if len(self.Title) > 200 {
		errors.add("title", "at most 200 characters")
	}
	if strings.TrimSpace(self.Content) == "" {
		errors.add("content", "required")
	}
	if len(self.Content) > 4000 {
		errors.add("content", "at most 4000 characters")
	}
	for _, audience := range self.AppliesTo {
		if !IsAgentAudience(audience) {
			errors.add("appliesTo", "%q is not an audience", audience)
		}
	}
	return errors.ErrOrNil()
}

// IsAgentAudience says whether a word names an audience.
func IsAgentAudience(audience AgentAudience) bool {
	for _, known := range AgentAudiences {
		if known == audience {
			return true
		}
	}
	return false
}

// Addressed says whether a memory is for an audience.
func (self *AgentMemory) Addressed(audience AgentAudience) bool {
	for _, candidate := range self.AppliesTo {
		if candidate == audience {
			return true
		}
	}
	return false
}

// Line is the memory as a prompt carries it.
func (self *AgentMemory) Line() string {
	if strings.TrimSpace(self.Title) == "" || strings.EqualFold(strings.TrimSpace(self.Title), strings.TrimSpace(self.Content)) {
		return strings.TrimSpace(self.Content)
	}
	return strings.TrimSpace(self.Title) + ": " + strings.TrimSpace(self.Content)
}

// AgentFeedbackKind is what kind of thing the person did.
type AgentFeedbackKind string

// The kinds of correction: the person filed a message somewhere other than
// the agent sorted it, or would not let a reply the agent wrote go.
const (
	FeedbackFiled         AgentFeedbackKind = "filed"
	FeedbackReplyDeclined AgentFeedbackKind = "reply_declined"
	FeedbackSorted        AgentFeedbackKind = "sorted"

	// FeedbackUnlearned is a fact the person struck from a page. Shown to
	// the next filing run as an example of what not to keep, which is the
	// only way that run learns anything: it sees no other correction.
	FeedbackUnlearned AgentFeedbackKind = "unlearned"
)

// AgentFeedback is a correction recorded from the person's own action,
// shown to the next run of the kind it corrects as an example.
type AgentFeedback struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"createdAt"`
	AgentID   string            `json:"agentId"`
	MailboxID string            `json:"mailboxId,omitempty"`
	Kind      AgentFeedbackKind `json:"kind"`
	MailID    string            `json:"mailId,omitempty"`

	// Said is the correction in words, ready for a prompt.
	Said string `json:"said"`
}

// AgentSchedule is a prompt run at times the person chose, or whenever a
// message they chose arrives.
type AgentSchedule struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	AgentID    string    `json:"agentId"`

	Name string `json:"name"`

	// Cron is five fields — minute hour day month weekday — read in the
	// person's zone. Empty for a schedule that runs on arriving mail.
	Cron   string `json:"cron"`
	Prompt string `json:"prompt"`

	// MailConditions make a schedule run when a message arrives that passes
	// every one of them, in a mailbox the agent was given, instead of at set
	// times. They are the tests a mailbox rule makes.
	MailConditions []MailboxRuleCondition `json:"mailConditions,omitempty"`

	// MailArrivals are the messages that passed since the schedule last ran,
	// for its next run to be told about. Kept off the API: they are the
	// run's to read, by the id each carries.
	MailArrivals []AgentScheduleArrival `json:"-"`

	// WrittenBy is who wrote that prompt: the person, the agent through a
	// tool, or TeaNode itself for the schedules a switch makes (the daily
	// brief, the asset estimates). A schedule runs with nobody watching and
	// its prompt arrives as the user turn, which is the most trusted thing in a conversation
	// -- right for the person's own standing instruction, and wrong for one
	// the agent wrote on the strength of something it read.
	WrittenBy string `json:"writtenBy,omitempty"`

	// Deliver is where the answer goes: mail, to the account's
	// notification address, or drawer, a turn in ConversationID.
	Deliver string `json:"deliver"`

	// ConversationID is the conversation the schedule was made in, where a
	// drawer schedule takes its turn; empty, or a conversation since
	// deleted, means the main one.
	ConversationID string `json:"conversationId,omitempty"`
	Enabled        bool   `json:"enabled"`

	LastRunAt *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
}

// IsMailTriggered is whether the schedule runs on arriving mail rather than
// at set times.
func (self *AgentSchedule) IsMailTriggered() bool {
	return len(self.MailConditions) > 0
}

// AgentScheduleArrival is one message that set a schedule off: enough for
// the run to say what came and to read it, not what it says.
type AgentScheduleArrival struct {
	MailboxItemID string    `json:"mailboxItemId"`
	MailboxName   string    `json:"mailboxName,omitempty"`
	From          string    `json:"from"`
	Subject       string    `json:"subject"`
	ReceivedAt    time.Time `json:"receivedAt"`
	// IsSenderVerified is whether the sender's domain vouched for the
	// message (DMARC passed), so that a From line anybody can write is not
	// taken for the sender it names.
	IsSenderVerified bool `json:"isSenderVerified"`
}

// MaximumScheduleArrivals is how many messages a schedule keeps for its next
// run; a flood past that is counted by the newest ones alone.
const MaximumScheduleArrivals = 50

// ScheduleMailFields are the tests a schedule's mail conditions may make:
// those a message can be judged by as it arrives, before the agent has
// sorted it.
var ScheduleMailFields = []string{"from", "to", "subject", "header", "any"}

// Who wrote a schedule's prompt.
const (
	WrittenByPerson = "person"
	WrittenByAgent  = "agent"
	// WrittenByBuiltIn is a schedule TeaNode wrote because the person pressed
	// a switch. Its prompt is handed to the run as the person's words, but
	// the run reads untrusted content (the brief reads the inbox), so it
	// does not count as the person asking for Passwords.
	WrittenByBuiltIn = "built_in"
)

// Where a schedule's answer goes.
const (
	AgentDeliverMail   = "mail"
	AgentDeliverDrawer = "drawer"
)

// KnownWriter is one of those three, or empty for anything else.
func KnownWriter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case WrittenByPerson:
		return WrittenByPerson
	case WrittenByAgent:
		return WrittenByAgent
	case WrittenByBuiltIn:
		return WrittenByBuiltIn
	}
	return ""
}

// Validate reports everything wrong with a schedule.
func (self *AgentSchedule) Validate() error {
	var errors ValidationErrors
	if strings.TrimSpace(self.Name) == "" {
		errors.add("name", "required")
	}
	switch {
	case self.IsMailTriggered() && strings.TrimSpace(self.Cron) != "":
		errors.add("cron", "a schedule runs at set times or on arriving mail, not both")
	case !self.IsMailTriggered() && strings.TrimSpace(self.Cron) == "":
		errors.add("cron", "required, or mail conditions")
	}
	for index, condition := range self.MailConditions {
		if !slices.Contains(ScheduleMailFields, condition.Field) {
			errors.add("mailConditions", "condition %d: %q is not a field; use from, to, subject, header or any", index, condition.Field)
			continue
		}
		if condition.Field == "any" {
			continue
		}
		switch condition.Operator {
		case "contains", "equals", "":
		case "matches":
			if _, err := regexp.Compile(condition.Value); err != nil {
				errors.add("mailConditions", "condition %d: %q is not a pattern: %s", index, condition.Value, err)
			}
		default:
			errors.add("mailConditions", "condition %d: %q is not an operator; use contains, equals or matches", index, condition.Operator)
		}
		if strings.TrimSpace(condition.Value) == "" {
			errors.add("mailConditions", "condition %d: needs a value to compare with", index)
		}
		if condition.Field == "header" && strings.TrimSpace(condition.Header) == "" {
			errors.add("mailConditions", "condition %d: a header condition names the header", index)
		}
	}
	if strings.TrimSpace(self.Prompt) == "" {
		errors.add("prompt", "required")
	}
	switch self.Deliver {
	case "", "mail", "drawer":
	default:
		errors.add("deliver", "%q is not mail or drawer", self.Deliver)
	}
	return errors.ErrOrNil()
}

// AgentTodo is one item of a conversation's task list.
type AgentTodo struct {
	ID             string     `json:"id"`
	CreatedAt      time.Time  `json:"createdAt"`
	ConversationID string     `json:"conversationId"`
	Text           string     `json:"text"`
	DoneAt         *time.Time `json:"doneAt,omitempty"`
}

// AgentConnectionStatus is where a person's connection to a server stands.
type AgentConnectionStatus string

// The statuses: pending while an authorization is under way; connected;
// error with the reason; disconnected by the person.
const (
	ConnectionPending      AgentConnectionStatus = "pending"
	ConnectionConnected    AgentConnectionStatus = "connected"
	ConnectionError        AgentConnectionStatus = "error"
	ConnectionDisconnected AgentConnectionStatus = "disconnected"
)

// AgentConnection is a person's own way into a connected server the
// operator declared. The sealed fields hold the credential or the tokens
// and are never returned to anybody.
type AgentConnection struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	AgentID    string    `json:"agentId"`
	ServerName string    `json:"serverName"`

	Status AgentConnectionStatus `json:"status"`

	// Credential is the person's own credential for a user-authenticated
	// server; Tokens the JSON of an OAuth authorization; Pending the state
	// and verifier of a flow under way. All sealed.
	Credential string `json:"-"`
	Tokens     string `json:"-"`
	Pending    string `json:"-"`

	LastError       string     `json:"lastError,omitempty"`
	LastConnectedAt *time.Time `json:"lastConnectedAt,omitempty"`
}

// AgentSkill is a skill installed from a registry: a file of declarations
// whose tools join everybody's catalog. Installed by an operator and
// belonging to the server, not to a person, which is why there is no
// agent here.
type AgentSkill struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Description string `json:"description"`

	// Content is the file as it was fetched and checked, kept so that a
	// server that can no longer reach the registry still has it and an
	// operator can read what is installed.
	Content string `json:"-"`

	Enabled bool `json:"enabled"`

	// Scope is who fills this skill's secrets in, when an operator has
	// settled it for the whole skill rather than leaving it to what the
	// skill's author declared per secret: "operator", "person", or empty
	// for the author's declaration.
	Scope string `json:"scope"`
}

// AgentSkillSecret is one person's own value for a secret an installed
// skill declared as theirs to fill in. The operator's values live in the
// configuration; these belong to the person whose agent uses them.
type AgentSkillSecret struct {
	AgentID    string    `json:"agentId"`
	Skill      string    `json:"skill"`
	Key        string    `json:"key"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// Value is sealed with the server secret and never leaves the server.
	Value string `json:"-"`
}
