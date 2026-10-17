package photo

import (
	"fmt"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
)

// The headers an uploader describes the file with, beyond the ones the
// protocol defines.
//
// X-Photo-Taken-At is an RFC 3339 date. With an offset (+09:00, or +00:00
// for an uploader that knows the photo was taken in UTC) it says the instant
// and the wall clock where the photo was taken, and both win over what the
// file's metadata says. Ending in Z it says only the instant: the offset is
// then unknown, and is taken from the file's EXIF OffsetTimeOriginal (or a
// video's creation date) when the photo is processed and finds one that
// agrees with the instant; without one the photo has no offset and is shown
// in the viewer's own zone.
const (
	headerClientId       = "X-Photo-Client-ID"
	headerFileRole       = "X-Photo-File-Role"
	headerKind           = "X-Photo-Kind"
	headerFilename       = "X-Photo-Filename"
	headerTakenAt        = "X-Photo-Taken-At"
	headerLatitude       = "X-Photo-Latitude"
	headerLongitude      = "X-Photo-Longitude"
	headerIsFavorite     = "X-Photo-Is-Favorite"
	headerIsScreenshot   = "X-Photo-Is-Screenshot"
	headerSource         = "X-Photo-Source"
	headerDeclaredSha256 = "X-Photo-Declared-SHA256"
	headerPhotoId        = "X-Photo-ID"
)

// The protocol's own headers.
const (
	headerUploadComplete    = "Upload-Complete"
	headerUploadIncomplete  = "Upload-Incomplete"
	headerUploadOffset      = "Upload-Offset"
	headerUploadLength      = "Upload-Length"
	headerUploadLimit       = "Upload-Limit"
	headerInteropVersion    = "Upload-Draft-Interop-Version"
	contentTypePartialBytes = "application/partial-upload"
)

// Bounds on the headers that are stored.
const (
	maximumClientIdLength    = 255
	maximumFilenameLength    = 255
	maximumContentTypeLength = 128
)

// headerError is a header the request got wrong, named so that the person
// reading the answer knows which one to fix.
type headerError struct {
	header  string
	message string
}

func (self *headerError) Error() string {
	return self.header + ": " + self.message
}

