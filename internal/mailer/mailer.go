// Package mailer sends mail on this server's own behalf: a message somebody
// composed in the dashboard, or a template rendered for an application that
// called the send endpoint.
package mailer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/aymerick/douceur/inliner"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
	"github.com/ziyan/teanode/internal/util/templating"
)

var log = logging.MustGetLogger("mailer")

// Message is what there is to send, before it is a message: who it is from
// and to, and its content in whichever forms are present.
type Message struct {
	// From is the address to send as. It has to be at a configured domain,
	// because the domain's key is what signs the message.
	From string

	// FromName is the display name beside it, if any.
	FromName string

	To  []string
	Cc  []string
	Bcc []string

	Subject string

	// Text and HTML are the two forms of the body. Either may be empty;
	// both empty is allowed only with an attachment.
	Text string
	HTML string

	Attachments []*mailparse.Attachment

	// Language the content is in, when known. Written as Content-Language.
	Language string

	// Headers are further header lines to write as given — In-Reply-To and
	// References on a reply, Bcc on a draft — after the ones built here.
	Headers []string
}

// Composed is a message built but not sent: what a draft stores.
type Composed struct {
	ID      string
	Domain  *models.Domain
	Headers []string
	Body    []byte
}

// Rendered is a template with its variables filled in.
type Rendered struct {
	// Subject line, rendered
	Subject string `json:"subject"`

	// HTML content, rendered, with its stylesheet inlined
	HTMLContent string `json:"htmlContent"`

	// Text content, rendered
	TextContent string `json:"textContent"`

	// Locale the content is in: the translation chosen, the template's own
	// locale when its default content was used, or empty when it has none
	Locale string `json:"locale"`

	// Names of the variables the template and its layout read, in every
	// locale, so a caller can see what the content it is previewing needs
	Variables []string `json:"variables"`
}

type Mailer interface {
	Close() error

	// Send builds a message and hands it to the exchange to sign, record and
	// deliver. The envelope carries where it came from — address, TLS,
	// credential — and comes back filled in.
	Send(ctx context.Context, envelope *mailparse.Envelope, message *Message) error

	// Compose builds the message as Send would and returns it instead of
	// sending it, for a draft that is stored as the message it will become.
	Compose(ctx context.Context, message *Message) (*Composed, error)

	// SendMail renders a template of the sender's domain in a locale and
	// sends it to the envelope's recipients.
	SendMail(ctx context.Context, envelope *mailparse.Envelope, templateName string, locale string, variables map[string]interface{}) error
}

type mailer struct {
	database db.Database
	config   config.Store
	exchange mx.Exchange
}

// New builds the mailer. The settings argument is gone: the only two it had
// were a default sender to fall back on, and nothing ever fell back — the one
// caller of SendMail has always supplied an address.
func New(database db.Database, configuration config.Store, exchange mx.Exchange, _ any) (Mailer, error) {
	return &mailer{
		database: database,
		config:   configuration,
		exchange: exchange,
	}, nil
}

func (self *mailer) Close() error {
	return nil
}

// Render fills a template in, choosing the translation closest to the locale
// asked for and falling back to the template's default content.
//
// The layout is chosen independently: a template translated into Japanese
// inside a layout that is not still renders in Japanese, inside the default
// layout, rather than falling back to the default of both.
func Render(template *models.Template, layout *models.Layout, locale string, variables map[string]interface{}) (*Rendered, error) {
	subjectSource, htmlSource, textSource, chosen := chooseTemplateContent(template, locale)

	var layoutHtml, layoutText string
	if layout != nil {
		layoutHtml, layoutText = chooseLayoutContent(layout, locale)
	}

	rendered := &Rendered{Locale: chosen, Variables: templating.Variables(sourcesOf(template, layout)...)}

	subjectBuffer, releaseSubjectBuffer := bufferpool.AcquireBuffer()
	defer releaseSubjectBuffer()
	if err := templating.Render(subjectBuffer, variables, subjectSource); err != nil {
		return nil, fmt.Errorf("mailer: subject: %w", err)
	}
	rendered.Subject = subjectBuffer.String()

	htmlBuffer, releaseHtmlBuffer := bufferpool.AcquireBuffer()
	defer releaseHtmlBuffer()
	if err := renderInLayout(htmlBuffer, variables, layoutHtml, htmlSource); err != nil {
		return nil, fmt.Errorf("mailer: html: %w", err)
	}
	rendered.HTMLContent = inlineStyles(htmlBuffer.String())

	textBuffer, releaseTextBuffer := bufferpool.AcquireBuffer()
	defer releaseTextBuffer()
	if err := renderInLayout(textBuffer, variables, layoutText, textSource); err != nil {
		return nil, fmt.Errorf("mailer: text: %w", err)
	}
	rendered.TextContent = textBuffer.String()

	return rendered, nil
}

