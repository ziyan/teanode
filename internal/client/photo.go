package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// The photo library: the same operations as the dashboard's Photos page, the
// iPhone app and the agent's photo tool. A photo is an ordinary row of the
// API (models.Photo); the bytes go up over the resumable upload endpoint,
// which UploadPhoto speaks.

// PhotoView is one photo with the files it is made of and the previews that
// have been made of it.
type PhotoView struct {
	Photo         *models.Photo          `json:"photo"`
	PhotoFiles    []*models.PhotoFile    `json:"photoFiles"`
	PhotoPreviews []*models.PhotoPreview `json:"photoPreviews"`

	// Albums are the albums the photo is in.
	Albums []*models.PhotoAlbumReference `json:"albums"`
}

// PhotoLibrary is how much a library holds, how much it may, and where files
// go up.
type PhotoLibrary struct {
	PhotoCount                        int64  `json:"photoCount"`
	VideoCount                        int64  `json:"videoCount"`
	FavoriteCount                     int64  `json:"favoriteCount"`
	TrashedCount                      int64  `json:"trashedCount"`
	UsedBytes                         int64  `json:"usedBytes"`
	MaximumFileBytes                  int64  `json:"maximumFileBytes"`
	MaximumAccountBytes               int64  `json:"maximumAccountBytes"`
	TrashRetentionDays                int    `json:"trashRetentionDays"`
	UploadPath                        string `json:"uploadPath"`
	UploadPartBytes                   int64  `json:"uploadPartBytes"`
	IsFaceRecognitionAvailable        bool   `json:"isFaceRecognitionAvailable"`
	FaceRecognitionUnavailableMessage string `json:"faceRecognitionUnavailableMessage"`
	IsDescriptionAvailable            bool   `json:"isDescriptionAvailable"`
	DescriptionUnavailableMessage     string `json:"descriptionUnavailableMessage"`
}

// PhotoFilter narrows a listing of the library. Every field is optional.
type PhotoFilter struct {
	// BeforePhotoID continues after the last photo of the previous page, and
	// Offset skips that many photos instead. Not both.
	BeforePhotoID string
	Offset        int
	PageSize      int

	// IsFavorite keeps favorites when true and the rest when false.
	IsFavorite *bool

	// IsTrashed lists the trash instead of the library.
	IsTrashed bool

	PhotoKind models.PhotoKind

	// TakenAfter and TakenBefore bound the date a photo is placed at.
	TakenAfter  *time.Time
	TakenBefore *time.Time

	SearchText string

	// NearLatitude, NearLongitude and NearDistanceKilometers keep photos
	// taken within that distance of a point. All three or none.
	NearLatitude           *float64
	NearLongitude          *float64
	NearDistanceKilometers *float64

	// AlbumID keeps the photos of one album.
	AlbumID string
}

// variables are the filter's arguments, leaving out what is not asked.
func (self *PhotoFilter) variables() map[string]any {
	variables := map[string]any{}
	if self == nil {
		return variables
	}
	if self.BeforePhotoID != "" {
		variables["beforePhotoId"] = self.BeforePhotoID
	}
	if self.Offset > 0 {
		variables["offset"] = self.Offset
	}
	if self.PageSize > 0 {
		variables["pageSize"] = self.PageSize
	}
	if self.IsFavorite != nil {
		variables["isFavorite"] = *self.IsFavorite
	}
	if self.IsTrashed {
		variables["isTrashed"] = true
	}
	if self.PhotoKind != "" {
		variables["photoKind"] = string(self.PhotoKind)
	}
	if self.TakenAfter != nil {
		variables["takenAfter"] = self.TakenAfter.UTC().Format(time.RFC3339Nano)
	}
	if self.TakenBefore != nil {
		variables["takenBefore"] = self.TakenBefore.UTC().Format(time.RFC3339Nano)
	}
	if self.SearchText != "" {
		variables["searchText"] = self.SearchText
	}
	if self.NearLatitude != nil {
		variables["nearLatitude"] = *self.NearLatitude
	}
	if self.NearLongitude != nil {
		variables["nearLongitude"] = *self.NearLongitude
	}
	if self.NearDistanceKilometers != nil {
		variables["nearDistanceKilometers"] = *self.NearDistanceKilometers
	}
	if self.AlbumID != "" {
		variables["albumId"] = self.AlbumID
	}
	return variables
}

