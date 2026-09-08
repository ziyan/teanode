package db

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// The record of a person asking to leave a mailing list.
//
// The list itself is not stored: it is every message that named it, and it
// exists for as long as that mail does. This is the other half — what the
// person did about it — and it outlives the mail, so that somebody who
// unsubscribes and then deletes every message still sees that they asked.

type SubscriptionQuery interface {
	// ListSubscriptions is the mailing lists a mailbox receives, newest
	// first, with how much of each it holds and what has been asked of it.
	ListSubscriptions(mailboxId string, limit, offset int) ([]*models.MailboxSubscription, error)

	// CountSubscriptions is how many there are, for the count under the list.
	CountSubscriptions(mailboxId string) (int64, error)

	// GetSubscription is one of them, or nil when the mailbox has no mail
	// from that list.
	GetSubscription(mailboxId, listKey string) (*models.MailboxSubscription, error)

	// RecordUnsubscribe stores what was asked and how it went. Asking again
	// writes the same row: a second attempt after a failure is an attempt,
	// not a second subscription.
	RecordUnsubscribe(mailboxId, listKey, method string, failed bool, reason string) error
}

type mailboxSubscriptionModel struct {
	ID          string `gorm:"primary_key:true;size:32"`
	CreatedAt   time.Time
	ModifiedAt  time.Time
	MailboxID   string `gorm:"column:mailbox_id;size:32"`
	ListKey     string `gorm:"column:list_key;type:text"`
	RequestedAt *time.Time
	Method      string `gorm:"size:16"`
	Failed      bool
	Error       string `gorm:"type:text"`
}

func (self *mailboxSubscriptionModel) TableName() string {
	return "mailbox_subscription"
}

// listUnsubscribeRequests is what has been asked of each of these lists, for
// the listing to hang on its rows.
func (self *transaction) listUnsubscribeRequests(mailboxId string, keys []string) (map[string]*mailboxSubscriptionModel, error) {
	requests := map[string]*mailboxSubscriptionModel{}
	if len(keys) == 0 {
		return requests, nil
	}
	var rows []mailboxSubscriptionModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"list_key\" IN ?", mailboxId, keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		row := rows[index]
		if row.RequestedAt != nil {
			at := row.RequestedAt.In(time.Local)
			row.RequestedAt = &at
		}
		requests[row.ListKey] = &row
	}
	return requests, nil
}

func (self *transaction) GetSubscription(mailboxId, listKey string) (*models.MailboxSubscription, error) {
	if listKey == "" {
		return nil, nil
	}
	// One list is the listing narrowed to it: the same grouping, the same
	// counts, and the same record of what was asked, rather than a second
	// query that could answer differently.
	subscriptions, err := self.ListSubscriptions(mailboxId, 0, 0)
	if err != nil {
		return nil, err
	}
	for _, subscription := range subscriptions {
		if subscription.Key == listKey {
			return subscription, nil
		}
	}
	return nil, nil
}

func (self *transaction) RecordUnsubscribe(mailboxId, listKey, method string, failed bool, reason string) error {
	now := time.Now().In(time.Local)
	var existing []mailboxSubscriptionModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"list_key\" = ?", mailboxId, listKey).
		Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) > 0 {
		return self.tx.Model(&mailboxSubscriptionModel{}).
			Where("\"id\" = ?", existing[0].ID).
			Updates(map[string]any{
				"modified_at":  now,
				"requested_at": now,
				"method":       method,
				"failed":       failed,
				"error":        reason,
			}).Error
	}
	return self.tx.Create(&mailboxSubscriptionModel{
		ID:          security.NewULID(),
		CreatedAt:   now,
		ModifiedAt:  now,
		MailboxID:   mailboxId,
		ListKey:     listKey,
		RequestedAt: &now,
		Method:      method,
		Failed:      failed,
		Error:       reason,
	}).Error
}
