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

func (c *simpleBodyCanonicalizer) Write(b []byte) (int, error) {
	written := len(b)
	b = append(c.crlfBuffer, b...)

	b = c.crlfFixer.Fix(b)

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

	c.crlfBuffer = b[end:]

	var err error
	if end > 0 {
		_, err = c.w.Write(b[:end])
	}
	return written, err
}

func (c *simpleBodyCanonicalizer) Close() error {
	// Flush crlfBuffer if it ends with a single \r (without a matching \n)
	if len(c.crlfBuffer) > 0 && c.crlfBuffer[len(c.crlfBuffer)-1] == '\r' {
		if _, err := c.w.Write(c.crlfBuffer); err != nil {
			return err
		}
	}
	c.crlfBuffer = nil

	if _, err := c.w.Write([]byte(crlf)); err != nil {
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

func (c *relaxedBodyCanonicalizer) Write(b []byte) (int, error) {
	written := len(b)

	b = c.crlfFixer.Fix(b)

	canonical := make([]byte, 0, len(b))
	for _, channel := range b {
		switch channel {
		case ' ', '\t':
			c.wsp = true
		case '\r', '\n':
			c.wsp = false
			c.crlfBuffer = append(c.crlfBuffer, channel)
		default:
			if len(c.crlfBuffer) > 0 {
				canonical = append(canonical, c.crlfBuffer...)
				c.crlfBuffer = c.crlfBuffer[:0]
			}
			if c.wsp {
				canonical = append(canonical, ' ')
				c.wsp = false
			}

			canonical = append(canonical, channel)
		}
	}

	if !c.written && len(canonical) > 0 {
		c.written = true
	}

	_, err := c.w.Write(canonical)
	return written, err
}

func (c *relaxedBodyCanonicalizer) Close() error {
	if c.written {
		if _, err := c.w.Write([]byte(crlf)); err != nil {
			return err
		}
	}
	return nil
}

func (self *relaxedCanonicalizer) CanonicalizeBody(w io.Writer) io.WriteCloser {
	return &relaxedBodyCanonicalizer{w: w}
}
