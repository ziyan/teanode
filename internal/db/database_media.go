package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// MediaOperation is what the dashboard and the send path need of the media
// table.
//
// Narrow, like the passkey and token operations: a row here decides what is
// served from the operator's own domain over HTTPS, so there is no general
// update that would let a caller point an existing picture at other bytes.
// Replacing a picture means uploading another and changing the template,
// which is a change somebody can see.
type MediaOperation interface {
	// CreateMedia stores the metadata of an uploaded file. The bytes are
	// written to storage separately, under the same identifier.
	CreateMedia(media *models.Media) (*models.Media, error)

	// GetMedia returns one by identifier, or nil when there is none. Nil
	// rather than an error because the caller is usually answering a request
	// for a name that may simply not exist, and a 404 is not a failure.
	GetMedia(mediaId string) (*models.Media, error)

	// ListMediaForDomain returns a domain's files, newest first.
	ListMediaForDomain(domainId string) ([]*models.Media, error)

	// DeleteMedia removes the metadata. The bytes are removed separately, and
	// in that order: a row with no bytes answers 404, while bytes with no row
	// are unreachable and swept later.
	DeleteMedia(mediaId string) error
}

type mediaModel struct {
	ID string `gorm:"column:id;primaryKey;size:32"`

	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`

	DomainID    string `gorm:"column:domain_id;size:255;index:media_domain_id_created_at"`
	Filename    string `gorm:"column:filename;size:255"`
	ContentType string `gorm:"column:content_type;size:128"`
	Size        int64  `gorm:"column:size"`
	Checksum    string `gorm:"column:checksum;size:64"`
}

func (mediaModel) TableName() string {
	return "media"
}

func mediaFromModel(model *mediaModel) *models.Media {
	return &models.Media{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt,
		ModifiedAt:  model.ModifiedAt,
		DomainID:    model.DomainID,
		Filename:    model.Filename,
		ContentType: model.ContentType,
		Size:        model.Size,
		Checksum:    model.Checksum,
	}
}

func (self *database) CreateMedia(media *models.Media) (*models.Media, error) {
	now := time.Now().UTC()
	model := &mediaModel{
		ID:          media.ID,
		CreatedAt:   now,
		ModifiedAt:  now,
		DomainID:    media.DomainID,
		Filename:    media.Filename,
		ContentType: media.ContentType,
		Size:        media.Size,
		Checksum:    media.Checksum,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return mediaFromModel(model), nil
}

func (self *database) GetMedia(mediaId string) (*models.Media, error) {
	if mediaId == "" {
		return nil, nil
	}
	var model mediaModel
	if err := self.db.First(&model, "\"id\" = ?", mediaId).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return mediaFromModel(&model), nil
}

func (self *database) ListMediaForDomain(domainId string) ([]*models.Media, error) {
	var found []mediaModel
	if err := self.db.Where("\"domain_id\" = ?", domainId).
		Order("\"created_at\" DESC").Find(&found).Error; err != nil {
		return nil, err
	}
	media := make([]*models.Media, 0, len(found))
	for index := range found {
		media = append(media, mediaFromModel(&found[index]))
	}
	return media, nil
}

func (self *database) DeleteMedia(mediaId string) error {
	return self.db.Where("\"id\" = ?", mediaId).Delete(&mediaModel{}).Error
}
