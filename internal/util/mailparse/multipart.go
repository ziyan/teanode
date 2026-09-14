package mailparse

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// ErrTooDeeplyNested is a message with multipart bodies nested past
// maximumPartDepth.
var ErrTooDeeplyNested = errors.New("mailparse: message is nested too deeply")

// maximumPartDepth bounds how deep TraverseParts follows multipart bodies
// into one another. Every level is a reader wrapped around the reader of
// the level above, so reading a byte at depth N passes through N readers,
// and a level costs a sender about fifty bytes: a message that nests a
// million levels is small and would take hours to walk. Real mail nests a
// handful.
const maximumPartDepth = 32

// ErrTooManyParts is a message carrying more parts than this server walks.
var ErrTooManyParts = errors.New("mailparse: message has too many parts")

// maximumParts bounds how many parts one message is walked through, across
// every level of it.
//
// Depth was bounded and breadth was not, and the callers are what makes that
// expensive: one of them opens a connection to the virus scanner per part,
// another decodes every compressed part it finds. A megabyte of "--b" repeated
// is two hundred thousand parts -- measured -- so a message nobody looks twice
// at became two hundred thousand connections, which exhausts the scanner and
// silently stops scanning for everybody else while it runs.
//
// A thousand is far past any real message: a photograph album from a phone is
// dozens, and the largest thing anybody has sent through this server is a
// hundred and change.
const maximumParts = 1000

func TraverseParts(headers []string, body []byte, callback func(textproto.MIMEHeader, io.Reader) error) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", FindHeaderValue(headers, "Content-Type"))
	header.Set("Content-Transfer-Encoding", FindHeaderValue(headers, "Content-Transfer-Encoding"))
	walked := 0
	return traverseParts(header, bytes.NewReader(body), callback, 0, &walked)
}

func traverseParts(header textproto.MIMEHeader, reader io.Reader, callback func(textproto.MIMEHeader, io.Reader) error, depth int, walked *int) error {
	contentType := header.Get("Content-Type")
	if contentType == "" {
		*walked++
		if *walked > maximumParts {
			return ErrTooManyParts
		}
		return callback(header, reader)
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(mediaType, "multipart/") || parameters["boundary"] == "" {
		*walked++
		if *walked > maximumParts {
			return ErrTooManyParts
		}
		return callback(header, reader)
	}
	if depth >= maximumPartDepth {
		return ErrTooDeeplyNested
	}

	mr := multipart.NewReader(reader, parameters["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := traverseParts(part.Header, part, callback, depth+1, walked); err != nil {
			return err
		}
	}
	// return nil
}

// Attachment is a file carried by a message being composed.
type Attachment struct {
	Filename string

	// ContentType as the sender declared it. Empty means nothing is known
	// about it, and it is sent as application/octet-stream.
	ContentType string

	Content []byte

	// ContentID is the name the HTML refers to the part by, without the
	// angle brackets: <img src="cid:logo.png"> finds the part whose
	// Content-ID is logo.png. Setting it makes the part inline.
	ContentID string

	// Inline says the part belongs to the body rather than being something
	// the reader is offered to open. A part with a ContentID is inline
	// whether or not this is set.
	Inline bool
}

// inline says whether a part belongs with the body rather than after it.
func (self *Attachment) inline() bool {
	return self != nil && (self.Inline || safeContentID(self.ContentID) != "")
}

// safeContentID is a Content-ID that can be written into a header, or the
// empty string for one that cannot be made into one.
//
// The value ends up between angle brackets in a header this package writes
// by hand, so anything outside the characters an addr-spec may hold is left
// out rather than escaped: a Content-ID is a name the HTML refers to, and a
// name that needs escaping is a name that will not be found anyway. A file
// called "a\r\nContent-Disposition: attachment" would otherwise be a header
// of its own, and a filename is somebody's to choose.
func safeContentID(id string) string {
	kept := strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9':
			return character
		}
		switch character {
		case '.', '-', '_', '+', '@', '%', '=', '~':
			return character
		}
		return -1
	}, strings.TrimSpace(id))
	if len(kept) > 200 {
		kept = kept[:200]
	}
	return kept
}

// ErrEmptyMessage is returned by Compose when there is nothing to send.
var ErrEmptyMessage = errors.New("mailparse: a message needs a body or an attachment")

// Compose writes a message body from its parts and returns the headers that
// describe it: MIME-Version and the top-level Content-Type.
//
// The shape follows what is present. Text alone is a single text/plain part
// and HTML alone a single text/html part, because a multipart/alternative
// with one empty alternative is a message some clients show as blank. Both
// together are a multipart/alternative, text first, which is the order a
// client that cannot show HTML expects. A picture the HTML refers to by
// cid: is part of the body rather than something to open, so those wrap the
// content in a multipart/related. Attachments wrap whichever of those in a
// multipart/mixed, the content first and the files after it.
func Compose(writer io.Writer, text, html []byte, attachments []*Attachment) ([]string, error) {
	if len(text) == 0 && len(html) == 0 && len(attachments) == 0 {
		return nil, ErrEmptyMessage
	}

	headers := []string{UnsplitHeader("MIME-Version", "1.0")}

	// The body and the pictures it refers to, as one thing. With no body
	// there is nothing for them to be related to, so they are files like
	// any other -- a multipart/related whose root is an empty text part is
	// a message clients show as blank with an orphan picture in it.
	var inline, separate []*Attachment
	for _, attachment := range attachments {
		if attachment.inline() && (len(text) > 0 || len(html) > 0) {
			inline = append(inline, attachment)
		} else {
			separate = append(separate, attachment)
		}
	}

	if len(separate) == 0 {
		header, err := writeRelated(writer, text, html, inline)
		if err != nil {
			return nil, err
		}
		return append(headers, unsplitMIMEHeader(header)...), nil
	}

	mixed := multipart.NewWriter(writer)
	if len(text) > 0 || len(html) > 0 || len(inline) > 0 {
		var content bytes.Buffer
		header, err := writeRelated(&content, text, html, inline)
		if err != nil {
			return nil, err
		}
		partWriter, err := mixed.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := partWriter.Write(content.Bytes()); err != nil {
			return nil, err
		}
	}
	for _, attachment := range separate {
		if err := writeAttachment(mixed, attachment); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}

	return append(headers, UnsplitHeader("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{
		"boundary": mixed.Boundary(),
	}))), nil
}

