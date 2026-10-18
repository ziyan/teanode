package db

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

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
	// ListPhotos lists a page of the photos the filter keeps: newest first,
	// or best match first when the filter has search words.
	ListPhotos(userId string, filter *PhotoFilter) ([]*models.Photo, error)

	// CountPhotos is how many photos the filter keeps, whatever page it
	// asks for. With search words it is at most the most a search ranks.
	CountPhotos(userId string, filter *PhotoFilter) (int, error)

	// CountUndescribedPhotos is how many of the photos the filter keeps have
	// no description yet. The filter's search words are ignored: a photo
	// nobody has described cannot be found by what is in it, so what this
	// answers is how much of the date or place range a search by words
	// cannot see into.
	CountUndescribedPhotos(userId string, filter *PhotoFilter) (int, error)

	// RequestPhotoDescription records that a description of the photo is
	// wanted, which is what a viewer shows as "describing" until it is
	// written. A photo that is not the person's is not found.
	RequestPhotoDescription(userId, photoId string) error

	// ClearPhotoDescriptionRequest records that the description is no longer
	// on its way, whether it was written or not.
	ClearPhotoDescriptionRequest(userId, photoId string) error

	// SetPhotoDescription keeps what a model said is in the picture,
	// replacing any description, and clears the request. The photo's vectors
	// are deleted, since they were made from the words it replaces, and its
	// search document is made again.
	SetPhotoDescription(userId, photoId string, description *PhotoDescription) error

	// PutPhotoVector keeps the meaning of the photo's description as one
	// model read it, replacing the vectors of other models.
	PutPhotoVector(userId, photoId, model string, vector []float32) error

	// FindPhotosByClientIDs is the photos the person has under those names: the
	// name a photo was made with, and the other names it was later recorded
	// under (see AddPhotoClientAlias). A photo found by another name comes with
	// that name as its ClientPhotoID, so that an uploader matching what it gets
	// to the names it asked for finds it.
	FindPhotosByClientIDs(userId string, clientPhotoIds []string) ([]*models.Photo, error)

	// FindPhotoByOriginalBlob is the person's oldest photo, in the library or
	// the Trash, whose original file is the blob. ErrNotFound when there is none.
	FindPhotoByOriginalBlob(userId, blobSha256 string) (*models.Photo, error)

	// AddPhotoClientAlias records another name an uploader has for the photo, so
	// that an upload of the same picture under that name is the photo. A name
	// that is taken already is left as it is.
	AddPhotoClientAlias(userId, photoId, clientPhotoId string) error

	// ReleasePhotoClientAlias takes a name off the photo it was recorded as
	// another name of, so that the name can be the own name of a photo made
	// for it. The value says there was such a name: a name that is the own
	// name of a photo, or is not an alias at all, is left as it is.
	ReleasePhotoClientAlias(userId, clientPhotoId string) (bool, error)

	// UpdatePhoto corrects a photo. A new date is stored with the offset that
	// came with it, in the same statement, so a date never keeps the offset of
	// the one it replaced.
	UpdatePhoto(userId, photoId string, update *PhotoUpdate) (*models.Photo, error)
	TrashPhotos(userId string, photoIds []string) (int, error)
	RestorePhotos(userId string, photoIds []string) (int, error)

	// MarkPhotosForDeletion deletes forever the named photos that are in the
	// trash, as far as the person can tell: they leave the trash and every
	// listing at once, and their name is freed for an upload of the same file.
	// The rows and the previews' bytes are deleted by cleanup, in batches, so
	// that a trash of thousands is not one request's transaction. It says
	// how many photos there were.
	MarkPhotosForDeletion(userId string, photoIds []string) (int, error)

	// MarkTrashForDeletion does the same to everything in the person's trash.
	MarkTrashForDeletion(userId string) (int, error)

	// ClaimPhotosForDeletion takes up to limit photos for cleanup to delete:
	// those marked, and those of any account that have been in the trash since
	// before expiredBefore, which it marks. A zero expiredBefore takes only
	// the marked. The photos stay readable by cleanup alone, so a crash in the
	// middle of deleting them is found and finished by the next pass.
	ClaimPhotosForDeletion(expiredBefore time.Time, limit int) ([]*models.Photo, error)

	// DeleteMarkedPhotos deletes the rows of marked photos, with their files
	// and previews, and says how many there were. A photo that is not marked is
	// left alone. The files' blobs are deleted by cleanup once nothing refers
	// to them. The groups of faces the photos' faces were in are worked out
	// again in the same transaction, so that a group does not keep the
	// vectors of faces that are gone, and a group left with no face is
	// deleted.
	DeleteMarkedPhotos(photoIds []string) (int, error)

	ReadPhotoLibraryCounts(userId string) (*PhotoLibraryCounts, error)

	// PhotoUsedBytes is the size of every file of every photo the account
	// has, those in the trash included and those being deleted not.
	PhotoUsedBytes(userId string) (int64, error)

	// PhotoOpenUploadBytes is how many bytes the account's uploads that are
	// still receiving hold, which count towards the account's limit before
	// they are photos. Only uploads a part arrived in after the time count: an
	// upload that has sat idle is not going to be finished soon, and cleanup
	// deletes it a week on, so it must not hold the room for a week. The
	// uploads of the named uploader photo and file role are left out when both
	// are given, because starting that upload again ends the ones before it.
	PhotoOpenUploadBytes(userId, exceptClientPhotoId string, exceptPhotoFileRole models.PhotoFileRole, updatedAfter time.Time) (int64, error)

	// PhotoOldestOpenUploadAt is when a part last arrived in the oldest of the
	// uploads PhotoOpenUploadBytes counts with the same arguments, which is
	// when its bytes stop being counted once it has sat idle for a while. The
	// value says there is one.
	PhotoOldestOpenUploadAt(userId, exceptClientPhotoId string, exceptPhotoFileRole models.PhotoFileRole, updatedAfter time.Time) (time.Time, bool, error)

	// SetPhotoFile records a file of a photo, replacing the one in that role.
	// The size is checked against the account's limit, the photo is queued for
	// processing when the file is one its previews are made from, and the
	// first value says the file differs from the one it replaced.
	SetPhotoFile(userId, photoId string, file *models.PhotoFile, maximumAccountBytes int64) (bool, error)
	GetPhotoFile(userId, photoId string, photoFileRole models.PhotoFileRole) (*models.PhotoFile, error)
	ListPhotoFiles(userId string, photoIds []string) ([]*models.PhotoFile, error)

	GetPhotoBlob(blobSha256 string) (*models.PhotoBlob, error)

	// InsertPhotoBlobIfAbsent records a blob whose bytes are stored. A blob
	// marked for deletion is unmarked and given a new age, so that cleanup
	// leaves it for the hour a file needs to name it. The value says a row was
	// made, as opposed to found.
	InsertPhotoBlobIfAbsent(blob *models.PhotoBlob) (bool, error)

	// MarkUnreferencedPhotoBlobDeleting finds the blob that no file has named
	// since before the time, oldest first, and marks it. Blobs in
	// exceptBlobSha256s are passed over: they are the ones a caller failed to
	// delete this time, so that one that cannot be deleted does not hold up the
	// rest. The mark is what a file arriving with the same bytes sees, and
	// stores them again for; the bytes are deleted after the mark is committed,
	// and the row by a second transaction, so that a crash between leaves a mark
	// and not a row whose bytes are gone.
	MarkUnreferencedPhotoBlobDeleting(unreferencedBefore time.Time, exceptBlobSha256s []string) (*models.PhotoBlob, error)

	// LockPhotoBlob takes, until the transaction ends, the lock that orders
	// everything that decides whether a blob's bytes are wanted: a file being
	// recorded against them, and cleanup deleting them. Whoever holds it sees
	// the blob, its mark, its files and its object as nobody else is changing
	// them.
	LockPhotoBlob(blobSha256 string) error

	// LockPhotoBlobForDeletion takes that lock, then locks a marked blob and
	// checks again that no file names it. A blob some file names after all, or
	// whose mark a file arriving with the same bytes cleared, comes back as nil
	// with its mark cleared; that is not an error, because an error would roll
	// the clearing back.
	LockPhotoBlobForDeletion(blobSha256 string) (*models.PhotoBlob, error)
	DeletePhotoBlob(blobSha256 string) error

	UpsertPhotoPreview(userId string, preview *models.PhotoPreview) (*models.PhotoPreview, error)
	GetPhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) (*models.PhotoPreview, error)
	ListPhotoPreviews(userId string, photoIds []string) ([]*models.PhotoPreview, error)
	DeletePhotoPreview(userId, photoId string, photoPreviewSize models.PhotoPreviewSize) error

	// DeletePhotoPreviewMadeAt deletes the preview's row only if it is the one
	// made at that time, which is how a worker tells its own row from one that
	// a later worker wrote over it. The value says a row was deleted.
	DeletePhotoPreviewMadeAt(userId, photoId string, photoPreviewSize models.PhotoPreviewSize, madeAt time.Time) (bool, error)

	// UpdatePhotoMetadata records what reading a photo's file found. What the
	// photo already has wins over what the file says, because the uploader's
	// headers and the person's corrections were put there first, and the
	// answer to that is decided here, in the one statement that writes, so
	// that a correction made while the file was being read is not undone.
	// The one thing the file adds to a date the photo already has is its
	// offset, when the photo has none and the file's date is the same
	// moment.
	UpdatePhotoMetadata(photoId string, metadata *PhotoMetadata) error

	// ClaimPhotosForProcessing takes up to limit photos that wait for their
	// previews and are due: those nobody has claimed, and those whose claim is
	// older than the time, which the instance that made it did not finish,
	// except those that wait out a backoff. Claiming counts an attempt, so a
	// file that kills the process each time it is read runs out of attempts. A
	// photo that has used all of them and is claimed again (its last claim ran
	// out) is failed instead of returned. Each photo comes back with the time of
	// its claim in ProcessingClaimedAt, which is what the claim is known by: the
	// functions below that take it act only while the photo is still claimed at
	// that time, so work that was overtaken (the claim ran out and another
	// instance took the photo, or the photo's file was replaced) cannot record
	// its outcome over the newer work's.
	// A caller that cannot make videos at the moment (the program that does is
	// not installed) says so with isVideoSkipped, and is given no video.
	ClaimPhotosForProcessing(limit int, staleBefore time.Time, maximumAttemptCount int, isVideoSkipped bool) ([]*models.Photo, error)

	// MarkPhotoReady records that previews are made, and clears the attempts
	// and the failure. The value says the photo was still claimed at claimedAt;
	// when it is false nothing was recorded, and the photo is somebody else's
	// to finish. That is not an error.
	MarkPhotoReady(photoId string, claimedAt time.Time) (bool, error)

	// ReleasePhotoProcessingClaim gives up a claim without an outcome, as a
	// worker does that was stopped: the attempt claiming counted is given back
	// and the photo is claimable at once. The value says the photo was still
	// claimed at claimedAt.
	ReleasePhotoProcessingClaim(photoId string, claimedAt time.Time) (bool, error)

	// RetryPhotoProcessing puts the named photos that are failed, or ready (to
	// make their previews again), back to waiting with a fresh count of
	// attempts and no wait, and says how many it moved. Photos that are
	// waiting already, in the trash or somebody else's are left alone.
	RetryPhotoProcessing(userId string, photoIds []string) (int, error)

	// RenewPhotoProcessingClaim renews the claim made at claimedAt on a photo
	// an instance is still working on, so that a long video is not taken by
	// another instance while it is being made. A renewed claim is known by the
	// time it was renewed at, which the caller chooses and so knows even when
	// the answer to the renewal is lost; the value says the photo was still
	// claimed at claimedAt, and is false when the claim was lost.
	RenewPhotoProcessingClaim(photoId string, claimedAt, renewedAt time.Time) (bool, error)

	// RequeuePhotosWithoutPreviews puts up to limit photos that are ready
	// and have no small preview back to waiting, and says how many there
	// were: photos marked ready before previews were made, or whose previews
	// were lost.
	RequeuePhotosWithoutPreviews(limit int) (int, error)

	// MarkPhotoProcessingFailed records a failure of an attempt that claiming
	// has counted. A failure that is the file's fault keeps the count: after
	// the maximum the photo is failed and stays so, and before it the photo
	// waits for a backoff of 1, 5, 15 and then 60 minutes. A failure that is
	// not the file's (the store, the database or a program the server lacks)
	// gives the attempt back, so that an outage does not use up a photo's
	// attempts; the photo waits 1, 5, 15 and then 60 minutes, by how many such
	// failures there have been in a row. The message is kept as given and must
	// be plain: no path and no output of a tool. As with MarkPhotoReady, the value says the photo was
	// still claimed at claimedAt; when it is false nothing was recorded.
	MarkPhotoProcessingFailed(photoId string, claimedAt time.Time, errorMessage string, isFileAtFault bool, maximumAttemptCount int) (bool, error)
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

	// IsTrashed lists the trash instead of the library, newest moved to the
	// trash first rather than newest taken first (a search is still ordered
	// by how well it matches).
	IsTrashed bool

	PhotoKind models.PhotoKind

	// TakenAfter and TakenBefore bound the date a photo is placed at: when it
	// was taken, or when it arrived if nothing says when it was taken.
	TakenAfter  *time.Time
	TakenBefore *time.Time

	// SearchText keeps photos that match the words: a word of the place name,
	// the file name, the camera, an album's title, the description or the
	// words read in the picture, or, for the fixed fields, a part of one. The
	// result is ordered by how well it matches instead of by date, and holds
	// at most MaximumRankedPhotos photos, so a page is reached by Offset or
	// by BeforePhotoID in that order.
	SearchText string

	// SearchMeaning is SearchText as a vector, when the caller has an
	// embedding model. Described photos whose description means something
	// like it are found too, and ranked together with the matches by words.
	SearchMeaning *PhotoSearchMeaning

	// NearLatitude, NearLongitude and NearDistanceKilometers keep photos
	// taken within that distance of a point. All three or none.
	NearLatitude           *float64
	NearLongitude          *float64
	NearDistanceKilometers *float64

	// AlbumID keeps the photos of one album, whoever added them. An album the
	// person does not reach is not found. In an album the photos that are not
	// the person's come without their places, unless the share that reaches the
	// person includes them, and the trash and the favorites are the person's own.
	AlbumID string

	// FaceGroupID keeps the photos with a face in one group of faces; a group
	// that is not the person's is not found. ContactID keeps the photos with a
	// face in any group that contact names.
	FaceGroupID string
	ContactID   string
}

