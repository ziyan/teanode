package apigraph

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/aggregate"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/templating"
)

type SendMutation interface {
	// Send a message as an address at a Domain: a Template rendered with variables, or content written by hand
	SendMail(ctx context.Context, arguments SendMailArguments) (*SendMailReturnValue, error)
}

// AttachmentParameters is a file to attach to a message.
type AttachmentParameters struct {
	// Name the recipient sees the file under
	Filename string `json:"filename"`

	// Media type of the file, such as "application/pdf"; application/octet-stream when unknown
	ContentType string `json:"contentType" graphapi:"nullable"`

	// The file itself, base64 encoded
	Content []byte `json:"content"`
}

// MessageParameters is a message to send: who it is from and to, and either
// a template with its variables or a subject and body written by hand.
type MessageParameters struct {
	// Address to send as; has to be at the Domain
	From string `json:"from"`

	// Display name beside the address, optional
	FromName string `json:"fromName" graphapi:"nullable"`

	// Recipients, each an address or "Name <address>"
	To []string `json:"to"`

	// Carbon copies, optional
	Cc []string `json:"cc" graphapi:"nullable"`

	// Blind carbon copies, optional; not written into the message
	Bcc []string `json:"bcc" graphapi:"nullable"`

	// Subject line; with a template, overrides the template's when set
	Subject string `json:"subject" graphapi:"nullable"`

	// ID of a Template to render; leave unset to send htmlContent and textContent as written
	TemplateID string `json:"templateId" graphapi:"nullable"`

	// Locale to render the Template in, or the language the content is in
	Locale string `json:"locale" graphapi:"nullable"`

	// Values for the Template's variables
	Variables map[string]interface{} `json:"variables" graphapi:"nullable"`

	// HTML body, when not using a template
	HTMLContent string `json:"htmlContent" graphapi:"nullable"`

	// Text body, when not using a template
	TextContent string `json:"textContent" graphapi:"nullable"`

	// Files to attach
	Attachments []*AttachmentParameters `json:"attachments" graphapi:"nullable"`
}

type SendMailArguments struct {
	// ID of the Domain to send as
	DomainID string `json:"domainId"`

	MessageParameters MessageParameters `json:"messageParameters"`
}

type SendMailReturnValue struct {
	// The message as stored, once it has been accepted
	Mail *models.Mail `json:"mail"`
}

