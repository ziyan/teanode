package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A message as a model is given it: the headers that matter, the text with
// the HTML boiled down and the quoted history cut, the attachments by name
// and type only, capped. Never the raw MIME and never an attachment's
// contents — a model reads what a person would read, nothing that came
// along with it.

// MessageContext is one message, reduced.
type MessageContext struct {
	MailID      string
	From        string
	To          string
	Cc          string
	Date        string
	Subject     string
	ListKey     string
	Attachments []string
	Text        string
	Truncated   bool

	// Facts the server already established, in a line the model can use:
	// authentication results, whether the sender is a contact, whether a
	// filter flagged it.
	Facts []string
}

// maximumPartBytes bounds one part read from storage, so that a message
// that is mostly a picture does not become a buffer that is mostly a
// picture.
const maximumPartBytes = 4 << 20

// LoadHeaders fills a message's headers from the spool, where they live;
// the row carries the envelope and a few named fields but not the header
// block. Nothing is changed when the message is no longer stored.
func LoadHeaders(ctx context.Context, store storage.Storage, mail *models.Mail) error {
	if mail == nil || len(mail.Headers) > 0 || store == nil {
		return nil
	}
	headers, _, err := store.Get(ctx, mail.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	mail.Headers = headers
	return nil
}

// BuildMessageContext reads a stored message and reduces it.
func BuildMessageContext(ctx context.Context, store storage.Storage, mail *models.Mail, maximumCharacters int, includeQuoted bool) (*MessageContext, error) {
	result := &MessageContext{
		MailID:  mail.ID,
		Subject: mail.Subject,
		ListKey: mail.ListKey,
	}
	result.From = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "From"))
	if result.From == "" {
		result.From = mail.From
	}
	if result.From == "" {
		result.From = mail.Sender
	}
	result.To = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "To"))
	if result.To == "" {
		result.To = strings.Join(mail.Recipients, ", ")
	}
	result.Cc = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "Cc"))
	result.Date = mail.ReceivedAt.Format("2006-01-02 15:04 MST")

	headers, body, err := store.Get(ctx, mail.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			result.Facts = facts(mail)
			result.Text = "(the message body is no longer stored)"
			return result, nil
		}
		return nil, fmt.Errorf("agent: reading message %s: %w", mail.ID, err)
	}
	if len(mail.Headers) == 0 {
		mail.Headers = headers
	}
	result.From = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "From"))
	if result.From == "" {
		result.From = mail.From
	}
	if result.From == "" {
		result.From = mail.Sender
	}
	result.To = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "To"))
	if result.To == "" {
		result.To = strings.Join(mail.Recipients, ", ")
	}
	result.Cc = mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(mail.Headers, "Cc"))
	result.Facts = facts(mail)
	text, html := "", ""
	if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			mediaType, parameters = "text/plain", map[string]string{}
		}
		filename := parameters["name"]
		if disposition := header.Get("Content-Disposition"); disposition != "" {
			if _, dispositionParameters, err := mime.ParseMediaType(disposition); err == nil && dispositionParameters["filename"] != "" {
				filename = dispositionParameters["filename"]
			}
		}
		if filename != "" {
			result.Attachments = append(result.Attachments, fmt.Sprintf("%s (%s)", mailparse.DecodeHeaderValue(filename), mediaType))
			return nil
		}
		if mediaType != "text/plain" && mediaType != "text/html" {
			return nil
		}
		decoded, err := readPart(header, reader)
		if err != nil {
			return nil
		}
		content := string(mailparse.DecodeCharset(decoded, parameters["charset"]))
		if mediaType == "text/plain" && text == "" {
			text = content
		} else if mediaType == "text/html" && html == "" {
			html = content
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("agent: parsing message %s: %w", mail.ID, err)
	}
	if strings.TrimSpace(text) == "" && html != "" {
		text = HTMLToText(html)
	}
	text = normalizeText(text)
	if !includeQuoted {
		text = stripQuoted(text)
	}
	if maximumCharacters > 0 && len(text) > maximumCharacters {
		text = text[:maximumCharacters] + "\n[… cut here: the message goes on]"
		result.Truncated = true
	}
	result.Text = strings.TrimSpace(text)
	return result, nil
}

func readPart(header textproto.MIMEHeader, reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maximumPartBytes)
	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		content, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		cleaned := strings.Map(func(character rune) rune {
			if character == '\r' || character == '\n' || character == ' ' || character == '\t' {
				return -1
			}
			return character
		}, string(content))
		return base64.StdEncoding.DecodeString(cleaned)
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(limited))
	default:
		return io.ReadAll(limited)
	}
}

