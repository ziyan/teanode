package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

// CurateRecordingSpeakers derives clean references only from explicit confirmed intervals.
// Lack of suitable audio is a successful result with no new references.
func (self *Agent) CurateRecordingSpeakers(ctx context.Context, userId, recordingId string) error {
	files, hasStreaming := self.settings.Storage.(storage.StreamFiles)
	if !hasStreaming {
		return fmt.Errorf("recording reference storage does not support streaming")
	}
	var source *models.Recording
	var corrections []*models.RecordingSpeakerCorrection
	var references []*models.RecordingVoiceReference
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		source, err = transaction.GetRecording(userId, recordingId)
		if err != nil {
			return err
		}
		corrections, err = transaction.ListRecordingSpeakerCorrections(userId, recordingId)
		if err != nil {
			return err
		}
		references, err = transaction.ListRecordingVoiceReferences(userId, recordingId)
		return err
	}); err != nil {
		return err
	}
	// Revoked rows remain as a durable refusal to select the same sample again.
	for _, reference := range references {
		if !reference.IsRevoked || reference.RecordingBlobID == "" {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			if _, err := transaction.LockRecording(userId, recordingId); err != nil {
				return err
			}
			if err := files.DeleteFileEverywhere(ctx, reference.RecordingBlobID); err != nil {
				return err
			}
			return transaction.DeleteRevokedRecordingVoiceReference(userId, recordingId, reference.ID)
		}); err != nil {
			return err
		}
	}
	maximumAccountBytes := self.settings.Configuration().Agent.Limits.MaxRecordingAccountBytes.Bytes()
	if maximumAccountBytes == 0 {
		maximumAccountBytes = 64 << 30
	}
	if maximumAccountBytes > math.MaxInt64 {
		return fmt.Errorf("recording account limit exceeds supported bytes")
	}
	referenceCountByIdentity := map[string]int{}
	for _, reference := range references {
		if !reference.IsRevoked {
			referenceCountByIdentity[reference.SpeakerIdentityID]++
		}
	}
	for _, correction := range corrections {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if correction.IsRemoved || referenceCountByIdentity[correction.SpeakerIdentityID] >= 6 || correction.PassageEndMS-correction.PassageStartMS < 2400 {
			continue
		}
		hasOverride := false
		for _, later := range corrections {
			if later.RecordingMediaID == correction.RecordingMediaID && later.SourceGeneration > correction.SourceGeneration && later.PassageStartMS < correction.PassageEndMS && later.PassageEndMS > correction.PassageStartMS {
				hasOverride = true
				break
			}
		}
		if hasOverride {
			continue
		}
		var selectedRun *models.RecordingTranscriptRun
		var selectedAttempt *models.RecordingTranscriptionAttempt
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			selectedRun, selectedAttempt, err = transaction.GetRecordingSpeakerEvidence(userId, recordingId, correction.RecordingTranscriptionAttemptID)
			return err
		}); err != nil {
			return err
		}
		if selectedRun == nil {
			continue
		}
		clipStartMS := correction.PassageStartMS + 200
		clipEndMS := min(correction.PassageEndMS-200, clipStartMS+6000)
		var chunks []*models.RecordingAudioChunk
		hasOverlap := false
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			hasOverlap, err = transaction.HasRecordingSpeakerOverlap(userId, selectedRun.ID, selectedAttempt.ID, correction.LocalSpeakerID, clipStartMS-200, clipEndMS+200)
			if err != nil {
				return err
			}
			chunks, err = transaction.ListRecordingAudioChunks(userId, selectedRun.ID)
			return err
		}); err != nil {
			return err
		}
		if hasOverlap {
			continue
		}
		var audioChunk *models.RecordingAudioChunk
		for _, chunk := range chunks {
			if chunk.ID == selectedAttempt.RecordingAudioChunkID && clipStartMS >= chunk.TimelineStartMS && clipEndMS <= chunk.TimelineStartMS+chunk.AudioDurationMS {
				audioChunk = chunk
				break
			}
		}
		if audioChunk == nil {
			continue
		}
		var audioBlob *models.RecordingBlob
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			audioBlob, err = transaction.GetRecordingBlob(userId, audioChunk.RecordingBlobID)
			return err
		}); err != nil {
			return err
		}
		if !audioBlob.IsStoredVerified {
			continue
		}
		sampleOffsetBytes := 44 + (clipStartMS-audioChunk.TimelineStartMS)*recording.AudioSampleRateHz*2/1000
		clipSizeBytes := (clipEndMS - clipStartMS) * recording.AudioSampleRateHz * 2 / 1000
		stream, err := files.OpenFileRange(ctx, audioBlob.ID, sampleOffsetBytes, clipSizeBytes)
		if err != nil {
			return err
		}
		samples, err := io.ReadAll(io.LimitReader(stream, clipSizeBytes+1))
		closeErr := stream.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(samples)) != clipSizeBytes {
			return fmt.Errorf("recording reference audio is incomplete")
		}
		if !isCleanRecordingVoice(samples) {
			continue
		}
		waveBytes := recordingReferenceWave(samples)
		digest := sha256.Sum256(waveBytes)
		reference := &models.RecordingVoiceReference{RecordingID: recordingId, RecordingMediaID: correction.RecordingMediaID, RecordingTranscriptRunID: selectedRun.ID, RecordingSpeakerCorrectionID: correction.ID, SpeakerIdentityID: correction.SpeakerIdentityID, PassageStartMS: clipStartMS, PassageEndMS: clipEndMS, SourceGeneration: source.SourceGeneration}
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			reference, err = transaction.CreateRecordingVoiceReference(userId, reference, &models.RecordingBlob{BlobSHA256: hex.EncodeToString(digest[:]), BlobSizeBytes: int64(len(waveBytes))}, int64(maximumAccountBytes))
			return err
		}); err != nil {
			if err == db.ErrTooMuchAsked {
				referenceCountByIdentity[correction.SpeakerIdentityID] = 6
				continue
			}
			return err
		}
		if reference.IsRevoked || reference.RecordingBlobID == "" {
			continue
		}
		referenceCountByIdentity[correction.SpeakerIdentityID]++
		// Reservation commits before bytes; a failed storage write retains a cleanup reference.
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			current, err := transaction.LockRecording(userId, recordingId)
			if err != nil {
				return err
			}
			if current.SourceGeneration != source.SourceGeneration {
				return db.ErrAlreadyExists
			}
			if err := files.PutFileStream(ctx, reference.RecordingBlobID, bytes.NewReader(waveBytes), int64(len(waveBytes))); err != nil {
				return err
			}
			return transaction.VerifyRecordingBlob(userId, reference.RecordingBlobID, hex.EncodeToString(digest[:]))
		}); err != nil {
			return err
		}
	}
	return self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.MarkRecordingSpeakerCuration(userId, recordingId, source.SourceGeneration)
	})
}

