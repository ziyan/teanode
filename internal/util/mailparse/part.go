package mailparse

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"strings"
)

// ErrNoSuchPart is returned when a message has no part at the position
// asked for. A distinct error so a caller can answer "not found" rather than
// "something went wrong", which is what a stale link deserves.
var ErrNoSuchPart = errors.New("mailparse: no such part")

// Part is one piece of a message, decoded.
type Part struct {
	// ContentType as the sender declared it, without its parameters.
	ContentType string

	// Filename the sender gave it, which may be empty, repeated across
	// parts, or a path. Nothing here trusts it.
	Filename string

	// ContentID is what a cid: reference in the HTML points at, without the
	// angle brackets it is written in.
	ContentID string

	// Inline parts are meant to be shown where the HTML refers to them
	// rather than listed as files.
	Inline bool

	Content []byte
}

// PartAt returns the part at a position in the message.
//
// By position because that is the only stable name a part has: a filename can
// repeat, be missing, or be a path, and a Content-ID is optional. A stored
// message never changes, so counting the same walk twice gives the same part.
func PartAt(headers []string, body []byte, index int) (*Part, error) {
	var found *Part
	position := 0

	if err := TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		current := position
		position++
		if current != index || found != nil {
			return nil
		}
		part, err := DecodePart(header, reader, 0)
		if err != nil {
			return err
		}
		found = part
		return nil
	}); err != nil {
		return nil, err
	}

	if found == nil {
		return nil, fmt.Errorf("%w: %d", ErrNoSuchPart, index)
	}
	return found, nil
}

// DecodePart reads one part, undoing its transfer encoding. A limit of zero
// reads all of it, which is what serving a file needs; the dashboard passes
// one when it is only going to render the first part of something.
func DecodePart(header textproto.MIMEHeader, reader io.Reader, limit int64) (*Part, error) {
	if limit > 0 {
		reader = io.LimitReader(reader, limit)
	}

	part := &Part{}

	mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		// A part with no usable content type is plain text, which is what
		// RFC 2045 says to assume.
		mediaType = "text/plain"
		parameters = map[string]string{}
	}
	part.ContentType = mediaType
	part.Filename = parameters["name"]

	if disposition := header.Get("Content-Disposition"); disposition != "" {
		part.Inline = strings.HasPrefix(strings.ToLower(disposition), "inline")
		if _, dispositionParameters, err := mime.ParseMediaType(disposition); err == nil {
			if dispositionParameters["filename"] != "" {
				part.Filename = dispositionParameters["filename"]
			}
		}
	}
	part.Filename = DecodeHeaderValue(part.Filename)
	part.ContentID = strings.Trim(header.Get("Content-ID"), "<> ")

	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		decoded, err := DecodeBase64String(string(content))
		if err != nil {
			return nil, err
		}
		part.Content = decoded
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(reader))
		if err != nil {
			return nil, err
		}
		part.Content = decoded
	default:
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		part.Content = decoded
	}

	return part, nil
}
