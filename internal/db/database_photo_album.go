package db

import (
	"slices"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PhotoAlbumOperation owns the albums: named sets of photos that belong to a
// person or to a circle. Who may do what with an album is decided here, in
// GetPhotoAlbumAccess: the owner, a member of the circle that owns it, and
// the people it is shared with each have their own part, and an album nobody
// reaches is not found.
//
// An album holds references. A photo in the trash stays in its albums but is
// not counted or listed there until it is restored.
type PhotoAlbumOperation interface {
	// ListPhotoAlbums lists the albums the person reaches, the most recently
	// changed first: their own, their circles', and those shared with them,
	// each with its photo count, its cover, the dates of its photos and what
	// the person may do with it.
	ListPhotoAlbums(userId string) ([]*models.PhotoAlbum, error)

	// GetPhotoAlbum is one album the person reaches at any level.
	GetPhotoAlbum(userId, photoAlbumId string) (*models.PhotoAlbum, error)

	// CreatePhotoAlbum makes an album of the person's holding the named
	// photos, which must all be the person's.
	CreatePhotoAlbum(userId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error)

	// CreateCirclePhotoAlbum makes an album that belongs to a circle the
	// person is in, holding the named photos, which must all be the
	// person's own: they stay theirs. ErrNotFound for a circle they are not
	// in.
	CreateCirclePhotoAlbum(userId, circleId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error)

	// RenamePhotoAlbum, DeletePhotoAlbum and SetPhotoAlbumCover are for the
	// album's manager: its owner, or an admin of the circle that owns it.
	RenamePhotoAlbum(userId, photoAlbumId, albumTitle string) (*models.PhotoAlbum, error)

	// DeletePhotoAlbum deletes the album and nothing else: its photos stay in
	// their owners' libraries.
	DeletePhotoAlbum(userId, photoAlbumId string) error

	// AddPhotosToAlbum puts the named photos, which must all be the person's
	// own, in an album they may contribute to, leaving alone those already in
	// it, and says how many were added.
	AddPhotosToAlbum(userId, photoAlbumId string, photoIds []string) (int, error)

	// RemovePhotosFromAlbum takes photos out of the album and says how many
	// were in it. The photos stay in their owners' libraries. The album's
	// manager may take out any photo, and somebody who contributes only the
	// photos they added; those that are not the person's to take out are left
	// where they are and not counted. If the cover goes, the album is left
	// with no chosen cover.
	RemovePhotosFromAlbum(userId, photoAlbumId string, photoIds []string) (int, error)

	// SetPhotoAlbumCover chooses the album's cover from among its photos.
	SetPhotoAlbumCover(userId, photoAlbumId, photoId string) (*models.PhotoAlbum, error)

	// ListPhotoAlbumsOfPhoto lists, by title, the albums a photo is in that
	// the person reaches. ErrNotFound unless the person may read the photo.
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

// photoAlbumModel is the album's row. Exactly one of UserID and CircleID is
// set.
type photoAlbumModel struct {
	ID           string    `gorm:"column:id;primaryKey"`
	UserID       *string   `gorm:"column:user_id"`
	CircleID     *string   `gorm:"column:circle_id"`
	AlbumTitle   string    `gorm:"column:album_title"`
	CoverPhotoID *string   `gorm:"column:cover_photo_id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (photoAlbumModel) TableName() string { return "photo_album" }

type photoAlbumItemModel models.PhotoAlbumItem

func (photoAlbumItemModel) TableName() string { return "photo_album_item" }

// photoAlbumRow is an album with what is worked out about its photos.
type photoAlbumRow struct {
	ID                         string
	UserID                     *string
	CircleID                   *string
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
		ID: row.ID, OwnerUserID: row.UserID, CircleID: row.CircleID, AlbumTitle: row.AlbumTitle, CoverPhotoID: row.CoverPhotoID,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
		PhotoCount: row.PhotoCount, FirstTakenAt: utcTime(row.FirstTakenAt), LastTakenAt: utcTime(row.LastTakenAt),
		CoverPreviewPhotoUpdatedAt: utcTime(row.CoverPreviewPhotoUpdatedAt),
	}
	if row.CoverPreviewPhotoID != nil {
		album.CoverPreviewPhotoID = *row.CoverPreviewPhotoID
	}
	return album
}

// photoAlbumsQuery selects the named albums with what is worked out about
// their photos: those not in the trash (a photo being deleted is in the trash
// as far as this is concerned). The cover is the chosen photo while it can be
// shown, else the newest photo that can be, and only a photo with a small
// preview can be shown.
const photoAlbumsQuery = `
	SELECT photo_album.id, photo_album.user_id, photo_album.circle_id, photo_album.album_title, photo_album.cover_photo_id, photo_album.created_at, photo_album.updated_at,
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
	WHERE photo_album.id IN ?`

// listReachablePhotoAlbums reads the albums the person reaches, or the one
// they name, newest change first, each marked with whose it is and what the
// person may do with it.
func (self *transaction) listReachablePhotoAlbums(userId, photoAlbumId string) ([]*models.PhotoAlbum, error) {
	reach, err := self.photoAlbumReach(userId, photoAlbumId)
	if err != nil {
		return nil, err
	}
	photoAlbumIds := make([]string, 0, len(reach))
	for reachedId := range reach {
		photoAlbumIds = append(photoAlbumIds, reachedId)
	}
	if len(photoAlbumIds) == 0 {
		return []*models.PhotoAlbum{}, nil
	}
	var rows []photoAlbumRow
	if err := self.tx.Raw(photoAlbumsQuery+` ORDER BY photo_album.updated_at DESC, photo_album.id DESC`, photoAlbumIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	albums := make([]*models.PhotoAlbum, 0, len(rows))
	ownerIds, circleIds := []string{}, []string{}
	for index := range rows {
		album := photoAlbumFromRow(&rows[index])
		access := reach[album.ID].access(album.ID, rows[index].UserID, rows[index].CircleID)
		album.IsOwnedByCaller = access.IsOwner
		album.ShareLevel, album.CanCallerContribute = access.ShareLevel, access.CanContribute()
		album.CanCallerManage, album.IsLocationIncluded = access.IsManager, access.IsLocationIncluded
		albums = append(albums, album)
		if rows[index].UserID != nil {
			ownerIds = append(ownerIds, *rows[index].UserID)
		}
		if rows[index].CircleID != nil {
			circleIds = append(circleIds, *rows[index].CircleID)
		}
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", uniqueStrings(circleIds)).Find(&circles).Error; err != nil {
			return nil, err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for _, album := range albums {
		if album.OwnerUserID != nil {
			album.OwnerUser = owners[*album.OwnerUserID]
			if album.OwnerUser != nil {
				album.OwnerUsername, album.OwnerName = album.OwnerUser.Username, album.OwnerUser.DisplayName()
			}
		}
		if album.CircleID != nil {
			album.CircleName = circleNames[*album.CircleID]
		}
	}
	return albums, nil
}

func (self *transaction) ListPhotoAlbums(userId string) ([]*models.PhotoAlbum, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	return self.listReachablePhotoAlbums(userId, "")
}

func (self *transaction) GetPhotoAlbum(userId, photoAlbumId string) (*models.PhotoAlbum, error) {
	if userId == "" || photoAlbumId == "" {
		return nil, ErrNotFound
	}
	albums, err := self.listReachablePhotoAlbums(userId, photoAlbumId)
	if err != nil {
		return nil, err
	}
	if len(albums) == 0 {
		return nil, ErrNotFound
	}
	return albums[0], nil
}

// photoAlbumNeed is how much of an album a change asks for.
type photoAlbumNeed int

const (
	photoAlbumNeedManage photoAlbumNeed = iota
	photoAlbumNeedContribute
)

// lockPhotoAlbum locks an album and checks that the person may do what the
// change needs, so that two changes to one album are made one after the other
// on the access as it stands. An album the person does not reach, and one they
// reach but not that far, is not found: which of the two it is is not theirs
// to learn. A change that adds to somebody else's album first takes the circle
// membership lock, as every change to who may reach an album does, so that
// somebody leaving at the same moment is seen by the cleanup that follows them.
func (self *transaction) lockPhotoAlbum(userId, photoAlbumId string, need photoAlbumNeed) (*photoAlbumModel, *models.PhotoAlbumAccess, error) {
	if userId == "" || photoAlbumId == "" {
		return nil, nil, ErrNotFound
	}
	if need == photoAlbumNeedContribute {
		var peeked photoAlbumModel
		isFound, err := findOneRow(self.tx.Where("id = ?", photoAlbumId), &peeked)
		if err != nil {
			return nil, nil, err
		}
		if !isFound {
			return nil, nil, ErrNotFound
		}
		if peeked.UserID == nil || *peeked.UserID != userId {
			if err := lockCircleMembership(self.tx); err != nil {
				return nil, nil, err
			}
		}
	}
	var row photoAlbumModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", photoAlbumId), &row)
	if err != nil {
		return nil, nil, err
	}
	if !isFound {
		return nil, nil, ErrNotFound
	}
	access, err := self.GetPhotoAlbumAccess(userId, photoAlbumId)
	if err != nil {
		return nil, nil, err
	}
	if (need == photoAlbumNeedManage && !access.IsManager) || (need == photoAlbumNeedContribute && !access.CanContribute()) {
		return nil, nil, ErrNotFound
	}
	return &row, access, nil
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
	return self.createPhotoAlbum(userId, "", albumTitle, photoIds)
}

func (self *transaction) CreateCirclePhotoAlbum(userId, circleId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error) {
	if circleId == "" {
		return nil, ErrNotFound
	}
	return self.createPhotoAlbum(userId, circleId, albumTitle, photoIds)
}

func (self *transaction) createPhotoAlbum(userId, circleId, albumTitle string, photoIds []string) (*models.PhotoAlbum, error) {
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
	row := photoAlbumModel{ID: newID(), AlbumTitle: albumTitle, CreatedAt: created, UpdatedAt: created}
	if circleId == "" {
		row.UserID = &userId
	} else {
		// The membership is held for the rest of the transaction, so that a
		// person leaving the circle at this moment is seen by the cleanup, or
		// refused here.
		if err := lockCircleMembership(self.tx); err != nil {
			return nil, err
		}
		var members []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, userId).Find(&members).Error; err != nil {
			return nil, err
		}
		if len(members) == 0 {
			return nil, ErrNotFound
		}
		row.CircleID = &circleId
	}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	if _, err := self.insertPhotoAlbumItems(userId, row.ID, photoIds); err != nil {
		return nil, err
	}
	if err := self.refreshPhotoSearchDocumentsOf(photoIds); err != nil {
		return nil, err
	}
	return self.GetPhotoAlbum(userId, row.ID)
}

func (self *transaction) RenamePhotoAlbum(userId, photoAlbumId, albumTitle string) (*models.PhotoAlbum, error) {
	albumTitle, err := models.NormalizePhotoAlbumTitle(albumTitle)
	if err != nil {
		return nil, err
	}
	if _, _, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedManage); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Updates(map[string]any{"album_title": albumTitle, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	// The title is part of the search document of every photo in the album.
	if err := self.refreshPhotoSearchDocuments("photo.id IN (SELECT photo_id FROM photo_album_item WHERE photo_album_id = ?)", photoAlbumId); err != nil {
		return nil, err
	}
	return self.GetPhotoAlbum(userId, photoAlbumId)
}

func (self *transaction) DeletePhotoAlbum(userId, photoAlbumId string) error {
	if _, _, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedManage); err != nil {
		return err
	}
	// The photos are named before the album goes, since its items go with it.
	var photoIds []string
	if err := self.tx.Model(&photoAlbumItemModel{}).Where("photo_album_id = ?", photoAlbumId).Pluck("photo_id", &photoIds).Error; err != nil {
		return err
	}
	if err := self.tx.Where("id = ?", photoAlbumId).Delete(&photoAlbumModel{}).Error; err != nil {
		return err
	}
	return self.refreshPhotoSearchDocumentsOf(photoIds)
}

func (self *transaction) AddPhotosToAlbum(userId, photoAlbumId string, photoIds []string) (int, error) {
	photoIds, err := distinctPhotoIds(photoIds)
	if err != nil {
		return 0, err
	}
	if _, _, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedContribute); err != nil {
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
		if err := self.refreshPhotoSearchDocumentsOf(photoIds); err != nil {
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
	_, access, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedContribute)
	if err != nil {
		return 0, err
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	removal := self.tx.Where("photo_album_id = ? AND photo_id IN ?", photoAlbumId, photoIds)
	if !access.IsManager {
		removal = removal.Where("added_by_user_id = ?", userId)
	}
	removed := removal.Delete(&photoAlbumItemModel{})
	if removed.Error != nil {
		return 0, removed.Error
	}
	if removed.RowsAffected > 0 {
		if err := self.afterPhotosLeaveAlbum(photoAlbumId, photoIds); err != nil {
			return 0, err
		}
	}
	return int(removed.RowsAffected), nil
}

// afterPhotosLeaveAlbum keeps what depends on an album's photos true once
// some have been taken out: a cover must be in the album, and the search
// document of a photo names the albums it is in.
func (self *transaction) afterPhotosLeaveAlbum(photoAlbumId string, photoIds []string) error {
	changes := map[string]any{"updated_at": photoNow(), "cover_photo_id": gorm.Expr("CASE WHEN cover_photo_id IN ? THEN NULL ELSE cover_photo_id END", photoIds)}
	if err := self.tx.Model(&photoAlbumModel{}).Where("id = ?", photoAlbumId).Updates(changes).Error; err != nil {
		return err
	}
	return self.refreshPhotoSearchDocumentsOf(photoIds)
}

func (self *transaction) SetPhotoAlbumCover(userId, photoAlbumId, photoId string) (*models.PhotoAlbum, error) {
	if photoId == "" {
		return nil, ErrNotFound
	}
	if _, _, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedManage); err != nil {
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
	if _, err := self.GetPhotoAccess(userId, photoId); err != nil {
		return nil, err
	}
	reach, err := self.photoAlbumReach(userId, "")
	if err != nil {
		return nil, err
	}
	reachedIds := make([]string, 0, len(reach))
	for reachedId := range reach {
		reachedIds = append(reachedIds, reachedId)
	}
	references := []*models.PhotoAlbumReference{}
	if len(reachedIds) == 0 {
		return references, nil
	}
	var rows []photoAlbumModel
	if err := self.tx.Joins("JOIN photo_album_item ON photo_album_item.photo_album_id = photo_album.id").
		Where("photo_album_item.photo_id = ? AND photo_album.id IN ?", photoId, reachedIds).
		Order("photo_album.album_title, photo_album.id").Find(&rows).Error; err != nil {
		return nil, err
	}
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
