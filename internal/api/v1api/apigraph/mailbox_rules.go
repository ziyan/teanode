package apigraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
)

// The two things the settings and compose pages ask about a mailbox that are
// not the mailbox itself: who has written to it, and what its rules would do.

type MailboxContactMutation interface {
	// Add a contact, or rename one
	SaveMailboxContact(ctx context.Context, arguments SaveMailboxContactArguments) (*models.MailboxContact, error)

	// Remove a contact; it comes back when that address writes again
	DeleteMailboxContact(ctx context.Context, arguments DeleteMailboxContactArguments) error

	// Forget several contacts at once, saying how many were removed
	DeleteMailboxContacts(ctx context.Context, arguments DeleteMailboxContactsArguments) (int, error)
}

type SaveMailboxContactArguments struct {
	MailboxID string `json:"mailboxId"`
	Address   string `json:"address"`
	Name      string `json:"name" graphapi:"nullable"`
}

func (self *graph) SaveMailboxContact(ctx context.Context, arguments SaveMailboxContactArguments) (*models.MailboxContact, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	if address := strings.TrimSpace(arguments.Address); len(address) > 320 || !models.IsEmailAddress(address) {
		return nil, fmt.Errorf("%w: %q is not an address", api.ErrInvalidArguments, arguments.Address)
	}
	contact, err := self.transaction(ctx).SaveContact(mailbox.ID, arguments.Address, arguments.Name)
	if err != nil {
		return nil, translateError(err)
	}
	return contact, nil
}

type DeleteMailboxContactArguments struct {
	MailboxID string `json:"mailboxId"`
	Address   string `json:"address"`
}

func (self *graph) DeleteMailboxContact(ctx context.Context, arguments DeleteMailboxContactArguments) error {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return err
	}
	return translateError(self.transaction(ctx).DeleteContact(mailbox.ID, arguments.Address))
}

type DeleteMailboxContactsArguments struct {
	MailboxID string `json:"mailboxId"`

	// The addresses to forget, as the list gives them
	Addresses []string `json:"addresses"`
}

// DeleteMailboxContacts forgets several at once, which is how a list of them
// is tidied: one at a time is a dialog per row.
//
// It reports how many were removed rather than failing on the first address
// that is already gone — two people tidying the same list should not turn one
// of them into an error.
func (self *graph) DeleteMailboxContacts(ctx context.Context, arguments DeleteMailboxContactsArguments) (int, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return 0, err
	}
	tx := self.transaction(ctx)
	removed := 0
	for _, address := range arguments.Addresses {
		if strings.TrimSpace(address) == "" {
			continue
		}
		if err := tx.DeleteContact(mailbox.ID, address); err != nil {
			return removed, translateError(err)
		}
		removed++
	}
	log.Noticef("%s forgot %d contacts of mailbox %q", operatorName(ctx), removed, mailbox.ID)
	return removed, nil
}

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

	// How many, at most 500
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
		limit = min(*arguments.First, 500)
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
	insights, err := tx.GetMailInsights(mailbox.ID, mailIdsOf(items))
	if err != nil {
		return nil, err
	}
	// A rule reads headers the row does not carry — To, Cc, any header a
	// condition names — so each message is read back from storage, the way
	// the rule saw it when it arrived.
	results := make([]*MailboxRuleTest, 0, len(items))
	for _, item := range items {
		result := &MailboxRuleTest{Item: item, Matched: []int{}}
		if item.Mail != nil {
			if headers, _, err := self.storage.Get(ctx, item.Mail.ID); err == nil {
				item.Mail.Headers = headers
			}
			senderKnown := false
			if address, _ := senderAddressOf(item.Mail); address != "" {
				contact, err := tx.GetContact(mailbox.ID, address)
				if err != nil {
					return nil, err
				}
				// Known means written to before this message: the contact
				// is touched before the rules run on arrival, so the
				// message being judged counts once already.
				senderKnown = contact != nil && contact.Count > 1
			}
			for index, rule := range arguments.Rules {
				if !rule.Enabled {
					continue
				}
				if mx.RuleMatches(rule, item.Mail, senderKnown, insights[item.Mail.ID]) {
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