const (
	photoSelection = `{ id createdAt updatedAt clientPhotoId photoKind photoSource takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel originalFilename isFavorite isScreenshot photoProcessingState processingAttemptCount processingErrorMessage trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl }`

	photoFilterDeclarations = `$beforePhotoId: String, $offset: Int, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $photoKind: String, $takenAfter: DateTime, $takenBefore: DateTime, $searchText: String, $nearLatitude: Float, $nearLongitude: Float, $nearDistanceKilometers: Float, $albumId: String`
	photoFilterArguments    = `beforePhotoId: $beforePhotoId, offset: $offset, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, photoKind: $photoKind, takenAfter: $takenAfter, takenBefore: $takenBefore, searchText: $searchText, nearLatitude: $nearLatitude, nearLongitude: $nearLongitude, nearDistanceKilometers: $nearDistanceKilometers, albumId: $albumId`
)

const (
	DocumentListPhotos = `query (` + photoFilterDeclarations + `) { ListPhotos(` + photoFilterArguments + `) ` + photoSelection + ` }`

	DocumentCountPhotos = `query (` + photoFilterDeclarations + `) { CountPhotos(` + photoFilterArguments + `) }`

	DocumentReadPhoto = `query ($photoId: String!) {
  ReadPhoto(photoId: $photoId) {
    photo ` + photoSelection + `
    photoFiles { photoId photoFileRole blobSha256 contentType fileSizeBytes originalFilename createdAt fileUrl }
    photoPreviews { photoId photoPreviewSize contentType previewSizeBytes widthPixels heightPixels createdAt previewUrl }
    albums { id albumTitle }
  }
}`

	DocumentReadPhotoLibrary = `query { ReadPhotoLibrary { photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays uploadPath uploadPartBytes isFaceRecognitionAvailable faceRecognitionUnavailableMessage isDescriptionAvailable descriptionUnavailableMessage } }`

	DocumentFindPhotos = `query ($clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: $clientPhotoIds) ` + photoSelection + ` }`

	DocumentUpdatePhoto = `mutation ($photoId: String!, $isFavorite: Boolean, $takenAt: DateTime) { UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite, takenAt: $takenAt) ` + photoSelection + ` }`

	DocumentTrashPhotos = `mutation ($photoIds: [String!]!) { TrashPhotos(photoIds: $photoIds) }`

	DocumentRestorePhotos = `mutation ($photoIds: [String!]!) { RestorePhotos(photoIds: $photoIds) }`

	DocumentDeletePhotos = `mutation ($photoIds: [String!]!) { DeletePhotos(photoIds: $photoIds) }`

	DocumentEmptyPhotoTrash = `mutation { EmptyPhotoTrash }`

	DocumentSavePhotoFromAttachment = `mutation ($agentAttachmentId: String!) { SavePhotoFromAttachment(agentAttachmentId: $agentAttachmentId) ` + photoSelection + ` }`
)

// MaximumPhotoIDs is how many photos one call to the operations that take a
// list of them may name.
const MaximumPhotoIDs = 500

// ListPhotos is a page of the library, newest first.
func ListPhotos(ctx context.Context, executor Executor, filter *PhotoFilter) ([]*models.Photo, error) {
	var result struct {
		ListPhotos []*models.Photo `json:"ListPhotos"`
	}
	if err := executor.Execute(ctx, DocumentListPhotos, filter.variables(), &result); err != nil {
		return nil, err
	}
	return result.ListPhotos, nil
}

// CountPhotos is how many photos the filter keeps, whatever page it asks for.
func CountPhotos(ctx context.Context, executor Executor, filter *PhotoFilter) (int, error) {
	var result struct {
		CountPhotos int `json:"CountPhotos"`
	}
	if err := executor.Execute(ctx, DocumentCountPhotos, filter.variables(), &result); err != nil {
		return 0, err
	}
	return result.CountPhotos, nil
}

