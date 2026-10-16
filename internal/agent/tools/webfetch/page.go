package webfetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// renderedBytes bounds the page a browser built, as it is read back.
const renderedBytes = 4 << 20

// mostMedia is how many media addresses a page lists: enough for a gallery,
// short of a page of thumbnails crowding out its text.
const mostMedia = 40

// thinTextCharacters is how little text a page may have before it is taken
// for a page its scripts build: a title and a line is what such a page sends.
const thinTextCharacters = 200

// pageMedia is one video, sound or picture on a page.
type pageMedia struct {
	Kind        string `json:"kind"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// readPage is what a page says: its title, its text, its media, and whether
// it runs scripts.
type readPage struct {
	title      string
	text       string
	media      []pageMedia
	hasScripts bool
}

// readHTML reads a page's HTML, with each media address made whole against
// base, the address the page came from.
func readHTML(page string, base *url.URL) readPage {
	read := readPage{text: tools.NormalizeText(tools.HTMLToText(page))}
	document, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return read
	}
	read.title = strings.TrimSpace(document.Find("title").First().Text())
	read.hasScripts = document.Find("script").Length() > 0
	seen := map[string]bool{}
	add := func(kind, address, description string) {
		if len(read.media) >= mostMedia {
			return
		}
		whole := wholeAddress(base, address)
		if whole == "" || seen[whole] {
			return
		}
		seen[whole] = true
		if kind == "" {
			kind = kindByExtension(whole)
		}
		read.media = append(read.media, pageMedia{Kind: kind, URL: whole, Description: strings.TrimSpace(description)})
	}
	// What the page says it is about first: a preview's video and picture
	// are the ones it means, ahead of the decoration in its body.
	document.Find("meta").Each(func(_ int, meta *goquery.Selection) {
		name := strings.ToLower(meta.AttrOr("property", meta.AttrOr("name", "")))
		content := meta.AttrOr("content", "")
		switch name {
		case "og:video", "og:video:url", "og:video:secure_url", "twitter:player:stream":
			add("video", content, "the page's preview video")
		case "og:audio", "og:audio:url", "og:audio:secure_url":
			add("audio", content, "the page's preview sound")
		case "og:image", "og:image:url", "og:image:secure_url", "twitter:image":
			add("picture", content, "the page's preview picture")
		}
	})
	document.Find("video, audio").Each(func(_ int, player *goquery.Selection) {
		kind := "video"
		if goquery.NodeName(player) == "audio" {
			kind = "audio"
		}
		description := player.AttrOr("aria-label", player.AttrOr("title", ""))
		add(kind, player.AttrOr("src", ""), description)
		player.Find("source").Each(func(_ int, source *goquery.Selection) {
			add(kind, source.AttrOr("src", ""), description)
		})
	})
	document.Find("img").Each(func(_ int, picture *goquery.Selection) {
		add("picture", picture.AttrOr("src", ""), picture.AttrOr("alt", ""))
	})
	// A link straight to a file is media too, such as a download of the video.
	document.Find("a[href]").Each(func(_ int, link *goquery.Selection) {
		address := link.AttrOr("href", "")
		if kindByExtension(wholeAddress(base, address)) != "" {
			add("", address, link.Text())
		}
	})
	return read
}

// wholeAddress is address made whole against base, or empty for one that
// is not on the web: a data: picture, a blob of the page's own, a fragment.
func wholeAddress(base *url.URL, address string) string {
	address = strings.TrimSpace(address)
	if address == "" || strings.HasPrefix(address, "#") {
		return ""
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

// kindByExtension is what a file's name says it is, or empty for a page.
func kindByExtension(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".mp4", ".webm", ".mov", ".m4v", ".m3u8":
		return "video"
	case ".mp3", ".m4a", ".wav", ".ogg", ".oga", ".flac", ".aac", ".opus":
		return "audio"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg":
		return "picture"
	}
	return ""
}

// wantsRendering says whether a page is run in a browser: when asked to,
// or, left to choose, when it is scripts and next to no text, which is what
// a page that builds itself sends before its scripts run.
func wantsRendering(render string, read readPage) bool {
	switch render {
	case "always":
		return true
	case "never":
		return false
	}
	return read.hasScripts && utf8.RuneCountInString(read.text) < thinTextCharacters
}

// asSentNote is what to say of a page read as sent that runs scripts: they
// may add more than it sent, a video, a gallery, the rest of an article.
// Empty when it runs none, or when asked not to run them.
func asSentNote(render string, read readPage) string {
	if !read.hasScripts || render == "never" {
		return ""
	}
	return "read as the server sent it; the page runs scripts, and render always reads what they add, such as a video"
}

// renderPage opens an address in a page of its own in the server's
// headless browser, waits for its scripts to settle, and is the HTML they
// built and the address it ended on.
func renderPage(ctx context.Context, address string) (string, string, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return "", "", err
	}
	rendering, ok := run.(tools.Rendering)
	if !ok {
		return "", "", fmt.Errorf("this run has no browser")
	}
	page, closePage, err := rendering.RenderingPage(ctx)
	if err != nil {
		return "", "", err
	}
	defer closePage()
	location, err := page.Navigate(ctx, address)
	if err != nil {
		return "", "", err
	}
	// Load is when the scripts start; what they fetch comes after. Waited
	// for briefly: a page that never stops fetching is read as it is.
	_, _ = page.Wait(ctx, "network_idle", "", 8*time.Second)
	encoded, err := page.Evaluate(ctx, "document.documentElement.outerHTML", 0)
	if err != nil {
		return "", "", err
	}
	var built string
	if err := json.Unmarshal([]byte(encoded), &built); err != nil {
		return "", "", fmt.Errorf("the page could not be read back: %w", err)
	}
	if len(built) > renderedBytes {
		built = built[:renderedBytes]
	}
	finalAddress := address
	if location != nil && location.URL != "" {
		finalAddress = location.URL
	}
	return built, finalAddress, nil
}
