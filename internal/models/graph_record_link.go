package models

import (
	"fmt"
	"time"
)

// Record links: how the person's own records join the graph.
//
// A record is not a page. A page is the agent's own writing about
// something; a record is the person's data, owned and permissioned
// elsewhere: a contact card, a mail message, an event, a transaction, a
// moment of a recording. A link names the record by kind and identifier and
// says what the page is to it. Nothing about the record is copied, so
// reading a link means reading the record, and the reader's right to read
// it is checked then.

// AgentRecordKind is which of the person's own records a link points at.
type AgentRecordKind string

// The kinds. A passage of a recording is the recording's identifier plus a
// range of milliseconds (AgentRecordLink.PassageStartMS and PassageEndMS).
const (
	RecordContact             AgentRecordKind = "contact"
	RecordMail                AgentRecordKind = "mail"
	RecordCalendarEvent       AgentRecordKind = "calendar_event"
	RecordReminder            AgentRecordKind = "reminder"
	RecordConversationMessage AgentRecordKind = "conversation_message"
	RecordFinanceTransaction  AgentRecordKind = "finance_transaction"
	RecordFinanceReceipt      AgentRecordKind = "finance_receipt"
	RecordRecordingPassage    AgentRecordKind = "recording_passage"
)

// AgentRecordKinds is every kind, for validation and for the tool's schema.
var AgentRecordKinds = []AgentRecordKind{
	RecordContact, RecordMail, RecordCalendarEvent, RecordReminder,
	RecordConversationMessage, RecordFinanceTransaction, RecordFinanceReceipt,
	RecordRecordingPassage,
}

// IsAgentRecordKind says whether a word names a kind.
func IsAgentRecordKind(recordKind AgentRecordKind) bool {
	for _, known := range AgentRecordKinds {
		if known == recordKind {
			return true
		}
	}
	return false
}

// AgentRecordRelation is what a page is to a record.
type AgentRecordRelation string

// The relations. IsContact is the page's person being that card; Wrote and
// Received are mail; Attended and Organized are events; SpokeIn is a
// passage of a recording; Paid and PaidBy are a transaction, the page being
// the payer or the payee; ReceiptFor is a receipt standing behind a
// transaction; MentionedIn and About are the loose ones.
const (
	RecordRelationIsContact   AgentRecordRelation = "is_contact"
	RecordRelationWrote       AgentRecordRelation = "wrote"
	RecordRelationReceived    AgentRecordRelation = "received"
	RecordRelationAttended    AgentRecordRelation = "attended"
	RecordRelationOrganized   AgentRecordRelation = "organized"
	RecordRelationSpokeIn     AgentRecordRelation = "spoke_in"
	RecordRelationMentionedIn AgentRecordRelation = "mentioned_in"
	RecordRelationPaid        AgentRecordRelation = "paid"
	RecordRelationPaidBy      AgentRecordRelation = "paid_by"
	RecordRelationReceiptFor  AgentRecordRelation = "receipt_for"
	RecordRelationAbout       AgentRecordRelation = "about"
)

// AgentRecordRelations is every relation.
var AgentRecordRelations = []AgentRecordRelation{
	RecordRelationIsContact, RecordRelationWrote, RecordRelationReceived,
	RecordRelationAttended, RecordRelationOrganized, RecordRelationSpokeIn,
	RecordRelationMentionedIn, RecordRelationPaid, RecordRelationPaidBy,
	RecordRelationReceiptFor, RecordRelationAbout,
}

// IsAgentRecordRelation says whether a word names a relation.
func IsAgentRecordRelation(recordRelation AgentRecordRelation) bool {
	for _, known := range AgentRecordRelations {
		if known == recordRelation {
			return true
		}
	}
	return false
}

// recordRelationsOf is which relations make sense for each kind, so that a
// page cannot be said to have "spoken in" a bank transaction.
var recordRelationsOf = map[AgentRecordKind][]AgentRecordRelation{
	RecordContact:             {RecordRelationIsContact, RecordRelationAbout},
	RecordMail:                {RecordRelationWrote, RecordRelationReceived, RecordRelationMentionedIn, RecordRelationAbout},
	RecordCalendarEvent:       {RecordRelationAttended, RecordRelationOrganized, RecordRelationMentionedIn, RecordRelationAbout},
	RecordReminder:            {RecordRelationMentionedIn, RecordRelationAbout},
	RecordConversationMessage: {RecordRelationMentionedIn, RecordRelationAbout},
	RecordFinanceTransaction:  {RecordRelationPaid, RecordRelationPaidBy, RecordRelationAbout},
	RecordFinanceReceipt:      {RecordRelationReceiptFor, RecordRelationPaid, RecordRelationPaidBy, RecordRelationAbout},
	RecordRecordingPassage:    {RecordRelationSpokeIn, RecordRelationAttended, RecordRelationMentionedIn, RecordRelationAbout},
}

// RecordRelationsOf is the relations that fit a kind of record.
func RecordRelationsOf(recordKind AgentRecordKind) []AgentRecordRelation {
	return recordRelationsOf[recordKind]
}

// AgentRecordDetail is where inside a record a link points, for the kinds
// that need more than an identifier.
type AgentRecordDetail struct {
	// TranscriptRunID is the transcript a recording passage's range is
	// counted in; a recording can be transcribed again.
	TranscriptRunID string `json:"transcriptRunId,omitempty"`

	// MailboxID is the mailbox a mail message is in.
	MailboxID string `json:"mailboxId,omitempty"`
}

