package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// LocationNotificationMuteLongest is how far ahead a timed mute may end. A
// mute that should last longer is one that lasts until the person unmutes.
const LocationNotificationMuteLongest = 30 * 24 * time.Hour

// LocationNotificationMuteOperation is the people whose arrivals and
// departures a person muted. docs/subsystems/location.md says what a mute
// does; what is here is only the rows, one for each pair of people.
type LocationNotificationMuteOperation interface {
	// MuteLocationNotifications mutes, for userId, the notices of where
	// mutedUserId arrives and leaves, until mutedUntil, or until unmuted when
	// it is nil. Muting again changes the end. ErrInvalidArguments for an end
	// that is not after now or is more than LocationNotificationMuteLongest
	// after it, or for muting oneself.
	MuteLocationNotifications(userId, mutedUserId string, mutedUntil *time.Time, now time.Time) (*models.LocationNotificationMute, error)

	// UnmuteLocationNotifications ends the mute, and says whether one was in
	// effect at now.
	UnmuteLocationNotifications(userId, mutedUserId string, now time.Time) (isUnmuted bool, err error)

	// GetLocationNotificationMutes is the person's mutes of the people named
	// that are in effect at the instant, by the muted person's id. A person
	// not muted is not in the answer.
	GetLocationNotificationMutes(userId string, mutedUserIds []string, at time.Time) (map[string]*models.LocationNotificationMute, error)

	// ListLocationNotificationMuters is the ids of the people who have muted
	// mutedUserId's notices at the instant.
	ListLocationNotificationMuters(mutedUserId string, at time.Time) ([]string, error)
}

type locationNotificationMuteModel struct {
	UserID      string     `gorm:"column:user_id;primaryKey"`
	MutedUserID string     `gorm:"column:muted_user_id;primaryKey"`
	MutedUntil  *time.Time `gorm:"column:muted_until"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

func (locationNotificationMuteModel) TableName() string { return "location_notification_mute" }

func (self *locationNotificationMuteModel) toModel() *models.LocationNotificationMute {
	return &models.LocationNotificationMute{
		UserID:      self.UserID,
		MutedUserID: self.MutedUserID,
		MutedUntil:  localTime(self.MutedUntil),
		CreatedAt:   self.CreatedAt.In(time.Local),
		UpdatedAt:   self.UpdatedAt.In(time.Local),
	}
}

func (self *transaction) MuteLocationNotifications(userId, mutedUserId string, mutedUntil *time.Time, now time.Time) (*models.LocationNotificationMute, error) {
	if userId == "" || mutedUserId == "" || now.IsZero() {
		return nil, ErrInvalidArguments
	}
	if userId == mutedUserId {
		return nil, fmt.Errorf("%w: nobody is told of their own arrivals, so there is nothing to mute", ErrInvalidArguments)
	}
	// PostgreSQL keeps microseconds; the mute answered is the one stored.
	now = now.Truncate(time.Microsecond)
	if mutedUntil != nil {
		truncated := mutedUntil.Truncate(time.Microsecond)
		mutedUntil = &truncated
	}
	if mutedUntil != nil && !mutedUntil.After(now) {
		return nil, fmt.Errorf("%w: a mute must end after now", ErrInvalidArguments)
	}
	if mutedUntil != nil && mutedUntil.After(now.Add(LocationNotificationMuteLongest)) {
		return nil, fmt.Errorf("%w: a mute may end at most 30 days from now; leave the end out to mute until you unmute", ErrInvalidArguments)
	}
	mute := locationNotificationMuteModel{UserID: userId, MutedUserID: mutedUserId, MutedUntil: mutedUntil, CreatedAt: now, UpdatedAt: now}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "muted_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"muted_until", "updated_at"}),
	}).Create(&mute).Error; err != nil {
		return nil, err
	}
	return mute.toModel(), nil
}

func (self *transaction) UnmuteLocationNotifications(userId, mutedUserId string, now time.Time) (bool, error) {
	if userId == "" || mutedUserId == "" || now.IsZero() {
		return false, ErrInvalidArguments
	}
	var deleted []locationNotificationMuteModel
	if err := self.tx.Clauses(clause.Returning{}).
		Where("\"user_id\" = ? AND \"muted_user_id\" = ?", userId, mutedUserId).
		Delete(&deleted).Error; err != nil {
		return false, err
	}
	for index := range deleted {
		if deleted[index].toModel().IsInEffectAt(now) {
			return true, nil
		}
	}
	return false, nil
}

func (self *transaction) GetLocationNotificationMutes(userId string, mutedUserIds []string, at time.Time) (map[string]*models.LocationNotificationMute, error) {
	inEffect := map[string]*models.LocationNotificationMute{}
	mutedUserIds = uniqueStrings(mutedUserIds)
	if userId == "" || len(mutedUserIds) == 0 {
		return inEffect, nil
	}
	var rows []locationNotificationMuteModel
	if err := self.tx.Where("\"user_id\" = ? AND \"muted_user_id\" IN ? AND (\"muted_until\" IS NULL OR \"muted_until\" > ?)", userId, mutedUserIds, at).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		inEffect[rows[index].MutedUserID] = rows[index].toModel()
	}
	return inEffect, nil
}

func (self *transaction) ListLocationNotificationMuters(mutedUserId string, at time.Time) ([]string, error) {
	if mutedUserId == "" {
		return nil, ErrInvalidArguments
	}
	var userIds []string
	if err := self.tx.Model(&locationNotificationMuteModel{}).
		Where("\"muted_user_id\" = ? AND (\"muted_until\" IS NULL OR \"muted_until\" > ?)", mutedUserId, at).
		Order("\"user_id\"").Pluck("user_id", &userIds).Error; err != nil {
		return nil, err
	}
	return userIds, nil
}
