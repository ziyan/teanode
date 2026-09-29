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

// The two kinds: one message the sorting judged worth telling, and a
// burst of messages alike that a count noticed, which no single message's
// sorting can see.
const (
	AlertCandidateMessage AlertCandidateKind = "message"
	AlertCandidateBurst   AlertCandidateKind = "burst"
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

	// BurstKey is a burst's sender domain and its subject with the digits
	// taken out; BurstCount how many messages it counted.
	BurstKey   string `json:"burstKey,omitempty"`
	BurstCount int    `json:"burstCount,omitempty"`

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
