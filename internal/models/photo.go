package models

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PhotoKind says what a photo is made of.
type PhotoKind string

const (
	PhotoKindPicture PhotoKind = "picture"
	PhotoKindVideo   PhotoKind = "video"
	// PhotoKindLivePhoto is a picture with a short video that plays when it
	// is pressed; the video is the photo's liveVideo file.
	PhotoKindLivePhoto PhotoKind = "livePhoto"
)

// IsValid says whether the server knows this kind.
func (self PhotoKind) IsValid() bool {
	switch self {
	case PhotoKindPicture, PhotoKindVideo, PhotoKindLivePhoto:
		return true
	}
	return false
}

// PhotoSource says where a photo came from.
type PhotoSource string

const (
	PhotoSourceIPhone       PhotoSource = "iphone"
	PhotoSourceDashboard    PhotoSource = "dashboard"
	PhotoSourceCommandLine  PhotoSource = "commandLine"
	PhotoSourceConversation PhotoSource = "conversation"
	PhotoSourceMail         PhotoSource = "mail"
	PhotoSourceComputer     PhotoSource = "computer"
	// PhotoSourceShared is a photo somebody else shared and the person saved
	// to their own library.
	PhotoSourceShared PhotoSource = "shared"
)

// IsValid says whether the server knows this source.
func (self PhotoSource) IsValid() bool {
	switch self {
	case PhotoSourceIPhone, PhotoSourceDashboard, PhotoSourceCommandLine, PhotoSourceConversation, PhotoSourceMail, PhotoSourceComputer, PhotoSourceShared:
		return true
	}
	return false
}

// PhotoFileRole says which of a photo's files one is.
type PhotoFileRole string

const (
	PhotoFileRoleOriginal PhotoFileRole = "original"
	// PhotoFileRoleEdited is the version the person made on a phone, which
	// previews are made from in preference to the original.
	PhotoFileRoleEdited    PhotoFileRole = "edited"
	PhotoFileRoleLiveVideo PhotoFileRole = "liveVideo"
)

// IsValid says whether the server knows this role.
func (self PhotoFileRole) IsValid() bool {
	switch self {
	case PhotoFileRoleOriginal, PhotoFileRoleEdited, PhotoFileRoleLiveVideo:
		return true
	}
	return false
}

// PhotoProcessingState is where a photo is on its way to having previews and
// the metadata read from its file.
type PhotoProcessingState string

const (
	PhotoProcessingWaiting PhotoProcessingState = "waiting"
	PhotoProcessingReady   PhotoProcessingState = "ready"
	PhotoProcessingFailed  PhotoProcessingState = "failed"
)

// PhotoUploadState is where a resumable upload is.
type PhotoUploadState string

const (
	PhotoUploadReceiving PhotoUploadState = "receiving"
	PhotoUploadComplete  PhotoUploadState = "complete"
	PhotoUploadFailed    PhotoUploadState = "failed"
)

// PhotoPreviewSize names one of the smaller copies made of a photo.
type PhotoPreviewSize string

const (
	// PhotoPreviewSmall is at most 400 pixels on the long side.
	PhotoPreviewSmall PhotoPreviewSize = "small"
	// PhotoPreviewLarge is at most 2048 pixels on the long side.
	PhotoPreviewLarge PhotoPreviewSize = "large"
	// PhotoPreviewVideo is a video every browser plays: H.264, at most 1280.
	PhotoPreviewVideo PhotoPreviewSize = "video"
)

// IsValid says whether the server knows this size.
func (self PhotoPreviewSize) IsValid() bool {
	switch self {
	case PhotoPreviewSmall, PhotoPreviewLarge, PhotoPreviewVideo:
		return true
	}
	return false
}

// PhotoUploadPartBytes is the most one stored part of an upload holds.
const PhotoUploadPartBytes int64 = 8 << 20

