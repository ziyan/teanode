package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// Invitations that arrived as mail, and the queue for reading them.
//
// Delivery writes a row and does no more: reading one means fetching the
// message from storage and decoding it, and a slow read or a failure inside
// the SMTP transaction would bounce mail that this server had already
// accepted. A worker claims the rows afterwards, with FOR UPDATE SKIP LOCKED
// so that several instances can run without reading the same message twice.

type calendarInvitationModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	UserID     string    `gorm:"column:user_id"`
	MailboxID  string    `gorm:"column:mailbox_id"`
	ItemID     string    `gorm:"column:item_id"`
	MailID     string    `gorm:"column:mail_id"`
	Recipient  string    `gorm:"column:recipient"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`

	Status    string     `gorm:"column:status"`
	Attempts  int        `gorm:"column:attempts"`
	NotBefore *time.Time `gorm:"column:not_before"`
	ClaimedAt *time.Time `gorm:"column:claimed_at"`
	ClaimedBy string     `gorm:"column:claimed_by"`
	Error     string     `gorm:"column:error"`

	Method     string  `gorm:"column:method"`
	UID        string  `gorm:"column:uid"`
	Sequence   int     `gorm:"column:sequence"`
	Organizer  string  `gorm:"column:organizer"`
	CalendarID *string `gorm:"column:calendar_id"`
	ObjectID   *string `gorm:"column:object_id"`
}

func (calendarInvitationModel) TableName() string { return "calendar_invitation" }

func (self *calendarInvitationModel) toModel() *models.CalendarInvitation {
	invitation := &models.CalendarInvitation{
		ID: self.ID, UserID: self.UserID, MailboxID: self.MailboxID,
		ItemID: self.ItemID, MailID: self.MailID, Recipient: self.Recipient,
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Status: models.CalendarInvitationStatus(self.Status), Attempts: self.Attempts,
		NotBefore: self.NotBefore, ClaimedAt: self.ClaimedAt, ClaimedBy: self.ClaimedBy,
		Error: self.Error, Method: self.Method, UID: self.UID,
		Sequence: self.Sequence, Organizer: self.Organizer,
	}
	if self.CalendarID != nil {
		invitation.CalendarID = *self.CalendarID
	}
	if self.ObjectID != nil {
		invitation.ObjectID = *self.ObjectID
	}
	return invitation
}

// NoteCalendarInvitation records that a message may carry a calendar part, so
// that a worker will look at it.
//
// Called from inside the delivery transaction, so it must be cheap and must
// not fail for anything but a real database error. A message that turns out
// to carry nothing is marked ignored later; deciding that here would mean
// reading the message, which is the whole thing this avoids.
//
// Nothing happens if the item already has a row. A message redelivered is the
// same invitation, and reading it twice would answer it twice.
func (self *transaction) NoteCalendarInvitation(invitation *models.CalendarInvitation) (*models.CalendarInvitation, error) {
	if invitation == nil || strings.TrimSpace(invitation.MailboxID) == "" ||
		strings.TrimSpace(invitation.ItemID) == "" {
		return nil, fmt.Errorf("db: an invitation needs a message it arrived in")
	}
	now := time.Now()
	row := &calendarInvitationModel{
		ID: newID(), UserID: invitation.UserID, MailboxID: invitation.MailboxID,
		ItemID: invitation.ItemID, MailID: invitation.MailID,
		Recipient: truncateRunes(strings.ToLower(strings.TrimSpace(invitation.Recipient)), 320),
		CreatedAt: now, ModifiedAt: now,
		Status: string(models.CalendarInvitationWaiting),
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mailbox_id"}, {Name: "item_id"}},
		DoNothing: true,
	}).Create(row).Error; err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