// renderInLayout renders content inside a layout, or on its own when the
// layout has nothing. A layout with no content and a template written as
// blocks would render to nothing at all, because a block only appears where
// its parent places it.
func renderInLayout(writer *bytes.Buffer, variables map[string]interface{}, layout, content string) error {
	if strings.TrimSpace(layout) == "" {
		return templating.Render(writer, variables, content)
	}
	return templating.Render(writer, variables, layout, content)
}

// sourcesOf is every piece of template text that could read a variable.
func sourcesOf(template *models.Template, layout *models.Layout) []string {
	sources := []string{template.Subject, template.HTMLContent, template.TextContent}
	for _, translation := range template.Translations {
		sources = append(sources, translation.Subject, translation.HTMLContent, translation.TextContent)
	}
	if layout != nil {
		sources = append(sources, layout.HTMLContent, layout.TextContent)
		for _, translation := range layout.Translations {
			sources = append(sources, translation.HTMLContent, translation.TextContent)
		}
	}
	return sources
}

func chooseTemplateContent(template *models.Template, locale string) (subject, html, text, chosen string) {
	locales := make([]string, 0, len(template.Translations))
	for _, translation := range template.Translations {
		locales = append(locales, translation.Locale)
	}
	if matched, ok := templating.MatchLocale(locale, locales); ok {
		for _, translation := range template.Translations {
			if translation.Locale == matched {
				return translation.Subject, translation.HTMLContent, translation.TextContent, translation.Locale
			}
		}
	}
	return template.Subject, template.HTMLContent, template.TextContent, template.Locale
}

func chooseLayoutContent(layout *models.Layout, locale string) (html, text string) {
	locales := make([]string, 0, len(layout.Translations))
	for _, translation := range layout.Translations {
		locales = append(locales, translation.Locale)
	}
	if matched, ok := templating.MatchLocale(locale, locales); ok {
		for _, translation := range layout.Translations {
			if translation.Locale == matched {
				return translation.HTMLContent, translation.TextContent
			}
		}
	}
	return layout.HTMLContent, layout.TextContent
}

// inlineStyles moves a stylesheet into style attributes, because most mail
// clients ignore a <style> block. Failing to is not a reason to fail the
// message: it goes out with the stylesheet where it was.
func inlineStyles(html string) string {
	if strings.TrimSpace(html) == "" {
		return html
	}
	inlined, err := inliner.Inline(html)
	if err != nil {
		log.Warningf("failed to inline css in html: %s", err)
		return html
	}
	return inlined
}