// PhotoSearchMeaning is some words as one embedding model read them.
type PhotoSearchMeaning struct {
	ModelName string
	Vector    []float32
}

// PhotoDescription is what a model wrote about a picture.
type PhotoDescription struct {
	// PhotoDescription is the sentences, and VisibleText the words it could
	// read in the picture.
	PhotoDescription string
	VisibleText      string

	// ModelName is the model that wrote them.
	ModelName string
}

// The most a description and the words read in a picture hold, in
// characters. A job cuts what a model wrote to these before it is kept.
const (
	MaximumPhotoDescriptionCharacters = 4000
	MaximumPhotoVisibleTextCharacters = 8000
)

// MaximumRankedPhotos is how many photos a search by words ranks. A search
// asks for the best, and the best are on the first pages.
const MaximumRankedPhotos = 500

// PhotoVectorTable is where the meaning of photos' descriptions is kept.
var PhotoVectorTable = VectorTable{Table: "photo_vector", IDColumn: "photo_id", ScopeColumn: "user_id", OrderColumn: "created_at", Candidates: 5000}

// photoMeaningFloor is the least similarity worth calling a match, the same
// as the graph's: unrelated text is a tenth to three tenths apart.
const photoMeaningFloor = 0.25

// photoSearchFusionConstant is the k of reciprocal rank fusion, as the
// knowledge search uses it.
const photoSearchFusionConstant = 60

