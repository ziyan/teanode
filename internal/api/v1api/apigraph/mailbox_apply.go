package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
)

type ApplyMailboxRulesArguments struct {
	MailboxID string `json:"mailboxId"`

	// The folder to run over; the Inbox when not given
	FolderID *string `json:"folderId"`

	// How many of the newest messages to consider, at most 500; 200 when not given
	First *int `json:"first"`
}

// MailboxRuleApplication is what applying the rules did.
type MailboxRuleApplication struct {
	// Messages looked at
	Considered int `json:"considered"`

	// Messages at least one rule matched
	Matched int `json:"matched"`

	// Actions carried out, by kind
	Moved   int `json:"moved"`
	Marked  int `json:"marked"`
	Flagged int `json:"flagged"`
	Deleted int `json:"deleted"`

	// Forward actions that matched and were left alone: old mail is not
	// resent
	Skipped int `json:"skipped"`
}

// ApplyMailboxRules runs a mailbox's rules over the mail already in a folder,
// the way arrival runs them over a new message. A rule written today does
// not reach into yesterday's Inbox by itself; this is the button that makes
// it. Forwarding is the one action not repeated: a forward on arrival sent
// the message once, and sending old mail again is not what a rule means.
func (self *graph) ApplyMailboxRules(ctx context.Context, arguments ApplyMailboxRulesArguments) (*MailboxRuleApplication, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	var folder *models.MailboxFolder
	if arguments.FolderID != nil && *arguments.FolderID != "" {
		_, folder, err = self.requireFolder(ctx, models.PermissionMailWrite, *arguments.FolderID)
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
			return &MailboxRuleApplication{}, nil
		}
	}
	limit := 200
	if arguments.First != nil && *arguments.First > 0 {
		limit = min(*arguments.First, 500)
	}
	items, err := tx.ListItems(folder.ID, &db.ItemOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}

	result := &MailboxRuleApplication{Considered: len(items)}
	for _, item := range items {
		if item.Mail == nil {
			continue
		}
		// The rule reads headers the row does not carry, the way it did on
		// arrival.
		if headers, _, err := self.storage.Get(ctx, item.Mail.ID); err == nil {
			item.Mail.Headers = headers
		}
		senderKnown := false
		if address, _ := senderAddressOf(item.Mail); address != "" {
			contact, err := tx.GetContact(mailbox.ID, address)
			if err != nil {
				return nil, err
			}
			senderKnown = contact != nil
		}
		fired := matchingRules(mailbox.Rules, item.Mail, senderKnown)
		if len(fired) == 0 {
			continue
		}
		result.Matched++
		current := item
		for _, index := range fired {
			for _, action := range mailbox.Rules[index].Actions {
				if current == nil {
					break
				}
				current, err = self.applyRuleAction(tx, mailbox, action, current, result)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return result, nil
}

// matchingRules is which rules fire for a message, in order, stopping after
// one that says so: the same walk arrival makes.
func matchingRules(rules []models.MailboxRule, mail *models.Mail, senderKnown bool) []int {
	var fired []int
	for index, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if mx.RuleMatches(rule, mail, senderKnown) {
			fired = append(fired, index)
			if rule.Stop {
				break
			}
		}
	}
	return fired
}

// applyRuleAction does what arrival does for one action, counting it, and
// returns the item where it now is: nil once it is gone.
func (self *graph) applyRuleAction(tx db.Transaction, mailbox *models.Mailbox, action models.MailboxRuleAction, item *models.MailboxItem, result *MailboxRuleApplication) (*models.MailboxItem, error) {
	yes := true
	switch action.Kind {
	case "markRead":
		if _, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Seen: &yes}); err != nil {
			return item, err
		}
		result.Marked++
		return item, nil
	case "flag":
		if _, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Flagged: &yes}); err != nil {
			return item, err
		}
		result.Flagged++
		return item, nil
	case "move":
		target, err := tx.GetFolder(action.FolderID)
		if err != nil {
			return item, err
		}
		if target == nil || target.MailboxID != mailbox.ID || target.ID == item.FolderID {
			// The folder was removed after the rule was written, or the
			// message is there already.
			return item, nil
		}
		moved, err := tx.MoveItems([]string{item.ID}, target.ID)
		if err != nil || len(moved) == 0 {
			return item, err
		}
		result.Moved++
		return moved[0], nil
	case "delete":
		trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
		if err != nil {
			return item, err
		}
		if trash == nil {
			_, err := tx.DeleteItems([]string{item.ID})
			if err == nil {
				result.Deleted++
			}
			return nil, err
		}
		if trash.ID == item.FolderID {
			return item, nil
		}
		moved, err := tx.MoveItems([]string{item.ID}, trash.ID)
		if err != nil || len(moved) == 0 {
			return item, err
		}
		result.Deleted++
		return moved[0], nil
	case "forward":
		result.Skipped++
		return item, nil
	default:
		return item, nil
	}
}