// ReadPhoto is one photo, with its files and previews.
func ReadPhoto(ctx context.Context, executor Executor, photoId string) (*PhotoView, error) {
	var result struct {
		ReadPhoto *PhotoView `json:"ReadPhoto"`
	}
	if err := executor.Execute(ctx, DocumentReadPhoto, map[string]any{"photoId": photoId}, &result); err != nil {
		return nil, err
	}
	if result.ReadPhoto == nil || result.ReadPhoto.Photo == nil {
		return nil, fmt.Errorf("%w: no photo %s", ErrNotFound, photoId)
	}
	return result.ReadPhoto, nil
}

// ReadPhotoLibrary is what the library holds and may hold.
func ReadPhotoLibrary(ctx context.Context, executor Executor) (*PhotoLibrary, error) {
	var result struct {
		ReadPhotoLibrary *PhotoLibrary `json:"ReadPhotoLibrary"`
	}
	if err := executor.Execute(ctx, DocumentReadPhotoLibrary, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadPhotoLibrary, nil
}

// FindPhotos is the photos the library already has under the uploader's own
// names for them, which is how an uploader decides what to skip. At most
// MaximumPhotoIDs names are asked at once.
func FindPhotos(ctx context.Context, executor Executor, clientPhotoIds []string) ([]*models.Photo, error) {
	var result struct {
		FindPhotos []*models.Photo `json:"FindPhotos"`
	}
	if err := executor.Execute(ctx, DocumentFindPhotos, map[string]any{"clientPhotoIds": clientPhotoIds}, &result); err != nil {
		return nil, err
	}
	return result.FindPhotos, nil
}

// SetPhotoFavorite makes a photo a favorite or not.
func SetPhotoFavorite(ctx context.Context, executor Executor, photoId string, isFavorite bool) (*models.Photo, error) {
	var result struct {
		UpdatePhoto *models.Photo `json:"UpdatePhoto"`
	}
	if err := executor.Execute(ctx, DocumentUpdatePhoto, map[string]any{"photoId": photoId, "isFavorite": isFavorite}, &result); err != nil {
		return nil, err
	}
	return result.UpdatePhoto, nil
}

// TrashPhotos moves photos to the trash and says how many it moved.
func TrashPhotos(ctx context.Context, executor Executor, photoIds []string) (int, error) {
	var result struct {
		TrashPhotos int `json:"TrashPhotos"`
	}
	if err := executor.Execute(ctx, DocumentTrashPhotos, map[string]any{"photoIds": photoIds}, &result); err != nil {
		return 0, err
	}
	return result.TrashPhotos, nil
}

// RestorePhotos takes photos out of the trash and says how many it took.
func RestorePhotos(ctx context.Context, executor Executor, photoIds []string) (int, error) {
	var result struct {
		RestorePhotos int `json:"RestorePhotos"`
	}
	if err := executor.Execute(ctx, DocumentRestorePhotos, map[string]any{"photoIds": photoIds}, &result); err != nil {
		return 0, err
	}
	return result.RestorePhotos, nil
}

// DeletePhotos deletes forever the photos that are in the trash, and says how
// many there were.
func DeletePhotos(ctx context.Context, executor Executor, photoIds []string) (int, error) {
	var result struct {
		DeletePhotos int `json:"DeletePhotos"`
	}
	if err := executor.Execute(ctx, DocumentDeletePhotos, map[string]any{"photoIds": photoIds}, &result); err != nil {
		return 0, err
	}
	return result.DeletePhotos, nil
}

// EmptyPhotoTrash deletes forever everything in the trash and says how many
// photos that was.
func EmptyPhotoTrash(ctx context.Context, executor Executor) (int, error) {
	var result struct {
		EmptyPhotoTrash int `json:"EmptyPhotoTrash"`
	}
	if err := executor.Execute(ctx, DocumentEmptyPhotoTrash, nil, &result); err != nil {
		return 0, err
	}
	return result.EmptyPhotoTrash, nil
}

// SavePhotoFromAttachment keeps a file of a conversation as a photo.
func SavePhotoFromAttachment(ctx context.Context, executor Executor, agentAttachmentId string) (*models.Photo, error) {
	var result struct {
		SavePhotoFromAttachment *models.Photo `json:"SavePhotoFromAttachment"`
	}
	if err := executor.Execute(ctx, DocumentSavePhotoFromAttachment, map[string]any{"agentAttachmentId": agentAttachmentId}, &result); err != nil {
		return nil, err
	}
	return result.SavePhotoFromAttachment, nil
}

// PhotoUpload is one file to send to the library.
type PhotoUpload struct {
	// Source is the file's bytes and SizeBytes their number. A source that
	// can be read at any offset is what lets a part be sent again without
	// the rest.
	Source    io.ReaderAt
	SizeBytes int64

	// ClientPhotoID is the uploader's stable name for the photo; sending the
	// same name again finds the photo that is there. The SHA-256 of the file
	// is the usual one.
	ClientPhotoID string

	Filename    string
	ContentType string

	// DeclaredSHA256 is the file's digest when the uploader knows it, so the
	// server can refuse bytes that arrived changed.
	DeclaredSHA256 string

	// PhotoSource says where the file came from. The default is commandLine.
	PhotoSource models.PhotoSource

	// OnProgress is told how many bytes the server has, as parts are stored.
	OnProgress func(storedBytes int64)
}

// The tries an upload makes at one part before giving up, and how long a part
// may take: 8 MiB over a slow line.
const (
	photoUploadTryCount    = 3
	photoUploadPartTimeout = 10 * time.Minute
)

// UploadPhoto sends a file over the resumable upload endpoint: the first part
// starts the upload, the others are appended at the offset the server has,
// and after a failure the server is asked where it got to (HEAD) and the
// upload goes on from there. It returns the new photo's identifier.
//
// A part is tried three times. An upload the server lost, or that was
// cancelled, is started again from the beginning, which counts as a try.
// The caller sees the progress through OnProgress.
func (self *Client) UploadPhoto(ctx context.Context, upload *PhotoUpload) (string, error) {
	if self.readOnly {
		return "", &ReadOnlyError{URL: self.url}
	}
	if upload == nil || upload.Source == nil || upload.SizeBytes <= 0 || strings.TrimSpace(upload.ClientPhotoID) == "" {
		return "", fmt.Errorf("client: a photo upload needs a file with some bytes in it and a name for the photo")
	}
	// A part takes longer than the minute other requests are given.
	uploadClient := *self.client
	uploadClient.Timeout = 0
	uploader := &photoUploader{client: self, http: &uploadClient, upload: upload}
	return uploader.run(ctx)
}

type photoUploader struct {
	client *Client
	http   *http.Client
	upload *PhotoUpload

	// location is the path of the upload on the server, empty before it
	// has been started.
	location string
}

// photoOutcome is what one request of an upload got back.
type photoOutcome struct {
	statusCode int
	header     http.Header
	message    string
}

func (self *photoUploader) run(ctx context.Context) (string, error) {
	offsetBytes := int64(0)
	failureCount := 0
	var lastFailure error
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if failureCount >= photoUploadTryCount {
			return "", fmt.Errorf("client: %s could not be uploaded after %d tries: %w", self.describe(), photoUploadTryCount, lastFailure)
		}
		endBytes := min(offsetBytes+models.PhotoUploadPartBytes, self.upload.SizeBytes)
		isLast := endBytes == self.upload.SizeBytes
		outcome, err := self.sendPart(ctx, offsetBytes, endBytes, isLast)
		if err != nil {
			// The part may have arrived whole before the answer was lost.
			lastFailure = err
			failureCount++
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			recovered, recoverError := self.recover(ctx)
			if recoverError == nil {
				if recovered.photoId != "" {
					return recovered.photoId, nil
				}
				if recovered.offsetBytes > offsetBytes {
					failureCount = 0
				}
				offsetBytes = recovered.offsetBytes
				self.reportProgress(offsetBytes)
			} else if errors.Is(recoverError, errPhotoUploadLost) {
				self.location, offsetBytes = "", 0
			}
			continue
		}
		switch {
		case outcome.statusCode == http.StatusCreated && outcome.header.Get("Upload-Complete") == "?1":
			photoId := outcome.header.Get("X-Photo-ID")
			if photoId == "" {
				return "", fmt.Errorf("client: the server stored %s and did not say which photo it is", self.describe())
			}
			self.reportProgress(self.upload.SizeBytes)
			return photoId, nil
		case outcome.statusCode == http.StatusCreated || outcome.statusCode == http.StatusNoContent:
			storedBytes, err := strconv.ParseInt(outcome.header.Get("Upload-Offset"), 10, 64)
			if err != nil || storedBytes < 0 || storedBytes > self.upload.SizeBytes {
				return "", fmt.Errorf("client: the server answered %s without saying how much of it there is", self.describe())
			}
			if storedBytes > offsetBytes {
				failureCount = 0
			} else {
				failureCount++
				lastFailure = fmt.Errorf("the server stored nothing of the part")
			}
			offsetBytes = storedBytes
			self.reportProgress(offsetBytes)
		case outcome.statusCode == http.StatusConflict:
			// The server is somewhere else than we thought: it says where.
			storedBytes, err := strconv.ParseInt(outcome.header.Get("Upload-Offset"), 10, 64)
			if err != nil || storedBytes < 0 || storedBytes > self.upload.SizeBytes {
				return "", fmt.Errorf("client: the server and the upload disagree about %s: %s", self.describe(), outcome.message)
			}
			failureCount++
			lastFailure = fmt.Errorf("the server was at %d bytes, not %d", storedBytes, offsetBytes)
			offsetBytes = storedBytes
		case outcome.statusCode == http.StatusNotFound && self.location != "":
			// Cancelled or failed on the server; the upload begins again.
			failureCount++
			lastFailure = fmt.Errorf("the server no longer has the upload: %s", outcome.message)
			self.location, offsetBytes = "", 0
		case isRetryablePhotoStatus(outcome.statusCode):
			failureCount++
			lastFailure = fmt.Errorf("the server answered HTTP %d: %s", outcome.statusCode, outcome.message)
			if recovered, err := self.recover(ctx); err == nil {
				if recovered.photoId != "" {
					return recovered.photoId, nil
				}
				offsetBytes = recovered.offsetBytes
			}
		case outcome.statusCode == http.StatusUnauthorized:
			return "", fmt.Errorf("%w: %s refused the upload", ErrUnauthorized, self.client.url)
		default:
			// The server looked at the file and said no: asking again says
			// the same.
			return "", fmt.Errorf("client: %s was refused (HTTP %d): %s", self.describe(), outcome.statusCode, outcome.message)
		}
	}
}