// HTMLToText reduces HTML to the text a person would read: block elements
// become line breaks, links keep their text, everything in a script or a
// style goes.
func HTMLToText(html string) string {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return stripTags(html)
	}
	document.Find("script, style, head, noscript").Remove()
	document.Find("br").Each(func(_ int, selection *goquery.Selection) { selection.ReplaceWithHtml("\n") })
	document.Find("p, div, li, tr, h1, h2, h3, h4, h5, h6, blockquote, pre, table").Each(func(_ int, selection *goquery.Selection) {
		selection.AppendHtml("\n")
		selection.PrependHtml("\n")
	})
	return document.Text()
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func stripTags(html string) string {
	return tagPattern.ReplaceAllString(html, " ")
}

var blankLines = regexp.MustCompile(`\n{3,}`)

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

var quoteIntroductions = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^On .{5,200}wrote:\s*$`),
	regexp.MustCompile(`(?im)^-{2,}\s*Original Message\s*-{2,}\s*$`),
	regexp.MustCompile(`(?im)^From: .+\nSent: .+`),
	regexp.MustCompile(`(?im)^Le .{5,200}a écrit\s*:\s*$`),
	regexp.MustCompile(`(?im)^Am .{5,200}schrieb .+:\s*$`),
}

// stripQuoted cuts the quoted history: everything from the first line that
// introduces it, and any line quoted with ">" before that.
func stripQuoted(text string) string {
	cut := len(text)
	for _, pattern := range quoteIntroductions {
		if location := pattern.FindStringIndex(text); location != nil && location[0] < cut {
			cut = location[0]
		}
	}
	text = text[:cut]
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// facts is what the server already knows about a message, said plainly.
func facts(mail *models.Mail) []string {
	var lines []string
	results := mail.AuthenticationResults
	if results.SPF != nil {
		lines = append(lines, "SPF: "+results.SPF.Result)
	}
	if len(results.DKIMs) > 0 {
		dkim := results.DKIMs[0].Result
		for _, candidate := range results.DKIMs {
			if candidate != nil && candidate.Result == "pass" {
				dkim = "pass"
			}
		}
		lines = append(lines, "DKIM: "+dkim)
	}
	if results.DMARC != nil {
		line := "DMARC: " + results.DMARC.Result
		if results.DMARC.Policy != "" {
			line += " (policy " + results.DMARC.Policy + ")"
		}
		lines = append(lines, line)
	}
	if results.SpamFilter != nil {
		lines = append(lines, fmt.Sprintf("spam filter: %s, score %.1f", results.SpamFilter.Result, results.SpamFilter.Score))
	}
	if mail.ListKey != "" {
		lines = append(lines, "from a mailing list: "+mail.ListKey)
	}
	if strings.Contains(strings.ToLower(mailparse.FindHeaderValue(mail.Headers, "Auto-Submitted")), "auto-") {
		lines = append(lines, "automatic message (Auto-Submitted)")
	}
	if precedence := strings.ToLower(mailparse.FindHeaderValue(mail.Headers, "Precedence")); precedence == "bulk" || precedence == "list" || precedence == "junk" {
		lines = append(lines, "Precedence: "+precedence)
	}
	if mailparse.FindHeaderValue(mail.Headers, "List-Unsubscribe") != "" {
		lines = append(lines, "carries a List-Unsubscribe header")
	}
	return lines
}

// Render writes the context the way the prompts include it.
func (self *MessageContext) Render() string {
	var builder strings.Builder
	builder.WriteString("From: " + self.From + "\n")
	if self.To != "" {
		builder.WriteString("To: " + self.To + "\n")
	}
	if self.Cc != "" {
		builder.WriteString("Cc: " + self.Cc + "\n")
	}
	builder.WriteString("Date: " + self.Date + "\n")
	builder.WriteString("Subject: " + self.Subject + "\n")
	if len(self.Attachments) > 0 {
		builder.WriteString("Attachments: " + strings.Join(self.Attachments, "; ") + "\n")
	}
	if len(self.Facts) > 0 {
		builder.WriteString("Server facts: " + strings.Join(self.Facts, "; ") + "\n")
	}
	builder.WriteString("\n")
	if self.Text == "" {
		builder.WriteString("(no text)")
	} else {
		builder.WriteString(self.Text)
	}
	return builder.String()
}
