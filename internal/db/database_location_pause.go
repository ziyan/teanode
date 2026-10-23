package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// LocationPauseLongest is how far ahead a timed pause may end. A pause that
// should last longer is one that lasts until the person resumes.
const LocationPauseLongest = 30 * 24 * time.Hour

// LocationPauseOperation is the times a person stopped their location being
// recorded. docs/subsystems/location.md says what a pause does; what is here
// is only the rows. A caller that changes pauses holds the person's location
// lock (LockLocation), which is what keeps one pause in effect at a time.
type LocationPauseOperation interface {
	// PauseLocation pauses the person's location from now, ending at endedAt,
	// or never when endedAt is nil. When a pause is in effect at now, it is
	// that pause whose end changes, and nothing is added: isStarted says
	// whether the pause is a new one. ErrInvalidArguments for an end that is
	// not after now or is more than LocationPauseLongest after it.
	PauseLocation(userId string, endedAt *time.Time, now time.Time) (pause *models.LocationPause, isStarted bool, err error)

	// ResumeLocation ends the pause in effect at now, at now, and says whether
	// there was one. A pause that began at now or later is deleted, since it
	// would end before it began.
	ResumeLocation(userId string, now time.Time) (isResumed bool, err error)

	// GetLocationPauseInEffect is the pause in effect at the instant, or nil.
	GetLocationPauseInEffect(userId string, at time.Time) (*models.LocationPause, error)

	// GetLocationPausesInEffect is GetLocationPauseInEffect for many people in
	// one query. A person who is not paused is not in the answer.
	GetLocationPausesInEffect(userIds []string, at time.Time) (map[string]*models.LocationPause, error)

	// ListLocationPauses reads the pauses that overlap the range, newest
	// first: those that began before until and were not over by from. At most
	// limitCount of them, all of them when it is zero or less; a caller that
	// must refuse a range holding more asks for one more than it will take.
	ListLocationPauses(userId string, from, until time.Time, limitCount int) ([]*models.LocationPause, error)
}

