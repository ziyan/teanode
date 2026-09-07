package models

import (
	"time"
)

// Mailbox is a container of folders belonging to one user. A person gets one
// when their account is made and may have more. Its small per-mailbox
// settings — rules, signature, out-of-office — are columns on it.
type Mailbox struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	UserID     string    `json:"userId"`
	Name       string    `json:"name"`

	// The signature the compose page appends when sending from this
	// mailbox; a user with two mailboxes signs differently from each.
	SignatureHTML string `json:"signatureHtml,omitempty"`
	SignatureText string `json:"signatureText,omitempty"`

	// Rules run in order when a message reaches the Inbox.
	Rules []MailboxRule `json:"rules"`

	// AutoReply is the out-of-office setting; nil when never set.
	AutoReply *MailboxAutoReply `json:"autoReply,omitempty"`

	// Addresses are the aliases of kind mailbox that deliver here, resolved
	// when read: what "send as" is checked against.
	Addresses []*MailboxAddress `json:"addresses,omitempty"`
}

// Validate reports everything wrong with the mailbox.
func (self *Mailbox) Validate() error {
	var errors ValidationErrors
	if self.UserID == "" {
		errors.add("userId", "required")
	}
	if self.Name == "" {
		errors.add("name", "required")
	} else if len(self.Name) > 128 {
		errors.add("name", "must be under 128 characters")
	}
	for index, rule := range self.Rules {
		if err := rule.Validate(); err != nil {
			errors.add("rules", "rule %d: %s", index, err)
		}
	}
	return errors.ErrOrNil()
}

// MailboxAddress is an address that delivers into a mailbox, and that the
// mailbox's owner may send as: an alias of kind mailbox, read with the
// mailbox.
type MailboxAddress struct {
	AliasID   string `json:"aliasId"`
	DomainID  string `json:"domainId"`
	Domain    string `json:"domain"`
	LocalPart string `json:"localPart"`
	Address   string `json:"address"`
}

// MailboxFolderKind marks the folders every mailbox has. Custom folders are
// the empty kind.
type MailboxFolderKind string

const (
	MailboxFolderKindCustom  MailboxFolderKind = ""
	MailboxFolderKindInbox   MailboxFolderKind = "inbox"
	MailboxFolderKindSent    MailboxFolderKind = "sent"
	MailboxFolderKindDrafts  MailboxFolderKind = "drafts"
	MailboxFolderKindArchive MailboxFolderKind = "archive"
	MailboxFolderKindJunk    MailboxFolderKind = "junk"
	MailboxFolderKindTrash   MailboxFolderKind = "trash"
)

// DefaultFolders are what a new mailbox starts with, in the order the tree
// shows them.
var DefaultFolders = []struct {
	Kind MailboxFolderKind
	Name string
}{
	{MailboxFolderKindInbox, "Inbox"},
	{MailboxFolderKindDrafts, "Drafts"},
	{MailboxFolderKindSent, "Sent"},
	{MailboxFolderKindArchive, "Archive"},
	{MailboxFolderKindJunk, "Junk"},
	{MailboxFolderKindTrash, "Trash"},
}

// MailboxFolder is a named place in a mailbox, nested as deep as its owner
// likes. Each folder is an IMAP mailbox; each item in it a message with a UID
// that never changes while it stays there.
type MailboxFolder struct {
	ID         string            `json:"id"`
	CreatedAt  time.Time         `json:"createdAt"`
	ModifiedAt time.Time         `json:"modifiedAt"`
	MailboxID  string            `json:"mailboxId"`
	ParentID   string            `json:"parentId,omitempty"`
	Name       string            `json:"name"`
	Kind       MailboxFolderKind `json:"kind,omitempty"`

	// IMAP's contract: UIDs in a folder only grow, and a folder that is
	// recreated announces itself with a new validity.
	UIDValidity uint64 `json:"uidValidity"`
	UIDNext     uint64 `json:"uidNext"`

	// ModSeq grows on every change to the folder's items; what IDLE watches
	// and what CONDSTORE compares.
	ModSeq uint64 `json:"modseq"`

	// Counted when the tree is listed, never stored.
	Unread int64 `json:"unread"`
	Total  int64 `json:"total"`
}

// Validate reports everything wrong with the folder.
func (self *MailboxFolder) Validate() error {
	var errors ValidationErrors
	if self.MailboxID == "" {
		errors.add("mailboxId", "required")
	}
	if self.Name == "" {
		errors.add("name", "required")
	} else if len(self.Name) > 128 {
		errors.add("name", "must be under 128 characters")
	}
	for _, character := range self.Name {
		if character == '/' || character < ' ' {
			errors.add("name", "may not contain a slash or a control character")
			break
		}
	}
	return errors.ErrOrNil()
}

