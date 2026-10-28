package db

import (
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// LocationVisitNoticeOperation is the arrivals and departures that wait before they are told (location_visit_notice).
type LocationVisitNoticeOperation interface {
	// CreateLocationVisitNotice stores a notice to be told at its due time.
	CreateLocationVisitNotice(notice *LocationVisitNotice, now time.Time) error

	// ClaimDueLocationVisitNotices takes the notices that are due, at most
	// limit, so that no other claimer gets them: a notice another transaction
	// is claiming is skipped, and one claimed less than reclaimAfter ago is
	// left to whoever claimed it. A notice claimed longer ago than that
	// returns to the queue, for the claimer that died before telling it.
	ClaimDueLocationVisitNotices(limit int, reclaimAfter time.Duration, now time.Time) ([]*LocationVisitNotice, error)

	// DeleteLocationVisitNotice forgets a notice that was told.
	DeleteLocationVisitNotice(noticeId string) error
}

// LocationVisitNotice is an arrival or a departure that waits to be told.
type LocationVisitNotice struct {
	ID             string     `gorm:"column:id;primaryKey"`
	UserID         string     `gorm:"column:user_id"`
	ClientVisitKey string     `gorm:"column:client_visit_key"`
	IsArrival      bool       `gorm:"column:is_arrival"`
	HappenedAt     time.Time  `gorm:"column:happened_at"`
	DueAt          time.Time  `gorm:"column:due_at"`
	ClaimedAt      *time.Time `gorm:"column:claimed_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
}

func (LocationVisitNotice) TableName() string { return "location_visit_notice" }

func (self *transaction) CreateLocationVisitNotice(notice *LocationVisitNotice, now time.Time) error {
	notice.ID = security.NewULID()
	notice.ClaimedAt = nil
	notice.CreatedAt = now
	return self.tx.Create(notice).Error
}

func (self *transaction) ClaimDueLocationVisitNotices(limit int, reclaimAfter time.Duration, now time.Time) ([]*LocationVisitNotice, error) {
	var claimed []*LocationVisitNotice
	if err := self.tx.Raw(`WITH candidates AS (
		SELECT "id" FROM "location_visit_notice"
		WHERE "due_at" <= ? AND ("claimed_at" IS NULL OR "claimed_at" < ?)
		ORDER BY "due_at" ASC, "id" ASC LIMIT ? FOR UPDATE SKIP LOCKED
	) UPDATE "location_visit_notice" SET "claimed_at" = ? FROM candidates
	WHERE "location_visit_notice"."id" = candidates."id" RETURNING "location_visit_notice".*`,
		now, now.Add(-reclaimAfter), limit, now).Scan(&claimed).Error; err != nil {
		return nil, err
	}
	return claimed, nil
}

func (self *transaction) DeleteLocationVisitNotice(noticeId string) error {
	return self.tx.Where(`"id" = ?`, noticeId).Delete(&LocationVisitNotice{}).Error
}
