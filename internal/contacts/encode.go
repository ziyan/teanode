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
	if structured[name] {
		built.WriteString(valueEscaper.Replace(field.Value))
	} else {
		built.WriteString(textEscaper.Replace(field.Value))
	}
	return built.String()
}

// parameter is one parameter value, quoted when it has to be.
//
// A quoted value may not itself contain a quotation mark, and there is no
// escape for one inside quotes, so a mark in the value is dropped rather than
// producing a line that cannot be read back.
func parameter(value string) string {
	if !strings.ContainsAny(value, needsQuoting) && !strings.Contains(value, "\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, "") + `"`
}

// writeFold writes one line, broken so that no line exceeds foldAt octets.
//
// Broken between characters, never inside one: a continuation that begins
// halfway through a multi-byte character is not text any more.
func writeFold(out *bytes.Buffer, text string) {
	if len(text) <= foldAt {
		out.WriteString(text)
		out.WriteString("\r\n")
		return
	}
	written := 0
	for index := range text {
		if index == 0 {
			continue
		}
		// The first line may be foldAt octets; every later one is a space
		// plus foldAt-1, so that the whole line is still within the limit.
		limit := foldAt
		if written > 0 {
			limit = foldAt - 1
		}
		if index-written < limit {
			continue
		}
		if written > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(text[written:index])
		out.WriteString("\r\n")
		written = index
	}
	if written < len(text) {
		if written > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(text[written:])
	}
	out.WriteString("\r\n")
}
