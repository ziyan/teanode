package mx

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A mailbox's rules run in order against a message just placed in its Inbox.
// Every condition of a rule must hold for its actions to run; a rule that
// stops ends the run. A failing rule is logged and skipped: a person's
// misspelled pattern must not lose them the message.

func (self *exchange) runRules(tx db.Transaction, mailbox *models.Mailbox, inbox *models.MailboxFolder, item *models.MailboxItem, mail *models.Mail) error {
	current := item
	for index, rule := range mailbox.Rules {
		if !rule.Enabled || current == nil {
			continue
		}
		matched, err := self.ruleMatches(tx, mailbox, rule, mail)
		if err != nil {
			log.Warningf("rule %d (%q) of mailbox %q could not be evaluated: %s", index, rule.Name, mailbox.ID, err)
			continue
		}
		if !matched {
			continue
		}
		for _, action := range rule.Actions {
			current, err = self.runRuleAction(tx, mailbox, action, current, mail)
			if err != nil {
				log.Warningf("rule %d (%q) of mailbox %q failed: %s", index, rule.Name, mailbox.ID, err)
				break
			}
			if current == nil {
				break
			}
		}
		if rule.Stop {
			break
		}
	}
	return nil
}

func (self *exchange) ruleMatches(tx db.Transaction, mailbox *models.Mailbox, rule models.MailboxRule, mail *models.Mail) (bool, error) {
	for _, condition := range rule.Conditions {
		holds, err := self.conditionHolds(tx, mailbox, condition, mail)
		if err != nil {
			return false, err
		}
		if !holds {
			return false, nil
		}
	}
	return true, nil
}

// RuleMatches is the dry run the settings page offers: which of the last
// messages would this rule match.
func RuleMatches(rule models.MailboxRule, mail *models.Mail, senderKnown bool) bool {
	for _, condition := range rule.Conditions {
		if !conditionHoldsWithout(condition, mail, senderKnown) {
			return false
		}
	}
	return true
}

func (self *exchange) conditionHolds(tx db.Transaction, mailbox *models.Mailbox, condition models.MailboxRuleCondition, mail *models.Mail) (bool, error) {
	senderKnown := false
	if condition.Field == "sender-known" {
		address, _ := senderOf(mail)
		contact, err := tx.GetContact(mailbox.ID, address)
		if err != nil {
			return false, err
		}
		// Seen before this message: the one arriving now counts once.
		senderKnown = contact != nil && contact.Count > 1
	}
	return conditionHoldsWithout(condition, mail, senderKnown), nil
}

func conditionHoldsWithout(condition models.MailboxRuleCondition, mail *models.Mail, senderKnown bool) bool {
	switch condition.Field {
	case "any":
		return true
	case "sender-known":
		return senderKnown
	case "score":
		var score float64
		if mail.AuthenticationResults.SpamFilter != nil {
			score = mail.AuthenticationResults.SpamFilter.Score
		}
		threshold, err := strconv.ParseFloat(strings.TrimSpace(condition.Value), 64)
		if err != nil {
			return false
		}
		switch condition.Operator {
		case "above":
			return score > threshold
		case "below":
			return score < threshold
		}
		return false
	}
	var subject string
	switch condition.Field {
	case "from":
		// The header when the message is in hand, as it is on receipt; the
		// row's own address when only the row was read, as in a dry run.
		subject = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "From"))
		if subject == "" {
			subject = mail.From
			if subject == "" {
				subject = mail.Sender
			}
		}
	case "to":
		subject = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "To")) + " " +
			mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "Cc")) + " " + strings.Join(mail.Recipients, " ")
	case "subject":
		subject = mail.Subject
	case "header":
		subject = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, condition.Header))
	default:
		return false
	}
	return compare(condition.Operator, subject, condition.Value)
}

func compare(operator, subject, value string) bool {
	switch operator {
	case "contains", "":
		return strings.Contains(strings.ToLower(subject), strings.ToLower(value))
	case "equals":
		return strings.EqualFold(strings.TrimSpace(subject), strings.TrimSpace(value))
	case "matches":
		pattern, err := regexp.Compile("(?i)" + value)
		if err != nil {
			return false
		}
		return pattern.MatchString(subject)
	}
	return false
}

func (self *exchange) runRuleAction(tx db.Transaction, mailbox *models.Mailbox, action models.MailboxRuleAction, item *models.MailboxItem, mail *models.Mail) (*models.MailboxItem, error) {
	yes := true
	switch action.Kind {
	case "markRead":
		_, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Seen: &yes})
		return item, err
	case "flag":
		_, err := tx.SetItemFlags([]string{item.ID}, models.MailboxItemFlags{Flagged: &yes})
		return item, err
	case "move":
		folder, err := tx.GetFolder(action.FolderID)
		if err != nil {
			return item, err
		}
		if folder == nil || folder.MailboxID != mailbox.ID {
			// The folder was removed after the rule was written.
			return item, nil
		}
		moved, err := tx.MoveItems([]string{item.ID}, folder.ID)
		if err != nil || len(moved) == 0 {
			return item, err
		}
		return moved[0], nil
	case "delete":
		trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
		if err != nil {
			return item, err
		}
		if trash == nil {
			_, err := tx.DeleteItems([]string{item.ID})
			return nil, err
		}
		moved, err := tx.MoveItems([]string{item.ID}, trash.ID)
		if err != nil || len(moved) == 0 {
			return item, err
		}
		return moved[0], nil
	case "forward":
		// Not back to where it has already been: a message that carries
		// the forward's own address as a Delivered-To has been through it,
		// and two mailboxes forwarding to each other would otherwise pass
		// it back and forth, a Received header longer each time, until it
		// outgrew the size limit — or for ever, without one.
		if !forwardable(mail, action.Address) {
			log.Warningf("not forwarding mail %q from mailbox %q to %q: it has been there, or has been forwarded too often", mail.ID, mailbox.ID, action.Address)
			return item, nil
		}
		// A forward is a delivery like any alias's: signed, queued, recorded.
		_, err := tx.CreateDelivery(&models.Delivery{
			MailID:      mail.ID,
			Mail:        mail,
			Recipient:   action.Address,
			Kind:        models.DeliveryKindForward,
			Status:      models.DeliveryStatusQueued,
			Method:      "email",
			Destination: action.Address,
			MailboxID:   mailbox.ID,
		}, nil)
		return item, err
	}
	return item, nil
}

// maximumForwardHops is how many Received headers a message may carry and
// still be forwarded by a rule. Real mail crosses a handful of hosts; one
// that has crossed this many is going round in circles.
const maximumForwardHops = 25

// forwardable says whether a rule may forward a message to an address: not
// one it has already been delivered to, and not one that has been forwarded
// more times than any message has a reason to be.
func forwardable(mail *models.Mail, address string) bool {
	if mailparse.CountHeaders(mail.Headers, "Received") > maximumForwardHops {
		return false
	}
	for _, header := range mail.Headers {
		key, value := mailparse.SplitHeader(header)
		if !strings.EqualFold(key, "Delivered-To") {
			continue
		}
		delivered, err := mailparse.ParseAddress(strings.TrimSpace(value))
		if err != nil {
			delivered = strings.TrimSpace(value)
		}
		if strings.EqualFold(delivered, address) {
			return false
		}
	}
	return true
}
