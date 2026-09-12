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

	// Actions carried out, by kind. A message counts once for each kind,
	// however many rules asked for it, and marking one that was already
	// read is not counted at all.
	Moved   int `json:"moved"`
	Marked  int `json:"marked"`
	Flagged int `json:"flagged"`
	Deleted int `json:"deleted"`

	// Forward actions that matched and were left alone: old mail is not
	// resent
	Skipped int `json:"skipped"`

	// Messages that could not be filed: the stored message could not be
	// read, or a change to it failed. The rest were still filed.
	Failed int `json:"failed"`
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
	// Whether anything needs the stored message or the contacts at all: a
	// rule about the sender or the subject is answered by the row, and
	// reading every message back from storage to answer it would hold this
	// transaction open across a network for nothing.
	needsHeaders, needsContacts := false, false
	for _, rule := range mailbox.Rules {
		if !rule.Enabled {
			continue
		}
		for _, condition := range rule.Conditions {
			switch condition.Field {
			case "to", "header":
				needsHeaders = true
			case "sender-known":
				needsContacts = true
			}
		}
	}
	items, err := tx.ListItems(folder.ID, &db.ItemOptions{Limit: limit})
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}

	// What the agent worked out about these messages, for the rules that
	// read it; a message without an insight fails those conditions, as it
	// would on arrival before the insight exists.
	insights, err := tx.GetMailInsights(mailbox.ID, mailIdsOf(items))
	if err != nil {
		return nil, err
	}

	result := &MailboxRuleApplication{}
	for _, item := range items {
		if item.Mail == nil {
			continue
		}
		result.Considered++
		// A rule reads headers the row does not carry, the way it did on
		// arrival. A message that cannot be read is counted rather than
		// judged against headers it does not have.
		if needsHeaders {
			headers, _, err := self.storage.Get(ctx, item.Mail.ID)
			if err != nil {
				log.Warningf("the stored message %q of mailbox %q could not be read, so the rules were not run over it: %s",
					item.Mail.ID, mailbox.ID, err)
				result.Failed++
				continue
			}
			item.Mail.Headers = headers
		}
		senderKnown := false
		if needsContacts {
			if address, _ := senderAddressOf(item.Mail); address != "" {
				contact, err := tx.GetLearnedContact(mailbox.ID, address)
				if err != nil {
					return nil, err
				}
				// Known means written to before this message, which is what
				// arrival means by it: the contact is touched before the
				// rules run, so its first message counts once already.
				senderKnown = contact != nil && contact.Count > 1
			}
		}
		fired := matchingRules(mailbox.Rules, item.Mail, senderKnown, insights[item.Mail.ID])
		if len(fired) == 0 {
			continue
		}
		result.Matched++
		// What this message had done to it, so that two rules moving it
		// count it once.
		done := map[string]bool{}
		current := item
		for _, index := range fired {
			for _, action := range mailbox.Rules[index].Actions {
				if current == nil {
					break
				}
				next, err := self.applyRuleAction(tx, mailbox, action, current, done)
				if err != nil {
					// One message that cannot be filed does not undo the
					// ones already filed, the way arrival keeps going
					// rather than losing a message to a bad rule.
					log.Warningf("a rule of mailbox %q could not be applied to %q: %s", mailbox.ID, current.ID, err)
					result.Failed++
					break
				}
				current = next
			}
		}
		result.Moved += count(done, "move")
		result.Marked += count(done, "markRead")
		result.Flagged += count(done, "flag")
		result.Deleted += count(done, "delete")
		result.Skipped += count(done, "forward")
	}
	return result, nil
}

// count is one if a message had this done to it.
func count(done map[string]bool, kind string) int {
	if done[kind] {
		return 1
	}
	return 0
}

// matchingRules is which rules fire for a message, in order, stopping after
// one that says so: the same walk arrival makes.
func matchingRules(rules []models.MailboxRule, mail *models.Mail, senderKnown bool, insight *models.MailInsight) []int {
	var fired []int
	for index, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if mx.RuleMatches(rule, mail, senderKnown, insight) {
			fired = append(fired, index)
			if rule.Stop {
				break
			}
		}
	}
	return fired
}

// applyRuleAction does what arrival does for one action, recording what it
// did, and returns the item where it now is: nil once it is gone. Nothing is
// recorded for an action that changed nothing — a message already read, or
// already in the folder a rule moves it to.
func (self *graph) applyRuleAction(tx db.Transaction, mailbox *models.Mailbox, action models.MailboxRuleAction, item *models.MailboxItem, done map[string]bool) (*models.MailboxItem, error) {
	yes := true
	switch action.Kind {
	case "markRead":
		changed, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Seen: &yes})
		if err != nil {
			return item, err
		}
		if changed > 0 {
			done["markRead"] = true
		}
		return item, nil
	case "flag":
		changed, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Flagged: &yes})
		if err != nil {
			return item, err
		}
		if changed > 0 {
			done["flag"] = true
		}
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
		done["move"] = true
		return moved[0], nil
	case "delete":
		trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
		if err != nil {
			return item, err
		}
		if trash == nil {
			_, err := tx.DeleteItems([]string{item.ID})
			if err == nil {
				done["delete"] = true
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
		done["delete"] = true
		return moved[0], nil
	case "forward":
		done["forward"] = true
		return item, nil
	default:
		return item, nil
	}
}

// mailIdsOf is the messages behind a page of items, for one lookup.
func mailIdsOf(items []*models.MailboxItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Mail != nil {
			ids = append(ids, item.Mail.ID)
		}
	}
	return ids
}
