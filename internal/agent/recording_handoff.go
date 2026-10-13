package agent

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

const (
	// recordingHandoffLongestMS is how much of the previous piece is read again.
	// A passage that began earlier than this before the cut is left as it is,
	// because replacing only its tail would show its beginning twice.
	recordingHandoffLongestMS int64 = 15000

	// recordingHandoffNearCutMS is how close to the end of a piece its last
	// passage must end for the speech to count as running into the cut.
	recordingHandoffNearCutMS int64 = 500

	// recordingHandoffGapMS is the longest silence between two pieces that
	// still counts as one continuous recording. A longer one is a pause.
	recordingHandoffGapMS int64 = 1000

	// recordingHandoffWait is how long a piece waits for the transcript of
	// the piece before it, so the handoff is not skipped only because the two
	// were picked up together.
	recordingHandoffWait = 30 * time.Minute

	// recordingPCMBytesPerMS is 16 kHz mono 16-bit audio.
	recordingPCMBytesPerMS int64 = recording.AudioSampleRateHz * 2 / 1000
)

// recordingHandoff is the end of the previous piece of a capture, to be read
// again in front of the next one.
type recordingHandoff struct {
	// StartMS is the timeline millisecond the previous piece's last passage began.
	StartMS int64
	// PCM runs from StartMS to the start of the piece it precedes, 16 kHz mono 16-bit.
	PCM []byte
}

// recordingHandoffBefore finds out whether the speech at the end of the piece
// before this one ran across the cut, and if so reads that passage's audio. It
// is nil when there is nothing to hand off: an import, a first piece, a pause
// between pieces, or a last passage that ended before the cut. When the piece
// before is still being transcribed it returns a Deferral, because its last
// passage is what decides.
func (self *Agent) recordingHandoffBefore(ctx context.Context, userId string, media *models.RecordingMedia, files storage.StreamFiles) (*recordingHandoff, error) {
	var source *models.Recording
	var siblings []*models.RecordingMedia
	var runs []*models.RecordingTranscriptRun
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		source, err = transaction.GetRecording(userId, media.RecordingID)
		if err != nil {
			return err
		}
		if source.RecordingOrigin != models.RecordingOriginCapture {
			return nil
		}
		siblings, err = transaction.ListRecordingMedia(userId, media.RecordingID)
		if err != nil {
			return err
		}
		runs, err = transaction.ListRecordingTranscriptRuns(userId, media.RecordingID)
		return err
	}); err != nil {
		return nil, err
	}
	if source.RecordingOrigin != models.RecordingOriginCapture {
		return nil, nil
	}
	var previousMedia *models.RecordingMedia
	for _, sibling := range siblings {
		if sibling.ID != media.ID && sibling.TimelineStartMS < media.TimelineStartMS && (previousMedia == nil || sibling.TimelineStartMS > previousMedia.TimelineStartMS) {
			previousMedia = sibling
		}
	}
	if previousMedia == nil || previousMedia.RecordingUploadState != models.RecordingUploadComplete {
		return nil, nil
	}
	var previousRun *models.RecordingTranscriptRun
	for _, candidate := range runs {
		if candidate.RecordingMediaID == previousMedia.ID && (previousRun == nil || candidate.VersionNumber > previousRun.VersionNumber) {
			previousRun = candidate
		}
	}
	// The piece before is a part of this one's input. If it has no transcript
	// yet, wait for it, but not forever: a failed or abandoned piece must not
	// hold up the rest of the recording.
	if previousRun == nil {
		if previousMedia.VerifiedAt != nil && time.Since(*previousMedia.VerifiedAt) < recordingHandoffWait {
			return nil, &Deferral{Until: time.Now().Add(5 * time.Second), Reason: "waiting for the transcript of the previous piece"}
		}
		return nil, nil
	}
	switch previousRun.RecordingProcessingState {
	case models.RecordingProcessingQueued, models.RecordingProcessingPreparing, models.RecordingProcessingTranscribing:
		if time.Since(previousRun.CreatedAt) < recordingHandoffWait {
			return nil, &Deferral{Until: time.Now().Add(5 * time.Second), Reason: "waiting for the transcript of the previous piece"}
		}
		return nil, nil
	case models.RecordingProcessingComplete:
	default:
		return nil, nil
	}
	var segments []*models.RecordingTranscriptSegment
	var chunks []*models.RecordingAudioChunk
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		segments, err = transaction.ListRecordingTranscriptSegments(userId, previousRun.ID)
		if err != nil {
			return err
		}
		chunks, err = transaction.ListRecordingAudioChunks(userId, previousRun.ID)
		return err
	}); err != nil {
		return nil, err
	}
	var lastSegment *models.RecordingTranscriptSegment
	for _, segment := range segments {
		if lastSegment == nil || segment.SegmentEndMS > lastSegment.SegmentEndMS || (segment.SegmentEndMS == lastSegment.SegmentEndMS && segment.SegmentStartMS < lastSegment.SegmentStartMS) {
			lastSegment = segment
		}
	}
	if lastSegment == nil {
		return nil, nil
	}
	previousEndMS := previousMedia.TimelineStartMS + previousRun.DerivedDurationMS
	gapMS := media.TimelineStartMS - previousEndMS
	if previousEndMS-lastSegment.SegmentEndMS > recordingHandoffNearCutMS || gapMS > recordingHandoffGapMS || gapMS < -recordingHandoffNearCutMS {
		return nil, nil
	}
	if lastSegment.SegmentStartMS >= media.TimelineStartMS || media.TimelineStartMS-lastSegment.SegmentStartMS > recordingHandoffLongestMS {
		return nil, nil
	}
	handoff := &recordingHandoff{StartMS: lastSegment.SegmentStartMS, PCM: make([]byte, (media.TimelineStartMS-lastSegment.SegmentStartMS)*recordingPCMBytesPerMS)}
	// The previous piece's own decoded audio is kept, so the tail is cut from
	// it instead of decoding the original again. Where it ends before this
	// piece begins, the rest stays silent.
	for _, chunk := range chunks {
		overlapStartMS := max(handoff.StartMS, chunk.TimelineStartMS)
		overlapEndMS := min(media.TimelineStartMS, chunk.TimelineStartMS+chunk.AudioDurationMS)
		if overlapEndMS <= overlapStartMS {
			continue
		}
		var blob *models.RecordingBlob
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			blob, err = transaction.GetRecordingBlob(userId, chunk.RecordingBlobID)
			return err
		}); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		if !blob.IsStoredVerified {
			return nil, nil
		}
		lengthBytes := (overlapEndMS - overlapStartMS) * recordingPCMBytesPerMS
		stream, err := files.OpenFileRange(ctx, blob.ID, 44+(overlapStartMS-chunk.TimelineStartMS)*recordingPCMBytesPerMS, lengthBytes)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		_, readError := io.ReadFull(stream, handoff.PCM[(overlapStartMS-handoff.StartMS)*recordingPCMBytesPerMS:][:lengthBytes])
		closeError := stream.Close()
		if readError != nil {
			return nil, readError
		}
		if closeError != nil {
			return nil, closeError
		}
	}
	return handoff, nil
}
