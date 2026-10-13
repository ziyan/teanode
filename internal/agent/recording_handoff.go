package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
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

	// recordingHandoffWait is how long a piece waits for the piece before it
	// to be published, so the handoff is not skipped only because the two were
	// picked up together. A piece before it that has no job going is not waited
	// for at all.
	recordingHandoffWait = 30 * time.Minute

	// recordingHandoffContinuationMS is how soon after the start of a piece its
	// first passage must begin for the speech to count as running on from the
	// piece before it.
	recordingHandoffContinuationMS int64 = 2000

	// recordingHandoffHeadLongestMS bounds how much of a piece is sent again
	// beside the end of the piece before it. A first passage longer than this
	// is left as the piece heard it.
	recordingHandoffHeadLongestMS int64 = 30000

	// recordingPiecesAtOnce is how many pieces of one recording are between
	// their upload and their publication at a time. Their provider requests
	// do not depend on one another, so this is what a backlog of pieces
	// clears at; it is bounded so one recording cannot take every slot.
	recordingPiecesAtOnce = 3

	// recordingWaitingPoll is the longest a piece that cannot go on yet waits
	// before looking again. The piece it waits for wakes it when it is done;
	// this only covers a wake that was missed, which happens when the wake
	// comes while the waiting piece is still running and is about to wait.
	// The first looks come sooner for that reason, and the wait doubles from
	// there so a long wait is not a busy one.
	recordingWaitingPoll = 30 * time.Second

	// recordingWaitingPollFirst is how long a piece waits the first time.
	recordingWaitingPollFirst = 2 * time.Second

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

// recordingWaitingPollFor is how long a job that has to wait waits, given how
// many times it has been claimed.
func recordingWaitingPollFor(job *models.AgentJob) time.Duration {
	attempts := max(job.Attempts, 1)
	if attempts > 5 {
		return recordingWaitingPoll
	}
	return min(recordingWaitingPollFirst<<(attempts-1), recordingWaitingPoll)
}

// recordingPieceBefore is the piece recorded just before a media, or nil.
func recordingPieceBefore(pieces []*db.RecordingPiece, media *models.RecordingMedia) *db.RecordingPiece {
	var previous *db.RecordingPiece
	for _, piece := range pieces {
		if piece.RecordingMediaID != media.ID && piece.TimelineStartMS < media.TimelineStartMS && (previous == nil || piece.TimelineStartMS > previous.TimelineStartMS) {
			previous = piece
		}
	}
	return previous
}

// countPendingRecordingPiecesBefore counts the pieces recorded before a media
// that are still on their way to being published.
func countPendingRecordingPiecesBefore(pieces []*db.RecordingPiece, media *models.RecordingMedia) int {
	pendingCount := 0
	for _, piece := range pieces {
		if piece.RecordingMediaID != media.ID && piece.TimelineStartMS < media.TimelineStartMS && piece.IsPending() {
			pendingCount++
		}
	}
	return pendingCount
}

// isRecordingPieceTooFarAhead says a new run's piece is behind enough
// unpublished pieces that it would be told to wait if it started now.
func (self *Agent) isRecordingPieceTooFarAhead(transaction db.Transaction, userId string, transcriptRun *models.RecordingTranscriptRun) (bool, error) {
	media, err := transaction.GetRecordingMedia(userId, transcriptRun.RecordingMediaID)
	if err != nil {
		return false, err
	}
	pieces, err := transaction.ListRecordingPieces(userId, transcriptRun.RecordingID)
	if err != nil {
		return false, err
	}
	return countPendingRecordingPiecesBefore(pieces, media) >= recordingPiecesAtOnce, nil
}

// waitForRecordingWindow keeps a piece from starting while the pieces before
// it that are not yet published already fill the window, so a backlog is worked
// off a few pieces at a time in the order it was recorded. It returns a Deferral
// when the piece must wait.
func (self *Agent) waitForRecordingWindow(ctx context.Context, job *models.AgentJob, userId string, media *models.RecordingMedia) error {
	var pieces []*db.RecordingPiece
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		pieces, err = transaction.ListRecordingPieces(userId, media.RecordingID)
		return err
	}); err != nil {
		return err
	}
	if countPendingRecordingPiecesBefore(pieces, media) >= recordingPiecesAtOnce {
		return &Deferral{Until: time.Now().Add(recordingWaitingPollFor(job)), Reason: "waiting for earlier pieces of the recording to be published"}
	}
	return nil
}

// wakeRecordingPieces makes the pieces of a recording that can go on now due
// and claims them, after a piece was published or ended without a transcript.
func (self *Agent) wakeRecordingPieces(ctx context.Context, userId, recordingId string) {
	var wokenCount int64
	if err := self.settings.Database.TransactionContext(context.WithoutCancel(ctx), func(transaction db.Transaction) (err error) {
		wokenCount, err = transaction.WakeRecordingTranscriptionJobs(userId, recordingId, recordingPiecesAtOnce)
		return err
	}); err != nil {
		log.Warningf("cannot wake the pieces of recording %q: %s", recordingId, err)
		return
	}
	if wokenCount > 0 {
		self.claimSoon()
	}
}

