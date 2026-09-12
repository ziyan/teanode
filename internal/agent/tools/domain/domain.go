// Package domain is the operator's domains and their delivery: domains,
// addresses, sending credentials, the queue.
package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		manage := []models.Permission{models.PermissionDomainManage, models.PermissionDomainManageAll}
		aliasFields := map[string]any{
			"domain":     tools.StringProperty("the domain, by name or id"),
			"pattern":    tools.StringProperty("the local part, or a pattern such as sales-* or *"),
			"kind":       tools.EnumProperty("what the alias does", "mailbox", "email", "webhook", "mailserver", "drop"),
			"email":      tools.StringProperty("for email: where to forward"),
			"webhook":    tools.StringProperty("for webhook: the address to post to"),
			"mailbox_id": tools.StringProperty("for mailbox: the mailbox that receives"),
			"comment":    tools.StringProperty("a note"),
			"disabled":   tools.BooleanProperty("off"),
		}
		return []*tools.Tool{
			{
				Name: "domain_list", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "The domains the person manages, with how many aliases and credentials each has.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					domains, err := operator.ListDomains(ctx)
					if err != nil {
						return nil, err
					}
					rows := make([]map[string]any, 0, len(domains))
					for _, domain := range domains {
						rows = append(rows, map[string]any{"domain_id": domain.ID, "domain": domain.Domain, "comment": domain.Comment, "aliases": len(domain.Aliases), "credentials": len(domain.Credentials), "mail_servers": domain.MailServers})
					}
					return tools.JSONResult(map[string]any{"domains": rows})
				},
			},
			{
				Name: "domain_get", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "One domain in full: its aliases, its credentials, its settings.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(domain)
				},
			},
			{
				Name: "domain_add", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionDomainManageAll},
				Description: "Add a domain to this server. The DNS records it needs come back with domain_dns_check afterwards.",
				Parameters: tools.Object(map[string]any{
					"domain":  tools.StringProperty("the domain name"),
					"comment": tools.StringProperty("a note about it"),
				}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain  string `json:"domain"`
						Comment string `json:"comment"`
					}](call)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{"domain": strings.ToLower(strings.TrimSpace(arguments.Domain))}
					if arguments.Comment != "" {
						parameters["comment"] = arguments.Comment
					}
					var result struct {
						CreateDomain *operator.DomainView `json:"CreateDomain"`
					}
					if err := tools.MustRun(ctx).Operations().Execute(ctx, `mutation ($domainParameters: DomainParametersInput!) { CreateDomain(domainParameters: $domainParameters) { id domain } }`, map[string]any{"domainParameters": parameters}, &result); err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(map[string]any{"domain_id": result.CreateDomain.ID, "domain": result.CreateDomain.Domain, "note": "added; run domain_dns_check for the records to publish"})
					if err != nil {
						return nil, err
					}
					answer.Note = "added " + result.CreateDomain.Domain
					return answer, nil
				},
			},
			{
				Name: "domain_update", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: manage,
				Description: "Change a domain's settings: comment, spam threshold, mail servers for a domain hosted elsewhere, link host.",
				Parameters: tools.Object(map[string]any{
					"domain":               tools.StringProperty("the domain, by name or id"),
					"comment":              tools.StringProperty("a note about it"),
					"spam_score_threshold": map[string]any{"type": "number", "description": "the score above which mail is junk"},
					"mail_servers":         tools.ArrayProperty("mail servers to forward to, host:port each", tools.StringProperty("host:port")),
					"link_host":            tools.StringProperty("the host links are written with"),
				}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain             string   `json:"domain"`
						Comment            *string  `json:"comment"`
						SpamScoreThreshold *float64 `json:"spam_score_threshold"`
						MailServers        []string `json:"mail_servers"`
						LinkHost           *string  `json:"link_host"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{}
					if arguments.Comment != nil {
						parameters["comment"] = *arguments.Comment
					}
					if arguments.SpamScoreThreshold != nil {
						parameters["spamFilterScoreThreshold"] = *arguments.SpamScoreThreshold
					}
					if arguments.MailServers != nil {
						parameters["mailServers"] = arguments.MailServers
					}
					if arguments.LinkHost != nil {
						parameters["linkHost"] = *arguments.LinkHost
					}
					if len(parameters) == 0 {
						return nil, fmt.Errorf("nothing to change")
					}
					if _, err := operator.Execute(ctx, `mutation ($domainId: String!, $domainParameters: DomainParametersInput!) { UpdateDomain(domainId: $domainId, domainParameters: $domainParameters) { id } }`, map[string]any{"domainId": domain.ID, "domainParameters": parameters}); err != nil {
						return nil, err
					}
					return tools.TextResult("changed %s", domain.Domain), nil
				},
			},
			{
				Name: "domain_remove", Family: tools.FamilyDomains, Risk: tools.RiskDestructive, Permissions: []models.Permission{models.PermissionDomainManageAll},
				Description: "Remove a domain and everything under it: aliases, credentials, templates. Cannot be undone.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Preview: func(arguments json.RawMessage) string {
					return "Remove the domain " + strings.TrimSpace(string(arguments)) + " and everything under it"
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($domainId: String!) { DeleteDomain(domainId: $domainId) }`, map[string]any{"domainId": domain.ID}); err != nil {
						return nil, err
					}
					return tools.TextResult("removed %s", domain.Domain), nil
				},
			},
			{
				Name: "domain_dns_check", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "Check a domain's DNS: which records are published as they should be and which are missing or wrong, with the values to publish.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					result, err := operator.Execute(ctx, `mutation ($domainId: String!) { CheckDomain(domainId: $domainId) { domain records { domain checkedAt error records { type name expected found } } } }`, map[string]any{"domainId": domain.ID})
					if err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(map[string]any{"domain": domain.Domain, "records": result["CheckDomain"]})
					if err != nil {
						return nil, err
					}
					answer.Untrusted = true
					return answer, nil
				},
			},
			{
				Name: "alias_list", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "The addresses of a domain: each alias with its pattern, what it does (a mailbox, a forward, a webhook, a mail server) and whether it is on.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					rows := make([]map[string]any, 0, len(domain.Aliases))
					for _, alias := range domain.Aliases {
						rows = append(rows, map[string]any{"alias_id": alias.ID, "pattern": alias.Pattern, "kind": alias.Kind, "email": alias.Email, "webhook": alias.Webhook, "mailbox_id": alias.MailboxID, "comment": alias.Comment, "disabled": alias.Disabled})
					}
					return tools.JSONResult(map[string]any{"domain": domain.Domain, "aliases": rows})
				},
			},
			{
				Name: "alias_add", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: manage,
				RiskOf:      aliasRisk,
				Description: "Add an address to a domain: what arrives at the pattern goes to a mailbox, is forwarded, is posted to a webhook, or is dropped.",
				Parameters:  tools.Object(aliasFields, "domain", "pattern", "kind"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain    string  `json:"domain"`
						Pattern   string  `json:"pattern"`
						Kind      string  `json:"kind"`
						Email     *string `json:"email"`
						Webhook   *string `json:"webhook"`
						MailboxID *string `json:"mailbox_id"`
						Comment   *string `json:"comment"`
						Disabled  *bool   `json:"disabled"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{"pattern": strings.TrimSpace(arguments.Pattern), "kind": arguments.Kind}
					for key, value := range map[string]*string{"email": arguments.Email, "webhook": arguments.Webhook, "mailboxId": arguments.MailboxID, "comment": arguments.Comment} {
						if value != nil {
							parameters[key] = *value
						}
					}
					if arguments.Disabled != nil {
						parameters["disabled"] = *arguments.Disabled
					}
					var result struct {
						CreateAlias struct {
							ID string `json:"id"`
						} `json:"CreateAlias"`
					}
					if err := tools.MustRun(ctx).Operations().Execute(ctx, `mutation ($domainId: String!, $aliasParameters: AliasParametersInput!) { CreateAlias(domainId: $domainId, aliasParameters: $aliasParameters) { id } }`, map[string]any{"domainId": domain.ID, "aliasParameters": parameters}, &result); err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(map[string]any{"alias_id": result.CreateAlias.ID, "address": arguments.Pattern + "@" + domain.Domain})
					if err != nil {
						return nil, err
					}
					answer.Note = "added " + arguments.Pattern + "@" + domain.Domain
					return answer, nil
				},
			},
			{
				Name: "alias_update", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: manage,
				RiskOf:      aliasRisk,
				Description: "Change an address: its pattern, what it does, its note, or switch it off and on.",
				Parameters:  tools.Object(mailbox.MergeProperties(aliasFields, map[string]any{"alias_id": tools.StringProperty("the alias, from alias_list")}), "alias_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						AliasID   string  `json:"alias_id"`
						Pattern   string  `json:"pattern"`
						Kind      string  `json:"kind"`
						Email     *string `json:"email"`
						Webhook   *string `json:"webhook"`
						MailboxID *string `json:"mailbox_id"`
						Comment   *string `json:"comment"`
						Disabled  *bool   `json:"disabled"`
					}](call)
					if err != nil {
						return nil, err
					}
					// The API replaces the alias whole, so what is not given is
					// read first and kept.
					domains, err := operator.ListDomains(ctx)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{}
					found := false
					for _, domain := range domains {
						for _, alias := range domain.Aliases {
							if alias.ID == arguments.AliasID {
								found = true
								parameters["pattern"], parameters["kind"], parameters["comment"], parameters["disabled"] = alias.Pattern, alias.Kind, alias.Comment, alias.Disabled
								if alias.Email != "" {
									parameters["email"] = alias.Email
								}
								if alias.Webhook != "" {
									parameters["webhook"] = alias.Webhook
								}
								if alias.MailboxID != "" {
									parameters["mailboxId"] = alias.MailboxID
								}
							}
						}
					}
					if !found {
						return nil, fmt.Errorf("there is no alias %q among the person's domains", arguments.AliasID)
					}
					if arguments.Pattern != "" {
						parameters["pattern"] = arguments.Pattern
					}
					if arguments.Kind != "" {
						parameters["kind"] = arguments.Kind
					}
					for key, value := range map[string]*string{"email": arguments.Email, "webhook": arguments.Webhook, "mailboxId": arguments.MailboxID, "comment": arguments.Comment} {
						if value != nil {
							parameters[key] = *value
						}
					}
					if arguments.Disabled != nil {
						parameters["disabled"] = *arguments.Disabled
					}
					if _, err := operator.Execute(ctx, `mutation ($aliasId: String!, $aliasParameters: AliasParametersInput!) { UpdateAlias(aliasId: $aliasId, aliasParameters: $aliasParameters) { id } }`, map[string]any{"aliasId": arguments.AliasID, "aliasParameters": parameters}); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the alias %s", parameters["pattern"]), nil
				},
			},
			{
				Name: "alias_remove", Family: tools.FamilyDomains, Risk: tools.RiskDestructive, Permissions: manage,
				Description: "Remove an address. Mail to it bounces from then on.",
				Parameters:  tools.Object(map[string]any{"alias_id": tools.StringProperty("the alias, from alias_list")}, "alias_id"),
				Preview: func(arguments json.RawMessage) string {
					return "Remove the alias " + strings.TrimSpace(string(arguments))
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						AliasID string `json:"alias_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`, map[string]any{"aliasId": arguments.AliasID}); err != nil {
						return nil, err
					}
					return tools.TextResult("removed the alias"), nil
				},
			},
			{
				Name: "alias_match", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "Which aliases an address would reach, in order: the answer to \"where does mail to X go?\".",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id"), "address": tools.StringProperty("the address, or its local part")}, "domain", "address"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain  string `json:"domain"`
						Address string `json:"address"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					result, err := operator.Execute(ctx, `query ($domainId: String!, $address: String!) { MatchAliases(domainId: $domainId, address: $address) { id pattern kind email webhook mailboxId disabled } }`, map[string]any{"domainId": domain.ID, "address": arguments.Address})
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"matches": result["MatchAliases"]})
				},
			},
			{
				Name: "credential_list", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: manage,
				Description: "The sending credentials of a domain: what programs authenticate with to send as it.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"domain": domain.Domain, "credentials": domain.Credentials})
				},
			},
			{
				Name: "credential_create", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: manage,
				Description: "Make a sending credential for a domain. The secret is shown once, to the person, exactly; never keep it.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id"), "comment": tools.StringProperty("what it is for"), "alias": tools.StringProperty("the address it sends as, when limited to one")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain  string  `json:"domain"`
						Comment *string `json:"comment"`
						Alias   *string `json:"alias"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{}
					if arguments.Comment != nil {
						parameters["comment"] = *arguments.Comment
					}
					if arguments.Alias != nil {
						parameters["alias"] = *arguments.Alias
					}
					result, err := operator.Execute(ctx, `mutation ($domainId: String!, $credentialParameters: CredentialParametersInput!) { CreateCredential(domainId: $domainId, credentialParameters: $credentialParameters) { credential { id alias comment } host port username password } }`, map[string]any{"domainId": domain.ID, "credentialParameters": parameters})
					if err != nil {
						return nil, err
					}
					answer, err := tools.JSONResult(result["CreateCredential"])
					if err != nil {
						return nil, err
					}
					answer.ShowVerbatim = true
					answer.Note = "made a credential for " + domain.Domain
					return answer, nil
				},
			},
			{
				Name: "credential_update", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: manage,
				Description: "Change a credential's note or the address it is limited to, or switch it off and on.",
				Parameters:  tools.Object(map[string]any{"credential_id": tools.StringProperty("the credential, from credential_list"), "comment": tools.StringProperty("a note"), "alias": tools.StringProperty("the address it sends as"), "disabled": tools.BooleanProperty("off")}, "credential_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						CredentialID string  `json:"credential_id"`
						Comment      *string `json:"comment"`
						Alias        *string `json:"alias"`
						Disabled     *bool   `json:"disabled"`
					}](call)
					if err != nil {
						return nil, err
					}
					parameters := map[string]any{}
					if arguments.Comment != nil {
						parameters["comment"] = *arguments.Comment
					}
					if arguments.Alias != nil {
						parameters["alias"] = *arguments.Alias
					}
					if arguments.Disabled != nil {
						parameters["disabled"] = *arguments.Disabled
					}
					if _, err := operator.Execute(ctx, `mutation ($credentialId: String!, $credentialParameters: CredentialParametersInput!) { UpdateCredential(credentialId: $credentialId, credentialParameters: $credentialParameters) { id } }`, map[string]any{"credentialId": arguments.CredentialID, "credentialParameters": parameters}); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the credential"), nil
				},
			},
			{
				Name: "credential_remove", Family: tools.FamilyDomains, Risk: tools.RiskDestructive, Permissions: manage,
				Description: "Remove a sending credential. Whatever used it stops sending.",
				Parameters:  tools.Object(map[string]any{"credential_id": tools.StringProperty("the credential, from credential_list")}, "credential_id"),
				Preview: func(arguments json.RawMessage) string {
					return "Remove the credential " + strings.TrimSpace(string(arguments))
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						CredentialID string `json:"credential_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					if _, err := operator.Execute(ctx, `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`, map[string]any{"credentialId": arguments.CredentialID}); err != nil {
						return nil, err
					}
					return tools.TextResult("removed the credential"), nil
				},
			},
			{
				Name: "queue_list", Family: tools.FamilyDomains, Risk: tools.RiskRead, Permissions: []models.Permission{models.PermissionQueueManage},
				Description: "Deliveries still waiting to go out, or being retried, for a domain.",
				Parameters:  tools.Object(map[string]any{"domain": tools.StringProperty("the domain, by name or id")}, "domain"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Domain string `json:"domain"`
					}](call)
					if err != nil {
						return nil, err
					}
					domain, err := operator.FindDomain(ctx, arguments.Domain)
					if err != nil {
						return nil, err
					}
					result, err := operator.Execute(ctx, `query ($domainId: String!) { ListPendingDeliveries(domainId: $domainId) { id mailId recipient kind status attempts attemptedAt retryAt } }`, map[string]any{"domainId": domain.ID})
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"domain": domain.Domain, "pending": result["ListPendingDeliveries"]})
				},
			},
			{
				Name: "queue_retry", Family: tools.FamilyDomains, Risk: tools.RiskWrite, Permissions: []models.Permission{models.PermissionQueueManage},
				Description: "Try a waiting delivery again now.",
				Parameters:  tools.Object(map[string]any{"delivery_id": tools.StringProperty("the delivery, from queue_list")}, "delivery_id"),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						DeliveryID string `json:"delivery_id"`
					}](call)
					if err != nil {
						return nil, err
					}
					result, err := operator.Execute(ctx, `mutation ($deliveryId: String!) { RetryDelivery(deliveryId: $deliveryId) { id status retryAt } }`, map[string]any{"deliveryId": arguments.DeliveryID})
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(result["RetryDelivery"])
				},
			},
		}
	})
}

// aliasRisk is what an address would do: one that forwards to an outside
// address or a webhook sends mail out of the server for as long as it
// exists, so making or changing it asks first.
func aliasRisk(arguments json.RawMessage) tools.Risk {
	var call struct {
		Kind    string `json:"kind"`
		Email   string `json:"email"`
		Webhook string `json:"webhook"`
	}
	if json.Unmarshal(arguments, &call) != nil {
		return tools.RiskWrite
	}
	switch strings.ToLower(call.Kind) {
	case "email", "webhook", "forward":
		return tools.RiskOutward
	}
	if call.Email != "" || call.Webhook != "" {
		return tools.RiskOutward
	}
	return tools.RiskWrite
}
