package models

import "time"

// The signals the sorting gives a message about telling the person
// unasked: nothing to say, worth saying today, or worth saying now.
const (
	AlertSignalNone = "none"
	AlertSignalSoon = "soon"
	AlertSignalNow  = "now"
)

// AlertCandidateKind is what made something a candidate for an alert.
type AlertCandidateKind string

// The kinds: one message the sorting judged worth telling, a burst of
// messages alike that a count noticed, which no single message's sorting
// can see, and a budget or savings target crossing that code computed
// after a sync.
const (
	AlertCandidateMessage AlertCandidateKind = "message"
	AlertCandidateBurst   AlertCandidateKind = "burst"
	AlertCandidateBudget  AlertCandidateKind = "budget"
)

// AgentAlertCandidate is something that might be worth telling the person
// about, waiting for the alert job to decide.
type AgentAlertCandidate struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	MailboxID string `json:"mailboxId"`

	// MailID is the message it is about: the one the sorting judged, or
	// the latest of a burst.
	MailID string `json:"mailId"`

	CandidateKind AlertCandidateKind `json:"candidateKind"`

	// AlertSignal is the sorting's signal for a message, and
	// CandidateReason its line, or what the count saw of a burst.
	AlertSignal     string `json:"alertSignal"`
	CandidateReason string `json:"candidateReason"`

	// BurstKey is a burst's sender address and its subject with the
	// digits taken out; BurstCount how many messages it counted.
	BurstKey   string `json:"burstKey,omitempty"`
	BurstCount int    `json:"burstCount,omitempty"`

	// BudgetKey names a budget crossing, such as
	// "spending-category:<id>:2026-09:at_risk", and becomes the subject
	// key of the alert that tells it, so the same crossing is told once.
	BudgetKey string `json:"budgetKey,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	// AlertID is the alert that told the person about it. DroppedAt and
	// DropReason say when and why it was not told instead.
	AlertID    string     `json:"alertId,omitempty"`
	DroppedAt  *time.Time `json:"droppedAt,omitempty"`
	DropReason string     `json:"dropReason,omitempty"`
}

// IsWaiting says the candidate has been neither told nor dropped.
func (self *AgentAlertCandidate) IsWaiting() bool {
	return self.AlertID == "" && self.DroppedAt == nil
}

// AgentAlert is what the agent told the person unasked, as it was said in
// the main conversation.
type AgentAlert struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// SubjectKey is the sender and what it is about, in the same words
	// each time the same thing comes up, which is how nothing is said
	// twice.
	SubjectKey string `json:"subjectKey"`
	AlertText  string `json:"alertText"`

	// IsUrgent says it could not wait for the morning, and was said at
	// night if that is when it came.
	IsUrgent bool `json:"isUrgent"`

	CandidateIDs   []string  `json:"candidateIds"`
	ConversationID string    `json:"conversationId"`
	MessageID      string    `json:"messageId"`
	SentAt         time.Time `json:"sentAt"`

	// What the alert was about in terms the model's wording does not
	// change, which a mute taken from it names: the burst keys of the
	// bursts it covered, the addresses and domains of the senders, and
	// what the sorting called the messages. Empty for an alert recorded
	// before they were kept.
	CoveredBurstKeys       []string `json:"coveredBurstKeys"`
	CoveredSenderAddresses []string `json:"coveredSenderAddresses"`
	CoveredSenderDomains   []string `json:"coveredSenderDomains"`
	CoveredMailCategories  []string `json:"coveredMailCategories"`
}

// AlertMuteScope is what a mute is matched against.
type AlertMuteScope string

// The things the person can say not to be told about: one sender's
// address, everybody at a domain, one subject (an alert's subject key, or
// a burst's), a kind of alert (a burst, a budget, or a category of the
// sorting, such as notification or receipt), or the budget alerts of one
// spending category, by its id.
const (
	AlertMuteSender           AlertMuteScope = "sender"
	AlertMuteDomain           AlertMuteScope = "domain"
	AlertMuteSubjectKey       AlertMuteScope = "subjectKey"
	AlertMuteKind             AlertMuteScope = "kind"
	AlertMuteSpendingCategory AlertMuteScope = "spendingCategory"
)

// IsValid says the scope is one of the five.
func (self AlertMuteScope) IsValid() bool {
	switch self {
	case AlertMuteSender, AlertMuteDomain, AlertMuteSubjectKey, AlertMuteKind, AlertMuteSpendingCategory:
		return true
	}
	return false
}

// AlertKindBurst is the kind a mute names to silence every burst, whatever
// it is of.
const AlertKindBurst = "burst"

// AlertKindBudget is the kind a mute names to silence every budget and
// savings target alert.
const AlertKindBudget = "budget"

// AgentAlertMute is the person's "don't tell me about these": a candidate
// or an alert that matches it is dropped, saying it was muted.
type AgentAlertMute struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	// MuteScope is what MuteTarget is: an address, a domain, a subject
	// key or a kind, in lower case.
	MuteScope  AlertMuteScope `json:"muteScope"`
	MuteTarget string         `json:"muteTarget"`

	// AlertID is the alert the person muted from, when they did; empty
	// when they named the target themselves.
	AlertID string `json:"alertId"`

	CreatedAt time.Time `json:"createdAt"`
}
