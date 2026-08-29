package mailparse_test

import (
	"bytes"
	"io"
	"net/textproto"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A composed message has to come back out of the same parser that reads
// arriving mail, part for part, or the dashboard shows one thing and the
// recipient gets another.
func TestComposeRoundTripsThroughTraverseParts(t *testing.T) {
	t.Parallel()

	attachment := &mailparse.Attachment{
		Filename:    "notes — 2026.txt",
		ContentType: "text/plain",
		Content:     bytes.Repeat([]byte("attached\n"), 200),
	}

	var body bytes.Buffer
	headers, err := mailparse.Compose(&body, []byte("hello\r\n"), []byte("<p>hello</p>"), []*mailparse.Attachment{attachment})
	if err != nil {
		t.Fatalf("compose failed: %s", err)
	}
	if !strings.HasPrefix(mailparse.FindHeaderValue(headers, "Content-Type"), "multipart/mixed") {
		t.Fatalf("expected a multipart/mixed message, got %q", mailparse.FindHeaderValue(headers, "Content-Type"))
	}
	if mailparse.FindHeaderValue(headers, "MIME-Version") != "1.0" {
		t.Errorf("expected MIME-Version 1.0, got %q", mailparse.FindHeaderValue(headers, "MIME-Version"))
	}

	// No line may be longer than SMTP allows. A base64 part written as one
	// line is the mistake this guards against.
	for _, line := range strings.Split(body.String(), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("a line of %d bytes is longer than SMTP permits", len(line))
		}
	}

	var parts []string
	if err := mailparse.TraverseParts(headers, body.Bytes(), func(header textproto.MIMEHeader, reader io.Reader) error {
		part, err := mailparse.DecodePart(header, reader, 0)
		if err != nil {
			return err
		}
		parts = append(parts, part.ContentType)
		return nil
	}); err != nil {
		t.Fatalf("traverse failed: %s", err)
	}
	expected := []string{"text/plain", "text/html", "text/plain"}
	if strings.Join(parts, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected parts %v, got %v", expected, parts)
	}

	// The attachment is the third part and comes back byte for byte, with
	// the filename it was given, non-ASCII included.
	part, err := mailparse.PartAt(headers, body.Bytes(), 2)
	if err != nil {
		t.Fatalf("cannot read the attachment back: %s", err)
	}
	if !bytes.Equal(part.Content, attachment.Content) {
		t.Errorf("the attachment came back changed")
	}
	if part.Filename != attachment.Filename {
		t.Errorf("expected filename %q, got %q", attachment.Filename, part.Filename)
	}
	if part.Inline {
		t.Errorf("an attachment is not inline")
	}

	text, err := mailparse.PartAt(headers, body.Bytes(), 0)
	if err != nil {
		t.Fatalf("cannot read the text back: %s", err)
	}
	if string(text.Content) != "hello\r\n" {
		t.Errorf("expected the text part back, got %q", text.Content)
	}
}

// One part only when there is one thing to say: a multipart/alternative with
// an empty half is what an old version of the mailer sent, and some clients
// showed the empty half.
func TestComposeSinglePart(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		text, html  string
		contentType string
	}{
		"text": {text: "plain", contentType: "text/plain"},
		"html": {html: "<b>rich</b>", contentType: "text/html"},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var body bytes.Buffer
			headers, err := mailparse.Compose(&body, []byte(testCase.text), []byte(testCase.html), nil)
			if err != nil {
				t.Fatalf("compose failed: %s", err)
			}
			contentType := mailparse.FindHeaderValue(headers, "Content-Type")
			if !strings.HasPrefix(contentType, testCase.contentType) {
				t.Fatalf("expected %s, got %q", testCase.contentType, contentType)
			}
			part, err := mailparse.PartAt(headers, body.Bytes(), 0)
			if err != nil {
				t.Fatalf("cannot read the part back: %s", err)
			}
			if _, err := mailparse.PartAt(headers, body.Bytes(), 1); err == nil {
				t.Errorf("expected exactly one part")
			}
			if string(part.Content) != testCase.text+testCase.html {
				t.Errorf("expected %q back, got %q", testCase.text+testCase.html, part.Content)
			}
		})
	}
}

func TestComposeRefusesNothing(t *testing.T) {
	t.Parallel()
	var body bytes.Buffer
	if _, err := mailparse.Compose(&body, nil, nil, nil); err == nil {
		t.Fatalf("expected an empty message to be refused")
	}
}
