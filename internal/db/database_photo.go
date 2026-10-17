package db

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrPhotoBlobGone is a file refused because the blob it names was deleted
	// between the bytes being stored and the file being recorded. The caller
	// stores the bytes again.
	ErrPhotoBlobGone = errors.New("db: the photo blob was deleted")
)

// PhotoOperation owns the library: photos, their files and previews, and the
// blobs the files name. Every query is scoped by the owner, so a photo that
// is somebody else's is not found.
type PhotoOperation interface {
	// CreatePhoto finds the photo with this uploader's name for it, or makes
	// it. The second value says it was made.
	CreatePhoto(photo *models.Photo) (*models.Photo, bool, error)
	GetPhoto(userId, photoId string) (*models.Photo, error)
	ListPhotos(userId string, filter *PhotoFilter) ([]*models.Photo, error)

	// CountPhotos is how many photos the filter keeps, whatever page it
	// asks for.
	CountPhotos(userId string, filter *PhotoFilter) (int, error)
	FindPhotosByClientIDs(userId string, clientPhotoIds []string) ([]*models.Photo, error)
	UpdatePhoto(userId, photoId string, update *PhotoUpdate) (*models.Photo, error)
	TrashPhotos(userId string, photoIds []string) (int, error)
	RestorePhotos(userId string, photoIds []string) (int, error)

	// ListTrashedPhotos lists photos already in the trash, those named or,
	// with none named, any, so a deletion can find their previews first.
	ListTrashedPhotos(userId string, photoIds []string, limit int) ([]*models.Photo, error)

	// DeletePhotos deletes forever the named photos that are in the trash and
	// says how many there were. Their blobs are deleted by cleanup.
	DeletePhotos(userId string, photoIds []string) (int, error)

	// ListPhotosTrashedBefore is the photos of every account that have been in
	// the trash since before the time, for the deletion that follows retention.
	ListPhotosTrashedBefore(trashedBefore time.Time, limit int) ([]*models.Photo, error)

	ReadPhotoLibraryCounts(userId string) (*PhotoLibraryCounts, error)

	// PhotoUsedBytes is the size of every file of every photo the account
	// has, those in the trash included.
	PhotoUsedBytes(userId string) (int64, error)

	// SetPhotoFile records a file of a photo, replacing the one in that role.
	// The size is checked against the account's limit, the photo is queued for
	// processing when the file is one its previews are made from, and the
	// first value says the file differs from the one it replaced.
	SetPhotoFile(userId, photoId string, file *models.PhotoFile, maximumAccountBytes int64) (bool, error)
	GetPhotoFile(userId, photoId string, photoFileRole models.PhotoFileRole) (*models.PhotoFile, error)
	ListPhotoFiles(userId string, photoIds []string) ([]*models.PhotoFile, error)

	GetPhotoBlob(blobSha256 string) (*models.PhotoBlob, error)

	// InsertPhotoBlobIfAbsent records a blob whose bytes are stored. A blob
	// marked for deletion is unmarked. The value says a row was made, as
	// opposed to found.
	InsertPhotoBlobIfAbsent(blob *models.PhotoBlob) (bool, error)

	// MarkUnreferencedPhotoBlobDeleting finds a blob created before the time
	// that no file names, and marks it. The bytes are deleted by a second
	// transaction, so that a crash between leaves a mark and not a row whose
	// bytes are gone.
	MarkUnreferencedPhotoBlobDeleting(createdBefore time.Time) (*models.PhotoBlob, error)

	// LockPhotoBlobForDeletion locks a marked blob and checks again that no
	// file names it. A blob some file names after all comes back as nil with
	// its mark cleared; that is not an error, because an error would roll the
	// clearing back.
	LockPhotoBlobForDeletion(blobSha256 string) (*models.PhotoBlob, error)
	DeletePhotoBlob(blobSha256 string) error

	UpsertPhotoPreview(userId string, preview *models.PhotoPreview) (*models.PhotoPreview, error)
	GetPhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) (*models.PhotoPreview, error)
	ListPhotoPreviews(userId string, photoIds []string) ([]*models.PhotoPreview, error)
	DeletePhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) error

	// ClaimPhotosForProcessing takes up to limit photos that wait for their
	// previews: those nobody has claimed, and those whose claim is older than
	// the time, which the instance that made it did not finish.
	ClaimPhotosForProcessing(limit int, staleBefore time.Time) ([]*models.Photo, error)
	MarkPhotoReady(photoId string) error

	// MarkPhotoProcessingFailed counts one failure. After the maximum, the
	// photo is failed and stays so; before it, the photo waits to be tried
	// again.
	MarkPhotoProcessingFailed(photoId, errorMessage string, maximumAttemptCount int) error
}

