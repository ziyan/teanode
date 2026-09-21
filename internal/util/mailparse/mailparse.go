// Package mailparse provides email parsing and canonicalization utilities.
package mailparse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"unicode"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("mailparse")

const crlf = "\r\n"

// ErrTooManyHeaders is a message whose header block is larger than any
// message has a reason to be.
var ErrTooManyHeaders = errors.New("mailparse: too many headers")

// Bounds on the header block. A header block is walked many times over —
// for every signature, for every rule — so it is bounded on the way in
// rather than at each of those. Real mail has tens of headers of a few
// hundred bytes; a long Received chain or a DKIM signature with a large
// key runs to a few kilobytes.
const (
	MaximumHeaders    = 4096
	MaximumHeaderSize = 64 * 1024
)

// Split splits mail into headers and body.
func Split(reader io.Reader) ([]string, []byte, error) {
	bufferedReader := bufio.NewReader(reader)
	text := textproto.NewReader(bufferedReader)
	var headers []string
	// The header being assembled, built up rather than appended to a
	// string: a continuation line appended to a string copies the whole
	// header again, which made a header of many short continuation lines
	// cost the square of its size.
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			headers = append(headers, current.String())
			current.Reset()
		}
	}
	for {
		l, err := text.ReadLine()
		if err != nil {
			return nil, nil, fmt.Errorf("mailparse: failed to read header: %w", err)
		}
		if len(l) == 0 {
			flush()
			break
		} else if current.Len() > 0 && (l[0] == ' ' || l[0] == '\t') {
			// This is a continuation line
			if current.Len()+len(l)+len(crlf) > MaximumHeaderSize {
				return nil, nil, ErrTooManyHeaders
			}
			current.WriteString(l)
			current.WriteString(crlf)
		} else {
			flush()
			if len(headers) >= MaximumHeaders || len(l)+len(crlf) > MaximumHeaderSize {
				return nil, nil, ErrTooManyHeaders
			}
			current.WriteString(l)
			current.WriteString(crlf)
		}
	}
	var body bytes.Buffer
	if _, err := io.Copy(&body, bufferedReader); err != nil {
		return nil, nil, err
	}
	return headers, body.Bytes(), nil
}

// Unsplit merges headers and body back together.
func Unsplit(writer io.Writer, body []byte, headers ...[]string) error {
	for _, h := range headers {
		for _, header := range h {
			if _, err := writer.Write([]byte(header)); err != nil {
				return err
			}
		}
	}
	if _, err := writer.Write([]byte(crlf)); err != nil {
		return err
	}
	if _, err := writer.Write(body); err != nil {
		return err
	}
	return nil
}

func StripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}
