package apigraph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

	// CountUndescribedPhotos is how many photos the filters keep that have no
	// description yet, so that a caller who searched a range can say how much
	// of it a search by what is in the pictures cannot see into. The search
	// words are ignored.
	CountUndescribedPhotos(context.Context, ListPhotosArguments) (int, error)
	ReadPhoto(context.Context, PhotoArguments) (*PhotoView, error)
	ReadPhotoLibrary(context.Context) (*PhotoLibraryView, error)
	FindPhotos(context.Context, FindPhotosArguments) ([]*models.Photo, error)

	// An address for one preview or one file of a photo that a player can
	// fetch by itself, with ranges, because it carries a signature instead
	// of the person's sign-in. Good for a few hours; the caller must own the
	// photo for now.
	PhotoPlaybackAddress(context.Context, PhotoPlaybackAddressArguments) (string, error)
}

// PhotoMutation changes the person's photo library. Files go up over the
// resumable upload endpoint, not through here.
type PhotoMutation interface {
	UpdatePhoto(context.Context, UpdatePhotoArguments) (*models.Photo, error)
	TrashPhotos(context.Context, PhotoIDsArguments) (int, error)
	RestorePhotos(context.Context, PhotoIDsArguments) (int, error)
	DeletePhotos(context.Context, PhotoIDsArguments) (int, error)
	EmptyPhotoTrash(context.Context) (int, error)

	// RetryPhotoProcessing puts photos whose previews failed, or that are
	// ready and whose previews should be made again, back to waiting with a
	// fresh count of attempts, and says how many it moved. Photos that are
	// waiting already are left alone.
	RetryPhotoProcessing(context.Context, PhotoIDsArguments) (int, error)
	SavePhotoFromAttachment(context.Context, SavePhotoFromAttachmentArguments) (*models.Photo, error)

	// DescribePhoto asks the person's assistant to look at the picture and
	// write what is in it, and answers at once with the photo as it is now:
	// the description is on its way when isDescriptionRequested is true, and
	// arrives in describedAt. A photo that has a description is left alone
	// unless isRedescribed is true.
	DescribePhoto(context.Context, DescribePhotoArguments) (*models.Photo, error)
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

	// SearchText keeps photos that match the words, best match first instead
	// of newest first, at most 500: the place, the file name, the camera, the
	// titles of the albums a photo is in, and, for a photo that has been
	// described, its description and the words read in it. A photo nobody has
	// described is found by the rest alone.
	SearchText string `json:"searchText" graphapi:"nullable"`

	// NearLatitude, NearLongitude and NearDistanceKilometers keep photos
	// taken within that distance of a point. All three or none.
	NearLatitude           *float64 `json:"nearLatitude"`
	NearLongitude          *float64 `json:"nearLongitude"`
	NearDistanceKilometers *float64 `json:"nearDistanceKilometers"`

	// AlbumID keeps the photos of one album.
	AlbumID string `json:"albumId" graphapi:"nullable"`

	// FaceGroupID keeps the photos with a face in one group of faces, and
	// ContactID the photos with a face in any group that contact names.
	FaceGroupID string `json:"faceGroupId" graphapi:"nullable"`
	ContactID   string `json:"contactId" graphapi:"nullable"`
}

// PhotoArguments names one photo.
type PhotoArguments struct {
	PhotoID string `json:"photoId"`
}

// DescribePhotoArguments names a photo to describe.
type DescribePhotoArguments struct {
	PhotoID string `json:"photoId"`

	// IsRedescribed asks for a new description of a photo that has one.
	IsRedescribed *bool `json:"isRedescribed"`
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
	PhotoID    string `json:"photoId"`
	IsFavorite *bool  `json:"isFavorite"`

	// TakenAt is the new date. It is an RFC 3339 time, and its offset is the
	// offset the photo is shown at: send the offset of the place the photo was
	// taken at, such as +09:00, and not Z unless that place is on UTC.
	TakenAt *time.Time `json:"takenAt"`
}

