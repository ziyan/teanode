package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

const (
	// recordingTemporaryCurationRuns is how many of a capture's latest
	// transcripts one curation looks at. Curation runs after every piece, so
	// earlier ones were looked at already; looking at all of them would make a
	// long capture slower with every piece.
	recordingTemporaryCurationRuns = 6

	// recordingTemporaryCandidates is how many of a speaker's passages are
	// tried for a clean sample before leaving that speaker for a later piece.
	recordingTemporaryCandidates = 5
)

// curateRecordingTemporarySpeakers gives every speaker of a capture that no
// sample is being sent for a sample of its voice and a stable label, so the
// next pieces are sent the sample and answer with the same label. It looks at
// the latest transcripts of the capture and skips a speaker the provider
// recognized by one of the samples it was sent, a speaker the person named, and
// one that already has a temporary label. A speaker with no clean passage yet
// is left for a later piece; not finding one is not an error. Then each new
// label is checked once against the earlier ones it may be another label for.
func (self *Agent) curateRecordingTemporarySpeakers(ctx context.Context, userId, recordingId string, files storage.StreamFiles) error {
	var source *models.Recording
	var runs []*models.RecordingTranscriptRun
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		source, err = transaction.GetRecording(userId, recordingId)
		if err != nil || source.RecordingOrigin != models.RecordingOriginCapture {
			return err
		}
		runs, err = transaction.ListRecordingTranscriptRuns(userId, recordingId)
		return err
	}); err != nil {
		return err
	}
	if source.RecordingOrigin != models.RecordingOriginCapture {
		return nil
	}
	// Newest first; only the latest completed version of each original counts.
	var latestRuns []*models.RecordingTranscriptRun
	seenMediaIds := map[string]bool{}
	for _, transcriptRun := range runs {
		if transcriptRun.RecordingProcessingState != models.RecordingProcessingComplete || seenMediaIds[transcriptRun.RecordingMediaID] {
			continue
		}
		seenMediaIds[transcriptRun.RecordingMediaID] = true
		latestRuns = append(latestRuns, transcriptRun)
		if len(latestRuns) == recordingTemporaryCurationRuns {
			break
		}
	}
	for _, transcriptRun := range latestRuns {
		if err := self.curateRecordingTemporarySpeakersOfRun(ctx, userId, transcriptRun, files); err != nil {
			return err
		}
	}
	return self.checkRecordingTemporarySpeakerMerges(ctx, userId, recordingId, latestRuns, files)
}

func (self *Agent) curateRecordingTemporarySpeakersOfRun(ctx context.Context, userId string, transcriptRun *models.RecordingTranscriptRun, files storage.StreamFiles) error {
	var attempts []*models.RecordingTranscriptionAttempt
	var segments []*models.RecordingTranscriptSegment
	var chunks []*models.RecordingAudioChunk
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		attempts, err = transaction.ListRecordingTranscriptionAttempts(userId, transcriptRun.ID)
		if err != nil {
			return err
		}
		segments, err = transaction.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
		if err != nil {
			return err
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(userId, segments); err != nil {
			return err
		}
		chunks, err = transaction.ListRecordingAudioChunks(userId, transcriptRun.ID)
		return err
	}); err != nil {
		return err
	}
	for _, attempt := range attempts {
		if attempt.RecordingAttemptState != models.RecordingAttemptComplete {
			continue
		}
		var audioChunk *models.RecordingAudioChunk
		for _, chunk := range chunks {
			if chunk.ID == attempt.RecordingAudioChunkID {
				audioChunk = chunk
			}
		}
		if audioChunk == nil {
			continue
		}
		passagesBySpeaker := map[string][]*models.RecordingTranscriptSegment{}
		isKnownSpeaker := map[string]bool{}
		for _, segment := range segments {
			if segment.RecordingTranscriptionAttemptID != attempt.ID {
				continue
			}
			passagesBySpeaker[segment.LocalSpeakerID] = append(passagesBySpeaker[segment.LocalSpeakerID], segment)
			if segment.EffectiveSpeakerIdentityID != "" || segment.TemporarySpeakerLabel != "" || segment.HasUserConfirmedSpeaker {
				isKnownSpeaker[segment.LocalSpeakerID] = true
			}
		}
		speakerIds := make([]string, 0, len(passagesBySpeaker))
		for localSpeakerId := range passagesBySpeaker {
			speakerIds = append(speakerIds, localSpeakerId)
		}
		sort.Strings(speakerIds)
		for _, localSpeakerId := range speakerIds {
			if isKnownSpeaker[localSpeakerId] {
				continue
			}
			if err := self.createRecordingTemporarySpeaker(ctx, userId, transcriptRun, attempt, audioChunk, localSpeakerId, passagesBySpeaker[localSpeakerId], files); err != nil {
				return err
			}
		}
	}
	return nil
}

