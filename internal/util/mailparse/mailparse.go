// Package mailparse provides email parsing and canonicalization utilities.
package mailparse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"unicode"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("mailparse")

const crlf = "\r\n"

// Split splits mail into headers and body.
func Split(reader io.Reader) ([]string, []byte, error) {
	bufferedReader := bufio.NewReader(reader)
	text := textproto.NewReader(bufferedReader)
	var headers []string
	for {
		l, err := text.ReadLine()
		if err != nil {
			return nil, nil, fmt.Errorf("mailparse: failed to read header: %w", err)
		}
		if len(l) == 0 {
			break
		} else if len(headers) > 0 && (l[0] == ' ' || l[0] == '\t') {
			// This is a continuation line
			headers[len(headers)-1] += l + crlf
		} else {
			headers = append(headers, l+crlf)
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