type locationPauseModel struct {
	ID        string     `gorm:"column:id;primaryKey"`
	UserID    string     `gorm:"column:user_id"`
	StartedAt time.Time  `gorm:"column:started_at"`
	EndedAt   *time.Time `gorm:"column:ended_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

func (locationPauseModel) TableName() string { return "location_pause" }

func (self *locationPauseModel) toModel() *models.LocationPause {
	return &models.LocationPause{
		ID:        self.ID,
		UserID:    self.UserID,
		StartedAt: self.StartedAt.In(time.Local),
		EndedAt:   localTime(self.EndedAt),
		CreatedAt: self.CreatedAt.In(time.Local),
		UpdatedAt: self.UpdatedAt.In(time.Local),
	}
}

// findLocationPauseInEffect is the row of the pause in effect at the instant,
// locked when isLocked so that two changes of it take turns.
func (self *transaction) findLocationPauseInEffect(userId string, at time.Time, isLocked bool) (*locationPauseModel, error) {
	locking := ""
	if isLocked {
		locking = " FOR UPDATE"
	}
	var found []locationPauseModel
	if err := self.tx.Raw(`SELECT * FROM "location_pause"
		WHERE user_id = @user AND started_at <= @at AND (ended_at IS NULL OR ended_at > @at)
		ORDER BY started_at DESC, id DESC LIMIT 1`+locking,
		sql.Named("user", userId), sql.Named("at", at)).Scan(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

func (self *transaction) PauseLocation(userId string, endedAt *time.Time, now time.Time) (*models.LocationPause, bool, error) {
	if userId == "" || now.IsZero() {
		return nil, false, ErrInvalidArguments
	}
	// PostgreSQL keeps microseconds. The pause answered is the one stored, so
	// that the start a visit is ended at is the start anybody reads back.
	now = now.Truncate(time.Microsecond)
	if endedAt != nil {
		truncated := endedAt.Truncate(time.Microsecond)
		endedAt = &truncated
	}
	if endedAt != nil && !endedAt.After(now) {
		return nil, false, fmt.Errorf("%w: a pause must end after now", ErrInvalidArguments)
	}
	if endedAt != nil && endedAt.After(now.Add(LocationPauseLongest)) {
		return nil, false, fmt.Errorf("%w: a pause may end at most 30 days from now; leave the end out to pause until you resume", ErrInvalidArguments)
	}
	inEffect, err := self.findLocationPauseInEffect(userId, now, true)
	if err != nil {
		return nil, false, err
	}
	if inEffect != nil {
		inEffect.EndedAt, inEffect.UpdatedAt = endedAt, now
		if err := self.tx.Model(&locationPauseModel{}).Where("\"id\" = ?", inEffect.ID).
			Updates(map[string]any{"ended_at": endedAt, "updated_at": now}).Error; err != nil {
			return nil, false, err
		}
		return inEffect.toModel(), false, nil
	}
	started := locationPauseModel{ID: newID(), UserID: userId, StartedAt: now, EndedAt: endedAt, CreatedAt: now, UpdatedAt: now}
	if err := self.tx.Create(&started).Error; err != nil {
		return nil, false, err
	}
	return started.toModel(), true, nil
}

func (self *transaction) ResumeLocation(userId string, now time.Time) (bool, error) {
	if userId == "" || now.IsZero() {
		return false, ErrInvalidArguments
	}
	now = now.Truncate(time.Microsecond)
	inEffect, err := self.findLocationPauseInEffect(userId, now, true)
	if err != nil || inEffect == nil {
		return false, err
	}
	// A pause cannot end at the moment it began, and one that lasted no time
	// left nothing to show.
	if !now.After(inEffect.StartedAt) {
		return true, self.tx.Where("\"id\" = ?", inEffect.ID).Delete(&locationPauseModel{}).Error
	}
	return true, self.tx.Model(&locationPauseModel{}).Where("\"id\" = ?", inEffect.ID).
		Updates(map[string]any{"ended_at": now, "updated_at": now}).Error
}

func (self *transaction) GetLocationPauseInEffect(userId string, at time.Time) (*models.LocationPause, error) {
	found, err := self.findLocationPauseInEffect(userId, at, false)
	if err != nil || found == nil {
		return nil, err
	}
	return found.toModel(), nil
}

func (self *transaction) GetLocationPausesInEffect(userIds []string, at time.Time) (map[string]*models.LocationPause, error) {
	inEffect := map[string]*models.LocationPause{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return inEffect, nil
	}
	var rows []locationPauseModel
	if err := self.tx.Raw(`SELECT DISTINCT ON (pause.user_id) pause.* FROM "location_pause" pause
		WHERE pause.user_id IN ? AND pause.started_at <= ? AND (pause.ended_at IS NULL OR pause.ended_at > ?)
		ORDER BY pause.user_id, pause.started_at DESC, pause.id DESC`, userIds, at, at).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		inEffect[rows[index].UserID] = rows[index].toModel()
	}
	return inEffect, nil
}

func (self *transaction) ListLocationPauses(userId string, from, until time.Time, limitCount int) ([]*models.LocationPause, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Where("\"user_id\" = ? AND \"started_at\" < ? AND (\"ended_at\" IS NULL OR \"ended_at\" > ?)", userId, until, from).
		Order("\"started_at\" DESC, \"id\" DESC")
	if limitCount > 0 {
		query = query.Limit(limitCount)
	}
	var rows []locationPauseModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	pauses := make([]*models.LocationPause, 0, len(rows))
	for index := range rows {
		pauses = append(pauses, rows[index].toModel())
	}
	return pauses, nil
}

// deleteEndedLocationPauses deletes the person's pauses that began in the
// range (all of them when both ends are nil) and were over by now. The pause
// in effect stays, so erasing history does not resume recording.
func (self *transaction) deleteEndedLocationPauses(userId string, from, until *time.Time, now time.Time) error {
	query := self.tx.Where("\"user_id\" = ? AND \"ended_at\" IS NOT NULL AND \"ended_at\" <= ?", userId, now)
	if from != nil {
		query = query.Where("\"started_at\" >= ?", *from)
	}
	if until != nil {
		query = query.Where("\"started_at\" < ?", *until)
	}
	return query.Delete(&locationPauseModel{}).Error
}
