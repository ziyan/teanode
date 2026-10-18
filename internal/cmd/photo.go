package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Photos: the library of pictures and videos. The same operations as the
// dashboard's Photos page and the agent's photo tool.

// photoUploadBatchSize is how many files are hashed and asked about before
// any is sent: FindPhotos takes up to 500 names, and a folder of a hundred
// thousand pictures starts uploading before it has been read.
const photoUploadBatchSize = 200

const defaultPhotoListSize = 100

func NewPhotoCommand() *cli.Command {
	return &cli.Command{
		Name:  "photo",
		Usage: "your library of photos and videos",
		Commands: []*cli.Command{
			{
				Name: "list", Usage: "the library, newest first",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.BoolFlag{Name: "favorites", Usage: "only the favorites"},
					&cli.BoolFlag{Name: "trash", Usage: "list the Trash instead of the library"},
					&cli.StringFlag{Name: "search", Usage: "photos matching these words, best match first: the place, file name, camera, album titles, and the description and the text in the picture of a photo that has been described (a photo nobody described is found by the rest)"},
					&cli.StringFlag{Name: "after", Usage: "only photos taken on or after this date (2025-07-12 or an RFC 3339 time)"},
					&cli.StringFlag{Name: "before", Usage: "only photos taken before this date"},
					&cli.StringFlag{Name: "kind", Usage: "only pictures, videos or Live Photos: picture, video or livePhoto"},
					&cli.StringFlag{Name: "near", Usage: "only photos taken near a point: <latitude>,<longitude>,<kilometers>, such as 10.5,20.25,2"},
					&cli.StringFlag{Name: "album", Usage: "only the photos of this album (its id is in teanode photo album list)"},
					&cli.StringFlag{Name: "face-group", Usage: "only the photos with a face in this group of faces (its id is in teanode photo people list)"},
					&cli.StringFlag{Name: "contact", Usage: "only the photos with a face in a group this contact names (its id is in teanode contact list)"},
					&cli.IntFlag{Name: "limit", Value: defaultPhotoListSize, Usage: "photos to list, 1 to 500"},
					&cli.StringFlag{Name: "continue-after", Usage: "continue after this photo, the last one of the previous page"},
				},
				Action: runPhotoList,
			},
			{Name: "read", Usage: "everything known about a photo", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoRead},
			{
				Name: "describe", Usage: "have the assistant look at a photo and write what is in it, so that a search finds it by that", ArgsUsage: "<id>",
				Flags:  []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "again", Usage: "write a new description of a photo that has one"}},
				Action: runPhotoDescribe,
			},
			{
				Name: "upload", Usage: "add pictures and videos; folders are walked, and what the library already has is skipped",
				ArgsUsage: "<file or folder>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoUpload,
			},
			{
				Name: "download", Usage: "save a photo's file", ArgsUsage: "<id>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Value: string(models.PhotoFileRoleOriginal), Usage: "which file: original, edited or liveVideo"},
					&cli.StringFlag{Name: "output", Usage: "where to save it: a file name, or a folder to put it in under its own name (default: its own name here)"},
					&cli.BoolFlag{Name: "force", Usage: "replace a file that is already there"},
				},
				Action: runPhotoDownload,
			},
			{
				Name: "save", Usage: "keep a photo in your own library: a photo from a shared album or a circle's (save <photo id>), or a file from a conversation with your assistant (save --attachment <attachment id>)",
				ArgsUsage: "<photo id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "attachment", Usage: "keep this file of a conversation as a photo, instead of a photo of a shared album (give no photo id with it)"},
				},
				Action: runPhotoSave,
			},
			{
				Name: "set-date", Usage: "correct the date a photo was taken: an RFC 3339 time whose offset is the place's, such as 2025-07-12T14:30:00+09:00", ArgsUsage: "<id> <time>",
				Flags: []cli.Flag{JSONFlag()}, Action: runPhotoSetDate,
			},
			{
				Name: "search-places", Usage: "find cities by name, to say where photos were taken: best match first, nearest first when --near is given, otherwise the largest; two numbers (35.0116, 135.7681) give that position", ArgsUsage: "<words>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "near", Usage: "put the cities nearest this point first: <latitude>,<longitude>, such as 10.5,20.25"},
					&cli.IntFlag{Name: "limit", Value: 10, Usage: "cities to list, 1 to 50"},
				},
				Action: runPhotoSearchPlaces,
			},
			{
				Name: "set-location", Usage: "say where photos were taken: the best match for some words (--place), or a position (--at); the photo is named after the nearest city, and the date it was taken is not changed", ArgsUsage: "<id>...",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "place", Usage: "a city, by name, such as \"kyoto japan\": the best match for these words is used (teanode photo search-places shows the choices)"},
					&cli.StringFlag{Name: "at", Usage: "a position: <latitude>,<longitude>, such as 10.5,20.25"},
				},
				Action: runPhotoSetLocation,
			},
			{Name: "remove-location", Usage: "remove the location from photos, for good: reading the file again does not bring it back (the file itself is not changed)", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoRemoveLocation},
			{Name: "favorite", Usage: "make a photo a favorite", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "off", Usage: "take it out of the favorites"}}, Action: runPhotoFavorite},
			{Name: "trash", Usage: "move photos to the Trash", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoTrash},
			{Name: "restore", Usage: "restore photos from the Trash", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoRestore},
			{
				Name: "retry", Usage: "make the thumbnails of photos again: the ones that failed, and ones that are ready but look wrong", ArgsUsage: "<id>...",
				Flags: []cli.Flag{JSONFlag()}, Action: runPhotoRetry,
			},
			{Name: "delete", Usage: "delete photos in the Trash forever", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoDelete},
			{Name: "empty-trash", Usage: "delete everything in the Trash forever", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoEmptyTrash},
			{Name: "library", Usage: "how many photos there are and how many of them are videos, the space used and the largest file", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoLibrary},
			newPhotoAlbumCommand(),
			newPhotoPeopleCommand(),
			newPhotoFacesCommand(),
		},
	}
}

