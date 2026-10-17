package db

import (
	"slices"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// PhotoFaceOperation owns the faces found in photos and the groups they are
// put in: one group for each person the server believes it has seen. Every
// query is scoped by the owner, so a face or a group that is somebody else's
// is not found.
type PhotoFaceOperation interface {
	// IsPhotoFaceRecognitionEnabled is the person's own switch for having
	// faces found in their photos; on until they turn it off.
	IsPhotoFaceRecognitionEnabled(userId string) (bool, error)

	// SetPhotoFaceRecognition switches the person's faces on or off. Off
	// deletes every face and group they have and forgets that any photo was
	// read, in this transaction, so that turning it on again reads the
	// photos again.
	SetPhotoFaceRecognition(userId string, isEnabled bool) error

	// ListPhotosWaitingForFaces lists up to limit ready photos, oldest first,
	// that have a large preview and whose faces have not been found, of
	// people who have faces switched on. The photos named are left out.
	ListPhotosWaitingForFaces(limit int, skippedPhotoIds []string) ([]*models.Photo, error)

	// LockPhotoFaceGrouping takes the lock that makes one person's grouping
	// happen one photo after another, even on two instances, until the
	// transaction ends.
	LockPhotoFaceGrouping(userId string) error

	// StorePhotoFaces keeps the faces found in a photo and records that they
	// were found; the photos found again are those whose pixels changed, which
	// is when the record is cleared. An old face the person confirmed lives
	// on in the new face found at about the same place, with its group. The
	// faces come back as stored, in the order given, with their vectors. The
	// photo is not found when it is not the person's or is being deleted, and
	// ErrAlreadyExists says another instance found its faces first.
	StorePhotoFaces(userId, photoId string, found []*models.PhotoFace) ([]*models.PhotoFace, error)

	// ListPhotoFaceGroupVectors lists the person's groups that have faces,
	// with the vectors a face is compared with, the hidden ones too: a hidden
	// group goes on taking its person's faces.
	ListPhotoFaceGroupVectors(userId string) ([]*PhotoFaceGroupVector, error)

	// StartPhotoFaceGroup makes a group of one face and says its identifier.
	StartPhotoFaceGroup(userId, photoFaceId string) (string, error)

	// JoinPhotoFaceGroup puts a face that has no group in a group, adding
	// its vector to the group's.
	JoinPhotoFaceGroup(userId, photoFaceId, photoFaceGroupId string) error

	// DeleteEmptyPhotoFaceGroups deletes groups that no face is in, which
	// are left behind when photos are deleted forever, and says how many.
	DeleteEmptyPhotoFaceGroups() (int, error)

	// ListPhotoFaceGroups lists the person's groups worth showing: those a
	// contact names, first by the contact's name, and the rest with at least
	// MinimumUnnamedPhotoFaceGroupFaceCount faces, the largest first. Hidden
	// groups are left out unless asked for.
	ListPhotoFaceGroups(userId string, isHiddenIncluded bool) ([]*models.PhotoFaceGroup, error)
	GetPhotoFaceGroup(userId, photoFaceGroupId string) (*models.PhotoFaceGroup, error)

	// ListPhotoFaces lists the faces of photos, each with the contact its
	// group is named for.
	ListPhotoFaces(userId string, photoIds []string) ([]*models.PhotoFace, error)
	GetPhotoFace(userId, photoFaceId string) (*models.PhotoFace, error)

	// NamePhotoFaceGroup names a group by a contact, which must be in one of
	// the person's own address books. Another contact replaces the one it had.
	NamePhotoFaceGroup(userId, photoFaceGroupId, addressBookId, contactId string) (*models.PhotoFaceGroup, error)
	UnnamePhotoFaceGroup(userId, photoFaceGroupId string) (*models.PhotoFaceGroup, error)

	// MergePhotoFaceGroups moves the faces of the other groups into the
	// first and deletes them. The first keeps its own contact; when it has
	// none it takes the contact of the first other group that has one.
	MergePhotoFaceGroups(userId string, photoFaceGroupIds []string) (*models.PhotoFaceGroup, error)

	// RemovePhotoFaceFromGroup takes a face out of its group into a group of
	// its own, which the person has confirmed so that it is not grouped again.
	RemovePhotoFaceFromGroup(userId, photoFaceId string) (*models.PhotoFaceGroup, error)

	HidePhotoFaceGroup(userId, photoFaceGroupId string, isHidden bool) (*models.PhotoFaceGroup, error)
}

// MinimumUnnamedPhotoFaceGroupFaceCount is how many faces a group nobody has
// named needs to be listed: a pair of faces is more often a chance likeness
// than a person.
const MinimumUnnamedPhotoFaceGroupFaceCount = 3

// MaximumMergedPhotoFaceGroups bounds how many groups one merge names.
const MaximumMergedPhotoFaceGroups = 50

// PhotoFaceGroupVector is a group as a face is compared with it.
type PhotoFaceGroupVector struct {
	ID string

	// VectorSum is the sum of the unit vectors of the faces in the group.
	// Its direction is the group's average direction, which is all a
	// similarity looks at, and a sum can take one more face without reading
	// the others.
	VectorSum []float32
}

type photoFaceModel struct {
	ID                string          `gorm:"column:id;primaryKey"`
	PhotoID           string          `gorm:"column:photo_id"`
	UserID            string          `gorm:"column:user_id"`
	BoxLeftFraction   float64         `gorm:"column:box_left_fraction"`
	BoxTopFraction    float64         `gorm:"column:box_top_fraction"`
	BoxWidthFraction  float64         `gorm:"column:box_width_fraction"`
	BoxHeightFraction float64         `gorm:"column:box_height_fraction"`
	DetectionScore    float64         `gorm:"column:detection_score"`
	FaceVector        pq.Float32Array `gorm:"column:face_vector;type:real[]"`
	PhotoFaceGroupID  *string         `gorm:"column:photo_face_group_id"`
	IsGroupConfirmed  bool            `gorm:"column:is_group_confirmed"`
	CreatedAt         time.Time       `gorm:"column:created_at"`
}

func (photoFaceModel) TableName() string { return "photo_face" }

func photoFaceFromModel(row *photoFaceModel) *models.PhotoFace {
	face := &models.PhotoFace{
		ID: row.ID, PhotoID: row.PhotoID, UserID: row.UserID, CreatedAt: row.CreatedAt.UTC(),
		BoxLeftFraction: row.BoxLeftFraction, BoxTopFraction: row.BoxTopFraction,
		BoxWidthFraction: row.BoxWidthFraction, BoxHeightFraction: row.BoxHeightFraction,
		DetectionScore: row.DetectionScore, FaceVector: slices.Clone([]float32(row.FaceVector)), IsGroupConfirmed: row.IsGroupConfirmed,
	}
	if row.PhotoFaceGroupID != nil {
		face.PhotoFaceGroupID = *row.PhotoFaceGroupID
	}
	return face
}

type photoFaceGroupModel struct {
	ID                   string          `gorm:"column:id;primaryKey"`
	UserID               string          `gorm:"column:user_id"`
	AddressBookID        *string         `gorm:"column:addressbook_id"`
	ContactID            *string         `gorm:"column:contact_id"`
	CoverPhotoFaceID     *string         `gorm:"column:cover_photo_face_id"`
	GroupVectorSum       pq.Float32Array `gorm:"column:group_vector_sum;type:real[]"`
	FaceGroupVectorCount int             `gorm:"column:face_group_vector_count"`
	IsHidden             bool            `gorm:"column:is_hidden"`
	CreatedAt            time.Time       `gorm:"column:created_at"`
	UpdatedAt            time.Time       `gorm:"column:updated_at"`
}

func (photoFaceGroupModel) TableName() string { return "photo_face_group" }

// photoFaceGroupRow is a group with what is worked out about its faces.
type photoFaceGroupRow struct {
	ID               string
	UserID           string
	AddressBookID    string
	ContactID        string
	ContactName      string
	IsHidden         bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
	FaceCount        int
	PhotoCount       int
	CoverPhotoFaceID string
}

func photoFaceGroupFromRow(row *photoFaceGroupRow) *models.PhotoFaceGroup {
	return &models.PhotoFaceGroup{
		ID: row.ID, UserID: row.UserID, CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
		ContactID: row.ContactID, ContactAddressBookID: row.AddressBookID, ContactName: row.ContactName,
		FaceCount: row.FaceCount, PhotoCount: row.PhotoCount, IsHidden: row.IsHidden, CoverPhotoFaceID: row.CoverPhotoFaceID,
	}
}

// photoFaceGroupsQuery selects the person's groups with what is worked out
// about their faces: those in photos that are not in the trash (a photo being
// deleted is in the trash as far as this is concerned). A group is shown by
// the face it was given as its cover while that face can be shown, else by its
// largest, surest one.
const photoFaceGroupsQuery = `
	SELECT photo_face_group.id, photo_face_group.user_id,
		COALESCE(photo_face_group.addressbook_id, '') AS address_book_id, COALESCE(photo_face_group.contact_id, '') AS contact_id,
		COALESCE(contact.name, '') AS contact_name, photo_face_group.is_hidden, photo_face_group.created_at, photo_face_group.updated_at,
		counts.face_count, counts.photo_count,
		COALESCE(
			(SELECT shown.id FROM photo_face shown JOIN photo ON photo.id = shown.photo_id
				WHERE shown.id = photo_face_group.cover_photo_face_id AND shown.photo_face_group_id = photo_face_group.id AND photo.trashed_at IS NULL),
			(SELECT best.id FROM photo_face best JOIN photo ON photo.id = best.photo_id
				WHERE best.photo_face_group_id = photo_face_group.id AND photo.trashed_at IS NULL
				ORDER BY best.detection_score * best.box_width_fraction * best.box_height_fraction DESC, best.id LIMIT 1),
			'') AS cover_photo_face_id
	FROM photo_face_group
	LEFT JOIN contact ON contact.addressbook_id = photo_face_group.addressbook_id AND contact.id = photo_face_group.contact_id
	CROSS JOIN LATERAL (
		SELECT count(*) AS face_count, count(DISTINCT photo_face.photo_id) AS photo_count
		FROM photo_face JOIN photo ON photo.id = photo_face.photo_id
		WHERE photo_face.photo_face_group_id = photo_face_group.id AND photo.trashed_at IS NULL
	) counts
	WHERE photo_face_group.user_id = ?`

// photoFaceNamesSearchExpression is the names of the contacts of the groups
// the faces of the photo are in, for the photo's search document, so that
// searching for a person's name finds the photos they are in.
const photoFaceNamesSearchExpression = `
	setweight(to_tsvector('simple', COALESCE((
		SELECT string_agg(DISTINCT contact.name, ' ')
		FROM photo_face
		JOIN photo_face_group ON photo_face_group.id = photo_face.photo_face_group_id
		JOIN contact ON contact.addressbook_id = photo_face_group.addressbook_id AND contact.id = photo_face_group.contact_id
		WHERE photo_face.photo_id = photo.id), '')), 'A')`

func (self *transaction) IsPhotoFaceRecognitionEnabled(userId string) (bool, error) {
	if userId == "" {
		return false, ErrNotFound
	}
	var rows []bool
	if err := self.tx.Raw(`SELECT is_photo_face_recognition_enabled FROM "user" WHERE id = ?`, userId).Scan(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, ErrNotFound
	}
	return rows[0], nil
}

func (self *transaction) SetPhotoFaceRecognition(userId string, isEnabled bool) error {
	if userId == "" {
		return ErrNotFound
	}
	if err := self.LockPhotoFaceGrouping(userId); err != nil {
		return err
	}
	changed := self.tx.Exec(`UPDATE "user" SET is_photo_face_recognition_enabled = ? WHERE id = ?`, isEnabled, userId)
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected == 0 {
		return ErrNotFound
	}
	if isEnabled {
		// The photos wait with no faces found already, from before the switch
		// was turned off or from while it was.
		return nil
	}
	// The photos whose search document holds a name from a face are named
	// before the faces go, and made again after.
	var namedPhotoIds []string
	if err := self.tx.Raw(`SELECT DISTINCT photo_face.photo_id FROM photo_face
		JOIN photo_face_group ON photo_face_group.id = photo_face.photo_face_group_id
		WHERE photo_face.user_id = ? AND photo_face_group.contact_id IS NOT NULL`, userId).Scan(&namedPhotoIds).Error; err != nil {
		return err
	}
	// The faces go first and then their groups, so that no face is set to
	// no group on its way out.
	if err := self.tx.Where("user_id = ?", userId).Delete(&photoFaceModel{}).Error; err != nil {
		return err
	}
	if err := self.tx.Where("user_id = ?", userId).Delete(&photoFaceGroupModel{}).Error; err != nil {
		return err
	}
	if err := self.tx.Model(&photoModel{}).Where("user_id = ? AND faces_found_at IS NOT NULL", userId).Update("faces_found_at", nil).Error; err != nil {
		return err
	}
	return self.refreshPhotoSearchDocumentsOf(namedPhotoIds)
}

func (self *transaction) ListPhotosWaitingForFaces(limit int, skippedPhotoIds []string) ([]*models.Photo, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	if skippedPhotoIds == nil {
		skippedPhotoIds = []string{}
	}
	// Person by person, so that one person with a large library and faces
	// switched off costs nothing, and the partial index answers each.
	var rows []photoModel
	if err := self.tx.Raw(`
		SELECT waiting.* FROM "user"
		CROSS JOIN LATERAL (
			SELECT photo.* FROM photo
			WHERE photo.user_id = "user".id AND photo.photo_processing_state = ? AND photo.faces_found_at IS NULL AND photo.trashed_at IS NULL
				AND photo.id <> ALL(?)
				AND EXISTS (SELECT 1 FROM photo_preview WHERE photo_preview.photo_id = photo.id AND photo_preview.photo_preview_size = ?)
			ORDER BY photo.created_at, photo.id LIMIT ?
		) waiting
		WHERE "user".is_photo_face_recognition_enabled
		ORDER BY waiting.created_at, waiting.id LIMIT ?`,
		models.PhotoProcessingReady, pq.Array(skippedPhotoIds), models.PhotoPreviewLarge, limit, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return photosFromModels(rows), nil
}

func (self *transaction) LockPhotoFaceGrouping(userId string) error {
	if userId == "" {
		return ErrNotFound
	}
	// A lock on a key made of the person, not on a row: the groups are many
	// rows, and a row lock on the person's account is taken by the upload of
	// every file. Two people whose keys collide only wait for each other.
	return self.tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "photo-face-grouping:"+userId).Error
}

// boxOverlap is how much of two boxes, in fractions of the same picture, is
// shared: the area they have in common over the area either covers.
func boxOverlap(first, second *models.PhotoFace) float64 {
	left := max(first.BoxLeftFraction, second.BoxLeftFraction)
	top := max(first.BoxTopFraction, second.BoxTopFraction)
	right := min(first.BoxLeftFraction+first.BoxWidthFraction, second.BoxLeftFraction+second.BoxWidthFraction)
	bottom := min(first.BoxTopFraction+first.BoxHeightFraction, second.BoxTopFraction+second.BoxHeightFraction)
	if right <= left || bottom <= top {
		return 0
	}
	common := (right - left) * (bottom - top)
	union := first.BoxWidthFraction*first.BoxHeightFraction + second.BoxWidthFraction*second.BoxHeightFraction - common
	if union <= 0 {
		return 0
	}
	return common / union
}

// minimumConfirmedBoxOverlap is how much a new box must share with the box of
// a face the person confirmed for the new face to be that face again.
const minimumConfirmedBoxOverlap = 0.5

func (self *transaction) StorePhotoFaces(userId, photoId string, found []*models.PhotoFace) ([]*models.PhotoFace, error) {
	if userId == "" || photoId == "" || len(found) > 5000 {
		return nil, ErrInvalidArguments
	}
	for _, face := range found {
		if face == nil || len(face.FaceVector) == 0 || face.BoxWidthFraction < 0 || face.BoxHeightFraction < 0 {
			return nil, ErrInvalidArguments
		}
	}
	// The photo is locked against being marked for deletion while its faces
	// are written, so that faces are not written for a photo cleanup has
	// already counted.
	var owned photoModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, photoId, userId, photoMarkedForDeletion), &owned)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	if owned.FacesFoundAt != nil {
		return nil, ErrAlreadyExists
	}
	var oldRows []photoFaceModel
	if err := self.tx.Where("photo_id = ?", photoId).Find(&oldRows).Error; err != nil {
		return nil, err
	}
	affectedGroupIds := []string{}
	namesChanged := false
	stored := make([]*models.PhotoFace, 0, len(found))
	rows := make([]photoFaceModel, 0, len(found))
	isOldKept := make([]bool, len(oldRows))
	createdAt := photoNow()
	for _, face := range found {
		row := photoFaceModel{
			ID: newID(), PhotoID: photoId, UserID: userId,
			BoxLeftFraction: face.BoxLeftFraction, BoxTopFraction: face.BoxTopFraction,
			BoxWidthFraction: face.BoxWidthFraction, BoxHeightFraction: face.BoxHeightFraction,
			DetectionScore: face.DetectionScore, FaceVector: pq.Float32Array(face.FaceVector), CreatedAt: createdAt,
		}
		bestOverlap, bestIndex := 0.0, -1
		for index := range oldRows {
			if isOldKept[index] || !oldRows[index].IsGroupConfirmed || oldRows[index].PhotoFaceGroupID == nil {
				continue
			}
			old := photoFaceFromModel(&oldRows[index])
			if overlap := boxOverlap(face, old); overlap >= minimumConfirmedBoxOverlap && overlap > bestOverlap {
				bestOverlap, bestIndex = overlap, index
			}
		}
		if bestIndex >= 0 {
			isOldKept[bestIndex] = true
			row.PhotoFaceGroupID, row.IsGroupConfirmed = oldRows[bestIndex].PhotoFaceGroupID, true
		}
		rows = append(rows, row)
	}
	for index := range oldRows {
		if oldRows[index].PhotoFaceGroupID != nil && !slices.Contains(affectedGroupIds, *oldRows[index].PhotoFaceGroupID) {
			affectedGroupIds = append(affectedGroupIds, *oldRows[index].PhotoFaceGroupID)
		}
	}
	if len(oldRows) > 0 {
		namesChanged = len(affectedGroupIds) > 0
		if err := self.tx.Where("photo_id = ?", photoId).Delete(&photoFaceModel{}).Error; err != nil {
			return nil, err
		}
	}
	if len(rows) > 0 {
		if err := self.tx.Create(&rows).Error; err != nil {
			return nil, err
		}
	}
	for _, groupId := range affectedGroupIds {
		if err := self.recomputePhotoFaceGroup(groupId); err != nil {
			return nil, err
		}
	}
	if err := self.tx.Model(&photoModel{}).Where("id = ?", photoId).Update("faces_found_at", photoNow()).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		stored = append(stored, photoFaceFromModel(&rows[index]))
	}
	if namesChanged {
		if err := self.refreshPhotoSearchDocumentsOf([]string{photoId}); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

// recomputePhotoFaceGroup makes a group's vector and count again from the
// faces that are in it, for when faces have left it. A group with no faces
// keeps the vector it had.
func (self *transaction) recomputePhotoFaceGroup(photoFaceGroupId string) error {
	return self.tx.Exec(`
		UPDATE photo_face_group SET
			group_vector_sum = COALESCE((
				SELECT array_agg(sums.total ORDER BY sums.position) FROM (
					SELECT numbers.position, sum(numbers.number) AS total
					FROM photo_face, unnest(photo_face.face_vector) WITH ORDINALITY AS numbers(number, position)
					WHERE photo_face.photo_face_group_id = photo_face_group.id
					GROUP BY numbers.position
				) sums), photo_face_group.group_vector_sum),
			face_group_vector_count = (SELECT count(*) FROM photo_face WHERE photo_face.photo_face_group_id = photo_face_group.id),
			updated_at = ?
		WHERE id = ?`, photoNow(), photoFaceGroupId).Error
}

func (self *transaction) ListPhotoFaceGroupVectors(userId string) ([]*PhotoFaceGroupVector, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var rows []photoFaceGroupModel
	if err := self.tx.Select("id", "group_vector_sum").Where("user_id = ? AND face_group_vector_count > 0", userId).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	vectors := make([]*PhotoFaceGroupVector, 0, len(rows))
	for index := range rows {
		vectors = append(vectors, &PhotoFaceGroupVector{ID: rows[index].ID, VectorSum: slices.Clone([]float32(rows[index].GroupVectorSum))})
	}
	return vectors, nil
}

// lockPhotoFace finds a face of the person's, with the photo it is in not
// being deleted, and locks it.
func (self *transaction) lockPhotoFace(userId, photoFaceId string) (*photoFaceModel, error) {
	if userId == "" || photoFaceId == "" {
		return nil, ErrNotFound
	}
	var row photoFaceModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "photo_face"}}).
		Joins("JOIN photo ON photo.id = photo_face.photo_id").
		Where("photo_face.id = ? AND photo_face.user_id = ? AND "+photoIsReadable, photoFaceId, userId, photoMarkedForDeletion), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &row, nil
}

