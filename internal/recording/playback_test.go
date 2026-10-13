package recording_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

func newPlaybackFixture(test *testing.T) *mediaFixture {
	test.Helper()
	if _, err := recording.NewAudioDecoder(test.Context()); err != nil {
		if os.Getenv("TEANODE_RECORDING_DECODER_REQUIRED") == "1" {
			test.Fatal(err)
		}
		test.Skip("FFmpeg with descriptor support is not installed")
	}
	command := exec.CommandContext(test.Context(), "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=2.5", "-ac", "1", "-c:a", "pcm_s16le", "-f", "caf", "pipe:1")
	content, err := command.Output()
	if err != nil {
		test.Fatal("synthetic CAF could not be generated", err)
	}
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = files.Close() })
	fixture := &mediaFixture{database: database, files: files, store: recording.New(database, files.(storage.StreamFiles)), content: content}
	fixture.userId = dbtest.CreateUser(test, database, "playback-owner")
	fixture.otherUserId = dbtest.CreateUser(test, database, "playback-other")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		fixture.source, err = transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "local-capture", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
		fixture.media, err = transaction.CreateRecordingMedia(fixture.userId, &models.RecordingMedia{RecordingID: fixture.source.ID, ClientMediaID: "local-media", OriginalFilename: "capture.caf", OriginalContentType: "audio/x-caf", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestBytes(content), TimelineStartMS: 7000, MediaDurationMS: 2500}, 1<<30, 2<<30)
		if err != nil {
			test.Fatal(err)
		}
	})
	fixture.upload(test)
	if _, _, err := fixture.store.Complete(test.Context(), fixture.userId, fixture.media.ID); err != nil {
		test.Fatal(err)
	}
	return fixture
}

func TestAudioPlaybackCAFOriginalPreservedAndPrivateRange(test *testing.T) {
	fixture := newPlaybackFixture(test)
	if ready, err := fixture.store.ReadPlayback(test.Context(), fixture.userId, fixture.media.ID); err != nil || ready != nil {
		test.Fatal("metadata read prepared media", ready, err)
	}
	if _, err := fixture.store.PreparePlayback(test.Context(), fixture.otherUserId, fixture.media.ID, 2<<30); !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign account prepared preview", err)
	}
	ready, err := fixture.store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 2<<30)
	if err != nil {
		test.Fatal(err)
	}
	if ready.PlaybackContentType != "audio/wav" || ready.PlaybackDurationMS != 2500 || ready.TimelineStartMS != 7000 || ready.PlaybackSizeBytes != 80044 || ready.RecordingMediaID != fixture.media.ID {
		test.Fatal("CAF playback lost timeline or size", ready)
	}
	var preview bytes.Buffer
	if err := fixture.store.CopyPlaybackRange(test.Context(), fixture.userId, fixture.media.ID, &preview, 0, ready.PlaybackSizeBytes); err != nil {
		test.Fatal(err)
	}
	if string(preview.Bytes()[:4]) != "RIFF" || binary.LittleEndian.Uint32(preview.Bytes()[40:44]) != 80000 || digestBytes(preview.Bytes()) != ready.PlaybackSHA256 {
		test.Fatal("preview header or digest mismatch")
	}
	var original bytes.Buffer
	if err := fixture.store.CopyRange(test.Context(), fixture.userId, fixture.media.ID, &original, 0, fixture.media.OriginalSizeBytes); err != nil || !bytes.Equal(original.Bytes(), fixture.content) {
		test.Fatal("preview changed recoverable original", err)
	}

	var portion bytes.Buffer
	if err := fixture.store.CopyPlaybackRange(test.Context(), fixture.userId, fixture.media.ID, &portion, 43, 12); err != nil || !bytes.Equal(portion.Bytes(), preview.Bytes()[43:55]) {
		test.Fatal("preview seek returned wrong bytes", err)
	}
	if err := fixture.store.CopyPlaybackRange(test.Context(), fixture.otherUserId, fixture.media.ID, io.Discard, 0, 44); !errors.Is(err, db.ErrNotFound) {
		test.Fatal("foreign account played preview", err)
	}
	if err := fixture.store.CopyPlaybackRange(test.Context(), fixture.userId, fixture.media.ID, io.Discard, ready.PlaybackSizeBytes-1, 2); !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatal("range exceeded verified derivative", err)
	}
	retry, err := fixture.store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 1)
	if err != nil || retry.PlaybackSHA256 != ready.PlaybackSHA256 {
		test.Fatal("ready playback duplicated quota", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(fixture.userId, fixture.source.ID); err != nil {
			test.Fatal(err)
		}
	})
	if _, err := fixture.store.ReadPlayback(test.Context(), fixture.userId, fixture.media.ID); !errors.Is(err, db.ErrRecordingDeleted) {
		test.Fatal("deleted recording exposed preview", err)
	}
	if err := fixture.store.Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
	if _, err := fixture.files.GetFile(test.Context(), db.RecordingPreviewFileID(fixture.media.ID)); !errors.Is(err, storage.ErrNotFound) {
		test.Fatal("deleted preview remained in storage", err)
	}
}

func TestAudioPlaybackFailedWriteReservationRetryAndLostStorage(test *testing.T) {
	fixture := newPlaybackFixture(test)
	files := &failingFiles{StreamFiles: fixture.files.(storage.StreamFiles)}
	files.shouldFailWrite.Store(true)
	store := recording.New(fixture.database, files)
	if _, err := store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 2<<30); err == nil {
		test.Fatal("failed storage published playback")
	}
	var pending *models.RecordingBlob
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		var err error
		pending, err = transaction.ReadRecordingPreview(fixture.userId, fixture.media.ID)
		if err != nil || pending.IsStoredVerified {
			test.Fatal("failed write lost cleanup reservation", err)
		}
	})
	if ready, err := store.ReadPlayback(test.Context(), fixture.userId, fixture.media.ID); err != nil || ready != nil {
		test.Fatal("unverified reservation became ready", err)
	}
	files.shouldFailWrite.Store(false)
	ready, err := store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 1)
	if err != nil || ready.PlaybackSHA256 != pending.BlobSHA256 {
		test.Fatal("reserved preview could not repair under reduced quota", err)
	}
	if err := files.DeleteFileEverywhere(test.Context(), pending.ID); err != nil {
		test.Fatal(err)
	}
	if _, err := store.ReadPlayback(test.Context(), fixture.userId, fixture.media.ID); !errors.Is(err, storage.ErrNotFound) {
		test.Fatal("missing storage advertised ready preview", err)
	}
	repaired, err := store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 1)
	if err != nil || repaired.PlaybackSHA256 != ready.PlaybackSHA256 {
		test.Fatal("lost derived bytes could not repair", err)
	}
	if err := fixture.files.PutFile(test.Context(), db.RecordingPartFileID(fixture.media.ID, 0), bytes.Repeat([]byte("x"), len(fixture.content))); err != nil {
		test.Fatal(err)
	}
	if err := files.DeleteFileEverywhere(test.Context(), pending.ID); err != nil {
		test.Fatal(err)
	}
	if _, err := store.PreparePlayback(test.Context(), fixture.userId, fixture.media.ID, 2<<30); err == nil {
		test.Fatal("corrupt original became a new playback derivative")
	}
}