// PhotoFilter narrows a page of the library.
type PhotoFilter struct {
	// BeforePhotoID continues after the last photo of the previous page, and
	// Offset skips that many photos instead, for a page reached by number.
	// Not both.
	BeforePhotoID string
	Offset        int

	PageSize int

	// IsFavorite keeps favorites when true and the rest when false.
	IsFavorite *bool

	// IsTrashed lists the trash instead of the library.
	IsTrashed bool

	PhotoKind models.PhotoKind

	// TakenAfter and TakenBefore bound the date a photo is placed at: when it
	// was taken, or when it arrived if nothing says when it was taken.
	TakenAfter  *time.Time
	TakenBefore *time.Time

	// SearchText keeps photos where every word is in the place name, the file
	// name or the camera.
	SearchText string

	// NearLatitude, NearLongitude and NearDistanceKilometers keep photos
	// taken within that distance of a point. All three or none.
	NearLatitude           *float64
	NearLongitude          *float64
	NearDistanceKilometers *float64
}

// PhotoUpdate is what a person may correct about a photo. Fields left nil
// are kept.
type PhotoUpdate struct {
	IsFavorite *bool
	TakenAt    *time.Time
}

// PhotoLibraryCounts are the totals a library page shows.
type PhotoLibraryCounts struct {
	PhotoCount    int64
	VideoCount    int64
	FavoriteCount int64
	TrashedCount  int64
}

type photoModel models.Photo

func (photoModel) TableName() string { return "photo" }

type photoBlobModel models.PhotoBlob

func (photoBlobModel) TableName() string { return "photo_blob" }

type photoFileModel models.PhotoFile

func (photoFileModel) TableName() string { return "photo_file" }

type photoPreviewModel models.PhotoPreview

func (photoPreviewModel) TableName() string { return "photo_preview" }

func photoFromModel(row *photoModel) *models.Photo {
	photo := models.Photo(*row)
	photo.CreatedAt = photo.CreatedAt.UTC()
	photo.UpdatedAt = photo.UpdatedAt.UTC()
	photo.TakenAt = utcTime(photo.TakenAt)
	photo.TrashedAt = utcTime(photo.TrashedAt)
	photo.ProcessingClaimedAt = utcTime(photo.ProcessingClaimedAt)
	return &photo
}

func photoFileFromModel(row *photoFileModel) *models.PhotoFile {
	file := models.PhotoFile(*row)
	file.CreatedAt = file.CreatedAt.UTC()
	return &file
}

func photoPreviewFromModel(row *photoPreviewModel) *models.PhotoPreview {
	preview := models.PhotoPreview(*row)
	preview.CreatedAt = preview.CreatedAt.UTC()
	return &preview
}