// MailboxItem is one message in one folder: the possession of it, with its
// flags. The message is the existing Mail; this only refers to it.
type MailboxItem struct {
	ID        string    `json:"id"`
	FolderID  string    `json:"folderId"`
	MailID    string    `json:"mailId"`
	Mail      *Mail     `json:"mail,omitempty"` // resolved when listed
	UID       uint64    `json:"uid"`
	ModSeq    uint64    `json:"modseq"`
	Seen      bool      `json:"seen"`
	Flagged   bool      `json:"flagged"`
	Answered  bool      `json:"answered"`
	Forwarded bool      `json:"forwarded"`
	Draft     bool      `json:"draft"`
	AddedAt   time.Time `json:"addedAt"`
}

// MailboxItemFlags is what STORE and the web UI change on an item. Nil
// leaves a flag alone.
type MailboxItemFlags struct {
	Seen      *bool
	Flagged   *bool
	Answered  *bool
	Forwarded *bool
	Draft     *bool
}

// MailboxFolderExpunge records a UID that left a folder and the modseq it
// left at, so a client can be told what vanished since its last sync.
type MailboxFolderExpunge struct {
	FolderID   string    `json:"folderId"`
	UID        uint64    `json:"uid"`
	ModSeq     uint64    `json:"modseq"`
	ExpungedAt time.Time `json:"expungedAt"`
}

// MailboxRule is one entry of Mailbox.Rules, run in array order when a
// message reaches the Inbox. No id and no row of its own: rules are saved as
// a whole.
type MailboxRule struct {
	Name       string                 `json:"name"`
	Enabled    bool                   `json:"enabled"`
	Conditions []MailboxRuleCondition `json:"conditions"` // all must match
	Actions    []MailboxRuleAction    `json:"actions"`    // in order
	Stop       bool                   `json:"stop"`       // no later rule runs after this one matches
}

// Validate reports everything wrong with the rule.
func (self *MailboxRule) Validate() error {
	var errors ValidationErrors
	if self.Name == "" {
		errors.add("name", "required")
	}
	for index, condition := range self.Conditions {
		switch condition.Field {
		case "from", "to", "subject", "header", "score", "sender-known", "any":
		default:
			errors.add("conditions", "condition %d: %q is not a field", index, condition.Field)
		}
		switch condition.Operator {
		case "contains", "equals", "matches", "above", "below", "":
		default:
			errors.add("conditions", "condition %d: %q is not an operator", index, condition.Operator)
		}
	}
	for index, action := range self.Actions {
		switch action.Kind {
		case "move", "markRead", "flag", "forward", "delete":
		default:
			errors.add("actions", "action %d: %q is not an action", index, action.Kind)
		}
		if action.Kind == "move" && action.FolderID == "" {
			errors.add("actions", "action %d: move needs a folder", index)
		}
		if action.Kind == "forward" && !IsEmailAddress(action.Address) {
			errors.add("actions", "action %d: forward needs an address", index)
		}
	}
	return errors.ErrOrNil()
}

// MailboxRuleCondition is one test: a field, how to compare, and against what.
type MailboxRuleCondition struct {
	Field    string `json:"field"` // from, to, subject, header, score, sender-known, any
	Header   string `json:"header,omitempty"`
	Operator string `json:"operator"` // contains, equals, matches, above, below
	Value    string `json:"value,omitempty"`
}

// MailboxRuleAction is one thing to do: move somewhere, mark, forward, delete.
type MailboxRuleAction struct {
	Kind     string `json:"kind"` // move, markRead, flag, forward, delete
	FolderID string `json:"folderId,omitempty"`
	Address  string `json:"address,omitempty"`
}

// MailboxAutoReply is the out-of-office setting: what to send, and when it is
// in force. Whether to send it to a given message is decided by the
// protections in the out-of-office path, not by anything here.
type MailboxAutoReply struct {
	Enabled bool       `json:"enabled"`
	From    *time.Time `json:"from,omitempty"`  // in force from; nil is now
	Until   *time.Time `json:"until,omitempty"` // in force until; nil is until turned off
	Subject string     `json:"subject"`         // "" means "Auto: " + the original subject
	Text    string     `json:"text"`
	HTML    string     `json:"html,omitempty"`
}

// MailboxContact is an address learned from traffic, for completion and for
// the "sender is known" rule condition.
type MailboxContact struct {
	MailboxID     string     `json:"mailboxId"`
	Address       string     `json:"address"`
	Name          string     `json:"name,omitempty"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	Count         int        `json:"count"`
	AutoRepliedAt *time.Time `json:"autoRepliedAt,omitempty"`
}

// MailboxAppPassword is what a mail program signs in with. It belongs to a
// mailbox, not a user: a program's "account" is one mailbox, so the login
// name is one of the mailbox's addresses and the app password is what says
// which mailbox that is. One per device, revocable alone; the hash never
// leaves the server.
type MailboxAppPassword struct {
	ID           string     `json:"id"`
	CreatedAt    time.Time  `json:"createdAt"`
	MailboxID    string     `json:"mailboxId"`
	Name         string     `json:"name"`
	PasswordHash string     `json:"-"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
}

// RedactForAudit is the app password as an audit row records it: without the
// hash.
func (self *MailboxAppPassword) RedactForAudit() any {
	if self == nil {
		return nil
	}
	redacted := *self
	redacted.PasswordHash = ""
	return &redacted
}