// SavePhotoFromAttachmentArguments names a file of a conversation to keep.
type SavePhotoFromAttachmentArguments struct {
	AgentAttachmentID string `json:"agentAttachmentId"`
}

// PhotoPlaybackAddressArguments name one preview or one file of a photo: exactly one of
// PhotoPreviewSize (such as video) and PhotoFileRole (such as original).
type PhotoPlaybackAddressArguments struct {
	PhotoID          string                   `json:"photoId"`
	PhotoPreviewSize *models.PhotoPreviewSize `json:"photoPreviewSize"`
	PhotoFileRole    *models.PhotoFileRole    `json:"photoFileRole"`
}

// PhotoView is one photo with the files it is made of and the previews that
// have been made of it.
type PhotoView struct {
	Photo         *models.Photo          `json:"photo"`
	PhotoFiles    []*models.PhotoFile    `json:"photoFiles"`
	PhotoPreviews []*models.PhotoPreview `json:"photoPreviews"`

	// Albums are the albums the photo is in, by title.
	Albums []*models.PhotoAlbumReference `json:"albums"`

	// Faces are the faces found in the photo, each with its group and the
	// name of the contact that names the group.
	Faces []*models.PhotoFace `json:"faces"`
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

	// IsFaceRecognitionAvailable says faces can be found on this server, and
	// when not FaceRecognitionUnavailableMessage says why.
	// IsPhotoFaceRecognitionEnabled is the person's own switch.
	IsFaceRecognitionAvailable        bool   `json:"isFaceRecognitionAvailable"`
	FaceRecognitionUnavailableMessage string `json:"faceRecognitionUnavailableMessage"`
	IsPhotoFaceRecognitionEnabled     bool   `json:"isPhotoFaceRecognitionEnabled"`
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

// withFilesAndPreviews fills in where each photo's previews are fetched
// from, and which files the server holds for it. An address carries the time
// the photo last changed, so that a preview made again is a new address and
// the old one can be cached for good.
func (self *graph) withFilesAndPreviews(ctx context.Context, userId string, photos []*models.Photo) error {
	photoIds := make([]string, 0, len(photos))
	for _, found := range photos {
		photoIds = append(photoIds, found.ID)
	}
	files, err := self.transaction(ctx).ListPhotoFiles(userId, photoIds)
	if err != nil {
		return err
	}
	fileRoles := map[string][]models.PhotoFileRole{}
	for _, file := range files {
		fileRoles[file.PhotoID] = append(fileRoles[file.PhotoID], file.PhotoFileRole)
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
		found.PhotoFileRoles = fileRoles[found.ID]
		if found.PhotoFileRoles == nil {
			found.PhotoFileRoles = []models.PhotoFileRole{}
		}
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
		AlbumID: arguments.AlbumID, FaceGroupID: arguments.FaceGroupID, ContactID: arguments.ContactID,
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
	filter := photoFilter(arguments)
	filter.SearchMeaning = self.photoSearchMeaning(ctx, principal.User.ID, filter.SearchText)
	photos, err := self.transaction(ctx).ListPhotos(principal.User.ID, filter)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.withFilesAndPreviews(ctx, principal.User.ID, photos); err != nil {
		return nil, translateError(err)
	}
	return photos, nil
}

// photoSearchMeaning is the words of a search as a vector, for finding
// described photos by what they mean. Nil, and the search by words alone,
// when the person has no assistant on or the server no embedding model.
//
// A page of a search asks for the listing and the count together, with the
// same words, so what the model answered is kept for a few seconds and the
// second does not call it again.
func (self *graph) photoSearchMeaning(ctx context.Context, userId, searchText string) *db.PhotoSearchMeaning {
	worker := self.agentWorker()
	if worker == nil || strings.TrimSpace(searchText) == "" {
		return nil
	}
	return self.photoSearchMeanings.read(userId, strings.TrimSpace(searchText), func() *db.PhotoSearchMeaning {
		found, err := self.transaction(ctx).GetAgentByUser(userId)
		if err != nil || !found.Active() {
			return nil
		}
		return worker.EmbedPhotoSearch(ctx, found, searchText)
	})
}

// photoSearchMeaningLifetime is how long the reading of a search's words is
// kept, and photoSearchMeaningLimit how many are kept at once.
const (
	photoSearchMeaningLifetime = 10 * time.Second
	photoSearchMeaningLimit    = 256
)

// photoSearchMeaningCache keeps the readings of the words of recent searches,
// by whose search it was and the words. An answer of nothing is kept too: a
// model that did not answer is not asked again for each of the requests of one
// page.
type photoSearchMeaningCache struct {
	mutex   sync.Mutex
	entries map[string]photoSearchMeaningEntry
}

type photoSearchMeaningEntry struct {
	meaning   *db.PhotoSearchMeaning
	expiresAt time.Time
}

// read gives the kept reading of the words, or makes one with embed and keeps
// it. Two requests that ask at the same moment may both make one; the cost of
// that is the cost of not having the cache.
func (self *photoSearchMeaningCache) read(userId, searchText string, embed func() *db.PhotoSearchMeaning) *db.PhotoSearchMeaning {
	key := userId + "\x00" + searchText
	now := time.Now()
	self.mutex.Lock()
	if entry, isKept := self.entries[key]; isKept && now.Before(entry.expiresAt) {
		self.mutex.Unlock()
		return entry.meaning
	}
	self.mutex.Unlock()

	meaning := embed()

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.entries == nil {
		self.entries = map[string]photoSearchMeaningEntry{}
	}
	if len(self.entries) >= photoSearchMeaningLimit {
		for oldKey, entry := range self.entries {
			if !now.Before(entry.expiresAt) {
				delete(self.entries, oldKey)
			}
		}
		if len(self.entries) >= photoSearchMeaningLimit {
			clear(self.entries)
		}
	}
	self.entries[key] = photoSearchMeaningEntry{meaning: meaning, expiresAt: now.Add(photoSearchMeaningLifetime)}
	return meaning
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
	filter.SearchMeaning = self.photoSearchMeaning(ctx, principal.User.ID, filter.SearchText)
	count, err := self.transaction(ctx).CountPhotos(principal.User.ID, filter)
	return count, translateError(err)
}

// CountUndescribedPhotos is how many photos the filters keep that no model
// has described. It is a count of a range, so the search words are not used.
func (self *graph) CountUndescribedPhotos(ctx context.Context, arguments ListPhotosArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	filter := photoFilter(arguments)
	filter.BeforePhotoID, filter.Offset = "", 0
	count, err := self.transaction(ctx).CountUndescribedPhotos(principal.User.ID, filter)
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
	if err := self.withFilesAndPreviews(ctx, principal.User.ID, []*models.Photo{found}); err != nil {
		return nil, translateError(err)
	}
	albums, err := transaction.ListPhotoAlbumsOfPhoto(principal.User.ID, found.ID)
	if err != nil {
		return nil, translateError(err)
	}
	faces, err := transaction.ListPhotoFaces(principal.User.ID, []string{found.ID})
	if err != nil {
		return nil, translateError(err)
	}
	withFacePictureAddresses(faces...)
	return &PhotoView{Photo: found, PhotoFiles: files, PhotoPreviews: previews, Albums: albums, Faces: faces}, nil
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
	descriptionUnavailableMessage, err := self.photoDescriptionUnavailableMessage(ctx, principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	isFaceRecognitionEnabled, err := transaction.IsPhotoFaceRecognitionEnabled(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	faceUnavailableMessage := photo.FaceRecognitionUnavailableMessage(self.config.Current())
	return &PhotoLibraryView{
		PhotoCount: counts.PhotoCount, VideoCount: counts.VideoCount, FavoriteCount: counts.FavoriteCount, TrashedCount: counts.TrashedCount,
		UsedBytes: usedBytes, MaximumFileBytes: int64(limits.MaxFileBytes.Bytes()), MaximumAccountBytes: int64(limits.MaxAccountBytes.Bytes()),
		TrashRetentionDays: int(math.Ceil(limits.TrashRetention.Duration().Hours() / 24)),
		UploadPath:         api.PathPhotoUploads, UploadPartBytes: models.PhotoUploadPartBytes,
		IsFaceRecognitionAvailable: faceUnavailableMessage == "", FaceRecognitionUnavailableMessage: faceUnavailableMessage,
		IsPhotoFaceRecognitionEnabled: isFaceRecognitionEnabled,
		IsDescriptionAvailable:        descriptionUnavailableMessage == "",
		DescriptionUnavailableMessage: descriptionUnavailableMessage,
	}, nil
}

// photoDescriptionUnavailableMessage says why the person cannot have a photo
// described, or "" when they can: it takes their assistant to be on, the
// permission to use it, and a model that reads pictures.
func (self *graph) photoDescriptionUnavailableMessage(ctx context.Context, userId string) (string, error) {
	const assistantOff = "Describing photos needs the assistant to be on."
	worker := self.agentWorker()
	if worker == nil {
		return assistantOff, nil
	}
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return "", err
	}
	if !principal.Permissions.Has(models.PermissionAgentUse) {
		return assistantOff, nil
	}
	found, err := self.transaction(ctx).GetAgentByUser(userId)
	if err != nil {
		return "", err
	}
	if !found.Active() {
		return assistantOff, nil
	}
	return worker.PhotoDescriptionUnavailableMessage(self.config.Current()), nil
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
	if err := self.withFilesAndPreviews(ctx, principal.User.ID, photos); err != nil {
		return nil, translateError(err)
	}
	return photos, nil
}

func (self *graph) PhotoPlaybackAddress(ctx context.Context, arguments PhotoPlaybackAddressArguments) (string, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return "", err
	}
	if (arguments.PhotoPreviewSize == nil) == (arguments.PhotoFileRole == nil) {
		return "", fmt.Errorf("%w: name a photoPreviewSize or a photoFileRole, not both and not neither", api.ErrInvalidArguments)
	}
	secret := []byte(nil)
	if self.config != nil {
		secret = self.config.Current().Secret()
	}
	if len(secret) == 0 {
		return "", errors.New("the server has no secret to sign an address with")
	}
	transaction := self.transaction(ctx)
	var path, playbackKind, name string
	until := time.Now().Add(photo.PlaybackFor)
	if arguments.PhotoPreviewSize != nil {
		if !arguments.PhotoPreviewSize.IsValid() {
			return "", fmt.Errorf("%w: photoPreviewSize is small, large or video", api.ErrInvalidArguments)
		}
		if _, err := transaction.GetPhotoPreview(principal.User.ID, arguments.PhotoID, *arguments.PhotoPreviewSize); err != nil {
			return "", translateError(err)
		}
		path, playbackKind, name = api.PhotoPlaybackPreviewPath(arguments.PhotoID, string(*arguments.PhotoPreviewSize)), photo.PlaybackKindPreview, string(*arguments.PhotoPreviewSize)
	} else {
		if !arguments.PhotoFileRole.IsValid() {
			return "", fmt.Errorf("%w: photoFileRole is original, edited or liveVideo", api.ErrInvalidArguments)
		}
		if _, err := transaction.GetPhotoFile(principal.User.ID, arguments.PhotoID, *arguments.PhotoFileRole); err != nil {
			return "", translateError(err)
		}
		path, playbackKind, name = api.PhotoPlaybackFilePath(arguments.PhotoID, string(*arguments.PhotoFileRole)), photo.PlaybackKindFile, string(*arguments.PhotoFileRole)
	}
	signature := photo.SignPlayback(secret, principal.User.ID, arguments.PhotoID, playbackKind, name, until)
	return path + "?owner=" + url.QueryEscape(principal.User.ID) + "&signature=" + url.QueryEscape(signature), nil
}

func (self *graph) UpdatePhoto(ctx context.Context, arguments UpdatePhotoArguments) (*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	if arguments.IsFavorite == nil && arguments.TakenAt == nil {
		return nil, fmt.Errorf("%w: say what to change", api.ErrInvalidArguments)
	}
	update := &db.PhotoUpdate{IsFavorite: arguments.IsFavorite, TakenAt: arguments.TakenAt}
	if arguments.TakenAt != nil {
		// The date comes with the offset it was written in, and replaces the
		// offset of the date it corrects.
		_, offsetSeconds := arguments.TakenAt.Zone()
		offsetMinutes := offsetSeconds / 60
		update.TakenAtOffsetMinutes = &offsetMinutes
	}
	updated, err := self.transaction(ctx).UpdatePhoto(principal.User.ID, arguments.PhotoID, update)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.withFilesAndPreviews(ctx, principal.User.ID, []*models.Photo{updated}); err != nil {
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

func (self *graph) RetryPhotoProcessing(ctx context.Context, arguments PhotoIDsArguments) (int, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return 0, err
	}
	retriedCount, err := self.transaction(ctx).RetryPhotoProcessing(principal.User.ID, arguments.PhotoIDs)
	return retriedCount, translateError(err)
}

func (self *graph) DescribePhoto(ctx context.Context, arguments DescribePhotoArguments) (*models.Photo, error) {
	principal, err := self.requirePhotosPerson(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	found, err := transaction.GetPhoto(principal.User.ID, arguments.PhotoID)
	if err != nil {
		return nil, translateError(err)
	}
	isRedescribed := arguments.IsRedescribed != nil && *arguments.IsRedescribed
	// A description that is there, or on its way, is not asked for again.
	isAlreadyDescribed := found.DescribedAt != nil && !isRedescribed
	if !isAlreadyDescribed && !found.IsDescriptionRequested {
		unavailableMessage, err := self.photoDescriptionUnavailableMessage(ctx, principal.User.ID)
		if err != nil {
			return nil, translateError(err)
		}
		if unavailableMessage != "" {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, unavailableMessage)
		}
		assistant, err := transaction.GetAgentByUser(principal.User.ID)
		if err != nil {
			return nil, translateError(err)
		}
		if _, err := self.agentWorker().QueuePhotoDescription(transaction, assistant.ID, found.ID); err != nil {
			return nil, translateError(err)
		}
		if found, err = transaction.GetPhoto(principal.User.ID, found.ID); err != nil {
			return nil, translateError(err)
		}
	}
	if err := self.withFilesAndPreviews(ctx, principal.User.ID, []*models.Photo{found}); err != nil {
		return nil, translateError(err)
	}
	return found, nil
}

// SavePhotoFromAttachment keeps a file of the person's conversation with
// their agent as a photo. The photo is named by the digest of the file, so
// saving the same file again finds the photo that is already there.
//
// Reading the file and storing it takes as long as the file is big, so this
// mutation does not run inside the request's transaction (see
// isTransactionFreeMutation): it reads the attachment's row in a short
// transaction of its own, leaves it, and then hashes and stores the file.
func (self *graph) SavePhotoFromAttachment(ctx context.Context, arguments SavePhotoFromAttachmentArguments) (*models.Photo, error) {
	var principal *api.Principal
	var attachment *models.AgentAttachment
	if err := self.inShortTransaction(ctx, func(ctx context.Context) error {
		var err error
		if principal, err = self.requirePhotosPerson(ctx); err != nil {
			return err
		}
		transaction := self.transaction(ctx)
		if attachment, err = transaction.GetAgentAttachment(arguments.AgentAttachmentID); err != nil {
			return translateError(err)
		}
		if attachment == nil {
			return api.ErrNotFound
		}
		assistant, err := transaction.GetAgent(attachment.AgentID)
		if err != nil {
			return translateError(err)
		}
		// A file of somebody else's conversation is not found, rather than
		// refused, because whose it is is not the caller's business.
		if assistant == nil || assistant.UserID != principal.User.ID {
			return api.ErrNotFound
		}
		return nil
	}); err != nil {
		return nil, err
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
