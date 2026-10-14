package recording

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
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
		DurationSeconds string            `json:"duration"`
		Tags            map[string]string `json:"tags"`
	} `json:"format"`
}

// importedContainer is what one look at an imported original tells: when it
// was made, when the container says so, and how long it plays.
type importedContainer struct {
	RecordedAt      time.Time
	HasRecordedAt   bool
	MediaDurationMS int64
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
	container := probeImportedContainer(ctx, file)
	return container.RecordedAt, container.HasRecordedAt
}

// probeImportedContainer reads the creation tags and the playing length of a
// container through a descriptor. Any failure leaves the part it concerns empty.
func probeImportedContainer(ctx context.Context, file *os.File) importedContainer {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return importedContainer{}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return importedContainer{}
	}
	ctx, cancel := context.WithTimeout(ctx, creationTimeProbeTimeout)
	defer cancel()
	arguments := []string{"-v", "error"}
	arguments = append(arguments, mediaDescriptorArguments()...)
	arguments = append(arguments, "-show_entries", "format=duration:format_tags=creation_time,com.apple.quicktime.creationdate", "-of", "json")
	command := exec.CommandContext(ctx, ffprobePath, arguments...)
	command.ExtraFiles = []*os.File{file}
	output := &boundedCommandOutput{maximumBytes: 64 << 10}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return importedContainer{}
	}
	var probe creationTimeProbe
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return importedContainer{}
	}
	var container importedContainer
	container.RecordedAt, container.HasRecordedAt = ParseCreationTime(probe.Format.Tags, time.Now())
	if durationSeconds, err := strconv.ParseFloat(strings.TrimSpace(probe.Format.DurationSeconds), 64); err == nil && durationSeconds > 0 && durationSeconds < maximumImportedDurationSeconds {
		container.MediaDurationMS = int64(durationSeconds * 1000)
	}
	return container
}

// maximumImportedDurationSeconds rejects lengths no meeting has, which a
// damaged container can report.
// containerReadLongest bounds copying an imported original and reading its container.
const containerReadLongest = 10 * time.Minute

const maximumImportedDurationSeconds = 7 * 24 * 60 * 60

// recordImportedContainer gives an imported original what its container
// says and the recording lacks: the occurrence date when none is set, and the
// playing length when none was reported. It never replaces either, and any
// failure leaves the recording as it was.
func (self *Store) recordImportedContainer(ctx context.Context, userId, mediaId string) {
	var media *models.RecordingMedia
	var source *models.Recording
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		if media, err = transaction.GetRecordingMedia(userId, mediaId); err != nil {
			return err
		}
		source, err = transaction.GetRecording(userId, media.RecordingID)
		return err
	}); err != nil || source.RecordingOrigin == models.RecordingOriginCapture || (source.RecordedAt != nil && media.MediaDurationMS > 0) {
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
	container := probeImportedContainer(ctx, file)
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if container.MediaDurationMS > 0 && media.MediaDurationMS == 0 {
			if _, err := transaction.SetRecordingMediaDurationIfUnknown(userId, mediaId, container.MediaDurationMS); err != nil {
				return err
			}
		}
		if container.HasRecordedAt && source.RecordedAt == nil {
			if _, err := transaction.SetRecordingRecordedAtIfUnset(userId, source.ID, container.RecordedAt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot record what the container of recording %q says: %s", source.ID, err)
	}
}

// FillImportedMediaDurations reads the length of imported originals that
// arrived before lengths were read at import. Each original is tried once per
// process, so one that cannot be read is not copied again on every pass.
func (self *Store) FillImportedMediaDurations(ctx context.Context, pageSize int) error {
	self.maintenanceMutex.Lock()
	defer self.maintenanceMutex.Unlock()
	var references []db.ImportedMediaReference
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		references, err = transaction.ListImportedMediaWithoutDuration(self.lastImportedMediaId, pageSize)
		return err
	}); err != nil {
		return err
	}
	for _, reference := range references {
		self.lastImportedMediaId = reference.RecordingMediaID
		self.recordImportedContainer(ctx, reference.UserID, reference.RecordingMediaID)
	}
	return nil
}
