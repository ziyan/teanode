// Package scheduling is invitations by mail.
//
// This is iMIP, defined in RFC 6047: an invitation is a message carrying a
// calendar part with METHOD:REQUEST, the answer is one with METHOD:REPLY, and
// calling a meeting off is METHOD:CANCEL. It is how invitations have always
// worked between organizations that share no calendar server, and since this
// is a mail server it is the way that matters.
//
// Nothing here happens during the SMTP transaction. Delivery notes that a
// message arrived and a worker reads it afterwards: fetching a message from
// storage and decoding it is exactly the kind of work that, done inside the
// transaction, turns a slow disk into mail this server refuses.
package scheduling

import (
	"bytes"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"strings"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

var log = logging.MustGetLogger("scheduling")

// maximumPart is how large a calendar part this server will read out of a
// message. The same ceiling an event has, which is what it would be stored
// as; anything larger is refused before it is decoded rather than after.
const maximumPart = calendar.MaximumObject

// CalendarPart is the calendar text in a message, or nothing.
//
// A message may carry it in two shapes, and both are common: as its own
// text/calendar part, which is what a calendar program sends, and as an
// attachment named something.ics, which is what a program that does not
// really speak the protocol sends. Both are read, because a person invited by
// either one still wants their invitation.
func CalendarPart(headers []string, body []byte) []byte {
	var found []byte
	err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		if found != nil {
			return nil
		}
		if !isCalendarPart(header) {
			return nil
		}
		// Bounded as it is read. A part claiming to be a calendar is not
		// a reason to read a gigabyte into memory. The bound is on the
		// encoded form, which is larger than what it decodes to.
		encoding := strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding")))
		content, err := io.ReadAll(io.LimitReader(reader, encodedLimit(encoding)))
		if err != nil {
			return nil
		}
		content, err = decoded(encoding, content)
		if err != nil {
			return nil
		}
		if len(content) > maximumPart || len(bytes.TrimSpace(content)) == 0 {
			return nil
		}
		found = content
		return nil
	})
	if err != nil {
		log.Debugf("a message could not be walked for a calendar part: %s", err)
		return nil
	}
	return found
}

// decoded is the part's bytes as they were written, rather than as they
// travelled.
//
// A walk over a message hands back each part as it stands in the file, and an
// invitation is nearly always base64 -- it is what a calendar program sends,
// because an iCalendar file has long lines and non-ASCII names in it. Reading
// it without decoding it meant handing the parser a wall of base64, which
// parses as nothing, so most real invitations were silently not invitations.
func decoded(encoding string, content []byte) ([]byte, error) {
	switch encoding {
	case "base64":
		return mailparse.DecodeBase64String(string(content))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(content)))
	default:
		// 7bit, 8bit, binary, and anything a sender made up: the bytes are
		// the text.
		return content, nil
	}
}

// encodedLimit is how much of a part to read for a calendar of at most
// maximumPart bytes, given how it was encoded. Base64 is four bytes for
// three; quoted-printable is three for one in the worst case, where every
// byte needs escaping.
func encodedLimit(encoding string) int64 {
	switch encoding {
	case "base64":
		return int64(maximumPart)*4/3 + 1024
	case "quoted-printable":
		return int64(maximumPart)*3 + 1024
	default:
		return int64(maximumPart) + 1
	}
}

// isCalendarPart is whether one part of a message is calendar text.
func isCalendarPart(header textproto.MIMEHeader) bool {
	contentType := header.Get("Content-Type")
	if contentType != "" {
		kind, _, err := mime.ParseMediaType(contentType)
		if err == nil && strings.EqualFold(kind, "text/calendar") {
			return true
		}
		// application/ics, which a few programs send, and
		// application/octet-stream with an .ics name, which is what
		// happens when a program gives up on saying what something is.
		if err == nil && strings.EqualFold(kind, "application/ics") {
			return true
		}
	}
	return strings.HasSuffix(strings.ToLower(fileNameOf(header)), ".ics")
}

// fileNameOf is what a part calls itself, from either of the two headers that
// carry a name.
func fileNameOf(header textproto.MIMEHeader) string {
	for _, field := range []string{"Content-Disposition", "Content-Type"} {
		value := header.Get(field)
		if value == "" {
			continue
		}
		_, parameters, err := mime.ParseMediaType(value)
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(parameters["filename"]); name != "" {
			return name
		}
		if name := strings.TrimSpace(parameters["name"]); name != "" {
			return name
		}
	}
	return ""
}
