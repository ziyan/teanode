package models

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TextCorrectionOrigin says who made a text correction.
type TextCorrectionOrigin string

const (
	// TextCorrectionOriginPerson is a correction the person typed. It always wins.
	TextCorrectionOriginPerson TextCorrectionOrigin = "person"

	// TextCorrectionOriginGlossary is a fix the term correction pass made
	// from the person's own notes, contacts and calendar. The person may undo it.
	TextCorrectionOriginGlossary TextCorrectionOrigin = "glossary"
)

// RecordingTranscriptTextCorrection preserves user edits independently of generated versions.
type RecordingTranscriptTextCorrection struct {
	ID                           string    `json:"id"`
	RecordingID                  string    `json:"recordingId"`
	RecordingMediaID             string    `json:"recordingMediaId"`
	RecordingTranscriptRunID     string    `json:"recordingTranscriptRunId"`
	RecordingTranscriptSegmentID string    `json:"recordingTranscriptSegmentId"`
	ClientCorrectionID           string    `json:"clientCorrectionId"`
	PassageStartMS               int64     `json:"passageStartMS"`
	PassageEndMS                 int64     `json:"passageEndMS"`
	OriginalTranscriptText       string    `json:"originalTranscriptText"`
	CorrectedTranscriptText      string    `json:"correctedTranscriptText"`
	SourceGeneration             int64     `json:"sourceGeneration"`
	CreatedAt                    time.Time `json:"createdAt"`
	IsRemoved                    bool      `json:"isRemoved"`

	// IsFolded is set on a glossary fix that was removed because a later fix
	// of the same passage started from the text it produced. It was replaced,
	// not undone, so it does not stop the pass from proposing the fix again.
	IsFolded bool `json:"isFolded"`

	// TextCorrectionOrigin is who made it. For a glossary correction,
	// GlossaryTerm is the term from the person's own data that it spells
	// right, and OriginalPhrase and CorrectedPhrase are the words changed
	// inside the passage; they stay on the row after removal so the same
	// fix is not made again.
	TextCorrectionOrigin TextCorrectionOrigin `json:"textCorrectionOrigin"`
	GlossaryTerm         string               `json:"glossaryTerm"`
	OriginalPhrase       string               `json:"originalPhrase"`
	CorrectedPhrase      string               `json:"correctedPhrase"`
}

// ReplaceWholePhrase replaces every occurrence of phrase in text with
// replacement and says how many it replaced. An occurrence must start and end
// on a word boundary: a phrase edge that is a letter or a digit may not touch
// another letter or digit, so "node" is not found inside "nodes". Any run of
// white space in the text matches the single space between the words of the
// phrase, because the model that proposed the phrase read the text with its
// white space collapsed. The match is otherwise exact, including case.
func ReplaceWholePhrase(text, phrase, replacement string) (string, int) {
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return text, 0
	}
	for index := range words {
		words[index] = regexp.QuoteMeta(words[index])
	}
	pattern := regexp.MustCompile(strings.Join(words, `\s+`))
	phraseRunes := []rune(strings.Join(strings.Fields(phrase), " "))
	needsStartBoundary := isWordRune(phraseRunes[0])
	needsEndBoundary := isWordRune(phraseRunes[len(phraseRunes)-1])
	var result strings.Builder
	count, copied := 0, 0
	for _, match := range pattern.FindAllStringIndex(text, -1) {
		if needsStartBoundary {
			if before, size := utf8.DecodeLastRuneInString(text[:match[0]]); size > 0 && isWordRune(before) {
				continue
			}
		}
		if needsEndBoundary {
			if after, size := utf8.DecodeRuneInString(text[match[1]:]); size > 0 && isWordRune(after) {
				continue
			}
		}
		result.WriteString(text[copied:match[0]])
		result.WriteString(replacement)
		copied = match[1]
		count++
	}
	if count == 0 {
		return text, 0
	}
	result.WriteString(text[copied:])
	return result.String(), count
}

func isWordRune(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsDigit(character)
}
