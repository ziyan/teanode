package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// recordingReferenceCandidate is a voice sample that may be sent with a
// transcription, under the name the provider should answer with.
type recordingReferenceCandidate struct {
	SpeakerName string
	BlobID      string
	DurationMS  int64
}

// recordingSpeakerReferences picks the voice samples to send with a piece of a
// recording. The provider takes at most four, so they go in this order: people
// already named in this recording, then the person themselves, then the
// recording's speakers nobody has named yet (who has spoken most first), then
// the account's other known voices, newest first. One sample per name.
func (self *Agent) recordingSpeakerReferences(ctx context.Context, userId, recordingId string, capabilities llm.TranscriptionCapabilities, files storage.StreamFiles) ([]llm.SpeakerReference, []string, error) {
	if !capabilities.HasSpeakerReferences || capabilities.MaximumSpeakerReferenceCount <= 0 {
		return nil, nil, nil
	}
	referenceLimit := min(4, capabilities.MaximumSpeakerReferenceCount)
	var references []*models.RecordingVoiceReference
	var identities []*models.RecordingSpeakerIdentity
	var corrections []*models.RecordingSpeakerCorrection
	var temporarySpeakers []*models.RecordingTemporarySpeaker
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		references, err = transaction.ListUsableRecordingVoiceReferences(userId, 24)
		if err != nil {
			return err
		}
		identities, err = transaction.ListRecordingSpeakerIdentities(userId)
		if err != nil {
			return err
		}
		corrections, err = transaction.ListRecordingSpeakerCorrections(userId, recordingId)
		if err != nil {
			return err
		}
		temporarySpeakers, err = transaction.ListRecordingTemporarySpeakers(userId, recordingId)
		return err
	}); err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	ownerIdentityId := ""
	for _, identity := range identities {
		names[identity.ID] = identity.ConfirmedName
		if identity.IsAccountOwner {
			ownerIdentityId = identity.ID
		}
	}
	namedInRecording := map[string]bool{}
	for _, correction := range corrections {
		if !correction.IsRemoved {
			namedInRecording[correction.SpeakerIdentityID] = true
		}
	}
	for _, temporarySpeaker := range temporarySpeakers {
		if temporarySpeaker.SpeakerIdentityID != "" {
			namedInRecording[temporarySpeaker.SpeakerIdentityID] = true
		}
	}
	// Each group keeps its own order; the groups are concatenated in priority order.
	var namedCandidates, ownerCandidates, temporaryCandidates, otherCandidates []recordingReferenceCandidate
	for _, temporarySpeaker := range temporarySpeakers {
		if temporarySpeaker.RecordingBlobID == "" {
			continue
		}
		candidate := recordingReferenceCandidate{SpeakerName: temporarySpeaker.SpeakerLabel, BlobID: temporarySpeaker.RecordingBlobID, DurationMS: temporarySpeaker.PassageEndMS - temporarySpeaker.PassageStartMS}
		if temporarySpeaker.SpeakerIdentityID != "" {
			candidate.SpeakerName = names[temporarySpeaker.SpeakerIdentityID]
			namedCandidates = append(namedCandidates, candidate)
		} else {
			temporaryCandidates = append(temporaryCandidates, candidate)
		}
	}
	// The provider takes few samples: those who have spoken most are the
	// likeliest to speak again, and the ones a wrong label costs most.
	sort.SliceStable(temporaryCandidates, func(left, right int) bool {
		return spokenMS(temporarySpeakers, temporaryCandidates[left].BlobID) > spokenMS(temporarySpeakers, temporaryCandidates[right].BlobID)
	})
	for _, reference := range references {
		candidate := recordingReferenceCandidate{SpeakerName: names[reference.SpeakerIdentityID], BlobID: reference.RecordingBlobID, DurationMS: reference.PassageEndMS - reference.PassageStartMS}
		switch {
		case namedInRecording[reference.SpeakerIdentityID]:
			namedCandidates = append(namedCandidates, candidate)
		case reference.SpeakerIdentityID == ownerIdentityId:
			ownerCandidates = append(ownerCandidates, candidate)
		default:
			otherCandidates = append(otherCandidates, candidate)
		}
	}
	// The sample of a named speaker that is the person comes with the other named ones.
	candidates := slices.Concat(namedCandidates, ownerCandidates, temporaryCandidates, otherCandidates)
	selectedNames := map[string]bool{}
	var speakerReferences []llm.SpeakerReference
	var referenceBlobIds []string
	for _, candidate := range candidates {
		if len(speakerReferences) >= referenceLimit {
			break
		}
		if candidate.SpeakerName == "" || selectedNames[candidate.SpeakerName] || candidate.DurationMS < 2000 || candidate.DurationMS > 10000 {
			continue
		}
		var blob *models.RecordingBlob
		err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			blob, err = transaction.GetRecordingBlob(userId, candidate.BlobID)
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
		selectedNames[candidate.SpeakerName] = true
		speakerReferences = append(speakerReferences, llm.SpeakerReference{SpeakerName: candidate.SpeakerName, MediaContentType: "audio/wav", MediaBytes: content, DurationMS: candidate.DurationMS})
		referenceBlobIds = append(referenceBlobIds, blob.ID)
	}
	return speakerReferences, referenceBlobIds, nil
}

func spokenMS(temporarySpeakers []*models.RecordingTemporarySpeaker, blobId string) int64 {
	for _, temporarySpeaker := range temporarySpeakers {
		if temporarySpeaker.RecordingBlobID == blobId {
			return temporarySpeaker.SpokenMS
		}
	}
	return 0
}
