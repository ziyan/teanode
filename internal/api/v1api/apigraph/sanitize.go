package apigraph

import (
	"io"
	"mime/quotedprintable"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// attribute and commentNodeType alias the html package's types so the
// sanitizer reads without repeating the import everywhere.
type attribute = html.Attribute

const commentNodeType = html.CommentNode

func quotedPrintableReader(reader io.Reader) io.Reader {
	return quotedprintable.NewReader(reader)
}

// blockedPlaceholder replaces a remote image source. The original is kept in a
// data attribute so the dashboard can offer to load it.
const blockedPlaceholder = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

// removedElements are dropped entirely, with their contents.
//
// This is a deny list applied on top of an allow list of attributes below.
// Mail is HTML written by strangers, and the dashboard renders it, so the
// question is not what looks harmful but what could act on the reader.
var removedElements = []string{
	"script", "iframe", "frame", "frameset", "object", "embed",
	"applet", "form", "input", "button", "select", "textarea", "link", "meta",
	"base", "svg", "math",
}

// allowedAttributes survive on any element. Everything else is removed, which
// covers every on* event handler without having to enumerate them.
var allowedAttributes = map[string]bool{
	"href": true, "src": true, "alt": true, "title": true, "width": true,
	"height": true, "align": true, "valign": true, "border": true,
	"cellpadding": true, "cellspacing": true, "colspan": true, "rowspan": true,
	"bgcolor": true, "color": true, "face": true, "size": true, "dir": true,
	"class": true, "id": true, "target": true, "type": true, "start": true,
	// Kept, and sanitised below. Removing it while keeping class and id
	// produced the worst of both: a message arrived with its whole skeleton
	// of nested tables intact and not one rule that made it a layout, so it
	// rendered as a column of fragments. CSS cannot execute anything, and the
	// frame's own policy is what stops it fetching anything.
	"style": true,
}

// cssThreats are the constructs through which CSS has historically reached
// outside itself: two of them script in old engines, one of them fetches.
//
// A declaration containing any of these is dropped. This is belt to the
// frame's braces — its policy is default-src 'none' with no source for a
// stylesheet, so an @import has nowhere to go — but a rule that cannot work
// should not be shipped to the browser to be refused.
var cssThreats = []string{"expression(", "javascript:", "behavior:", "-moz-binding", "@import"}

// sanitizeHTML makes a message body safe to render, and reports whether it
// referred to remote content.
//
// Three things are being prevented. Script execution, by removing the elements
// and attributes that can carry it. Tracking, by refusing to load remote
// images, which is how a sender learns that a message was opened and from
// where. And navigation of the dashboard itself, by forcing links to open
// elsewhere and stripping anything that is not an ordinary URL.
//
// The result is still rendered inside a sandboxed frame by the dashboard. This
// is the inner of two layers, not the only one.
func sanitizeHTML(input string) (string, bool) {
	var hasRemoteContent bool
	var hasRemoteStyle bool

	document, err := goquery.NewDocumentFromReader(strings.NewReader(input))
	if err != nil {
		// If it cannot be parsed it cannot be made safe, so show nothing
		// rather than something unchecked.
		log.Warningf("failed to parse an HTML part, not displaying it: %s", err)
		return "", false
	}

	for _, name := range removedElements {
		document.Find(name).Remove()
	}

	// A <style> block stays, with its contents sanitised. Mail from anything
	// that composes HTML for a living puts its layout here and refers to it
	// by class; dropping it leaves the classes pointing at nothing.
	document.Find("style").Each(func(_ int, selection *goquery.Selection) {
		cleaned, remote := sanitizeCSS(selection.Text())
		if remote {
			hasRemoteStyle = true
		}
		selection.SetText(cleaned)
	})

	// Comments can hide markup that a lenient parser later resurrects.
	document.Find("*").Contents().Each(func(_ int, selection *goquery.Selection) {
		for _, node := range selection.Nodes {
			if node.Type == commentNodeType {
				selection.Remove()
			}
		}
	})

	document.Find("*").Each(func(_ int, selection *goquery.Selection) {
		node := selection.Nodes[0]

		var kept []attribute
		for _, existing := range node.Attr {
			key := strings.ToLower(existing.Key)
			if !allowedAttributes[key] {
				continue
			}
			value := strings.TrimSpace(existing.Val)

			switch key {
			case "src":
				// Remote images are the tracking pixel problem. Embedded ones
				// referenced by cid: are part of the message itself.
				if isRemoteURL(value) {
					hasRemoteContent = true
					kept = append(kept,
						attribute{Key: "data-blocked-src", Val: value},
						attribute{Key: "src", Val: blockedPlaceholder})
					continue
				}
				if !isSafeURL(value) {
					continue
				}
			case "href":
				if !isSafeURL(value) {
					continue
				}
			case "style":
				cleaned, remote := sanitizeCSS(value)
				if remote {
					hasRemoteContent = true
				}
				if strings.TrimSpace(cleaned) == "" {
					continue
				}
				value = cleaned
			}
			kept = append(kept, attribute{Key: key, Val: value})
		}
		node.Attr = kept
	})

	// Any link that survives opens in a new tab, and without handing the
	// destination a reference back to the dashboard.
	document.Find("a[href]").Each(func(_ int, selection *goquery.Selection) {
		selection.SetAttr("target", "_blank")
		selection.SetAttr("rel", "noopener noreferrer nofollow")
	})

	// Only the body is rendered, and mail almost always puts its stylesheet in
	// the head — so a sheet that survived sanitising would be dropped here
	// instead, which is the same broken layout by a different route. Move it
	// where it will be kept.
	document.Find("head style").Each(func(_ int, selection *goquery.Selection) {
		html, err := goquery.OuterHtml(selection)
		if err != nil {
			return
		}
		document.Find("body").PrependHtml(html)
		selection.Remove()
	})

	rendered, err := document.Find("body").Html()
	if err != nil {
		log.Warningf("failed to render an HTML part, not displaying it: %s", err)
		return "", hasRemoteContent || hasRemoteStyle
	}
	return rendered, hasRemoteContent || hasRemoteStyle
}

// isSafeURL rejects anything that is not an ordinary link. javascript: and
// data: are the two that execute or impersonate.
func isSafeURL(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	// Control characters are stripped by parsers in inconsistent ways and are
	// a classic way to smuggle a scheme past a check.
	trimmed = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, trimmed)

	for _, scheme := range []string{"javascript:", "vbscript:", "data:", "file:", "about:"} {
		if strings.HasPrefix(trimmed, scheme) {
			return false
		}
	}
	return true
}

