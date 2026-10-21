// Package linkpreview reads what a web page says about itself, for the card
// shown under a message that holds a link: its title, a line about it, the
// name of its site and a picture.
//
// The page is fetched by this server and not by whoever is reading, so the
// page's site learns that this server looked and nothing about who was
// reading or which conversation the link was in. The same goes for the
// picture, which the dashboard and the app load from this server too. Every
// fetch goes through safefetch, which refuses addresses inside the network
// this server sits in.
package linkpreview

import (
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

const (
	titleLength       = 200
	descriptionLength = 500
	siteNameLength    = 100
)

// Page is what a page says about itself. Every field may be empty.
type Page struct {
	Title       string
	Description string
	SiteName    string

	// ImageURL is the address of the page's picture, made whole against the
	// address the page was fetched from.
	ImageURL string
}

// HasWords says the page told something worth a card: a title or a
// description. A page with only a picture is not previewed.
func (self *Page) HasWords() bool {
	return self.Title != "" || self.Description != ""
}

// Parse reads a page's HTML. Open Graph tags come first, then Twitter's, then
// the page's own title and description. base is the address the page came
// from, which a relative picture address is read against.
func Parse(page io.Reader, base *url.URL) (*Page, error) {
	document, err := goquery.NewDocumentFromReader(page)
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	document.Find("meta").Each(func(_ int, meta *goquery.Selection) {
		name := strings.ToLower(strings.TrimSpace(meta.AttrOr("property", meta.AttrOr("name", ""))))
		content := tidy(meta.AttrOr("content", ""))
		if name == "" || content == "" {
			return
		}
		// The first of a name wins: a page repeats og:image to list more
		// than one, best first.
		if _, isKnown := tags[name]; !isKnown {
			tags[name] = content
		}
	})
	first := func(names ...string) string {
		for _, name := range names {
			if content := tags[name]; content != "" {
				return content
			}
		}
		return ""
	}
	found := &Page{
		Title:       first("og:title", "twitter:title"),
		Description: first("og:description", "twitter:description", "description"),
		SiteName:    first("og:site_name"),
		ImageURL:    wholeImageAddress(base, first("og:image", "og:image:url", "og:image:secure_url", "twitter:image", "twitter:image:src")),
	}
	if found.Title == "" {
		found.Title = tidy(document.Find("title").First().Text())
	}
	found.Title = cut(found.Title, titleLength)
	found.Description = cut(found.Description, descriptionLength)
	found.SiteName = cut(found.SiteName, siteNameLength)
	return found, nil
}

// wholeImageAddress is address read against base, or empty when it is not an
// address on the web (a data: picture, a path with no page to be relative to).
func wholeImageAddress(base *url.URL, address string) string {
	if address == "" {
		return ""
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

// tidy is text with each run of white space made one space and anything that
// is not meant to be seen taken out, so that a page cannot put a line break or
// a control character into a card.
func tidy(text string) string {
	text = strings.Map(func(character rune) rune {
		switch {
		case unicode.IsSpace(character):
			return ' '
		case unicode.IsControl(character), character == utf8.RuneError:
			return -1
		}
		return character
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

// cut is text of at most limit characters, ending in an ellipsis where it was
// cut.
func cut(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return strings.TrimSpace(string(characters[:limit-1])) + "…"
}