// photoFaceQualityExpression is how well a face stands for its group: the
// surer and the larger, the better.
const photoFaceQualityExpression = "detection_score * box_width_fraction * box_height_fraction"

func (self *transaction) StartPhotoFaceGroup(userId, photoFaceId string) (string, error) {
	face, err := self.lockPhotoFace(userId, photoFaceId)
	if err != nil {
		return "", err
	}
	if face.PhotoFaceGroupID != nil {
		return "", ErrInvalidArguments
	}
	created := photoNow()
	row := photoFaceGroupModel{
		ID: newID(), UserID: userId, CoverPhotoFaceID: &face.ID, GroupVectorSum: face.FaceVector, FaceGroupVectorCount: 1,
		CreatedAt: created, UpdatedAt: created,
	}
	if err := self.tx.Create(&row).Error; err != nil {
		return "", err
	}
	if err := self.tx.Model(&photoFaceModel{}).Where("id = ?", face.ID).Update("photo_face_group_id", row.ID).Error; err != nil {
		return "", err
	}
	return row.ID, nil
}

func (self *transaction) JoinPhotoFaceGroup(userId, photoFaceId, photoFaceGroupId string) error {
	face, err := self.lockPhotoFace(userId, photoFaceId)
	if err != nil {
		return err
	}
	if face.PhotoFaceGroupID != nil {
		return ErrInvalidArguments
	}
	var group photoFaceGroupModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", photoFaceGroupId, userId), &group)
	if err != nil {
		return err
	}
	if !isFound {
		return ErrNotFound
	}
	if len(group.GroupVectorSum) != len(face.FaceVector) {
		return ErrInvalidArguments
	}
	if err := self.tx.Model(&photoFaceModel{}).Where("id = ?", face.ID).Update("photo_face_group_id", group.ID).Error; err != nil {
		return err
	}
	// A better face than the cover shows the group better.
	if err := self.tx.Exec(`
		UPDATE photo_face_group SET
			group_vector_sum = (
				SELECT array_agg(sums.number + added.number ORDER BY sums.position)
				FROM unnest(photo_face_group.group_vector_sum) WITH ORDINALITY AS sums(number, position)
				JOIN unnest(CAST(? AS real[])) WITH ORDINALITY AS added(number, position) ON added.position = sums.position),
			face_group_vector_count = face_group_vector_count + 1,
			cover_photo_face_id = CASE WHEN cover_photo_face_id IS NULL OR (
				SELECT `+photoFaceQualityExpression+` FROM photo_face WHERE id = photo_face_group.cover_photo_face_id) < ?
				THEN ? ELSE cover_photo_face_id END,
			updated_at = ?
		WHERE id = ?`,
		pq.Float32Array(face.FaceVector), face.DetectionScore*face.BoxWidthFraction*face.BoxHeightFraction, face.ID, photoNow(), group.ID).Error; err != nil {
		return err
	}
	if group.ContactID == nil {
		return nil
	}
	// The photo is now one of the photos of a person who has a name.
	return self.refreshPhotoSearchDocumentsOf([]string{face.PhotoID})
}

