package apimedia

import (
	"net/http"
	"strings"
	"testing"
)

// What a picture is decided by, which is the bytes and not the name.
//
// The one that matters is the last: a file called logo.png whose content is
// HTML would, if believed, be served as a document over HTTPS from the
// operator's own domain, where a script in it would run with that origin.
func TestDetectContentType(t *testing.T) {
	t.Parallel()

	png := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 32)...)
	gif := append([]byte("GIF89a"), make([]byte, 32)...)
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 32)...)
	webp := append(append([]byte("RIFF"), 0, 0, 0, 0), []byte("WEBPVP8 ")...)

	tests := []struct {
		name    string
		content []byte
		want    string
		allowed bool
	}{
		{"a png", png, "image/png", true},
		{"a gif", gif, "image/gif", true},
		{"a jpeg", jpeg, "image/jpeg", true},
		{"a webp", webp, "image/webp", true},
		{"html calling itself an image", []byte("<!DOCTYPE html><html><body>hi"), "text/html", false},
		{"an svg, which is a document that can carry script",
			[]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), "text/xml", false},
		{"a script", []byte("#!/bin/sh\necho hello\n"), "text/plain", false},
		{"a pdf", []byte("%PDF-1.7\n%oeuf\n"), "application/pdf", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := detectContentType(test.content)
			if !strings.HasPrefix(got, test.want) {
				t.Errorf("detectContentType = %q, want %q", got, test.want)
			}
			if displayable[got] != test.allowed {
				t.Errorf("%q is allowed = %v, want %v", got, displayable[got], test.allowed)
			}
		})
	}
}

// The name is echoed in a Content-Disposition header, so it may not be a path,
// a quote, or a control character that would end the header early.
func TestFilename(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"logo.png":                  "logo.png",
		"  spaced.png  ":            "spaced.png",
		"../../etc/passwd":          "passwd",
		`C:\Users\someone\logo.png`: "logo.png",
		"quote\".png":               "quote.png",
		"break\r\nX-Evil: 1":        "breakX-Evil: 1",
		"":                          "image",
		".":                         "image",
		"..":                        "image",
	}
	for value, want := range tests {
		if got := filename(value); got != want {
			t.Errorf("filename(%q) = %q, want %q", value, got, want)
		}
	}

	// And it cannot grow without bound, because it goes in a header.
	if got := filename(strings.Repeat("a", 400)); len(got) != 128 {
		t.Errorf("a long name came back %d characters, want it trimmed to 128", len(got))
	}
}

// The allow list is the security boundary, so it is written down here too: a
// change to it should have to change this test.
func TestOnlyPicturesAreServed(t *testing.T) {
	t.Parallel()

	for _, contentType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !displayable[contentType] {
			t.Errorf("%s should be allowed", contentType)
		}
	}
	for _, contentType := range []string{
		"image/svg+xml", "text/html", "application/pdf", "application/javascript",
		"text/xml", "application/octet-stream", "",
	} {
		if displayable[contentType] {
			t.Errorf("%s should not be allowed", contentType)
		}
	}
}

// http.DetectContentType returns a type with parameters for some inputs, and
// the comparison is against the bare type.
func TestParametersAreDropped(t *testing.T) {
	t.Parallel()

	if got := detectContentType([]byte("hello, this is plain text and nothing else")); got != "text/plain" {
		t.Errorf("detectContentType = %q, want the type without its charset", got)
	}
	// Confirm the standard library really does add one here, so this test is
	// guarding something rather than nothing.
	if raw := http.DetectContentType([]byte("hello, this is plain text and nothing else")); !strings.Contains(raw, ";") {
		t.Skip("the standard library no longer adds parameters; nothing to drop")
	}
}