// writeRelated writes the body together with the pictures it refers to. With
// no such pictures it is the body alone: a multipart/related holding one
// thing is a wrapper that says nothing, and some clients show it as an
// attachment rather than as a message.
func writeRelated(writer io.Writer, text, html []byte, inline []*Attachment) (textproto.MIMEHeader, error) {
	if len(inline) == 0 {
		return writeContent(writer, text, html)
	}
	related := multipart.NewWriter(writer)
	var content bytes.Buffer
	header, err := writeContent(&content, text, html)
	if err != nil {
		return nil, err
	}
	partWriter, err := related.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := partWriter.Write(content.Bytes()); err != nil {
		return nil, err
	}
	for _, attachment := range inline {
		if err := writeAttachment(related, attachment); err != nil {
			return nil, err
		}
	}
	if err := related.Close(); err != nil {
		return nil, err
	}
	result := make(textproto.MIMEHeader)
	// "type" names the part a reader should start from, which for a message
	// built here is always the body.
	result.Set("Content-Type", mime.FormatMediaType("multipart/related", map[string]string{
		"boundary": related.Boundary(), "type": startType(text, html),
	}))
	return result, nil
}

// startType is the media type of the part a multipart/related starts from.
func startType(text, html []byte) string {
	switch {
	case len(text) > 0 && len(html) > 0:
		return "multipart/alternative"
	case len(html) > 0:
		return "text/html"
	default:
		return "text/plain"
	}
}

// writeContent writes the readable part of a message and returns the header
// describing what it wrote.
func writeContent(writer io.Writer, text, html []byte) (textproto.MIMEHeader, error) {
	if len(text) > 0 && len(html) > 0 {
		alternative := multipart.NewWriter(writer)
		for _, part := range []struct {
			contentType string
			content     []byte
		}{
			{"text/plain", text},
			{"text/html", html},
		} {
			partWriter, err := alternative.CreatePart(textHeader(part.contentType))
			if err != nil {
				return nil, err
			}
			if err := writeBase64(partWriter, part.content); err != nil {
				return nil, err
			}
		}
		if err := alternative.Close(); err != nil {
			return nil, err
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{
			"boundary": alternative.Boundary(),
		}))
		return header, nil
	}

	contentType, content := "text/plain", text
	if len(html) > 0 {
		contentType, content = "text/html", html
	}
	if err := writeBase64(writer, content); err != nil {
		return nil, err
	}
	return textHeader(contentType), nil
}

func textHeader(contentType string) textproto.MIMEHeader {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"charset": "utf-8"}))
	header.Set("Content-Transfer-Encoding", "base64")
	return header
}

func writeAttachment(into *multipart.Writer, attachment *Attachment) error {
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// FormatMediaType encodes a filename that is not plain ASCII the way RFC
	// 2231 says to, which is what a client expects to find it under.
	parameters := map[string]string{}
	if attachment.Filename != "" {
		parameters["filename"] = attachment.Filename
	}
	disposition := "attachment"
	if attachment.inline() {
		disposition = "inline"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"name": attachment.Filename}))
	header.Set("Content-Disposition", mime.FormatMediaType(disposition, parameters))
	header.Set("Content-Transfer-Encoding", "base64")
	if id := safeContentID(attachment.ContentID); id != "" {
		// Written into the map rather than through Set, which canonicalizes
		// the name to "Content-Id". Both are the same field to a reader --
		// header names are case-insensitive -- but every other mail agent
		// writes "Content-ID", and a message that looks like the others is
		// one fewer thing for somebody debugging to wonder about.
		//
		// Written raw, so the value has to be safe before it gets here:
		// nothing else escapes it, and a Content-ID is a file's name, which
		// somebody chose when they uploaded it.
		header["Content-ID"] = []string{"<" + id + ">"}
	}
	partWriter, err := into.CreatePart(header)
	if err != nil {
		return err
	}
	return writeBase64(partWriter, attachment.Content)
}

// base64LineLength is what RFC 2045 allows on a line of base64, and what
// every client wraps at. SMTP refuses lines over 998 bytes, so a part written
// as one line — which is what an encoder writes on its own — is a message
// that some servers will not take.
const base64LineLength = 76

func writeBase64(writer io.Writer, content []byte) error {
	encoded := base64.StdEncoding.EncodeToString(content)
	for len(encoded) > 0 {
		line := encoded
		if len(line) > base64LineLength {
			line = encoded[:base64LineLength]
		}
		encoded = encoded[len(line):]
		if _, err := fmt.Fprintf(writer, "%s%s", line, crlf); err != nil {
			return err
		}
	}
	return nil
}

// unsplitMIMEHeader turns a part header into header lines, in a fixed order
// so the output of composing is reproducible.
func unsplitMIMEHeader(header textproto.MIMEHeader) []string {
	var lines []string
	for _, key := range []string{"Content-Type", "Content-Transfer-Encoding"} {
		if value := header.Get(key); value != "" {
			lines = append(lines, UnsplitHeader(key, value))
		}
	}
	return lines
}
