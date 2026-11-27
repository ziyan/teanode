package models

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// An answer is Markdown written for the dashboard and the app, which read
// the hidden markers in it and draw the rest. Anywhere else it is text a
// person reads as it is, or hears, so what is hidden has to come off first,
// and, where nothing draws Markdown, the Markdown too.

// hiddenMarkerPattern matches, in order of preference, a fenced code block,
// a code span, and an HTML comment, whole or cut off by the end of the
// text. Code is matched so that a comment shown as an example inside it is
// left alone.
var hiddenMarkerPattern = regexp.MustCompile("(?s)```.*?(?:```|$)|`[^`\n]*`|<!--.*?(?:-->|$)")

// StripHiddenMarkers takes everything off an answer that only the dashboard
// and the app read: the suggested replies it ends with, and any other HTML
// comment, including one still being written. For a surface that draws
// Markdown but not these, such as a chat app, a mail or a terminal.
func StripHiddenMarkers(text string) string {
	text = StripSuggestedReplies(text)
	text = hiddenMarkerPattern.ReplaceAllStringFunc(text, func(found string) string {
		if strings.HasPrefix(found, "<!--") {
			return ""
		}
		return found
	})
	return strings.TrimRight(text, " \n")
}

// PlainText is an answer as a surface that draws no Markdown shows it: a
// notification, a title, a line in a list. What is hidden is gone and no
// Markdown mark is left: a heading, a list item, a table row, a quote, a
// rule, a fence line, emphasis, strikethrough, a code span, a link's target,
// an HTML tag or an escape. A picture reads as its alt text, a list item as
// its words, a table row as its cells joined by " · ". The code inside a
// fence stays as text. Lines are kept; a caller that wants one line joins
// them, and a caller that cuts does so after this, so a cut never lands
// inside syntax.
func PlainText(text string) string {
	text = StripHiddenMarkers(text)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	converted := make([]string, 0, len(lines))
	isInFence := false
	isInTable := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			isInFence = !isInFence
			continue
		}
		if isInFence {
			converted = append(converted, line)
			continue
		}
		if trimmed == "" {
			isInTable = false
			converted = append(converted, "")
			continue
		}
		if ruleLinePattern.MatchString(trimmed) || setextLinePattern.MatchString(trimmed) {
			continue
		}
		isSeparator := isTableSeparator(trimmed)
		isNextSeparator := index+1 < len(lines) && isTableSeparator(strings.TrimSpace(lines[index+1]))
		if isSeparator && isInTable {
			continue
		}
		if strings.Contains(trimmed, "|") && (strings.HasPrefix(trimmed, "|") || isInTable || isNextSeparator) {
			isInTable = true
			if isSeparator {
				continue
			}
			var cells []string
			for _, cell := range strings.Split(trimmed, "|") {
				if cell = strings.TrimSpace(cell); cell != "" {
					cells = append(cells, plainInline(cell))
				}
			}
			if len(cells) > 0 {
				converted = append(converted, strings.Join(cells, " · "))
			}
			continue
		}
		isInTable = false
		for strings.HasPrefix(trimmed, ">") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		}
		trimmed = headingPattern.ReplaceAllString(trimmed, "")
		if found := listItemPattern.FindString(trimmed); found != "" {
			trimmed = strings.TrimPrefix(trimmed, found)
			trimmed = taskBoxPattern.ReplaceAllString(trimmed, "")
		}
		converted = append(converted, plainInline(trimmed))
	}
	return strings.TrimSpace(strings.Join(converted, "\n"))
}

var (
	// ruleLinePattern is a horizontal rule: three or more of one mark, with
	// spaces allowed between.
	ruleLinePattern = regexp.MustCompile(`^(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)

	// setextLinePattern is the line of = under a heading. The line of - is a
	// rule already.
	setextLinePattern = regexp.MustCompile(`^=+$`)

	tableSeparatorPattern = regexp.MustCompile(`^\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?$`)

	// headingPattern is the marks in front of a heading, and the ones that
	// may close it.
	headingPattern = regexp.MustCompile(`^#{1,6}(?:\s+|$)|\s+#+\s*$`)

	// listItemPattern is the bullet or number in front of a list item, which
	// a list item needs a space after: -5 and 3.5 are not lists.
	listItemPattern = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])\s+`)

	taskBoxPattern = regexp.MustCompile(`^\[[ xX]\]\s+`)

	// linkPattern is a Markdown link or picture, [words](target) or
	// ![words](target): both read as their words.
	linkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

	autolinkPattern  = regexp.MustCompile(`<((?:https?://|mailto:)[^>\s]*)>`)
	lineBreakPattern = regexp.MustCompile(`(?i)<br\s*/?>`)
	tagPattern       = regexp.MustCompile(`</?[A-Za-z][^<>\n]*>`)

	// emphasisPattern is a pair of bold marks or of strikethrough marks
	// around words. Only a pair is taken off: a name such as __init__ or
	// a**b has marks that are not Markdown, and dropping every one
	// corrupted it. Single marks and underscores are read by removePairs.
	emphasisPattern = regexp.MustCompile("\\*\\*([^*\\s](?:[^*\n]*[^*\\s])?)\\*\\*|~~([^~\\s](?:[^~\n]*[^~\\s])?)~~")

	// codeSpanPattern and escapePattern are taken out of a line before its
	// emphasis is read, so the marks inside a span and an escaped mark are
	// not mistaken for Markdown.
	codeSpanPattern = regexp.MustCompile("`([^`\n]+)`")
	escapePattern   = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+\\-.!|~<>&])")
)

