// Package notes is the format a phone's Notes app keeps a note in when it
// keeps its notes in a mail account: one message a note, in a folder of their
// own, a single HTML part, and a few headers of the phone's that say it is a
// note and which one. Every edit is a new message with the same identifier,
// and the old one is flagged deleted.
//
// Reading and writing that format is here; where the notes are kept and who
// may change them is the mailbox's.
package notes

import (
	"crypto/rand"
	"fmt"
	"html"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// The headers are the phone's contract and keep the phone's names.
const (
	// HeaderType says what kind of thing a message is; a note says TypeNote.
	HeaderType = "X-Uniform-Type-Identifier"

	// HeaderIdentifier is the note's identity, the same in every version of
	// it. The Message-ID is not: each version is a message of its own.
	HeaderIdentifier = "X-Universally-Unique-Identifier"

	// HeaderCreatedDate is when the note was first written, carried from
	// version to version, as an RFC 5322 date.
	HeaderCreatedDate = "X-Mail-Created-Date"

	// TypeNote is the value of HeaderType on a note.
	TypeNote = "com.apple.mail-note"
)

// UneditableReason is what a person is told when a note cannot be changed
// here: a new version is written from text or HTML alone, which would drop
// the pictures and attachments the phone keeps in the message.
const UneditableReason = "this note has pictures or attachments; change it on the phone"

// titleLimit bounds the title, which is the Subject of the message: a note
// whose first line is a paragraph is still named by a line.
const titleLimit = 200

// Identifier is the note identifier these headers carry, or empty when they
// are not a note's.
func Identifier(headers []string) string {
	if !strings.EqualFold(strings.TrimSpace(mailparse.FindHeaderValue(headers, HeaderType)), TypeNote) {
		return ""
	}
	identifier := strings.TrimSpace(mailparse.FindHeaderValue(headers, HeaderIdentifier))
	// The column holds 64 characters; a UUID is 36. Anything longer is not
	// an identifier the phone wrote, and is better not a note than a note
	// that cannot be stored.
	if len(identifier) > 64 {
		return ""
	}
	return identifier
}

// CreatedAt is when the note these headers belong to was first written, or
// the zero time when they do not say.
func CreatedAt(headers []string) time.Time {
	value := strings.TrimSpace(mailparse.FindHeaderValue(headers, HeaderCreatedDate))
	if value == "" {
		return time.Time{}
	}
	at, err := mail.ParseDate(value)
	if err != nil {
		return time.Time{}
	}
	return at
}

// NewIdentifier is an identifier for a new note, in the form the phone
// writes one: a random UUID in capitals.
func NewIdentifier() string {
	var bytes [16]byte
	_, _ = rand.Read(bytes[:])
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]))
}

// Headers are the lines that make a message a note: its type, its identity
// and when it was first written.
func Headers(identifier string, createdAt time.Time) []string {
	return []string{
		mailparse.UnsplitHeader(HeaderType, TypeNote),
		mailparse.UnsplitHeader(HeaderIdentifier, identifier),
		mailparse.UnsplitHeader(HeaderCreatedDate, createdAt.Format(time.RFC1123Z)),
	}
}

// Title is what a note is called: its first line with anything in it, as on
// the phone, which names a note that way and writes the name as the Subject.
func Title(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if utf8.RuneCountInString(line) > titleLimit {
				line = string([]rune(line)[:titleLimit])
			}
			return line
		}
	}
	return ""
}

// Preview is what follows the title, on one line and cut to about limit
// characters: what a list of notes shows under each name.
func Preview(text string, limit int) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines = lines[index+1:]
			break
		}
	}
	preview := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	if limit > 0 && utf8.RuneCountInString(preview) > limit {
		preview = strings.TrimSpace(string([]rune(preview)[:limit])) + "…"
	}
	return preview
}

// TextToHTML writes plain text as the phone writes a note: a div a line, an
// empty line as a div holding a line break, and a run of lines starting
// "- " as a list.
func TextToHTML(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var builder strings.Builder
	builder.WriteString("<html><head></head><body>")
	isInList := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if item, isItem := strings.CutPrefix(line, "- "); isItem {
			if !isInList {
				builder.WriteString("<ul>")
				isInList = true
			}
			builder.WriteString("<li>")
			builder.WriteString(html.EscapeString(item))
			builder.WriteString("</li>")
			continue
		}
		if isInList {
			builder.WriteString("</ul>")
			isInList = false
		}
		if strings.TrimSpace(line) == "" {
			builder.WriteString("<div><br></div>")
			continue
		}
		builder.WriteString("<div>")
		builder.WriteString(html.EscapeString(line))
		builder.WriteString("</div>")
	}
	if isInList {
		builder.WriteString("</ul>")
	}
	builder.WriteString("</body></html>")
	return builder.String()
}

// Document puts the body of a note in the document the phone writes around
// one. HTML that is already a document is left as it is.
func Document(body string) string {
	if strings.Contains(strings.ToLower(body), "<body") {
		return body
	}
	return "<html><head></head><body>" + body + "</body></html>"
}

// IsEditable says whether a version of a note can be replaced by one written
// from its text or HTML without losing anything: not when the message has
// more than one part, which is how the phone keeps a picture or a file in a
// note, and not when the HTML shows a picture.
func IsEditable(headers []string, html string) bool {
	contentType := strings.ToLower(strings.TrimSpace(mailparse.FindHeaderValue(headers, "Content-Type")))
	if strings.HasPrefix(contentType, "multipart/") {
		return false
	}
	return !hasPicture(html)
}
