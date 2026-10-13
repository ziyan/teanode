package agent

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

const (
	// recordingTurnLongestMS bounds the audio of one second-pass request, so
	// a long monologue is several requests and each one gets the end of the
	// one before it as its prompt.
	recordingTurnLongestMS int64 = 30000
	// recordingTurnShortestMS is the shortest turn worth a second request.
	// Under it the audio holds a word or two, which the model hears worse
	// alone than the speaker-separating model did in context.
	recordingTurnShortestMS int64 = 1000
	// recordingTurnContextCharacters is how much of the turn before is sent
	// as the prompt, so a sentence that continues across a turn is spelled
	// the same way.
	recordingTurnContextCharacters = 200
	// recordingTurnFailuresLongest is how many turns in a row may fail before
	// the pass gives up on the rest of the run.
	recordingTurnFailuresLongest = 3
)

// recordingTurn is consecutive passages of one speaker in one chunk of audio,
// transcribed again as a whole.
type recordingTurn struct {
	Segments []*models.RecordingTranscriptSegment
	Chunk    *models.RecordingAudioChunk
}

func (self *recordingTurn) startMS() int64 { return self.Segments[0].SegmentStartMS }
func (self *recordingTurn) endMS() int64   { return self.Segments[len(self.Segments)-1].SegmentEndMS }

// recordingTurnsOf groups the passages of a run into turns: consecutive
// passages of the same speaker in the same chunk, at most 30 seconds long.
func recordingTurnsOf(segments []*models.RecordingTranscriptSegment, chunks []*models.RecordingAudioChunk) []*recordingTurn {
	ordered := append([]*models.RecordingTranscriptSegment(nil), segments...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].SegmentStartMS != ordered[right].SegmentStartMS {
			return ordered[left].SegmentStartMS < ordered[right].SegmentStartMS
		}
		return ordered[left].SegmentEndMS < ordered[right].SegmentEndMS
	})
	var turns []*recordingTurn
	var current *recordingTurn
	for _, segment := range ordered {
		var chunk *models.RecordingAudioChunk
		for _, candidate := range chunks {
			if segment.SegmentStartMS >= candidate.TimelineStartMS && segment.SegmentEndMS <= candidate.TimelineStartMS+candidate.AudioDurationMS {
				chunk = candidate
				break
			}
		}
		if chunk == nil {
			current = nil
			continue
		}
		if current != nil && current.Chunk == chunk && current.Segments[0].LocalSpeakerID == segment.LocalSpeakerID && segment.SegmentEndMS-current.startMS() <= recordingTurnLongestMS {
			current.Segments = append(current.Segments, segment)
			continue
		}
		current = &recordingTurn{Segments: []*models.RecordingTranscriptSegment{segment}, Chunk: chunk}
		turns = append(turns, current)
	}
	return turns
}

// recordingAccurateModelName is the plain transcription model the second pass
// uses, written provider:model, or empty when there is none: the voice
// settings' model through their OpenAI-compatible provider.
func (self *Agent) recordingAccurateModelName() string {
	configuration := self.settings.Configuration()
	if !configuration.Agent.Recording.IsAccurateWordsOn() {
		return ""
	}
	provider := configuration.Agent.VoiceProvider()
	if provider == nil {
		return ""
	}
	return provider.Name + ":" + configuration.Agent.Voice.EffectiveTranscriptionModel()
}

// transcribeRecordingAccurateWords is the second pass over a run whose
// speaker-separated passages are published. Each turn's audio is sent to the
// plain transcription model with the vocabulary hint and the end of the turn
// before, and the words that come back are stored beside the passages' first
// words. Speakers and times are never touched. A turn that cannot be
// transcribed again keeps the first words, and so does everything after a
// failure that would repeat (the budget, the provider being unavailable): the
// pass never fails the run, only a canceled context ends it early.
func (self *Agent) transcribeRecordingAccurateWords(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, files storage.StreamFiles, chunks []*models.RecordingAudioChunk, vocabularyPrompt string) error {
	modelName := self.recordingAccurateModelName()
	if modelName == "" || self.settings.Registry == nil {
		return nil
	}
	selection, err := self.settings.Registry.TranscribingFor(modelName, false, false)
	if err != nil {
		log.Warningf("recording %q keeps its first words, the second pass cannot use %q: %s", transcriptRun.RecordingID, modelName, err)
		return nil
	}
	var segments []*models.RecordingTranscriptSegment
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		segments, err = transaction.ListRecordingTranscriptSegments(run.Agent.UserID, transcriptRun.ID)
		return err
	}); err != nil {
		return ctxErrorOnly(ctx, err, transcriptRun.RecordingID)
	}
	claim := db.RecordingWorkerClaim{JobID: run.Job.ID, ClaimID: run.Job.ClaimID}
	previousText := ""
	failureCount := 0
	for _, turn := range recordingTurnsOf(segments, chunks) {
		firstText := joinedTurnText(turn.Segments)
		// A job that starts over finds the turns it already did.
		if turn.hasAccurateWords() {
			previousText = joinedAccurateTurnText(turn.Segments)
			continue
		}
		if turn.endMS()-turn.startMS() < recordingTurnShortestMS || len(strings.Fields(firstText)) == 0 {
			previousText = firstText
			continue
		}
		accurateText, isOver, err := self.transcribeRecordingTurn(ctx, run, transcriptRun, selection, files, claim, turn, modelName, vocabularyPrompt, previousText)
		if err := ctxErrorOnly(ctx, err, transcriptRun.RecordingID); err != nil {
			return err
		}
		if err != nil {
			failureCount++
		} else {
			failureCount = 0
		}
		// A provider that refused three turns in a row will refuse the rest.
		if isOver || failureCount >= recordingTurnFailuresLongest {
			return nil
		}
		if accurateText == "" {
			previousText = firstText
			continue
		}
		previousText = accurateText
	}
	return nil
}

