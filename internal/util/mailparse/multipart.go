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

func TraverseParts(headers []string, body []byte, callback func(textproto.MIMEHeader, io.Reader) error) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", FindHeaderValue(headers, "Content-Type"))
	header.Set("Content-Transfer-Encoding", FindHeaderValue(headers, "Content-Transfer-Encoding"))
	return traverseParts(header, bytes.NewReader(body), callback)
}

func traverseParts(header textproto.MIMEHeader, reader io.Reader, callback func(textproto.MIMEHeader, io.Reader) error) error {
	contentType := header.Get("Content-Type")
	if contentType == "" {
		return callback(header, reader)
	}
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(mediaType, "multipart/") || parameters["boundary"] == "" {
		return callback(header, reader)
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
		if err := traverseParts(part.Header, part, callback); err != nil {
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
// client that cannot show HTML expects. Attachments wrap whichever of those
// in a multipart/mixed, the content first and the files after it.
func Compose(writer io.Writer, text, html []byte, attachments []*Attachment) ([]string, error) {
	if len(text) == 0 && len(html) == 0 && len(attachments) == 0 {
		return nil, ErrEmptyMessage
	}

	headers := []string{UnsplitHeader("MIME-Version", "1.0")}

	if len(attachments) == 0 {
		header, err := writeContent(writer, text, html)
		if err != nil {
			return nil, err
		}
		return append(headers, unsplitMIMEHeader(header)...), nil
	}

	mixed := multipart.NewWriter(writer)
	if len(text) > 0 || len(html) > 0 {
		var content bytes.Buffer
		header, err := writeContent(&content, text, html)
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
	for _, attachment := range attachments {
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

func writeAttachment(mixed *multipart.Writer, attachment *Attachment) error {
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
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", mime.FormatMediaType(contentType, map[string]string{"name": attachment.Filename}))
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", parameters))
	header.Set("Content-Transfer-Encoding", "base64")
	partWriter, err := mixed.CreatePart(header)
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
