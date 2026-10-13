package recording

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// Bound local decoding across HTTP stores as well as concurrent requests.
var playbackPreparationSlots = make(chan struct{}, 2)

const maximumPlaybackBytes int64 = 2 << 30
const maximumPlaybackDurationMS int64 = 7 * 24 * 60 * 60 * 1000

func playbackView(media *models.RecordingMedia, blob *models.RecordingBlob) *models.RecordingPlayback {
	return &models.RecordingPlayback{
		RecordingMediaID:    media.ID,
		PlaybackContentType: "audio/wav", PlaybackSizeBytes: blob.BlobSizeBytes,
		PlaybackSHA256:     blob.BlobSHA256,
		PlaybackDurationMS: ((blob.BlobSizeBytes-44)/audioSampleBytes*1000 + AudioSampleRateHz - 1) / AudioSampleRateHz,
		TimelineStartMS:    media.TimelineStartMS,
	}
}

// ReadPlayback never decodes media or invokes a provider. A missing or unfinished
// derivative is distinct from a missing, foreign, or deleted original.
func (self *Store) ReadPlayback(ctx context.Context, userId, mediaId string) (*models.RecordingPlayback, error) {
	var media *models.RecordingMedia
	var blob *models.RecordingBlob
	err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(userId, mediaId)
		if err != nil {
			return err
		}
		blob, err = transaction.ReadRecordingPreview(userId, mediaId)
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if blob == nil || !blob.IsStoredVerified {
		return nil, nil
	}
	if blob.BlobSizeBytes <= 44 || (blob.BlobSizeBytes-44)%audioSampleBytes != 0 {
		return nil, db.ErrInvalidArguments
	}
	stream, err := self.files.OpenFileRange(ctx, blob.ID, 0, 1)
	if err != nil {
		return nil, err
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	return playbackView(media, blob), nil
}

// PreparePlayback preserves the verified original and publishes a recoverable,
// account-owned WAV through the existing quota and deletion lifecycle.
func (self *Store) PreparePlayback(ctx context.Context, userId, mediaId string, maximumAccountBytes int64) (*models.RecordingPlayback, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	ready, err := self.ReadPlayback(ctx, userId, mediaId)
	if err == nil && ready != nil {
		return ready, nil
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	select {
	case playbackPreparationSlots <- struct{}{}:
		defer func() { <-playbackPreparationSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("%w: recording playback preparation is busy; retry shortly", db.ErrTooMuchAsked)
	}
	var media *models.RecordingMedia
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		var err error
		media, err = transaction.GetRecordingMedia(userId, mediaId)
		return err
	}); err != nil {
		return nil, err
	}
	if media.RecordingUploadState != models.RecordingUploadComplete || media.VerifiedAt == nil {
		return nil, db.ErrInvalidArguments
	}
	if media.OriginalSizeBytes > maximumPlaybackBytes {
		return nil, fmt.Errorf("%w: recording original exceeds the playback preparation limit", db.ErrTooMuchAsked)
	}
	original, err := PrivateMediaFile()
	if err != nil {
		return nil, err
	}
	defer func() { _ = original.Close() }()
	originalDigest := sha256.New()
	if err := self.CopyRange(ctx, userId, mediaId, io.MultiWriter(original, originalDigest), 0, media.OriginalSizeBytes); err != nil {
		return nil, err
	}
	if hex.EncodeToString(originalDigest.Sum(nil)) != media.OriginalSHA256 {
		return nil, fmt.Errorf("recording: stored original failed integrity verification")
	}
	decoder, err := NewAudioDecoder(ctx)
	if err != nil {
		return nil, err
	}
	preview, err := PrivateMediaFile()
	if err != nil {
		return nil, err
	}
	defer func() { _ = preview.Close() }()
	waveHeader, err := waveFromPCM(nil)
	if err != nil {
		return nil, err
	}
	if _, err := preview.Write(waveHeader); err != nil {
		return nil, err
	}
	previewSizeBytes := int64(len(waveHeader))
	_, err = decoder.Decode(ctx, original, media.TimelineStartMS, maximumPlaybackDurationMS, maximumAudioChunkDurationMS, func(chunk *DecodedAudioChunk) error {
		pcmBytes := chunk.WaveBytes[44:]
		if int64(len(pcmBytes)) > maximumPlaybackBytes-previewSizeBytes {
			return db.ErrTooMuchAsked
		}
		writtenBytes, err := preview.Write(pcmBytes)
		previewSizeBytes += int64(writtenBytes)
		return err
	})
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(waveHeader[4:8], uint32(previewSizeBytes-8))
	binary.LittleEndian.PutUint32(waveHeader[40:44], uint32(previewSizeBytes-44))
	if _, err := preview.WriteAt(waveHeader, 0); err != nil {
		return nil, err
	}
	if _, err := preview.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	previewDigest := sha256.New()
	if _, err := io.Copy(previewDigest, preview); err != nil {
		return nil, err
	}
	blob := &models.RecordingBlob{ID: db.RecordingPreviewFileID(mediaId), RecordingID: media.RecordingID, RecordingMediaID: mediaId, RecordingBlobPurpose: models.RecordingBlobAudioPreview, BlobSizeBytes: previewSizeBytes, BlobSHA256: hex.EncodeToString(previewDigest.Sum(nil))}
	// Commit the cleanup reference before any persistent storage write.
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.ReserveRecordingPreview(userId, mediaId, blob, maximumAccountBytes)
		return err
	}); err != nil {
		return nil, err
	}
	if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if _, err := transaction.LockRecording(userId, media.RecordingID); err != nil {
			return err
		}
		reserved, err := transaction.ReadRecordingPreview(userId, mediaId)
		if err != nil {
			return err
		}
		if reserved.BlobSHA256 != blob.BlobSHA256 || reserved.BlobSizeBytes != blob.BlobSizeBytes {
			return db.ErrAlreadyExists
		}
		if _, err := preview.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := self.files.PutFileStream(ctx, blob.ID, preview, blob.BlobSizeBytes); err != nil {
			return err
		}
		return transaction.VerifyRecordingBlob(userId, blob.ID, blob.BlobSHA256)
	}); err != nil {
		return nil, err
	}
	return playbackView(media, blob), nil
}

