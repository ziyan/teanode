package agent

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// The tax document address: the person's own mailbox address with a
// detail after a plus, taxes- and a random token, as the statement import
// address is made. A message mailed there is filed in the Archive, read,
// and the tax_mail job copies its PDFs and pictures into tax document
// uploads, queues the reading of each as a paystub or a W-2, and tells the
// person what arrived. The token is kept sealed on the agent
// (agent.tax_document_token) and is the whole of the gate; regenerating it
// ends the old address.

// TaxDocumentAddressDetailPrefix begins the detail of a tax document
// address: the part after the plus, before the token.
const TaxDocumentAddressDetailPrefix = "taxes-"

// taxMailNoticeRetry is how long the notice of a mailed tax document waits
// when a turn is running in the conversation it is told in.
const taxMailNoticeRetry = time.Minute

// taxDocumentToken is the agent's tax document token, made and kept the
// first time it is asked for, or a new one in its place when isRenewed.
func (self *Agent) taxDocumentToken(ctx context.Context, agentRow *models.Agent, isRenewed bool) (string, error) {
	var token string
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		// Read again inside the transaction, the token kept now.
		current, err := tx.GetAgent(agentRow.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return db.ErrNotFound
		}
		if current.TaxDocumentToken != "" && !isRenewed {
			token, err = self.OpenSecret(current.TaxDocumentToken)
			return err
		}
		if token, err = newStatementToken(); err != nil {
			return err
		}
		sealed, err := self.SealSecret(token)
		if err != nil {
			return err
		}
		_, err = tx.UpdateAgent(agentRow.ID, func(changed *models.Agent) error {
			changed.TaxDocumentToken = sealed
			return nil
		})
		return err
	})
	return token, err
}

// TaxDocumentAddress is the address paystubs and W-2s are mailed to: the
// local part of the person's first mailbox address, a plus, taxes- and the
// token, at that address's domain, the token made the first time. Empty
// when the person has no mailbox with an address. A new token in the old
// one's place when isRenewed, which ends the old address at once.
func (self *Agent) TaxDocumentAddress(ctx context.Context, agentRow *models.Agent, owner *models.User, isRenewed bool) (string, error) {
	token, err := self.taxDocumentToken(ctx, agentRow, isRenewed)
	if err != nil {
		return "", err
	}
	var address string
	err = self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		mailboxes, err := tx.ListMailboxes(owner.ID)
		if err != nil {
			return err
		}
		for _, mailbox := range mailboxes {
			for _, mailboxAddress := range mailbox.Addresses {
				if mailboxAddress.LocalPart != "" && mailboxAddress.Domain != "" {
					address = mailparse.UnsplitAddress(mailboxAddress.LocalPart+"+"+TaxDocumentAddressDetailPrefix+token, mailboxAddress.Domain)
					return nil
				}
			}
		}
		return nil
	})
	return address, err
}

// IsTaxDocumentToken is the delivery hook's question: is this the token of
// the tax document address of the mailbox's owner? Only when the agent is
// on, finance is offered and the person's agent is active. Asked inside the
// delivery transaction, so it reads and decides and nothing more.
func (self *Agent) IsTaxDocumentToken(tx db.Transaction, mailbox *models.Mailbox, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if mailbox == nil || token == "" {
		return false
	}
	configuration := self.settings.Configuration()
	if !configuration.Agent.Enabled || !isFinanceOffered(configuration) {
		return false
	}
	agentRow, err := tx.GetAgentByUser(mailbox.UserID)
	if err != nil || agentRow == nil || !agentRow.Active() || agentRow.TaxDocumentToken == "" {
		return false
	}
	expected, err := self.OpenSecret(agentRow.TaxDocumentToken)
	if err != nil || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(expected)), []byte(token)) == 1
}

// OnTaxDocumentDelivery is the delivery hook for a message that reached a
// tax document address, already filed: it queues the tax_mail job, in the
// delivery transaction, and does nothing else there.
func (self *Agent) OnTaxDocumentDelivery(tx db.Transaction, mailbox *models.Mailbox, item *models.MailboxItem, mail *models.Mail) {
	agentRow, err := tx.GetAgentByUser(mailbox.UserID)
	if err != nil || agentRow == nil {
		log.Warningf("cannot find the agent to read the tax documents in message %q: %v", mail.ID, err)
		return
	}
	if _, err := self.Enqueue(tx, models.AgentJobTaxMail, agentRow.ID, mailbox.ID, mail.ID); err != nil {
		log.Warningf("cannot queue the reading of the tax documents in message %q: %s", mail.ID, err)
	}
}

// runTaxMail is the job a message at the tax document address queues: copy
// its PDFs and pictures into tax document uploads, queue the reading of
// each, and tell the person what arrived. A retry copies nothing twice,
// since a file already copied is found again by its name and bytes.
func (self *Agent) runTaxMail(ctx context.Context, run *Run) error {
	mailId := run.Job.SubjectID
	var mail *models.Mail
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		mail, err = tx.GetMail(mailId, nil)
		return err
	}); err != nil {
		return err
	}
	if mail == nil {
		return nil
	}
	copied, err := self.CopyMailTaxDocuments(ctx, run.Agent, mailId)
	if err != nil {
		// The message is stored just after the delivery commits; a job that
		// runs first tries again on the ladder.
		return fmt.Errorf("the message with the tax documents cannot be read yet: %w", err)
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		for _, attachment := range copied {
			if _, err := self.QueueTaxDocumentReading(tx, run.Agent.ID, attachment.ID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	checkIn := models.AlertMarker + " A message arrived at " + personName(run.Owner) + "'s tax document address; you told them what came in. This is not the person speaking. What you said follows."
	err = self.tellMailImport(ctx, run, checkIn, TaxMailSummary(copied, CanReadPDFs()))
	if errors.Is(err, errTurnRunning) {
		return &Deferral{Until: time.Now().Add(taxMailNoticeRetry), Reason: "a turn is running in the conversation the tax documents' notice goes to"}
	}
	return err
}

// TaxMailSummary is what came of a message mailed to the tax document
// address, in a sentence for the person: the files being read, or why
// nothing is.
func TaxMailSummary(copied []*models.AgentAttachment, canReadPDFs bool) string {
	if len(copied) == 0 {
		return "A message reached your tax document address, but it carries no PDF or picture to read a paystub or W-2 out of."
	}
	names := make([]string, 0, len(copied))
	hasPDF := false
	for _, attachment := range copied {
		names = append(names, attachment.Name)
		hasPDF = hasPDF || IsPDFAttachment(attachment)
	}
	summary := fmt.Sprintf("Received %s at your tax document address (%s) and reading %s as paystubs or W-2s; they show under Finance, Taxes once read.",
		countOf(len(copied), "file", "files"), strings.Join(names, ", "), map[bool]string{true: "it", false: "them"}[len(copied) == 1])
	if hasPDF && !canReadPDFs {
		summary += " This server cannot read PDFs, so send a photo or a screenshot of a PDF instead."
	}
	return summary
}
