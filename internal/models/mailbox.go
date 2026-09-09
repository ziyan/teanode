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

	// PinnedAt is when the owner pinned the folder to the top of the rail,
	// beside the Inbox and Starred; nil for a folder that sits in the tree
	// only. The pins are shown in the order they were made.
	PinnedAt *time.Time `json:"pinnedAt,omitempty"`

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
	Deleted   bool      `json:"deleted"` // IMAP's \Deleted, awaiting EXPUNGE
	AddedAt   time.Time `json:"addedAt"`

	// ImagesAt is when the reader chose to load this message's remote
	// pictures. Set once and kept, so the choice is not asked for again every
	// time the message is opened.
	ImagesAt *time.Time `json:"imagesAt,omitempty"`
}

// MailboxThread is a conversation as a folder's list shows one: the newest of
// its messages that are in that folder, how many of them there are, how many
// are unread, and who has taken part.
//
// A conversation is not a stored thing. It is every message sharing a
// ThreadID, which the server derives from the In-Reply-To and References
// headers when the message is stored. So a thread has no id of its own beyond
// the id of the message that started it, and no row anywhere: it is what a
// query groups.
type MailboxThread struct {
	// ThreadID is the id of the message that began the conversation.
	ThreadID string `json:"threadId"`

	// Item is the newest message of the conversation in this folder, which is
	// what the row shows. Its Mail is resolved the way a listed item's is.
	Item *MailboxItem `json:"item"`

	// Count is how many of the conversation's messages are in this folder,
	// and Unread how many of those have not been read.
	Count  int `json:"count"`
	Unread int `json:"unread"`

	// Flagged is whether any of them is starred, so that a conversation
	// carries the star of any message in it.
	Flagged bool `json:"flagged"`

	// Participants are the people who have written, oldest first, by the name
	// they wrote under or their address when they gave none.
	Participants []string `json:"participants"`

	// ItemIDs is every message of the conversation in this folder, so that
	// starring, moving or deleting the row acts on the conversation rather
	// than on the one message the row happens to show.
	ItemIDs []string `json:"itemIds"`

	// HasDraft says the conversation has an unsent message in it, anywhere in
	// the mailbox — a reply begun and left. The draft is in the Drafts
	// folder while the conversation is being read from the Inbox, so this is
	// asked of the whole mailbox rather than of the folder listed.
	HasDraft bool `json:"hasDraft"`
}

// MailboxSubscription is one mailing list a mailbox receives: what it is,
// how much of it there is, and whether leaving it has been asked for.
//
// It is not a stored thing but a grouping of stored things — every message
// that named the same list — so it exists for as long as there is mail from
// it. What is stored is the request to leave, which outlives the mail.
type MailboxSubscription struct {
	// Key identifies the list: the identifier the list publishes for itself,
	// or the address it sends from when it publishes none.
	Key string `json:"key"`

	// Name is what to call it, and From the address the newest message came
	// from, which is not always what the name says.
	Name string `json:"name"`
	From string `json:"from"`

	// Count is how much of it this mailbox holds, Unread how much of that has
	// not been read.
	Count  int `json:"count"`
	Unread int `json:"unread"`

	// LastAt is when the newest arrived and LastItemID which message it is,
	// so the row opens on something.
	LastAt     time.Time `json:"lastAt"`
	LastItemID string    `json:"lastItemId"`

	// LogoDomain is the sending domain whose published logo this server holds,
	// empty unless there is one to show. Set only when the newest message
	// proved it came from that domain: a mark shown for mail that failed its
	// checks is an aid to whoever is pretending to be the sender.
	LogoDomain string `json:"logoDomain,omitempty"`

	// OneClick is the newest message promising that one request is enough to
	// leave, and Unsubscribe the addresses it offered to leave by.
	OneClick    bool     `json:"oneClick"`
	Unsubscribe []string `json:"unsubscribe"`

	// Stripped says the sender did offer a way out and something between them
	// and here removed it — a relay that hides the reader's address. Worth
	// saying, because "no way to leave" otherwise reads as the sender's doing.
	Stripped bool `json:"stripped,omitempty"`

	// MutedAt is when the reader asked for this list to stop arriving in the
	// Inbox. It keeps coming and goes straight to the Archive, read.
	MutedAt *time.Time `json:"mutedAt,omitempty"`

	// ImagesAt is when the reader said this list's pictures may be loaded
	// without asking. The cost is the same as loading them once, repeated:
	// the sender learns each message was opened.
	ImagesAt *time.Time `json:"imagesAt,omitempty"`

	// What this person has already asked for. RequestedAt is when they asked
	// to leave, nil while they have not; Method is how it was asked —
	// oneClick, mail, or link for a page they were handed; Failed and Error
	// say what went wrong when something did.
	RequestedAt *time.Time `json:"requestedAt,omitempty"`
	Method      string     `json:"method,omitempty"`
	Failed      bool       `json:"failed,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// How a subscription was left, or asked to be.
const (
	// UnsubscribeOneClick is the request RFC 8058 describes: one POST, which
	// the sender undertook to honour without asking anything further.
	UnsubscribeOneClick = "oneClick"

	// UnsubscribeMail is a message sent to the address the sender named.
	UnsubscribeMail = "mail"

	// UnsubscribeLink is a page handed to the reader, because a page that
	// wants a human cannot be pressed by a server.
	UnsubscribeLink = "link"
)

// MailboxItemFlags is what STORE and the web UI change on an item. Nil
// leaves a flag alone.
type MailboxItemFlags struct {
	Seen      *bool
	Flagged   *bool
	Answered  *bool
	Forwarded *bool
	Draft     *bool
	Deleted   *bool
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
	Header   string `json:"header,omitempty" graphapi:"nullable"`
	Operator string `json:"operator"` // contains, equals, matches, above, below
	Value    string `json:"value,omitempty" graphapi:"nullable"`
}

// MailboxRuleAction is one thing to do: move somewhere, mark, forward, delete.
type MailboxRuleAction struct {
	Kind     string `json:"kind"` // move, markRead, flag, forward, delete
	FolderID string `json:"folderId,omitempty" graphapi:"nullable"`
	Address  string `json:"address,omitempty" graphapi:"nullable"`
}

// MailboxAutoReply is the out-of-office setting: what to send, and when it is
// in force. Whether to send it to a given message is decided by the
// protections in the out-of-office path, not by anything here.
type MailboxAutoReply struct {
	Enabled bool       `json:"enabled"`
	From    *time.Time `json:"from,omitempty"`              // in force from; nil is now
	Until   *time.Time `json:"until,omitempty"`             // in force until; nil is until turned off
	Subject string     `json:"subject" graphapi:"nullable"` // "" means "Auto: " + the original subject
	Text    string     `json:"text" graphapi:"nullable"`
	HTML    string     `json:"html,omitempty" graphapi:"nullable"`
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