func (self *mailer) SendMail(ctx context.Context, envelope *mailparse.Envelope, templateName string, locale string, variables map[string]interface{}) error {
	if len(envelope.Recipients) == 0 {
		return fmt.Errorf("mailer: missing recipients")
	}
	if envelope.Sender == "" {
		return fmt.Errorf("mailer: the envelope names no sender")
	}
	senderAddress, err := mailparse.ParseAddress(envelope.Sender)
	if err != nil {
		return err
	}
	_, domainDomain := mailparse.SplitAddress(senderAddress)

	var template *models.Template
	var layout *models.Layout
	if err := self.database.Transaction(func(tx db.Transaction) error {
		domain, err := tx.GetDomainByName(domainDomain)
		if err != nil {
			return err
		}
		if domain == nil {
			return fmt.Errorf("mailer: %q is not a configured domain", domainDomain)
		}
		template, err = tx.GetTemplateByName(domain.ID, templateName, nil)
		if err != nil {
			return err
		}
		if template == nil {
			return fmt.Errorf("mailer: template %q not found", templateName)
		}
		if template.LayoutID != "" {
			layout, err = tx.GetLayout(template.LayoutID, nil)
			if err != nil {
				return err
			}
			if layout == nil {
				return fmt.Errorf("mailer: layout for template %q not found", templateName)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	rendered, err := Render(template, layout, locale, variables)
	if err != nil {
		return err
	}

	return self.Send(ctx, envelope, &Message{
		From:     senderAddress,
		To:       envelope.Recipients,
		Subject:  rendered.Subject,
		Text:     rendered.TextContent,
		HTML:     rendered.HTMLContent,
		Language: rendered.Locale,
	})
}

func (self *mailer) Send(ctx context.Context, envelope *mailparse.Envelope, message *Message) error {
	composed, err := self.Compose(ctx, message)
	if err != nil {
		return err
	}
	envelope.DomainID = composed.Domain.ID
	envelope.Sender = message.From

	// Everybody named goes on the envelope; only To and Cc go in the
	// headers, which is what makes Bcc blind.
	recipients := make([]string, 0, len(message.To)+len(message.Cc)+len(message.Bcc))
	seen := make(map[string]bool)
	for _, list := range [][]string{message.To, message.Cc, message.Bcc} {
		for _, recipient := range list {
			address, err := mailparse.ParseAddress(recipient)
			if err != nil {
				return err
			}
			if seen[strings.ToLower(address)] {
				continue
			}
			seen[strings.ToLower(address)] = true
			recipients = append(recipients, address)
		}
	}
	if len(recipients) == 0 {
		return fmt.Errorf("mailer: missing recipients")
	}
	envelope.Recipients = recipients

	// fill the envelope
	envelope.ID = composed.ID
	envelope.ReceivedAt = time.Now().In(time.Local)
	envelope.Size = uint64(len(composed.Body))
	envelope.Headers = composed.Headers
	envelope.Body = composed.Body
	if envelope.IP == nil {
		envelope.IP = net.IPv4(127, 0, 0, 1)
	}
	return self.exchange.HandleEnvelope(ctx, envelope)
}

func (self *mailer) Compose(ctx context.Context, message *Message) (*Composed, error) {
	// Said rather than guessed. This used to fall back to a configured
	// "primary" domain when the envelope named no sender, which meant a
	// caller that forgot would send as some arbitrary domain instead of
	// being told.
	if message.From == "" {
		return nil, fmt.Errorf("mailer: the message names no sender")
	}
	senderAddress, err := mailparse.ParseAddress(message.From)
	if err != nil {
		return nil, err
	}
	message.From = senderAddress
	_, domainDomain := mailparse.SplitAddress(senderAddress)
	var domain *models.Domain
	var domains []*models.Domain
	if err := self.database.Transaction(func(tx db.Transaction) error {
		var err error
		if domain, err = tx.GetDomainByName(domainDomain); err != nil {
			return err
		}
		domains, err = tx.ListDomains()
		return err
	}); err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, fmt.Errorf("mailer: %q is not a configured domain", domainDomain)
	}

	// message and envelope id
	id := security.NewULID()

	// Every picture this server stores gets an address of its own for this
	// message: under the sending domain, so a recipient sees no other, and
	// unique, so a fetch of it says this message was opened. Here rather than
	// where the template was rendered, because this is where the message
	// first has an identifier to belong to.
	html := self.rewriteMedia(id, domain, domains, message.HTML)

	var body bytes.Buffer
	bodyHeaders, err := mailparse.Compose(&body, []byte(message.Text), []byte(html), message.Attachments)
	if err != nil {
		return nil, err
	}

	from := &mail.Address{Name: message.FromName, Address: senderAddress}
	headers := []string{
		mailparse.UnsplitHeader("Message-ID", fmt.Sprintf("<%s@%s>", id, domain.Domain)),
		mailparse.UnsplitHeader("Date", time.Now().Format(time.RFC1123Z)),
		mailparse.UnsplitHeader("From", from.String()),
		mailparse.UnsplitHeader("To", strings.Join(message.To, ", ")),
	}
	if len(message.Cc) > 0 {
		headers = append(headers, mailparse.UnsplitHeader("Cc", strings.Join(message.Cc, ", ")))
	}
	headers = append(headers, mailparse.UnsplitHeader("Subject", mailparse.EncodeHeaderValue(message.Subject)))
	if message.Language != "" {
		headers = append(headers, mailparse.UnsplitHeader("Content-Language", message.Language))
	}
	headers = append(headers, message.Headers...)
	headers = mailparse.MergeHeaders(headers, bodyHeaders)
	return &Composed{ID: id, Domain: domain, Headers: headers, Body: body.Bytes()}, nil
}
