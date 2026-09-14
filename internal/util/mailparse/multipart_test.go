package mailparse_test

import (
	"bytes"
	"errors"
	"fmt"
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

// A message can nest multipart bodies as deep as its sender likes, at about
// fifty bytes a level, and each level wraps a reader around the one above
// it. Walking a million levels is hours of work over a small message, and
// the parts are read on receipt as well as in the dashboard.
func TestTraverseRefusesAMessageNestedTooDeeply(t *testing.T) {
	t.Parallel()

	nested := func(depth int) ([]string, []byte) {
		var body bytes.Buffer
		for level := 1; level <= depth; level++ {
			fmt.Fprintf(&body, "--b%d\r\nContent-Type: multipart/mixed; boundary=b%d\r\n\r\n", level-1, level)
		}
		fmt.Fprintf(&body, "--b%d\r\nContent-Type: text/plain\r\n\r\nhello\r\n--b%d--\r\n", depth, depth)
		for level := depth - 1; level >= 0; level-- {
			fmt.Fprintf(&body, "--b%d--\r\n", level)
		}
		return []string{"Content-Type: multipart/mixed; boundary=b0"}, body.Bytes()
	}

	const depth = 200
	headers, body := nested(depth)
	parts := 0
	err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		parts++
		_, _ = io.Copy(io.Discard, reader)
		return nil
	})
	if err == nil {
		t.Fatalf("a message nested %d deep was walked, %d parts", depth, parts)
	}
	if err != mailparse.ErrTooDeeplyNested {
		t.Errorf("got %v, want %v", err, mailparse.ErrTooDeeplyNested)
	}

	// A few levels is what real mail does, and must still be read.
	headers, body = nested(4)
	parts = 0
	if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		parts++
		return nil
	}); err != nil {
		t.Fatalf("a message nested four deep failed: %s", err)
	}
	if parts != 1 {
		t.Errorf("got %d parts, want 1", parts)
	}
}

// Depth was bounded and breadth was not.
//
// A megabyte of "--b" repeated is two hundred thousand parts, and the callers
// are what makes that expensive: one opens a connection to the virus scanner
// per part, another decodes every compressed part it finds. So a message
// nobody looks twice at became two hundred thousand connections, and virus
// scanning silently stopped for everybody else while it ran.
func TestAMessageOfNothingButBoundariesIsRefused(t *testing.T) {
	var body bytes.Buffer
	body.WriteString("--b\r\nContent-Type: text/plain\r\n\r\nhello\r\n")
	for index := 0; index < 5000; index++ {
		body.WriteString("--b\r\n\r\n")
	}
	body.WriteString("--b--\r\n")

	walked := 0
	err := mailparse.TraverseParts(
		[]string{"Content-Type: multipart/mixed; boundary=b"}, body.Bytes(),
		func(_ textproto.MIMEHeader, _ io.Reader) error {
			walked++
			return nil
		})
	if !errors.Is(err, mailparse.ErrTooManyParts) {
		t.Fatalf("a message with thousands of parts is refused: %v after %d", err, walked)
	}
	if walked > 1001 {
		t.Fatalf("and refused before doing the work: %d parts walked", walked)
	}

	// An ordinary message is untouched.
	walked = 0
	ordinary := "--b\r\nContent-Type: text/plain\r\n\r\nhello\r\n" +
		"--b\r\nContent-Type: text/html\r\n\r\n<p>hello</p>\r\n--b--\r\n"
	if err := mailparse.TraverseParts(
		[]string{"Content-Type: multipart/alternative; boundary=b"}, []byte(ordinary),
		func(_ textproto.MIMEHeader, _ io.Reader) error {
			walked++
			return nil
		}); err != nil || walked != 2 {
		t.Fatalf("two parts, no complaint: %d %v", walked, err)
	}
}

// A picture the body refers to travels with the body.
//
// A message styled well enough to be worth sending usually has something in
// it — a mark, a chart, a photograph — and a picture written as an ordinary
// attachment arrives at the bottom with a broken image where it should be.
// It belongs in a multipart/related beside the body, with a Content-ID for
// the cid: in the HTML to find.
func TestAPictureTheBodyRefersToIsPartOfIt(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	headers, err := mailparse.Compose(&body, []byte("Figures attached."), []byte(`<p><img src="cid:chart.png"></p>`), []*mailparse.Attachment{
		{Filename: "chart.png", ContentType: "image/png", Content: []byte("PNG"), ContentID: "chart.png"},
		{Filename: "figures.csv", ContentType: "text/csv", Content: []byte("a,b\n")},
	})
	if err != nil {
		t.Fatalf("Compose: %s", err)
	}
	written := strings.Join(headers, "\r\n") + "\r\n" + body.String()
	// The outside is the mixed wrapper, because there is a file to open.
	if !strings.Contains(written, "multipart/mixed") {
		t.Fatalf("a file to open makes a mixed message:\n%s", written)
	}
	if !strings.Contains(written, "multipart/related") || !strings.Contains(written, "multipart/alternative") {
		t.Fatalf("the body and its picture are related, and the body has both forms:\n%s", written)
	}
	if !strings.Contains(written, "Content-ID: <chart.png>") {
		t.Fatalf("the picture is named for cid: to find:\n%s", written)
	}
	if !strings.Contains(written, `Content-Disposition: inline; filename=chart.png`) {
		t.Fatalf("the picture belongs to the body:\n%s", written)
	}
	if !strings.Contains(written, `Content-Disposition: attachment; filename=figures.csv`) {
		t.Fatalf("and the file is still a file:\n%s", written)
	}

	// Read back: the picture is an inline part with its identifier, the file
	// is an attachment. This is what makes a draft survive being saved.
	parts := []*mailparse.Part{}
	if err := mailparse.TraverseParts(headers, body.Bytes(), func(header textproto.MIMEHeader, reader io.Reader) error {
		part, err := mailparse.DecodePart(header, reader, 0)
		if err != nil {
			return err
		}
		parts = append(parts, part)
		return nil
	}); err != nil {
		t.Fatalf("TraverseParts: %s", err)
	}
	var picture, file *mailparse.Part
	for _, part := range parts {
		switch part.Filename {
		case "chart.png":
			picture = part
		case "figures.csv":
			file = part
		}
	}
	if picture == nil || !picture.Inline || picture.ContentID != "chart.png" {
		t.Fatalf("the picture reads back inline and named: %+v", picture)
	}
	if file == nil || file.Inline {
		t.Fatalf("the file reads back as a file: %+v", file)
	}
}

// With nothing to open, there is no mixed wrapper: a message that is a body
// and the pictures in it is a multipart/related and no more. A wrapper
// holding one thing is what makes some clients show a message as an
// attachment.
func TestAPictureAloneNeedsNoMixedWrapper(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	headers, err := mailparse.Compose(&body, nil, []byte(`<img src="cid:logo.png">`), []*mailparse.Attachment{
		{Filename: "logo.png", ContentType: "image/png", Content: []byte("PNG"), ContentID: "logo.png"},
	})
	if err != nil {
		t.Fatalf("Compose: %s", err)
	}
	written := strings.Join(headers, "\r\n")
	if strings.Contains(written, "multipart/mixed") {
		t.Fatalf("nothing to open, so nothing to wrap: %s", written)
	}
	if !strings.Contains(written, "multipart/related") || !strings.Contains(written, `type="text/html"`) {
		t.Fatalf("the body and its picture, and where a reader starts: %s", written)
	}
}