// publishRecordingRun stitches a piece whose speech-to-text is in to the piece
// before it and makes its passages final for the next piece to read. The
// pieces are published in the order they were recorded: a piece whose
// predecessor is still on its way waits (a Deferral), and the predecessor wakes
// it when it is published.
//
// When the speech at the end of the predecessor ran across the cut, the end of
// the predecessor and the start of this piece, through the end of the passage
// that carried on, are sent as one more chunk, so the sentence is heard whole.
// The passages this piece heard on its own in that stretch are replaced by the
// ones from the second look, and the predecessor's are hidden from where it
// began (see db.PublishRecordingTranscriptRun).
func (self *Agent) publishRecordingRun(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, selection *llm.TranscriptionSelection, files storage.StreamFiles, vocabularyPrompt string) error {
	if transcriptRun.PublishedAt != nil {
		return nil
	}
	userId := run.Agent.UserID
	claim := db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}
	var source *models.Recording
	var media *models.RecordingMedia
	var pieces []*db.RecordingPiece
	var chunks []*models.RecordingAudioChunk
	var segments []*models.RecordingTranscriptSegment
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if source, err = transaction.GetRecording(userId, transcriptRun.RecordingID); err != nil {
			return err
		}
		if media, err = transaction.GetRecordingMedia(userId, transcriptRun.RecordingMediaID); err != nil {
			return err
		}
		if source.RecordingOrigin == models.RecordingOriginCapture {
			if pieces, err = transaction.ListRecordingPieces(userId, transcriptRun.RecordingID); err != nil {
				return err
			}
		}
		if chunks, err = transaction.ListRecordingAudioChunks(userId, transcriptRun.ID); err != nil {
			return err
		}
		segments, err = transaction.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
		return err
	}); err != nil {
		return err
	}
	publish := func(handoff *db.RecordingHandoffPublication) error {
		if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
			return transaction.PublishRecordingTranscriptRun(userId, transcriptRun.ID, claim, handoff)
		}); err != nil {
			return err
		}
		now := time.Now()
		transcriptRun.PublishedAt = &now
		self.wakeRecordingPieces(ctx, userId, transcriptRun.RecordingID)
		return nil
	}
	if source.RecordingOrigin != models.RecordingOriginCapture {
		return publish(nil)
	}
	// A second look that was begun before the job stopped is finished, not
	// decided again: the piece before may have moved on since.
	var handoffChunk *models.RecordingAudioChunk
	for _, chunk := range chunks {
		if chunk.TimelineStartMS < media.TimelineStartMS {
			handoffChunk = chunk
		}
	}
	if handoffChunk == nil {
		previous := recordingPieceBefore(pieces, media)
		if previous != nil && previous.IsUploadComplete {
			// The piece before is a part of this one's input. If it has
			// no transcript yet, wait for it, but not forever: a failed or
			// abandoned piece must not hold up the rest of the recording.
			isWaitingForUpload := previous.RecordingTranscriptRunID == "" && previous.VerifiedAt != nil && time.Since(*previous.VerifiedAt) < recordingHandoffWait
			if (isWaitingForUpload || previous.IsPending()) && time.Since(transcriptRun.UpdatedAt) < recordingHandoffWait {
				return &Deferral{Until: time.Now().Add(recordingWaitingPollFor(run.Job)), Reason: "waiting for the previous piece to be published"}
			}
		}
		handoff, err := self.recordingHandoffBefore(ctx, userId, media, previous, files)
		if err != nil {
			return err
		}
		var headMS int64
		if handoff != nil {
			headMS = recordingHandoffHeadMS(media, chunks, segments)
		}
		if handoff == nil || headMS == 0 {
			return publish(nil)
		}
		decoded, err := self.recordingHandoffAudio(ctx, userId, media, chunks, handoff, headMS, files)
		if err != nil {
			return err
		}
		if decoded == nil {
			return publish(nil)
		}
		decoded.ChunkIndex = len(chunks)
		maximumAccountBytes := self.settings.Configuration().Agent.Limits.MaxRecordingAccountBytes.Bytes()
		if maximumAccountBytes == 0 {
			maximumAccountBytes = 64 << 30
		}
		if maximumAccountBytes > math.MaxInt64 {
			return fmt.Errorf("recording: account limit exceeds supported byte sizes")
		}
		if handoffChunk, err = self.storeRecordingChunk(ctx, run, transcriptRun, files, decoded, int64(maximumAccountBytes)); err != nil {
			return err
		}
	}
	shouldContinue, err := self.transcribeRecordingChunk(ctx, run, transcriptRun, selection, files, handoffChunk, vocabularyPrompt)
	if err != nil || !shouldContinue {
		return err
	}
	var attempt *models.RecordingTranscriptionAttempt
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		attempt, err = transaction.GetLatestRecordingChunkAttempt(userId, handoffChunk.ID)
		return err
	}); err != nil {
		return err
	}
	if attempt == nil || attempt.RecordingAttemptState != models.RecordingAttemptComplete {
		return fmt.Errorf("recording: the second look at the cut has no transcript")
	}
	return publish(&db.RecordingHandoffPublication{HandoffStartMS: handoffChunk.TimelineStartMS, HandoffEndMS: handoffChunk.TimelineStartMS + handoffChunk.AudioDurationMS, HandoffAttemptID: attempt.ID})
}