func photoBlobFromModel(row *photoBlobModel) *models.PhotoBlob {
	blob := models.PhotoBlob(*row)
	blob.CreatedAt = blob.CreatedAt.UTC()
	blob.DeletingAt = utcTime(blob.DeletingAt)
	return &blob
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

// photoErrorMessageBytes bounds the failure text kept on a photo, which comes
// from a tool's output and can be long.
const photoErrorMessageBytes = 4096

// photoTimelineExpression is the date a photo sits at in the timeline.
const photoTimelineExpression = "COALESCE(photo.taken_at, photo.created_at)"

// photoNow is the time to store: UTC, to the microsecond PostgreSQL keeps.
func photoNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func (self *transaction) CreatePhoto(photo *models.Photo) (*models.Photo, bool, error) {
	if photo == nil || photo.UserID == "" || strings.TrimSpace(photo.ClientPhotoID) == "" || len(photo.ClientPhotoID) > 255 || !photo.PhotoKind.IsValid() || !photo.PhotoSource.IsValid() || len(photo.OriginalFilename) > 255 {
		return nil, false, ErrInvalidArguments
	}
	if (photo.Latitude != nil && (*photo.Latitude < -90 || *photo.Latitude > 90)) || (photo.Longitude != nil && (*photo.Longitude < -180 || *photo.Longitude > 180)) {
		return nil, false, ErrInvalidArguments
	}
	created := photoNow()
	row := photoModel(*photo)
	row.ID = newID()
	row.CreatedAt = created
	row.UpdatedAt = created
	row.PhotoProcessingState = models.PhotoProcessingWaiting
	row.ProcessingAttemptCount = 0
	row.ProcessingErrorMessage = ""
	row.ProcessingClaimedAt = nil
	row.TrashedAt = nil
	// Two requests with the same uploader name race to this insert; the
	// unique key decides, and the loser reads the winner's row.
	inserted := self.tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "client_photo_id"}}, DoNothing: true}).Create(&row)
	if inserted.Error != nil {
		return nil, false, inserted.Error
	}
	if inserted.RowsAffected == 1 {
		return photoFromModel(&row), true, nil
	}
	var existing photoModel
	if err := self.tx.Where("user_id = ? AND client_photo_id = ?", photo.UserID, photo.ClientPhotoID).Take(&existing).Error; err != nil {
		return nil, false, err
	}
	return photoFromModel(&existing), false, nil
}

