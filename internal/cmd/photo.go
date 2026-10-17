package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
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
					&cli.BoolFlag{Name: "trash", Usage: "list the trash instead of the library"},
					&cli.StringFlag{Name: "search", Usage: "only photos whose place, file name or camera has every one of these words"},
					&cli.StringFlag{Name: "after", Usage: "only photos taken on or after this date (2025-07-12 or an RFC 3339 time)"},
					&cli.StringFlag{Name: "before", Usage: "only photos taken before this date"},
					&cli.StringFlag{Name: "kind", Usage: "only pictures, videos or Live Photos: picture, video or livePhoto"},
					&cli.StringFlag{Name: "album", Usage: "only the photos of this album (its id is in teanode photo album list)"},
					&cli.IntFlag{Name: "limit", Value: defaultPhotoListSize, Usage: "photos to list, 1 to 500"},
					&cli.StringFlag{Name: "continue-after", Usage: "continue after this photo, the last one of the previous page"},
				},
				Action: runPhotoList,
			},
			{Name: "show", Usage: "everything known about a photo", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoShow},
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
			{Name: "favorite", Usage: "make a photo a favorite", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "off", Usage: "take it out of the favorites"}}, Action: runPhotoFavorite},
			{Name: "trash", Usage: "move photos to the trash", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoTrash},
			{Name: "restore", Usage: "take photos out of the trash", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoRestore},
			{Name: "delete", Usage: "delete photos in the trash forever", ArgsUsage: "<id>...", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoDelete},
			{Name: "empty-trash", Usage: "delete everything in the trash forever", Flags: []cli.Flag{JSONFlag(), ForceFlag()}, Action: runPhotoEmptyTrash},
			{Name: "library", Usage: "how many photos and videos there are, the space used and the largest file", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoLibrary},
			newPhotoAlbumCommand(),
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

func photoFilterOf(command *cli.Command) (*client.PhotoFilter, error) {
	filter := &client.PhotoFilter{
		PageSize: int(command.Int("limit")), BeforePhotoID: command.String("continue-after"),
		IsTrashed: command.Bool("trash"), AlbumID: command.String("album"), SearchText: strings.TrimSpace(command.String("search")), PhotoKind: models.PhotoKind(command.String("kind")),
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
	if filter.TakenAfter, err = parsePhotoDate("after", command.String("after")); err != nil {
		return nil, err
	}
	if filter.TakenBefore, err = parsePhotoDate("before", command.String("before")); err != nil {
		return nil, err
	}
	return filter, nil
}

// photoTakenText is when a photo was taken, on the clock where it was taken
// when its offset is known, and on this computer's clock when it is not: the
// same rule as the dashboard's Photos page.
func photoTakenText(photo *models.Photo) string {
	if photo.TakenAt == nil {
		return ""
	}
	if photo.TakenAtOffsetMinutes != nil {
		return photo.TakenAt.In(time.FixedZone("", *photo.TakenAtOffsetMinutes*60)).Format("2006-01-02 15:04")
	}
	return photo.TakenAt.In(time.Local).Format("2006-01-02 15:04")
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
		return "trash"
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
	rows := make([][]string, 0, len(photos))
	for _, photo := range photos {
		rows = append(rows, []string{photo.ID, photoTakenText(photo), string(photo.PhotoKind), photo.PlaceName, photoSizeText(photo), photoDurationText(photo), photoStateText(photo), photo.OriginalFilename})
	}
	return printTable([]string{"id", "date", "kind", "place", "size", "length", "state", "file"}, rows)
}

func runPhotoShow(ctx context.Context, command *cli.Command) error {
	photoId := command.Args().First()
	if photoId == "" {
		return usage("usage: teanode photo show <id>")
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
	if photo.Latitude != nil && photo.Longitude != nil {
		fields = append(fields, [2]string{"coordinates", fmt.Sprintf("%.5f, %.5f", *photo.Latitude, *photo.Longitude)})
	}
	if photo.ProcessingErrorMessage != "" {
		fields = append(fields, [2]string{"problem", photo.ProcessingErrorMessage})
	}
	if photo.TrashedAt != nil {
		fields = append(fields, [2]string{"in the trash since", photo.TrashedAt.Local().Format("2006-01-02 15:04")})
	}
	for _, file := range view.PhotoFiles {
		fields = append(fields, [2]string{"file " + string(file.PhotoFileRole), fmt.Sprintf("%s, %s", client.PhotoByteSizeText(file.FileSizeBytes), file.ContentType)})
	}
	for _, album := range view.Albums {
		fields = append(fields, [2]string{"album", fmt.Sprintf("%s (%s)", album.AlbumTitle, album.ID)})
	}
	return printFields(fields)
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
		placeholder, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("%s is already there; --force replaces it", path)
			}
			return err
		}
		_ = placeholder.Close()
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".photo-download-*")
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
	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return err
	}
	isSaved = true
	return nil
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
	_, _ = fmt.Fprintf(command.Writer, "%s %d of %d photos\n", verb, count, len(photoIds))
	return nil
}

func runPhotoTrash(ctx context.Context, command *cli.Command) error {
	return runPhotoMove(ctx, command, client.TrashPhotos, "trashedCount", "moved to the trash")
}

func runPhotoRestore(ctx context.Context, command *cli.Command) error {
	return runPhotoMove(ctx, command, client.RestorePhotos, "restoredCount", "restored")
}

func runPhotoDelete(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() == 0 {
		return usage("usage: teanode photo delete <id>...")
	}
	if err := confirm(command, "This deletes the photos forever; only photos already in the trash are deleted."); err != nil {
		return err
	}
	return runPhotoMove(ctx, command, client.DeletePhotos, "deletedCount", "deleted forever")
}

func runPhotoEmptyTrash(ctx context.Context, command *cli.Command) error {
	if err := confirm(command, "This deletes everything in the trash forever."); err != nil {
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
	_, _ = fmt.Fprintf(command.Writer, "deleted %d photos forever\n", count)
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
		{"photos", fmt.Sprint(library.PhotoCount)}, {"videos", fmt.Sprint(library.VideoCount)},
		{"favorites", fmt.Sprint(library.FavoriteCount)}, {"in the trash", fmt.Sprint(library.TrashedCount)},
		{"space used", library.SpaceUsedText()},
		{"largest file", client.PhotoByteSizeText(library.MaximumFileBytes)},
		{"trash kept for", fmt.Sprintf("%d days", library.TrashRetentionDays)},
	})
}