// recordingHandoffHeadMS says how much of the start of a piece is sent again
// beside the end of the piece before it: through the end of the passage that
// carried on from the cut, and anything that overlaps it. It is zero when the
// piece does not start with such a passage, or when it is too long to be sent
// again.
func recordingHandoffHeadMS(media *models.RecordingMedia, chunks []*models.RecordingAudioChunk, segments []*models.RecordingTranscriptSegment) int64 {
	var first *models.RecordingTranscriptSegment
	for _, segment := range segments {
		if first == nil || segment.SegmentStartMS < first.SegmentStartMS || (segment.SegmentStartMS == first.SegmentStartMS && segment.SegmentEndMS > first.SegmentEndMS) {
			first = segment
		}
	}
	if first == nil || first.SegmentStartMS-media.TimelineStartMS > recordingHandoffContinuationMS {
		return 0
	}
	endMS := first.SegmentEndMS
	for isExtended := true; isExtended; {
		isExtended = false
		for _, segment := range segments {
			if segment.SegmentStartMS < endMS && segment.SegmentEndMS > endMS {
				endMS = segment.SegmentEndMS
				isExtended = true
			}
		}
	}
	headMS := endMS - media.TimelineStartMS
	if headMS <= 0 || headMS > recordingHandoffHeadLongestMS || len(chunks) == 0 || endMS > chunks[0].TimelineStartMS+chunks[0].AudioDurationMS {
		return 0
	}
	return headMS
}

// recordingHandoffAudio joins the end of the piece before with the start of this
// piece, read from its first chunk, into one chunk that begins where the handoff
// does. It is nil when the first chunk's audio cannot be read.
func (self *Agent) recordingHandoffAudio(ctx context.Context, userId string, media *models.RecordingMedia, chunks []*models.RecordingAudioChunk, handoff *recordingHandoff, headMS int64, files storage.StreamFiles) (*recording.DecodedAudioChunk, error) {
	var blob *models.RecordingBlob
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		blob, err = transaction.GetRecordingBlob(userId, chunks[0].RecordingBlobID)
		return err
	}); err != nil {
		return nil, err
	}
	if !blob.IsStoredVerified {
		return nil, nil
	}
	headBytes := headMS * recordingPCMBytesPerMS
	samples := make([]byte, len(handoff.PCM)+int(headBytes))
	copy(samples, handoff.PCM)
	stream, err := files.OpenFileRange(ctx, blob.ID, 44, headBytes)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, readError := io.ReadFull(stream, samples[len(handoff.PCM):])
	closeError := stream.Close()
	if readError != nil {
		return nil, readError
	}
	if closeError != nil {
		return nil, closeError
	}
	return &recording.DecodedAudioChunk{TimelineStartMS: handoff.StartMS, AudioDurationMS: media.TimelineStartMS - handoff.StartMS + headMS, WaveBytes: recordingReferenceWave(samples)}, nil
}

// recordingHandoffBefore finds out whether the speech at the end of the piece
// before this one ran across the cut, and if so reads that passage's audio. It
// is nil when there is nothing to hand off: an import, a first piece, a pause
// between pieces, a last passage that ended before the cut, or a piece before
// that has no published transcript.
func (self *Agent) recordingHandoffBefore(ctx context.Context, userId string, media *models.RecordingMedia, previous *db.RecordingPiece, files storage.StreamFiles) (*recordingHandoff, error) {
	if previous == nil || !previous.IsUploadComplete || previous.RecordingTranscriptRunID == "" {
		return nil, nil
	}
	// Segments are final once the piece is published, which is before it is
	// complete: the second pass over its words does not change them.
	isFinal := previous.RecordingProcessingState == models.RecordingProcessingComplete || (previous.RecordingProcessingState == models.RecordingProcessingTranscribing && previous.PublishedAt != nil)
	if !isFinal {
		return nil, nil
	}
	var segments []*models.RecordingTranscriptSegment
	var chunks []*models.RecordingAudioChunk
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		segments, err = transaction.ListRecordingTranscriptSegments(userId, previous.RecordingTranscriptRunID)
		if err != nil {
			return err
		}
		chunks, err = transaction.ListRecordingAudioChunks(userId, previous.RecordingTranscriptRunID)
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
	previousEndMS := previous.TimelineStartMS + previous.DerivedDurationMS
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
