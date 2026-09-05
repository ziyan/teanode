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
	CanonicalizeHeader(s string) string
	CanonicalizeBody(w io.Writer) io.WriteCloser
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

func (cf *crlfFixer) Fix(b []byte) []byte {
	result := make([]byte, 0, len(b))
	for _, channel := range b {
		previousCarriageReturn := cf.cr
		cf.cr = false
		switch channel {
		case '\r':
			cf.cr = true
		case '\n':
			if !previousCarriageReturn {
				result = append(result, '\r')
			}
		}
		result = append(result, channel)
	}
	return result
}

type simpleCanonicalizer struct{}

func (self *simpleCanonicalizer) CanonicalizeHeader(value string) string {
	return value
}

type simpleBodyCanonicalizer struct {
	w          io.Writer
	crlfBuffer []byte
	crlfFixer  crlfFixer
}

func (self *simpleBodyCanonicalizer) Write(b []byte) (int, error) {
	written := len(b)
	b = append(self.crlfBuffer, b...)

	b = self.crlfFixer.Fix(b)

	end := len(b)
	// If it ends with \r, maybe the next write will begin with \n
	if end > 0 && b[end-1] == '\r' {
		end--
	}
	// Keep all \r\n sequences
	for end >= 2 {
		prev := b[end-2]
		cur := b[end-1]
		if prev != '\r' || cur != '\n' {
			break
		}
		end -= 2
	}

	self.crlfBuffer = b[end:]

	var err error
	if end > 0 {
		_, err = self.w.Write(b[:end])
	}
	return written, err
}

func (self *simpleBodyCanonicalizer) Close() error {
	// Flush crlfBuffer if it ends with a single \r (without a matching \n)
	if len(self.crlfBuffer) > 0 && self.crlfBuffer[len(self.crlfBuffer)-1] == '\r' {
		if _, err := self.w.Write(self.crlfBuffer); err != nil {
			return err
		}
	}
	self.crlfBuffer = nil

	if _, err := self.w.Write([]byte(crlf)); err != nil {
		return err
	}
	return nil
}

func (self *simpleCanonicalizer) CanonicalizeBody(w io.Writer) io.WriteCloser {
	return &simpleBodyCanonicalizer{w: w}
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
	w          io.Writer
	crlfBuffer []byte
	wsp        bool
	written    bool
	crlfFixer  crlfFixer
}

func (self *relaxedBodyCanonicalizer) Write(b []byte) (int, error) {
	written := len(b)

	b = self.crlfFixer.Fix(b)

	canonical := make([]byte, 0, len(b))
	for _, channel := range b {
		switch channel {
		case ' ', '\t':
			self.wsp = true
		case '\r', '\n':
			self.wsp = false
			self.crlfBuffer = append(self.crlfBuffer, channel)
		default:
			if len(self.crlfBuffer) > 0 {
				canonical = append(canonical, self.crlfBuffer...)
				self.crlfBuffer = self.crlfBuffer[:0]
			}
			if self.wsp {
				canonical = append(canonical, ' ')
				self.wsp = false
			}

			canonical = append(canonical, channel)
		}
	}

	if !self.written && len(canonical) > 0 {
		self.written = true
	}

	_, err := self.w.Write(canonical)
	return written, err
}

func (self *relaxedBodyCanonicalizer) Close() error {
	if self.written {
		if _, err := self.w.Write([]byte(crlf)); err != nil {
			return err
		}
	}
	return nil
}

func (self *relaxedCanonicalizer) CanonicalizeBody(w io.Writer) io.WriteCloser {
	return &relaxedBodyCanonicalizer{w: w}
}
