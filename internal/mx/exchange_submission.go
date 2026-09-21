package mx

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

func (self *exchange) AcceptSubmission(ctx context.Context, transaction db.Transaction, envelope *mailparse.Envelope) (*models.Mail, error) {
	if envelope == nil || envelope.MailboxID == "" || envelope.SpecialPrefix != "" {
		return nil, fmt.Errorf("submission must name a mailbox")
	}
	var accepted *models.Mail
	err := transaction.TransactionContext(ctx, func(command db.Transaction) error {
		deliveries, err := self.prepareOutgoing(ctx, command, envelope, false)
		if err != nil {
			return err
		}
		if len(deliveries) == 0 {
			return mailparse.ErrMailBoxUnavailable
		}
		mails := distinctMails(deliveries)
		if len(mails) != 1 {
			return fmt.Errorf("submission did not produce one stored message")
		}
		mail := mails[0]
		// Storage must succeed before SQL acceptance can commit. A later SQL
		// failure can leave an unreferenced object for the retention sweep.
		if err := self.storage.Put(ctx, mail.ID, mail.Headers, mail.Body); err != nil {
			return fmt.Errorf("store submitted message: %w", err)
		}
		retryAt := time.Now()
		for _, delivery := range pendingDeliveries(deliveries) {
			if _, err := command.ModifyDelivery(delivery.ID, func(stored *models.Delivery) error {
				stored.RetryAt = &retryAt
				return nil
			}, nil); err != nil {
				return err
			}
		}
		command.AfterCommit(self.wakeDeliveryQueue)
		accepted = mail
		return nil
	})
	if err != nil {
		return nil, err
	}
	return accepted, nil
}
