package telegram

import (
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Telegram's Markdown is its older one: *bold*, _italic_, `code`, fenced
// code and [text](url), and nothing else. The model writes the Markdown
// every other surface reads, so it is turned into this one on the way out;
// what Telegram still refuses is sent as plain text by the client.
var (
	telegramBold          = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	telegramUnderlineBold = regexp.MustCompile(`__([^_\n]+)__`)
	telegramStrike        = regexp.MustCompile(`~~([^~\n]+)~~`)
	telegramHeading       = regexp.MustCompile(`(?m)^#{1,6}[ \t]+(.+?)[ \t#]*$`)
	telegramStarBullet    = regexp.MustCompile(`(?m)^([ \t]*)\*[ \t]+`)
)

// telegramMarkdown is text written in common Markdown as Telegram shows
// it: bold with one star, a heading as a bold line, a star bullet as a
// dot, and a cited message as its subject. Code is left exactly as it is.
func telegramMarkdown(text string) string {
	text = models.UnlinkMailCitations(text)
	var builder strings.Builder
	for index, part := range strings.Split(text, "```") {
		if index > 0 {
			builder.WriteString("```")
		}
		// The odd parts are inside a fence.
		if index%2 == 1 {
			builder.WriteString(part)
			continue
		}
		for spanIndex, span := range strings.Split(part, "`") {
			if spanIndex > 0 {
				builder.WriteString("`")
			}
			if spanIndex%2 == 1 {
				builder.WriteString(span)
				continue
			}
			builder.WriteString(telegramProse(span))
		}
	}
	return builder.String()
}

// telegramProse turns the Markdown of text outside code.
func telegramProse(text string) string {
	text = telegramStarBullet.ReplaceAllString(text, "$1• ")
	text = telegramBold.ReplaceAllString(text, "*$1*")
	text = telegramUnderlineBold.ReplaceAllString(text, "*$1*")
	text = telegramStrike.ReplaceAllString(text, "$1")
	text = telegramHeading.ReplaceAllStringFunc(text, func(line string) string {
		title := strings.Trim(telegramHeading.FindStringSubmatch(line)[1], "*")
		return "*" + title + "*"
	})
	return text
}
