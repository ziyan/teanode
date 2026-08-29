// Package testmail builds synthetic messages and keys for tests.
//
// It exists so that no test needs a captured real message. Fixtures taken from
// a live mailbox carry somebody's correspondence and their address, cannot be
// edited without breaking the very signatures they exist to exercise, and are
// awkward to publish. Messages built here are signed at test time with keys
// generated here, so a test can assert a verdict rather than merely observing
// one, and can construct the failure it wants to check.
package testmail

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
	"testing"
)

// Key returns an RSA key for signing. Each call generates a fresh one: tests
// that need the same key twice should hold on to it.
//
// 1024 bits is below what a real deployment should use and is chosen because
// these keys are thrown away within milliseconds, and generating 2048-bit keys
// for every subtest makes the suite noticeably slower.
func Key(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("failed to generate a key: %s", err)
	}
	return key
}

// PublicKeyRecord returns the DNS TXT value that publishes a key, in the form
// a resolver would return it.
func PublicKeyRecord(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()

	encoded, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("failed to encode the public key: %s", err)
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(encoded)
}

// Message is a synthetic message: headers as mailparse produces them, and a
// body.
type Message struct {
	Headers []string
	Body    []byte
}

// Options describes the message to build. The zero value produces a plausible
// short plain text message.
type Options struct {
	From    string
	To      string
	Subject string

	// Body is the message body. When empty a short one is used.
	Body string

	// Multipart wraps the body in a multipart/alternative with a text and an
	// HTML part, which is what most real mail looks like and exercises the
	// body canonicalizers against something with boundaries in it.
	Multipart bool

	// ExtraHeaders are appended verbatim, for constructing awkward cases such
	// as a duplicated header or one folded across lines.
	ExtraHeaders []string
}

// Build assembles a message. Header order is fixed so that a signature over it
// is reproducible within a test run.
func Build(options *Options) *Message {
	from := options.From
	if from == "" {
		from = "sender@example.net"
	}
	to := options.To
	if to == "" {
		to = "recipient@example.com"
	}
	subject := options.Subject
	if subject == "" {
		subject = "a synthetic message"
	}
	body := options.Body
	if body == "" {
		body = "This message was built by internal/util/testmail.\r\nIt is not from anybody.\r\n"
	}

	headers := []string{
		"Date: Tue, 18 Aug 2026 12:00:00 +0000",
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"Message-ID: <" + messageIdentifier() + "@example.net>",
		"MIME-Version: 1.0",
	}

	if options.Multipart {
		const boundary = "synthetic-boundary-0000"
		headers = append(headers, "Content-Type: multipart/alternative; boundary=\""+boundary+"\"")
		var builder strings.Builder
		builder.WriteString("--" + boundary + "\r\n")
		builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		builder.WriteString(body)
		builder.WriteString("\r\n--" + boundary + "\r\n")
		builder.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
		builder.WriteString("<html><body><p>" + body + "</p></body></html>\r\n")
		builder.WriteString("\r\n--" + boundary + "--\r\n")
		return &Message{
			Headers: append(headers, options.ExtraHeaders...),
			Body:    []byte(builder.String()),
		}
	}

	headers = append(headers, "Content-Type: text/plain; charset=utf-8")
	return &Message{
		Headers: append(headers, options.ExtraHeaders...),
		Body:    []byte(body),
	}
}

// messageIdentifier returns a value unique enough for one test run. It does
// not need to be unpredictable.
func messageIdentifier() string {
	value, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "0"
	}
	return fmt.Sprintf("%x", value)
}

// Resolver answers DKIM and ARC key lookups from a fixed map, so a test never
// touches the network.
type Resolver struct {
	Records map[string][]string
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver {
	return &Resolver{Records: make(map[string][]string)}
}

// Publish records a public key at the name a verifier will look it up under.
func (self *Resolver) Publish(t *testing.T, selector, domain string, key *rsa.PrivateKey) {
	t.Helper()
	self.Records[fmt.Sprintf("%s._domainkey.%s", selector, domain)] = []string{PublicKeyRecord(t, key)}
}

// PublishValue records a literal TXT value, for malformed-record cases.
func (self *Resolver) PublishValue(selector, domain, value string) {
	self.Records[fmt.Sprintf("%s._domainkey.%s", selector, domain)] = []string{value}
}

func (self *Resolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	records, ok := self.Records[name]
	if !ok {
		return nil, fmt.Errorf("testmail: no record published at %q", name)
	}
	return records, nil
}
