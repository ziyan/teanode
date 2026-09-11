package tools

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Text as a tool hands it to the model: HTML reduced to what a person
// would read, line endings squared, the quoted history of a reply cut.

// HTMLToText reduces HTML to the text a person would read: block elements
// become line breaks, links keep their text, everything in a script or a
// style goes.
func HTMLToText(html string) string {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return stripTags(html)
	}
	document.Find("script, style, head, noscript").Remove()
	document.Find("br").Each(func(_ int, selection *goquery.Selection) { selection.ReplaceWithHtml("\n") })
	document.Find("p, div, li, tr, h1, h2, h3, h4, h5, h6, blockquote, pre, table").Each(func(_ int, selection *goquery.Selection) {
		selection.AppendHtml("\n")
		selection.PrependHtml("\n")
	})
	return document.Text()
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(html string) string {
	return tagPattern.ReplaceAllString(html, " ")
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// NormalizeText squares the line endings, trims trailing blanks and
// collapses runs of blank lines.
func NormalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

var quoteIntroductions = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^On .{5,200}wrote:\s*$`),
	regexp.MustCompile(`(?im)^-{2,}\s*Original Message\s*-{2,}\s*$`),
	regexp.MustCompile(`(?im)^From: .+\nSent: .+`),
	regexp.MustCompile(`(?im)^Le .{5,200}a écrit\s*:\s*$`),
	regexp.MustCompile(`(?im)^Am .{5,200}schrieb .+:\s*$`),
}

// StripQuoted cuts the quoted history: everything from the first line that
// introduces it, and any line quoted with ">" before that.
func StripQuoted(text string) string {
	cut := len(text)
	for _, pattern := range quoteIntroductions {
		if location := pattern.FindStringIndex(text); location != nil && location[0] < cut {
			cut = location[0]
		}
	}
	text = text[:cut]
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