// PhotoUpdate is what a person may correct about a photo. Fields left nil
// are kept.
type PhotoUpdate struct {
	IsFavorite *bool

	// TakenAt is the new date, and TakenAtOffsetMinutes the offset that came
	// with it; they are stored together. An offset needs a date.
	TakenAt              *time.Time
	TakenAtOffsetMinutes *int
}

// PhotoMetadata is what is read from a photo's file. Zero values say the file
// did not say: the photo keeps what it has.
type PhotoMetadata struct {
	// OwnerLocation is the owner's time zone, which gives an instant to a
	// date the file wrote as a wall clock, and an offset to a date that has
	// none. Nil is UTC.
	OwnerLocation *time.Location

	// IsTakenAtWallClock says TakenAt, which has no offset, is a reading of a
	// clock held as if it were UTC (a picture's EXIF date), and not an
	// instant that is UTC (a video container's creation time).
	IsTakenAtWallClock bool

	PhotoKind            models.PhotoKind
	WidthPixels          int
	HeightPixels         int
	DurationMS           int64
	TakenAt              *time.Time
	TakenAtOffsetMinutes *int
	Latitude             *float64
	Longitude            *float64
	PlaceName            string
	CameraMake           string
	CameraModel          string
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

// photoClientAliasModel is a name an uploader has for a photo besides the one
// the photo was made with.
type photoClientAliasModel struct {
	UserID        string    `gorm:"column:user_id;primaryKey"`
	ClientPhotoID string    `gorm:"column:client_photo_id;primaryKey"`
	PhotoID       string    `gorm:"column:photo_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (photoClientAliasModel) TableName() string { return "photo_client_alias" }

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
	photo.ProcessingNotBefore = utcTime(photo.ProcessingNotBefore)
	photo.DescribedAt = utcTime(photo.DescribedAt)
	photo.DescriptionRequestedAt = utcTime(photo.DescriptionRequestedAt)
	photo.IsDescriptionRequested = photo.DescriptionRequestedAt != nil && time.Since(*photo.DescriptionRequestedAt) < models.PhotoDescriptionRequestLifetime
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

// photoMarkedForDeletion is the time a photo in the trash is given when it is
// deleted forever. No photo was taken before 1970, so it can never be a time
// a photo was trashed at, and "in the trash" is "trashed after it".
// Keeping the mark in a column that is there avoids a second column every
// query would have to agree about.
var photoMarkedForDeletion = time.Unix(0, 0).UTC()

// photoIsReadable keeps the photos that are in the library or in the trash,
// not those on their way to deletion.
const photoIsReadable = "(photo.trashed_at IS NULL OR photo.trashed_at > ?)"

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
	row.ProcessingClaimedAt, row.ProcessingNotBefore = nil, nil
	row.TrashedAt = nil
	row.PhotoDescription, row.DescriptionVisibleText, row.DescriptionModel = "", "", ""
	row.DescribedAt, row.DescriptionRequestedAt = nil, nil
	// Two requests with the same uploader name race to this insert; the
	// unique key decides, and the loser reads the winner's row.
	inserted := self.tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "client_photo_id"}}, DoNothing: true}).Create(&row)
	if inserted.Error != nil {
		return nil, false, inserted.Error
	}
	if inserted.RowsAffected == 1 {
		if err := self.refreshPhotoSearchDocumentsOf([]string{row.ID}); err != nil {
			return nil, false, err
		}
		return photoFromModel(&row), true, nil
	}
	var existing photoModel
	isFound, err := findOneRow(self.tx.Where("user_id = ? AND client_photo_id = ?", photo.UserID, photo.ClientPhotoID), &existing)
	if err != nil {
		return nil, false, err
	}
	if !isFound {
		return nil, false, gorm.ErrRecordNotFound
	}
	return photoFromModel(&existing), false, nil
}

func (self *transaction) GetPhoto(userId, photoId string) (*models.Photo, error) {
	if userId == "" || photoId == "" {
		return nil, ErrNotFound
	}
	var row photoModel
	isFound, err := findOneRow(self.tx.Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, photoId, userId, photoMarkedForDeletion), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return photoFromModel(&row), nil
}

// photoTakenAtAgreementSeconds is how far apart the instant an uploader sent
// and the instant a file's own date and offset make may be for the file's
// offset to be taken as the photo's. Both are written to the second; the
// margin covers a writer that rounds the other way.
const photoTakenAtAgreementSeconds = 60

// findOneRow reads the first row the query matches into row and says
// whether there was one. It is how the photo queries ask "is there one?":
// First and Take log "record not found" when the answer is no, and the
// periodic jobs ask on every idle pass.
func findOneRow(query *gorm.DB, row any) (bool, error) {
	result := query.Limit(1).Find(row)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// maximumPhotoOffset bounds how far into the library a page is reached by
// skipping, because skipping reads every photo it skips.
const maximumPhotoOffset = 100000

// photoScope is which photos a listing reads and what it may reveal of them.
// A listing without an album reads the person's own library. A listing of an
// album reads the photos in it, whoever added them, and the person who asks
// sees the place of only those that are theirs, unless the share that reaches
// them includes places. A listing must not give a place away by other means
// either: a filter by distance and a search by the words of a place would each
// tell a reader where a photo was taken, so for the photos whose place is
// held back they leave the place out.
type photoScope struct {
	readerUserId string

	// isInAlbum says the listing is of one album, photoAlbumId.
	isInAlbum    bool
	photoAlbumId string

	// isPlaceHeldBack says the place of the photos that are not the reader's
	// own is not the reader's to learn.
	isPlaceHeldBack bool
}

// ownPhotoCondition keeps the reader's own photos.
func (self photoScope) ownPhotoCondition() (string, []any) {
	return "photo.user_id = ?", []any{self.readerUserId}
}

// scopeOfPhotoListing works out what a listing reads. An album the person does
// not reach is not found.
func (self *transaction) scopeOfPhotoListing(userId string, filter *PhotoFilter) (photoScope, error) {
	scope := photoScope{readerUserId: userId}
	if filter.AlbumID == "" {
		return scope, nil
	}
	access, err := self.GetPhotoAlbumAccess(userId, filter.AlbumID)
	if err != nil {
		return scope, err
	}
	scope.isInAlbum, scope.photoAlbumId, scope.isPlaceHeldBack = true, filter.AlbumID, !access.IsLocationIncluded
	return scope, nil
}

// photosMatching is the photos the filter keeps, before any paging. Listing
// and counting both start here, so that they cannot disagree about what a
// filter means.
func (self *transaction) photosMatching(userId string, filter *PhotoFilter) (*gorm.DB, error) {
	query, _, err := self.photosMatchingInScope(userId, filter)
	return query, err
}

func (self *transaction) photosMatchingInScope(userId string, filter *PhotoFilter) (*gorm.DB, photoScope, error) {
	if userId == "" {
		return nil, photoScope{}, ErrNotFound
	}
	if filter == nil {
		return nil, photoScope{}, ErrInvalidArguments
	}
	scope, err := self.scopeOfPhotoListing(userId, filter)
	if err != nil {
		return nil, scope, err
	}
	query := self.tx.Model(&photoModel{})
	if scope.isInAlbum {
		query = query.Where("EXISTS (SELECT 1 FROM photo_album_item WHERE photo_album_item.photo_id = photo.id AND photo_album_item.photo_album_id = ?)", filter.AlbumID)
	} else {
		query = query.Where("photo.user_id = ?", userId)
	}
	query, err = self.photoFaceConditions(query, userId, filter)
	if err != nil {
		return nil, scope, err
	}
	if filter.IsTrashed {
		// The trash is the owner's alone, in an album as in the library.
		query = query.Where("photo.trashed_at > ?", photoMarkedForDeletion)
		if scope.isInAlbum {
			query = query.Where("photo.user_id = ?", userId)
		}
	} else {
		query = query.Where("photo.trashed_at IS NULL")
	}
	if filter.IsFavorite != nil {
		// A favorite is a mark in the owner's library; on somebody else's
		// photo it is not shown and not asked about.
		switch {
		case !scope.isInAlbum:
			query = query.Where("photo.is_favorite = ?", *filter.IsFavorite)
		case *filter.IsFavorite:
			query = query.Where("photo.user_id = ? AND photo.is_favorite", userId)
		default:
			query = query.Where("(photo.user_id <> ? OR NOT photo.is_favorite)", userId)
		}
	}
	if filter.PhotoKind != "" {
		if !filter.PhotoKind.IsValid() {
			return nil, scope, ErrInvalidArguments
		}
		query = query.Where("photo.photo_kind = ?", filter.PhotoKind)
	}
	if filter.TakenAfter != nil {
		query = query.Where(photoTimelineExpression+" >= ?", *filter.TakenAfter)
	}
	if filter.TakenBefore != nil {
		query = query.Where(photoTimelineExpression+" < ?", *filter.TakenBefore)
	}
	hasNear := filter.NearLatitude != nil || filter.NearLongitude != nil || filter.NearDistanceKilometers != nil
	if hasNear {
		if filter.NearLatitude == nil || filter.NearLongitude == nil || filter.NearDistanceKilometers == nil || *filter.NearDistanceKilometers <= 0 ||
			*filter.NearLatitude < -90 || *filter.NearLatitude > 90 || *filter.NearLongitude < -180 || *filter.NearLongitude > 180 {
			return nil, scope, ErrInvalidArguments
		}
		// The great-circle distance by the haversine formula, on a sphere of
		// the earth's mean radius: good to a fraction of a percent, which is
		// plenty for "photos near here".
		query = query.Where(`photo.latitude IS NOT NULL AND photo.longitude IS NOT NULL AND
			12742 * asin(sqrt(least(1, power(sin(radians(photo.latitude - ?) / 2), 2) +
				cos(radians(?)) * cos(radians(photo.latitude)) * power(sin(radians(photo.longitude - ?) / 2), 2)))) <= ?`,
			*filter.NearLatitude, *filter.NearLatitude, *filter.NearLongitude, *filter.NearDistanceKilometers)
		if scope.isPlaceHeldBack {
			condition, arguments := scope.ownPhotoCondition()
			query = query.Where(condition, arguments...)
		}
	}
	return query, scope, nil
}

func (self *transaction) ListPhotos(userId string, filter *PhotoFilter) ([]*models.Photo, error) {
	query, err := self.photosMatching(userId, filter)
	if err != nil {
		return nil, err
	}
	if filter.PageSize < 1 || filter.PageSize > 500 || filter.Offset < 0 || filter.Offset > maximumPhotoOffset || (filter.Offset > 0 && filter.BeforePhotoID != "") {
		return nil, ErrInvalidArguments
	}
	if strings.TrimSpace(filter.SearchText) != "" {
		return self.listRankedPhotos(userId, filter)
	}
	// The library runs newest first by the date a photo is placed at; the
	// Trash runs newest first by when the photo was moved there, which is the
	// order its groups are read in, so that a page never ends in the middle of
	// an older group's days.
	orderExpression := photoTimelineExpression
	if filter.IsTrashed {
		orderExpression = "photo.trashed_at"
	}
	if filter.BeforePhotoID != "" {
		var cursor struct {
			PlacedAt *time.Time
			ID       string
		}
		cursorQuery := self.tx.Model(&photoModel{}).Select(orderExpression+" AS placed_at, photo.id").Where("photo.id = ?", filter.BeforePhotoID)
		if filter.AlbumID == "" {
			cursorQuery = cursorQuery.Where("photo.user_id = ?", userId)
		} else {
			cursorQuery = cursorQuery.Where("EXISTS (SELECT 1 FROM photo_album_item WHERE photo_album_item.photo_id = photo.id AND photo_album_item.photo_album_id = ?)", filter.AlbumID)
		}
		isFound, err := findOneRow(cursorQuery, &cursor)
		if err != nil {
			return nil, err
		}
		if !isFound || cursor.PlacedAt == nil {
			return nil, ErrNotFound
		}
		// In the Trash, a photo deleted forever since the last page keeps its row
		// until cleanup, trashed at the epoch: nothing in the Trash is before it,
		// and the page after it would be empty, ending the listing early. It is
		// not found, as a photo restored since is, and the caller starts again.
		if filter.IsTrashed && !cursor.PlacedAt.After(photoMarkedForDeletion) {
			return nil, ErrNotFound
		}
		query = query.Where("("+orderExpression+", photo.id) < (?, ?)", *cursor.PlacedAt, cursor.ID)
	}
	var rows []photoModel
	if err := query.Order(orderExpression + " DESC, photo.id DESC").Offset(filter.Offset).Limit(filter.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	return photosFromModels(rows), nil
}

func (self *transaction) CountPhotos(userId string, filter *PhotoFilter) (int, error) {
	if filter != nil && strings.TrimSpace(filter.SearchText) != "" {
		rankedIds, err := self.rankPhotos(userId, filter, MaximumRankedPhotos)
		return len(rankedIds), err
	}
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
	if len(clientPhotoIds) == 0 {
		return photosFromModels(rows), nil
	}
	if err := self.tx.Where("user_id = ? AND client_photo_id IN ?", userId, clientPhotoIds).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	photos := photosFromModels(rows)
	var aliases []photoClientAliasModel
	if err := self.tx.Where("user_id = ? AND client_photo_id IN ?", userId, clientPhotoIds).Find(&aliases).Error; err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return photos, nil
	}
	aliasedPhotoIds := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		aliasedPhotoIds = append(aliasedPhotoIds, alias.PhotoID)
	}
	var aliasedRows []photoModel
	if err := self.tx.Where("user_id = ? AND id IN ? AND "+photoIsReadable, userId, aliasedPhotoIds, photoMarkedForDeletion).Find(&aliasedRows).Error; err != nil {
		return nil, err
	}
	aliasedPhotos := photosFromModels(aliasedRows)
	named := map[string]bool{}
	for _, found := range photos {
		named[found.ClientPhotoID] = true
	}
	for _, alias := range aliases {
		if named[alias.ClientPhotoID] {
			continue
		}
		for _, found := range aliasedPhotos {
			if found.ID == alias.PhotoID {
				renamed := *found
				renamed.ClientPhotoID = alias.ClientPhotoID
				photos = append(photos, &renamed)
			}
		}
	}
	sort.SliceStable(photos, func(left, right int) bool { return photos[left].ID < photos[right].ID })
	return photos, nil
}

func (self *transaction) FindPhotoByOriginalBlob(userId, blobSha256 string) (*models.Photo, error) {
	if userId == "" || !validPhotoSha256(blobSha256) {
		return nil, ErrNotFound
	}
	var row photoModel
	isFound, err := findOneRow(self.tx.
		Joins("JOIN photo_file ON photo_file.photo_id = photo.id AND photo_file.photo_file_role = ?", models.PhotoFileRoleOriginal).
		Where("photo.user_id = ? AND photo_file.blob_sha256 = ? AND "+photoIsReadable, userId, blobSha256, photoMarkedForDeletion).
		Order("photo.created_at, photo.id"), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	return photoFromModel(&row), nil
}

func (self *transaction) AddPhotoClientAlias(userId, photoId, clientPhotoId string) error {
	if userId == "" || photoId == "" || strings.TrimSpace(clientPhotoId) == "" || len(clientPhotoId) > 255 {
		return ErrInvalidArguments
	}
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return err
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&photoClientAliasModel{
		UserID: userId, ClientPhotoID: clientPhotoId, PhotoID: photoId, CreatedAt: photoNow(),
	}).Error
}

func (self *transaction) ReleasePhotoClientAlias(userId, clientPhotoId string) (bool, error) {
	if userId == "" || strings.TrimSpace(clientPhotoId) == "" {
		return false, ErrInvalidArguments
	}
	deleted := self.tx.Where("user_id = ? AND client_photo_id = ? AND NOT EXISTS (SELECT 1 FROM photo WHERE photo.user_id = ? AND photo.client_photo_id = ?)",
		userId, clientPhotoId, userId, clientPhotoId).Delete(&photoClientAliasModel{})
	return deleted.RowsAffected == 1, deleted.Error
}

func (self *transaction) UpdatePhoto(userId, photoId string, update *PhotoUpdate) (*models.Photo, error) {
	if update == nil || (update.TakenAt == nil && update.TakenAtOffsetMinutes != nil) ||
		(update.TakenAtOffsetMinutes != nil && !isPlausibleOffsetMinutes(*update.TakenAtOffsetMinutes)) {
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
		// The date and its offset change together: the old offset belongs to
		// the old date. A date given with no offset has none stored.
		changes["taken_at"] = update.TakenAt.UTC()
		if update.TakenAtOffsetMinutes != nil {
			changes["taken_at_offset_minutes"] = *update.TakenAtOffsetMinutes
		} else {
			changes["taken_at_offset_minutes"] = gorm.Expr("NULL")
		}
	}
	if err := self.tx.Model(&photoModel{}).Where("id = ? AND user_id = ?", photoId, userId).Updates(changes).Error; err != nil {
		return nil, err
	}
	return self.GetPhoto(userId, photoId)
}

// isPlausibleOffsetMinutes says an offset is one a clock has had: nothing is
// further from UTC than 14 hours east or 12 hours west, and a day either way
// is room for any error.
func isPlausibleOffsetMinutes(offsetMinutes int) bool {
	return offsetMinutes >= -24*60 && offsetMinutes <= 24*60
}

func (self *transaction) TrashPhotos(userId string, photoIds []string) (int, error) {
	return self.changePhotoTrash(userId, photoIds, "trashed_at IS NULL", nil, map[string]any{"trashed_at": photoNow(), "updated_at": photoNow()})
}

func (self *transaction) RestorePhotos(userId string, photoIds []string) (int, error) {
	return self.changePhotoTrash(userId, photoIds, "trashed_at > ?", []any{photoMarkedForDeletion}, map[string]any{"trashed_at": nil, "updated_at": photoNow()})
}

func (self *transaction) changePhotoTrash(userId string, photoIds []string, condition string, conditionArguments []any, changes map[string]any) (int, error) {
	if userId == "" || len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	updated := self.tx.Model(&photoModel{}).Where("user_id = ? AND id IN ? AND "+condition, append([]any{userId, photoIds}, conditionArguments...)...).Updates(changes)
	return int(updated.RowsAffected), updated.Error
}

// markedForDeletionChanges is what a photo is given when it is deleted
// forever. Its name for the uploader is freed, so that sending the same file
// again makes a new photo instead of finding one that is about to go; it
// cannot be found by the old name or restored.
func markedForDeletionChanges() map[string]any {
	return map[string]any{"trashed_at": photoMarkedForDeletion, "client_photo_id": gorm.Expr("'deleting-' || id"), "updated_at": photoNow()}
}

func (self *transaction) MarkPhotosForDeletion(userId string, photoIds []string) (int, error) {
	if userId == "" || len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	marked := self.tx.Model(&photoModel{}).Where("user_id = ? AND id IN ? AND trashed_at > ?", userId, photoIds, photoMarkedForDeletion).Updates(markedForDeletionChanges())
	return int(marked.RowsAffected), marked.Error
}

func (self *transaction) MarkTrashForDeletion(userId string) (int, error) {
	if userId == "" {
		return 0, ErrInvalidArguments
	}
	marked := self.tx.Model(&photoModel{}).Where("user_id = ? AND trashed_at > ?", userId, photoMarkedForDeletion).Updates(markedForDeletionChanges())
	return int(marked.RowsAffected), marked.Error
}

func (self *transaction) ClaimPhotosForDeletion(expiredBefore time.Time, limit int) ([]*models.Photo, error) {
	if limit < 1 || limit > 500 {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	if expiredBefore.IsZero() {
		query = query.Where("trashed_at = ?", photoMarkedForDeletion)
	} else {
		query = query.Where("trashed_at = ? OR (trashed_at > ? AND trashed_at < ?)", photoMarkedForDeletion, photoMarkedForDeletion, expiredBefore)
	}
	var rows []photoModel
	if err := query.Order("trashed_at, id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	expiredIds := make([]string, 0, len(rows))
	for index := range rows {
		if rows[index].TrashedAt != nil && rows[index].TrashedAt.After(photoMarkedForDeletion) {
			expiredIds = append(expiredIds, rows[index].ID)
		}
	}
	if len(expiredIds) > 0 {
		if err := self.tx.Model(&photoModel{}).Where("id IN ?", expiredIds).Updates(markedForDeletionChanges()).Error; err != nil {
			return nil, err
		}
	}
	return photosFromModels(rows), nil
}

func (self *transaction) DeleteMarkedPhotos(photoIds []string) (int, error) {
	if len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	// The groups these photos' faces are in, found before the faces go with
	// their photos.
	var groupIds []string
	if err := self.tx.Table("photo_face").Distinct().
		Where("photo_face.photo_face_group_id IS NOT NULL AND photo_face.photo_id IN (SELECT photo.id FROM photo WHERE photo.id IN ? AND photo.trashed_at = ?)", photoIds, photoMarkedForDeletion).
		Pluck("photo_face.photo_face_group_id", &groupIds).Error; err != nil {
		return 0, err
	}
	deleted := self.tx.Where("id IN ? AND trashed_at = ?", photoIds, photoMarkedForDeletion).Delete(&photoModel{})
	if deleted.Error != nil {
		return 0, deleted.Error
	}
	// A group keeps the sum of the vectors of the faces it had, so the faces
	// that are gone are taken out of it, and a group with none left is deleted.
	for _, groupId := range groupIds {
		if err := self.recomputePhotoFaceGroup(groupId); err != nil {
			return 0, err
		}
	}
	if len(groupIds) > 0 {
		if err := self.tx.Exec(`DELETE FROM photo_face_group WHERE id IN ? AND NOT EXISTS (SELECT 1 FROM photo_face WHERE photo_face.photo_face_group_id = photo_face_group.id)`, groupIds).Error; err != nil {
			return 0, err
		}
	}
	return int(deleted.RowsAffected), nil
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
		COUNT(*) FILTER (WHERE trashed_at > ?) AS trashed_count`, models.PhotoKindVideo, models.PhotoKindVideo, photoMarkedForDeletion).Scan(&counts).Error
	return &counts, err
}