// SendMail sends a message the operator composed. It goes the way a
// credential's submission goes: signed with the domain's key, recorded
// under Mail, and queued for delivery.
func (self *graph) SendMail(ctx context.Context, arguments SendMailArguments) (*SendMailReturnValue, error) {
	domain, err := self.requireDomainPermission(ctx, models.PermissionDomainManage, arguments.DomainID)
	if err != nil {
		return nil, err
	}
	tx := api.ContextTransaction(ctx)
	parameters := &arguments.MessageParameters

	// The sender is an address at this domain. Anything else would be
	// signed with a key for a domain it does not belong to, which receivers
	// rightly refuse.
	fromAddress, err := mail.ParseAddress(parameters.From)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not an address", api.ErrInvalidArguments, parameters.From)
	}
	if _, fromDomain := mailparse.SplitAddress(fromAddress.Address); fromDomain != domain.Domain {
		return nil, fmt.Errorf("%w: the sender has to be an address at %s", api.ErrInvalidArguments, domain.Domain)
	}
	fromName := parameters.FromName
	if fromName == "" {
		fromName = fromAddress.Name
	}

	to, err := parseAddresses(parameters.To)
	if err != nil {
		return nil, err
	}
	cc, err := parseAddresses(parameters.Cc)
	if err != nil {
		return nil, err
	}
	bcc, err := parseAddresses(parameters.Bcc)
	if err != nil {
		return nil, err
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		return nil, fmt.Errorf("%w: a message needs a recipient", api.ErrInvalidArguments)
	}

	if parameters.Locale != "" && !templating.ValidLocale(parameters.Locale) {
		return nil, fmt.Errorf("%w: %q is not a language tag such as en or zh-CN", api.ErrInvalidArguments, parameters.Locale)
	}

	// The same limit the SMTP listener applies, so what can be sent from
	// here is what a mail client could send.
	limit := self.config.Current().SMTP.MaxMessageSize.Bytes()
	var total uint64
	attachments := make([]*mailparse.Attachment, 0, len(parameters.Attachments))
	for _, attachment := range parameters.Attachments {
		if attachment == nil {
			continue
		}
		if strings.TrimSpace(attachment.Filename) == "" {
			return nil, fmt.Errorf("%w: an attachment needs a filename", api.ErrInvalidArguments)
		}
		total += uint64(len(attachment.Content))
		if limit > 0 && total > limit {
			return nil, fmt.Errorf("%w: the attachments come to more than the %d bytes a message may be", api.ErrInvalidArguments, limit)
		}
		attachments = append(attachments, &mailparse.Attachment{
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			Content:     attachment.Content,
		})
	}

	message := &mailer.Message{
		From:        fromAddress.Address,
		FromName:    fromName,
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     parameters.Subject,
		Attachments: attachments,
		Language:    parameters.Locale,
	}

	if parameters.TemplateID != "" {
		template, err := tx.GetTemplate(parameters.TemplateID, nil)
		if err != nil {
			return nil, err
		}
		if template == nil || template.DomainID != domain.ID {
			return nil, fmt.Errorf("%w: no such template", api.ErrNotFound)
		}
		layout, err := self.requireLayoutOfDomain(tx, domain.ID, template.LayoutID)
		if err != nil {
			return nil, err
		}
		rendered, err := mailer.Render(template, layout, parameters.Locale, parameters.Variables)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		if message.Subject == "" {
			message.Subject = rendered.Subject
		}
		message.Text = rendered.TextContent
		message.HTML = rendered.HTMLContent
		message.Language = rendered.Locale
	} else {
		message.Text = parameters.TextContent
		message.HTML = parameters.HTMLContent
		if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.HTML) == "" && len(attachments) == 0 {
			return nil, fmt.Errorf("%w: a message needs a body or an attachment", api.ErrInvalidArguments)
		}
	}

	envelope := &mailparse.Envelope{}
	if request := api.ContextRequest(ctx); request != nil {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			host = request.RemoteAddr
		}
		envelope.IP = net.ParseIP(host)
		envelope.Location = self.locator.Locate(envelope.IP)
		envelope.TLS = request.TLS
	}

	if err := self.mailer.Send(ctx, envelope, message); err != nil {
		return nil, err
	}

	// The exchange stored it under its own transaction. Find it by the
	// envelope, which is the one identifier both sides hold.
	mails, err := tx.ListMails(domain.ID, &db.Options{
		Limit:   1,
		Columns: mailColumns,
		Aggregations: aggregate.Pipeline{{Match: &aggregate.Filter{
			Operation: aggregate.OperationEqual,
			Field:     "envelopeId",
			Value:     &envelope.ID,
		}}},
	})
	if err != nil {
		return nil, err
	}
	if len(mails) == 0 {
		// Sent, but not where the dashboard can show it. Saying so is
		// better than failing what has already gone.
		log.Warningf("sent envelope %q but found no stored mail for it", envelope.ID)
		return &SendMailReturnValue{}, nil
	}
	return &SendMailReturnValue{Mail: mails[0]}, nil
}

// parseAddresses accepts each entry as an address or "Name <address>" and
// returns the addresses alone.
func parseAddresses(entries []string) ([]string, error) {
	addresses := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		parsed, err := mail.ParseAddress(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not an address", api.ErrInvalidArguments, entry)
		}
		addresses = append(addresses, parsed.Address)
	}
	return addresses, nil
}
