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

	// SetSubscriptionMuted turns a list's mail away from the Inbox, or lets it
	// back. The row outlives the mail, so a list muted while it was quiet is
	// still muted when it writes again.
	SetSubscriptionMuted(mailboxId, listKey string, muted bool) error

	// SubscriptionIsMuted is asked by delivery for every message that names a
	// list, so it is one row by primary key and nothing more.
	SubscriptionIsMuted(mailboxId, listKey string) (bool, error)

	// SetSubscriptionImages says that this list's pictures may be loaded
	// without asking, every time, or takes that back.
	SetSubscriptionImages(mailboxId, listKey string, show bool) error
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
	Error       string     `gorm:"type:text"`
	MutedAt     *time.Time `gorm:"column:muted_at"`
	ImagesAt    *time.Time `gorm:"column:images_at"`
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

// SetSubscriptionMuted records that a list should keep arriving and stop being
// in the way, or that it should stop doing so.
//
// The same row the unsubscribe request uses, for the same reason: it outlives
// the mail, so a list muted while it was quiet is still muted when it writes
// again months later.
func (self *transaction) SetSubscriptionMuted(mailboxId, listKey string, muted bool) error {
	return self.setSubscriptionTime(mailboxId, listKey, "muted_at", muted)
}

// setSubscriptionTime sets or clears one of the row's "when they asked for
// this" columns, creating the row when there is something to record. Turning
// something off that was never on is not a row.
func (self *transaction) setSubscriptionTime(mailboxId, listKey, column string, on bool) error {
	now := time.Now().In(time.Local)
	var at *time.Time
	if on {
		at = &now
	}

	var existing []mailboxSubscriptionModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"list_key\" = ?", mailboxId, listKey).
		Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) > 0 {
		return self.tx.Model(&mailboxSubscriptionModel{}).
			Where("\"id\" = ?", existing[0].ID).
			Updates(map[string]any{"modified_at": now, column: at}).Error
	}
	if !on {
		return nil
	}
	row := &mailboxSubscriptionModel{
		ID:         security.NewULID(),
		CreatedAt:  now,
		ModifiedAt: now,
		MailboxID:  mailboxId,
		ListKey:    listKey,
	}
	switch column {
	case "muted_at":
		row.MutedAt = at
	case "images_at":
		row.ImagesAt = at
	}
	return self.tx.Create(row).Error
}

// SetSubscriptionImages says the pictures in this list's mail may be loaded
// without asking. A newsletter is pictures with a few words around them, and a
// reader who trusts one sender should be able to say so once rather than on
// every issue.
func (self *transaction) SetSubscriptionImages(mailboxId, listKey string, show bool) error {
	return self.setSubscriptionTime(mailboxId, listKey, "images_at", show)
}

// SubscriptionIsMuted answers the delivery path, which asks for every message
// that names a list.
func (self *transaction) SubscriptionIsMuted(mailboxId, listKey string) (bool, error) {
	if mailboxId == "" || listKey == "" {
		return false, nil
	}
	var count int64
	err := self.tx.Model(&mailboxSubscriptionModel{}).
		Where("\"mailbox_id\" = ? AND \"list_key\" = ? AND \"muted_at\" IS NOT NULL", mailboxId, listKey).
		Count(&count).Error
	return count > 0, err
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
