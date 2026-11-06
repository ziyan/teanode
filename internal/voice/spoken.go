package voice

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Reading a message aloud: what is written for the eye, made into what can be
// said, in pieces small enough to start soon. Both clients play the pieces in
// turn, asking for the next while one plays, so the pieces are cut here.

const (
	// firstPieceBytes bounds the first piece: it is what the person waits for.
	firstPieceBytes = 240

	// pieceBytes bounds the pieces after it, which are spoken while the one
	// before plays.
	pieceBytes = 1500
)

var (
	spokenCodeFence  = regexp.MustCompile("(?m)^[ \\t]*```.*$")
	spokenImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	spokenLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	spokenAddress    = regexp.MustCompile(`https?://\S+`)
	spokenHeading    = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	spokenBullet     = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]+`)
	spokenQuote      = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)
	spokenRule       = regexp.MustCompile(`(?m)^[ \t]*(?:[-*_][ \t]*){3,}$`)
	spokenTableRule  = regexp.MustCompile(`(?m)^[ \t]*\|?[ \t:|-]*-[ \t:|-]*\|?[ \t]*$\n?`)
	spokenEmphasis   = regexp.MustCompile("\\*\\*|__|`|~~")
	spokenLoneStar   = regexp.MustCompile(`(^|\s)\*(\S[^*\n]*\S|\S)\*(\s|$|[.,;:!?])`)
	spokenSpaces     = regexp.MustCompile(`[ \t]+`)
	spokenBlankLines = regexp.MustCompile(`\n\s*\n+`)
	spokenSentence   = regexp.MustCompile(`[^.!?。！？\n]*(?:[.!?。！？]+["')\]”’）」]*\s*|\n+|$)`)
)

// SpokenText is written text as it is said: the words of a link and not its
// address, no markers of emphasis, a table's cells one after another.
func SpokenText(text string) string {
	text = spokenCodeFence.ReplaceAllString(text, "")
	text = spokenImage.ReplaceAllString(text, "$1")
	text = spokenLink.ReplaceAllString(text, "$1")
	text = spokenAddress.ReplaceAllString(text, "")
	text = spokenTableRule.ReplaceAllString(text, "")
	text = spokenRule.ReplaceAllString(text, "")
	text = spokenHeading.ReplaceAllString(text, "")
	text = spokenBullet.ReplaceAllString(text, "")
	text = spokenQuote.ReplaceAllString(text, "")
	text = spokenEmphasis.ReplaceAllString(text, "")
	text = spokenLoneStar.ReplaceAllString(text, "$1$2$3")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			for cell := range cells {
				cells[cell] = strings.TrimSpace(cells[cell])
			}
			line = strings.Join(cells, ", ")
		}
		lines[index] = strings.TrimSpace(spokenSpaces.ReplaceAllString(line, " "))
	}
	return strings.TrimSpace(spokenBlankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// SpokenPieces is text cut for reading aloud: on sentences, a short piece
// first, and none longer than Speak takes.
func SpokenPieces(text string) []string {
	var pieces []string
	var piece strings.Builder
	finish := func() {
		if said := strings.TrimSpace(piece.String()); said != "" {
			pieces = append(pieces, said)
		}
		piece.Reset()
	}
	limit := func() int {
		if len(pieces) == 0 {
			return firstPieceBytes
		}
		return pieceBytes
	}
	for _, sentence := range spokenSentence.FindAllString(SpokenText(text), -1) {
		// A sentence longer than a piece is cut where a word ends.
		for len(sentence) > pieceBytes {
			finish()
			cut := cutAtWord(sentence, pieceBytes)
			pieces = append(pieces, strings.TrimSpace(sentence[:cut]))
			sentence = sentence[cut:]
		}
		if piece.Len() > 0 && piece.Len()+len(sentence) > limit() {
			finish()
		}
		piece.WriteString(sentence)
	}
	finish()
	return pieces
}

// cutAtWord is where to cut text at or before most bytes: after a space, or
// where a character ends when there are no spaces, as in Chinese.
func cutAtWord(text string, most int) int {
	if space := strings.LastIndexAny(text[:most], " \n"); space > most/2 {
		return space + 1
	}
	cut := most
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return cut
}
