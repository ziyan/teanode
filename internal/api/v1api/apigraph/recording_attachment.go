package apigraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

// RecordingAttachmentMutation copies explicitly delivered account-owned evidence.
type RecordingAttachmentMutation interface {
	ImportRecordingAttachment(context.Context, ImportRecordingAttachmentArguments) (*RecordingView, error)
}

// ImportRecordingAttachmentArguments retains retry identity without a filesystem path.
type ImportRecordingAttachmentArguments struct {
	RecordingMediaID  string `json:"recordingMediaId" graphapi:"nullable"`
	AgentAttachmentID string `json:"agentAttachmentId"`
	ClientRecordingID string `json:"clientRecordingId"`
	RecordingTitle    string `json:"recordingTitle"`
}

func (self *graph) ImportRecordingAttachment(ctx context.Context, arguments ImportRecordingAttachmentArguments) (*RecordingView, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	attachment, err := transaction.GetAgentAttachment(arguments.AgentAttachmentID)
	if err != nil {
		return nil, translateError(err)
	}
	if attachment == nil {
		return nil, translateError(db.ErrNotFound)
	}
	assistant, err := transaction.GetAgent(attachment.AgentID)
	if err != nil {
		return nil, translateError(err)
	}
	if assistant == nil || assistant.UserID != owner.ID {
		return nil, translateError(db.ErrNotFound)
	}
	limits, err := self.recordingLimits()
	if err != nil {
		return nil, err
	}
	if attachment.Size <= 0 || attachment.Size > limits.MaximumMediaBytes {
		return nil, translateError(db.ErrTooMuchAsked)
	}
	origin := models.RecordingOriginAudioImport
	if strings.HasPrefix(attachment.ContentType, "video/") {
		origin = models.RecordingOriginVideoImport
	} else if !strings.HasPrefix(attachment.ContentType, "audio/") {
		return nil, fmt.Errorf("attach an audio or video file")
	}
	files, isStreaming := self.storage.(storage.StreamFiles)
	if !isStreaming {
		return nil, fmt.Errorf("recording storage is unavailable")
	}
	stream, err := files.OpenFileRange(ctx, attachment.ID, 0, attachment.Size)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	copiedBytes, copyError := io.Copy(digest, io.LimitReader(stream.ReadCloser, attachment.Size+1))
	_ = stream.Close()
	if copyError != nil {
		return nil, copyError
	}
	if copiedBytes != attachment.Size {
		return nil, fmt.Errorf("attachment size does not match its stored bytes")
	}
	var source *models.Recording
	var media *models.RecordingMedia
	originalSha256 := hex.EncodeToString(digest.Sum(nil))
	if arguments.RecordingMediaID != "" {
		media, err = transaction.GetRecordingMedia(owner.ID, arguments.RecordingMediaID)
		if err != nil {
			return nil, translateError(err)
		}
		if media.OriginalSizeBytes != attachment.Size || media.OriginalSHA256 != originalSha256 {
			return nil, translateError(db.ErrAlreadyExists)
		}
		source, err = transaction.GetRecording(owner.ID, media.RecordingID)
		if err != nil {
			return nil, translateError(err)
		}
	} else {
		source, err = transaction.CreateRecording(&models.Recording{UserID: owner.ID, ClientRecordingID: arguments.ClientRecordingID, RecordingTitle: arguments.RecordingTitle, RecordingOrigin: origin})
		if err != nil {
			return nil, translateError(err)
		}
		media, err = transaction.CreateRecordingMedia(owner.ID, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "attachment-" + attachment.ID, OriginalFilename: attachment.Name, OriginalContentType: attachment.ContentType, OriginalSizeBytes: attachment.Size, OriginalSHA256: originalSha256}, limits.MaximumMediaBytes, limits.MaximumAccountBytes)
		if err != nil {
			return nil, translateError(err)
		}
	}

	// Publish the recoverable manifest before writing bytes. A later failure leaves
	// a resumable original, rather than blobs whose transaction discarded their IDs.
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	store := recording.New(self.database, files)
	for partIndex := 0; partIndex < media.PartCount; partIndex++ {
		partSizeBytes := media.PartSizeBytes(partIndex)
		partStream, err := files.OpenFileRange(ctx, attachment.ID, int64(partIndex)*models.RecordingUploadPartBytes, partSizeBytes)
		if err != nil {
			return nil, err
		}
		partBytes, readError := io.ReadAll(io.LimitReader(partStream.ReadCloser, partSizeBytes+1))
		_ = partStream.Close()
		if readError != nil {
			return nil, readError
		}
		if int64(len(partBytes)) != partSizeBytes {
			return nil, fmt.Errorf("attachment part is incomplete")
		}
		partDigest := sha256.Sum256(partBytes)
		if _, err := store.PutPart(ctx, owner.ID, media.ID, partIndex, hex.EncodeToString(partDigest[:]), partBytes); err != nil {
			return nil, translateError(err)
		}
	}
	if _, _, err := store.Complete(ctx, owner.ID, media.ID); err != nil {
		return nil, translateError(err)
	}
	return self.ReadRecording(ctx, RecordingArguments{RecordingID: source.ID})
}