// Photo is one item in a person's library: a picture, a video, or a Live
// Photo. The bytes are in its files.
type Photo struct {
	ID                   string               `json:"id"`
	UserID               string               `json:"userId"`
	CreatedAt            time.Time            `json:"createdAt"`
	UpdatedAt            time.Time            `json:"updatedAt"`
	ClientPhotoID        string               `json:"clientPhotoId"`
	PhotoKind            PhotoKind            `json:"photoKind"`
	PhotoSource          PhotoSource          `json:"photoSource"`
	TakenAt              *time.Time           `json:"takenAt" graphapi:"nullable"`
	TakenAtOffsetMinutes *int                 `json:"takenAtOffsetMinutes" graphapi:"nullable"`
	WidthPixels          int                  `json:"widthPixels"`
	HeightPixels         int                  `json:"heightPixels"`
	DurationMS           int64                `json:"durationMS"`
	Latitude             *float64             `json:"latitude" graphapi:"nullable"`
	Longitude            *float64             `json:"longitude" graphapi:"nullable"`
	PlaceName            string               `json:"placeName"`
	CameraMake           string               `json:"cameraMake"`
	CameraModel          string               `json:"cameraModel"`
	OriginalFilename     string               `json:"originalFilename"`
	IsFavorite           bool                 `json:"isFavorite"`
	IsScreenshot         bool                 `json:"isScreenshot"`
	PhotoProcessingState PhotoProcessingState `json:"photoProcessingState"`
	// ProcessingAttemptCount is how many times making previews has failed.
	ProcessingAttemptCount int    `json:"processingAttemptCount"`
	ProcessingErrorMessage string `json:"processingErrorMessage"`
	// ProcessingClaimedAt is when an instance took the photo to process it.
	ProcessingClaimedAt *time.Time `json:"-"`
	TrashedAt           *time.Time `json:"trashedAt" graphapi:"nullable"`

	// The preview addresses are worked out when the photo is read, from the
	// previews that exist; each is empty until its preview is made. They
	// carry the time the photo last changed, so a new preview is a new
	// address and the old one can be cached for good.
	SmallPreviewURL string `json:"smallPreviewUrl" gorm:"-"`
	LargePreviewURL string `json:"largePreviewUrl" gorm:"-"`
	VideoPreviewURL string `json:"videoPreviewUrl" gorm:"-"`
}

// PhotoBlob is the bytes of a file in the blob store, named by their SHA-256.
type PhotoBlob struct {
	BlobSHA256    string     `json:"blobSha256"`
	BlobSizeBytes int64      `json:"blobSizeBytes"`
	CreatedAt     time.Time  `json:"createdAt"`
	DeletingAt    *time.Time `json:"-"`
}

// PhotoBlobFileID is the name of a blob in the blob store.
func PhotoBlobFileID(blobSha256 string) string {
	return "photo-blob-" + blobSha256
}

// PhotoFile is one file a photo is made of.
type PhotoFile struct {
	PhotoID          string        `json:"photoId"`
	PhotoFileRole    PhotoFileRole `json:"photoFileRole"`
	BlobSHA256       string        `json:"blobSha256"`
	ContentType      string        `json:"contentType"`
	FileSizeBytes    int64         `json:"fileSizeBytes"`
	OriginalFilename string        `json:"originalFilename"`
	CreatedAt        time.Time     `json:"createdAt"`

	// FileURL is where the owner downloads it; worked out when read.
	FileURL string `json:"fileUrl" gorm:"-"`
}

// PhotoUploadHeaders is what the first request of an upload said the file is.
// Kept with the upload because the request that completes it may be a later
// one, and may say nothing of this.
type PhotoUploadHeaders struct {
	ClientPhotoID        string        `json:"clientPhotoId"`
	PhotoFileRole        PhotoFileRole `json:"photoFileRole"`
	PhotoKind            PhotoKind     `json:"photoKind"`
	PhotoSource          PhotoSource   `json:"photoSource"`
	OriginalFilename     string        `json:"originalFilename"`
	ContentType          string        `json:"contentType"`
	TakenAt              *time.Time    `json:"takenAt,omitempty"`
	TakenAtOffsetMinutes *int          `json:"takenAtOffsetMinutes,omitempty"`
	Latitude             *float64      `json:"latitude,omitempty"`
	Longitude            *float64      `json:"longitude,omitempty"`
	IsFavorite           bool          `json:"isFavorite"`
	IsScreenshot         bool          `json:"isScreenshot"`
	DeclaredSHA256       string        `json:"declaredSha256,omitempty"`
}

