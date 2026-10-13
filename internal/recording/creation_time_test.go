package recording_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

func TestParseCreationTimeAcceptsOnlyPlausibleDates(test *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for name, testCase := range map[string]struct {
		tags map[string]string
		want string
	}{
		"container tag":            {map[string]string{"creation_time": "2024-05-06T07:08:09.000000Z"}, "2024-05-06T07:08:09Z"},
		"upper case tag":           {map[string]string{"CREATION_TIME": "2024-05-06T07:08:09Z"}, "2024-05-06T07:08:09Z"},
		"apple tag keeps offset":   {map[string]string{"com.apple.quicktime.creationdate": "2024-05-06T16:08:09+0900", "creation_time": "2024-05-06T07:08:09Z"}, "2024-05-06T07:08:09Z"},
		"apple tag is preferred":   {map[string]string{"com.apple.quicktime.creationdate": "2024-05-07T01:00:00+0100", "creation_time": "2024-05-01T00:00:00Z"}, "2024-05-07T00:00:00Z"},
		"before 2000":              {map[string]string{"creation_time": "1999-12-31T23:59:59Z"}, ""},
		"clock zero":               {map[string]string{"creation_time": "1970-01-01T00:00:00Z"}, ""},
		"in the future":            {map[string]string{"creation_time": "2027-01-01T00:00:00Z"}, ""},
		"not a date":               {map[string]string{"creation_time": "yesterday"}, ""},
		"unplausible falls back":   {map[string]string{"com.apple.quicktime.creationdate": "1904-01-01T00:00:00+0000", "creation_time": "2024-05-06T07:08:09Z"}, "2024-05-06T07:08:09Z"},
		"no tags":                  {nil, ""},
		"empty value":              {map[string]string{"creation_time": " "}, ""},
		"space separated, no zone": {map[string]string{"creation_time": "2024-05-06 07:08:09"}, "2024-05-06T07:08:09Z"},
	} {
		got, isFound := recording.ParseCreationTime(testCase.tags, now)
		if testCase.want == "" {
			if isFound {
				test.Errorf("%s: found %s, want none", name, got)
			}
			continue
		}
		if !isFound || got.Format(time.RFC3339) != testCase.want {
			test.Errorf("%s: got %s %v, want %s", name, got, isFound, testCase.want)
		}
	}
}

// generatedAudio makes a small m4a whose container carries the given creation time tag.
func generatedAudio(test *testing.T, creationTime string) []byte {
	test.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		test.Skip("ffmpeg is not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		test.Skip("ffprobe is not installed")
	}
	path := filepath.Join(test.TempDir(), "fixture.m4a")
	arguments := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1"}
	if creationTime != "" {
		arguments = append(arguments, "-metadata", "creation_time="+creationTime)
	}
	arguments = append(arguments, "-y", path)
	if output, err := exec.Command("ffmpeg", arguments...).CombinedOutput(); err != nil {
		test.Skipf("ffmpeg cannot make the fixture: %s %s", err, output)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		test.Fatal(err)
	}
	return content
}

// importAndComplete imports content as a recording, optionally with an occurrence date already set,
// verifies the original and returns the stored recording.
func importAndComplete(test *testing.T, origin models.RecordingOrigin, content []byte, presetRecordedAt *time.Time) *models.Recording {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = files.Close() })
	store := recording.New(database, files.(storage.StreamFiles))
	userId := dbtest.CreateUser(test, database, "creation-owner")
	var source *models.Recording
	var media *models.RecordingMedia
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if source, err = transaction.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "creation-source", RecordedAt: presetRecordedAt, RecordingOrigin: origin}); err != nil {
			test.Fatal(err)
		}
		if media, err = transaction.CreateRecordingMedia(userId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "creation-media", OriginalFilename: "voice.m4a", OriginalContentType: "audio/mp4", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestBytes(content)}, 1<<30, 2<<30); err != nil {
			test.Fatal(err)
		}
	})
	if _, err := store.PutPart(test.Context(), userId, media.ID, 0, digestBytes(content), content); err != nil {
		test.Fatal(err)
	}
	if _, _, err := store.Complete(test.Context(), userId, media.ID); err != nil {
		test.Fatal(err)
	}
	var stored *models.Recording
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if stored, err = transaction.GetRecording(userId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	return stored
}

func TestImportedOriginalGetsItsContainerCreationTime(test *testing.T) {
	stored := importAndComplete(test, models.RecordingOriginAudioImport, generatedAudio(test, "2024-05-06T07:08:09Z"), nil)
	if stored.RecordedAt == nil || stored.RecordedAt.Format(time.RFC3339) != "2024-05-06T07:08:09Z" {
		test.Fatalf("the container's creation time was not used: %v", stored.RecordedAt)
	}
}

func TestContainerCreationTimeNeverReplacesAnEarlierDateOrDescribesACapture(test *testing.T) {
	content := generatedAudio(test, "2024-05-06T07:08:09Z")
	preset := time.Date(2031, 1, 2, 3, 4, 0, 0, time.UTC)
	if stored := importAndComplete(test, models.RecordingOriginAudioImport, content, &preset); stored.RecordedAt == nil || !stored.RecordedAt.Equal(preset) {
		test.Fatalf("a date somebody set was replaced: %v", stored.RecordedAt)
	}
	if stored := importAndComplete(test, models.RecordingOriginCapture, content, nil); stored.RecordedAt != nil {
		test.Fatalf("a capture got a date from its container: %v", stored.RecordedAt)
	}
}

func TestImplausibleOrMissingContainerTimeLeavesTheDateUnknown(test *testing.T) {
	if stored := importAndComplete(test, models.RecordingOriginAudioImport, generatedAudio(test, "1970-01-01T00:00:00Z"), nil); stored.RecordedAt != nil {
		test.Fatalf("a clock-zero tag became a date: %v", stored.RecordedAt)
	}
	if stored := importAndComplete(test, models.RecordingOriginAudioImport, generatedAudio(test, ""), nil); stored.RecordedAt != nil {
		test.Fatalf("an untagged file got a date: %v", stored.RecordedAt)
	}
}
