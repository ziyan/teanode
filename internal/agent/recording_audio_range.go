package agent

import (
	"context"
	"io"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// readRecordingPCM reads lengthBytes of a piece's derived audio from
// offsetBytes in its file. A piece's length is rounded to the millisecond and
// can say a millisecond more than its file holds, and the object store
// refuses a range past the end of a file: what lies past the end is left
// silent rather than asked for.
func readRecordingPCM(ctx context.Context, files storage.StreamFiles, blob *models.RecordingBlob, offsetBytes, lengthBytes int64) ([]byte, error) {
	samples := make([]byte, lengthBytes)
	availableBytes := min(lengthBytes, blob.BlobSizeBytes-offsetBytes)
	if availableBytes <= 0 {
		return samples, nil
	}
	stream, err := files.OpenFileRange(ctx, blob.ID, offsetBytes, availableBytes)
	if err != nil {
		return nil, err
	}
	_, readError := io.ReadFull(stream, samples[:availableBytes])
	closeError := stream.Close()
	if readError != nil {
		return nil, readError
	}
	if closeError != nil {
		return nil, closeError
	}
	return samples, nil
}
