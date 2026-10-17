package db

import (
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// photoUploadLimit is how many uploads one account may have open at once. A
// phone backing up a library queues a few at a time; this is for the one
// that queues without end.
const photoUploadLimit = 1000

// PhotoUploadOperation owns resumable uploads and the receipts of their parts.
type PhotoUploadOperation interface {
	CreatePhotoUpload(upload *models.PhotoUpload) (*models.PhotoUpload, error)
	GetPhotoUpload(userId, photoUploadId string) (*models.PhotoUpload, error)
	ListPhotoUploadParts(userId, photoUploadId string) ([]*models.PhotoUploadPart, error)

	// AppendPhotoUploadPart records a whole stored part. The offset is where
	// the caller believes the upload stands, and a different one is refused
	// with ErrAlreadyExists: somebody else appended in between.
	AppendPhotoUploadPart(userId string, part *models.PhotoUploadPart, expectedOffsetBytes int64) (*models.PhotoUpload, error)

	CompletePhotoUpload(userId, photoUploadId, photoId string) (*models.PhotoUpload, error)

	// FailPhotoUpload ends an upload that is still receiving. Its parts are
	// deleted by cleanup.
	FailPhotoUpload(userId, photoUploadId string) (*models.PhotoUpload, error)

	// LockPhotoUploadForCleanup finds one upload that has failed, or has not
	// moved since the time, and locks it. ErrNotFound says there is none.
	LockPhotoUploadForCleanup(idleBefore time.Time) (*models.PhotoUpload, error)
	ListPhotoUploadPartsForCleanup(photoUploadId string) ([]*models.PhotoUploadPart, error)
	DeletePhotoUpload(photoUploadId string) error
}

type photoUploadModel models.PhotoUpload

func (photoUploadModel) TableName() string { return "photo_upload" }

type photoUploadPartModel models.PhotoUploadPart

func (photoUploadPartModel) TableName() string { return "photo_upload_part" }

func photoUploadFromModel(row *photoUploadModel) *models.PhotoUpload {
	upload := models.PhotoUpload(*row)
	upload.CreatedAt = upload.CreatedAt.UTC()
	upload.UpdatedAt = upload.UpdatedAt.UTC()
	return &upload
}

func photoUploadPartFromModel(row *photoUploadPartModel) *models.PhotoUploadPart {
	part := models.PhotoUploadPart(*row)
	part.StoredAt = part.StoredAt.UTC()
	return &part
}

func (self *transaction) CreatePhotoUpload(upload *models.PhotoUpload) (*models.PhotoUpload, error) {
	if upload == nil || upload.UserID == "" || upload.PhotoUploadHeaders.ClientPhotoID == "" || !upload.PhotoUploadHeaders.PhotoFileRole.IsValid() ||
		(upload.DeclaredSizeBytes != nil && *upload.DeclaredSizeBytes < 0) {
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", upload.UserID).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var openCount int64
	if err := self.tx.Model(&photoUploadModel{}).Where("user_id = ? AND photo_upload_state = ?", upload.UserID, models.PhotoUploadReceiving).Count(&openCount).Error; err != nil {
		return nil, err
	}
	if openCount >= photoUploadLimit {
		return nil, ErrTooMuchAsked
	}
	created := photoNow()
	row := photoUploadModel{ID: newID(), UserID: upload.UserID, CreatedAt: created, UpdatedAt: created, PhotoUploadState: models.PhotoUploadReceiving, DeclaredSizeBytes: upload.DeclaredSizeBytes, PhotoUploadHeaders: upload.PhotoUploadHeaders}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return photoUploadFromModel(&row), nil
}

func (self *transaction) photoUploadOwned(userId, photoUploadId string, isLocked bool) (*photoUploadModel, error) {
	if userId == "" || photoUploadId == "" {
		return nil, ErrNotFound
	}
	query := self.tx.Where("id = ? AND user_id = ?", photoUploadId, userId)
	if isLocked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row photoUploadModel
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (self *transaction) GetPhotoUpload(userId, photoUploadId string) (*models.PhotoUpload, error) {
	row, err := self.photoUploadOwned(userId, photoUploadId, false)
	if err != nil {
		return nil, err
	}
	return photoUploadFromModel(row), nil
}

func (self *transaction) ListPhotoUploadParts(userId, photoUploadId string) ([]*models.PhotoUploadPart, error) {
	if _, err := self.photoUploadOwned(userId, photoUploadId, false); err != nil {
		return nil, err
	}
	return self.ListPhotoUploadPartsForCleanup(photoUploadId)
}

func (self *transaction) ListPhotoUploadPartsForCleanup(photoUploadId string) ([]*models.PhotoUploadPart, error) {
	var rows []photoUploadPartModel
	if err := self.tx.Where("photo_upload_id = ?", photoUploadId).Order("part_index").Find(&rows).Error; err != nil {
		return nil, err
	}
	parts := make([]*models.PhotoUploadPart, 0, len(rows))
	for index := range rows {
		parts = append(parts, photoUploadPartFromModel(&rows[index]))
	}
	return parts, nil
}

func (self *transaction) AppendPhotoUploadPart(userId string, part *models.PhotoUploadPart, expectedOffsetBytes int64) (*models.PhotoUpload, error) {
	if part == nil || part.PartSizeBytes <= 0 || part.PartSizeBytes > models.PhotoUploadPartBytes || part.PartIndex < 0 || !validPhotoSha256(part.PartSHA256) {
		return nil, ErrInvalidArguments
	}
	// Locked, so that two requests appending to one upload are told apart by
	// the offset each expected instead of both succeeding.
	upload, err := self.photoUploadOwned(userId, part.PhotoUploadID, true)
	if err != nil {
		return nil, err
	}
	if upload.PhotoUploadState != models.PhotoUploadReceiving || upload.ReceivedSizeBytes != expectedOffsetBytes {
		return nil, ErrAlreadyExists
	}
	var partCount int64
	if err := self.tx.Model(&photoUploadPartModel{}).Where("photo_upload_id = ?", part.PhotoUploadID).Count(&partCount).Error; err != nil {
		return nil, err
	}
	if int64(part.PartIndex) != partCount {
		return nil, ErrAlreadyExists
	}
	stored := photoNow()
	row := photoUploadPartModel{PhotoUploadID: part.PhotoUploadID, PartIndex: part.PartIndex, PartSizeBytes: part.PartSizeBytes, PartSHA256: part.PartSHA256, StoredAt: stored}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	upload.ReceivedSizeBytes += part.PartSizeBytes
	upload.UpdatedAt = stored
	if err := self.tx.Model(&photoUploadModel{}).Where("id = ?", upload.ID).Updates(map[string]any{"received_size_bytes": upload.ReceivedSizeBytes, "updated_at": stored}).Error; err != nil {
		return nil, err
	}
	return photoUploadFromModel(upload), nil
}

func (self *transaction) CompletePhotoUpload(userId, photoUploadId, photoId string) (*models.PhotoUpload, error) {
	upload, err := self.photoUploadOwned(userId, photoUploadId, true)
	if err != nil {
		return nil, err
	}
	if upload.PhotoUploadState == models.PhotoUploadComplete && upload.PhotoID != nil && *upload.PhotoID == photoId {
		return photoUploadFromModel(upload), nil
	}
	if upload.PhotoUploadState != models.PhotoUploadReceiving {
		return nil, ErrAlreadyExists
	}
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return nil, err
	}
	updatedAt := photoNow()
	if err := self.tx.Model(&photoUploadModel{}).Where("id = ?", photoUploadId).Updates(map[string]any{"photo_upload_state": models.PhotoUploadComplete, "photo_id": photoId, "updated_at": updatedAt}).Error; err != nil {
		return nil, err
	}
	upload.PhotoUploadState = models.PhotoUploadComplete
	upload.PhotoID = &photoId
	upload.UpdatedAt = updatedAt
	return photoUploadFromModel(upload), nil
}

func (self *transaction) FailPhotoUpload(userId, photoUploadId string) (*models.PhotoUpload, error) {
	upload, err := self.photoUploadOwned(userId, photoUploadId, true)
	if err != nil {
		return nil, err
	}
	if upload.PhotoUploadState != models.PhotoUploadReceiving {
		return photoUploadFromModel(upload), nil
	}
	updatedAt := photoNow()
	if err := self.tx.Model(&photoUploadModel{}).Where("id = ?", photoUploadId).Updates(map[string]any{"photo_upload_state": models.PhotoUploadFailed, "updated_at": updatedAt}).Error; err != nil {
		return nil, err
	}
	upload.PhotoUploadState = models.PhotoUploadFailed
	upload.UpdatedAt = updatedAt
	return photoUploadFromModel(upload), nil
}

func (self *transaction) LockPhotoUploadForCleanup(idleBefore time.Time) (*models.PhotoUpload, error) {
	var row photoUploadModel
	err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("photo_upload_state = ? OR updated_at < ?", models.PhotoUploadFailed, idleBefore).
		Order("updated_at").Limit(1).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return photoUploadFromModel(&row), nil
}

func (self *transaction) DeletePhotoUpload(photoUploadId string) error {
	return self.tx.Where("id = ?", photoUploadId).Delete(&photoUploadModel{}).Error
}
