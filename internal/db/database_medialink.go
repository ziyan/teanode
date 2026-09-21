package db

import (
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// MediaLinkOperation is what the send path and the picture endpoint need of
// the per-message addresses.
//
// The write on a fetch is deliberately not an update of arbitrary columns:
// this is reached by anybody on the internet, and the only thing such a
// request may change is that it happened.
type MediaLinkOperation interface {
	// CreateMediaLink records one address, made while a message is being
	// sent.
	CreateMediaLink(link *models.MediaLink) (*models.MediaLink, error)

	// GetMediaLink resolves an address, or nil when there is no such token.
	GetMediaLink(token string) (*models.MediaLink, error)

	// RecordMediaLinkOpen notes that an address was fetched: the first time
	// if it is the first, the last time always, and one more on the count.
	RecordMediaLinkOpen(token string, at time.Time, ip, userAgent string) error

	// ListMediaLinksForEnvelope returns the addresses put in one message,
	// which is how the dashboard answers whether it was opened.
	ListMediaLinksForEnvelope(envelopeId string) ([]*models.MediaLink, error)

	// ListMediaLinksForEnvelopes is the same question about a page of
	// messages, in one query. The mail list asks it about every row it is
	// showing, and one query per row would be five hundred.
	ListMediaLinksForEnvelopes(envelopeIds []string) ([]*models.MediaLink, error)
}

type mediaLinkModel struct {
	Token string `gorm:"column:token;primaryKey;size:32"`

	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`

	MediaID    string `gorm:"column:media_id;size:32"`
	EnvelopeID string `gorm:"column:envelope_id;size:32;index"`

	OpenedAt     *time.Time `gorm:"column:opened_at"`
	LastOpenedAt *time.Time `gorm:"column:last_opened_at"`
	OpenCount    int64      `gorm:"column:open_count"`
	IP           string     `gorm:"column:ip;size:64"`
	UserAgent    string     `gorm:"column:user_agent;size:512"`
}

func (mediaLinkModel) TableName() string {
	return "media_link"
}

func mediaLinkFromModel(model *mediaLinkModel) *models.MediaLink {
	return &models.MediaLink{
		Token:        model.Token,
		CreatedAt:    model.CreatedAt,
		ModifiedAt:   model.ModifiedAt,
		MediaID:      model.MediaID,
		EnvelopeID:   model.EnvelopeID,
		OpenedAt:     model.OpenedAt,
		LastOpenedAt: model.LastOpenedAt,
		OpenCount:    model.OpenCount,
		IP:           model.IP,
		UserAgent:    model.UserAgent,
	}
}

func (self *database) CreateMediaLink(link *models.MediaLink) (*models.MediaLink, error) {
	now := time.Now().UTC()
	model := &mediaLinkModel{
		Token:      link.Token,
		CreatedAt:  now,
		ModifiedAt: now,
		MediaID:    link.MediaID,
		EnvelopeID: link.EnvelopeID,
	}
	if err := self.db.Create(model).Error; err != nil {
		return nil, err
	}
	return mediaLinkFromModel(model), nil
}

func (self *database) GetMediaLink(token string) (*models.MediaLink, error) {
	if token == "" {
		return nil, nil
	}
	var model mediaLinkModel
	if err := self.db.First(&model, "\"token\" = ?", token).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return mediaLinkFromModel(&model), nil
}

func (self *database) RecordMediaLinkOpen(token string, at time.Time, ip, userAgent string) error {
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	// One statement, and the count incremented in the database rather than
	// read and written back: several fetches of the same picture arrive at
	// once when a message is opened on a phone and a laptop, and a read
	// followed by a write would lose one.
	//
	// COALESCE on opened_at is what makes the first fetch the first: after it
	// is set, it stays.
	return self.db.Model(&mediaLinkModel{}).
		Where("\"token\" = ?", token).
		Updates(map[string]any{
			"opened_at":      gorm.Expr("COALESCE(\"opened_at\", ?)", at.UTC()),
			"last_opened_at": at.UTC(),
			"open_count":     gorm.Expr("\"open_count\" + 1"),
			"ip":             ip,
			"user_agent":     userAgent,
			"modified_at":    at.UTC(),
		}).Error
}

func (self *database) ListMediaLinksForEnvelope(envelopeId string) ([]*models.MediaLink, error) {
	var found []mediaLinkModel
	if err := self.db.Where("\"envelope_id\" = ?", envelopeId).
		Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	links := make([]*models.MediaLink, 0, len(found))
	for index := range found {
		links = append(links, mediaLinkFromModel(&found[index]))
	}
	return links, nil
}

func (self *database) ListMediaLinksForEnvelopes(envelopeIds []string) ([]*models.MediaLink, error) {
	if len(envelopeIds) == 0 {
		return nil, nil
	}
	var found []mediaLinkModel
	if err := self.db.Where("\"envelope_id\" IN ?", envelopeIds).
		Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	links := make([]*models.MediaLink, 0, len(found))
	for index := range found {
		links = append(links, mediaLinkFromModel(&found[index]))
	}
	return links, nil
}