func (self *transaction) DeleteEmptyPhotoFaceGroups() (int, error) {
	deleted := self.tx.Exec(`DELETE FROM photo_face_group WHERE NOT EXISTS (SELECT 1 FROM photo_face WHERE photo_face.photo_face_group_id = photo_face_group.id) AND updated_at < ?`, photoNow().Add(-time.Hour))
	return int(deleted.RowsAffected), deleted.Error
}

func (self *transaction) ListPhotoFaceGroups(userId string, isHiddenIncluded bool) ([]*models.PhotoFaceGroup, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var rows []photoFaceGroupRow
	if err := self.tx.Raw(photoFaceGroupsQuery+`
		AND (? OR NOT photo_face_group.is_hidden)
		AND counts.face_count > 0 AND (photo_face_group.contact_id IS NOT NULL OR counts.face_count >= ?)
		ORDER BY (photo_face_group.contact_id IS NULL), lower(contact.name), counts.face_count DESC, photo_face_group.id`,
		userId, isHiddenIncluded, MinimumUnnamedPhotoFaceGroupFaceCount).Scan(&rows).Error; err != nil {
		return nil, err
	}
	groups := make([]*models.PhotoFaceGroup, 0, len(rows))
	for index := range rows {
		groups = append(groups, photoFaceGroupFromRow(&rows[index]))
	}
	return groups, nil
}

