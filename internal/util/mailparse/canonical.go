package mailparse

import (
	"io"
	"regexp"
	"strings"
)

var reduceWhitespacePattern = regexp.MustCompile(`[ \t\r\n]+`)

// Canonicalization is a canonicalization algorithm.
type Canonicalization string

const (
	CanonicalizationSimple  Canonicalization = "simple"
	CanonicalizationRelaxed Canonicalization = "relaxed"
)

type Canonicalizer interface {
	CanonicalizeHeader(text string) string
	CanonicalizeBody(writer io.Writer) io.WriteCloser
}

var canonicalizers = map[Canonicalization]Canonicalizer{
	CanonicalizationSimple:  new(simpleCanonicalizer),
	CanonicalizationRelaxed: new(relaxedCanonicalizer),
}

func GetCanonicalizer(canonicalization Canonicalization) (Canonicalizer, bool) {
	canonicalizer, ok := canonicalizers[canonicalization]
	return canonicalizer, ok
}

func ParseCanonicalization(value string) (Canonicalization, Canonicalization) {
	header := CanonicalizationSimple
	body := CanonicalizationSimple

	parts := strings.SplitN(StripWhitespace(value), "/", 2)
	if parts[0] != "" {
		header = Canonicalization(parts[0])
	}
	if len(parts) > 1 {
		body = Canonicalization(parts[1])
	}
	return header, body
}

// crlfFixer fixes any lone LF without a preceding CR.
type crlfFixer struct {
	cr bool
}

func (self *crlfFixer) Fix(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for _, character := range data {
		previousCarriageReturn := self.cr
		self.cr = false
		switch character {
		case '\r':
			self.cr = true
		case '\n':
			if !previousCarriageReturn {
				result = append(result, '\r')
			}
		}
		result = append(result, character)
	}
	return result
}

type simpleCanonicalizer struct{}

func (self *simpleCanonicalizer) CanonicalizeHeader(value string) string {
	return value
}

type simpleBodyCanonicalizer struct {
	writer     io.Writer
	crlfBuffer []byte
	crlfFixer  crlfFixer
}

func (self *simpleBodyCanonicalizer) Write(data []byte) (int, error) {
	written := len(data)
	data = append(self.crlfBuffer, data...)

	data = self.crlfFixer.Fix(data)

	end := len(data)
	// If it ends with \r, maybe the next write will begin with \n
	if end > 0 && data[end-1] == '\r' {
		end--
	}
	// Keep all \r\n sequences
	for end >= 2 {
		prev := data[end-2]
		cur := data[end-1]
		if prev != '\r' || cur != '\n' {
			break
		}
		end -= 2
	}

	self.crlfBuffer = data[end:]

	var err error
	if end > 0 {
		_, err = self.writer.Write(data[:end])
	}
	return written, err
}

func (self *simpleBodyCanonicalizer) Close() error {
	// Flush crlfBuffer if it ends with a single \r (without a matching \n)
	if len(self.crlfBuffer) > 0 && self.crlfBuffer[len(self.crlfBuffer)-1] == '\r' {
		if _, err := self.writer.Write(self.crlfBuffer); err != nil {
			return err
		}
	}
	self.crlfBuffer = nil

	if _, err := self.writer.Write([]byte(crlf)); err != nil {
		return err
	}
	return nil
}

func (self *simpleCanonicalizer) CanonicalizeBody(writer io.Writer) io.WriteCloser {
	return &simpleBodyCanonicalizer{writer: writer}
}

type relaxedCanonicalizer struct{}

func (self *relaxedCanonicalizer) CanonicalizeHeader(header string) string {
	keyValue := strings.SplitN(header, ":", 2)
	key := strings.TrimSpace(strings.ToLower(keyValue[0]))
	var value string
	if len(keyValue) > 1 {
		value = strings.TrimSpace(reduceWhitespacePattern.ReplaceAllString(keyValue[1], " "))
	}
	return key + ":" + value + crlf
}

type relaxedBodyCanonicalizer struct {
	writer     io.Writer
	crlfBuffer []byte
	wsp        bool
	written    bool
	crlfFixer  crlfFixer
}

func (self *relaxedBodyCanonicalizer) Write(data []byte) (int, error) {
	written := len(data)

	data = self.crlfFixer.Fix(data)

	canonical := make([]byte, 0, len(data))
	for _, character := range data {
		switch character {
		case ' ', '\t':
			self.wsp = true
		case '\r', '\n':
			self.wsp = false
			self.crlfBuffer = append(self.crlfBuffer, character)
		default:
			if len(self.crlfBuffer) > 0 {
				canonical = append(canonical, self.crlfBuffer...)
				self.crlfBuffer = self.crlfBuffer[:0]
			}
			if self.wsp {
				canonical = append(canonical, ' ')
				self.wsp = false
			}

			canonical = append(canonical, character)
		}
	}

	if !self.written && len(canonical) > 0 {
		self.written = true
	}

	_, err := self.writer.Write(canonical)
	return written, err
}

func (self *relaxedBodyCanonicalizer) Close() error {
	if self.written {
		if _, err := self.writer.Write([]byte(crlf)); err != nil {
			return err
		}
	}
	return nil
}

func (self *relaxedCanonicalizer) CanonicalizeBody(writer io.Writer) io.WriteCloser {
	return &relaxedBodyCanonicalizer{writer: writer}
}