func (self *transaction) GetPhoto(userId, photoId string) (*models.Photo, error) {
	if userId == "" || photoId == "" {
		return nil, ErrNotFound
	}
	var row photoModel
	if err := self.tx.Where("id = ? AND user_id = ?", photoId, userId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return photoFromModel(&row), nil
}

// maximumPhotoOffset bounds how far into the library a page is reached by
// skipping, because skipping reads every photo it skips.
const maximumPhotoOffset = 100000

// photosMatching is the photos the filter keeps, before any paging. Listing
// and counting both start here, so that they cannot disagree about what a
// filter means.
func (self *transaction) photosMatching(userId string, filter *PhotoFilter) (*gorm.DB, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if filter == nil {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Model(&photoModel{}).Where("photo.user_id = ?", userId)
	if filter.IsTrashed {
		query = query.Where("photo.trashed_at IS NOT NULL")
	} else {
		query = query.Where("photo.trashed_at IS NULL")
	}
	if filter.IsFavorite != nil {
		query = query.Where("photo.is_favorite = ?", *filter.IsFavorite)
	}
	if filter.PhotoKind != "" {
		if !filter.PhotoKind.IsValid() {
			return nil, ErrInvalidArguments
		}
		query = query.Where("photo.photo_kind = ?", filter.PhotoKind)
	}
	if filter.TakenAfter != nil {
		query = query.Where(photoTimelineExpression+" >= ?", *filter.TakenAfter)
	}
	if filter.TakenBefore != nil {
		query = query.Where(photoTimelineExpression+" < ?", *filter.TakenBefore)
	}
	if words := strings.Fields(filter.SearchText); len(words) > 0 {
		if len(words) > 8 {
			return nil, ErrInvalidArguments
		}
		for _, word := range words {
			if utf8.RuneCountInString(word) > 100 {
				return nil, ErrInvalidArguments
			}
			pattern := "%" + escapeLike(word) + "%"
			query = query.Where("(photo.place_name ILIKE ? OR photo.original_filename ILIKE ? OR photo.camera_make ILIKE ? OR photo.camera_model ILIKE ?)", pattern, pattern, pattern, pattern)
		}
	}
	hasNear := filter.NearLatitude != nil || filter.NearLongitude != nil || filter.NearDistanceKilometers != nil
	if hasNear {
		if filter.NearLatitude == nil || filter.NearLongitude == nil || filter.NearDistanceKilometers == nil || *filter.NearDistanceKilometers <= 0 ||
			*filter.NearLatitude < -90 || *filter.NearLatitude > 90 || *filter.NearLongitude < -180 || *filter.NearLongitude > 180 {
			return nil, ErrInvalidArguments
		}
		// The great-circle distance by the haversine formula, on a sphere of
		// the earth's mean radius: good to a fraction of a percent, which is
		// plenty for "photos near here".
		query = query.Where(`photo.latitude IS NOT NULL AND photo.longitude IS NOT NULL AND
			12742 * asin(sqrt(least(1, power(sin(radians(photo.latitude - ?) / 2), 2) +
				cos(radians(?)) * cos(radians(photo.latitude)) * power(sin(radians(photo.longitude - ?) / 2), 2)))) <= ?`,
			*filter.NearLatitude, *filter.NearLatitude, *filter.NearLongitude, *filter.NearDistanceKilometers)
	}
	return query, nil
}

func (self *transaction) ListPhotos(userId string, filter *PhotoFilter) ([]*models.Photo, error) {
	query, err := self.photosMatching(userId, filter)
	if err != nil {
		return nil, err
	}
	if filter.PageSize < 1 || filter.PageSize > 500 || filter.Offset < 0 || filter.Offset > maximumPhotoOffset || (filter.Offset > 0 && filter.BeforePhotoID != "") {
		return nil, ErrInvalidArguments
	}
	if filter.BeforePhotoID != "" {
		var cursor struct {
			PlacedAt time.Time
			ID       string
		}
		if err := self.tx.Model(&photoModel{}).Select(photoTimelineExpression+" AS placed_at, photo.id").Where("photo.id = ? AND photo.user_id = ?", filter.BeforePhotoID, userId).Take(&cursor).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		query = query.Where("("+photoTimelineExpression+", photo.id) < (?, ?)", cursor.PlacedAt, cursor.ID)
	}
	var rows []photoModel
	if err := query.Order(photoTimelineExpression + " DESC, photo.id DESC").Offset(filter.Offset).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return photosFromModels(rows), nil
}

func (self *transaction) CountPhotos(userId string, filter *PhotoFilter) (int, error) {
	query, err := self.photosMatching(userId, filter)
	if err != nil {
		return 0, err
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func photosFromModels(rows []photoModel) []*models.Photo {
	photos := make([]*models.Photo, 0, len(rows))
	for index := range rows {
		photos = append(photos, photoFromModel(&rows[index]))
	}
	return photos
}

func (self *transaction) FindPhotosByClientIDs(userId string, clientPhotoIds []string) ([]*models.Photo, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if len(clientPhotoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	var rows []photoModel
	if len(clientPhotoIds) > 0 {
		if err := self.tx.Where("user_id = ? AND client_photo_id IN ?", userId, clientPhotoIds).Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
	}
	return photosFromModels(rows), nil
}

func (self *transaction) UpdatePhoto(userId, photoId string, update *PhotoUpdate) (*models.Photo, error) {
	if update == nil {
		return nil, ErrInvalidArguments
	}
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return nil, err
	}
	changes := map[string]any{"updated_at": photoNow()}
	if update.IsFavorite != nil {
		changes["is_favorite"] = *update.IsFavorite
	}
	if update.TakenAt != nil {
		changes["taken_at"] = update.TakenAt.UTC()
	}
	if err := self.tx.Model(&photoModel{}).Where("id = ? AND user_id = ?", photoId, userId).Updates(changes).Error; err != nil {
		return nil, err
	}
	return self.GetPhoto(userId, photoId)
}

func (self *transaction) TrashPhotos(userId string, photoIds []string) (int, error) {
	return self.changePhotoTrash(userId, photoIds, "trashed_at IS NULL", map[string]any{"trashed_at": photoNow(), "updated_at": photoNow()})
}

func (self *transaction) RestorePhotos(userId string, photoIds []string) (int, error) {
	return self.changePhotoTrash(userId, photoIds, "trashed_at IS NOT NULL", map[string]any{"trashed_at": nil, "updated_at": photoNow()})
}

func (self *transaction) changePhotoTrash(userId string, photoIds []string, condition string, changes map[string]any) (int, error) {
	if userId == "" || len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	updated := self.tx.Model(&photoModel{}).Where("user_id = ? AND id IN ? AND "+condition, userId, photoIds).Updates(changes)
	return int(updated.RowsAffected), updated.Error
}

func (self *transaction) ListTrashedPhotos(userId string, photoIds []string, limit int) ([]*models.Photo, error) {
	if userId == "" || limit < 1 || limit > 500 || len(photoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Where("user_id = ? AND trashed_at IS NOT NULL", userId)
	if len(photoIds) > 0 {
		query = query.Where("id IN ?", photoIds)
	}
	var rows []photoModel
	if err := query.Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return photosFromModels(rows), nil
}

func (self *transaction) DeletePhotos(userId string, photoIds []string) (int, error) {
	if userId == "" || len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	deleted := self.tx.Where("user_id = ? AND id IN ? AND trashed_at IS NOT NULL", userId, photoIds).Delete(&photoModel{})
	return int(deleted.RowsAffected), deleted.Error
}

func (self *transaction) ListPhotosTrashedBefore(trashedBefore time.Time, limit int) ([]*models.Photo, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidArguments
	}
	var rows []photoModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("trashed_at IS NOT NULL AND trashed_at < ?", trashedBefore).Order("trashed_at").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return photosFromModels(rows), nil
}

func (self *transaction) ReadPhotoLibraryCounts(userId string) (*PhotoLibraryCounts, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var counts PhotoLibraryCounts
	err := self.tx.Model(&photoModel{}).Where("user_id = ?", userId).Select(`
		COUNT(*) FILTER (WHERE trashed_at IS NULL AND photo_kind <> ?) AS photo_count,
		COUNT(*) FILTER (WHERE trashed_at IS NULL AND photo_kind = ?) AS video_count,
		COUNT(*) FILTER (WHERE trashed_at IS NULL AND is_favorite) AS favorite_count,
		COUNT(*) FILTER (WHERE trashed_at IS NOT NULL) AS trashed_count`, models.PhotoKindVideo, models.PhotoKindVideo).Scan(&counts).Error
	return &counts, err
}

func (self *transaction) PhotoUsedBytes(userId string) (int64, error) {
	if userId == "" {
		return 0, ErrNotFound
	}
	var usedBytes int64
	err := self.tx.Table("photo_file").Select("COALESCE(SUM(photo_file.file_size_bytes), 0)").Joins("JOIN photo ON photo.id = photo_file.photo_id").Where("photo.user_id = ?", userId).Scan(&usedBytes).Error
	return usedBytes, err
}

func isPhotoFileSourceOfPreviews(photoFileRole models.PhotoFileRole) bool {
	return photoFileRole == models.PhotoFileRoleOriginal || photoFileRole == models.PhotoFileRoleEdited
}

func (self *transaction) SetPhotoFile(userId, photoId string, file *models.PhotoFile, maximumAccountBytes int64) (bool, error) {
	if file == nil || !file.PhotoFileRole.IsValid() || !validPhotoSha256(file.BlobSHA256) || file.FileSizeBytes < 0 || len(file.ContentType) > 128 || len(file.OriginalFilename) > 255 || maximumAccountBytes < 0 {
		return false, ErrInvalidArguments
	}
	// The account's row is locked so that two files arriving together are
	// each counted against the other.
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}
	photo, err := self.GetPhoto(userId, photoId)
	if err != nil {
		return false, err
	}
	var previous photoFileModel
	err = self.tx.Where("photo_id = ? AND photo_file_role = ?", photoId, file.PhotoFileRole).Take(&previous).Error
	hasPrevious := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if hasPrevious && previous.BlobSHA256 == file.BlobSHA256 {
		return false, nil
	}
	if maximumAccountBytes > 0 {
		usedBytes, err := self.PhotoUsedBytes(userId)
		if err != nil {
			return false, err
		}
		if hasPrevious {
			usedBytes -= previous.FileSizeBytes
		}
		if usedBytes > maximumAccountBytes-file.FileSizeBytes {
			return false, ErrTooMuchAsked
		}
	}
	row := photoFileModel{PhotoID: photoId, PhotoFileRole: file.PhotoFileRole, BlobSHA256: file.BlobSHA256, ContentType: file.ContentType, FileSizeBytes: file.FileSizeBytes, OriginalFilename: file.OriginalFilename, CreatedAt: photoNow()}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "photo_id"}, {Name: "photo_file_role"}},
		DoUpdates: clause.AssignmentColumns([]string{"blob_sha256", "content_type", "file_size_bytes", "original_filename", "created_at"}),
	}).Create(&row).Error; err != nil {
		// The foreign key to the blob: cleanup deleted it after the bytes
		// were stored and before this file was recorded.
		if strings.Contains(err.Error(), "23503") {
			return false, ErrPhotoBlobGone
		}
		return false, err
	}
	changes := map[string]any{"updated_at": photoNow()}
	if isPhotoFileSourceOfPreviews(file.PhotoFileRole) {
		// A different file means different previews and a different date and
		// place, so the photo goes back to waiting, whatever it was.
		changes["photo_processing_state"] = models.PhotoProcessingWaiting
		changes["processing_attempt_count"] = 0
		changes["processing_error_message"] = ""
		changes["processing_claimed_at"] = nil
	}
	if file.PhotoFileRole == models.PhotoFileRoleLiveVideo && photo.PhotoKind == models.PhotoKindPicture {
		changes["photo_kind"] = models.PhotoKindLivePhoto
	}
	return true, self.tx.Model(&photoModel{}).Where("id = ?", photoId).Updates(changes).Error
}

