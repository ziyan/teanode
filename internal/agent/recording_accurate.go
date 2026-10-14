package agent

import (
	"context"
	"errors"
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
	samples, err := readRecordingPCM(ctx, files, blob, 44+(turn.startMS()-turn.Chunk.TimelineStartMS)*recordingPCMBytesPerMS, lengthBytes)
	if err != nil {
		return nil, err
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

// splitRecordingTurnText hands the turn's words back to its passages. Each
// passage starts where its first word lines up with the accurate words, found
// by the longest run of words the two passes share; a passage with no word that
// lines up is cut by its share of the turn's time instead, moved to a sentence
// end close by when there is one. A turn of one passage keeps the text whole. It returns nil
// when the words cannot be shared out, fewer words than passages, so the
// passages keep their first words.
func splitRecordingTurnText(accurateText string, segments []*models.RecordingTranscriptSegment) map[string]string {
	accurateText = strings.TrimSpace(accurateText)
	if accurateText == "" || len(segments) == 0 || !isPlausibleAccurateText(accurateText, segments) {
		return nil
	}
	if len(segments) == 1 {
		return map[string]string{segments[0].ID: accurateText}
	}
	tokens := recordingTextTokens(accurateText)
	if len(tokens) < len(segments) {
		return nil
	}
	alignedCuts := alignedRecordingCuts(tokens, segments)
	turnStartMS := segments[0].SegmentStartMS
	turnDurationMS := float64(segments[len(segments)-1].SegmentEndMS - turnStartMS)
	// cuts[index] is the token index where passage index+1 begins.
	cuts := make([]int, 0, len(segments)-1)
	previousCut := 0
	for index := 0; index < len(segments)-1; index++ {
		lowestCut, highestCut := previousCut+1, len(tokens)-(len(segments)-1-index)
		if aligned := alignedCuts[index]; aligned >= lowestCut && aligned <= highestCut {
			cuts = append(cuts, aligned)
			previousCut = aligned
			continue
		}
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

// isPlausibleAccurateText says whether the second reading of a turn can stand
// for the first. A transcription model can answer with something other than
// what was said: a few words of it wrapped as data, text the audio does not
// hold, or the words heard as another language (a short turn read alone has
// no language but its own to go by). Such an answer keeps the first words: one
// that looks like data, one written in a script the first reading has none of
// (Latin aside, which names and terms are written in whatever the language),
// one that lost more than half of the first reading's words, and one with more
// than twice as many.
func isPlausibleAccurateText(accurateText string, segments []*models.RecordingTranscriptSegment) bool {
	if (strings.HasPrefix(accurateText, "{") && strings.HasSuffix(accurateText, "}")) || (strings.HasPrefix(accurateText, "[") && strings.HasSuffix(accurateText, "]")) {
		return false
	}
	firstText := ""
	for _, segment := range segments {
		firstText += segment.TranscriptText + " "
	}
	firstScripts := recordingTextScripts(firstText)
	for script := range recordingTextScripts(accurateText) {
		if len(firstScripts) > 0 && script != "Latin" && !firstScripts[script] {
			return false
		}
	}
	firstWordCount := 0
	for _, segment := range segments {
		words, _ := recordingWords(recordingTextTokens(segment.TranscriptText))
		firstWordCount += len(words)
	}
	accurateWords, _ := recordingWords(recordingTextTokens(accurateText))
	if firstWordCount < recordingPlausibleWordsMinimum {
		return true
	}
	return 2*len(accurateWords) >= firstWordCount && len(accurateWords) <= 2*firstWordCount
}

// recordingTextScripts names the scripts the letters of a text are written in.
func recordingTextScripts(text string) map[string]bool {
	scripts := map[string]bool{}
	for _, character := range text {
		if !unicode.IsLetter(character) {
			continue
		}
		for name, table := range unicode.Scripts {
			if unicode.Is(table, character) {
				scripts[name] = true
				break
			}
		}
	}
	return scripts
}

// Below this many words, a first reading is too short to judge a second one
// by its share of them.
const recordingPlausibleWordsMinimum = 4

// recordingWords are the words of a text, lowercase letters and digits only, so
// "person-our" and "person. Our" both read as person, our. Each word carries
// the index of the token it came from.
func recordingWords(tokens []string) ([]string, []int) {
	var words []string
	var tokenIndexes []int
	for tokenIndex, token := range tokens {
		var current []rune
		flush := func() {
			if len(current) > 0 {
				words = append(words, string(current))
				tokenIndexes = append(tokenIndexes, tokenIndex)
				current = nil
			}
		}
		for _, character := range strings.ToLower(token) {
			if unicode.IsLetter(character) || unicode.IsDigit(character) {
				current = append(current, character)
			} else {
				flush()
			}
		}
		flush()
	}
	return words, tokenIndexes
}

// alignedRecordingCuts says, for each passage after the first, the token of the
// accurate text where it starts: the token of the accurate word lined up with
// the passage's first word that lines up at all, by the longest common run of
// words between the first pass and the accurate one. -1 where none lines up.
func alignedRecordingCuts(tokens []string, segments []*models.RecordingTranscriptSegment) []int {
	accurateWords, accurateTokenIndexes := recordingWords(tokens)
	var firstWords []string
	passageStarts := make([]int, 0, len(segments))
	for _, segment := range segments {
		passageStarts = append(passageStarts, len(firstWords))
		words, _ := recordingWords(recordingTextTokens(segment.TranscriptText))
		firstWords = append(firstWords, words...)
	}
	cuts := make([]int, len(segments)-1)
	for index := range cuts {
		cuts[index] = -1
	}
	if len(firstWords) == 0 || len(accurateWords) == 0 || len(firstWords)*len(accurateWords) > 4_000_000 {
		return cuts
	}
	// lengths[first][accurate] is the longest common run of the words from
	// those positions on.
	lengths := make([][]int, len(firstWords)+1)
	for index := range lengths {
		lengths[index] = make([]int, len(accurateWords)+1)
	}
	for first := len(firstWords) - 1; first >= 0; first-- {
		for accurate := len(accurateWords) - 1; accurate >= 0; accurate-- {
			if firstWords[first] == accurateWords[accurate] {
				lengths[first][accurate] = lengths[first+1][accurate+1] + 1
			} else {
				lengths[first][accurate] = max(lengths[first+1][accurate], lengths[first][accurate+1])
			}
		}
	}
	alignedTo := make([]int, len(firstWords))
	for index := range alignedTo {
		alignedTo[index] = -1
	}
	for first, accurate := 0, 0; first < len(firstWords) && accurate < len(accurateWords); {
		switch {
		case firstWords[first] == accurateWords[accurate]:
			alignedTo[first] = accurate
			first++
			accurate++
		case lengths[first+1][accurate] >= lengths[first][accurate+1]:
			first++
		default:
			accurate++
		}
	}
	for index := range cuts {
		passageStart, passageEnd := passageStarts[index+1], len(firstWords)
		if index+2 < len(passageStarts) {
			passageEnd = passageStarts[index+2]
		}
		for first := passageStart; first < passageEnd; first++ {
			if alignedTo[first] >= 0 {
				cuts[index] = accurateTokenIndexes[alignedTo[first]]
				break
			}
		}
	}
	return cuts
}
