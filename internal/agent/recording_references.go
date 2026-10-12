package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

func (self *Agent) recordingSpeakerReferences(ctx context.Context, userId string, capabilities llm.TranscriptionCapabilities, files storage.StreamFiles) ([]llm.SpeakerReference, []string, error) {
	if !capabilities.HasSpeakerReferences || capabilities.MaximumSpeakerReferenceCount <= 0 {
		return nil, nil, nil
	}
	referenceLimit := min(4, capabilities.MaximumSpeakerReferenceCount)
	var references []*models.RecordingVoiceReference
	var identities []*models.RecordingSpeakerIdentity
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		references, err = transaction.ListUsableRecordingVoiceReferences(userId, 24)
		if err != nil {
			return err
		}
		identities, err = transaction.ListRecordingSpeakerIdentities(userId)
		return err
	}); err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	for _, identity := range identities {
		names[identity.ID] = identity.ConfirmedName
	}
	selectedNames := map[string]bool{}
	var speakerReferences []llm.SpeakerReference
	var referenceBlobIds []string
	for _, reference := range references {
		if len(speakerReferences) >= referenceLimit {
			break
		}
		speakerName := names[reference.SpeakerIdentityID]
		durationMS := reference.PassageEndMS - reference.PassageStartMS
		if speakerName == "" || selectedNames[speakerName] || durationMS < 2000 || durationMS > 10000 {
			continue
		}
		var blob *models.RecordingBlob
		err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			blob, err = transaction.GetRecordingBlob(userId, reference.RecordingBlobID)
			return err
		})
		if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if !blob.IsStoredVerified || blob.RecordingBlobPurpose != models.RecordingBlobSpeakerReference || blob.BlobSizeBytes < 44 || blob.BlobSizeBytes > 320044 {
			continue
		}
		stream, err := files.OpenFileRange(ctx, blob.ID, 0, blob.BlobSizeBytes)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		content, readError := io.ReadAll(io.LimitReader(stream, blob.BlobSizeBytes+1))
		_ = stream.Close()
		if readError != nil {
			return nil, nil, readError
		}
		digest := sha256.Sum256(content)
		if int64(len(content)) != blob.BlobSizeBytes || hex.EncodeToString(digest[:]) != blob.BlobSHA256 {
			return nil, nil, fmt.Errorf("recording voice reference failed its integrity check")
		}
		selectedNames[speakerName] = true
		speakerReferences = append(speakerReferences, llm.SpeakerReference{SpeakerName: speakerName, MediaContentType: "audio/wav", MediaBytes: content, DurationMS: durationMS})
		referenceBlobIds = append(referenceBlobIds, blob.ID)
	}
	return speakerReferences, referenceBlobIds, nil
}
