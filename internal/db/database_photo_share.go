package db

import (
	"database/sql"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// PhotoAlbumShareOperation is who may read and write which album: an album
// shared with another person or with a circle at a level, and the albums a
// circle keeps for itself.
//
// "May this person read this photo or this album, and how far" has one
// answer, GetPhotoAccess and GetPhotoAlbumAccess, and every door asks it: the
// dashboard's API, the phone, the command line and the agent. A caller that
// compares a photo's or an album's owner with the person instead is wrong for
// a circle's album, which has none, and for a photo somebody else added to it.
type PhotoAlbumShareOperation interface {
	// GetPhotoAlbumAccess is what the person may do with the album. ErrNotFound
	// when there is no such album or the person reaches it at no level: an
	// album they own, one that belongs to a circle they are in, or one shared
	// with them. A share to a circle reaches a member only while the album's
	// owner is still in that circle, and a share to a person only while the
	// two still share a circle.
	GetPhotoAlbumAccess(userId, photoAlbumId string) (*models.PhotoAlbumAccess, error)

	// GetPhotoAccess is why the person may read the photo. ErrNotFound unless
	// it is theirs or is in an album they reach, and not in the trash for
	// anybody but its owner.
	GetPhotoAccess(userId, photoId string) (*models.PhotoAccess, error)

	// GetPhotoFaceOwnerUserID is whose photo the face was found in, for the
	// route that serves a face's picture and has only the face's identifier
	// to start from. ErrNotFound when there is no such face.
	GetPhotoFaceOwnerUserID(photoFaceId string) (string, error)

	// SharePhotoAlbum gives a person or a circle a level of access to an album
	// the sharer manages, or changes the level they have. ErrNotFound unless
	// the sharer manages the album and, for a person, the two share a circle,
	// or, for a circle, the sharer is in it. ErrInvalidArguments for a level
	// that cannot be given, a grantee that is not exactly one, oneself, and an
	// album that belongs to a circle: every member of it has it already.
	SharePhotoAlbum(sharerUserId string, share *models.PhotoAlbumShare) (*models.PhotoAlbumShare, error)

	// UpdatePhotoAlbumShare changes the level of a share and whether it
	// includes where photos were taken. ErrNotFound unless the person manages
	// the album it is for.
	UpdatePhotoAlbumShare(userId, photoAlbumShareId string, shareLevel models.PhotoAlbumShareLevel, isLocationIncluded bool) (*models.PhotoAlbumShare, error)

	// DeletePhotoAlbumShare takes a share back, and with it the photos of the
	// people it reached that are no longer theirs to have in the album.
	// ErrNotFound unless the person manages the album it is for.
	DeletePhotoAlbumShare(userId, photoAlbumShareId string) error

	// ListPhotoAlbumShares are an album's shares with who they are for, by
	// when they were made. ErrNotFound unless the person manages the album.
	ListPhotoAlbumShares(userId, photoAlbumId string) ([]*models.PhotoAlbumShare, error)

	// ListPhotoAlbumReaderUserIDs are everybody who reads the album now, its
	// owner and the members of the circle that owns it included, once each.
	ListPhotoAlbumReaderUserIDs(photoAlbumId string) ([]string, error)
}

type photoAlbumShareModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	PhotoAlbumID       string    `gorm:"column:photo_album_id"`
	GranteeUserID      *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID    *string   `gorm:"column:grantee_circle_id"`
	ShareLevel         string    `gorm:"column:share_level"`
	IsLocationIncluded bool      `gorm:"column:is_location_included"`
	CreatedByUserID    *string   `gorm:"column:created_by_user_id"`
	CreatedAt          time.Time `gorm:"column:created_at"`
}

func (photoAlbumShareModel) TableName() string { return "photo_album_share" }

