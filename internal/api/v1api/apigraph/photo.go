package apigraph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// PhotoQuery reads the person's photo library.
type PhotoQuery interface {
	ListPhotos(context.Context, ListPhotosArguments) ([]*models.Photo, error)
	CountPhotos(context.Context, ListPhotosArguments) (int, error)
	ReadPhoto(context.Context, PhotoArguments) (*PhotoView, error)
	ReadPhotoLibrary(context.Context) (*PhotoLibraryView, error)
	FindPhotos(context.Context, FindPhotosArguments) ([]*models.Photo, error)
}

// PhotoMutation changes the person's photo library. Files go up over the
// resumable upload endpoint, not through here.
type PhotoMutation interface {
	UpdatePhoto(context.Context, UpdatePhotoArguments) (*models.Photo, error)
	TrashPhotos(context.Context, PhotoIDsArguments) (int, error)
	RestorePhotos(context.Context, PhotoIDsArguments) (int, error)
	DeletePhotos(context.Context, PhotoIDsArguments) (int, error)
	EmptyPhotoTrash(context.Context) (int, error)
	SavePhotoFromAttachment(context.Context, SavePhotoFromAttachmentArguments) (*models.Photo, error)
}

// photoPageSize is how many photos a page holds when the caller does not say.
const photoPageSize = 100

// ListPhotosArguments pages through the library newest first, by the date a
// photo was taken, or arrived if that is not known. Every filter is optional.
type ListPhotosArguments struct {
	// BeforePhotoID continues after the last photo of the previous page.
	BeforePhotoID string `json:"beforePhotoId" graphapi:"nullable"`

	// Offset skips that many photos, at most 100000, for a page reached by
	// number. Not together with beforePhotoId.
	Offset int `json:"offset" graphapi:"nullable"`

	// PageSize is how many photos to return, at most 500. Zero means 100.
	PageSize int `json:"pageSize" graphapi:"nullable"`

	// IsFavorite keeps favorites when true and the rest when false.
	IsFavorite *bool `json:"isFavorite"`

	// IsTrashed lists the trash instead of the library.
	IsTrashed *bool `json:"isTrashed"`

	PhotoKind *models.PhotoKind `json:"photoKind"`

	// TakenAfter and TakenBefore bound the date a photo is placed at.
	TakenAfter  *time.Time `json:"takenAfter"`
	TakenBefore *time.Time `json:"takenBefore"`

	// SearchText keeps photos whose place name, file name or camera has
	// every word.
	SearchText string `json:"searchText" graphapi:"nullable"`

	// NearLatitude, NearLongitude and NearDistanceKilometers keep photos
	// taken within that distance of a point. All three or none.
	NearLatitude           *float64 `json:"nearLatitude"`
	NearLongitude          *float64 `json:"nearLongitude"`
	NearDistanceKilometers *float64 `json:"nearDistanceKilometers"`
}

// PhotoArguments names one photo.
type PhotoArguments struct {
	PhotoID string `json:"photoId"`
}

// PhotoIDsArguments names photos, at most 500.
type PhotoIDsArguments struct {
	PhotoIDs []string `json:"photoIds"`
}

// FindPhotosArguments asks which of an uploader's photos are already here.
type FindPhotosArguments struct {
	ClientPhotoIDs []string `json:"clientPhotoIds"`
}

// UpdatePhotoArguments corrects a photo. A field left out is kept.
type UpdatePhotoArguments struct {
	PhotoID    string     `json:"photoId"`
	IsFavorite *bool      `json:"isFavorite"`
	TakenAt    *time.Time `json:"takenAt"`
}

// SavePhotoFromAttachmentArguments names a file of a conversation to keep.
type SavePhotoFromAttachmentArguments struct {
	AgentAttachmentID string `json:"agentAttachmentId"`
}

// PhotoView is one photo with the files it is made of and the previews that
// have been made of it.
type PhotoView struct {
	Photo         *models.Photo          `json:"photo"`
	PhotoFiles    []*models.PhotoFile    `json:"photoFiles"`
	PhotoPreviews []*models.PhotoPreview `json:"photoPreviews"`
}

// PhotoLibraryView is what a library page shows beside the photos: how much
// there is, how much is allowed, and where to upload.
type PhotoLibraryView struct {
	PhotoCount    int64 `json:"photoCount"`
	VideoCount    int64 `json:"videoCount"`
	FavoriteCount int64 `json:"favoriteCount"`
	TrashedCount  int64 `json:"trashedCount"`

	// UsedBytes is the size of every file the library holds, the trash
	// included.
	UsedBytes int64 `json:"usedBytes"`

	// MaximumFileBytes is the largest file an upload may carry, and
	// MaximumAccountBytes the most the library may hold, zero for no limit.
	MaximumFileBytes    int64 `json:"maximumFileBytes"`
	MaximumAccountBytes int64 `json:"maximumAccountBytes"`

	// TrashRetentionDays is how long a photo stays in the trash.
	TrashRetentionDays int `json:"trashRetentionDays"`

	// UploadPath is where files go up, and UploadPartBytes how much of a file
	// one stored part holds.
	UploadPath      string `json:"uploadPath"`
	UploadPartBytes int64  `json:"uploadPartBytes"`

	IsFaceRecognitionAvailable        bool   `json:"isFaceRecognitionAvailable"`
	FaceRecognitionUnavailableMessage string `json:"faceRecognitionUnavailableMessage"`
	IsDescriptionAvailable            bool   `json:"isDescriptionAvailable"`
	DescriptionUnavailableMessage     string `json:"descriptionUnavailableMessage"`
}

