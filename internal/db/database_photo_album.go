package db

import (
	"slices"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PhotoAlbumOperation owns the albums: named sets of a person's photos. Every
// query is scoped by the owner, so an album or a photo that is somebody
// else's is not found.
//
// An album holds references. A photo in the trash stays in its albums but is
// not counted or listed there until it is restored.
type PhotoAlbumOperation interface {
	// ListPhotoAlbums lists the person's albums, the most recently changed
	// first, each with its photo count, its cover and the dates of its photos.
	ListPhotoAlbums(userId string) ([]*models.PhotoAlbum, error)
	GetPhotoAlbum(userId, photoAlbumId string) (*models.PhotoAlbum, error)

	// CreatePhotoAlbum makes an album holding the named photos, which must all
	// be the person's.
	CreatePhotoAlbum(userId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error)
	RenamePhotoAlbum(userId, photoAlbumId, albumTitle string) (*models.PhotoAlbum, error)

	// DeletePhotoAlbum deletes the album and nothing else: its photos stay in
	// the library.
	DeletePhotoAlbum(userId, photoAlbumId string) error

	// AddPhotosToAlbum puts the named photos, which must all be the person's,
	// in the album, leaving alone those already in it, and says how many were
	// added.
	AddPhotosToAlbum(userId, photoAlbumId string, photoIds []string) (int, error)

	// RemovePhotosFromAlbum takes photos out of the album and says how many
	// were in it. The photos stay in the library. If the cover goes, the album
	// is left with no chosen cover.
	RemovePhotosFromAlbum(userId, photoAlbumId string, photoIds []string) (int, error)

	// SetPhotoAlbumCover chooses the album's cover from among its photos.
	SetPhotoAlbumCover(userId, photoAlbumId, photoId string) (*models.PhotoAlbum, error)

	// ListPhotoAlbumsOfPhoto lists the albums a photo is in, by title.
	ListPhotoAlbumsOfPhoto(userId, photoId string) ([]*models.PhotoAlbumReference, error)

	// ListPhotosInNoAlbum lists, in time order, the person's photos that are
	// not in the trash, are in no album and say when they were taken, taken
	// within the dates. Only the columns that say where and when are filled
	// in. A question with more answers than a suggestion needs is refused with
	// ErrTooMuchAsked, so that the dates are narrowed.
	ListPhotosInNoAlbum(userId string, takenAfter, takenBefore *time.Time) ([]*models.Photo, error)
}

// maximumPhotosInNoAlbum bounds how many photos a suggestion reads at once.
const maximumPhotosInNoAlbum = 100000

type photoAlbumModel models.PhotoAlbum

func (photoAlbumModel) TableName() string { return "photo_album" }

type photoAlbumItemModel models.PhotoAlbumItem

func (photoAlbumItemModel) TableName() string { return "photo_album_item" }

// photoAlbumRow is an album with what is worked out about its photos.
type photoAlbumRow struct {
	ID                         string
	UserID                     string
	AlbumTitle                 string
	CoverPhotoID               *string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
	PhotoCount                 int
	FirstTakenAt               *time.Time
	LastTakenAt                *time.Time
	CoverPreviewPhotoID        *string
	CoverPreviewPhotoUpdatedAt *time.Time
}

func photoAlbumFromRow(row *photoAlbumRow) *models.PhotoAlbum {
	album := &models.PhotoAlbum{
		ID: row.ID, UserID: row.UserID, AlbumTitle: row.AlbumTitle, CoverPhotoID: row.CoverPhotoID,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
		PhotoCount: row.PhotoCount, FirstTakenAt: utcTime(row.FirstTakenAt), LastTakenAt: utcTime(row.LastTakenAt),
		CoverPreviewPhotoUpdatedAt: utcTime(row.CoverPreviewPhotoUpdatedAt),
	}
	if row.CoverPreviewPhotoID != nil {
		album.CoverPreviewPhotoID = *row.CoverPreviewPhotoID
	}
	return album
}

// photoAlbumsQuery selects the person's albums with what is worked out about
// their photos: those not in the trash (a photo being deleted is in the trash
// as far as this is concerned). The cover is the chosen photo while it can be
// shown, else the newest photo that can be, and only a photo with a small
// preview can be shown.
const photoAlbumsQuery = `
	SELECT photo_album.id, photo_album.user_id, photo_album.album_title, photo_album.cover_photo_id, photo_album.created_at, photo_album.updated_at,
		COALESCE(counts.photo_count, 0) AS photo_count, counts.first_taken_at, counts.last_taken_at,
		cover.id AS cover_preview_photo_id, cover.updated_at AS cover_preview_photo_updated_at
	FROM photo_album
	LEFT JOIN LATERAL (
		SELECT count(*) AS photo_count,
			min(` + photoTimelineExpression + `) AS first_taken_at,
			max(` + photoTimelineExpression + `) AS last_taken_at
		FROM photo_album_item
		JOIN photo ON photo.id = photo_album_item.photo_id
		WHERE photo_album_item.photo_album_id = photo_album.id AND photo.trashed_at IS NULL
	) counts ON true
	LEFT JOIN LATERAL (
		SELECT photo.id, photo.updated_at
		FROM photo_album_item
		JOIN photo ON photo.id = photo_album_item.photo_id
		JOIN photo_preview ON photo_preview.photo_id = photo.id AND photo_preview.photo_preview_size = 'small'
		WHERE photo_album_item.photo_album_id = photo_album.id AND photo.trashed_at IS NULL
		ORDER BY COALESCE(photo.id = photo_album.cover_photo_id, false) DESC, ` + photoTimelineExpression + ` DESC, photo.id DESC
		LIMIT 1
	) cover ON true
	WHERE photo_album.user_id = ?`

func (self *transaction) ListPhotoAlbums(userId string) ([]*models.PhotoAlbum, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var rows []photoAlbumRow
	if err := self.tx.Raw(photoAlbumsQuery+` ORDER BY photo_album.updated_at DESC, photo_album.id DESC`, userId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	albums := make([]*models.PhotoAlbum, 0, len(rows))
	for index := range rows {
		albums = append(albums, photoAlbumFromRow(&rows[index]))
	}
	return albums, nil
}

func (self *transaction) GetPhotoAlbum(userId, photoAlbumId string) (*models.PhotoAlbum, error) {
	if userId == "" || photoAlbumId == "" {
		return nil, ErrNotFound
	}
	var rows []photoAlbumRow
	if err := self.tx.Raw(photoAlbumsQuery+` AND photo_album.id = ?`, userId, photoAlbumId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return photoAlbumFromRow(&rows[0]), nil
}

// lockPhotoAlbum finds an album of the person's and locks it, so that two
// changes to one album are made one after the other.
func (self *transaction) lockPhotoAlbum(userId, photoAlbumId string) (*photoAlbumModel, error) {
	if userId == "" || photoAlbumId == "" {
		return nil, ErrNotFound
	}
	var row photoAlbumModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", photoAlbumId, userId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &row, nil
}

// distinctPhotoIds is the photos named, each once, in the order named. The
// most one call names is the most the other photo operations take.
func distinctPhotoIds(photoIds []string) ([]string, error) {
	if len(photoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	distinct := make([]string, 0, len(photoIds))
	for _, photoId := range photoIds {
		if photoId == "" {
			return nil, ErrInvalidArguments
		}
		if !slices.Contains(distinct, photoId) {
			distinct = append(distinct, photoId)
		}
	}
	return distinct, nil
}

// requireOwnedPhotos checks that every named photo is the person's and is not
// being deleted. One that is not is not found, and nothing is changed, so that
// a mistaken identifier is told and not skipped.
func (self *transaction) requireOwnedPhotos(userId string, photoIds []string) error {
	if len(photoIds) == 0 {
		return nil
	}
	var foundCount int64
	if err := self.tx.Model(&photoModel{}).Where("photo.user_id = ? AND photo.id IN ? AND "+photoIsReadable, userId, photoIds, photoMarkedForDeletion).Count(&foundCount).Error; err != nil {
		return err
	}
	if int(foundCount) != len(photoIds) {
		return ErrNotFound
	}
	return nil
}

// insertPhotoAlbumItems puts the photos in the album, skipping those already
// in it, and says how many were put in.
func (self *transaction) insertPhotoAlbumItems(userId, photoAlbumId string, photoIds []string) (int, error) {
	if len(photoIds) == 0 {
		return 0, nil
	}
	addedAt := photoNow()
	items := make([]photoAlbumItemModel, 0, len(photoIds))
	for _, photoId := range photoIds {
		items = append(items, photoAlbumItemModel{PhotoAlbumID: photoAlbumId, PhotoID: photoId, AddedByUserID: userId, AddedAt: addedAt})
	}
	inserted := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&items)
	return int(inserted.RowsAffected), inserted.Error
}

func (self *transaction) touchPhotoAlbum(photoAlbumId string) error {
	return self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Update("updated_at", photoNow()).Error
}

func (self *transaction) CreatePhotoAlbum(userId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	albumTitle, err := models.NormalizePhotoAlbumTitle(albumTitle)
	if err != nil {
		return nil, err
	}
	photoIds, err = distinctPhotoIds(photoIds)
	if err != nil {
		return nil, err
	}
	if err := self.requireOwnedPhotos(userId, photoIds); err != nil {
		return nil, err
	}
	created := photoNow()
	row := photoAlbumModel{ID: newID(), UserID: userId, AlbumTitle: albumTitle, CreatedAt: created, UpdatedAt: created}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	if _, err := self.insertPhotoAlbumItems(userId, row.ID, photoIds); err != nil {
		return nil, err
	}
	return self.GetPhotoAlbum(userId, row.ID)
}

func (self *transaction) RenamePhotoAlbum(userId, photoAlbumId, albumTitle string) (*models.PhotoAlbum, error) {
	albumTitle, err := models.NormalizePhotoAlbumTitle(albumTitle)
	if err != nil {
		return nil, err
	}
	if _, err := self.lockPhotoAlbum(userId, photoAlbumId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Updates(map[string]any{"album_title": albumTitle, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	return self.GetPhotoAlbum(userId, photoAlbumId)
}

func (self *transaction) DeletePhotoAlbum(userId, photoAlbumId string) error {
	if _, err := self.lockPhotoAlbum(userId, photoAlbumId); err != nil {
		return err
	}
	return self.tx.Where("id = ?", photoAlbumId).Delete(&photoAlbumModel{}).Error
}

func (self *transaction) AddPhotosToAlbum(userId, photoAlbumId string, photoIds []string) (int, error) {
	photoIds, err := distinctPhotoIds(photoIds)
	if err != nil {
		return 0, err
	}
	if _, err := self.lockPhotoAlbum(userId, photoAlbumId); err != nil {
		return 0, err
	}
	if err := self.requireOwnedPhotos(userId, photoIds); err != nil {
		return 0, err
	}
	addedCount, err := self.insertPhotoAlbumItems(userId, photoAlbumId, photoIds)
	if err != nil {
		return 0, err
	}
	if addedCount > 0 {
		if err := self.touchPhotoAlbum(photoAlbumId); err != nil {
			return 0, err
		}
	}
	return addedCount, nil
}

func (self *transaction) RemovePhotosFromAlbum(userId, photoAlbumId string, photoIds []string) (int, error) {
	photoIds, err := distinctPhotoIds(photoIds)
	if err != nil {
		return 0, err
	}
	if _, err := self.lockPhotoAlbum(userId, photoAlbumId); err != nil {
		return 0, err
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	removed := self.tx.Where("photo_album_id = ? AND photo_id IN ?", photoAlbumId, photoIds).Delete(&photoAlbumItemModel{})
	if removed.Error != nil {
		return 0, removed.Error
	}
	if removed.RowsAffected > 0 {
		// A cover must be in the album.
		changes := map[string]any{"updated_at": photoNow(), "cover_photo_id": gorm.Expr("CASE WHEN cover_photo_id IN ? THEN NULL ELSE cover_photo_id END", photoIds)}
		if err := self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Updates(changes).Error; err != nil {
			return 0, err
		}
	}
	return int(removed.RowsAffected), nil
}

func (self *transaction) SetPhotoAlbumCover(userId, photoAlbumId, photoId string) (*models.PhotoAlbum, error) {
	if photoId == "" {
		return nil, ErrNotFound
	}
	if _, err := self.lockPhotoAlbum(userId, photoAlbumId); err != nil {
		return nil, err
	}
	// A photo in the trash is not shown in the album, so it cannot stand for it.
	var memberCount int64
	if err := self.tx.Model(&photoAlbumItemModel{}).
		Joins("JOIN photo ON photo.id = photo_album_item.photo_id").
		Where("photo_album_item.photo_album_id = ? AND photo_album_item.photo_id = ? AND photo.trashed_at IS NULL", photoAlbumId, photoId).
		Count(&memberCount).Error; err != nil {
		return nil, err
	}
	if memberCount == 0 {
		return nil, ErrNotFound
	}
	if err := self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Updates(map[string]any{"cover_photo_id": photoId, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	return self.GetPhotoAlbum(userId, photoAlbumId)
}

func (self *transaction) ListPhotoAlbumsOfPhoto(userId, photoId string) ([]*models.PhotoAlbumReference, error) {
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return nil, err
	}
	var rows []photoAlbumModel
	if err := self.tx.Joins("JOIN photo_album_item ON photo_album_item.photo_album_id = photo_album.id").
		Where("photo_album_item.photo_id = ? AND photo_album.user_id = ?", photoId, userId).
		Order("photo_album.album_title, photo_album.id").Find(&rows).Error; err != nil {
		return nil, err
	}
	references := make([]*models.PhotoAlbumReference, 0, len(rows))
	for _, row := range rows {
		references = append(references, &models.PhotoAlbumReference{ID: row.ID, AlbumTitle: row.AlbumTitle})
	}
	return references, nil
}

func (self *transaction) ListPhotosInNoAlbum(userId string, takenAfter, takenBefore *time.Time) ([]*models.Photo, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	query := self.tx.Model(&photoModel{}).
		Select("photo.id, photo.user_id, photo.taken_at, photo.taken_at_offset_minutes, photo.place_name").
		Where("photo.user_id = ? AND photo.trashed_at IS NULL AND photo.taken_at IS NOT NULL", userId).
		Where("NOT EXISTS (SELECT 1 FROM photo_album_item WHERE photo_album_item.photo_id = photo.id)")
	if takenAfter != nil {
		query = query.Where("photo.taken_at >= ?", *takenAfter)
	}
	if takenBefore != nil {
		query = query.Where("photo.taken_at < ?", *takenBefore)
	}
	var rows []photoModel
	if err := query.Order("photo.taken_at, photo.id").Limit(maximumPhotosInNoAlbum + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > maximumPhotosInNoAlbum {
		return nil, ErrTooMuchAsked
	}
	return photosFromModels(rows), nil
}
