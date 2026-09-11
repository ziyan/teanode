// Package mailaudit is the mail audit as tools: search, one message, its
// content, marks, and the reports.
package mailaudit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		audit := []models.Permission{models.PermissionMailAudit, models.PermissionMailAuditAll}
		return []*tools.Tool{
			{
				Name: "mail_audit_search", Family: tools.FamilyAudit, Risk: tools.RiskRead, Permissions: audit,
				Description: "Every message that passed through a domain the person audits — arriving and leaving, delivered and refused — as the operator's Mail page shows it. Not the person's own mailbox: that is mail_search.",
				Parameters: tools.Object(map[string]any{
					"domain":  tools.StringProperty("the domain, by name or id"),
					"from":    tools.StringProperty("part of the sender"),
					"to":      tools.StringProperty("part of a recipient"),
					"subject": tools.StringProperty("part of the subject"),
					"status":  tools.StringProperty("accepted, rejected, and the rest"),
					"kind":    tools.EnumProperty("arriving or leaving", "incoming", "outgoing"),
					"since":   tools.StringProperty("an ISO date"),
					"limit":   tools.IntegerProperty("how many, 20 by default"),
				}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain  string `json:"domain"`
						From    string `json:"from"`
						To      string `json:"to"`
						Subject string `json:"subject"`
						Status  string `json:"status"`
						Kind    string `json:"kind"`
						Since   string `json:"since"`
						Limit   int    `json:"limit"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					var filters []map[string]any
					contains := func(field, value string) {
						if strings.TrimSpace(value) != "" {
							filters = append(filters, map[string]any{"operation": "contains", "field": field, "value": strings.TrimSpace(value)})
						}
					}
					contains("sender", arguments.From)
					contains("subject", arguments.Subject)
					if arguments.To != "" {
						filters = append(filters, map[string]any{"operation": "contains", "field": "recipients", "value": strings.TrimSpace(arguments.To)})
					}
					if arguments.Status != "" {
						filters = append(filters, map[string]any{"operation": "equal", "field": "status", "value": arguments.Status})
					}
					if arguments.Kind != "" {
						filters = append(filters, map[string]any{"operation": "equal", "field": "kind", "value": arguments.Kind})
					}
					if arguments.Since != "" {
						since, err := tools.ParseTime(arguments.Since, tools.Location(tools.MustRun(ctx).Owner()), time.Now())
						if err != nil {
							return nil, err
						}
						filters = append(filters, map[string]any{"operation": "greaterEqual", "field": "receivedAt", "value": since.UTC().Format(time.RFC3339)})
					}
					var stages []map[string]any
					if len(filters) == 1 {
						stages = append(stages, map[string]any{"match": filters[0]})
					} else if len(filters) > 1 {
						stages = append(stages, map[string]any{"match": map[string]any{"operation": "and", "filters": filters}})
					}
					stages = append(stages, map[string]any{"sort": []map[string]any{{"field": "receivedAt", "direction": "descending"}}})
					limit := arguments.Limit
					if limit <= 0 {
						limit = 20
					}
					result, err := operator.Execute(ctx, `query ($domainId: String!, $aggregations: [StageInput!]) { ListMails(domainId: $domainId, aggregations: $aggregations) { id sender recipients from subject status kind size receivedAt } }`, map[string]any{"domainId": domain.ID, "aggregations": stages})
					if err != nil {
						return nil, err
					}
					mails, _ := result["ListMails"].([]any)
					if len(mails) > limit {
						mails = mails[:limit]
					}
					answer, err := tools.JSONResult(map[string]any{"domain": domain.Domain, "mails": mails})
					if err != nil {
						return nil, err
					}
					answer.Untrusted = true
					return answer, nil
				},
			},
			{
				Name: "mail_audit_get", Family: tools.FamilyAudit, Risk: tools.RiskRead, Permissions: audit,
				Description: "One message of the audit in full: envelope, headers, authentication results, and its deliveries.",
				Parameters:  tools.Object(map[string]any{"mail_id": tools.StringProperty("the message, from mail_audit_search")}, "mail_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						MailID string `json:"mail_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					result, err := operator.Execute(ctx, `query ($mailId: String!) { GetMail(mailId: $mailId) { id envelopeId hello ip rdns sender recipients messageId from subject status kind size receivedAt authenticationResults { spf { result } dkims { result domain } dmarc { result } spamFilter { result score } } } ListDeliveriesByMail(mailId: $mailId) { id recipient kind status attempts attemptedAt deliveredAt droppedAt } }`, map[string]any{"mailId": arguments.MailID})
					if err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(map[string]any{"mail": result["GetMail"], "deliveries": result["ListDeliveriesByMail"]})
					if err != nil {
						return nil, err
					}
					answer.Untrusted = true
					return answer, nil
				},
			},
			{
				Name: "mail_audit_content", Family: tools.FamilyAudit, Risk: tools.RiskRead, Permissions: audit,
				Description: "The text of a message of the audit. Reading somebody else's mail: only when the person asked for that message.",
				Parameters:  tools.Object(map[string]any{"mail_id": tools.StringProperty("the message"), "max_characters": tools.IntegerProperty("bound, 8000 by default")}, "mail_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						MailID        string `json:"mail_id"`
						MaxCharacters int    `json:"max_characters"`
					}](call)
					if err != nil {
						return nil, err
					}
					content, err := mailbox.GetContent(ctx, tools.MustRun(ctx).Operations(), arguments.MailID)
					if err != nil {
						return nil, err
					}
					if content == nil {
						return nil, fmt.Errorf("the message has no stored content")
					}
					text := content.Text
					if strings.TrimSpace(text) == "" && content.HTML != "" {
						text = tools.HTMLToText(content.HTML)
					}
					text = tools.NormalizeText(text)
					limit := arguments.MaxCharacters
					if limit <= 0 {
						limit = 8000
					}
					if len(text) > limit {
						text = text[:limit]
					}
					answer, err := tools.JSONResult(map[string]any{"text": text})
					if err != nil {
						return nil, err
					}
					answer.Untrusted = true
					return answer, nil
				},
			},
			{
				Name: "mail_audit_mark", Family: tools.FamilyAudit, Risk: tools.RiskWrite, Permissions: audit,
				Description: "Tell the spam filter about a message of the audit: spam, or ham (not spam), so it learns.",
				Parameters:  tools.Object(map[string]any{"mail_id": tools.StringProperty("the message"), "label": tools.EnumProperty("what it is", "spam", "ham")}, "mail_id", "label"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						MailID string `json:"mail_id"`
						Label  string `json:"label"`
					}](call)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($mailId: String!, $label: String!) { MarkMail(mailId: $mailId, label: $label) { mailId label learnedSpam learnedHam } }`, map[string]any{"mailId": arguments.MailID, "label": arguments.Label}); err != nil {
						return nil, err
					}
					return tools.TextResult("marked the message as %s", arguments.Label), nil
				},
			},
			{
				Name: "report_list", Family: tools.FamilyAudit, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionReportRead},
				Description: "DMARC aggregate reports received about a domain: who sent as it, and whether it aligned.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id"), "limit": tools.IntegerProperty("how many, 20 by default")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
						Limit  int    `json:"limit"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					limit := arguments.Limit
					if limit <= 0 {
						limit = 20
					}
					result, err := operator.Execute(ctx, `query ($domainId: String!) { ListReports(domainId: $domainId) { id domainId beginAt endAt count ip rdns fromDomain senderDomain } }`, map[string]any{"domainId": domain.ID})
					if err != nil {
						return nil, err
					}
					reports, _ := result["ListReports"].([]any)
					if len(reports) > limit {
						reports = reports[:limit]
					}
					answer, err := tools.JSONResult(map[string]any{"domain": domain.Domain, "reports": reports})
					if err != nil {
						return nil, err
					}
					answer.Untrusted = true
					return answer, nil
				},
			},
		}
	})
}
