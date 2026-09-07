package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
)

// The two things the settings and compose pages ask about a mailbox that are
// not the mailbox itself: who has written to it, and what its rules would do.

type MailboxRulesQuery interface {
	// People who have written to this mailbox, for completing an address
	ListMailboxContacts(ctx context.Context, arguments ListMailboxContactsArguments) ([]*models.MailboxContact, error)

	// Which of the newest messages in a folder each rule would match: a dry
	// run of rules as written, before they are saved
	TestMailboxRules(ctx context.Context, arguments TestMailboxRulesArguments) ([]*MailboxRuleTest, error)
}

type ListMailboxContactsArguments struct {
	MailboxID string `json:"mailboxId"`

	// Beginning of an address or a name; empty lists the most recent
	Prefix *string `json:"prefix"`

	// How many, at most 50
	First *int `json:"first"`
}

func (self *graph) ListMailboxContacts(ctx context.Context, arguments ListMailboxContactsArguments) ([]*models.MailboxContact, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	prefix := ""
	if arguments.Prefix != nil {
		prefix = strings.TrimSpace(*arguments.Prefix)
	}
	limit := 10
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, 50)
	}
	contacts, err := self.transaction(ctx).ListContacts(mailbox.ID, prefix, limit)
	if err != nil {
		return nil, err
	}
	if contacts == nil {
		contacts = []*models.MailboxContact{}
	}
	return contacts, nil
}

type TestMailboxRulesArguments struct {
	MailboxID string `json:"mailboxId"`

	// The rules as written on the page, saved or not
	Rules []models.MailboxRule `json:"rules"`

	// Folder to draw the messages from; the Inbox when unset
	FolderID *string `json:"folderId"`

	// How many of its newest messages, at most 100
	First *int `json:"first"`
}

// MailboxRuleTest is one message and the rules that would match it, in the
// order they would run, stopping where a rule says stop.
type MailboxRuleTest struct {
	Item *models.MailboxItem `json:"item"`

	// Indexes into the rules given, of those that matched
	Matched []int `json:"matched"`
}

func (self *graph) TestMailboxRules(ctx context.Context, arguments TestMailboxRulesArguments) ([]*MailboxRuleTest, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	var folder *models.MailboxFolder
	if arguments.FolderID != nil && *arguments.FolderID != "" {
		_, folder, err = self.requireFolder(ctx, models.PermissionMailRead, *arguments.FolderID)
		if err != nil {
			return nil, err
		}
		if folder.MailboxID != mailbox.ID {
			return nil, api.ErrNotFound
		}
	} else {
		folder, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil {
			return nil, err
		}
		if folder == nil {
			return []*MailboxRuleTest{}, nil
		}
	}
	limit := 20
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, 100)
	}
	items, err := tx.ListItems(folder.ID, &db.ItemOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}
	results := make([]*MailboxRuleTest, 0, len(items))
	for _, item := range items {
		result := &MailboxRuleTest{Item: item, Matched: []int{}}
		if item.Mail != nil {
			senderKnown := false
			if address, _ := senderAddressOf(item.Mail); address != "" {
				contact, err := tx.GetContact(mailbox.ID, address)
				if err != nil {
					return nil, err
				}
				senderKnown = contact != nil
			}
			for index, rule := range arguments.Rules {
				if !rule.Enabled {
					continue
				}
				if mx.RuleMatches(rule, item.Mail, senderKnown) {
					result.Matched = append(result.Matched, index)
					if rule.Stop {
						break
					}
				}
			}
		}
		results = append(results, result)
	}
	return results, nil
}

// senderAddressOf is the From address of a message, lower-cased, and its
// display name.
func senderAddressOf(mail *models.Mail) (string, string) {
	from := mail.From
	if from == "" {
		from = mail.Sender
	}
	return strings.ToLower(strings.TrimSpace(from)), ""
}