func (self *transaction) GetPhotoFile(userId, photoId string, photoFileRole models.PhotoFileRole) (*models.PhotoFile, error) {
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return nil, err
	}
	var row photoFileModel
	if err := self.tx.Where("photo_id = ? AND photo_file_role = ?", photoId, photoFileRole).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return photoFileFromModel(&row), nil
}

func (self *transaction) ListPhotoFiles(userId string, photoIds []string) ([]*models.PhotoFile, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if len(photoIds) == 0 {
		return []*models.PhotoFile{}, nil
	}
	if len(photoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	var rows []photoFileModel
	if err := self.tx.Joins("JOIN photo ON photo.id = photo_file.photo_id").Where("photo.user_id = ? AND photo_file.photo_id IN ?", userId, photoIds).Order("photo_file.photo_id, photo_file.photo_file_role").Find(&rows).Error; err != nil {
		return nil, err
	}
	files := make([]*models.PhotoFile, 0, len(rows))
	for index := range rows {
		files = append(files, photoFileFromModel(&rows[index]))
	}
	return files, nil
}

func validPhotoSha256(digest string) bool {
	return validRecordingSHA256(digest)
}

func (self *transaction) GetPhotoBlob(blobSha256 string) (*models.PhotoBlob, error) {
	var row photoBlobModel
	if err := self.tx.Where("blob_sha256 = ?", blobSha256).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return photoBlobFromModel(&row), nil
}

func (self *transaction) InsertPhotoBlobIfAbsent(blob *models.PhotoBlob) (bool, error) {
	if blob == nil || !validPhotoSha256(blob.BlobSHA256) || blob.BlobSizeBytes < 0 {
		return false, ErrInvalidArguments
	}
	// xmax is zero on a row this statement inserted and not on one it
	// updated, which is how a caller learns that the blob it believed
	// was there had gone.
	rows, err := self.tx.Raw(`INSERT INTO photo_blob (blob_sha256, blob_size_bytes, created_at, deleting_at) VALUES (?, ?, ?, NULL)
		ON CONFLICT (blob_sha256) DO UPDATE SET deleting_at = NULL WHERE photo_blob.deleting_at IS NOT NULL
		RETURNING (xmax = 0)`, blob.BlobSHA256, blob.BlobSizeBytes, photoNow()).Rows()
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	isInserted := false
	if rows.Next() {
		if err := rows.Scan(&isInserted); err != nil {
			return false, err
		}
	}
	return isInserted, rows.Err()
}

func (self *transaction) MarkUnreferencedPhotoBlobDeleting(createdBefore time.Time) (*models.PhotoBlob, error) {
	var row photoBlobModel
	err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("created_at < ? AND NOT EXISTS (SELECT 1 FROM photo_file WHERE photo_file.blob_sha256 = photo_blob.blob_sha256)", createdBefore).
		Order("created_at").Limit(1).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if row.DeletingAt == nil {
		markedAt := photoNow()
		if err := self.tx.Model(&photoBlobModel{}).Where("blob_sha256 = ?", row.BlobSHA256).Update("deleting_at", markedAt).Error; err != nil {
			return nil, err
		}
		row.DeletingAt = &markedAt
	}
	return photoBlobFromModel(&row), nil
}

func (self *transaction) LockPhotoBlobForDeletion(blobSha256 string) (*models.PhotoBlob, error) {
	var row photoBlobModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("blob_sha256 = ?", blobSha256).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Counted after the lock is held and in a statement of its own: a file
	// being recorded takes a share of this row first, so one that was not
	// there a moment ago cannot appear while the lock is ours.
	var referenceCount int64
	if err := self.tx.Model(&photoFileModel{}).Where("blob_sha256 = ?", blobSha256).Count(&referenceCount).Error; err != nil {
		return nil, err
	}
	if referenceCount > 0 {
		if err := self.tx.Model(&photoBlobModel{}).Where("blob_sha256 = ?", blobSha256).Update("deleting_at", nil).Error; err != nil {
			return nil, err
		}
		return nil, nil
	}
	return photoBlobFromModel(&row), nil
}

func (self *transaction) DeletePhotoBlob(blobSha256 string) error {
	return self.tx.Where("blob_sha256 = ?", blobSha256).Delete(&photoBlobModel{}).Error
}

func (self *transaction) UpsertPhotoPreview(userId string, preview *models.PhotoPreview) (*models.PhotoPreview, error) {
	if preview == nil || !preview.PhotoPreviewSize.IsValid() || preview.PreviewSizeBytes < 0 || preview.WidthPixels < 0 || preview.HeightPixels < 0 || len(preview.ContentType) > 128 {
		return nil, ErrInvalidArguments
	}
	if _, err := self.GetPhoto(userId, preview.PhotoID); err != nil {
		return nil, err
	}
	row := photoPreviewModel(*preview)
	row.CreatedAt = photoNow()
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "photo_id"}, {Name: "photo_preview_size"}},
		DoUpdates: clause.AssignmentColumns([]string{"content_type", "preview_size_bytes", "width_pixels", "height_pixels", "created_at"}),
	}).Create(&row).Error; err != nil {
		return nil, err
	}
	return photoPreviewFromModel(&row), nil
}

