package recording

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

var log = logging.MustGetLogger("recording")

// creationTimeProbeTimeout bounds one look at a container's tags; the date is
// a convenience, so a slow or hostile file must not hold the upload open.
const creationTimeProbeTimeout = 30 * time.Second

// earliestPlausibleCreationTime rejects the zero dates (1904, 1970) that
// containers carry when a recorder had no clock.
var earliestPlausibleCreationTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

type creationTimeProbe struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

// ParseCreationTime reads the moment a container says it was made. The
// Apple tag is tried first because it keeps the local offset; a value that
// does not parse, lies in the future or predates 2000 is not a date.
func ParseCreationTime(tags map[string]string, now time.Time) (time.Time, bool) {
	lowered := make(map[string]string, len(tags))
	for name, value := range tags {
		lowered[strings.ToLower(name)] = strings.TrimSpace(value)
	}
	for _, name := range []string{"com.apple.quicktime.creationdate", "creation_time"} {
		text := lowered[name]
		if text == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			parsed, err := time.Parse(layout, text)
			if err != nil {
				continue
			}
			parsed = parsed.UTC().Truncate(time.Microsecond)
			if parsed.Before(earliestPlausibleCreationTime) || parsed.After(now) {
				break
			}
			return parsed, true
		}
	}
	return time.Time{}, false
}

// ProbeCreationTime asks ffprobe for the container's creation tags through a
// descriptor, as decoding does. Any failure means no date.
func ProbeCreationTime(ctx context.Context, file *os.File) (time.Time, bool) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return time.Time{}, false
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return time.Time{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, creationTimeProbeTimeout)
	defer cancel()
	arguments := []string{"-v", "error"}
	arguments = append(arguments, mediaDescriptorArguments()...)
	arguments = append(arguments, "-show_entries", "format_tags=creation_time,com.apple.quicktime.creationdate", "-of", "json")
	command := exec.CommandContext(ctx, ffprobePath, arguments...)
	command.ExtraFiles = []*os.File{file}
	output := &boundedCommandOutput{maximumBytes: 64 << 10}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return time.Time{}, false
	}
	var probe creationTimeProbe
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return time.Time{}, false
	}
	return ParseCreationTime(probe.Format.Tags, time.Now())
}

// recordCreationTime gives an imported original that arrived without an
// occurrence date the one its container carries. It never replaces a date
// somebody set, and any failure leaves the recording as it was.
func (self *Store) recordCreationTime(ctx context.Context, userId, mediaId string) {
	var media *models.RecordingMedia
	var source *models.Recording
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		if media, err = transaction.GetRecordingMedia(userId, mediaId); err != nil {
			return err
		}
		source, err = transaction.GetRecording(userId, media.RecordingID)
		return err
	}); err != nil || source.RecordedAt != nil || source.RecordingOrigin == models.RecordingOriginCapture {
		return
	}
	file, err := PrivateMediaFile()
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	if err := self.CopyRange(ctx, userId, mediaId, file, 0, media.OriginalSizeBytes); err != nil {
		return
	}
	recordedAt, isFound := ProbeCreationTime(ctx, file)
	if !isFound {
		return
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.SetRecordingRecordedAtIfUnset(userId, source.ID, recordedAt)
		return err
	}); err != nil {
		log.Warningf("cannot record the creation time of recording %q: %s", source.ID, err)
	}
}