// ctxErrorOnly returns err when the job's own context ended, which the job must
// stop for, and logs any other failure of the pass, which it must not fail for.
func ctxErrorOnly(ctx context.Context, err error, recordingId string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log.Warningf("the second pass of recording %q failed, the first words stand: %s", recordingId, err)
	return nil
}

func (self *recordingTurn) hasAccurateWords() bool {
	for _, segment := range self.Segments {
		if segment.AccurateTranscriptText != "" {
			return true
		}
	}
	return false
}

func joinedTurnText(segments []*models.RecordingTranscriptSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if text := strings.TrimSpace(segment.TranscriptText); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func joinedAccurateTurnText(segments []*models.RecordingTranscriptSegment) string {
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		text := strings.TrimSpace(segment.AccurateTranscriptText)
		if text == "" {
			text = strings.TrimSpace(segment.TranscriptText)
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// transcribeRecordingTurn sends one turn and stores its words. It returns the
// words stored, empty when the first words stand, and whether the pass should
// stop because the next turn would fail the same way.
func (self *Agent) transcribeRecordingTurn(ctx context.Context, run *Run, transcriptRun *models.RecordingTranscriptRun, selection *llm.TranscriptionSelection, files storage.StreamFiles, claim db.RecordingWorkerClaim, turn *recordingTurn, modelName, vocabularyPrompt, previousText string) (string, bool, error) {
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return self.recordingAuthority(transaction, run, modelName, true)
	}); err != nil {
		if ctx.Err() != nil {
			return "", true, ctx.Err()
		}
		// The budget or the account is not available now; the first words
		// stand for this and every later turn.
		log.Noticef("the second pass of recording %q stopped: %s", transcriptRun.RecordingID, err)
		return "", true, nil
	}
	audioBytes, err := self.recordingTurnAudio(ctx, run.Agent.UserID, files, turn)
	if err != nil {
		return "", false, err
	}
	prompt := vocabularyPrompt
	if tail := recordingTurnContext(previousText, recordingTurnContextCharacters); tail != "" {
		prompt = strings.TrimSpace(prompt + "\n" + tail)
	}
	response, err := selection.Transcriber.Transcribe(ctx, &llm.TranscriptionRequest{Model: selection.Model, MediaFilename: "turn.wav", MediaContentType: "audio/wav", MediaBytes: audioBytes, MediaDurationMS: turn.endMS() - turn.startMS(), VocabularyPrompt: prompt})
	if err != nil {
		return "", false, err
	}
	accurateText := strings.TrimSpace(response.TranscriptText)
	texts := splitRecordingTurnText(accurateText, turn.Segments)
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), jobCompletionTimeout)
	defer cancelCompletion()
	// The words and what they cost are written together, so a turn paid for
	// is a turn kept.
	if err := self.settings.Database.TransactionContext(completionContext, func(transaction db.Transaction) error {
		if err := RecordUsageIn(transaction, run.Agent.ID, "", modelName, string(models.AgentJobRecording), response.TranscriptionUsage.Usage); err != nil {
			return err
		}
		if texts == nil {
			return nil
		}
		return transaction.SetRecordingAccurateWords(run.Agent.UserID, transcriptRun.ID, claim, texts)
	}); err != nil {
		return "", false, err
	}
	if texts == nil {
		return "", false, nil
	}
	return accurateText, false, nil
}

