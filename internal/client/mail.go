package client

import (
	"context"
	"encoding/json"
	"time"
)

// Mail is a message the server has handled, as the list shows it.
type Mail struct {
	ID                    string          `json:"id"`
	CreatedAt             time.Time       `json:"createdAt"`
	ReceivedAt            time.Time       `json:"receivedAt"`
	DomainID              string          `json:"domainId"`
	CredentialID          string          `json:"credentialId"`
	EnvelopeID            string          `json:"envelopeId"`
	Kind                  string          `json:"kind"`
	Status                string          `json:"status"`
	Sender                string          `json:"sender"`
	Recipients            []string        `json:"recipients"`
	From                  string          `json:"from"`
	Subject               string          `json:"subject"`
	MessageID             string          `json:"messageId"`
	Size                  uint64          `json:"size"`
	IP                    string          `json:"ip"`
	RDNS                  string          `json:"rdns"`
	Hello                 string          `json:"hello"`
	TLSVersion            string          `json:"tlsVersion"`
	AuthenticationResults json.RawMessage `json:"authenticationResults"`
	Deliveries            []*Delivery     `json:"deliveries"`
}

const mailFields = `{
	id createdAt receivedAt domainId credentialId envelopeId kind status
	sender recipients from subject messageId size ip rdns hello tlsVersion
	authenticationResults {
		spf { domain ip result }
		dkims { result domain selector }
		dmarc { domain policy }
		arc { result instances }
		spamFilter { score result symbols checks { symbol score description } }
		antivirus { viruses }
		errors
	}
	deliveries ` + deliveryFields + `
}`

// Filter is one equality or substring test on a mail column, for ListMails.
type Filter struct {
	Field string
	Value string

	// Contains matches a substring rather than the whole value, for subjects.
	Contains bool
}

// stages turns filters into the aggregation pipeline the API takes: one
// match stage with every test joined by "and".
func stages(filters []Filter) []map[string]any {
	if len(filters) == 0 {
		return nil
	}
	tests := make([]map[string]any, 0, len(filters))
	for _, filter := range filters {
		operation := "equal"
		if filter.Contains {
			operation = "contains"
		}
		tests = append(tests, map[string]any{"operation": operation, "field": filter.Field, "value": filter.Value})
	}
	if len(tests) == 1 {
		return []map[string]any{{"match": tests[0]}}
	}
	return []map[string]any{{"match": map[string]any{"operation": "and", "filters": tests}}}
}

// ListMails returns handled mail, newest first, for one domain or every
// domain when domainId is empty.
func ListMails(ctx context.Context, connection *Client, domainId string, filters []Filter, first int) ([]*Mail, error) {
	var result struct {
		ListMails []*Mail `json:"ListMails"`
	}
	query := `query ($domainId: String!, $aggregations: [StageInput], $pagination: PaginationInput) {
		ListMails(domainId: $domainId, aggregations: $aggregations, pagination: $pagination) ` + mailFields + `
	}`
	variables := map[string]any{"domainId": domainId, "pagination": pagination(first)}
	if pipeline := stages(filters); pipeline != nil {
		variables["aggregations"] = pipeline
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListMails, nil
}

// GetMail returns one message.
func GetMail(ctx context.Context, connection *Client, mailId string) (*Mail, error) {
	var result struct {
		GetMail *Mail `json:"GetMail"`
	}
	query := `query ($mailId: String!) { GetMail(mailId: $mailId) ` + mailFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"mailId": mailId}, &result); err != nil {
		return nil, err
	}
	return result.GetMail, nil
}

// Facet is one value a column takes, and how many messages carry it.
type Facet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// CountMailsBy counts messages by the values of one field.
func CountMailsBy(ctx context.Context, connection *Client, domainId, field string, filters []Filter) ([]*Facet, error) {
	var result struct {
		CountMailsBy []*Facet `json:"CountMailsBy"`
	}
	query := `query ($domainId: String!, $field: String!, $aggregations: [StageInput]) {
		CountMailsBy(domainId: $domainId, field: $field, aggregations: $aggregations) { value count }
	}`
	variables := map[string]any{"domainId": domainId, "field": field}
	if pipeline := stages(filters); pipeline != nil {
		variables["aggregations"] = pipeline
	}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.CountMailsBy, nil
}