func (self *transaction) PhotoUsedBytes(userId string) (int64, error) {
	if userId == "" {
		return 0, ErrNotFound
	}
	var usedBytes int64
	err := self.tx.Table("photo_file").Select("COALESCE(SUM(photo_file.file_size_bytes), 0)").Joins("JOIN photo ON photo.id = photo_file.photo_id").Where("photo.user_id = ? AND "+photoIsReadable, userId, photoMarkedForDeletion).Scan(&usedBytes).Error
	return usedBytes, err
}

// openPhotoUploads are the account's uploads that are still receiving and had
// a part arrive after the time, without the named uploader's file.
func (self *transaction) openPhotoUploads(userId, exceptClientPhotoId string, exceptPhotoFileRole models.PhotoFileRole, updatedAfter time.Time) *gorm.DB {
	query := self.tx.Model(&photoUploadModel{}).
		Where("user_id = ? AND photo_upload_state = ? AND updated_at > ?", userId, models.PhotoUploadReceiving, updatedAfter.UTC())
	if exceptClientPhotoId != "" {
		query = query.Where("NOT (upload_headers->>'clientPhotoId' = ? AND upload_headers->>'photoFileRole' = ?)", exceptClientPhotoId, string(exceptPhotoFileRole))
	}
	return query
}