// ClaimCalendarInvitations takes the ones waiting to be read.
func (self *transaction) ClaimCalendarInvitations(instance string, limit int, now time.Time) ([]*models.CalendarInvitation, error) {
	if limit <= 0 {
		return nil, nil
	}
	var due []calendarInvitationModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("\"status\" = ? AND (\"not_before\" IS NULL OR \"not_before\" <= ?)",
			string(models.CalendarInvitationWaiting), now).
		Order("\"created_at\" ASC").Limit(limit).Find(&due).Error; err != nil {
		return nil, err
	}
	claimed := make([]*models.CalendarInvitation, 0, len(due))
	for index := range due {
		if err := self.tx.Model(&calendarInvitationModel{}).Where("\"id\" = ?", due[index].ID).
			Updates(map[string]any{
				"claimed_at": now, "claimed_by": instance,
				"attempts": gorm.Expr("\"attempts\" + 1"), "modified_at": now,
				// Put out of reach of another instance until this one has
				// had its turn. A worker that dies mid-read leaves the row
				// waiting, and it is picked up again after this.
				"not_before": now.Add(retryInvitationAfter),
			}).Error; err != nil {
			return nil, err
		}
		due[index].ClaimedAt = &now
		due[index].ClaimedBy = instance
		due[index].Attempts++
		claimed = append(claimed, due[index].toModel())
	}
	return claimed, nil
}

// retryInvitationAfter is how long a claimed invitation is left alone before
// another worker may take it. Long enough that a slow read is not raced,
// short enough that a worker killed mid-read does not leave somebody without
// their invitation for the rest of the day.
const retryInvitationAfter = 5 * time.Minute

// MaximumInvitationAttempts is how many times reading one is tried before it
// is left alone. A message that cannot be read will not become readable, and
// retrying for ever fills the log with the same failure.
const MaximumInvitationAttempts = 5

// FinishCalendarInvitation records what a message turned out to be.
func (self *transaction) FinishCalendarInvitation(invitation *models.CalendarInvitation) error {
	if invitation == nil || strings.TrimSpace(invitation.ID) == "" {
		return fmt.Errorf("db: which invitation")
	}
	// Cut to the columns rather than handed over whole. A message may carry
	// an identifier or an organizer longer than anything real, and a write
	// that fails on the length leaves the row waiting -- to be claimed again
	// every few minutes, for ever, re-reading the same message. Twenty of
	// those starve every real invitation on the server.
	updates := map[string]any{
		"status": string(invitation.Status), "error": truncateRunes(invitation.Error, 2000),
		"method": truncateRunes(invitation.Method, 32), "uid": truncateRunes(invitation.UID, 255),
		"sequence": invitation.Sequence, "organizer": truncateRunes(invitation.Organizer, 320),
		"modified_at": time.Now(), "not_before": invitation.NotBefore,
	}
	if invitation.CalendarID != "" {
		updates["calendar_id"] = invitation.CalendarID
	}
	if invitation.ObjectID != "" {
		updates["object_id"] = invitation.ObjectID
	}
	// Only the worker that holds it finishes it: a read that outlived its
	// claim and was handed to another worker must not overwrite what that
	// worker is doing.
	query := self.tx.Model(&calendarInvitationModel{}).Where("\"id\" = ?", invitation.ID)
	if invitation.ClaimedBy != "" {
		query = query.Where("\"claimed_by\" = ?", invitation.ClaimedBy)
	}
	return query.Updates(updates).Error
}

// GetCalendarInvitationForItem is what the reader asks: is this message an
// invitation, and if so what came of it?
func (self *transaction) GetCalendarInvitationForItem(mailboxId, itemId string) (*models.CalendarInvitation, error) {
	var found []calendarInvitationModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"item_id\" = ?", mailboxId, itemId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// GetCalendarInvitation is one by its own identifier.
func (self *transaction) GetCalendarInvitation(invitationId string) (*models.CalendarInvitation, error) {
	var found []calendarInvitationModel
	if err := self.tx.Where("\"id\" = ?", invitationId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// SweepCalendarInvitations removes the rows that turned out to be nothing.
//
// A row is written for every delivered message, because deciding whether one
// carries an invitation means reading it and that cannot happen during
// delivery. Most turn out to carry nothing, and keeping a row per message
// ever delivered would be a second copy of the mail table that nobody reads.
//
// Only the ones that came to nothing, and only once they are old enough that
// nothing is still looking at them. What was actually an invitation is kept:
// it is what the reader looks up to show the card, and what an answer is
// recorded against.
func (self *transaction) SweepCalendarInvitations(before time.Time) (int64, error) {
	result := self.tx.Where(
		"\"status\" = ? AND \"modified_at\" < ? AND \"uid\" = ''",
		string(models.CalendarInvitationIgnored), before).
		Delete(&calendarInvitationModel{})
	return result.RowsAffected, result.Error
}