func (self *transaction) GetPhotoFaceGroup(userId, photoFaceGroupId string) (*models.PhotoFaceGroup, error) {
	if userId == "" || photoFaceGroupId == "" {
		return nil, ErrNotFound
	}
	var rows []photoFaceGroupRow
	if err := self.tx.Raw(photoFaceGroupsQuery+` AND photo_face_group.id = ?`, userId, photoFaceGroupId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return photoFaceGroupFromRow(&rows[0]), nil
}

// photoFaceRow is a face with the contact its group is named for. The fields
// are written out: gorm does not fill an embedded struct from a raw query.
type photoFaceRow struct {
	ID                string
	PhotoID           string
	UserID            string
	BoxLeftFraction   float64
	BoxTopFraction    float64
	BoxWidthFraction  float64
	BoxHeightFraction float64
	DetectionScore    float64
	PhotoFaceGroupID  *string
	IsGroupConfirmed  bool
	CreatedAt         time.Time
	ContactID         string
	ContactName       string
}

func photoFaceFromRow(row *photoFaceRow) *models.PhotoFace {
	face := photoFaceFromModel(&photoFaceModel{
		ID: row.ID, PhotoID: row.PhotoID, UserID: row.UserID, BoxLeftFraction: row.BoxLeftFraction, BoxTopFraction: row.BoxTopFraction,
		BoxWidthFraction: row.BoxWidthFraction, BoxHeightFraction: row.BoxHeightFraction, DetectionScore: row.DetectionScore,
		PhotoFaceGroupID: row.PhotoFaceGroupID, IsGroupConfirmed: row.IsGroupConfirmed, CreatedAt: row.CreatedAt,
	})
	face.ContactID, face.ContactName = row.ContactID, row.ContactName
	return face
}

const photoFacesQuery = `
	SELECT photo_face.id, photo_face.photo_id, photo_face.user_id, photo_face.box_left_fraction, photo_face.box_top_fraction,
		photo_face.box_width_fraction, photo_face.box_height_fraction, photo_face.detection_score, photo_face.photo_face_group_id,
		photo_face.is_group_confirmed, photo_face.created_at,
		COALESCE(photo_face_group.contact_id, '') AS contact_id, COALESCE(contact.name, '') AS contact_name
	FROM photo_face
	JOIN photo ON photo.id = photo_face.photo_id
	LEFT JOIN photo_face_group ON photo_face_group.id = photo_face.photo_face_group_id
	LEFT JOIN contact ON contact.addressbook_id = photo_face_group.addressbook_id AND contact.id = photo_face_group.contact_id
	WHERE photo.user_id = ? AND ` + photoIsReadable

func (self *transaction) ListPhotoFaces(userId string, photoIds []string) ([]*models.PhotoFace, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if len(photoIds) == 0 {
		return []*models.PhotoFace{}, nil
	}
	if len(photoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	var rows []photoFaceRow
	if err := self.tx.Raw(photoFacesQuery+` AND photo_face.photo_id IN ? ORDER BY photo_face.photo_id, photo_face.box_left_fraction, photo_face.id`,
		userId, photoMarkedForDeletion, photoIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	faces := make([]*models.PhotoFace, 0, len(rows))
	for index := range rows {
		faces = append(faces, photoFaceFromRow(&rows[index]))
	}
	return faces, nil
}

func (self *transaction) GetPhotoFace(userId, photoFaceId string) (*models.PhotoFace, error) {
	if userId == "" || photoFaceId == "" {
		return nil, ErrNotFound
	}
	var rows []photoFaceRow
	if err := self.tx.Raw(photoFacesQuery+` AND photo_face.id = ?`, userId, photoMarkedForDeletion, photoFaceId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return photoFaceFromRow(&rows[0]), nil
}

// lockPhotoFaceGroup finds a group of the person's and locks it, so that two
// changes to one group are made one after the other.
func (self *transaction) lockPhotoFaceGroup(userId, photoFaceGroupId string) (*photoFaceGroupModel, error) {
	if userId == "" || photoFaceGroupId == "" {
		return nil, ErrNotFound
	}
	var row photoFaceGroupModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", photoFaceGroupId, userId), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return &row, nil
}

// refreshSearchDocumentsOfPhotoFaceGroups makes the search document again of
// the photos that have a face in one of the groups, for when the name of a
// group changes.
func (self *transaction) refreshSearchDocumentsOfPhotoFaceGroups(photoFaceGroupIds []string) error {
	if len(photoFaceGroupIds) == 0 {
		return nil
	}
	return self.refreshPhotoSearchDocuments("photo.id IN (SELECT photo_id FROM photo_face WHERE photo_face_group_id = ANY(?))", pq.Array(photoFaceGroupIds))
}

func (self *transaction) NamePhotoFaceGroup(userId, photoFaceGroupId, addressBookId, contactId string) (*models.PhotoFaceGroup, error) {
	if addressBookId == "" || contactId == "" {
		return nil, ErrInvalidArguments
	}
	if _, err := self.lockPhotoFaceGroup(userId, photoFaceGroupId); err != nil {
		return nil, err
	}
	// The contact is in an address book that is the person's, or it is not
	// found: whose it is is not the caller's business.
	var contactCount int64
	if err := self.tx.Table("contact").Joins("JOIN addressbook ON addressbook.id = contact.addressbook_id").
		Where("contact.addressbook_id = ? AND contact.id = ? AND addressbook.user_id = ?", addressBookId, contactId, userId).Count(&contactCount).Error; err != nil {
		return nil, err
	}
	if contactCount == 0 {
		return nil, ErrNotFound
	}
	if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ?", photoFaceGroupId).
		Updates(map[string]any{"addressbook_id": addressBookId, "contact_id": contactId, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	if err := self.refreshSearchDocumentsOfPhotoFaceGroups([]string{photoFaceGroupId}); err != nil {
		return nil, err
	}
	return self.GetPhotoFaceGroup(userId, photoFaceGroupId)
}

func (self *transaction) UnnamePhotoFaceGroup(userId, photoFaceGroupId string) (*models.PhotoFaceGroup, error) {
	if _, err := self.lockPhotoFaceGroup(userId, photoFaceGroupId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ?", photoFaceGroupId).
		Updates(map[string]any{"addressbook_id": nil, "contact_id": nil, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	if err := self.refreshSearchDocumentsOfPhotoFaceGroups([]string{photoFaceGroupId}); err != nil {
		return nil, err
	}
	return self.GetPhotoFaceGroup(userId, photoFaceGroupId)
}

func (self *transaction) HidePhotoFaceGroup(userId, photoFaceGroupId string, isHidden bool) (*models.PhotoFaceGroup, error) {
	if _, err := self.lockPhotoFaceGroup(userId, photoFaceGroupId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ?", photoFaceGroupId).
		Updates(map[string]any{"is_hidden": isHidden, "updated_at": photoNow()}).Error; err != nil {
		return nil, err
	}
	return self.GetPhotoFaceGroup(userId, photoFaceGroupId)
}

func (self *transaction) MergePhotoFaceGroups(userId string, photoFaceGroupIds []string) (*models.PhotoFaceGroup, error) {
	distinctIds := make([]string, 0, len(photoFaceGroupIds))
	for _, photoFaceGroupId := range photoFaceGroupIds {
		if photoFaceGroupId == "" {
			return nil, ErrInvalidArguments
		}
		if !slices.Contains(distinctIds, photoFaceGroupId) {
			distinctIds = append(distinctIds, photoFaceGroupId)
		}
	}
	if len(distinctIds) < 2 || len(distinctIds) > MaximumMergedPhotoFaceGroups {
		return nil, ErrInvalidArguments
	}
	// Grouping a photo's faces could add one to a group that this deletes.
	if err := self.LockPhotoFaceGrouping(userId); err != nil {
		return nil, err
	}
	target, err := self.lockPhotoFaceGroup(userId, distinctIds[0])
	if err != nil {
		return nil, err
	}
	others := make([]*photoFaceGroupModel, 0, len(distinctIds)-1)
	for _, otherId := range distinctIds[1:] {
		other, err := self.lockPhotoFaceGroup(userId, otherId)
		if err != nil {
			return nil, err
		}
		others = append(others, other)
	}
	changes := map[string]any{"updated_at": photoNow()}
	if target.ContactID == nil {
		for _, other := range others {
			if other.ContactID != nil {
				changes["addressbook_id"], changes["contact_id"] = other.AddressBookID, other.ContactID
				break
			}
		}
	}
	if err := self.tx.Model(&photoFaceModel{}).Where("photo_face_group_id IN ?", distinctIds[1:]).Update("photo_face_group_id", target.ID).Error; err != nil {
		return nil, err
	}
	// The covers of the groups that go are faces of the group that stays now,
	// and the cover it has is kept.
	if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ?", target.ID).Updates(changes).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Where("id IN ?", distinctIds[1:]).Delete(&photoFaceGroupModel{}).Error; err != nil {
		return nil, err
	}
	if err := self.recomputePhotoFaceGroup(target.ID); err != nil {
		return nil, err
	}
	if err := self.refreshSearchDocumentsOfPhotoFaceGroups([]string{target.ID}); err != nil {
		return nil, err
	}
	return self.GetPhotoFaceGroup(userId, target.ID)
}

func (self *transaction) RemovePhotoFaceFromGroup(userId, photoFaceId string) (*models.PhotoFaceGroup, error) {
	if err := self.LockPhotoFaceGrouping(userId); err != nil {
		return nil, err
	}
	face, err := self.lockPhotoFace(userId, photoFaceId)
	if err != nil {
		return nil, err
	}
	oldGroupId := face.PhotoFaceGroupID
	if oldGroupId != nil {
		if err := self.tx.Model(&photoFaceModel{}).Where("id = ?", face.ID).Update("photo_face_group_id", nil).Error; err != nil {
			return nil, err
		}
	}
	face.PhotoFaceGroupID = nil
	created := photoNow()
	row := photoFaceGroupModel{
		ID: newID(), UserID: userId, CoverPhotoFaceID: &face.ID, GroupVectorSum: face.FaceVector, FaceGroupVectorCount: 1,
		CreatedAt: created, UpdatedAt: created,
	}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&photoFaceModel{}).Where("id = ?", face.ID).Updates(map[string]any{"photo_face_group_id": row.ID, "is_group_confirmed": true}).Error; err != nil {
		return nil, err
	}
	if oldGroupId != nil {
		// The group the face left may have had it as its cover; it is shown
		// by another face then.
		if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ? AND cover_photo_face_id = ?", *oldGroupId, face.ID).Update("cover_photo_face_id", nil).Error; err != nil {
			return nil, err
		}
		if err := self.recomputePhotoFaceGroup(*oldGroupId); err != nil {
			return nil, err
		}
	}
	if err := self.refreshPhotoSearchDocumentsOf([]string{face.PhotoID}); err != nil {
		return nil, err
	}
	return self.GetPhotoFaceGroup(userId, row.ID)
}

// photoFaceConditions narrows a listing of photos to those with a face in one
// group, or in any group a contact names, when the filter asks.
func (self *transaction) photoFaceConditions(query *gorm.DB, userId string, filter *PhotoFilter) (*gorm.DB, error) {
	if filter.FaceGroupID != "" {
		var groupCount int64
		if err := self.tx.Model(&photoFaceGroupModel{}).Where("id = ? AND user_id = ?", filter.FaceGroupID, userId).Count(&groupCount).Error; err != nil {
			return nil, err
		}
		if groupCount == 0 {
			return nil, ErrNotFound
		}
		query = query.Where("EXISTS (SELECT 1 FROM photo_face WHERE photo_face.photo_id = photo.id AND photo_face.photo_face_group_id = ?)", filter.FaceGroupID)
	}
	if filter.ContactID != "" {
		query = query.Where(`EXISTS (SELECT 1 FROM photo_face JOIN photo_face_group ON photo_face_group.id = photo_face.photo_face_group_id
			WHERE photo_face.photo_id = photo.id AND photo_face_group.user_id = ? AND photo_face_group.contact_id = ?)`, userId, filter.ContactID)
	}
	return query, nil
}