func (self *transaction) GetPhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) (*models.PhotoPreview, error) {
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return nil, err
	}
	var row photoPreviewModel
	if err := self.tx.Where("photo_id = ? AND photo_preview_size = ?", photoId, photoPreviewSize).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return photoPreviewFromModel(&row), nil
}

func (self *transaction) ListPhotoPreviews(userId string, photoIds []string) ([]*models.PhotoPreview, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if len(photoIds) == 0 {
		return []*models.PhotoPreview{}, nil
	}
	if len(photoIds) > 500 {
		return nil, ErrInvalidArguments
	}
	var rows []photoPreviewModel
	if err := self.tx.Joins("JOIN photo ON photo.id = photo_preview.photo_id").Where("photo.user_id = ? AND photo_preview.photo_id IN ?", userId, photoIds).Order("photo_preview.photo_id, photo_preview.photo_preview_size").Find(&rows).Error; err != nil {
		return nil, err
	}
	previews := make([]*models.PhotoPreview, 0, len(rows))
	for index := range rows {
		previews = append(previews, photoPreviewFromModel(&rows[index]))
	}
	return previews, nil
}

func (self *transaction) DeletePhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) error {
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return err
	}
	return self.tx.Where("photo_id = ? AND photo_preview_size = ?", photoId, photoPreviewSize).Delete(&photoPreviewModel{}).Error
}