// recordingTurnAudio cuts the turn's span from the chunk it lies in.
func (self *Agent) recordingTurnAudio(ctx context.Context, userId string, files storage.StreamFiles, turn *recordingTurn) ([]byte, error) {
	var blob *models.RecordingBlob
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		blob, err = transaction.GetRecordingBlob(userId, turn.Chunk.RecordingBlobID)
		return err
	}); err != nil {
		return nil, err
	}
	if !blob.IsStoredVerified {
		return nil, errors.New("recording: derived audio is not verified")
	}
	lengthBytes := (turn.endMS() - turn.startMS()) * recordingPCMBytesPerMS
	stream, err := files.OpenFileRange(ctx, blob.ID, 44+(turn.startMS()-turn.Chunk.TimelineStartMS)*recordingPCMBytesPerMS, lengthBytes)
	if err != nil {
		return nil, err
	}
	samples := make([]byte, lengthBytes)
	_, readError := io.ReadFull(stream, samples)
	closeError := stream.Close()
	if readError != nil {
		return nil, readError
	}
	if closeError != nil {
		return nil, closeError
	}
	return recordingReferenceWave(samples), nil
}

// recordingTurnContext is the end of text, at most count characters, starting at a
// word.
func recordingTurnContext(text string, count int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= count {
		return string(runes)
	}
	tail := runes[len(runes)-count:]
	if unicode.IsSpace(runes[len(runes)-count-1]) || unicode.Is(unicode.Han, tail[0]) {
		return strings.TrimSpace(string(tail))
	}
	if index := strings.IndexFunc(string(tail), unicode.IsSpace); index >= 0 {
		return strings.TrimSpace(string(tail)[index:])
	}
	return strings.TrimSpace(string(tail))
}

// recordingTextTokens cuts text into pieces that can be handed out to
// passages: a word with the spaces after it, and each character of a script
// written without spaces by itself.
func recordingTextTokens(text string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, string(current))
			current = nil
		}
	}
	isSpaceless := func(character rune) bool {
		return unicode.Is(unicode.Han, character) || unicode.Is(unicode.Hiragana, character) || unicode.Is(unicode.Katakana, character) || unicode.Is(unicode.Hangul, character)
	}
	for _, character := range text {
		switch {
		case unicode.IsSpace(character):
			current = append(current, character)
		case isSpaceless(character):
			flush()
			current = append(current, character)
		default:
			if len(current) > 0 && (unicode.IsSpace(current[len(current)-1]) || isSpaceless(current[0])) {
				flush()
			}
			current = append(current, character)
		}
	}
	flush()
	return tokens
}

func isRecordingSentenceEnd(token string) bool {
	trimmed := strings.TrimRightFunc(token, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune("\"')]”’」』", character)
	})
	return strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "。") || strings.HasSuffix(trimmed, "！") || strings.HasSuffix(trimmed, "？")
}

// splitRecordingTurnText hands the turn's words back to its passages by their
// share of the turn's time, moving each cut to a sentence end close by when
// there is one. A turn of one passage keeps the text whole. It returns nil
// when the words cannot be shared out, fewer words than passages, so the
// passages keep their first words.
func splitRecordingTurnText(accurateText string, segments []*models.RecordingTranscriptSegment) map[string]string {
	accurateText = strings.TrimSpace(accurateText)
	if accurateText == "" || len(segments) == 0 {
		return nil
	}
	if len(segments) == 1 {
		return map[string]string{segments[0].ID: accurateText}
	}
	tokens := recordingTextTokens(accurateText)
	if len(tokens) < len(segments) {
		return nil
	}
	turnStartMS := segments[0].SegmentStartMS
	turnDurationMS := float64(segments[len(segments)-1].SegmentEndMS - turnStartMS)
	// cuts[index] is the token index where passage index+1 begins.
	cuts := make([]int, 0, len(segments)-1)
	previousCut := 0
	for index := 0; index < len(segments)-1; index++ {
		share := 0.0
		if turnDurationMS > 0 {
			share = float64(segments[index].SegmentEndMS-turnStartMS) / turnDurationMS
		} else {
			share = float64(index+1) / float64(len(segments))
		}
		cut := int(share*float64(len(tokens)) + 0.5)
		window := max(1, int(float64(len(tokens))*0.1))
		best, bestDistance := cut, window+1
		for candidate := max(previousCut+1, cut-window); candidate <= min(len(tokens)-(len(segments)-1-index), cut+window); candidate++ {
			if isRecordingSentenceEnd(tokens[candidate-1]) {
				distance := candidate - cut
				if distance < 0 {
					distance = -distance
				}
				if distance < bestDistance {
					best, bestDistance = candidate, distance
				}
			}
		}
		// Leave a token for each passage still to come.
		best = max(previousCut+1, min(best, len(tokens)-(len(segments)-1-index)))
		cuts = append(cuts, best)
		previousCut = best
	}
	texts := make(map[string]string, len(segments))
	start := 0
	for index, segment := range segments {
		end := len(tokens)
		if index < len(cuts) {
			end = cuts[index]
		}
		texts[segment.ID] = strings.TrimSpace(strings.Join(tokens[start:end], ""))
		start = end
	}
	return texts
}