// parsePhotoDate reads a date given as a day, in the shell's zone, or as an
// RFC 3339 time.
func parsePhotoDate(flag, text string) (*time.Time, error) {
	if text == "" {
		return nil, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", text, time.Local); err == nil {
		return &parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return &parsed, nil
	}
	return nil, usage(fmt.Sprintf("--%s must be a date such as 2025-07-12 or an RFC 3339 time, not %q", flag, text))
}

// parsePhotoNear reads "<latitude>,<longitude>,<kilometers>", the point and
// how near to it a photo was taken.
func parsePhotoNear(text string) (latitude, longitude, kilometers *float64, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil, nil, nil
	}
	parts := strings.Split(text, ",")
	numbers := make([]float64, 0, 3)
	for _, part := range parts {
		number, parseErr := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if parseErr != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, nil, nil, usage(fmt.Sprintf("--near must be <latitude>,<longitude>,<kilometers>, such as 10.5,20.25,2, not %q", text))
		}
		numbers = append(numbers, number)
	}
	if len(numbers) != 3 {
		return nil, nil, nil, usage(fmt.Sprintf("--near must be <latitude>,<longitude>,<kilometers>, such as 10.5,20.25,2, not %q", text))
	}
	if numbers[0] < -90 || numbers[0] > 90 {
		return nil, nil, nil, usage("--near: the latitude is from -90 to 90 degrees")
	}
	if numbers[1] < -180 || numbers[1] > 180 {
		return nil, nil, nil, usage("--near: the longitude is from -180 to 180 degrees")
	}
	if numbers[2] <= 0 {
		return nil, nil, nil, usage("--near: the distance is more than 0 kilometers")
	}
	return &numbers[0], &numbers[1], &numbers[2], nil
}

// parsePhotoPoint reads "<latitude>,<longitude>", a position on the earth.
func parsePhotoPoint(flag, text string) (latitude, longitude float64, err error) {
	parts := strings.Split(strings.TrimSpace(text), ",")
	if len(parts) != 2 {
		return 0, 0, usage(fmt.Sprintf("--%s must be <latitude>,<longitude>, such as 10.5,20.25, not %q", flag, text))
	}
	latitude, latitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	longitude, longitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if latitudeErr != nil || longitudeErr != nil || math.IsNaN(latitude) || math.IsNaN(longitude) {
		return 0, 0, usage(fmt.Sprintf("--%s must be <latitude>,<longitude>, such as 10.5,20.25, not %q", flag, text))
	}
	if latitude < -90 || latitude > 90 {
		return 0, 0, usage(fmt.Sprintf("--%s: the latitude is from -90 to 90 degrees", flag))
	}
	if longitude < -180 || longitude > 180 {
		return 0, 0, usage(fmt.Sprintf("--%s: the longitude is from -180 to 180 degrees", flag))
	}
	return latitude, longitude, nil
}

func photoFilterOf(command *cli.Command) (*client.PhotoFilter, error) {
	filter := &client.PhotoFilter{
		PageSize: int(command.Int("limit")), BeforePhotoID: command.String("continue-after"),
		IsTrashed: command.Bool("trash"), AlbumID: command.String("album"), FaceGroupID: command.String("face-group"), ContactID: command.String("contact"), SearchText: strings.TrimSpace(command.String("search")), PhotoKind: models.PhotoKind(command.String("kind")),
	}
	if filter.PageSize < 1 || filter.PageSize > 500 {
		return nil, usage("--limit must be from 1 to 500")
	}
	if filter.PhotoKind != "" && !filter.PhotoKind.IsValid() {
		return nil, usage(fmt.Sprintf("--kind must be one of picture, video, livePhoto, not %q", filter.PhotoKind))
	}
	if command.Bool("favorites") {
		isFavorite := true
		filter.IsFavorite = &isFavorite
	}
	var err error
	if filter.NearLatitude, filter.NearLongitude, filter.NearDistanceKilometers, err = parsePhotoNear(command.String("near")); err != nil {
		return nil, err
	}
	if filter.TakenAfter, err = parsePhotoDate("after", command.String("after")); err != nil {
		return nil, err
	}
	if filter.TakenBefore, err = parsePhotoDate("before", command.String("before")); err != nil {
		return nil, err
	}
	return filter, nil
}