func (self *photoAlbumShareModel) toModel() *models.PhotoAlbumShare {
	share := &models.PhotoAlbumShare{
		ID: self.ID, PhotoAlbumID: self.PhotoAlbumID, ShareLevel: models.PhotoAlbumShareLevel(self.ShareLevel),
		IsLocationIncluded: self.IsLocationIncluded, CreatedAt: self.CreatedAt.UTC(),
	}
	if self.GranteeUserID != nil {
		share.GranteeUserID = *self.GranteeUserID
	}
	if self.GranteeCircleID != nil {
		share.GranteeCircleID = *self.GranteeCircleID
	}
	if self.CreatedByUserID != nil {
		share.CreatedByUserID = *self.CreatedByUserID
	}
	return share
}

// photoAlbumReachQuery selects the ways one person reaches albums: the
// albums they own, the albums of the circles they are in, and the shares
// that reach them. With an album named it is that album only. This is the
// one statement that says who reaches an album; the joins on circle_member
// are what make a share stop reaching somebody the moment the circle it
// depended on is gone: a share to a person needs the two to share a circle,
// and a share to a circle needs both the reader and the album's owner to be
// in it.
//
// Where a photo was taken is shown to somebody only through a share that
// includes it, and only for the photos of the album's owner, whose share it
// is: album_owner_user_id says whose that is, for the caller to compare with
// the owner of each photo. A photo somebody else added to the album is theirs,
// and they shared its place with nobody. The owner and a member of a circle
// that owns the album are given none here: the owner of a photo sees its place
// whoever added it to what, and nobody sees the place of a photo somebody else
// added to their album unless it was shared with that.
//
// A share stops reaching people while its owner is disabled or no longer has
// photos:use (@permission), as a Location or a Health share does.
var photoAlbumReachQuery = `
SELECT album.id AS photo_album_id, 'owner' AS reach_kind, 'contribute' AS share_level, false AS is_circle_admin, false AS is_location_included,
  COALESCE(album.user_id, '') AS album_owner_user_id, ''::text AS share_id, NULL::text AS grantee_user_id, NULL::text AS grantee_circle_id,
  album.created_at AS share_created_at
FROM "photo_album" album
WHERE album.user_id = @reader AND (@album = '' OR album.id = @album)
UNION ALL
SELECT album.id, 'circle', 'contribute', member.member_role = 'admin', false, COALESCE(album.user_id, ''), ''::text, NULL::text, NULL::text,
  album.created_at
FROM "photo_album" album
JOIN "circle_member" member ON member.circle_id = album.circle_id AND member.user_id = @reader
WHERE @album = '' OR album.id = @album
UNION ALL
SELECT share.photo_album_id, 'share', share.share_level, false, share.is_location_included, album.user_id,
  share.id, share.grantee_user_id, share.grantee_circle_id, share.created_at
FROM "photo_album_share" share
JOIN "photo_album" album ON album.id = share.photo_album_id
JOIN "user" owner_user ON owner_user.id = album.user_id AND owner_user.disabled_at IS NULL
WHERE album.user_id IS NOT NULL AND album.user_id <> @reader AND (@album = '' OR album.id = @album)
  AND ` + userHasPermissionSQL("album.user_id") + `
  AND (
    (share.grantee_user_id = @reader AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @reader AND theirs.user_id = album.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @reader)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = album.user_id))
  )`

type photoAlbumReachRow struct {
	PhotoAlbumID       string `gorm:"column:photo_album_id"`
	ReachKind          string `gorm:"column:reach_kind"`
	ShareLevel         string `gorm:"column:share_level"`
	IsCircleAdmin      bool   `gorm:"column:is_circle_admin"`
	IsLocationIncluded bool   `gorm:"column:is_location_included"`

	// What names the share, for the rows whose ReachKind is share; ListPhotoAlbumSharesReaching
	// reads them. AlbumOwnerUserID is the album's owner.
	AlbumOwnerUserID string    `gorm:"column:album_owner_user_id"`
	ShareID          string    `gorm:"column:share_id"`
	GranteeUserID    *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID  *string   `gorm:"column:grantee_circle_id"`
	ShareCreatedAt   time.Time `gorm:"column:share_created_at"`
}

// photoAlbumReach is everything the ways that reach one person to one album
// add up to.
type photoAlbumReach struct {
	isOwner            bool
	isCircleMember     bool
	isCircleAdmin      bool
	shareLevel         models.PhotoAlbumShareLevel
	isLocationIncluded bool
}

