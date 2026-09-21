package mailer

import (
	"context"
	"fmt"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *mailer) AcceptSubmission(ctx context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *Message) (*models.Mail, error) {
	if envelope == nil || envelope.MailboxID == "" {
		return nil, fmt.Errorf("mailer: submission must name a mailbox")
	}
	var accepted *models.Mail
	err := transaction.TransactionContext(ctx, func(command db.Transaction) error {
		composed, err := self.compose(command, message)
		if err != nil {
			return err
		}
		if err := prepareEnvelope(envelope, message, composed); err != nil {
			return err
		}
		accepted, err = self.exchange.AcceptSubmission(ctx, command, envelope)
		return err
	})
	if err != nil {
		return nil, err
	}
	return accepted, nil
}
