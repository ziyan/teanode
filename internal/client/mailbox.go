package client

import (
	"context"
	"time"
)

// MailboxView is a mailbox with its folders, as the server returns it.
type MailboxView struct {
	Mailbox        *Mailbox         `json:"mailbox"`
	Folders        []*MailboxFolder `json:"folders"`
	Unread         int              `json:"unread"`
	MaxMessageSize uint64           `json:"maxMessageSize"`
}

// Mailbox is where a person's mail lives.
type Mailbox struct {
	ID            string            `json:"id"`
	UserID        string            `json:"userId"`
	Name          string            `json:"name"`
	SignatureText string            `json:"signatureText"`
	SignatureHTML string            `json:"signatureHtml"`
	Addresses     []*MailboxAddress `json:"addresses"`
	Rules         []MailboxRule     `json:"rules"`
	AutoReply     *MailboxAutoReply `json:"autoReply"`
}

// MailboxAddress is an address that delivers into a mailbox: an alias of
// kind mailbox, read back from its pattern.
type MailboxAddress struct {
	AliasID   string `json:"aliasId"`
	DomainID  string `json:"domainId"`
	Domain    string `json:"domain"`
	LocalPart string `json:"localPart"`
	Address   string `json:"address"`
}

// MailboxFolder is one folder of a mailbox. The kind is empty for a folder
// somebody made; the rest are the ones every mailbox has.
type MailboxFolder struct {
	ID        string     `json:"id"`
	MailboxID string     `json:"mailboxId"`
	ParentID  string     `json:"parentId"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	PinnedAt  *time.Time `json:"pinnedAt"`
	Unread    int        `json:"unread"`
	Total     int        `json:"total"`
}

// MailboxRule files arriving mail: every condition must match, then every
// action runs, in order.
type MailboxRule struct {
	Name       string                 `json:"name"`
	Enabled    bool                   `json:"enabled"`
	Conditions []MailboxRuleCondition `json:"conditions"`
	Actions    []MailboxRuleAction    `json:"actions"`
	Stop       bool                   `json:"stop"`
}

// MailboxRuleCondition is one test: a field, how to compare it, and what to.
type MailboxRuleCondition struct {
	Field    string `json:"field"`
	Header   string `json:"header,omitempty"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
}

// MailboxRuleAction is one thing to do with a message a rule matched.
type MailboxRuleAction struct {
	Kind     string `json:"kind"`
	FolderID string `json:"folderId,omitempty"`
	Address  string `json:"address,omitempty"`
}

// MailboxAutoReply is the out-of-office setting.
type MailboxAutoReply struct {
	Enabled bool       `json:"enabled"`
	From    *time.Time `json:"from,omitempty"`
	Until   *time.Time `json:"until,omitempty"`
	Subject string     `json:"subject"`
	Text    string     `json:"text"`
	HTML    string     `json:"html,omitempty"`
}

// MailboxSummary is a mailbox with its owner, for pointing an alias at one.
type MailboxSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
	UserName string `json:"userName"`
}