func (self *transaction) ClaimPhotosForProcessing(limit int, staleBefore time.Time) ([]*models.Photo, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	// A photo with only a Live Photo's video has nothing to make previews
	// from yet; it waits for its picture to arrive.
	var rows []photoModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where(`photo_processing_state = ? AND (processing_claimed_at IS NULL OR processing_claimed_at < ?)
			AND EXISTS (SELECT 1 FROM photo_file WHERE photo_file.photo_id = photo.id AND photo_file.photo_file_role IN ?)`,
			models.PhotoProcessingWaiting, staleBefore, []models.PhotoFileRole{models.PhotoFileRoleOriginal, models.PhotoFileRoleEdited}).
		Order("created_at, id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	claimedAt := photoNow()
	for index := range rows {
		if err := self.tx.Model(&photoModel{}).Where("id = ?", rows[index].ID).Update("processing_claimed_at", claimedAt).Error; err != nil {
			return nil, err
		}
		rows[index].ProcessingClaimedAt = &claimedAt
	}
	return photosFromModels(rows), nil
}

func (self *transaction) MarkPhotoReady(photoId string) error {
	return self.tx.Model(&photoModel{}).Where("id = ?", photoId).Updates(map[string]any{
		"photo_processing_state":   models.PhotoProcessingReady,
		"processing_error_message": "",
		"processing_claimed_at":    nil,
		"updated_at":               photoNow(),
	}).Error
}

func (self *transaction) MarkPhotoProcessingFailed(photoId, errorMessage string, maximumAttemptCount int) error {
	if maximumAttemptCount < 1 {
		return ErrInvalidArguments
	}
	var row photoModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", photoId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	attemptCount := row.ProcessingAttemptCount + 1
	photoProcessingState := models.PhotoProcessingWaiting
	if attemptCount >= maximumAttemptCount {
		photoProcessingState = models.PhotoProcessingFailed
	}
	if len(errorMessage) > photoErrorMessageBytes {
		errorMessage = strings.ToValidUTF8(errorMessage[:photoErrorMessageBytes], "")
	}
	return self.tx.Model(&photoModel{}).Where("id = ?", photoId).Updates(map[string]any{
		"photo_processing_state":   photoProcessingState,
		"processing_attempt_count": attemptCount,
		"processing_error_message": errorMessage,
		"processing_claimed_at":    nil,
		"updated_at":               photoNow(),
	}).Error
}
