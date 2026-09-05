package apigraph

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// maximumRenderedBytes caps how much of one part is returned. A message can be
// tens of megabytes, and no dashboard needs to display that; the raw source is
// available separately for anyone who really wants it.
const maximumRenderedBytes = 2 * 1024 * 1024

type ContentQuery interface {
	// Get the content of a stored Mail, decoded and ready to display
	GetMailContent(ctx context.Context, arguments GetMailContentArguments) (*MailContent, error)
}

// MailContent is a stored message, taken apart for display.
type MailContent struct {
	// ID of the Mail
	MailID string `json:"mailId"`

	// Whether the message is still stored. Retention removes old messages, and
	// one received before storage was configured was never written.
	Available bool `json:"available"`

	// Plain text part, decoded
	Text string `json:"text,omitempty"`

	// HTML part, with anything that could act on the reader removed
	HTML string `json:"html,omitempty"`

	// Whether the HTML referred to remote images, which are not loaded
	HasRemoteContent bool `json:"hasRemoteContent"`

	// Files attached to the message
	Attachments []*Attachment `json:"attachments,omitempty"`

	// Headers worth showing above the message
	Headers []*Header `json:"headers,omitempty"`

	// RawHeaders is the header block exactly as it arrived, for when the
	// parsed view has folded or decoded away the thing being investigated.
	RawHeaders string `json:"rawHeaders,omitempty"`

	// Size of the stored message in bytes
	Size int `json:"size"`
}

