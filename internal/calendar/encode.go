package calendar

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/emersion/go-ical"
)

// foldAt is the longest line RFC 5545 asks for, in octets, counting neither
// the carriage return nor the line feed that end it.
const foldAt = 75

// Encode writes a calendar back out in the form this server stores.
//
// The library's own encoder does the work, and deliberately: it was measured
// against the cases that destroy a vCard -- a parameter in quotes, a colon
// inside one, a newline written into one, an escaped semicolon in a value --
// and it returns every one of them unchanged. Writing another encoder here
// would be carrying a scar from the address book into a place that does not
// have the wound.
//
// What it does not do is fold. RFC 5545 section 3.1 asks for lines of at most
// 75 octets, continued by a leading space, and the library writes a value of
// any length as one line. Most clients read a long line without complaining,
// which is why this is a conformance gap rather than a defect, but it is a
// gap this server does not have to have.
func Encode(cal *ical.Calendar) ([]byte, error) {
	if cal == nil {
		return nil, fmt.Errorf("calendar: there is nothing to write")
	}
	var written bytes.Buffer
	if err := ical.NewEncoder(&written).Encode(cal); err != nil {
		return nil, fmt.Errorf("calendar: that event cannot be written: %w", err)
	}
	return fold(written.Bytes()), nil
}

// fold breaks every line that is too long.
//
// It works over the encoder's output rather than inside it, which is what
// makes it safe: the encoder has already decided what a line says, escapes
// and all, and folding only decides where it is allowed to break. A line that
// is already short enough is passed through untouched, so the ordinary event
// comes out of here byte for byte as the library wrote it.
func fold(encoded []byte) []byte {
	// An already-folded line must not be folded again from its start: the
	// continuation would count the leading space twice and creep leftwards
	// on every pass. Nothing here unfolds first, because the library never
	// folds, so every line arriving is whole.
	lines := strings.Split(string(encoded), "\r\n")
	var out bytes.Buffer
	out.Grow(len(encoded) + len(encoded)/foldAt + 16)
	for index, line := range lines {
		if index == len(lines)-1 && line == "" {
			// The trailing empty piece left by the final "\r\n", which
			// has already been written by the line before it.
			break
		}
		writeFold(&out, line)
	}
	return out.Bytes()
}

// writeFold writes one line, broken so that no line exceeds foldAt octets.
//
// Broken between characters, never inside one: a continuation that begins
// halfway through a multi-byte character is not text any more.
func writeFold(out *bytes.Buffer, text string) {
	written := 0
	first := true
	for {
		// The first line may be foldAt octets; every later one begins
		// with a space, so it may be one less.
		limit := foldAt
		if !first {
			limit = foldAt - 1
		}
		if len(text)-written <= limit {
			break
		}
		// The last character boundary that still fits. Measured by where
		// a character ends rather than where it starts: eighteen emoji
		// all begin within seventy-five octets and the last of them ends
		// at seventy-seven.
		cut := written
		for index := range text[written:] {
			at := written + index
			if at == written {
				continue
			}
			if at-written > limit {
				break
			}
			cut = at
		}
		if cut == written {
			// One character longer than a whole line, which cannot be
			// folded. Written as it is; a reader would rather have a long
			// line than half a character.
			break
		}
		if !first {
			out.WriteByte(' ')
		}
		out.WriteString(text[written:cut])
		out.WriteString("\r\n")
		written = cut
		first = false
	}
	// Whatever is left, which by now fits.
	if !first {
		out.WriteByte(' ')
	}
	out.WriteString(text[written:])
	out.WriteString("\r\n")
}

// Unfold puts a folded line back together, which is what a reader does before
// looking at it. Exported for the tests, which prove that what folding did
// can be undone.
func Unfold(encoded []byte) []byte {
	return bytes.ReplaceAll(encoded, []byte("\r\n "), nil)
}