// photoTakenText is when a photo was taken, on the clock where it was taken:
// the photo's offset says which, the same rule as the dashboard's Photos page
// and the agent's photo tool.
func photoTakenText(photo *models.Photo) string {
	return client.PhotoTakenText(photo)
}

// photoDateText is photoTakenText for the details of one photo, with the
// offset said when it is known so that the reader sees it is not their own
// clock.
func photoDateText(photo *models.Photo) string {
	text := photoTakenText(photo)
	if text == "" || photo.TakenAtOffsetMinutes == nil {
		return text
	}
	return text + " (" + client.PhotoOffsetText(*photo.TakenAtOffsetMinutes) + ")"
}

func photoDurationText(photo *models.Photo) string {
	if photo.DurationMS <= 0 {
		return ""
	}
	duration := (time.Duration(photo.DurationMS) * time.Millisecond).Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(duration.Minutes()), int(duration.Seconds())%60)
}

func photoSizeText(photo *models.Photo) string {
	if photo.WidthPixels == 0 || photo.HeightPixels == 0 {
		return ""
	}
	return fmt.Sprintf("%dx%d", photo.WidthPixels, photo.HeightPixels)
}

func photoStateText(photo *models.Photo) string {
	switch {
	case photo.PhotoProcessingState == models.PhotoProcessingFailed:
		return "failed"
	case photo.PhotoProcessingState == models.PhotoProcessingWaiting:
		return "processing"
	case photo.TrashedAt != nil:
		return "Trash"
	case photo.IsFavorite:
		return "favorite"
	}
	return ""
}

func runPhotoList(ctx context.Context, command *cli.Command) error {
	filter, err := photoFilterOf(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	photos, err := client.ListPhotos(ctx, connection, filter)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(photos)
	}
	if len(photos) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no photos here")
		return nil
	}
	if err := printPhotoTable(photos); err != nil {
		return err
	}
	// A search by words cannot see into photos nobody described, and a range
	// is where to look at them, so the range's share is said.
	if filter.SearchText != "" && (filter.TakenAfter != nil || filter.TakenBefore != nil) {
		if undescribed, err := client.CountUndescribedPhotos(ctx, connection, filter); err == nil && undescribed > 0 {
			fmt.Fprintf(os.Stderr, "note: %d photos in these dates have no description, so --search cannot find them by what is in them; teanode photo describe <id> writes one\n", undescribed)
		}
	}
	// The page is not the whole library, and says so, once the whole is
	// known; the count is a second request and only a full page needs it.
	if len(photos) == filter.PageSize {
		countFilter := *filter
		countFilter.BeforePhotoID, countFilter.PageSize = "", 0
		if total, err := client.CountPhotos(ctx, connection, &countFilter); err == nil && total > len(photos) {
			fmt.Fprintf(os.Stderr, "note: %d photos listed of %d that match; add --continue-after %s for the next page\n", len(photos), total, photos[len(photos)-1].ID)
		}
	}
	return nil
}

// printPhotoTable writes a page of photos, one a row.
func printPhotoTable(photos []*models.Photo) error {
	// A listing of an album holds other people's photos too; then each row
	// says whose it is.
	hasOthers := false
	for _, photo := range photos {
		hasOthers = hasOthers || photo.OwnerName != ""
	}
	rows := make([][]string, 0, len(photos))
	for _, photo := range photos {
		row := []string{photo.ID, photoTakenText(photo), string(photo.PhotoKind), photo.PlaceName, photoSizeText(photo), photoDurationText(photo), photoStateText(photo), photo.OriginalFilename}
		if hasOthers {
			owner := "you"
			if photo.OwnerName != "" {
				owner = forTerminal(photo.OwnerName)
			}
			row = append(row, owner)
		}
		rows = append(rows, row)
	}
	headers := []string{"id", "date", "kind", "place", "size", "length", "state", "file"}
	if hasOthers {
		headers = append(headers, "owner")
	}
	return printTable(headers, rows)
}