// Attachment describes a file attached to a message. The content is not
// included; it is fetched separately.
type Attachment struct {
	// Index in the message, which is how it is fetched: the message is
	// stored and never changes, so its parts keep their order.
	Index int `json:"index"`

	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`

	// Inline attachments are referenced from the HTML rather than listed.
	Inline bool `json:"inline"`
}

// Header is one header line, decoded from any transfer encoding.
type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// interestingHeaders are listed first, in this order, because they are what a
// reader is usually looking for. Everything else follows in the order it
// appeared — nothing is hidden. Which header explains a delivery is not
// something this code can know: an allowlist here meant that the one you
// needed was always the one missing.
var interestingHeaders = []string{
	"Date", "From", "Reply-To", "To", "Cc", "Subject", "Message-ID",
	"List-Unsubscribe", "Auto-Submitted", "Precedence",
}

// describeHeaders returns every header on the message: the ones worth reading
// first, in a fixed order, then the rest as they appeared.
//
// A header can legitimately appear more than once — Received is the obvious
// one, and the whole point of it is the sequence — so this walks the lines
// rather than looking each name up.
func describeHeaders(headers []string) []*Header {
	described := make([]*Header, 0, len(headers))
	taken := make([]bool, len(headers))

	appendHeader := func(index int) {
		key, value, ok := splitHeader(headers[index])
		if !ok {
			return
		}
		taken[index] = true
		described = append(described, &Header{
			Key:   key,
			Value: strings.TrimSpace(mailparse.DecodeHeaderValue(value)),
		})
	}

	for _, wanted := range interestingHeaders {
		for index, header := range headers {
			if taken[index] {
				continue
			}
			if key, _, ok := splitHeader(header); ok && strings.EqualFold(key, wanted) {
				appendHeader(index)
			}
		}
	}
	for index := range headers {
		if !taken[index] {
			appendHeader(index)
		}
	}
	return described
}

// splitHeader divides a header line at the first colon. A line without one is
// a continuation that the parser should already have folded in, or rubbish;
// either way there is no name to show it under.
func splitHeader(header string) (string, string, bool) {
	key, value, ok := strings.Cut(header, ":")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", false
	}
	return key, value, true
}

type GetMailContentArguments struct {
	// ID of the Mail
	MailID string `json:"mailId"`
}

func (self *graph) GetMailContent(ctx context.Context, arguments GetMailContentArguments) (*MailContent, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	mail, err := api.ContextTransaction(ctx).GetMail(arguments.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}
	// The domain may have been removed since; that is not a reason to hide
	// mail that was received while it existed.
	if mail.DomainID != "" && self.config.Current().FindDomainByID(mail.DomainID) == nil {
		log.Debugf("showing mail %q whose domain %q is no longer configured", mail.ID, mail.DomainID)
	}

	headers, body, err := self.storage.Get(ctx, mail.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return &MailContent{MailID: mail.ID, Available: false}, nil
		}
		return nil, err
	}

	return renderContent(mail.ID, headers, body)
}

func renderContent(mailId string, headers []string, body []byte) (*MailContent, error) {
	content := &MailContent{
		MailID:    mailId,
		Available: true,
		Size:      len(body),
	}

	content.Headers = describeHeaders(headers)
	content.RawHeaders = strings.Join(headers, "\r\n")

	partIndex := 0
	inlineParts := map[string]int{}

	// Walk the MIME tree and take the first text and HTML part, which is what
	// a mail client displays. Everything with a filename is an attachment.
	if err := mailparse.TraverseParts(headers, body, func(header textproto.MIMEHeader, reader io.Reader) error {
		mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
		if err != nil {
			// A part with no usable content type is treated as plain text,
			// which is what RFC 2045 says to assume.
			mediaType = "text/plain"
			parameters = map[string]string{}
		}

		filename := parameters["name"]
		if disposition := header.Get("Content-Disposition"); disposition != "" {
			if _, dispositionParameters, err := mime.ParseMediaType(disposition); err == nil {
				if dispositionParameters["filename"] != "" {
					filename = dispositionParameters["filename"]
				}
			}
		}

		decoded, err := readPart(header, reader)
		if err != nil {
			log.Warningf("failed to decode a part of mail %q: %s", mailId, err)
			return nil
		}

		// Every part gets a number, whether or not it turns out to be an
		// attachment, so the number is the same one the endpoint counts to.
		index := partIndex
		partIndex++

		if filename != "" {
			// A part referenced from the HTML carries a Content-ID; that is
			// what "cid:" in an img src points at.
			contentId := strings.Trim(header.Get("Content-ID"), "<>")
			if contentId != "" {
				inlineParts[contentId] = index
			}
			content.Attachments = append(content.Attachments, &Attachment{
				Index:       index,
				Filename:    mailparse.DecodeHeaderValue(filename),
				ContentType: mediaType,
				Size:        len(decoded),
				Inline:      strings.HasPrefix(strings.ToLower(header.Get("Content-Disposition")), "inline"),
			})
			return nil
		}

		switch mediaType {
		case "text/plain":
			if content.Text == "" {
				content.Text = string(decoded)
			}
		case "text/html":
			if content.HTML == "" {
				sanitized, hasRemote := sanitizeHtml(string(decoded))
				content.HTML = sanitized
				content.HasRemoteContent = hasRemote
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("apigraph: cannot read the message: %w", err)
	}

	// An image the message brought with it is part of the message, so it is
	// pointed at the attachment that holds it rather than left as a cid: URL
	// the browser cannot resolve. This is not the tracking-pixel case: no
	// request leaves this server, and the sender learns nothing.
	content.HTML = resolveInlineParts(content.HTML, mailId, inlineParts)

	return content, nil
}

// inlineReference matches a cid: URL in an attribute, which is how HTML mail
// refers to an image it carried along with it.
var inlineReference = regexp.MustCompile(`(?i)(src|href)\s*=\s*["']cid:([^"']+)["']`)

// resolveInlineParts rewrites those references to the attachment endpoint.
func resolveInlineParts(html string, mailId string, parts map[string]int) string {
	if html == "" || len(parts) == 0 {
		return html
	}
	return inlineReference.ReplaceAllStringFunc(html, func(match string) string {
		groups := inlineReference.FindStringSubmatch(match)
		if groups == nil {
			return match
		}
		index, ok := parts[strings.Trim(groups[2], "<> ")]
		if !ok {
			// A reference to a part that is not here. Left as it was, so it
			// fails visibly rather than pointing at the wrong attachment.
			return match
		}
		return fmt.Sprintf(`%s="%s"`, groups[1], api.MailAttachmentPath(mailId, index))
	})
}

func readPart(header textproto.MIMEHeader, reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maximumRenderedBytes)

	switch strings.ToLower(strings.TrimSpace(header.Get("Content-Transfer-Encoding"))) {
	case "base64":
		content, err := io.ReadAll(limited)
		if err != nil {
			return nil, err
		}
		return mailparse.DecodeBase64String(string(content))
	case "quoted-printable":
		return io.ReadAll(quotedPrintableReader(limited))
	default:
		return io.ReadAll(limited)
	}
}