// requirePhotosPerson is the caller, if they may keep a photo library.
func (self *graph) requirePhotosPerson(ctx context.Context) (*api.Principal, error) {
	if err := self.requireService(models.ServicePhotos); err != nil {
		return nil, err
	}
	principal, err := self.requirePermission(ctx, models.PermissionPhotosUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// withPreviewAddresses fills in where each photo's previews are fetched
// from. An address carries the time the photo last changed, so that a
// preview made again is a new address and the old one can be cached for good.
func (self *graph) withPreviewAddresses(ctx context.Context, userId string, photos []*models.Photo) error {
	photoIds := make([]string, 0, len(photos))
	for _, found := range photos {
		photoIds = append(photoIds, found.ID)
	}
	previews, err := self.transaction(ctx).ListPhotoPreviews(userId, photoIds)
	if err != nil {
		return err
	}
	madeSizes := map[string]map[models.PhotoPreviewSize]bool{}
	for _, preview := range previews {
		if madeSizes[preview.PhotoID] == nil {
			madeSizes[preview.PhotoID] = map[models.PhotoPreviewSize]bool{}
		}
		madeSizes[preview.PhotoID][preview.PhotoPreviewSize] = true
	}
	for _, found := range photos {
		address := func(photoPreviewSize models.PhotoPreviewSize) string {
			if !madeSizes[found.ID][photoPreviewSize] {
				return ""
			}
			return photoPreviewAddress(found, photoPreviewSize)
		}
		found.SmallPreviewURL, found.LargePreviewURL, found.VideoPreviewURL = address(models.PhotoPreviewSmall), address(models.PhotoPreviewLarge), address(models.PhotoPreviewVideo)
	}
	return nil
}

func photoPreviewAddress(found *models.Photo, photoPreviewSize models.PhotoPreviewSize) string {
	return api.PhotoPreviewPath(found.ID, string(photoPreviewSize)) + "?v=" + strconv.FormatInt(found.UpdatedAt.Unix(), 10)
}

// photoFilter is what the filters of a listing mean, for listing and for
// counting alike.
func photoFilter(arguments ListPhotosArguments) *db.PhotoFilter {
	filter := &db.PhotoFilter{
		BeforePhotoID: arguments.BeforePhotoID, Offset: arguments.Offset, PageSize: arguments.PageSize, IsFavorite: arguments.IsFavorite, IsTrashed: arguments.IsTrashed != nil && *arguments.IsTrashed,
		TakenAfter: arguments.TakenAfter, TakenBefore: arguments.TakenBefore, SearchText: arguments.SearchText,
		NearLatitude: arguments.NearLatitude, NearLongitude: arguments.NearLongitude, NearDistanceKilometers: arguments.NearDistanceKilometers,
	}
	if filter.PageSize == 0 {
		filter.PageSize = photoPageSize
	}
	if arguments.PhotoKind != nil {
		filter.PhotoKind = *arguments.PhotoKind
	}
	return filter
}

func (self *graph) ListPhotos(ctx context.Context, arguments ListPhotosArguments) ([]*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	photos, err := self.transaction(ctx).ListPhotos(principal.User.ID, photoFilter(arguments))
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.withPreviewAddresses(ctx, principal.User.ID, photos); err != nil {
		return nil, translateError(err)
	}
	return photos, nil
}

// CountPhotos is how many photos the filters of a listing keep. The paging
// arguments are ignored: it is the size of the whole listing.
func (self *graph) CountPhotos(ctx context.Context, arguments ListPhotosArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	filter := photoFilter(arguments)
	filter.BeforePhotoID, filter.Offset = "", 0
	count, err := self.transaction(ctx).CountPhotos(principal.User.ID, filter)
	return count, translateError(err)
}

func (self *graph) ReadPhoto(ctx context.Context, arguments PhotoArguments) (*PhotoView, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	found, err := transaction.GetPhoto(principal.User.ID, arguments.PhotoID)
	if err != nil {
		return nil, translateError(err)
	}
	files, err := transaction.ListPhotoFiles(principal.User.ID, []string{found.ID})
	if err != nil {
		return nil, translateError(err)
	}
	for _, file := range files {
		file.FileURL = api.PhotoFilePath(file.PhotoID, string(file.PhotoFileRole))
	}
	previews, err := transaction.ListPhotoPreviews(principal.User.ID, []string{found.ID})
	if err != nil {
		return nil, translateError(err)
	}
	for _, preview := range previews {
		preview.PreviewURL = photoPreviewAddress(found, preview.PhotoPreviewSize)
	}
	if err := self.withPreviewAddresses(ctx, principal.User.ID, []*models.Photo{found}); err != nil {
		return nil, translateError(err)
	}
	return &PhotoView{Photo: found, PhotoFiles: files, PhotoPreviews: previews}, nil
}

func (self *graph) ReadPhotoLibrary(ctx context.Context) (*PhotoLibraryView, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	counts, err := transaction.ReadPhotoLibraryCounts(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	usedBytes, err := transaction.PhotoUsedBytes(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	limits := self.config.Current().Photos
	if limits.MaxFileBytes.Bytes() > math.MaxInt64 || limits.MaxAccountBytes.Bytes() > math.MaxInt64 {
		return nil, fmt.Errorf("photo limits exceed supported byte sizes")
	}
	return &PhotoLibraryView{
		PhotoCount: counts.PhotoCount, VideoCount: counts.VideoCount, FavoriteCount: counts.FavoriteCount, TrashedCount: counts.TrashedCount,
		UsedBytes: usedBytes, MaximumFileBytes: int64(limits.MaxFileBytes.Bytes()), MaximumAccountBytes: int64(limits.MaxAccountBytes.Bytes()),
		TrashRetentionDays: int(math.Ceil(limits.TrashRetention.Duration().Hours() / 24)),
		UploadPath:         api.PathPhotoUploads, UploadPartBytes: models.PhotoUploadPartBytes,
		FaceRecognitionUnavailableMessage: "Finding faces in photos is not set up yet.",
		DescriptionUnavailableMessage:     "Describing photos is not set up yet.",
	}, nil
}

func (self *graph) FindPhotos(ctx context.Context, arguments FindPhotosArguments) ([]*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	photos, err := self.transaction(ctx).FindPhotosByClientIDs(principal.User.ID, arguments.ClientPhotoIDs)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.withPreviewAddresses(ctx, principal.User.ID, photos); err != nil {
		return nil, translateError(err)
	}
	return photos, nil
}

func (self *graph) UpdatePhoto(ctx context.Context, arguments UpdatePhotoArguments) (*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	if arguments.IsFavorite == nil && arguments.TakenAt == nil {
		return nil, fmt.Errorf("%w: say what to change", api.ErrInvalidArguments)
	}
	updated, err := self.transaction(ctx).UpdatePhoto(principal.User.ID, arguments.PhotoID, &db.PhotoUpdate{IsFavorite: arguments.IsFavorite, TakenAt: arguments.TakenAt})
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.withPreviewAddresses(ctx, principal.User.ID, []*models.Photo{updated}); err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

func (self *graph) TrashPhotos(ctx context.Context, arguments PhotoIDsArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	trashedCount, err := self.transaction(ctx).TrashPhotos(principal.User.ID, arguments.PhotoIDs)
	return trashedCount, translateError(err)
}

func (self *graph) RestorePhotos(ctx context.Context, arguments PhotoIDsArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	restoredCount, err := self.transaction(ctx).RestorePhotos(principal.User.ID, arguments.PhotoIDs)
	return restoredCount, translateError(err)
}

// DeletePhotos deletes forever photos that are already in the trash; the
// rest of those named are left where they are. The photos are marked here and
// leave every listing at once; the cleanup job deletes their rows and files in
// batches, so that a large trash is not one request's transaction.
func (self *graph) DeletePhotos(ctx context.Context, arguments PhotoIDsArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	deletedCount, err := self.transaction(ctx).MarkPhotosForDeletion(principal.User.ID, arguments.PhotoIDs)
	return deletedCount, translateError(err)
}

// EmptyPhotoTrash deletes forever everything in the person's trash, the same
// way DeletePhotos does, and says how many photos that was.
func (self *graph) EmptyPhotoTrash(ctx context.Context) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	deletedCount, err := self.transaction(ctx).MarkTrashForDeletion(principal.User.ID)
	return deletedCount, translateError(err)
}

// SavePhotoFromAttachment keeps a file of the person's conversation with
// their agent as a photo. The photo is named by the digest of the file, so
// saving the same file again finds the photo that is already there.
func (self *graph) SavePhotoFromAttachment(ctx context.Context, arguments SavePhotoFromAttachmentArguments) (*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	attachment, err := transaction.GetAgentAttachment(arguments.AgentAttachmentID)
	if err != nil {
		return nil, translateError(err)
	}
	if attachment == nil {
		return nil, api.ErrNotFound
	}
	assistant, err := transaction.GetAgent(attachment.AgentID)
	if err != nil {
		return nil, translateError(err)
	}
	// A file of somebody else's conversation is not found, rather than
	// refused, because whose it is is not the caller's business.
	if assistant == nil || assistant.UserID != principal.User.ID {
		return nil, api.ErrNotFound
	}
	store, err := self.photoStore()
	if err != nil {
		return nil, err
	}
	saved, err := store.SaveAttachment(ctx, principal.User.ID, attachment)
	if err != nil {
		var tooLarge *photo.FileTooLargeError
		var overQuota *photo.QuotaError
		if errors.As(err, &tooLarge) || errors.As(err, &overQuota) || errors.Is(err, photo.ErrNotAPhoto) || errors.Is(err, photo.ErrEmptyFile) {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		return nil, translateError(err)
	}
	return saved, nil
}
