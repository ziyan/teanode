package agent

import (
	"bytes"
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A piece's length can say a millisecond more than its file holds; a read
// that runs to its end gets what the file has and silence after it.
func TestAReadPastTheEndOfAPieceIsSilenceNotAnError(test *testing.T) {
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = files.Close() }()
	content := bytes.Repeat([]byte{7}, 100)
	if err := files.PutFile(test.Context(), "piece-audio", content); err != nil {
		test.Fatal(err)
	}
	blob := &models.RecordingBlob{ID: "piece-audio", BlobSizeBytes: int64(len(content))}
	samples, err := readRecordingPCM(test.Context(), files.(storage.StreamFiles), blob, 80, 40)
	if err != nil {
		test.Fatal(err)
	}
	if len(samples) != 40 || !bytes.Equal(samples[:20], content[80:]) || !bytes.Equal(samples[20:], make([]byte, 20)) {
		test.Fatal("the read past the end was not the file's bytes and then silence", samples)
	}
	samples, err = readRecordingPCM(test.Context(), files.(storage.StreamFiles), blob, 120, 10)
	if err != nil || !bytes.Equal(samples, make([]byte, 10)) {
		test.Fatal("a read wholly past the end was not silence", err)
	}
}