// createRecordingTemporarySpeaker cuts the first clean sample out of a
// speaker's longest passages and keeps it as a new temporary speaker. The
// reservation commits before the bytes, so a failed write leaves a reference
// for the cleanup to find, and the next curation writes the bytes again.
func (self *Agent) createRecordingTemporarySpeaker(ctx context.Context, userId string, transcriptRun *models.RecordingTranscriptRun, attempt *models.RecordingTranscriptionAttempt, audioChunk *models.RecordingAudioChunk, localSpeakerId string, passages []*models.RecordingTranscriptSegment, files storage.StreamFiles) error {
	maximumAccountBytes := self.settings.Configuration().Agent.Limits.MaxRecordingAccountBytes.Bytes()
	if maximumAccountBytes == 0 {
		maximumAccountBytes = 64 << 30
	}
	if maximumAccountBytes > math.MaxInt64 {
		return fmt.Errorf("recording account limit exceeds supported bytes")
	}
	sort.SliceStable(passages, func(left, right int) bool {
		return passages[left].SegmentEndMS-passages[left].SegmentStartMS > passages[right].SegmentEndMS-passages[right].SegmentStartMS
	})
	var audioBlob *models.RecordingBlob
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		audioBlob, err = transaction.GetRecordingBlob(userId, audioChunk.RecordingBlobID)
		return err
	}); err != nil {
		return err
	}
	if !audioBlob.IsStoredVerified {
		return nil
	}
	for candidateIndex, passage := range passages {
		if candidateIndex == recordingTemporaryCandidates {
			break
		}
		clipStartMS := passage.SegmentStartMS + 200
		clipEndMS := min(passage.SegmentEndMS-200, clipStartMS+6000)
		if passage.SegmentEndMS-passage.SegmentStartMS < 2400 || clipStartMS < audioChunk.TimelineStartMS || clipEndMS > audioChunk.TimelineStartMS+audioChunk.AudioDurationMS {
			continue
		}
		var hasOverlap bool
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			hasOverlap, err = transaction.HasRecordingSpeakerOverlap(userId, transcriptRun.ID, attempt.ID, localSpeakerId, clipStartMS-200, clipEndMS+200)
			return err
		}); err != nil {
			return err
		}
		if hasOverlap {
			continue
		}
		sampleOffsetBytes := 44 + (clipStartMS-audioChunk.TimelineStartMS)*recording.AudioSampleRateHz*2/1000
		clipSizeBytes := (clipEndMS - clipStartMS) * recording.AudioSampleRateHz * 2 / 1000
		samples, err := readRecordingPCM(ctx, files, audioBlob, sampleOffsetBytes, clipSizeBytes)
		if err != nil {
			return err
		}
		if !isCleanRecordingVoice(samples) {
			continue
		}
		waveBytes := recordingReferenceWave(samples)
		digest := sha256.Sum256(waveBytes)
		blob := &models.RecordingBlob{BlobSHA256: hex.EncodeToString(digest[:]), BlobSizeBytes: int64(len(waveBytes))}
		var temporarySpeaker *models.RecordingTemporarySpeaker
		err = self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			temporarySpeaker, err = transaction.CreateRecordingTemporarySpeaker(userId, &models.RecordingTemporarySpeaker{RecordingID: transcriptRun.RecordingID, SourceAttemptID: attempt.ID, SourceLocalSpeakerID: localSpeakerId, PassageStartMS: clipStartMS, PassageEndMS: clipEndMS}, blob, int64(maximumAccountBytes))
			return err
		})
		if errors.Is(err, db.ErrTooMuchAsked) {
			return nil
		}
		if err != nil {
			return err
		}
		return self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			sampleBlob, err := transaction.GetRecordingBlob(userId, temporarySpeaker.RecordingBlobID)
			if err != nil {
				return err
			}
			if sampleBlob.IsStoredVerified {
				return nil
			}
			if err := files.PutFileStream(ctx, sampleBlob.ID, bytes.NewReader(waveBytes), int64(len(waveBytes))); err != nil {
				return err
			}
			return transaction.VerifyRecordingBlob(userId, sampleBlob.ID, hex.EncodeToString(digest[:]))
		})
	}
	// No passage gave a clean sample: the speaker still gets a label, so the
	// conversation shows "Speaker N" for every voice and each can be named.
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		_, err := transaction.CreateRecordingTemporarySpeaker(userId, &models.RecordingTemporarySpeaker{RecordingID: transcriptRun.RecordingID, SourceAttemptID: attempt.ID, SourceLocalSpeakerID: localSpeakerId}, nil, int64(maximumAccountBytes))
		return err
	})
	if errors.Is(err, db.ErrTooMuchAsked) {
		return nil
	}
	return err
}