func (self *photoUploader) describe() string {
	if self.upload.Filename != "" {
		return self.upload.Filename
	}
	return self.upload.ClientPhotoID
}

func (self *photoUploader) reportProgress(storedBytes int64) {
	if self.upload.OnProgress != nil {
		self.upload.OnProgress(storedBytes)
	}
}

func isRetryablePhotoStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// sendPart sends the bytes from offsetBytes to endBytes: as the request that
// starts the upload when it has not been started, and as an append when it
// has.
func (self *photoUploader) sendPart(ctx context.Context, offsetBytes, endBytes int64, isLast bool) (*photoOutcome, error) {
	method, address := http.MethodPatch, self.client.url+self.location
	if self.location == "" {
		method, address = http.MethodPost, self.client.url+api.PathPhotoUploads
		offsetBytes, endBytes, isLast = 0, min(models.PhotoUploadPartBytes, self.upload.SizeBytes), models.PhotoUploadPartBytes >= self.upload.SizeBytes
	}
	requestContext, cancel := context.WithTimeout(ctx, photoUploadPartTimeout)
	defer cancel()
	// The 104 tells us where the upload is before the answer comes, so that
	// an answer that never comes can be recovered from.
	trace := &httptrace.ClientTrace{Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
		if code == 104 {
			if location := photoUploadLocation(header.Get("Location")); location != "" {
				self.location = location
			}
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(requestContext, trace), method, address,
		io.NewSectionReader(self.upload.Source, offsetBytes, endBytes-offsetBytes))
	if err != nil {
		return nil, err
	}
	request.ContentLength = endBytes - offsetBytes
	if self.client.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.client.token)
	}
	if isLast {
		request.Header.Set("Upload-Complete", "?1")
	} else {
		request.Header.Set("Upload-Complete", "?0")
	}
	if method == http.MethodPost {
		photoSource := self.upload.PhotoSource
		if photoSource == "" {
			photoSource = models.PhotoSourceCommandLine
		}
		request.Header.Set("Content-Type", self.upload.ContentType)
		request.Header.Set("Upload-Length", strconv.FormatInt(self.upload.SizeBytes, 10))
		request.Header.Set("X-Photo-Client-ID", self.upload.ClientPhotoID)
		request.Header.Set("X-Photo-Source", string(photoSource))
		if self.upload.Filename != "" {
			request.Header.Set("X-Photo-Filename", self.upload.Filename)
		}
		if self.upload.DeclaredSHA256 != "" {
			request.Header.Set("X-Photo-Declared-SHA256", self.upload.DeclaredSHA256)
		}
	} else {
		request.Header.Set("Content-Type", "application/partial-upload")
		request.Header.Set("Upload-Offset", strconv.FormatInt(offsetBytes, 10))
	}
	outcome, err := self.do(request)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		// The final answer carries the address too, for a client that sees
		// no 104.
		if location := photoUploadLocation(outcome.header.Get("Location")); location != "" {
			self.location = location
		}
	}
	return outcome, nil
}