// AgentRecordLink joins a page to one of the person's own records.
type AgentRecordLink struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	NodeID  string `json:"nodeId"`

	// NodePath, NodeName and NodeKind are filled when the link is read for
	// a record's backlinks, where the page is the other end. Not stored.
	NodePath string        `json:"nodePath,omitempty"`
	NodeName string        `json:"nodeName,omitempty"`
	NodeKind AgentNodeKind `json:"nodeKind,omitempty"`

	RecordKind   AgentRecordKind   `json:"recordKind"`
	RecordID     string            `json:"recordId"`
	RecordDetail AgentRecordDetail `json:"recordDetail"`

	// PassageStartMS and PassageEndMS are the range of a recording passage
	// in milliseconds on the recording's timeline; zero for any other kind.
	PassageStartMS int64 `json:"passageStartMS"`
	PassageEndMS   int64 `json:"passageEndMS"`

	RecordRelation AgentRecordRelation `json:"recordRelation"`

	// LinkStatus is the same vocabulary as a link between two pages:
	// stated, or proposed where the agent only guessed.
	LinkStatus AgentEdgeStatus `json:"linkStatus"`

	Evidence   []Evidence `json:"evidence"`
	Note       string     `json:"note,omitempty"`
	HappenedAt *time.Time `json:"happenedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ModifiedAt time.Time  `json:"modifiedAt"`
}

// Validate says whether a link could be stored: a known kind, a relation
// that fits it, a record named, and a range only where it means something.
func (self *AgentRecordLink) Validate() error {
	if !IsAgentRecordKind(self.RecordKind) {
		return fmt.Errorf("%q is not a kind of record", self.RecordKind)
	}
	if !IsAgentRecordRelation(self.RecordRelation) {
		return fmt.Errorf("%q is not a relation to a record", self.RecordRelation)
	}
	fits := false
	for _, known := range recordRelationsOf[self.RecordKind] {
		if known == self.RecordRelation {
			fits = true
		}
	}
	if !fits {
		return fmt.Errorf("a page cannot be %q to a record of kind %q", self.RecordRelation, self.RecordKind)
	}
	if self.RecordID == "" || len(self.RecordID) > 128 {
		return fmt.Errorf("a link needs the record's identifier")
	}
	if self.LinkStatus != "" && self.LinkStatus != EdgeStated && self.LinkStatus != EdgeProposed {
		return fmt.Errorf("%q is not a link status", self.LinkStatus)
	}
	if self.RecordKind == RecordRecordingPassage {
		if self.PassageStartMS < 0 || self.PassageEndMS < self.PassageStartMS {
			return fmt.Errorf("a passage needs a start that is not after its end")
		}
	} else if self.PassageStartMS != 0 || self.PassageEndMS != 0 {
		return fmt.Errorf("only a passage of a recording has a range")
	}
	return nil
}

// Proposed says whether this link is the agent's own guess.
func (self *AgentRecordLink) Proposed() bool {
	return self.LinkStatus == EdgeProposed
}

// Reference is the link's record as a person or a command line names it:
// "contact:ID", or "recording_passage:ID@START-END" for a range of a
// recording, in milliseconds.
func (self *AgentRecordLink) Reference() string {
	return FormatRecordReference(self.RecordKind, self.RecordID, self.PassageStartMS, self.PassageEndMS)
}

// FormatRecordReference is a record named in one word: kind, colon,
// identifier, and for a passage "@start-end" in milliseconds.
func FormatRecordReference(recordKind AgentRecordKind, recordId string, startMS, endMS int64) string {
	reference := string(recordKind) + ":" + recordId
	if recordKind == RecordRecordingPassage {
		reference += fmt.Sprintf("@%d-%d", startMS, endMS)
	}
	return reference
}

// ParseRecordReference is the inverse of FormatRecordReference. A passage
// without a range is the whole recording, 0 to 0.
func ParseRecordReference(reference string) (recordKind AgentRecordKind, recordId string, startMS, endMS int64, err error) {
	kindText, rest, found := cutOnce(reference, ':')
	recordKind = AgentRecordKind(kindText)
	if !found || rest == "" || !IsAgentRecordKind(recordKind) {
		return "", "", 0, 0, fmt.Errorf("%q is not a record; write kind:id, with kind one of %s", reference, joinRecordKinds())
	}
	recordId = rest
	if recordKind == RecordRecordingPassage {
		if identifier, rangeText, hasRange := cutOnce(rest, '@'); hasRange {
			recordId = identifier
			if _, scanErr := fmt.Sscanf(rangeText, "%d-%d", &startMS, &endMS); scanErr != nil {
				return "", "", 0, 0, fmt.Errorf("%q is not a range; write START-END in milliseconds", rangeText)
			}
		}
	}
	return recordKind, recordId, startMS, endMS, nil
}

func cutOnce(text string, separator byte) (string, string, bool) {
	for index := 0; index < len(text); index++ {
		if text[index] == separator {
			return text[:index], text[index+1:], true
		}
	}
	return text, "", false
}

func joinRecordKinds() string {
	text := ""
	for index, recordKind := range AgentRecordKinds {
		if index > 0 {
			text += ", "
		}
		text += string(recordKind)
	}
	return text
}
