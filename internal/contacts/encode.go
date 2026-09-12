package contacts

import (
	"bytes"
	"sort"
	"strings"

	"github.com/emersion/go-vcard"
)

// Writing a card out.
//
// This does not use the library's encoder, for two reasons found by putting
// awkward cards through it and reading what came back.
//
// It never quotes a parameter value. A card carrying
//
//	EMAIL;TYPE="work;main":ada@example.com
//
// decodes to one parameter whose value contains a semicolon, and comes back
// out unquoted as EMAIL;TYPE=work;main:ada@example.com -- which is a
// different thing, and reading *that* back gives a parameter called MAIN
// whose value is the address and an EMAIL property with no value at all. The
// address is destroyed on the second write, which for a contact edited in the
// dashboard means the first edit.
//
// And it never folds. RFC 6350 asks for lines of at most 75 octets; a note of
// five thousand characters came out as a single five-thousand-character line,
// which some clients will not read.
//
// Escaping is deliberately the same as the library's, because its decoder is
// the one reading this back: it turns \\ into \, \n into a newline and \, into
// a comma, and nothing else. Matching it exactly is what makes a stored card a
// fixed point -- encode, decode, encode again and the bytes are the same --
// which is what lets the ETag be taken over what is stored.

const (
	// foldAt is where a line is broken, in octets, per RFC 6350 section
	// 3.2. The continuation begins with one space, which is not part of
	// the value.
	foldAt = 75

	// needsQuoting are the characters that cannot appear in a bare
	// parameter value.
	needsQuoting = ";:,"
)

// structured are the properties whose value is several components separated
// by semicolons. In those a semicolon is punctuation and must stay bare; in
// every other property it is part of the text and has to be escaped.
var structured = map[string]bool{
	vcard.FieldName: true, vcard.FieldAddress: true, vcard.FieldOrganization: true,
	vcard.FieldGender: true, "CLIENTPIDMAP": true,
}

// The library's decoder turns \\ into a backslash, \n into a newline and \, into
// a comma, and leaves \; alone. Escaping exactly what it unescapes is what
// makes a stored card a fixed point; the semicolon is handled separately,
// because the decoder does not, and a note reading "call him\; he knows" would
// otherwise gain a visible backslash every time it passed through here.
var valueEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n", ",", "\\,")

var textEscaper = strings.NewReplacer("\\", "\\\\", "\n", "\\n", ",", "\\,", ";", "\\;")

// escapeStructured writes a value whose semicolons are punctuation.
//
// The decoder leaves an escaped semicolon alone, and escaping the backslash
// in front of it would turn ORG:Acme\; Inc;Engines into a company called
// "Acme\" and another called " Inc". The escape is set aside, the rest is
// escaped as usual, and it is put back.
func escapeStructured(value string) string {
	const aside = "\x00"
	value = strings.ReplaceAll(value, "\\;", aside)
	value = valueEscaper.Replace(value)
	return strings.ReplaceAll(value, aside, "\\;")
}

// Encode writes a card out in the one form this server stores.
func Encode(card vcard.Card) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("BEGIN:VCARD\r\n")
	// Version first, and exactly once, because a card that does not say
	// which version it is cannot be read by anything.
	out.WriteString("VERSION:4.0\r\n")

	names := make([]string, 0, len(card))
	for name := range card {
		if name == vcard.FieldVersion {
			continue
		}
		names = append(names, name)
	}
	// A stable order, so that the same card always gives the same bytes and
	// therefore the same ETag.
	sort.Strings(names)

	for _, name := range names {
		for _, field := range card[name] {
			if field == nil {
				continue
			}
			writeFold(&out, line(name, field))
		}
	}
	out.WriteString("END:VCARD\r\n")
	return out.Bytes(), nil
}

// line is one property, unfolded.
func line(name string, field *vcard.Field) string {
	var built strings.Builder
	if field.Group != "" {
		built.WriteString(field.Group)
		built.WriteByte('.')
	}
	built.WriteString(name)

	keys := make([]string, 0, len(field.Params))
	for key := range field.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range field.Params[key] {
			built.WriteByte(';')
			built.WriteString(key)
			built.WriteByte('=')
			built.WriteString(parameter(value))
		}
	}
	built.WriteByte(':')
	value := withoutControls(field.Value)
	if structured[name] {
		built.WriteString(escapeStructured(value))
	} else {
		built.WriteString(textEscaper.Replace(value))
	}
	return built.String()
}

// parameter is one parameter value, quoted when it has to be.
//
// Control characters are removed first, and that is not tidiness. The
// library's decoder turns \n inside a parameter into a real newline, and a
// real newline written back out ends the line: everything after it becomes a
// property of its own, and the property it came from loses its value. A card
// carrying UID;X="a\nUID:other":u came back with a second, genuine UID.
//
// A quoted value may not contain a quotation mark and there is no escape for
// one inside quotes, so a mark is dropped rather than producing a line that
// cannot be read back.
func parameter(value string) string {
	// Every control character, the newline included. A value has an
	// escaper that turns a newline into the two characters \n; a
	// parameter has none, so a newline here is a newline on the wire and
	// the rest of the line becomes a property of its own.
	value = strings.Map(func(letter rune) rune {
		if letter < 0x20 || letter == 0x7f {
			return -1
		}
		return letter
	}, value)
	// A backslash and a quotation mark both go, and neither is fussiness.
	//
	// The library's decoder reads a quoted parameter with Go's own
	// unquoting, so a backslash inside quotes is taken as the start of an
	// escape: a value ending in one escapes the closing quote, the
	// parameters then fail to parse, and the whole property is dropped
	// without a word -- an address destroyed the next time the card is
	// read. A quotation mark has no escape inside quotes at all. Neither
	// has any business in a parameter value, so neither is written.
	value = strings.NewReplacer(`\`, "", `"`, "").Replace(value)
	if !strings.ContainsAny(value, needsQuoting) {
		return value
	}
	return `"` + value + `"`
}

// withoutControls drops the characters that would end a line or split it,
// keeping the newline, which the value escapers write as the two characters
// \n. Nothing else in that range has a meaning in a card or a way to be
// written safely.
func withoutControls(value string) string {
	return strings.Map(func(letter rune) rune {
		if letter == '\n' {
			return letter
		}
		if letter < 0x20 || letter == 0x7f {
			return -1
		}
		return letter
	}, value)
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