// photoUploadLocation is the part of the address an upload was given that is
// ours: the path, since the host the server saw is not always the one the
// client reaches it by.
func photoUploadLocation(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(parsed.Path, api.PathPhotoUploads+"/") {
		return ""
	}
	return parsed.Path
}

func (self *photoUploader) do(request *http.Request) (*photoOutcome, error) {
	response, err := self.http.Do(request)
	if err != nil {
		return nil, &ConnectionError{URL: self.client.url, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	// The answer to a finished upload is JSON naming the photo; the header
	// says it too.
	if response.StatusCode == http.StatusCreated && response.Header.Get("Upload-Complete") == "?1" {
		message = ""
	}
	return &photoOutcome{statusCode: response.StatusCode, header: response.Header, message: message}, nil
}

// errPhotoUploadLost is an upload the server has no record of.
var errPhotoUploadLost = errors.New("client: the server has no such upload")

// photoRecovery is where an upload is according to the server.
type photoRecovery struct {
	offsetBytes int64
	photoId     string
}

// recover asks the server how much of the upload it has, or that it is
// complete and which photo it made.
func (self *photoUploader) recover(ctx context.Context) (*photoRecovery, error) {
	if self.location == "" {
		// The request that starts an upload never told us where it is, so
		// there is nothing to ask; it is started again.
		return &photoRecovery{}, nil
	}
	requestContext, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodHead, self.client.url+self.location, nil)
	if err != nil {
		return nil, err
	}
	if self.client.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.client.token)
	}
	outcome, err := self.do(request)
	if err != nil {
		return nil, err
	}
	switch outcome.statusCode {
	case http.StatusNoContent, http.StatusOK:
	case http.StatusNotFound:
		return nil, errPhotoUploadLost
	default:
		return nil, fmt.Errorf("the server answered HTTP %d when asked where the upload is", outcome.statusCode)
	}
	if outcome.header.Get("Upload-Complete") == "?1" && outcome.header.Get("X-Photo-ID") != "" {
		return &photoRecovery{photoId: outcome.header.Get("X-Photo-ID")}, nil
	}
	storedBytes, err := strconv.ParseInt(outcome.header.Get("Upload-Offset"), 10, 64)
	if err != nil || storedBytes < 0 || storedBytes > self.upload.SizeBytes {
		return nil, fmt.Errorf("the server did not say how much of the upload it has")
	}
	return &photoRecovery{offsetBytes: storedBytes}, nil
}

// DownloadPhotoFile streams one of a photo's files to destination and checks
// that all of it arrived: a file shorter than the server says it is, because
// the connection dropped, is an error and not a smaller photo.
func (self *Client) DownloadPhotoFile(ctx context.Context, file *models.PhotoFile, destination io.Writer) error {
	if file == nil || file.PhotoID == "" {
		return fmt.Errorf("client: a photo file is required")
	}
	// A photo takes longer than the minute other requests are given.
	downloadClient := *self.client
	downloadClient.Timeout = 0
	downloader := &Client{url: self.url, token: self.token, client: &downloadClient, readOnly: self.readOnly}
	response, err := downloader.Download(ctx, api.PhotoFilePath(file.PhotoID, string(file.PhotoFileRole)))
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	copiedBytes, err := io.Copy(destination, response.Body)
	if err != nil {
		return err
	}
	if copiedBytes != file.FileSizeBytes {
		return fmt.Errorf("client: the server sent %d bytes of a file of %d", copiedBytes, file.FileSizeBytes)
	}
	return nil
}
