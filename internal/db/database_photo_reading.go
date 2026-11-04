package db

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// PhotoReadingOperation is reading new photos as they arrive: the person's
// switch, and the photos waiting to be queued.
type PhotoReadingOperation interface {
	// IsPhotoReadingEnabled is the person's own switch for having their new
	// photos read; on until they turn it off.
	IsPhotoReadingEnabled(userId string) (bool, error)

	// SetPhotoReading switches it on or off. Off leaves what was read.
	SetPhotoReading(userId string, isEnabled bool) error

	// ListPhotosToRead lists up to limit pictures and videos of the person's
	// library, newest first, that are ready, were taken (or added, when they
	// carry no date) at or after since, and have been neither described,
	// asked to be, nor queued to be read. Photos saved from somebody else's
	// share are not read.
	ListPhotosToRead(userId string, since time.Time, limit int) ([]*models.Photo, error)

	// MarkPhotoReadingQueued records that a photo was queued to be read.
	MarkPhotoReadingQueued(userId, photoId string) error
}

func (self *transaction) IsPhotoReadingEnabled(userId string) (bool, error) {
	if userId == "" {
		return false, ErrNotFound
	}
	var rows []bool
	if err := self.tx.Raw(`SELECT is_photo_reading_enabled FROM "user" WHERE id = ?`, userId).Scan(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, ErrNotFound
	}
	return rows[0], nil
}

func (self *transaction) SetPhotoReading(userId string, isEnabled bool) error {
	if userId == "" {
		return ErrNotFound
	}
	changed := self.tx.Exec(`UPDATE "user" SET is_photo_reading_enabled = ? WHERE id = ?`, isEnabled, userId)
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) ListPhotosToRead(userId string, since time.Time, limit int) ([]*models.Photo, error) {
	if userId == "" || limit <= 0 {
		return nil, nil
	}
	var rows []*photoModel
	if err := self.tx.Where("photo.user_id = ? AND photo.trashed_at IS NULL AND photo.reading_queued_at IS NULL AND photo.described_at IS NULL"+
		" AND photo.description_requested_at IS NULL AND photo.photo_processing_state = ? AND photo.photo_kind IN ? AND photo.photo_source <> ?"+
		" AND "+photoTimelineExpression+" >= ?",
		userId, models.PhotoProcessingReady, []models.PhotoKind{models.PhotoKindPicture, models.PhotoKindLivePhoto, models.PhotoKindVideo}, models.PhotoSourceShared, since).
		Order(photoTimelineExpression + " DESC, photo.id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	photos := make([]*models.Photo, 0, len(rows))
	for _, row := range rows {
		photos = append(photos, photoFromModel(row))
	}
	return photos, nil
}

func (self *transaction) MarkPhotoReadingQueued(userId, photoId string) error {
	if userId == "" || photoId == "" {
		return ErrNotFound
	}
	// updated_at is left alone: it is part of the address of every preview.
	marked := self.tx.Model(&photoModel{}).Where("photo.id = ? AND photo.user_id = ?", photoId, userId).Update("reading_queued_at", photoNow())
	if marked.Error != nil {
		return marked.Error
	}
	if marked.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