// access is the reach as the access answer: the person's level and powers
// over the album, whose columns the album's row gave.
func (self *photoAlbumReach) access(photoAlbumId string, ownerUserId, circleId *string) *models.PhotoAlbumAccess {
	access := &models.PhotoAlbumAccess{PhotoAlbumID: photoAlbumId, IsOwner: self.isOwner, IsLocationIncluded: self.isLocationIncluded}
	if ownerUserId != nil {
		access.OwnerUserID = *ownerUserId
	}
	if circleId != nil {
		access.CircleID = *circleId
	}
	access.IsManager = self.isOwner || self.isCircleAdmin
	switch {
	case self.isOwner:
		// The owner has more than any level, and no level of their own.
	case self.isCircleMember || self.shareLevel == models.PhotoAlbumShareLevelContribute:
		access.ShareLevel = models.PhotoAlbumShareLevelContribute
	default:
		access.ShareLevel = self.shareLevel
	}
	return access
}

// photoAlbumReach is, for each album the person reaches (or the one they
// name), how they reach it.
func (self *transaction) photoAlbumReach(userId, photoAlbumId string) (map[string]*photoAlbumReach, error) {
	var rows []photoAlbumReachRow
	if err := self.tx.Raw(photoAlbumReachQuery, sql.Named("reader", userId), sql.Named("album", photoAlbumId),
		sql.Named("permission", string(models.PermissionPhotosUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	reach := make(map[string]*photoAlbumReach, len(rows))
	for _, row := range rows {
		found := reach[row.PhotoAlbumID]
		if found == nil {
			found = &photoAlbumReach{}
			reach[row.PhotoAlbumID] = found
		}
		switch row.ReachKind {
		case "owner":
			found.isOwner = true
		case "circle":
			found.isCircleMember = true
			found.isCircleAdmin = found.isCircleAdmin || row.IsCircleAdmin
		default:
			level := models.PhotoAlbumShareLevel(row.ShareLevel)
			if level == models.PhotoAlbumShareLevelContribute || found.shareLevel == "" {
				found.shareLevel = level
			}
			found.isLocationIncluded = found.isLocationIncluded || row.IsLocationIncluded
		}
	}
	return reach, nil
}

func (self *transaction) GetPhotoAlbumAccess(userId, photoAlbumId string) (*models.PhotoAlbumAccess, error) {
	if userId == "" || photoAlbumId == "" {
		return nil, ErrNotFound
	}
	reach, err := self.photoAlbumReach(userId, photoAlbumId)
	if err != nil {
		return nil, err
	}
	found := reach[photoAlbumId]
	if found == nil {
		return nil, ErrNotFound
	}
	var row photoAlbumModel
	isFound, err := findOneRow(self.tx.Where("id = ?", photoAlbumId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return found.access(row.ID, row.UserID, row.CircleID), nil
}

func (self *transaction) GetPhotoAccess(userId, photoId string) (*models.PhotoAccess, error) {
	if userId == "" || photoId == "" {
		return nil, ErrNotFound
	}
	var row photoModel
	isFound, err := findOneRow(self.tx.Select("id", "user_id", "trashed_at").
		Where("photo.id = ? AND "+photoIsReadable, photoId, photoMarkedForDeletion), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	if row.UserID == userId {
		return &models.PhotoAccess{PhotoID: row.ID, OwnerUserID: row.UserID, IsOwner: true, IsLocationIncluded: true}, nil
	}
	// The trash is the owner's alone: a photo in it is gone from every album
	// as far as anybody else can tell, until it is restored.
	if row.TrashedAt != nil {
		return nil, ErrNotFound
	}
	// A disabled account's photos reach nobody else, in an album of another
	// person as in its own.
	var enabledCount int64
	if err := self.tx.Model(&userModel{}).Where("\"id\" = ? AND \"disabled_at\" IS NULL", row.UserID).Count(&enabledCount).Error; err != nil {
		return nil, err
	}
	if enabledCount == 0 {
		return nil, ErrNotFound
	}
	var reaching []struct {
		IsLocationIncluded bool   `gorm:"column:is_location_included"`
		AlbumOwnerUserID   string `gorm:"column:album_owner_user_id"`
	}
	if err := self.tx.Raw(`SELECT reach.is_location_included AS is_location_included, reach.album_owner_user_id AS album_owner_user_id
		FROM "photo_album_item" item
		JOIN (`+photoAlbumReachQuery+`) reach ON reach.photo_album_id = item.photo_album_id
		WHERE item.photo_id = @photo`,
		sql.Named("reader", userId), sql.Named("album", ""), sql.Named("photo", photoId),
		sql.Named("permission", string(models.PermissionPhotosUse))).Scan(&reaching).Error; err != nil {
		return nil, err
	}
	if len(reaching) == 0 {
		return nil, ErrNotFound
	}
	access := &models.PhotoAccess{PhotoID: row.ID, OwnerUserID: row.UserID}
	for _, found := range reaching {
		// The share that includes places is the album owner's, so it covers
		// the album owner's photos and not the ones others added to it.
		access.IsLocationIncluded = access.IsLocationIncluded || (found.IsLocationIncluded && found.AlbumOwnerUserID == row.UserID)
	}
	return access, nil
}

func (self *transaction) GetPhotoFaceOwnerUserID(photoFaceId string) (string, error) {
	if photoFaceId == "" {
		return "", ErrNotFound
	}
	var ownerUserIds []string
	if err := self.tx.Table("photo_face").Joins("JOIN photo ON photo.id = photo_face.photo_id").
		Where("photo_face.id = ?", photoFaceId).Limit(1).Pluck("photo.user_id", &ownerUserIds).Error; err != nil {
		return "", err
	}
	if len(ownerUserIds) == 0 {
		return "", ErrNotFound
	}
	return ownerUserIds[0], nil
}

// lockPhotoAlbumToShare takes the circle membership lock and then the
// album's row, and checks that the person manages the album and that it is a
// person's. The lock goes first, as it does in every change that rests on who
// is in a circle, so that a share made at the moment somebody leaves is seen
// by the cleanup that follows them.
func (self *transaction) lockPhotoAlbumToShare(userId, photoAlbumId string) (*photoAlbumModel, error) {
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	row, _, err := self.lockPhotoAlbum(userId, photoAlbumId, photoAlbumNeedManage)
	if err != nil {
		return nil, err
	}
	// A circle's album is not shared further: every member has it.
	if row.UserID == nil {
		return nil, ErrInvalidArguments
	}
	return row, nil
}

func (self *transaction) SharePhotoAlbum(sharerUserId string, share *models.PhotoAlbumShare) (*models.PhotoAlbumShare, error) {
	if share == nil || sharerUserId == "" || !share.ShareLevel.IsValid() || (share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	album, err := self.lockPhotoAlbumToShare(sharerUserId, share.PhotoAlbumID)
	if err != nil {
		return nil, err
	}
	if share.GranteeUserID != "" {
		if share.GranteeUserID == sharerUserId {
			return nil, ErrInvalidArguments
		}
		var count int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNotFound
		}
		isMutual, err := self.holdMutualCircle(sharerUserId, share.GranteeUserID)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	} else {
		var members []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&members).Error; err != nil {
			return nil, err
		}
		if len(members) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &photoAlbumShareModel{
		ID:                 newID(),
		PhotoAlbumID:       album.ID,
		GranteeUserID:      nullableText(share.GranteeUserID),
		GranteeCircleID:    nullableText(share.GranteeCircleID),
		ShareLevel:         string(share.ShareLevel),
		IsLocationIncluded: share.IsLocationIncluded,
		CreatedByUserID:    &sharerUserId,
		CreatedAt:          photoNow(),
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"share_level", "is_location_included"})}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "photo_album_id"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "photo_album_id"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	query := self.tx.Where("\"photo_album_id\" = ?", album.ID)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	var kept photoAlbumShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	return kept.toModel(), nil
}

// shareToManage is the share, with the album it is for locked, when the
// person manages that album. A share that is not there and one for an album
// the person does not manage are both not found.
func (self *transaction) shareToManage(userId, photoAlbumShareId string) (*photoAlbumShareModel, error) {
	if userId == "" || photoAlbumShareId == "" {
		return nil, ErrNotFound
	}
	var found photoAlbumShareModel
	isFound, err := findOneRow(self.tx.Where("id = ?", photoAlbumShareId), &found)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	// The album's lock is taken after the one on the circle membership, in
	// that order, and the share is read again once the album is ours.
	if _, err := self.lockPhotoAlbumToShare(userId, found.PhotoAlbumID); err != nil {
		if errors.Is(err, ErrInvalidArguments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	isFound, err = findOneRow(self.tx.Where("id = ?", photoAlbumShareId), &found)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &found, nil
}

func (self *transaction) UpdatePhotoAlbumShare(userId, photoAlbumShareId string, shareLevel models.PhotoAlbumShareLevel, isLocationIncluded bool) (*models.PhotoAlbumShare, error) {
	if !shareLevel.IsValid() {
		return nil, ErrInvalidArguments
	}
	found, err := self.shareToManage(userId, photoAlbumShareId)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoAlbumShareModel{}).Where("id = ?", found.ID).
		Updates(map[string]any{"share_level": string(shareLevel), "is_location_included": isLocationIncluded}).Error; err != nil {
		return nil, err
	}
	found.ShareLevel, found.IsLocationIncluded = string(shareLevel), isLocationIncluded
	return found.toModel(), nil
}

func (self *transaction) DeletePhotoAlbumShare(userId, photoAlbumShareId string) error {
	found, err := self.shareToManage(userId, photoAlbumShareId)
	if err != nil {
		return err
	}
	if err := self.tx.Where("id = ?", found.ID).Delete(&photoAlbumShareModel{}).Error; err != nil {
		return err
	}
	return self.removeUnreachablePhotoAlbumItems([]string{found.PhotoAlbumID})
}

func (self *transaction) ListPhotoAlbumShares(userId, photoAlbumId string) ([]*models.PhotoAlbumShare, error) {
	access, err := self.GetPhotoAlbumAccess(userId, photoAlbumId)
	if err != nil {
		return nil, err
	}
	if !access.IsManager {
		return nil, ErrNotFound
	}
	var rows []photoAlbumShareModel
	if err := self.tx.Where("\"photo_album_id\" = ?", photoAlbumId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	userIds, circleIds := []string{}, []string{}
	for _, row := range rows {
		if row.GranteeUserID != nil {
			userIds = append(userIds, *row.GranteeUserID)
		}
		if row.GranteeCircleID != nil {
			circleIds = append(circleIds, *row.GranteeCircleID)
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", circleIds).Find(&circles).Error; err != nil {
			return nil, err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	shares := make([]*models.PhotoAlbumShare, 0, len(rows))
	for index := range rows {
		share := rows[index].toModel()
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUser, share.GranteeUsername, share.GranteeName = user, user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
		shares = append(shares, share)
	}
	return shares, nil
}

func (self *transaction) ListPhotoAlbumReaderUserIDs(photoAlbumId string) ([]string, error) {
	if photoAlbumId == "" {
		return []string{}, nil
	}
	var userIds []string
	if err := self.tx.Raw(`
		SELECT album.user_id FROM "photo_album" album WHERE album.id = @album AND album.user_id IS NOT NULL
		UNION
		SELECT member.user_id FROM "photo_album" album
		JOIN "circle_member" member ON member.circle_id = album.circle_id
		WHERE album.id = @album
		UNION
		SELECT share.grantee_user_id FROM "photo_album_share" share
		JOIN "photo_album" album ON album.id = share.photo_album_id
		WHERE share.photo_album_id = @album AND share.grantee_user_id IS NOT NULL AND album.user_id IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM "circle_member" mine
		      JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		      WHERE mine.user_id = share.grantee_user_id AND theirs.user_id = album.user_id)
		UNION
		SELECT member.user_id FROM "photo_album_share" share
		JOIN "photo_album" album ON album.id = share.photo_album_id
		JOIN "circle_member" member ON member.circle_id = share.grantee_circle_id
		WHERE share.photo_album_id = @album AND album.user_id IS NOT NULL
		  AND EXISTS (SELECT 1 FROM "circle_member" owner WHERE owner.circle_id = share.grantee_circle_id AND owner.user_id = album.user_id)
		ORDER BY 1`, sql.Named("album", photoAlbumId)).Scan(&userIds).Error; err != nil {
		return nil, err
	}
	if userIds == nil {
		userIds = []string{}
	}
	return userIds, nil
}

// removeUnreachablePhotoAlbumItems takes out of these albums the photos added
// by people who no longer reach the album at all: the album was unshared from
// them, or they or its owner left the circle it was shared through. Somebody
// who still reaches the album, even at a lower level, keeps their photos in
// it. It is the one place that decides whose photos stay in an album, and
// every change to who reaches an album ends in it.
func (self *transaction) removeUnreachablePhotoAlbumItems(photoAlbumIds []string) error {
	for _, photoAlbumId := range photoAlbumIds {
		var adderUserIds []string
		if err := self.tx.Model(&photoAlbumItemModel{}).Where("photo_album_id = ?", photoAlbumId).Distinct().Pluck("added_by_user_id", &adderUserIds).Error; err != nil {
			return err
		}
		slices.Sort(adderUserIds)
		var goneUserIds []string
		for _, adderUserId := range adderUserIds {
			reach, err := self.photoAlbumReach(adderUserId, photoAlbumId)
			if err != nil {
				return err
			}
			if reach[photoAlbumId] == nil {
				goneUserIds = append(goneUserIds, adderUserId)
			}
		}
		if len(goneUserIds) == 0 {
			continue
		}
		var photoIds []string
		if err := self.tx.Model(&photoAlbumItemModel{}).Where("photo_album_id = ? AND added_by_user_id IN ?", photoAlbumId, goneUserIds).Pluck("photo_id", &photoIds).Error; err != nil {
			return err
		}
		if err := self.tx.Where("photo_album_id = ? AND added_by_user_id IN ?", photoAlbumId, goneUserIds).Delete(&photoAlbumItemModel{}).Error; err != nil {
			return err
		}
		if err := self.afterPhotosLeaveAlbum(photoAlbumId, photoIds); err != nil {
			return err
		}
	}
	return nil
}

// releasePhotoAlbumsOutsideCircles ends, for one person, what only a circle
// made possible once they no longer have it, in the transaction that took
// them out of it; the circle code calls it (endSharesOutsideCircles) beside
// the same cleanup of calendars, pages and skills.
//
// Three things are left over when somebody leaves a circle. A direct share
// between two people made possible by the circle they shared, which would
// reach them again, unchosen, the day they met in another circle. A share the
// person made to a circle they are no longer in. And the photos they put in
// albums they now reach no more, and the photos other people put in their
// albums through a share that no longer reaches them. A share to a circle
// that somebody else left stops reaching that person by the join in
// photoAlbumReachQuery and needs no deleting; the photos are found by asking
// who still reaches each album, which holds for a circle that was deleted as
// well, and so needs no circle named.
func releasePhotoAlbumsOutsideCircles(tx *gorm.DB, userId string) error {
	if err := tx.Exec(`DELETE FROM "photo_album_share" WHERE "id" IN (
		SELECT share.id FROM "photo_album_share" share
		JOIN "photo_album" album ON album.id = share.photo_album_id
		WHERE share.grantee_user_id IS NOT NULL AND album.user_id IS NOT NULL
		  AND (album.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = album.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "photo_album_share" share
		JOIN "photo_album" album ON album.id = share.photo_album_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND album.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error; err != nil {
		return err
	}
	var photoAlbumIds []string
	if err := tx.Raw(`SELECT DISTINCT item.photo_album_id FROM "photo_album_item" item
		JOIN "photo_album" album ON album.id = item.photo_album_id
		WHERE item.added_by_user_id = @user OR album.user_id = @user
		ORDER BY 1`, sql.Named("user", userId)).Scan(&photoAlbumIds).Error; err != nil {
		return err
	}
	return (&transaction{tx: tx}).removeUnreachablePhotoAlbumItems(photoAlbumIds)
}