func isRemoteURL(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://") ||
		strings.HasPrefix(trimmed, "//")
}

// sanitizeCSS removes the constructs in cssThreats, and reports whether what
// is left refers to something remote.
//
// Declaration by declaration rather than by parsing the sheet: a CSS parser is
// a dependency and a source of its own bugs, and the question here is narrow
// enough to answer by looking. Anything unrecognised is kept — CSS a browser
// does not understand is ignored by the browser, so the failure mode of being
// too permissive about properties is a rule that does nothing.
func sanitizeCSS(input string) (string, bool) {
	if input == "" {
		return "", false
	}

	lower := strings.ToLower(input)
	remote := strings.Contains(lower, "url(http") || strings.Contains(lower, "url('http") ||
		strings.Contains(lower, `url("http`)

	// Nothing to strip is the common case, and it should not cost a rebuild
	// of the string.
	dangerous := false
	for _, threat := range cssThreats {
		if strings.Contains(lower, threat) {
			dangerous = true
			break
		}
	}
	if !dangerous {
		return input, remote
	}

	// Split on the boundaries that end a declaration or a rule, keeping the
	// separators, so what is rebuilt is still the same sheet minus the pieces
	// that were removed.
	var builder strings.Builder
	for _, piece := range splitKeepingSeparators(input, ";{}") {
		if containsAny(strings.ToLower(piece), cssThreats) {
			continue
		}
		builder.WriteString(piece)
	}
	return builder.String(), remote
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// splitKeepingSeparators cuts a string after each of the given characters,
// so that reassembling the pieces reproduces the original exactly.
func splitKeepingSeparators(value string, separators string) []string {
	var pieces []string
	start := 0
	for index, letter := range value {
		if strings.ContainsRune(separators, letter) {
			pieces = append(pieces, value[start:index+1])
			start = index + 1
		}
	}
	if start < len(value) {
		pieces = append(pieces, value[start:])
	}
	return pieces
}
