package models

import (
	"regexp"
	"strings"
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

// linkPattern is a Markdown link or picture, [words](target) or
// ![words](target).
var linkPattern = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

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
// notification, a title, a line in a list. What is hidden is gone, a link
// or a picture keeps its words without its target, and bold and code marks
// are dropped. Line breaks are kept.
func PlainText(text string) string {
	text = StripHiddenMarkers(text)
	text = linkPattern.ReplaceAllString(text, "$1")
	for _, mark := range []string{"**", "__", "`"} {
		text = strings.ReplaceAll(text, mark, "")
	}
	return strings.TrimSpace(text)
}