// The fixed normalized PCM format comes from TeaNode's own decoder. Conservative
// gates reject clipping, nearly silent speech and high measured background noise.
// This is sample screening, not a promise that a model's diarization is correct.
func isCleanRecordingVoice(samples []byte) bool {
	if len(samples) < 64000 || len(samples)%2 != 0 {
		return false
	}
	frameSamples := 320
	voicedFrames, quietFrames, totalFrames := 0, 0, 0
	clippedSamples := 0
	for start := 0; start+frameSamples*2 <= len(samples); start += frameSamples * 2 {
		energy := float64(0)
		for offset := start; offset < start+frameSamples*2; offset += 2 {
			sample := float64(int16(binary.LittleEndian.Uint16(samples[offset:]))) / 32768
			energy += sample * sample
			if math.Abs(sample) > 0.98 {
				clippedSamples++
			}
		}
		rms := math.Sqrt(energy / float64(frameSamples))
		totalFrames++
		if rms > 0.025 {
			voicedFrames++
		}
		if rms < 0.008 {
			quietFrames++
		}
	}
	return totalFrames > 0 && voicedFrames*100 >= totalFrames*30 && quietFrames*100 >= totalFrames*5 && clippedSamples*1000 < len(samples)/2
}
func recordingReferenceWave(samples []byte) []byte {
	waveBytes := make([]byte, 44+len(samples))
	copy(waveBytes, "RIFF")
	binary.LittleEndian.PutUint32(waveBytes[4:], uint32(len(waveBytes)-8))
	copy(waveBytes[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(waveBytes[16:], 16)
	binary.LittleEndian.PutUint16(waveBytes[20:], 1)
	binary.LittleEndian.PutUint16(waveBytes[22:], 1)
	binary.LittleEndian.PutUint32(waveBytes[24:], 16000)
	binary.LittleEndian.PutUint32(waveBytes[28:], 32000)
	binary.LittleEndian.PutUint16(waveBytes[32:], 2)
	binary.LittleEndian.PutUint16(waveBytes[34:], 16)
	copy(waveBytes[36:], "data")
	binary.LittleEndian.PutUint32(waveBytes[40:], uint32(len(samples)))
	copy(waveBytes[44:], samples)
	return waveBytes
}

type recordingCurationKey struct {
	Worker      *Agent
	RecordingID string
}

var recordingCurationInFlight sync.Map

// WakeRecordingSpeakerCuration coalesces bounded local work after a committed confirmation.
// Persistent generation markers let the periodic worker recover missed wakeups.
func (self *Agent) WakeRecordingSpeakerCuration(userId, recordingId string) {
	if self.ctx == nil {
		return
	}
	curationKey := recordingCurationKey{Worker: self, RecordingID: recordingId}
	if _, isRunning := recordingCurationInFlight.LoadOrStore(curationKey, true); isRunning {
		return
	}
	go func() {
		defer recordingCurationInFlight.Delete(curationKey)
		ctx, cancel := context.WithTimeout(self.ctx, 2*time.Minute)
		defer cancel()
		if err := self.CurateRecordingSpeakers(ctx, userId, recordingId); err != nil && ctx.Err() == nil {
			log.Warningf("cannot curate confirmed recording speakers: %s", err)
		}
	}()
}