// MailboxContact is an address the mailbox has corresponded with.
type MailboxContact struct {
	MailboxID  string    `json:"mailboxId"`
	Address    string    `json:"address"`
	Name       string    `json:"name"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Count      int       `json:"count"`
}

// MailboxAppPassword is what one mail program signs in with.
type MailboxAppPassword struct {
	ID         string     `json:"id"`
	MailboxID  string     `json:"mailboxId"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// CreatedAppPassword is an app password at the one moment its secret is
// known: the server keeps only a hash of it.
type CreatedAppPassword struct {
	AppPassword *MailboxAppPassword `json:"appPassword"`
	Password    string              `json:"password"`
	Username    string              `json:"username"`
}

// MailProgramSettings is where a mail program connects.
type MailProgramSettings struct {
	IMAPHost       string `json:"imapHost"`
	IMAPPort       int    `json:"imapPort"`
	IMAPSPort      int    `json:"imapsPort"`
	SubmissionHost string `json:"submissionHost"`
	SubmissionPort int    `json:"submissionPort"`
}

// MailboxRuleTest is one message the rules were tried against, and which of
// them matched, by position in the list given.
type MailboxRuleTest struct {
	Item    *MailboxItem `json:"item"`
	Matched []int        `json:"matched"`
}

// MailboxItem is one message in one folder.
type MailboxItem struct {
	ID       string       `json:"id"`
	FolderID string       `json:"folderId"`
	Seen     bool         `json:"seen"`
	Flagged  bool         `json:"flagged"`
	Mail     *MailboxMail `json:"mail"`
}

// MailboxMail is the little of a message a list needs.
type MailboxMail struct {
	ID         string     `json:"id"`
	From       string     `json:"from"`
	FromName   string     `json:"fromName"`
	Subject    string     `json:"subject"`
	ReceivedAt *time.Time `json:"receivedAt"`
}

// MailboxRuleApplication is what applying the rules to stored mail did.
type MailboxRuleApplication struct {
	Considered int `json:"considered"`
	Matched    int `json:"matched"`
	Moved      int `json:"moved"`
	Marked     int `json:"marked"`
	Flagged    int `json:"flagged"`
	Deleted    int `json:"deleted"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

const mailboxFields = `{
	mailbox {
		id userId name signatureText signatureHtml
		addresses { aliasId domainId domain localPart address }
		rules { name enabled stop conditions { field header operator value } actions { kind folderId address } }
		autoReply { enabled from until subject text html }
	}
	folders { id mailboxId parentId name kind pinnedAt unread total }
	unread
	maxMessageSize
}`

const folderFields = `{ id mailboxId parentId name kind pinnedAt unread total }`

// DocumentListMailboxes and the rest are the documents this file sends,
// named so that a test can validate every one of them against the schema the
// server builds. A field renamed on the server is then a failing test rather
// than a command that fails in somebody's terminal.
const (
	DocumentListMailboxes    = `query { ListMailboxes ` + mailboxFields + ` }`
	DocumentListAllMailboxes = `query { ListAllMailboxes { id name userId username userName } }`

	DocumentCreateMailboxFolder = `mutation ($mailboxId: String!, $name: String!, $parentId: String) {
		CreateMailboxFolder(mailboxId: $mailboxId, name: $name, parentId: $parentId) ` + folderFields + `
	}`
	DocumentUpdateMailboxFolder = `mutation ($folderId: String!, $name: String, $parentId: String) {
		UpdateMailboxFolder(folderId: $folderId, name: $name, parentId: $parentId) ` + folderFields + `
	}`
	DocumentSetMailboxFolderPinned = `mutation ($folderId: String!, $pinned: Boolean!) {
		SetMailboxFolderPinned(folderId: $folderId, pinned: $pinned) ` + folderFields + `
	}`
	DocumentDeleteMailboxFolder = `mutation ($folderId: String!) { DeleteMailboxFolder(folderId: $folderId) }`

	DocumentUpdateMailbox = `mutation ($mailboxId: String!, $name: String, $signatureText: String, $signatureHtml: String,
		$rules: [MailboxRuleInput!], $autoReply: MailboxAutoReplyInput, $clearAutoReply: Boolean) {
		UpdateMailbox(mailboxId: $mailboxId, name: $name, signatureText: $signatureText, signatureHtml: $signatureHtml,
			rules: $rules, autoReply: $autoReply, clearAutoReply: $clearAutoReply) ` + mailboxFields + `
	}`

	DocumentTestMailboxRules = `query ($mailboxId: String!, $folderId: String, $first: Int, $rules: [MailboxRuleInput!]!) {
		TestMailboxRules(mailboxId: $mailboxId, folderId: $folderId, first: $first, rules: $rules) {
			matched
			item { id folderId seen flagged mail { id from fromName subject receivedAt } }
		}
	}`
	DocumentApplyMailboxRules = `mutation ($mailboxId: String!, $folderId: String, $first: Int) {
		ApplyMailboxRules(mailboxId: $mailboxId, folderId: $folderId, first: $first) {
			considered matched moved marked flagged deleted skipped failed
		}
	}`

	DocumentListMailboxContacts = `query ($mailboxId: String!, $prefix: String, $first: Int) {
		ListMailboxContacts(mailboxId: $mailboxId, prefix: $prefix, first: $first) {
			mailboxId address name lastSeenAt count
		}
	}`
	DocumentSaveMailboxContact = `mutation ($mailboxId: String!, $address: String!, $name: String) {
		SaveMailboxContact(mailboxId: $mailboxId, address: $address, name: $name) { mailboxId address name lastSeenAt count }
	}`
	DocumentDeleteMailboxContact = `mutation ($mailboxId: String!, $address: String!) {
		DeleteMailboxContact(mailboxId: $mailboxId, address: $address)
	}`

	DocumentListMailboxAppPasswords = `query ($mailboxId: String!) {
		ListMailboxAppPasswords(mailboxId: $mailboxId) { id mailboxId name createdAt lastUsedAt }
	}`
	DocumentCreateMailboxAppPassword = `mutation ($mailboxId: String!, $name: String!) {
		CreateMailboxAppPassword(mailboxId: $mailboxId, name: $name) {
			password username appPassword { id mailboxId name createdAt lastUsedAt }
		}
	}`
	DocumentDeleteMailboxAppPassword = `mutation ($appPasswordId: String!) {
		DeleteMailboxAppPassword(appPasswordId: $appPasswordId)
	}`

	DocumentGetMailProgramSettings = `query { GetMailProgramSettings { imapHost imapPort imapsPort submissionHost submissionPort } }`
)

// ListMailboxes returns the caller's own mailboxes with their folders.
func ListMailboxes(ctx context.Context, connection *Client) ([]*MailboxView, error) {
	var result struct {
		ListMailboxes []*MailboxView `json:"ListMailboxes"`
	}
	if err := connection.Execute(ctx, DocumentListMailboxes, nil, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxes, nil
}

// ListAllMailboxes returns every mailbox on the server with its owner. It
// needs the permission to manage domains or users; an ordinary account is
// told the operation does not exist.
func ListAllMailboxes(ctx context.Context, connection *Client) ([]*MailboxSummary, error) {
	var result struct {
		ListAllMailboxes []*MailboxSummary `json:"ListAllMailboxes"`
	}
	if err := connection.Execute(ctx, DocumentListAllMailboxes, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAllMailboxes, nil
}

// CreateMailboxFolder adds a folder, at the top level or under another.
func CreateMailboxFolder(ctx context.Context, connection *Client, mailboxId, name, parentId string) (*MailboxFolder, error) {
	var result struct {
		CreateMailboxFolder *MailboxFolder `json:"CreateMailboxFolder"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "name": name}
	if parentId != "" {
		variables["parentId"] = parentId
	}
	if err := connection.Execute(ctx, DocumentCreateMailboxFolder, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateMailboxFolder, nil
}

// UpdateMailboxFolder renames a folder, moves it under another, or moves it
// to the top level with an empty parent. Only what is given changes.
func UpdateMailboxFolder(ctx context.Context, connection *Client, folderId string, name, parentId *string) (*MailboxFolder, error) {
	var result struct {
		UpdateMailboxFolder *MailboxFolder `json:"UpdateMailboxFolder"`
	}
	variables := map[string]any{"folderId": folderId}
	if name != nil {
		variables["name"] = *name
	}
	if parentId != nil {
		variables["parentId"] = *parentId
	}
	if err := connection.Execute(ctx, DocumentUpdateMailboxFolder, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateMailboxFolder, nil
}

// SetMailboxFolderPinned pins a folder to the top of the rail, or takes it
// down. The Inbox is always there and cannot be pinned.
func SetMailboxFolderPinned(ctx context.Context, connection *Client, folderId string, pinned bool) (*MailboxFolder, error) {
	var result struct {
		SetMailboxFolderPinned *MailboxFolder `json:"SetMailboxFolderPinned"`
	}
	variables := map[string]any{"folderId": folderId, "pinned": pinned}
	if err := connection.Execute(ctx, DocumentSetMailboxFolderPinned, variables, &result); err != nil {
		return nil, err
	}
	return result.SetMailboxFolderPinned, nil
}

// DeleteMailboxFolder removes a folder somebody made, and everything in it.
func DeleteMailboxFolder(ctx context.Context, connection *Client, folderId string) error {
	return connection.Execute(ctx, DocumentDeleteMailboxFolder, map[string]any{"folderId": folderId}, nil)
}

// MailboxParameters is what UpdateMailbox may change. A nil field is left
// alone; the rules and the out-of-office setting replace what is stored.
type MailboxParameters struct {
	Name           *string
	SignatureText  *string
	SignatureHTML  *string
	Rules          *[]MailboxRule
	AutoReply      *MailboxAutoReply
	ClearAutoReply bool
}

// UpdateMailbox changes a mailbox's name, signature, rules or out-of-office
// setting.
func UpdateMailbox(ctx context.Context, connection *Client, mailboxId string, parameters *MailboxParameters) (*MailboxView, error) {
	var result struct {
		UpdateMailbox *MailboxView `json:"UpdateMailbox"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if parameters.Name != nil {
		variables["name"] = *parameters.Name
	}
	if parameters.SignatureText != nil {
		variables["signatureText"] = *parameters.SignatureText
	}
	if parameters.SignatureHTML != nil {
		variables["signatureHtml"] = *parameters.SignatureHTML
	}
	if parameters.Rules != nil {
		variables["rules"] = *parameters.Rules
	}
	if parameters.AutoReply != nil {
		variables["autoReply"] = parameters.AutoReply
	}
	if parameters.ClearAutoReply {
		variables["clearAutoReply"] = true
	}
	if err := connection.Execute(ctx, DocumentUpdateMailbox, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateMailbox, nil
}

// TestMailboxRules says which of the given rules would match the mail
// already in a folder, without changing anything.
func TestMailboxRules(ctx context.Context, connection *Client, mailboxId, folderId string, first int, rules []MailboxRule) ([]*MailboxRuleTest, error) {
	var result struct {
		TestMailboxRules []*MailboxRuleTest `json:"TestMailboxRules"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "rules": rules}
	if folderId != "" {
		variables["folderId"] = folderId
	}
	if first > 0 {
		variables["first"] = first
	}
	if err := connection.Execute(ctx, DocumentTestMailboxRules, variables, &result); err != nil {
		return nil, err
	}
	return result.TestMailboxRules, nil
}

// ApplyMailboxRules runs the stored rules over the mail already in a folder.
func ApplyMailboxRules(ctx context.Context, connection *Client, mailboxId, folderId string, first int) (*MailboxRuleApplication, error) {
	var result struct {
		ApplyMailboxRules *MailboxRuleApplication `json:"ApplyMailboxRules"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if folderId != "" {
		variables["folderId"] = folderId
	}
	if first > 0 {
		variables["first"] = first
	}
	if err := connection.Execute(ctx, DocumentApplyMailboxRules, variables, &result); err != nil {
		return nil, err
	}
	return result.ApplyMailboxRules, nil
}

// ListMailboxContacts returns the addresses the mailbox has corresponded
// with, most recent first, optionally those beginning with a prefix.
func ListMailboxContacts(ctx context.Context, connection *Client, mailboxId, prefix string, first int) ([]*MailboxContact, error) {
	var result struct {
		ListMailboxContacts []*MailboxContact `json:"ListMailboxContacts"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if prefix != "" {
		variables["prefix"] = prefix
	}
	if first > 0 {
		variables["first"] = first
	}
	if err := connection.Execute(ctx, DocumentListMailboxContacts, variables, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxContacts, nil
}

// SaveMailboxContact adds a contact or renames one. An empty name clears the
// name the mailbox learned.
func SaveMailboxContact(ctx context.Context, connection *Client, mailboxId, address, name string) (*MailboxContact, error) {
	var result struct {
		SaveMailboxContact *MailboxContact `json:"SaveMailboxContact"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "address": address}
	if name != "" {
		variables["name"] = name
	}
	if err := connection.Execute(ctx, DocumentSaveMailboxContact, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveMailboxContact, nil
}

// DeleteMailboxContact removes a contact. It comes back if that address
// writes again.
func DeleteMailboxContact(ctx context.Context, connection *Client, mailboxId, address string) error {
	variables := map[string]any{"mailboxId": mailboxId, "address": address}
	return connection.Execute(ctx, DocumentDeleteMailboxContact, variables, nil)
}

// ListMailboxAppPasswords returns the app passwords of a mailbox, one per
// device. The passwords themselves are never returned.
func ListMailboxAppPasswords(ctx context.Context, connection *Client, mailboxId string) ([]*MailboxAppPassword, error) {
	var result struct {
		ListMailboxAppPasswords []*MailboxAppPassword `json:"ListMailboxAppPasswords"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if err := connection.Execute(ctx, DocumentListMailboxAppPasswords, variables, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxAppPasswords, nil
}

// CreateMailboxAppPassword makes an app password for a device. The password
// is in the reply and nowhere else, ever.
func CreateMailboxAppPassword(ctx context.Context, connection *Client, mailboxId, name string) (*CreatedAppPassword, error) {
	var result struct {
		CreateMailboxAppPassword *CreatedAppPassword `json:"CreateMailboxAppPassword"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "name": name}
	if err := connection.Execute(ctx, DocumentCreateMailboxAppPassword, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateMailboxAppPassword, nil
}

// DeleteMailboxAppPassword revokes one device's app password.
func DeleteMailboxAppPassword(ctx context.Context, connection *Client, appPasswordId string) error {
	variables := map[string]any{"appPasswordId": appPasswordId}
	return connection.Execute(ctx, DocumentDeleteMailboxAppPassword, variables, nil)
}

// GetMailProgramSettings returns the hosts and ports a mail program needs.
func GetMailProgramSettings(ctx context.Context, connection *Client) (*MailProgramSettings, error) {
	var result struct {
		GetMailProgramSettings *MailProgramSettings `json:"GetMailProgramSettings"`
	}
	if err := connection.Execute(ctx, DocumentGetMailProgramSettings, nil, &result); err != nil {
		return nil, err
	}
	return result.GetMailProgramSettings, nil
}

// MailboxSubscription is one mailing list a mailbox receives: who sends it,
// how much of it there is, and whether leaving has been asked for.
type MailboxSubscription struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	From        string     `json:"from"`
	Count       int        `json:"count"`
	Unread      int        `json:"unread"`
	LastAt      time.Time  `json:"lastAt"`
	LastItemID  string     `json:"lastItemId"`
	OneClick    bool       `json:"oneClick"`
	Unsubscribe []string   `json:"unsubscribe"`
	RequestedAt *time.Time `json:"requestedAt"`
	Method      string     `json:"method"`
	Failed      bool       `json:"failed"`
	Error       string     `json:"error"`
}

// MailboxSubscriptionPage is a page of them, with how many there are in all.
type MailboxSubscriptionPage struct {
	Subscriptions []*MailboxSubscription `json:"subscriptions"`
	Total         int64                  `json:"total"`
}

// MailboxThreadView is the mail of one subscription, read as a conversation
// is: the messages it sent, newest first.
type MailboxThreadView struct {
	ThreadID  string               `json:"threadId"`
	Subject   string               `json:"subject"`
	Items     []*MailboxThreadItem `json:"items"`
	Truncated bool                 `json:"truncated"`
}

// MailboxThreadItem is one message in that view, with the folder it sits in.
type MailboxThreadItem struct {
	Item       *MailboxItem `json:"item"`
	FolderID   string       `json:"folderId"`
	FolderName string       `json:"folderName"`
	FolderKind string       `json:"folderKind"`
}

const (
	subscriptionFields = `{
		key name from count unread lastAt lastItemId oneClick unsubscribe
		requestedAt method failed error
	}`

	documentListMailboxSubscriptions = `query ($mailboxId: String!, $first: Int, $offset: Int) {
		ListMailboxSubscriptions(mailboxId: $mailboxId, first: $first, offset: $offset) {
			total
			subscriptions ` + subscriptionFields + `
		}
	}`

	documentGetMailboxSubscription = `query ($mailboxId: String!, $key: String!) {
		GetMailboxSubscription(mailboxId: $mailboxId, key: $key) ` + subscriptionFields + `
	}`

	documentReadMailboxSubscription = `query ($mailboxId: String!, $key: String!) {
		ReadMailboxSubscription(mailboxId: $mailboxId, key: $key) {
			threadId subject truncated
			items {
				folderId folderName folderKind
				item { id folderId seen flagged mail { id from fromName subject receivedAt } }
			}
		}
	}`

	documentUnsubscribeMailboxSubscription = `mutation ($mailboxId: String!, $key: String!) {
		UnsubscribeMailboxSubscription(mailboxId: $mailboxId, key: $key) ` + subscriptionFields + `
	}`
)

// ListMailboxSubscriptions is every mailing list a mailbox receives.
func ListMailboxSubscriptions(ctx context.Context, connection *Client, mailboxId string, first, offset int) (*MailboxSubscriptionPage, error) {
	var result struct {
		ListMailboxSubscriptions *MailboxSubscriptionPage `json:"ListMailboxSubscriptions"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if first > 0 {
		variables["first"] = first
	}
	if offset > 0 {
		variables["offset"] = offset
	}
	if err := connection.Execute(ctx, documentListMailboxSubscriptions, variables, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxSubscriptions, nil
}

// GetMailboxSubscription is one of them, by the key that identifies the list.
func GetMailboxSubscription(ctx context.Context, connection *Client, mailboxId, key string) (*MailboxSubscription, error) {
	var result struct {
		GetMailboxSubscription *MailboxSubscription `json:"GetMailboxSubscription"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "key": key}
	if err := connection.Execute(ctx, documentGetMailboxSubscription, variables, &result); err != nil {
		return nil, err
	}
	return result.GetMailboxSubscription, nil
}

// ReadMailboxSubscription is its mail, grouped the way a conversation is.
func ReadMailboxSubscription(ctx context.Context, connection *Client, mailboxId, key string) (*MailboxThreadView, error) {
	var result struct {
		ReadMailboxSubscription *MailboxThreadView `json:"ReadMailboxSubscription"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "key": key}
	if err := connection.Execute(ctx, documentReadMailboxSubscription, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadMailboxSubscription, nil
}

// UnsubscribeMailboxSubscription asks to leave a list, by whichever way the
// sender offered: a one-click request, a message to the address it named, or
// a page handed back for a person to open.
func UnsubscribeMailboxSubscription(ctx context.Context, connection *Client, mailboxId, key string) (*MailboxSubscription, error) {
	var result struct {
		UnsubscribeMailboxSubscription *MailboxSubscription `json:"UnsubscribeMailboxSubscription"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "key": key}
	if err := connection.Execute(ctx, documentUnsubscribeMailboxSubscription, variables, &result); err != nil {
		return nil, err
	}
	return result.UnsubscribeMailboxSubscription, nil
}