func (self *transaction) PhotoOpenUploadBytes(userId, exceptClientPhotoId string, exceptPhotoFileRole models.PhotoFileRole, updatedAfter time.Time) (int64, error) {
	if userId == "" {
		return 0, ErrNotFound
	}
	var openBytes int64
	err := self.openPhotoUploads(userId, exceptClientPhotoId, exceptPhotoFileRole, updatedAfter).Select("COALESCE(SUM(received_size_bytes), 0)").Scan(&openBytes).Error
	return openBytes, err
}

func (self *transaction) PhotoOldestOpenUploadAt(userId, exceptClientPhotoId string, exceptPhotoFileRole models.PhotoFileRole, updatedAfter time.Time) (time.Time, bool, error) {
	if userId == "" {
		return time.Time{}, false, ErrNotFound
	}
	var oldestAt *time.Time
	if err := self.openPhotoUploads(userId, exceptClientPhotoId, exceptPhotoFileRole, updatedAfter).Select("MIN(updated_at)").Scan(&oldestAt).Error; err != nil || oldestAt == nil {
		return time.Time{}, false, err
	}
	return oldestAt.UTC(), true, nil
}

func isPhotoFileSourceOfPreviews(photoFileRole models.PhotoFileRole) bool {
	return photoFileRole == models.PhotoFileRoleOriginal || photoFileRole == models.PhotoFileRoleEdited
}