// PhotoUpload is a file on its way in, received in parts.
type PhotoUpload struct {
	ID                 string             `json:"id"`
	UserID             string             `json:"userId"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
	PhotoUploadState   PhotoUploadState   `json:"photoUploadState"`
	DeclaredSizeBytes  *int64             `json:"declaredSizeBytes" graphapi:"nullable"`
	ReceivedSizeBytes  int64              `json:"receivedSizeBytes"`
	PhotoUploadHeaders PhotoUploadHeaders `json:"photoUploadHeaders" gorm:"column:upload_headers;serializer:json"`
	// PhotoID is the photo the upload became, once it is complete.
	PhotoID *string `json:"photoId" graphapi:"nullable"`
}

// PhotoUploadPart is a receipt for one whole stored part of an upload.
type PhotoUploadPart struct {
	PhotoUploadID string    `json:"photoUploadId"`
	PartIndex     int       `json:"partIndex"`
	PartSizeBytes int64     `json:"partSizeBytes"`
	PartSHA256    string    `json:"partSha256"`
	StoredAt      time.Time `json:"storedAt"`
}

// PhotoUploadPartFileID is the name of a stored part in the blob store.
func PhotoUploadPartFileID(photoUploadId string, partIndex int) string {
	return fmt.Sprintf("photo-upload-%s-%d", photoUploadId, partIndex)
}

// PhotoPreview is one smaller copy made of a photo.
type PhotoPreview struct {
	PhotoID          string           `json:"photoId"`
	PhotoPreviewSize PhotoPreviewSize `json:"photoPreviewSize"`
	ContentType      string           `json:"contentType"`
	PreviewSizeBytes int64            `json:"previewSizeBytes"`
	WidthPixels      int              `json:"widthPixels"`
	HeightPixels     int              `json:"heightPixels"`
	CreatedAt        time.Time        `json:"createdAt"`

	// PreviewURL is where the owner fetches it; worked out when read.
	PreviewURL string `json:"previewUrl" gorm:"-"`
}

// PhotoPreviewFileID is the name of a preview in the blob store.
func PhotoPreviewFileID(photoId string, photoPreviewSize PhotoPreviewSize) string {
	return fmt.Sprintf("photo-preview-%s-%s", photoId, photoPreviewSize)
}

// MaximumPhotoAlbumTitleLength is the most characters an album's title holds.
const MaximumPhotoAlbumTitleLength = 255

// NormalizePhotoAlbumTitle trims a title and refuses one that is empty or too
// long, saying what the limit is.
func NormalizePhotoAlbumTitle(albumTitle string) (string, error) {
	albumTitle = strings.TrimSpace(albumTitle)
	if length := utf8.RuneCountInString(albumTitle); length < 1 || length > MaximumPhotoAlbumTitleLength {
		return "", ValidationErrors{{Path: "albumTitle", Message: fmt.Sprintf("must be from 1 to %d characters", MaximumPhotoAlbumTitleLength)}}
	}
	return albumTitle, nil
}

// PhotoAlbum is a named set of a person's photos. It holds references: a photo
// can be in several albums, and deleting the album leaves the photos.
type PhotoAlbum struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	AlbumTitle string    `json:"albumTitle"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	// CoverPhotoID is the photo the person chose to stand for the album, if
	// they did. Until they do, and while the photo they chose is in the trash
	// or has no small preview yet, the album shows its newest photo.
	CoverPhotoID *string `json:"coverPhotoId" graphapi:"nullable"`

	// The rest is worked out when the album is read, from the photos that are
	// not in the trash: a photo in the trash stays in its albums but is not
	// counted or shown until it is restored.
	PhotoCount int `json:"photoCount" gorm:"-"`

	// FirstTakenAt and LastTakenAt bound the date the album's photos are
	// placed at (when taken, or when they arrived if that is not known). Null
	// for an album with no photos.
	FirstTakenAt *time.Time `json:"firstTakenAt" graphapi:"nullable" gorm:"-"`
	LastTakenAt  *time.Time `json:"lastTakenAt" graphapi:"nullable" gorm:"-"`

	// CoverPreviewURL is where the album's cover picture is fetched from:
	// empty when no photo of the album has a small preview yet.
	CoverPreviewURL string `json:"coverPreviewUrl" gorm:"-"`

	// CoverPreviewPhotoID and CoverPreviewPhotoUpdatedAt name the photo the
	// cover is made from and when it last changed, so that the API can address
	// its preview the way it addresses any other.
	CoverPreviewPhotoID        string     `json:"-" gorm:"-"`
	CoverPreviewPhotoUpdatedAt *time.Time `json:"-" gorm:"-"`
}

// PhotoAlbumItem puts a photo in an album.
type PhotoAlbumItem struct {
	PhotoAlbumID  string    `json:"photoAlbumId"`
	PhotoID       string    `json:"photoId"`
	AddedByUserID string    `json:"addedByUserId"`
	AddedAt       time.Time `json:"addedAt"`
}

// PhotoAlbumReference is the album a photo is in, as the photo says it: enough
// to name it and to open it.
type PhotoAlbumReference struct {
	ID         string `json:"id"`
	AlbumTitle string `json:"albumTitle"`
}

// PhotoAlbumSuggestion is a run of photos that belong together and are in no
// album: a trip or an event. Nothing is created from it until somebody asks.
type PhotoAlbumSuggestion struct {
	// TakenAfter and TakenBefore bound the run the way a listing's filters do:
	// the first photo is taken on or after the first and the last before the
	// second.
	TakenAfter  time.Time `json:"takenAfter"`
	TakenBefore time.Time `json:"takenBefore"`

	// PlaceNames are the places in the run, the one with most photos first.
	PlaceNames []string `json:"placeNames"`

	// AlbumTitle is a proposal, in English, for the person to change.
	AlbumTitle string `json:"albumTitle"`

	PhotoCount int `json:"photoCount"`

	// SamplePhotoIDs are a few photos spread across the run, to show it by.
	SamplePhotoIDs []string `json:"samplePhotoIds"`
}
