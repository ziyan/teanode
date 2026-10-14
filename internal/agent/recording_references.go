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
	var recentSpeakerNames map[string]bool
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
		if err != nil {
			return err
		}
		recentSpeakerNames, err = recentRecordingSpeakerNames(transaction, userId, recordingId)
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
		// A label merged into another is that voice, which is sent under the other's sample.
		if temporarySpeaker.RecordingBlobID == "" || temporarySpeaker.MergedIntoTemporarySpeakerID != "" {
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
	// Who spoke in the latest pieces is the likeliest to speak in this one: a
	// meeting of more people than there are samples otherwise gives everyone
	// past the first few a new label in every piece. They go first, each group
	// keeping the order above.
	sort.SliceStable(candidates, func(left, right int) bool {
		return recentSpeakerNames[candidates[left].SpeakerName] && !recentSpeakerNames[candidates[right].SpeakerName]
	})
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
		content, isUsable, err := self.readRecordingSpeakerSample(ctx, userId, candidate.BlobID, files)
		if err != nil {
			return nil, nil, err
		}
		if !isUsable {
			continue
		}
		selectedNames[candidate.SpeakerName] = true
		speakerReferences = append(speakerReferences, llm.SpeakerReference{SpeakerName: candidate.SpeakerName, MediaContentType: "audio/wav", MediaBytes: content, DurationMS: candidate.DurationMS})
		referenceBlobIds = append(referenceBlobIds, candidate.BlobID)
	}
	return speakerReferences, referenceBlobIds, nil
}

// readRecordingSpeakerSample reads the bytes of a voice sample after checking
// that it is a verified speaker reference of a size a provider takes. A sample
// that is missing, deleted or not yet written is not usable, which is not an
// error: it is left out. One whose bytes fail the integrity check is an error.
func (self *Agent) readRecordingSpeakerSample(ctx context.Context, userId, blobId string, files storage.StreamFiles) ([]byte, bool, error) {
	var blob *models.RecordingBlob
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		blob, err = transaction.GetRecordingBlob(userId, blobId)
		return err
	})
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !blob.IsStoredVerified || blob.RecordingBlobPurpose != models.RecordingBlobSpeakerReference || blob.BlobSizeBytes < 44 || blob.BlobSizeBytes > 320044 {
		return nil, false, nil
	}
	stream, err := files.OpenFileRange(ctx, blob.ID, 0, blob.BlobSizeBytes)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	content, readError := io.ReadAll(io.LimitReader(stream, blob.BlobSizeBytes+1))
	_ = stream.Close()
	if readError != nil {
		return nil, false, readError
	}
	digest := sha256.Sum256(content)
	if int64(len(content)) != blob.BlobSizeBytes || hex.EncodeToString(digest[:]) != blob.BlobSHA256 {
		return nil, false, fmt.Errorf("recording voice reference failed its integrity check")
	}
	return content, true, nil
}

func spokenMS(temporarySpeakers []*models.RecordingTemporarySpeaker, blobId string) int64 {
	for _, temporarySpeaker := range temporarySpeakers {
		if temporarySpeaker.RecordingBlobID == blobId {
			return temporarySpeaker.SpokenMS
		}
	}
	return 0
}

// recordingRecentPieceCount is how many of a recording's latest finished
// pieces say who has just been speaking.
const recordingRecentPieceCount = 2

// recentRecordingSpeakerNames are the names the speakers of a recording's
// latest finished pieces on its timeline go by, the confirmed name or the
// temporary label, which is the name their sample is sent under. Latest is by
// where a piece sits on the timeline, not when it was transcribed: pieces are
// transcribed side by side, and an old piece may be transcribed again.
func recentRecordingSpeakerNames(transaction db.Transaction, userId, recordingId string) (map[string]bool, error) {
	mediaFiles, err := transaction.ListRecordingMedia(userId, recordingId)
	if err != nil {
		return nil, err
	}
	runs, err := transaction.ListRecordingTranscriptRuns(userId, recordingId)
	if err != nil {
		return nil, err
	}
	// The latest finished run of each piece; runs come newest first.
	latestRunByMediaId := map[string]*models.RecordingTranscriptRun{}
	for _, transcriptRun := range runs {
		if transcriptRun.RecordingProcessingState == models.RecordingProcessingComplete && latestRunByMediaId[transcriptRun.RecordingMediaID] == nil {
			latestRunByMediaId[transcriptRun.RecordingMediaID] = transcriptRun
		}
	}
	sort.SliceStable(mediaFiles, func(left, right int) bool {
		return mediaFiles[left].TimelineStartMS > mediaFiles[right].TimelineStartMS
	})
	names := map[string]bool{}
	pieceCount := 0
	for _, media := range mediaFiles {
		transcriptRun := latestRunByMediaId[media.ID]
		if transcriptRun == nil {
			continue
		}
		if pieceCount == recordingRecentPieceCount {
			break
		}
		pieceCount++
		segments, err := transaction.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
		if err != nil {
			return nil, err
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(userId, segments); err != nil {
			return nil, err
		}
		for _, segment := range segments {
			switch {
			case segment.EffectiveSpeakerIdentityID != "" && segment.EffectiveSpeakerName != "":
				names[segment.EffectiveSpeakerName] = true
			case segment.TemporarySpeakerLabel != "":
				names[segment.TemporarySpeakerLabel] = true
			}
		}
	}
	return names, nil
}