func (self *transaction) SetPhotoFile(userId, photoId string, file *models.PhotoFile, maximumAccountBytes int64) (bool, error) {
	if file == nil || !file.PhotoFileRole.IsValid() || !validPhotoSha256(file.BlobSHA256) || file.FileSizeBytes < 0 || len(file.ContentType) > 128 || len(file.OriginalFilename) > 255 || maximumAccountBytes < 0 {
		return false, ErrInvalidArguments
	}
	// The account's row is locked so that two files arriving together are
	// each counted against the other. Making the photo earlier in this
	// transaction took a key share lock on this row, because the photo refers
	// to it. FOR UPDATE conflicts with key share, so two uploads of one
	// account would each wait for the other's key share lock and deadlock.
	// FOR NO KEY UPDATE does not conflict with key share but still conflicts
	// with itself, so the uploads still take turns.
	var owner userModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id = ?", userId), &owner)
	if err != nil {
		return false, err
	}
	if !isFound {
		return false, ErrNotFound
	}
	photo, err := self.GetPhoto(userId, photoId)
	if err != nil {
		return false, err
	}
	var previous photoFileModel
	hasPrevious, err := findOneRow(self.tx.Where("photo_id = ? AND photo_file_role = ?", photoId, file.PhotoFileRole), &previous)
	if err != nil {
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
		changes["processing_outage_count"] = 0
		changes["processing_error_message"] = ""
		changes["processing_claimed_at"] = nil
		changes["processing_not_before"] = nil
		// The pixels differ, so the faces are found again; the ones the
		// person confirmed are kept where they are still found.
		changes["faces_found_at"] = nil
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
	isFound, err := findOneRow(self.tx.Where("photo_id = ? AND photo_file_role = ?", photoId, photoFileRole), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
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
	isFound, err := findOneRow(self.tx.Where("blob_sha256 = ?", blobSha256), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
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
	// A new blob is unreferenced from the moment it is made. One that was
	// marked and is unmarked gets a new age, so that it keeps for the hour a
	// file needs to name it, unless a file names it already.
	createdAt := photoNow()
	rows, err := self.tx.Raw(`INSERT INTO photo_blob (blob_sha256, blob_size_bytes, created_at, deleting_at, unreferenced_since) VALUES (?, ?, ?, NULL, ?)
		ON CONFLICT (blob_sha256) DO UPDATE SET deleting_at = NULL, created_at = EXCLUDED.created_at,
			unreferenced_since = CASE WHEN EXISTS (SELECT 1 FROM photo_file WHERE photo_file.blob_sha256 = photo_blob.blob_sha256) THEN NULL ELSE EXCLUDED.created_at END
			WHERE photo_blob.deleting_at IS NOT NULL
		RETURNING (xmax = 0)`, blob.BlobSHA256, blob.BlobSizeBytes, createdAt, createdAt).Rows()
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

func (self *transaction) MarkUnreferencedPhotoBlobDeleting(unreferencedBefore time.Time, exceptBlobSha256s []string) (*models.PhotoBlob, error) {
	// The column is kept by a trigger on photo_file and read through an index,
	// so this does not look at the blobs that files name; the check that no file
	// does is a second look at the one blob found.
	query := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("unreferenced_since < ? AND NOT EXISTS (SELECT 1 FROM photo_file WHERE photo_file.blob_sha256 = photo_blob.blob_sha256)", unreferencedBefore.UTC())
	if len(exceptBlobSha256s) > 0 {
		query = query.Where("blob_sha256 NOT IN ?", exceptBlobSha256s)
	}
	var row photoBlobModel
	isFound, err := findOneRow(query.Order("unreferenced_since"), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
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

func (self *transaction) LockPhotoBlob(blobSha256 string) error {
	if !validPhotoSha256(blobSha256) {
		return ErrInvalidArguments
	}
	// A lock on a key made of the digest, not on the row: the row may not be
	// there yet when a file is recorded, and a row lock cannot also cover the
	// object in the store, which is deleted by a call made while it is held.
	// Two digests whose keys collide only wait for each other.
	return self.tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "photo-blob:"+blobSha256).Error
}

func (self *transaction) LockPhotoBlobForDeletion(blobSha256 string) (*models.PhotoBlob, error) {
	if err := self.LockPhotoBlob(blobSha256); err != nil {
		return nil, err
	}
	var row photoBlobModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("blob_sha256 = ?", blobSha256), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
	}
	// A file arriving with the same bytes while the bytes were being deleted
	// cleared the mark and stored them again; they are its to keep.
	if row.DeletingAt == nil {
		return nil, nil
	}
	// Counted after the locks are held and in a statement of its own: a file
	// is recorded under the same advisory lock, so one that was not there a
	// moment ago cannot appear while the lock is ours.
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
	// The photo is locked against being marked for deletion while the
	// preview's row is written, so that cleanup, which lists the previews of
	// a photo after marking it, cannot miss one written in between and leave
	// its bytes with no row to name them.
	var owned photoModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("photo.id = ? AND photo.user_id = ? AND "+photoIsReadable, preview.PhotoID, userId, photoMarkedForDeletion), &owned)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
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
	isFound, err := findOneRow(self.tx.Where("photo_id = ? AND photo_preview_size = ?", photoId, photoPreviewSize), &row)
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, ErrNotFound
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

func (self *transaction) DeletePhotoPreviewMadeAt(userId, photoId string, photoPreviewSize models.PhotoPreviewSize, madeAt time.Time) (bool, error) {
	if _, err := self.GetPhoto(userId, photoId); err != nil {
		return false, err
	}
	deleted := self.tx.Where("photo_id = ? AND photo_preview_size = ? AND created_at = ?", photoId, photoPreviewSize, madeAt.UTC()).Delete(&photoPreviewModel{})
	return deleted.RowsAffected == 1, deleted.Error
}

// photoProcessingBackoffs are the waits after the first, second, third and
// later failures of a photo that are the file's fault.
var photoProcessingBackoffs = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// photoProcessingBackoff is how long a photo waits after its attempt of that
// number failed.
func photoProcessingBackoff(attemptCount int) time.Duration {
	if attemptCount < 1 {
		return photoProcessingBackoffs[0]
	}
	return photoProcessingBackoffs[min(attemptCount, len(photoProcessingBackoffs))-1]
}

// photoProcessingInterruptedMessage is what a photo is given when it used all
// its attempts and was claimed again: the instance that had it never said how
// it went, which for a file that is always read the same way means reading it
// stops the process.
const photoProcessingInterruptedMessage = "The file could not be processed."

func (self *transaction) ClaimPhotosForProcessing(limit int, staleBefore time.Time, maximumAttemptCount int, isVideoSkipped bool) ([]*models.Photo, error) {
	if limit < 1 || limit > 100 || maximumAttemptCount < 1 {
		return nil, ErrInvalidArguments
	}
	// A photo with only a Live Photo's video has nothing to make previews
	// from yet; it waits for its picture to arrive. One that failed waits out
	// its backoff.
	claimedAt := photoNow()
	claimed := make([]photoModel, 0, limit)
	// A photo that is failed instead of claimed does not count towards the
	// limit, so more are looked at until the limit is reached or none wait.
	// Each round changes every photo it looks at, so it ends.
	for len(claimed) < limit {
		var rows []photoModel
		query := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`photo_processing_state = ? AND (processing_claimed_at IS NULL OR processing_claimed_at < ?)
				AND (processing_not_before IS NULL OR processing_not_before <= ?)
				AND `+photoIsReadable+`
				AND EXISTS (SELECT 1 FROM photo_file WHERE photo_file.photo_id = photo.id AND photo_file.photo_file_role IN ?)`,
				models.PhotoProcessingWaiting, staleBefore, claimedAt, photoMarkedForDeletion, []models.PhotoFileRole{models.PhotoFileRoleOriginal, models.PhotoFileRoleEdited})
		if isVideoSkipped {
			query = query.Where("photo_kind <> ?", models.PhotoKindVideo)
		}
		if err := query.Order("created_at, id").Limit(limit - len(claimed)).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for index := range rows {
			if rows[index].ProcessingAttemptCount >= maximumAttemptCount {
				// Every attempt was claimed and none ended in a record: the
				// process died each time.
				if err := self.tx.Model(&photoModel{}).Where("id = ?", rows[index].ID).Updates(map[string]any{
					"photo_processing_state":   models.PhotoProcessingFailed,
					"processing_error_message": photoProcessingInterruptedMessage,
					"processing_claimed_at":    nil,
					"processing_not_before":    nil,
					"updated_at":               claimedAt,
				}).Error; err != nil {
					return nil, err
				}
				continue
			}
			rows[index].ProcessingAttemptCount++
			if err := self.tx.Model(&photoModel{}).Where("id = ?", rows[index].ID).Updates(map[string]any{
				"processing_claimed_at":    claimedAt,
				"processing_attempt_count": rows[index].ProcessingAttemptCount,
			}).Error; err != nil {
				return nil, err
			}
			rows[index].ProcessingClaimedAt = &claimedAt
			claimed = append(claimed, rows[index])
		}
	}
	return photosFromModels(claimed), nil
}

// claimedPhoto is the photo whose claim is the one at claimedAt, which is the
// condition every function that records the end of the work on a claim holds
// to.
const claimedPhoto = "id = ? AND photo_processing_state = 'waiting' AND processing_claimed_at = ?"

func (self *transaction) MarkPhotoReady(photoId string, claimedAt time.Time) (bool, error) {
	marked := self.tx.Model(&photoModel{}).Where(claimedPhoto, photoId, claimedAt.UTC()).Updates(map[string]any{
		"photo_processing_state":   models.PhotoProcessingReady,
		"processing_attempt_count": 0,
		"processing_outage_count":  0,
		"processing_error_message": "",
		"processing_claimed_at":    nil,
		"processing_not_before":    nil,
		"updated_at":               photoNow(),
	})
	return marked.RowsAffected > 0, marked.Error
}

func (self *transaction) ReleasePhotoProcessingClaim(photoId string, claimedAt time.Time) (bool, error) {
	released := self.tx.Model(&photoModel{}).Where(claimedPhoto, photoId, claimedAt.UTC()).Updates(map[string]any{
		"processing_attempt_count": gorm.Expr("GREATEST(processing_attempt_count - 1, 0)"),
		"processing_claimed_at":    nil,
	})
	return released.RowsAffected > 0, released.Error
}

func (self *transaction) RetryPhotoProcessing(userId string, photoIds []string) (int, error) {
	if userId == "" || len(photoIds) > 500 {
		return 0, ErrInvalidArguments
	}
	if len(photoIds) == 0 {
		return 0, nil
	}
	retried := self.tx.Model(&photoModel{}).
		Where("user_id = ? AND id IN ? AND photo_processing_state IN ? AND trashed_at IS NULL", userId, photoIds,
			[]models.PhotoProcessingState{models.PhotoProcessingFailed, models.PhotoProcessingReady}).
		Updates(map[string]any{
			"photo_processing_state":   models.PhotoProcessingWaiting,
			"processing_attempt_count": 0,
			"processing_outage_count":  0,
			"processing_error_message": "",
			"processing_claimed_at":    nil,
			"processing_not_before":    nil,
			"updated_at":               photoNow(),
		})
	return int(retried.RowsAffected), retried.Error
}

func (self *transaction) MarkPhotoProcessingFailed(photoId string, claimedAt time.Time, errorMessage string, isFileAtFault bool, maximumAttemptCount int) (bool, error) {
	if maximumAttemptCount < 1 {
		return false, ErrInvalidArguments
	}
	var row photoModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", photoId), &row)
	if err != nil {
		return false, err
	}
	if !isFound {
		return false, ErrNotFound
	}
	if row.ProcessingClaimedAt == nil || !row.ProcessingClaimedAt.Equal(claimedAt) || row.PhotoProcessingState != models.PhotoProcessingWaiting {
		return false, nil
	}
	// Claiming counted this attempt. A failure that is not the file's gives it
	// back, and waits the longer the more of them there have been in a row,
	// since a store or a program that is down does not come back in a minute.
	attemptCount := row.ProcessingAttemptCount
	outageCount := 0
	photoProcessingState := models.PhotoProcessingWaiting
	var backoff time.Duration
	if isFileAtFault {
		attemptCount = max(attemptCount, 1)
		if attemptCount >= maximumAttemptCount {
			photoProcessingState = models.PhotoProcessingFailed
		}
		backoff = photoProcessingBackoff(attemptCount)
	} else {
		attemptCount = max(attemptCount-1, 0)
		outageCount = row.ProcessingOutageCount + 1
		backoff = photoProcessingBackoff(outageCount)
	}
	if len(errorMessage) > photoErrorMessageBytes {
		errorMessage = strings.ToValidUTF8(errorMessage[:photoErrorMessageBytes], "")
	}
	changes := map[string]any{
		"photo_processing_state":   photoProcessingState,
		"processing_attempt_count": attemptCount,
		"processing_outage_count":  outageCount,
		"processing_error_message": errorMessage,
		"processing_claimed_at":    nil,
		"updated_at":               photoNow(),
	}
	if photoProcessingState == models.PhotoProcessingWaiting {
		changes["processing_not_before"] = photoNow().Add(backoff)
	} else {
		changes["processing_not_before"] = nil
	}
	return true, self.tx.Model(&photoModel{}).Where("id = ?", photoId).Updates(changes).Error
}

func (self *transaction) UpdatePhotoMetadata(photoId string, metadata *PhotoMetadata) error {
	if metadata == nil || (metadata.PhotoKind != "" && !metadata.PhotoKind.IsValid()) || metadata.WidthPixels < 0 || metadata.HeightPixels < 0 || metadata.DurationMS < 0 ||
		len(metadata.PlaceName) > 255 || len(metadata.CameraMake) > 128 || len(metadata.CameraModel) > 128 ||
		(metadata.Latitude != nil && (*metadata.Latitude < -90 || *metadata.Latitude > 90)) || (metadata.Longitude != nil && (*metadata.Longitude < -180 || *metadata.Longitude > 180)) ||
		(metadata.TakenAtOffsetMinutes != nil && !isPlausibleOffsetMinutes(*metadata.TakenAtOffsetMinutes)) {
		return ErrInvalidArguments
	}
	// The row is locked, so that the date it has is the date the offset is
	// decided against: a correction made while the file was being read is
	// neither undone nor given the wrong offset.
	var current photoModel
	isFound, err := findOneRow(self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", photoId), &current)
	if err != nil {
		return err
	}
	if !isFound {
		return ErrNotFound
	}
	takenAt, offsetMinutes := settlePhotoTakenAt(current.TakenAt, current.TakenAtOffsetMinutes, metadata)

	// A column is only replaced where the photo has nothing. The size and the
	// length describe the file itself, so they are replaced whenever the file
	// said, and the kind is whatever the caller settled on.
	changes := map[string]any{
		"updated_at":   photoNow(),
		"latitude":     gorm.Expr("CASE WHEN latitude IS NULL OR longitude IS NULL THEN ? ELSE latitude END", metadata.Latitude),
		"longitude":    gorm.Expr("CASE WHEN latitude IS NULL OR longitude IS NULL THEN ? ELSE longitude END", metadata.Longitude),
		"place_name":   gorm.Expr("CASE WHEN place_name = '' THEN ? ELSE place_name END", metadata.PlaceName),
		"camera_make":  gorm.Expr("CASE WHEN camera_make = '' THEN ? ELSE camera_make END", metadata.CameraMake),
		"camera_model": gorm.Expr("CASE WHEN camera_model = '' THEN ? ELSE camera_model END", metadata.CameraModel),
	}
	if takenAt != nil {
		changes["taken_at"] = takenAt.UTC()
		if offsetMinutes != nil {
			changes["taken_at_offset_minutes"] = *offsetMinutes
		}
	}
	if metadata.PhotoKind != "" {
		changes["photo_kind"] = metadata.PhotoKind
	}
	if metadata.WidthPixels > 0 && metadata.HeightPixels > 0 {
		changes["width_pixels"], changes["height_pixels"] = metadata.WidthPixels, metadata.HeightPixels
	}
	if metadata.DurationMS > 0 {
		changes["duration_ms"] = metadata.DurationMS
	}
	if err := self.tx.Model(&photoModel{}).Where("id = ?", photoId).Updates(changes).Error; err != nil {
		return err
	}
	return self.refreshPhotoSearchDocumentsOf([]string{photoId})
}

// settlePhotoTakenAt is the date and offset a photo has once what its file
// says is taken in, so that a photo that has a date always has an instant and
// an offset.
//
//   - A photo with no date takes the file's. A date the file wrote as a wall
//     clock, with no offset, is read on the owner's clock, and gets the
//     offset that clock had then; an instant with no offset gets the owner's
//     offset at that instant.
//   - A photo that has a date keeps it. If it has no offset (the uploader
//     sent an instant with a Z), the file's own offset completes it when the
//     file's date is the same moment, and the owner's clock gives it
//     otherwise.
//
// The date is nil when the photo has none and the file did not say.
func settlePhotoTakenAt(currentTakenAt *time.Time, currentOffsetMinutes *int, metadata *PhotoMetadata) (*time.Time, *int) {
	ownerLocation := metadata.OwnerLocation
	if ownerLocation == nil {
		ownerLocation = time.UTC
	}
	offsetOf := func(moment time.Time) *int {
		_, offsetSeconds := moment.In(ownerLocation).Zone()
		offsetMinutes := offsetSeconds / 60
		return &offsetMinutes
	}
	if currentTakenAt == nil {
		if metadata.TakenAt == nil {
			return nil, nil
		}
		if metadata.TakenAtOffsetMinutes != nil {
			return metadata.TakenAt, metadata.TakenAtOffsetMinutes
		}
		if !metadata.IsTakenAtWallClock {
			return metadata.TakenAt, offsetOf(*metadata.TakenAt)
		}
		// A wall clock, held as if it were UTC.
		wallClock := metadata.TakenAt.UTC()
		moment := time.Date(wallClock.Year(), wallClock.Month(), wallClock.Day(), wallClock.Hour(), wallClock.Minute(), wallClock.Second(), wallClock.Nanosecond(), ownerLocation)
		return &moment, offsetOf(moment)
	}
	if currentOffsetMinutes != nil {
		return nil, nil
	}
	if metadata.TakenAt != nil && metadata.TakenAtOffsetMinutes != nil &&
		math.Abs(metadata.TakenAt.Sub(*currentTakenAt).Seconds()) <= photoTakenAtAgreementSeconds {
		return currentTakenAt, metadata.TakenAtOffsetMinutes
	}
	return currentTakenAt, offsetOf(*currentTakenAt)
}

func (self *transaction) RenewPhotoProcessingClaim(photoId string, claimedAt, renewedAt time.Time) (bool, error) {
	if !renewedAt.After(claimedAt) {
		return false, ErrInvalidArguments
	}
	renewed := self.tx.Model(&photoModel{}).Where(claimedPhoto, photoId, claimedAt.UTC()).Update("processing_claimed_at", renewedAt.UTC().Truncate(time.Microsecond))
	return renewed.RowsAffected > 0, renewed.Error
}

func (self *transaction) RequeuePhotosWithoutPreviews(limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, ErrInvalidArguments
	}
	requeued := self.tx.Exec(`UPDATE photo SET photo_processing_state = ?, processing_attempt_count = 0, processing_outage_count = 0, processing_error_message = '', processing_claimed_at = NULL, processing_not_before = NULL, updated_at = ?
		WHERE id IN (
			SELECT id FROM photo
			WHERE photo_processing_state = ? AND (trashed_at IS NULL OR trashed_at > ?)
				AND NOT EXISTS (SELECT 1 FROM photo_preview WHERE photo_preview.photo_id = photo.id AND photo_preview.photo_preview_size = ?)
				AND EXISTS (SELECT 1 FROM photo_file WHERE photo_file.photo_id = photo.id AND photo_file.photo_file_role IN ?)
			ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED)`,
		models.PhotoProcessingWaiting, photoNow(), models.PhotoProcessingReady, photoMarkedForDeletion, models.PhotoPreviewSmall,
		[]models.PhotoFileRole{models.PhotoFileRoleOriginal, models.PhotoFileRoleEdited}, limit)
	return int(requeued.RowsAffected), requeued.Error
}