func runPhotoRead(ctx context.Context, command *cli.Command) error {
	photoId := command.Args().First()
	if photoId == "" {
		return usage("usage: teanode photo read <id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadPhoto(ctx, connection, photoId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	photo := view.Photo
	fields := [][2]string{
		{"id", photo.ID}, {"kind", string(photo.PhotoKind)}, {"date", photoDateText(photo)}, {"place", photo.PlaceName},
		{"size", photoSizeText(photo)}, {"length", photoDurationText(photo)}, {"camera", strings.TrimSpace(photo.CameraMake + " " + photo.CameraModel)},
		{"file name", photo.OriginalFilename}, {"favorite", yesNo(photo.IsFavorite)}, {"from", string(photo.PhotoSource)},
		{"added", photo.CreatedAt.Local().Format("2006-01-02 15:04")}, {"processing", string(photo.PhotoProcessingState)},
	}
	if photo.OwnerName != "" {
		fields = append(fields, [2]string{"owner", forTerminal(photo.OwnerName)})
	}
	if photo.Latitude != nil && photo.Longitude != nil {
		fields = append(fields, [2]string{"coordinates", fmt.Sprintf("%.5f, %.5f", *photo.Latitude, *photo.Longitude)})
	}
	if photo.ProcessingErrorMessage != "" {
		fields = append(fields, [2]string{"problem", photo.ProcessingErrorMessage})
	}
	switch {
	case photo.DescribedAt != nil:
		fields = append(fields, [2]string{"description", photo.PhotoDescription})
		if photo.DescriptionVisibleText != "" {
			for index, line := range strings.Split(photo.DescriptionVisibleText, "\n") {
				name := ""
				if index == 0 {
					name = "text in the picture"
				}
				fields = append(fields, [2]string{name, line})
			}
		}
	case photo.IsDescriptionRequested:
		fields = append(fields, [2]string{"description", "on its way"})
	default:
		fields = append(fields, [2]string{"description", "none yet (teanode photo describe " + photo.ID + ")"})
	}
	if photo.TrashedAt != nil {
		fields = append(fields, [2]string{"in the Trash since", photo.TrashedAt.Local().Format("2006-01-02 15:04")})
	}
	for _, file := range view.PhotoFiles {
		fields = append(fields, [2]string{"file " + string(file.PhotoFileRole), fmt.Sprintf("%s, %s", client.PhotoByteSizeText(file.FileSizeBytes), file.ContentType)})
	}
	for _, album := range view.Albums {
		fields = append(fields, [2]string{"album", fmt.Sprintf("%s (%s)", album.AlbumTitle, album.ID)})
	}
	for _, face := range view.Faces {
		fields = append(fields, [2]string{"person", photoFaceText(face)})
	}
	return printFields(fields)
}

func runPhotoDescribe(ctx context.Context, command *cli.Command) error {
	photoId := command.Args().First()
	if photoId == "" {
		return usage("usage: teanode photo describe <id> [--again]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	photo, err := client.DescribePhoto(ctx, connection, photoId, command.Bool("again"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(photo)
	}
	switch {
	case photo.IsDescriptionRequested:
		_, _ = fmt.Fprintf(command.Writer, "%s: describing; it takes a few seconds, then teanode photo read %s has it\n", photo.ID, photo.ID)
	case photo.DescribedAt != nil:
		_, _ = fmt.Fprintf(command.Writer, "%s: already described (add --again to write a new description)\n%s\n", photo.ID, forTerminal(photo.PhotoDescription))
	default:
		_, _ = fmt.Fprintf(command.Writer, "%s: not described\n", photo.ID)
	}
	return nil
}

// photoExtensionContentTypes are the files a camera or a phone writes, by
// extension: the types the library keeps, which the server also settles from
// the name when it is not told. A file in a folder is uploaded only if it
// is on this list, so that a folder's sidecar files are not sent to be
// refused.
var photoExtensionContentTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif", ".tif": "image/tiff", ".tiff": "image/tiff",
	".dng": "image/x-adobe-dng", ".nef": "image/x-nikon-nef", ".cr2": "image/x-canon-cr2", ".cr3": "image/x-canon-cr3",
	".arw": "image/x-sony-arw", ".raf": "image/x-fuji-raf", ".orf": "image/x-olympus-orf", ".rw2": "image/x-panasonic-rw2",
	".mov": "video/quicktime", ".mp4": "video/mp4", ".m4v": "video/x-m4v", ".3gp": "video/3gpp", ".3g2": "video/3gpp2",
	".webm": "video/webm", ".mkv": "video/x-matroska",
}

// photoContentType is the type a file is sent as, or empty when its name is
// not one a camera writes.
func photoContentType(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	if known, isKnown := photoExtensionContentTypes[extension]; isKnown {
		return known
	}
	return ""
}

// photoFileToUpload is a file found on its way to the library.
type photoFileToUpload struct {
	path      string
	sizeBytes int64
	// sha256 is the digest of the file's bytes, which is the name the
	// library knows the photo by.
	sha256 string
}

// collectPhotoPaths turns the arguments into the files they name: a file is
// taken as it is, and a folder is walked for the pictures and videos in it,
// leaving out what is hidden (a name that starts with a dot, and everything
// under such a folder). The count is of files in folders that are not
// pictures or videos.
func collectPhotoPaths(arguments []string) (paths []string, otherFileCount int, err error) {
	seen := map[string]bool{}
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	for _, argument := range arguments {
		info, err := os.Stat(argument)
		if err != nil {
			return nil, 0, err
		}
		if !info.IsDir() {
			add(argument)
			continue
		}
		walkError := filepath.WalkDir(argument, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path != argument && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			// A link to a file is followed; a link to a folder is not, so a
			// loop cannot trap the walk.
			if entry.Type()&fs.ModeSymlink != 0 {
				if target, err := os.Stat(path); err != nil || !target.Mode().IsRegular() {
					return nil
				}
			} else if !entry.Type().IsRegular() {
				return nil
			}
			if photoContentType(path) == "" {
				otherFileCount++
				return nil
			}
			add(path)
			return nil
		})
		if walkError != nil {
			return nil, 0, walkError
		}
	}
	return paths, otherFileCount, nil
}

// hashPhotoFile reads the file once, in pieces, for its digest.
func hashPhotoFile(ctx context.Context, path string) (*photoFileToUpload, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	sizeBytes, err := io.Copy(digest, &contextReader{ctx: ctx, source: file})
	if err != nil {
		return nil, err
	}
	return &photoFileToUpload{path: path, sizeBytes: sizeBytes, sha256: hex.EncodeToString(digest.Sum(nil))}, nil
}

// contextReader stops a long read when the command is interrupted.
type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (self *contextReader) Read(content []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return self.source.Read(content)
}

// photoUploadResult is what became of one file, for --json.
type photoUploadResult struct {
	Path         string `json:"path"`
	Outcome      string `json:"outcome"`
	PhotoID      string `json:"photoId,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

const (
	photoOutcomeUploaded     = "uploaded"
	photoOutcomeAlreadyThere = "alreadyThere"
	photoOutcomeFailed       = "failed"
)

func runPhotoUpload(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	if len(arguments) == 0 {
		return usage("usage: teanode photo upload <file or folder>...")
	}
	paths, otherFileCount, err := collectPhotoPaths(arguments)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isJson := command.Bool("json")
	var results []*photoUploadResult
	report := func(result *photoUploadResult) {
		results = append(results, result)
		if isJson {
			return
		}
		switch result.Outcome {
		case photoOutcomeUploaded:
			_, _ = fmt.Fprintf(command.Writer, "uploaded         %s (%s)\n", forTerminal(result.Path), result.PhotoID)
		case photoOutcomeAlreadyThere:
			_, _ = fmt.Fprintf(command.Writer, "already there    %s\n", forTerminal(result.Path))
		default:
			_, _ = fmt.Fprintf(command.Writer, "failed           %s: %s\n", forTerminal(result.Path), forTerminal(result.ErrorMessage))
		}
	}

	for start := 0; start < len(paths); start += photoUploadBatchSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := paths[start:min(start+photoUploadBatchSize, len(paths))]
		hashed := make([]*photoFileToUpload, 0, len(batch))
		clientPhotoIds := make([]string, 0, len(batch))
		for _, path := range batch {
			file, err := hashPhotoFile(ctx, path)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				report(&photoUploadResult{Path: path, Outcome: photoOutcomeFailed, ErrorMessage: err.Error()})
				continue
			}
			if file.sizeBytes == 0 {
				report(&photoUploadResult{Path: path, Outcome: photoOutcomeFailed, ErrorMessage: "the file is empty"})
				continue
			}
			hashed = append(hashed, file)
			clientPhotoIds = append(clientPhotoIds, file.sha256)
		}
		known := map[string]bool{}
		if len(clientPhotoIds) > 0 {
			found, err := client.FindPhotos(ctx, connection, clientPhotoIds)
			if err != nil {
				return describeError(command, err)
			}
			for _, photo := range found {
				known[photo.ClientPhotoID] = true
			}
		}
		for _, file := range hashed {
			if known[file.sha256] {
				report(&photoUploadResult{Path: file.path, Outcome: photoOutcomeAlreadyThere})
				// Two copies of one picture in the same batch are one photo.
				continue
			}
			photoId, err := uploadPhotoFile(ctx, connection, file, !isJson)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				report(&photoUploadResult{Path: file.path, Outcome: photoOutcomeFailed, ErrorMessage: err.Error()})
				continue
			}
			known[file.sha256] = true
			report(&photoUploadResult{Path: file.path, Outcome: photoOutcomeUploaded, PhotoID: photoId})
		}
	}

	counts := map[string]int{}
	for _, result := range results {
		counts[result.Outcome]++
	}
	if isJson {
		if err := PrintJSON(map[string]any{
			"results": results, "uploadedCount": counts[photoOutcomeUploaded], "alreadyThereCount": counts[photoOutcomeAlreadyThere],
			"failedCount": counts[photoOutcomeFailed], "skippedFileCount": otherFileCount,
		}); err != nil {
			return err
		}
	} else {
		summary := fmt.Sprintf("%d uploaded, %d already there, %d failed", counts[photoOutcomeUploaded], counts[photoOutcomeAlreadyThere], counts[photoOutcomeFailed])
		if otherFileCount > 0 {
			summary += fmt.Sprintf(" (%d files in the folders are not pictures or videos and were left out)", otherFileCount)
		}
		_, _ = fmt.Fprintln(command.Writer, summary)
	}
	if counts[photoOutcomeFailed] > 0 {
		return fmt.Errorf("%d of the files could not be uploaded", counts[photoOutcomeFailed])
	}
	return nil
}

// uploadPhotoFile sends one file. It does not keep anything on disk: an
// upload cut short is started again the next time the command runs, and the
// server deletes the part it was left with when it has sat idle for a week.
func uploadPhotoFile(ctx context.Context, connection *client.Client, file *photoFileToUpload, isProgressWanted bool) (string, error) {
	source, err := os.Open(file.path)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	contentType := photoContentType(file.path)
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(file.path)))
	}
	upload := &client.PhotoUpload{
		Source: source, SizeBytes: file.sizeBytes, ClientPhotoID: file.sha256, Filename: filepath.Base(file.path),
		ContentType: contentType, DeclaredSHA256: file.sha256, PhotoSource: models.PhotoSourceCommandLine,
	}
	// A progress figure is for a person at a terminal, and only a file of
	// more than a part has a middle.
	if isProgressWanted && file.sizeBytes > models.PhotoUploadPartBytes && term.IsTerminal(int(os.Stderr.Fd())) {
		upload.OnProgress = func(storedBytes int64) {
			fmt.Fprintf(os.Stderr, "\r%s: %d%%", forTerminal(filepath.Base(file.path)), storedBytes*100/file.sizeBytes)
		}
		defer fmt.Fprint(os.Stderr, "\r\x1b[2K")
	}
	return connection.UploadPhoto(ctx, upload)
}

// photoDownloadName is the name to save a file under: its own, as the
// server knows it, reduced to a name and not a path, because the server's
// text is somebody else's.
func photoDownloadName(photo *models.Photo, file *models.PhotoFile) string {
	name := file.OriginalFilename
	if name == "" {
		name = photo.OriginalFilename
	}
	name = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, name)
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		extensions, _ := mime.ExtensionsByType(file.ContentType)
		name = photo.ID
		if known, isKnown := reverseContentExtension(file.ContentType); isKnown {
			name += known
		} else if len(extensions) > 0 {
			name += extensions[0]
		}
	}
	return name
}

func reverseContentExtension(contentType string) (string, bool) {
	best := ""
	for extension, candidate := range photoExtensionContentTypes {
		if candidate == contentType && (best == "" || len(extension) < len(best) || (len(extension) == len(best) && extension < best)) {
			best = extension
		}
	}
	return best, best != ""
}

// photoDownloadPath is where the file goes: the given output, a file in the
// given folder, or the file's own name here.
func photoDownloadPath(output, name string) string {
	if output == "" {
		return name
	}
	if info, err := os.Stat(output); err == nil && info.IsDir() {
		return filepath.Join(output, name)
	}
	return output
}

func runPhotoDownload(ctx context.Context, command *cli.Command) error {
	photoId := command.Args().First()
	if photoId == "" {
		return usage("usage: teanode photo download <id> [--file original|edited|liveVideo] [--output PATH] [--force]")
	}
	role := models.PhotoFileRole(command.String("file"))
	if !role.IsValid() {
		return usage(fmt.Sprintf("--file must be original, edited or liveVideo, not %q", role))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadPhoto(ctx, connection, photoId)
	if err != nil {
		return describeError(command, err)
	}
	var file *models.PhotoFile
	for _, candidate := range view.PhotoFiles {
		if candidate.PhotoFileRole == role {
			file = candidate
		}
	}
	if file == nil {
		roles := make([]string, 0, len(view.PhotoFiles))
		for _, candidate := range view.PhotoFiles {
			roles = append(roles, string(candidate.PhotoFileRole))
		}
		return fmt.Errorf("this photo has no %s file; it has %s", role, strings.Join(roles, ", "))
	}
	path := photoDownloadPath(command.String("output"), photoDownloadName(view.Photo, file))
	if err := savePhotoFile(ctx, connection, file, path, command.Bool("force")); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "saved %s (%s)\n", forTerminal(path), client.PhotoByteSizeText(file.FileSizeBytes))
	return nil
}

// savePhotoFile streams a file to a path. It is written under another name
// and moved into place only when all of it has arrived, so that a file cut
// short is never left looking like the photo. Without force it never replaces
// a file: the name is taken first, so that two downloads racing for one name
// do not both win.
func savePhotoFile(ctx context.Context, connection *client.Client, file *models.PhotoFile, path string, isForced bool) error {
	if !isForced {
		placeholder, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%s is already there; --force replaces it", path)
			}
			return err
		}
		_ = placeholder.Close()
	}
	temporary, err := createDownloadTemporary(filepath.Dir(path))
	if err != nil {
		if !isForced {
			_ = os.Remove(path)
		}
		return err
	}
	isSaved := false
	defer func() {
		if !isSaved {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
			if !isForced {
				_ = os.Remove(path)
			}
		}
	}()
	if err := connection.DownloadPhotoFile(ctx, file, temporary); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return err
	}
	isSaved = true
	return nil
}

// createDownloadTemporary makes the file a download is written to. It is made
// with the mode 0666 and left for the umask to narrow, as any program's new file
// is: a person who keeps a umask of 077 gets a file only they can read, and not
// the world-readable one that forcing 0644 would give them. (os.CreateTemp
// makes a file only its owner can read, which is the other way to be wrong:
// the photo would be unreadable to the group a shared directory is for.)
func createDownloadTemporary(directory string) (*os.File, error) {
	for range 10 {
		random := make([]byte, 8)
		if _, err := rand.Read(random); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(filepath.Join(directory, ".photo-download-"+hex.EncodeToString(random)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if !errors.Is(err, fs.ErrExist) {
			return file, err
		}
	}
	return nil, fmt.Errorf("a file to download to could not be made in %s", directory)
}

func runPhotoSave(ctx context.Context, command *cli.Command) error {
	attachmentId := strings.TrimSpace(command.String("attachment"))
	photoId := command.Args().First()
	if (attachmentId == "") == (photoId == "") || command.Args().Len() > 1 {
		return usage("usage: teanode photo save <photo id> (a photo of a shared album or a circle's), or teanode photo save --attachment <attachment id> (a file of a conversation)")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var saved *models.Photo
	if attachmentId != "" {
		saved, err = client.SavePhotoFromAttachment(ctx, connection, attachmentId)
		if err != nil {
			return describeNotFound(command, err, "attachment "+attachmentId)
		}
	} else {
		saved, err = client.SavePhotoToLibrary(ctx, connection, photoId)
		if err != nil {
			return describeNotFound(command, err, "photo "+photoId)
		}
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	_, _ = fmt.Fprintf(command.Writer, "saved to your library as %s\n", saved.ID)
	return nil
}

// runPhotoSetDate corrects the date a photo was taken. The offset of the time is
// the offset the photo is shown at, so it is required: a time with no offset
// would be read on the server's clock, which is not where the photo was taken.
func runPhotoSetDate(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode photo set-date <id> <time>, such as 2025-07-12T14:30:00+09:00 (the offset is the place's)")
	}
	photoId := command.Args().First()
	takenAt, err := parsePhotoTakenAt(command.Args().Get(1))
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	photo, err := client.SetPhotoTakenAt(ctx, connection, photoId, takenAt)
	if err != nil {
		return describeNotFound(command, err, "photo "+photoId)
	}
	if command.Bool("json") {
		return PrintJSON(photo)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s was taken %s\n", photo.ID, photoDateText(photo))
	return nil
}

// parsePhotoTakenAt reads an RFC 3339 time that has an offset.
func parsePhotoTakenAt(text string) (time.Time, error) {
	takenAt, err := client.ParsePhotoTakenAt(text)
	if err != nil {
		return time.Time{}, usage(err.Error())
	}
	return takenAt, nil
}

func runPhotoSearchPlaces(ctx context.Context, command *cli.Command) error {
	words := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if words == "" {
		return usage("usage: teanode photo search-places <words> [--near <latitude>,<longitude>]")
	}
	limit := int(command.Int("limit"))
	if limit < 1 || limit > 50 {
		return usage("--limit is from 1 to 50")
	}
	var nearLatitude, nearLongitude *float64
	if near := command.String("near"); strings.TrimSpace(near) != "" {
		latitude, longitude, err := parsePhotoPoint("near", near)
		if err != nil {
			return err
		}
		nearLatitude, nearLongitude = &latitude, &longitude
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	places, err := client.SearchPhotoPlaces(ctx, connection, words, nearLatitude, nearLongitude, limit)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(places)
	}
	if len(places) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no city matches")
		return nil
	}
	rows := make([][]string, 0, len(places))
	for _, found := range places {
		row := []string{forTerminal(found.PlaceName), strconv.FormatFloat(found.Latitude, 'f', 4, 64), strconv.FormatFloat(found.Longitude, 'f', 4, 64)}
		if found.DistanceKilometers != nil {
			row = append(row, strconv.FormatFloat(*found.DistanceKilometers, 'f', 1, 64))
		}
		rows = append(rows, row)
	}
	headers := []string{"place", "latitude", "longitude"}
	if nearLatitude != nil {
		headers = append(headers, "kilometers")
	}
	return printTable(headers, rows)
}

// runPhotoSetLocation says where photos were taken. A place given as words is
// the best match for them, looked up first, so that what is stored is a
// position the person can see the name of.
func runPhotoSetLocation(ctx context.Context, command *cli.Command) error {
	photoIds := command.Args().Slice()
	placeWords, positionText := strings.TrimSpace(command.String("place")), strings.TrimSpace(command.String("at"))
	if len(photoIds) == 0 || (placeWords == "") == (positionText == "") {
		return usage("usage: teanode photo set-location <id>... --place <words>, or --at <latitude>,<longitude> (exactly one of them)")
	}
	if len(photoIds) > client.MaximumPhotoIDs {
		return usage(fmt.Sprintf("name at most %d photos at once", client.MaximumPhotoIDs))
	}
	var latitude, longitude *float64
	if positionText != "" {
		parsedLatitude, parsedLongitude, err := parsePhotoPoint("at", positionText)
		if err != nil {
			return err
		}
		latitude, longitude = &parsedLatitude, &parsedLongitude
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := client.ResolvePhotoPlace(ctx, connection, placeWords, latitude, longitude)
	if errors.Is(err, client.ErrNoPlaceMatches) {
		return usage(fmt.Sprintf("no city matches %q; teanode photo search-places shows what the table has, or give a position with --at", placeWords))
	}
	if err != nil {
		return describeError(command, err)
	}
	changedCount, err := client.SetPhotoLocation(ctx, connection, photoIds, found.Latitude, found.Longitude)
	if err != nil {
		return describeError(command, err)
	}
	// The name the photos have now, which is the nearest city's and can be
	// another than the one picked, or none.
	placeName := client.PlacedName(ctx, connection, photoIds, found.Latitude, found.Longitude)
	if command.Bool("json") {
		return PrintJSON(map[string]any{"changedCount": changedCount, "latitude": found.Latitude, "longitude": found.Longitude, "placeName": placeName})
	}
	placedText := placeName
	if placedText == "" {
		placedText = client.PositionText(found.Latitude, found.Longitude)
	}
	_, _ = fmt.Fprintf(command.Writer, "set the location of %d of %d photos to %s\n", changedCount, len(photoIds), forTerminal(placedText))
	return nil
}

func runPhotoRemoveLocation(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() == 0 {
		return usage("usage: teanode photo remove-location <id>...")
	}
	if err := confirm(command, "This removes the location from these photos for good; TeaNode keeps no copy of it. Copies already shared or downloaded keep the location."); err != nil {
		return err
	}
	return runPhotoMove(ctx, command, client.RemovePhotoLocation, "changedCount", "removed the location from")
}

func runPhotoFavorite(ctx context.Context, command *cli.Command) error {
	photoId := command.Args().First()
	if photoId == "" {
		return usage("usage: teanode photo favorite <id> [--off]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	photo, err := client.SetPhotoFavorite(ctx, connection, photoId, !command.Bool("off"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(photo)
	}
	if photo.IsFavorite {
		_, _ = fmt.Fprintf(command.Writer, "%s: favorite\n", photo.ID)
	} else {
		_, _ = fmt.Fprintf(command.Writer, "%s: not a favorite\n", photo.ID)
	}
	return nil
}

// runPhotoMove is trash and restore: the same shape, a list of photos in and
// a count out.
func runPhotoMove(ctx context.Context, command *cli.Command, move func(context.Context, client.Executor, []string) (int, error), countName, verb string) error {
	photoIds := command.Args().Slice()
	if len(photoIds) == 0 {
		return usage(fmt.Sprintf("usage: teanode photo %s <id>...", command.Name))
	}
	if len(photoIds) > client.MaximumPhotoIDs {
		return usage(fmt.Sprintf("name at most %d photos at once", client.MaximumPhotoIDs))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	count, err := move(ctx, connection, photoIds)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{countName: count})
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %d of %d photos", verb, count, len(photoIds))
	// Only photos in the Trash are deleted, and the server leaves the rest
	// alone, so a lower count needs saying why.
	if countName == "deletedCount" && count < len(photoIds) {
		_, _ = fmt.Fprint(command.Writer, "; the others were not in the Trash and were left alone")
	}
	_, _ = fmt.Fprintln(command.Writer)
	return nil
}

func runPhotoTrash(ctx context.Context, command *cli.Command) error {
	return runPhotoMove(ctx, command, client.TrashPhotos, "trashedCount", "moved to the Trash")
}

func runPhotoRestore(ctx context.Context, command *cli.Command) error {
	return runPhotoMove(ctx, command, client.RestorePhotos, "restoredCount", "restored")
}

func runPhotoRetry(ctx context.Context, command *cli.Command) error {
	return runPhotoMove(ctx, command, client.RetryPhotoProcessing, "retriedCount", "queued to make thumbnails again for")
}

func runPhotoDelete(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() == 0 {
		return usage("usage: teanode photo delete <id>...")
	}
	if err := confirm(command, "This deletes the photos forever; only photos already in the Trash are deleted."); err != nil {
		return err
	}
	return runPhotoMove(ctx, command, client.DeletePhotos, "deletedCount", "deleted forever")
}

func runPhotoEmptyTrash(ctx context.Context, command *cli.Command) error {
	if err := confirm(command, "This deletes everything in the Trash forever."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	count, err := client.EmptyPhotoTrash(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{"deletedCount": count})
	}
	_, _ = fmt.Fprintf(command.Writer, "deleted %s forever\n", client.PhotosCountText(count))
	return nil
}

func runPhotoLibrary(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	library, err := client.ReadPhotoLibrary(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(library)
	}
	return printFields([][2]string{
		{"photos", client.PhotoCountText(library)},
		{"favorites", fmt.Sprint(library.FavoriteCount)}, {"in the Trash", fmt.Sprint(library.TrashedCount)},
		{"space used", library.SpaceUsedText()},
		{"largest file", client.PhotoByteSizeText(library.MaximumFileBytes)},
		{"Trash kept for", fmt.Sprintf("%d days", library.TrashRetentionDays)},
		{"finding faces", photoFaceRecognitionText(library)},
	})
}
