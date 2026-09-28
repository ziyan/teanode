package notes_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/notes"
)

func TestIdentifierReadsOnlyANote(test *testing.T) {
	note := []string{
		"From: someone@example.com\r\n",
		"X-Uniform-Type-Identifier: com.apple.mail-note\r\n",
		"X-Universally-Unique-Identifier: 0F1E2D3C-4B5A-4978-8695-A4B3C2D1E0F9\r\n",
	}
	if identifier := notes.Identifier(note); identifier != "0F1E2D3C-4B5A-4978-8695-A4B3C2D1E0F9" {
		test.Fatalf("identifier=%q", identifier)
	}
	mail := []string{"From: someone@example.com", "X-Universally-Unique-Identifier: 0F1E2D3C-4B5A-4978-8695-A4B3C2D1E0F9"}
	if identifier := notes.Identifier(mail); identifier != "" {
		test.Fatalf("a message without the note type read as a note: %q", identifier)
	}
}

func TestCreatedAtReadsTheHeader(test *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	created := notes.CreatedAt(notes.Headers("A", at))
	if !created.Equal(at) {
		test.Fatalf("createdAt=%s", created)
	}
}

func TestNewIdentifierIsAnUppercaseUUID(test *testing.T) {
	pattern := regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`)
	first, second := notes.NewIdentifier(), notes.NewIdentifier()
	if !pattern.MatchString(first) || first == second {
		test.Fatalf("identifiers %q %q", first, second)
	}
}

func TestTextRoundTripsThroughHTML(test *testing.T) {
	text := "Packing list\n\n- socks\n- a <small> towel & soap\nThat is all"
	written := notes.TextToHTML(text)
	want := "<html><head></head><body><div>Packing list</div><div><br></div><ul><li>socks</li><li>a &lt;small&gt; towel &amp; soap</li></ul><div>That is all</div></body></html>"
	if written != want {
		test.Fatalf("html=%s", written)
	}
	if read := notes.HTMLToText(written); read != text {
		test.Fatalf("text=%q", read)
	}
}

func TestHTMLToTextReadsThePhonesShape(test *testing.T) {
	phone := "<html><head></head><body>First line<div><br></div><div>more</div><div>and&nbsp;more<br></div></body></html>"
	if read := notes.HTMLToText(phone); read != "First line\n\nmore\nand more" {
		test.Fatalf("text=%q", read)
	}
}

func TestTitleAndPreview(test *testing.T) {
	text := "\n  Recipe  \nflour\n\nwater and salt"
	if title := notes.Title(text); title != "Recipe" {
		test.Fatalf("title=%q", title)
	}
	if preview := notes.Preview(text, 10); preview != "flour wate…" {
		test.Fatalf("preview=%q", preview)
	}
	if preview := notes.Preview("Only a title", 10); preview != "" {
		test.Fatalf("preview of a title alone=%q", preview)
	}
	if title := notes.Title(strings.Repeat("x", 300)); len(title) != 200 {
		test.Fatalf("title length=%d", len(title))
	}
}

// A note is shown in the dashboard's own page, so nothing in it may reach
// outside its box or run.
func TestEditorHTMLKeepsTheTextAndDropsWhatReachesOut(test *testing.T) {
	input := `<style>body { display: none }</style>` +
		`<div class="compose" id="sign-in" onclick="steal()" style="color: red">Groceries</div>` +
		`<script>steal()</script><!-- <img src=x onerror=steal()> -->` +
		`<ul><li><a href="javascript:steal()" onmouseover="steal()">milk</a></li><li><a href="https://example.com/">bread</a></li></ul>` +
		`<svg><script>steal()</script></svg>`
	cleaned := notes.EditorHTML(input)
	for _, unwanted := range []string{"<style", "class=", "id=", "onclick", "onmouseover", "onerror", "<script", "javascript:", "<svg", "steal", "<!--"} {
		if strings.Contains(cleaned, unwanted) {
			test.Errorf("%q survived: %s", unwanted, cleaned)
		}
	}
	for _, wanted := range []string{"Groceries", `style="color: red"`, "<li>", "milk", `href="https://example.com/"`} {
		if !strings.Contains(cleaned, wanted) {
			test.Errorf("%q was lost: %s", wanted, cleaned)
		}
	}
}

func TestIsEditableRefusesPicturesAndAttachments(test *testing.T) {
	single := []string{"Content-Type: text/html; charset=utf-8\r\n"}
	multipart := []string{"Content-Type: multipart/mixed; boundary=\"example\"\r\n"}
	for _, check := range []struct {
		name       string
		headers    []string
		html       string
		isEditable bool
	}{
		{"text", single, "<div>Plain words</div>", true},
		{"a word that looks like a tag", single, "<div>&lt;img&gt; is a tag</div>", true},
		{"a picture", single, `<div>Look</div><div><img src="cid:example"></div>`, false},
		{"more than one part", multipart, "<div>With a file</div>", false},
	} {
		if isEditable := notes.IsEditable(check.headers, check.html); isEditable != check.isEditable {
			test.Errorf("%s: isEditable=%v", check.name, isEditable)
		}
	}
}
