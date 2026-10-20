package db

import (
	"time"

	"gorm.io/gorm/clause"
)

// LinkPreviewOperation is the cache of what web pages say about themselves.
// Fetching a page is the caller's (internal/linkpreview); a row is the answer,
// or the record that there was none.
type LinkPreviewOperation interface {
	// ListLinkPreviews is what is cached for these addresses, keyed by
	// address. An address that was never fetched is absent.
	ListLinkPreviews(urls []string) (map[string]*LinkPreview, error)

	// GetLinkPreviewByImageFile is the row whose picture is stored under the
	// file, or nil when no row names it.
	GetLinkPreviewByImageFile(imageFileId string) (*LinkPreview, error)

	// SaveLinkPreview keeps a page's preview, replacing the row for the same
	// address, and says which picture file the row named before, so the caller
	// can remove it once this commits. Empty when the row is new or named none.
	SaveLinkPreview(preview *LinkPreview) (replacedImageFileId string, err error)

	// ListLinkPreviewsFetchedBefore is the oldest rows fetched before the
	// time, at most limit of them, for removal.
	ListLinkPreviewsFetchedBefore(before time.Time, limit int) ([]*LinkPreview, error)

	// DeleteLinkPreviews removes the rows for these addresses.
	DeleteLinkPreviews(urls []string) error
}

// LinkPreview is what one page said about itself when it was last fetched.
type LinkPreview struct {
	// URL is the page's address without its tracking parameters or fragment.
	URL string `gorm:"column:url;primaryKey;type:text"`

	Title       string `gorm:"column:title;type:text"`
	Description string `gorm:"column:description;type:text"`
	SiteName    string `gorm:"column:site_name;type:text"`

	// ImageFileID is the file store's name for the page's picture, empty when
	// there is none.
	ImageFileID       string `gorm:"column:image_file_id;size:128"`
	ImageSizeBytes    int    `gorm:"column:image_size_bytes"`
	ImageWidthPixels  int    `gorm:"column:image_width_pixels"`
	ImageHeightPixels int    `gorm:"column:image_height_pixels"`

	FetchedAt time.Time `gorm:"column:fetched_at"`

	// FetchFailure is empty for a page that was read, and a short word for why
	// when it could not be.
	FetchFailure string `gorm:"column:fetch_failure;type:text"`
}

func (self *LinkPreview) TableName() string {
	return "link_preview"
}

func (self *transaction) ListLinkPreviews(urls []string) (map[string]*LinkPreview, error) {
	previews := map[string]*LinkPreview{}
	if len(urls) == 0 {
		return previews, nil
	}
	var rows []LinkPreview
	if err := self.tx.Where("\"url\" IN ?", urls).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		preview := rows[index]
		preview.FetchedAt = preview.FetchedAt.In(time.Local)
		previews[preview.URL] = &preview
	}
	return previews, nil
}

func (self *transaction) GetLinkPreviewByImageFile(imageFileId string) (*LinkPreview, error) {
	if imageFileId == "" {
		return nil, nil
	}
	var rows []LinkPreview
	if err := self.tx.Where("\"image_file_id\" = ?", imageFileId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	preview := rows[0]
	preview.FetchedAt = preview.FetchedAt.In(time.Local)
	return &preview, nil
}

func (self *transaction) SaveLinkPreview(preview *LinkPreview) (string, error) {
	var existing []LinkPreview
	if err := self.tx.Select("\"image_file_id\"").Where("\"url\" = ?", preview.URL).Limit(1).Find(&existing).Error; err != nil {
		return "", err
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "url"}},
		UpdateAll: true,
	}).Create(preview).Error; err != nil {
		return "", err
	}
	if len(existing) == 0 || existing[0].ImageFileID == preview.ImageFileID {
		return "", nil
	}
	return existing[0].ImageFileID, nil
}

func (self *transaction) ListLinkPreviewsFetchedBefore(before time.Time, limit int) ([]*LinkPreview, error) {
	var rows []LinkPreview
	if err := self.tx.Where("\"fetched_at\" < ?", before).Order("\"fetched_at\" ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	previews := make([]*LinkPreview, len(rows))
	for index := range rows {
		previews[index] = &rows[index]
	}
	return previews, nil
}

func (self *transaction) DeleteLinkPreviews(urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	return self.tx.Where("\"url\" IN ?", urls).Delete(&LinkPreview{}).Error
}