// CopyPlaybackRange rechecks ownership between bounded reads, just like the
// original route. It cannot expose arbitrary agent files or source attachments.
func (self *Store) CopyPlaybackRange(ctx context.Context, userId, mediaId string, writer io.Writer, offsetBytes, sizeBytes int64) error {
	ready, err := self.ReadPlayback(ctx, userId, mediaId)
	if err != nil {
		return err
	}
	if ready == nil {
		return db.ErrNotFound
	}
	if offsetBytes < 0 || sizeBytes <= 0 || offsetBytes > ready.PlaybackSizeBytes || sizeBytes > ready.PlaybackSizeBytes-offsetBytes {
		return db.ErrInvalidArguments
	}
	for remainingBytes := sizeBytes; remainingBytes > 0; {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.database.TransactionContext(ctx, func(transaction db.Transaction) error {
			blob, err := transaction.ReadRecordingPreview(userId, mediaId)
			if err != nil {
				return err
			}
			if !blob.IsStoredVerified || blob.BlobSHA256 != ready.PlaybackSHA256 {
				return db.ErrInvalidArguments
			}
			return nil
		}); err != nil {
			return err
		}
		portionBytes := min(remainingBytes, models.RecordingUploadPartBytes)
		stream, err := self.files.OpenFileRange(ctx, db.RecordingPreviewFileID(mediaId), offsetBytes, portionBytes)
		if err != nil {
			return err
		}
		copiedBytes, copyError := io.Copy(writer, stream)
		closeError := stream.Close()
		if copyError != nil {
			return copyError
		}
		if closeError != nil {
			return closeError
		}
		if copiedBytes != portionBytes {
			return io.ErrUnexpectedEOF
		}
		remainingBytes -= copiedBytes
		offsetBytes += copiedBytes
	}
	return nil
}