// MailContent is a stored message taken apart for display.
type MailContent struct {
	MailID           string        `json:"mailId"`
	Available        bool          `json:"available"`
	Text             string        `json:"text"`
	HTML             string        `json:"html"`
	HasRemoteContent bool          `json:"hasRemoteContent"`
	Attachments      []*Attachment `json:"attachments"`
	Headers          []*Header     `json:"headers"`
	RawHeaders       string        `json:"rawHeaders"`
	Size             int           `json:"size"`
}

// Attachment is a file attached to a message.
type Attachment struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Inline      bool   `json:"inline"`
}

// Header is one decoded header line.
type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GetMailContent returns a stored message decoded: its text, its HTML with
// scripts removed, its headers and its attachments.
func GetMailContent(ctx context.Context, connection *Client, mailId string) (*MailContent, error) {
	var result struct {
		GetMailContent *MailContent `json:"GetMailContent"`
	}
	query := `query ($mailId: String!) {
		GetMailContent(mailId: $mailId) {
			mailId available text html hasRemoteContent size rawHeaders
			attachments { index filename contentType size inline }
			headers { key value }
		}
	}`
	if err := connection.Execute(ctx, query, map[string]any{"mailId": mailId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailContent, nil
}

// MailOpens is whether a sent message has been looked at, as far as a
// fetched picture can say.
type MailOpens struct {
	MailID       string     `json:"mailId"`
	Trackable    bool       `json:"trackable"`
	Opened       bool       `json:"opened"`
	OpenedAt     *time.Time `json:"openedAt"`
	LastOpenedAt *time.Time `json:"lastOpenedAt"`
	OpenCount    int64      `json:"openCount"`
	IP           string     `json:"ip"`
	UserAgent    string     `json:"userAgent"`
}

// GetMailOpens returns whether a sent message was opened.
func GetMailOpens(ctx context.Context, connection *Client, mailId string) (*MailOpens, error) {
	var result struct {
		GetMailOpens *MailOpens `json:"GetMailOpens"`
	}
	query := `query ($mailId: String!) {
		GetMailOpens(mailId: $mailId) { mailId trackable opened openedAt lastOpenedAt openCount ip userAgent }
	}`
	if err := connection.Execute(ctx, query, map[string]any{"mailId": mailId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailOpens, nil
}

// AttachmentParameters is a file to attach to a message being sent.
type AttachmentParameters struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType,omitempty"`
	Content     []byte `json:"content"`
}

// MessageParameters is a message to send: who it is from and to, and either
// a template with its variables or a subject and body written by hand.
type MessageParameters struct {
	From        string                  `json:"from"`
	FromName    string                  `json:"fromName,omitempty"`
	To          []string                `json:"to"`
	Cc          []string                `json:"cc,omitempty"`
	Bcc         []string                `json:"bcc,omitempty"`
	Subject     string                  `json:"subject,omitempty"`
	TemplateID  string                  `json:"templateId,omitempty"`
	Locale      string                  `json:"locale,omitempty"`
	Variables   map[string]any          `json:"variables,omitempty"`
	HTMLContent string                  `json:"htmlContent,omitempty"`
	TextContent string                  `json:"textContent,omitempty"`
	Attachments []*AttachmentParameters `json:"attachments,omitempty"`
}

// SendMail sends a message as an address at a domain, and returns it as
// stored, or nil when it was sent but not found afterwards.
func SendMail(ctx context.Context, connection *Client, domainId string, message *MessageParameters) (*Mail, error) {
	var result struct {
		SendMail struct {
			Mail *Mail `json:"mail"`
		} `json:"SendMail"`
	}
	query := `mutation ($domainId: String!, $messageParameters: MessageParametersInput!) {
		SendMail(domainId: $domainId, messageParameters: $messageParameters) { mail ` + mailFields + ` }
	}`
	variables := map[string]any{"domainId": domainId, "messageParameters": message}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.SendMail.Mail, nil
}