// isTableSeparator is the |---|:---:| row under a table's header.
func isTableSeparator(line string) bool {
	return strings.Contains(line, "-") && strings.Contains(line, "|") && tableSeparatorPattern.MatchString(line)
}

// placeholderMark brackets the number of a piece of a line set aside while
// its emphasis is read. It is a control character no answer holds.
const placeholderMark = "\x00"

// plainInline is one line of prose without its inline marks.
func plainInline(line string) string {
	var keptPieces []string
	keep := func(piece string) string {
		keptPieces = append(keptPieces, piece)
		return placeholderMark + string(rune('A'+len(keptPieces)-1)) + placeholderMark
	}
	line = strings.ReplaceAll(line, placeholderMark, "")
	line = escapePattern.ReplaceAllStringFunc(line, func(found string) string { return keep(found[1:]) })
	line = codeSpanPattern.ReplaceAllStringFunc(line, func(found string) string { return keep(found[1 : len(found)-1]) })
	line = linkPattern.ReplaceAllString(line, "$1")
	line = autolinkPattern.ReplaceAllString(line, "$1")
	line = lineBreakPattern.ReplaceAllString(line, " ")
	line = tagPattern.ReplaceAllString(line, "")
	line = html.UnescapeString(line)
	line = emphasisPattern.ReplaceAllString(line, "$1$2")
	line = removePairs(line, "__", true)
	line = removePairs(line, "*", true)
	line = removePairs(line, "_", true)
	if len(keptPieces) > 0 {
		for index := len(keptPieces) - 1; index >= 0; index-- {
			line = strings.ReplaceAll(line, placeholderMark+string(rune('A'+index))+placeholderMark, keptPieces[index])
		}
	}
	return strings.TrimSpace(line)
}

func isEmphasisWordRune(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

// removePairs takes a mark off the words it surrounds, only when it comes in
// a pair: an opening mark that is followed by a word and a closing mark that
// follows one. A mark alone, or with a space inside it, is not Markdown, and
// dropping every one corrupted names such as a*b and 2 * 3 * 4. With
// isBounded, the pair also may not touch a letter or digit outside it, which
// keeps snake_case and 2*3*4 as typed, and a lowercase word between two
// underscores as __init__ is written is left as it is, since it is a name
// before it is bold.
func removePairs(line, mark string, isBounded bool) string {
	runes := []rune(line)
	markRunes := []rune(mark)
	var result []rune
	for position := 0; position < len(runes); {
		if !hasPrefixAt(runes, position, markRunes) {
			result = append(result, runes[position])
			position++
			continue
		}
		wordStart := position + len(markRunes)
		isOpening := wordStart < len(runes) && !unicode.IsSpace(runes[wordStart]) && runes[wordStart] != markRunes[0]
		if isOpening && isBounded && position > 0 && isEmphasisWordRune(runes[position-1]) {
			isOpening = false
		}
		if isOpening {
			if closing := findClosing(runes, wordStart, markRunes, isBounded); closing >= 0 {
				inside := string(runes[wordStart:closing])
				if mark == "__" && isLowercaseName(inside) {
					result = append(result, runes[position:closing+len(markRunes)]...)
				} else {
					result = append(result, []rune(inside)...)
				}
				position = closing + len(markRunes)
				continue
			}
		}
		result = append(result, markRunes...)
		position += len(markRunes)
	}
	return string(result)
}

func hasPrefixAt(runes []rune, position int, prefix []rune) bool {
	if position+len(prefix) > len(runes) {
		return false
	}
	for offset, character := range prefix {
		if runes[position+offset] != character {
			return false
		}
	}
	return true
}

// findClosing is where the mark that closes a pair is, or -1: one that
// follows a word, and with isBounded does not lead into one.
func findClosing(runes []rune, from int, markRunes []rune, isBounded bool) int {
	for position := from + 1; position+len(markRunes) <= len(runes); position++ {
		if !hasPrefixAt(runes, position, markRunes) || unicode.IsSpace(runes[position-1]) || runes[position-1] == markRunes[0] {
			continue
		}
		after := position + len(markRunes)
		if after < len(runes) && runes[after] == markRunes[0] {
			continue
		}
		if isBounded && after < len(runes) && isEmphasisWordRune(runes[after]) {
			continue
		}
		return position
	}
	return -1
}

func isLowercaseName(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if !unicode.IsLower(character) && !unicode.IsDigit(character) && character != '_' {
			return false
		}
	}
	return true
}