// parseUploadHeaders reads what the first request of an upload says the file
// is. Everything but the client's name for the photo is optional.
func parseUploadHeaders(request *http.Request) (*models.PhotoUploadHeaders, *headerError) {
	headers := &models.PhotoUploadHeaders{PhotoFileRole: models.PhotoFileRoleOriginal, PhotoSource: models.PhotoSourceCommandLine}

	clientPhotoId := request.Header.Get(headerClientId)
	if err := checkText(headerClientId, clientPhotoId, maximumClientIdLength); err != nil {
		return nil, err
	}
	if strings.TrimSpace(clientPhotoId) == "" {
		return nil, &headerError{headerClientId, "is required: the uploader's own stable name for this photo, such as the SHA-256 of the file"}
	}
	headers.ClientPhotoID = clientPhotoId

	if value := request.Header.Get(headerFileRole); value != "" {
		headers.PhotoFileRole = models.PhotoFileRole(value)
		if !headers.PhotoFileRole.IsValid() {
			return nil, &headerError{headerFileRole, "must be original, edited or liveVideo"}
		}
	}
	if value := request.Header.Get(headerKind); value != "" {
		headers.PhotoKind = models.PhotoKind(value)
		if !headers.PhotoKind.IsValid() {
			return nil, &headerError{headerKind, "must be picture, video or livePhoto"}
		}
	}
	if value := request.Header.Get(headerSource); value != "" {
		headers.PhotoSource = models.PhotoSource(value)
		// Where a photo was saved from inside the server is the server's to
		// say; an uploader claiming it would be claiming a trail it has not.
		if !headers.PhotoSource.IsValid() || headers.PhotoSource == models.PhotoSourceConversation || headers.PhotoSource == models.PhotoSourceShared {
			return nil, &headerError{headerSource, "must be iphone, dashboard, commandLine, mail or computer"}
		}
	}
	if value := request.Header.Get(headerFilename); value != "" {
		if err := checkText(headerFilename, value, maximumFilenameLength); err != nil {
			return nil, err
		}
		if strings.ContainsAny(value, `/\`) {
			return nil, &headerError{headerFilename, "is a file name, not a path"}
		}
		headers.OriginalFilename = value
	}
	if value := request.Header.Get("Content-Type"); value != "" {
		mediaType, _, err := mime.ParseMediaType(value)
		if err != nil || len(mediaType) > maximumContentTypeLength || !strings.Contains(mediaType, "/") {
			return nil, &headerError{"Content-Type", "is not a media type such as image/jpeg"}
		}
		headers.ContentType = mediaType
	}
	if value := request.Header.Get(headerTakenAt); value != "" {
		takenAt, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, &headerError{headerTakenAt, "must be an RFC 3339 date, such as 2024-05-17T09:30:00+02:00, or 2024-05-17T07:30:00Z when the offset is not known"}
		}
		utc := takenAt.UTC()
		headers.TakenAt = &utc
		// A date written with Z says the instant and nothing about where the
		// photo was taken: a phone knows when, and not in which zone. The
		// offset is left empty, for the file's EXIF OffsetTimeOriginal to
		// fill in when it is read. An uploader that means UTC itself, with
		// the offset known to be zero, writes +00:00.
		if !strings.HasSuffix(value, "Z") {
			_, offsetSeconds := takenAt.Zone()
			offsetMinutes := offsetSeconds / 60
			headers.TakenAtOffsetMinutes = &offsetMinutes
		}
	}
	latitude, err := parseCoordinate(request, headerLatitude, 90)
	if err != nil {
		return nil, err
	}
	longitude, err := parseCoordinate(request, headerLongitude, 180)
	if err != nil {
		return nil, err
	}
	if (latitude == nil) != (longitude == nil) {
		return nil, &headerError{headerLatitude + " and " + headerLongitude, "go together: send both or neither"}
	}
	headers.Latitude, headers.Longitude = latitude, longitude
	if headers.IsFavorite, err = parseFlag(request, headerIsFavorite); err != nil {
		return nil, err
	}
	if headers.IsScreenshot, err = parseFlag(request, headerIsScreenshot); err != nil {
		return nil, err
	}
	if value := request.Header.Get(headerDeclaredSha256); value != "" {
		value = strings.ToLower(value)
		if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
			return nil, &headerError{headerDeclaredSha256, "must be 64 hexadecimal digits"}
		}
		headers.DeclaredSHA256 = value
	}
	return headers, nil
}

// checkText refuses a header that is too long, is not text, or carries a
// control character: these are stored and shown.
func checkText(header, value string, maximumLength int) *headerError {
	if len(value) > maximumLength {
		return &headerError{header, fmt.Sprintf("is longer than %d bytes", maximumLength)}
	}
	if !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return &headerError{header, "must be text without control characters"}
	}
	return nil
}

func parseCoordinate(request *http.Request, header string, limit float64) (*float64, *headerError) {
	value := request.Header.Get(header)
	if value == "" {
		return nil, nil
	}
	coordinate, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate < -limit || coordinate > limit {
		return nil, &headerError{header, fmt.Sprintf("must be a number of degrees from -%g to %g", limit, limit)}
	}
	return &coordinate, nil
}

func parseFlag(request *http.Request, header string) (bool, *headerError) {
	switch strings.ToLower(request.Header.Get(header)) {
	case "":
		return false, nil
	case "true", "1", "?1":
		return true, nil
	case "false", "0", "?0":
		return false, nil
	}
	return false, &headerError{header, "must be true or false"}
}

// parseCompleteness reads whether the request carries the end of the file.
// The protocol has said it two ways: Upload-Complete, ?1 for the end, and
// before that Upload-Incomplete, ?0 for the end. A request that says
// neither is a plain upload and is the whole file.
func parseCompleteness(request *http.Request) (bool, *headerError) {
	if value := request.Header.Get(headerUploadComplete); value != "" {
		switch value {
		case "?1":
			return true, nil
		case "?0":
			return false, nil
		}
		return false, &headerError{headerUploadComplete, "must be ?1 or ?0"}
	}
	if value := request.Header.Get(headerUploadIncomplete); value != "" {
		switch value {
		case "?0":
			return true, nil
		case "?1":
			return false, nil
		}
		return false, &headerError{headerUploadIncomplete, "must be ?1 or ?0"}
	}
	return true, nil
}

// parseInteropVersion reads the draft revision the client speaks, which
// decides how the limit is written, and which is echoed back. Zero says the
// client did not name one.
func parseInteropVersion(request *http.Request) (int, *headerError) {
	value := request.Header.Get(headerInteropVersion)
	if value == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || version < 1 || version > 1000 {
		return 0, &headerError{headerInteropVersion, "must be a number"}
	}
	return version, nil
}

// parseByteCount reads a header that is a count of bytes.
func parseByteCount(request *http.Request, header string) (*int64, *headerError) {
	value := request.Header.Get(header)
	if value == "" {
		return nil, nil
	}
	count, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || count < 0 {
		return nil, &headerError{header, "must be a count of bytes"}
	}
	return &count, nil
}
