package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"

	"gopkg.in/yaml.v3"
)

// The operator families: domains and delivery, the mail audit, people and
// access, the server, and the person's own account. Each tool is offered
// only to somebody who holds the permission behind it, and every one goes
// through the API as that person, so a tool can never do what the person
// could not do from the dashboard. Names, not ids, wherever a person would
// use a name; ids where the API needs them, given back in every row.

func registerOperatorTools(catalog *Catalog) {
	registerDomainTools(catalog)
	registerAuditTools(catalog)
	registerPeopleTools(catalog)
	registerServerTools(catalog)
	registerAccountTools(catalog)
}

// execute runs a document and discards the shape of the answer.
func execute(ctx context.Context, call *Call, document string, variables map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := call.Run.settings.Operations.Execute(ctx, document, variables, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- Family 2: domains and delivery ---------------------------------------

// domainView is a domain as ListDomains returns it, in the parts the tools
// use.
type domainView struct {
	ID                       string   `json:"id"`
	Domain                   string   `json:"domain"`
	Subdomain                string   `json:"subdomain"`
	Comment                  string   `json:"comment"`
	SpamFilterScoreThreshold float64  `json:"spamFilterScoreThreshold"`
	MailServers              []string `json:"mailServers"`
	Aliases                  []*struct {
		ID        string `json:"id"`
		Pattern   string `json:"pattern"`
		Comment   string `json:"comment"`
		Kind      string `json:"kind"`
		Email     string `json:"email"`
		Webhook   string `json:"webhook"`
		MailboxID string `json:"mailboxId"`
		Disabled  bool   `json:"disabled"`
	} `json:"aliases"`
	Credentials []*struct {
		ID       string `json:"id"`
		Alias    string `json:"alias"`
		Comment  string `json:"comment"`
		Disabled bool   `json:"disabled"`
	} `json:"credentials"`
}

const documentListDomains = `query { ListDomains { id domain subdomain comment spamFilterScoreThreshold mailServers
	aliases { id pattern comment kind email webhook mailboxId disabled }
	credentials { id alias comment disabled } } }`

func listDomains(ctx context.Context, call *Call) ([]*domainView, error) {
	var result struct {
		ListDomains []*domainView `json:"ListDomains"`
	}
	if err := call.Run.settings.Operations.Execute(ctx, documentListDomains, nil, &result); err != nil {
		return nil, err
	}
	return result.ListDomains, nil
}

// findDomain resolves a domain by name or id among the ones the person
// manages.
func findDomain(ctx context.Context, call *Call, nameOrId string) (*domainView, error) {
	domains, err := listDomains(ctx, call)
	if err != nil {
		return nil, err
	}
	nameOrId = strings.ToLower(strings.TrimSpace(nameOrId))
	if nameOrId == "" {
		if len(domains) == 1 {
			return domains[0], nil
		}
		return nil, fmt.Errorf("which domain? the person manages %d", len(domains))
	}
	for _, domain := range domains {
		if domain.ID == nameOrId || strings.ToLower(domain.Domain) == nameOrId {
			return domain, nil
		}
	}
	return nil, fmt.Errorf("the person does not manage a domain %q", nameOrId)
}

func registerDomainTools(catalog *Catalog) {
	manage := []models.Permission{models.PermissionDomainManage, models.PermissionDomainManageAll}
	catalog.Register(&Tool{
		Name: "domain_list", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "The domains the person manages, with how many aliases and credentials each has.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			domains, err := listDomains(ctx, call)
			if err != nil {
				return nil, err
			}
			rows := make([]map[string]any, 0, len(domains))
			for _, domain := range domains {
				rows = append(rows, map[string]any{"domain_id": domain.ID, "domain": domain.Domain, "comment": domain.Comment, "aliases": len(domain.Aliases), "credentials": len(domain.Credentials), "mail_servers": domain.MailServers})
			}
			return jsonResult(map[string]any{"domains": rows})
		},
	})
	catalog.Register(&Tool{
		Name: "domain_get", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "One domain in full: its aliases, its credentials, its settings.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			return jsonResult(domain)
		},
	})
	catalog.Register(&Tool{
		Name: "domain_add", Family: FamilyDomains, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionDomainManageAll},
		Description: "Add a domain to this server. The DNS records it needs come back with domain_dns_check afterwards.",
		Parameters: object(map[string]any{
			"domain":  stringProperty("the domain name"),
			"comment": stringProperty("a note about it"),
		}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
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
				CreateDomain *domainView `json:"CreateDomain"`
			}
			if err := call.Run.settings.Operations.Execute(ctx, `mutation ($domainParameters: DomainParametersInput!) { CreateDomain(domainParameters: $domainParameters) { id domain } }`, map[string]any{"domainParameters": parameters}, &result); err != nil {
				return nil, err
			}
			answer, err := jsonResult(map[string]any{"domain_id": result.CreateDomain.ID, "domain": result.CreateDomain.Domain, "note": "added; run domain_dns_check for the records to publish"})
			if err != nil {
				return nil, err
			}
			answer.Note = "added " + result.CreateDomain.Domain
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "domain_update", Family: FamilyDomains, Risk: RiskWrite, Permissions: manage,
		Description: "Change a domain's settings: comment, spam threshold, mail servers for a domain hosted elsewhere, link host.",
		Parameters: object(map[string]any{
			"domain":               stringProperty("the domain, by name or id"),
			"comment":              stringProperty("a note about it"),
			"spam_score_threshold": map[string]any{"type": "number", "description": "the score above which mail is junk"},
			"mail_servers":         arrayProperty("mail servers to forward to, host:port each", stringProperty("host:port")),
			"link_host":            stringProperty("the host links are written with"),
		}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain             string   `json:"domain"`
				Comment            *string  `json:"comment"`
				SpamScoreThreshold *float64 `json:"spam_score_threshold"`
				MailServers        []string `json:"mail_servers"`
				LinkHost           *string  `json:"link_host"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
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
			if _, err := execute(ctx, call, `mutation ($domainId: String!, $domainParameters: DomainParametersInput!) { UpdateDomain(domainId: $domainId, domainParameters: $domainParameters) { id } }`, map[string]any{"domainId": domain.ID, "domainParameters": parameters}); err != nil {
				return nil, err
			}
			return textResult("changed %s", domain.Domain), nil
		},
	})
	catalog.Register(&Tool{
		Name: "domain_remove", Family: FamilyDomains, Risk: RiskDestructive, Permissions: []models.Permission{models.PermissionDomainManageAll},
		Description: "Remove a domain and everything under it: aliases, credentials, templates. Cannot be undone.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Preview: func(arguments json.RawMessage) string {
			return "Remove the domain " + strings.TrimSpace(string(arguments)) + " and everything under it"
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			if _, err := execute(ctx, call, `mutation ($domainId: String!) { DeleteDomain(domainId: $domainId) }`, map[string]any{"domainId": domain.ID}); err != nil {
				return nil, err
			}
			return textResult("removed %s", domain.Domain), nil
		},
	})
	catalog.Register(&Tool{
		Name: "domain_dns_check", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "Check a domain's DNS: which records are published as they should be and which are missing or wrong, with the values to publish.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			result, err := execute(ctx, call, `mutation ($domainId: String!) { CheckDomain(domainId: $domainId) { domain records { domain checkedAt error records { type name expected found } } } }`, map[string]any{"domainId": domain.ID})
			if err != nil {
				return nil, err
			}
			answer, err := jsonResult(map[string]any{"domain": domain.Domain, "records": result["CheckDomain"]})
			if err != nil {
				return nil, err
			}
			answer.Untrusted = true
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "alias_list", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "The addresses of a domain: each alias with its pattern, what it does (a mailbox, a forward, a webhook, a mail server) and whether it is on.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			rows := make([]map[string]any, 0, len(domain.Aliases))
			for _, alias := range domain.Aliases {
				rows = append(rows, map[string]any{"alias_id": alias.ID, "pattern": alias.Pattern, "kind": alias.Kind, "email": alias.Email, "webhook": alias.Webhook, "mailbox_id": alias.MailboxID, "comment": alias.Comment, "disabled": alias.Disabled})
			}
			return jsonResult(map[string]any{"domain": domain.Domain, "aliases": rows})
		},
	})
	aliasFields := map[string]any{
		"domain":     stringProperty("the domain, by name or id"),
		"pattern":    stringProperty("the local part, or a pattern such as sales-* or *"),
		"kind":       enumProperty("what the alias does", "mailbox", "email", "webhook", "mailserver", "drop"),
		"email":      stringProperty("for email: where to forward"),
		"webhook":    stringProperty("for webhook: the address to post to"),
		"mailbox_id": stringProperty("for mailbox: the mailbox that receives"),
		"comment":    stringProperty("a note"),
		"disabled":   booleanProperty("off"),
	}
	catalog.Register(&Tool{
		Name: "alias_add", Family: FamilyDomains, Risk: RiskWrite, Permissions: manage,
		Description: "Add an address to a domain: what arrives at the pattern goes to a mailbox, is forwarded, is posted to a webhook, or is dropped.",
		Parameters:  object(aliasFields, "domain", "pattern", "kind"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
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
			domain, err := findDomain(ctx, call, arguments.Domain)
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
			if err := call.Run.settings.Operations.Execute(ctx, `mutation ($domainId: String!, $aliasParameters: AliasParametersInput!) { CreateAlias(domainId: $domainId, aliasParameters: $aliasParameters) { id } }`, map[string]any{"domainId": domain.ID, "aliasParameters": parameters}, &result); err != nil {
				return nil, err
			}
			answer, err := jsonResult(map[string]any{"alias_id": result.CreateAlias.ID, "address": arguments.Pattern + "@" + domain.Domain})
			if err != nil {
				return nil, err
			}
			answer.Note = "added " + arguments.Pattern + "@" + domain.Domain
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "alias_update", Family: FamilyDomains, Risk: RiskWrite, Permissions: manage,
		Description: "Change an address: its pattern, what it does, its note, or switch it off and on.",
		Parameters:  object(mergeProperties(aliasFields, map[string]any{"alias_id": stringProperty("the alias, from alias_list")}), "alias_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
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
			domains, err := listDomains(ctx, call)
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
			if _, err := execute(ctx, call, `mutation ($aliasId: String!, $aliasParameters: AliasParametersInput!) { UpdateAlias(aliasId: $aliasId, aliasParameters: $aliasParameters) { id } }`, map[string]any{"aliasId": arguments.AliasID, "aliasParameters": parameters}); err != nil {
				return nil, err
			}
			return textResult("changed the alias %s", parameters["pattern"]), nil
		},
	})
	catalog.Register(&Tool{
		Name: "alias_remove", Family: FamilyDomains, Risk: RiskDestructive, Permissions: manage,
		Description: "Remove an address. Mail to it bounces from then on.",
		Parameters:  object(map[string]any{"alias_id": stringProperty("the alias, from alias_list")}, "alias_id"),
		Preview: func(arguments json.RawMessage) string {
			return "Remove the alias " + strings.TrimSpace(string(arguments))
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				AliasID string `json:"alias_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			if _, err := execute(ctx, call, `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`, map[string]any{"aliasId": arguments.AliasID}); err != nil {
				return nil, err
			}
			return textResult("removed the alias"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "alias_match", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "Which aliases an address would reach, in order: the answer to \"where does mail to X go?\".",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id"), "address": stringProperty("the address, or its local part")}, "domain", "address"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain  string `json:"domain"`
				Address string `json:"address"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			result, err := execute(ctx, call, `query ($domainId: String!, $address: String!) { MatchAliases(domainId: $domainId, address: $address) { id pattern kind email webhook mailboxId disabled } }`, map[string]any{"domainId": domain.ID, "address": arguments.Address})
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"matches": result["MatchAliases"]})
		},
	})
	catalog.Register(&Tool{
		Name: "credential_list", Family: FamilyDomains, Risk: RiskRead, Permissions: manage,
		Description: "The sending credentials of a domain: what programs authenticate with to send as it.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"domain": domain.Domain, "credentials": domain.Credentials})
		},
	})
	catalog.Register(&Tool{
		Name: "credential_create", Family: FamilyDomains, Risk: RiskWrite, Permissions: manage,
		Description: "Make a sending credential for a domain. The secret is shown once, to the person, exactly; never keep it.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id"), "comment": stringProperty("what it is for"), "alias": stringProperty("the address it sends as, when limited to one")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain  string  `json:"domain"`
				Comment *string `json:"comment"`
				Alias   *string `json:"alias"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
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
			result, err := execute(ctx, call, `mutation ($domainId: String!, $credentialParameters: CredentialParametersInput!) { CreateCredential(domainId: $domainId, credentialParameters: $credentialParameters) { credential { id alias comment } host port username password } }`, map[string]any{"domainId": domain.ID, "credentialParameters": parameters})
			if err != nil {
				return nil, err
			}
			answer, err := jsonResult(result["CreateCredential"])
			if err != nil {
				return nil, err
			}
			answer.ShowVerbatim = true
			answer.Note = "made a credential for " + domain.Domain
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "credential_update", Family: FamilyDomains, Risk: RiskWrite, Permissions: manage,
		Description: "Change a credential's note or the address it is limited to, or switch it off and on.",
		Parameters:  object(map[string]any{"credential_id": stringProperty("the credential, from credential_list"), "comment": stringProperty("a note"), "alias": stringProperty("the address it sends as"), "disabled": booleanProperty("off")}, "credential_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
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
			if _, err := execute(ctx, call, `mutation ($credentialId: String!, $credentialParameters: CredentialParametersInput!) { UpdateCredential(credentialId: $credentialId, credentialParameters: $credentialParameters) { id } }`, map[string]any{"credentialId": arguments.CredentialID, "credentialParameters": parameters}); err != nil {
				return nil, err
			}
			return textResult("changed the credential"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "credential_remove", Family: FamilyDomains, Risk: RiskDestructive, Permissions: manage,
		Description: "Remove a sending credential. Whatever used it stops sending.",
		Parameters:  object(map[string]any{"credential_id": stringProperty("the credential, from credential_list")}, "credential_id"),
		Preview: func(arguments json.RawMessage) string {
			return "Remove the credential " + strings.TrimSpace(string(arguments))
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				CredentialID string `json:"credential_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			if _, err := execute(ctx, call, `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`, map[string]any{"credentialId": arguments.CredentialID}); err != nil {
				return nil, err
			}
			return textResult("removed the credential"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "queue_list", Family: FamilyDomains, Risk: RiskRead, Permissions: []models.Permission{models.PermissionQueueManage},
		Description: "Deliveries still waiting to go out, or being retried, for a domain.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			result, err := execute(ctx, call, `query ($domainId: String!) { ListPendingDeliveries(domainId: $domainId) { id mailId recipient kind status attempts attemptedAt retryAt } }`, map[string]any{"domainId": domain.ID})
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"domain": domain.Domain, "pending": result["ListPendingDeliveries"]})
		},
	})
	catalog.Register(&Tool{
		Name: "queue_retry", Family: FamilyDomains, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionQueueManage},
		Description: "Try a waiting delivery again now.",
		Parameters:  object(map[string]any{"delivery_id": stringProperty("the delivery, from queue_list")}, "delivery_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				DeliveryID string `json:"delivery_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			result, err := execute(ctx, call, `mutation ($deliveryId: String!) { RetryDelivery(deliveryId: $deliveryId) { id status retryAt } }`, map[string]any{"deliveryId": arguments.DeliveryID})
			if err != nil {
				return nil, err
			}
			return jsonResult(result["RetryDelivery"])
		},
	})
}

// --- Family 3: the mail audit ----------------------------------------------

func registerAuditTools(catalog *Catalog) {
	audit := []models.Permission{models.PermissionMailAudit, models.PermissionMailAuditAll}
	catalog.Register(&Tool{
		Name: "mail_audit_search", Family: FamilyAudit, Risk: RiskRead, Permissions: audit,
		Description: "Every message that passed through a domain the person audits — arriving and leaving, delivered and refused — as the operator's Mail page shows it. Not the person's own mailbox: that is mail_search.",
		Parameters: object(map[string]any{
			"domain":  stringProperty("the domain, by name or id"),
			"from":    stringProperty("part of the sender"),
			"to":      stringProperty("part of a recipient"),
			"subject": stringProperty("part of the subject"),
			"status":  stringProperty("accepted, rejected, and the rest"),
			"kind":    enumProperty("arriving or leaving", "incoming", "outgoing"),
			"since":   stringProperty("an ISO date"),
			"limit":   integerProperty("how many, 20 by default"),
		}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
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
			domain, err := findDomain(ctx, call, arguments.Domain)
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
				since, err := parseTime(arguments.Since, Location(call.Run.Owner()), time.Now())
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
			result, err := execute(ctx, call, `query ($domainId: String!, $aggregations: [StageInput!]) { ListMails(domainId: $domainId, aggregations: $aggregations) { id sender recipients from subject status kind size receivedAt } }`, map[string]any{"domainId": domain.ID, "aggregations": stages})
			if err != nil {
				return nil, err
			}
			mails, _ := result["ListMails"].([]any)
			if len(mails) > limit {
				mails = mails[:limit]
			}
			answer, err := jsonResult(map[string]any{"domain": domain.Domain, "mails": mails})
			if err != nil {
				return nil, err
			}
			answer.Untrusted = true
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "mail_audit_get", Family: FamilyAudit, Risk: RiskRead, Permissions: audit,
		Description: "One message of the audit in full: envelope, headers, authentication results, and its deliveries.",
		Parameters:  object(map[string]any{"mail_id": stringProperty("the message, from mail_audit_search")}, "mail_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				MailID string `json:"mail_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			result, err := execute(ctx, call, `query ($mailId: String!) { GetMail(mailId: $mailId) { id envelopeId hello ip rdns sender recipients messageId from subject status kind size receivedAt authenticationResults { spf { result } dkims { result domain } dmarc { result } spamFilter { result score } } } ListDeliveriesByMail(mailId: $mailId) { id recipient kind status attempts attemptedAt deliveredAt droppedAt } }`, map[string]any{"mailId": arguments.MailID})
			if err != nil {
				return nil, err
			}
			answer, err := jsonResult(map[string]any{"mail": result["GetMail"], "deliveries": result["ListDeliveriesByMail"]})
			if err != nil {
				return nil, err
			}
			answer.Untrusted = true
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "mail_audit_content", Family: FamilyAudit, Risk: RiskRead, Permissions: audit,
		Description: "The text of a message of the audit. Reading somebody else's mail: only when the person asked for that message.",
		Parameters:  object(map[string]any{"mail_id": stringProperty("the message"), "max_characters": integerProperty("bound, 8000 by default")}, "mail_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				MailID        string `json:"mail_id"`
				MaxCharacters int    `json:"max_characters"`
			}](call)
			if err != nil {
				return nil, err
			}
			content, err := getContent(ctx, call.Run.settings.Operations, arguments.MailID)
			if err != nil {
				return nil, err
			}
			if content == nil {
				return nil, fmt.Errorf("the message has no stored content")
			}
			text := content.Text
			if strings.TrimSpace(text) == "" && content.HTML != "" {
				text = HTMLToText(content.HTML)
			}
			text = normalizeText(text)
			limit := arguments.MaxCharacters
			if limit <= 0 {
				limit = 8000
			}
			if len(text) > limit {
				text = text[:limit]
			}
			answer, err := jsonResult(map[string]any{"text": text})
			if err != nil {
				return nil, err
			}
			answer.Untrusted = true
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "mail_audit_mark", Family: FamilyAudit, Risk: RiskWrite, Permissions: audit,
		Description: "Tell the spam filter about a message of the audit: spam, or ham (not spam), so it learns.",
		Parameters:  object(map[string]any{"mail_id": stringProperty("the message"), "label": enumProperty("what it is", "spam", "ham")}, "mail_id", "label"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				MailID string `json:"mail_id"`
				Label  string `json:"label"`
			}](call)
			if err != nil {
				return nil, err
			}
			if _, err := execute(ctx, call, `mutation ($mailId: String!, $label: String!) { MarkMail(mailId: $mailId, label: $label) { mailId label learnedSpam learnedHam } }`, map[string]any{"mailId": arguments.MailID, "label": arguments.Label}); err != nil {
				return nil, err
			}
			return textResult("marked the message as %s", arguments.Label), nil
		},
	})
	catalog.Register(&Tool{
		Name: "report_list", Family: FamilyAudit, Risk: RiskRead, Permissions: []models.Permission{models.PermissionReportRead},
		Description: "DMARC aggregate reports received about a domain: who sent as it, and whether it aligned.",
		Parameters:  object(map[string]any{"domain": stringProperty("the domain, by name or id"), "limit": integerProperty("how many, 20 by default")}, "domain"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Domain string `json:"domain"`
				Limit  int    `json:"limit"`
			}](call)
			if err != nil {
				return nil, err
			}
			domain, err := findDomain(ctx, call, arguments.Domain)
			if err != nil {
				return nil, err
			}
			limit := arguments.Limit
			if limit <= 0 {
				limit = 20
			}
			result, err := execute(ctx, call, `query ($domainId: String!) { ListReports(domainId: $domainId) { id domainId beginAt endAt count ip rdns fromDomain senderDomain } }`, map[string]any{"domainId": domain.ID})
			if err != nil {
				return nil, err
			}
			reports, _ := result["ListReports"].([]any)
			if len(reports) > limit {
				reports = reports[:limit]
			}
			answer, err := jsonResult(map[string]any{"domain": domain.Domain, "reports": reports})
			if err != nil {
				return nil, err
			}
			answer.Untrusted = true
			return answer, nil
		},
	})
}

// --- Family 4: people and access -------------------------------------------

func registerPeopleTools(catalog *Catalog) {
	catalog.Register(&Tool{
		Name: "user_list", Family: FamilyPeople, Risk: RiskRead, Permissions: []models.Permission{models.PermissionUserManage},
		Description: "The accounts on this server, with their groups and whether they may sign in.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			result, err := execute(ctx, call, `query { ListUsers { id username name email disabledAt groupIds locale timezone } }`, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"users": result["ListUsers"]})
		},
	})
	catalog.Register(&Tool{
		Name: "user_add", Family: FamilyPeople, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionUserManage},
		Description: "Make an account. Without a password the person signs in another way — a passkey, single sign-on, or a password set later.",
		Parameters: object(map[string]any{
			"username":  stringProperty("the username"),
			"name":      stringProperty("what to call them"),
			"email":     stringProperty("where notifications go"),
			"group_ids": arrayProperty("the groups to put them in", stringProperty("a group id, from group_list")),
		}, "username"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Username string   `json:"username"`
				Name     *string  `json:"name"`
				Email    *string  `json:"email"`
				GroupIDs []string `json:"group_ids"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{"username": strings.TrimSpace(arguments.Username)}
			if arguments.Name != nil {
				variables["name"] = *arguments.Name
			}
			if arguments.Email != nil {
				variables["email"] = *arguments.Email
			}
			if arguments.GroupIDs != nil {
				variables["groupIds"] = arguments.GroupIDs
			}
			result, err := execute(ctx, call, `mutation ($username: String!, $name: String, $email: String, $groupIds: [String!]) { CreateUser(username: $username, name: $name, email: $email, groupIds: $groupIds) { id username } }`, variables)
			if err != nil {
				return nil, err
			}
			answer, err := jsonResult(result["CreateUser"])
			if err != nil {
				return nil, err
			}
			answer.Note = "made the account " + arguments.Username
			return answer, nil
		},
	})
	catalog.Register(&Tool{
		Name: "user_update", Family: FamilyPeople, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionUserManage},
		Description: "Change an account: name, notification address, groups, or whether it may sign in.",
		Parameters: object(map[string]any{
			"user_id":   stringProperty("the account, from user_list"),
			"name":      stringProperty("what to call them"),
			"email":     stringProperty("where notifications go"),
			"group_ids": arrayProperty("the groups, replacing the current ones", stringProperty("a group id")),
			"disabled":  booleanProperty("whether they may not sign in"),
		}, "user_id"),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				UserID   string   `json:"user_id"`
				Name     *string  `json:"name"`
				Email    *string  `json:"email"`
				GroupIDs []string `json:"group_ids"`
				Disabled *bool    `json:"disabled"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{"userId": arguments.UserID}
			if arguments.Name != nil {
				variables["name"] = *arguments.Name
			}
			if arguments.Email != nil {
				variables["email"] = *arguments.Email
			}
			if arguments.GroupIDs != nil {
				variables["groupIds"] = arguments.GroupIDs
			}
			if arguments.Disabled != nil {
				variables["disabled"] = *arguments.Disabled
			}
			if _, err := execute(ctx, call, `mutation ($userId: String!, $name: String, $email: String, $groupIds: [String!], $disabled: Boolean) { UpdateUser(userId: $userId, name: $name, email: $email, groupIds: $groupIds, disabled: $disabled) { id } }`, variables); err != nil {
				return nil, err
			}
			return textResult("changed the account"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "user_remove", Family: FamilyPeople, Risk: RiskDestructive, Permissions: []models.Permission{models.PermissionUserManage},
		Description: "Delete an account and what only it held. Cannot be undone; disabling is the reversible choice.",
		Parameters:  object(map[string]any{"user_id": stringProperty("the account, from user_list")}, "user_id"),
		Preview: func(arguments json.RawMessage) string {
			return "Delete the account " + strings.TrimSpace(string(arguments))
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				UserID string `json:"user_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			if _, err := execute(ctx, call, `mutation ($userId: String!) { DeleteUser(userId: $userId) }`, map[string]any{"userId": arguments.UserID}); err != nil {
				return nil, err
			}
			return textResult("deleted the account"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "group_list", Family: FamilyPeople, Risk: RiskRead, Permissions: []models.Permission{models.PermissionGroupManage, models.PermissionUserManage},
		Description: "The groups: who is in each, which roles it carries, which domains it reaches.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			result, err := execute(ctx, call, `query { ListGroups { id name description idpGroup userIds roleIds domainIds } }`, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"groups": result["ListGroups"]})
		},
	})
	catalog.Register(&Tool{
		Name: "group_manage", Family: FamilyPeople, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionGroupManage},
		Description: "Make, change or delete a group: its members, roles and domains. Deleting asks first.",
		Parameters: object(map[string]any{
			"action":      enumProperty("what to do", "create", "update", "delete"),
			"group_id":    stringProperty("for update and delete: the group"),
			"name":        stringProperty("the name"),
			"description": stringProperty("what it is for"),
			"user_ids":    arrayProperty("the members, replacing the current ones", stringProperty("a user id")),
			"role_ids":    arrayProperty("the roles, replacing the current ones", stringProperty("a role id")),
			"domain_ids":  arrayProperty("the domains, replacing the current ones", stringProperty("a domain id")),
		}, "action"),
		RiskOf: func(arguments json.RawMessage) Risk {
			if strings.Contains(string(arguments), `"delete"`) {
				return RiskDestructive
			}
			return RiskWrite
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Action      string   `json:"action"`
				GroupID     string   `json:"group_id"`
				Name        *string  `json:"name"`
				Description *string  `json:"description"`
				UserIDs     []string `json:"user_ids"`
				RoleIDs     []string `json:"role_ids"`
				DomainIDs   []string `json:"domain_ids"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{}
			if arguments.Name != nil {
				variables["name"] = *arguments.Name
			}
			if arguments.Description != nil {
				variables["description"] = *arguments.Description
			}
			if arguments.UserIDs != nil {
				variables["userIds"] = arguments.UserIDs
			}
			if arguments.RoleIDs != nil {
				variables["roleIds"] = arguments.RoleIDs
			}
			if arguments.DomainIDs != nil {
				variables["domainIds"] = arguments.DomainIDs
			}
			switch arguments.Action {
			case "create":
				if arguments.Name == nil {
					return nil, fmt.Errorf("a group needs a name")
				}
				result, err := execute(ctx, call, `mutation ($name: String!, $description: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) { CreateGroup(name: $name, description: $description, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) { id name } }`, variables)
				if err != nil {
					return nil, err
				}
				return jsonResult(result["CreateGroup"])
			case "update":
				variables["groupId"] = arguments.GroupID
				if _, err := execute(ctx, call, `mutation ($groupId: String!, $name: String, $description: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) { UpdateGroup(groupId: $groupId, name: $name, description: $description, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) { id } }`, variables); err != nil {
					return nil, err
				}
				return textResult("changed the group"), nil
			case "delete":
				if _, err := execute(ctx, call, `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`, map[string]any{"groupId": arguments.GroupID}); err != nil {
					return nil, err
				}
				return textResult("deleted the group"), nil
			}
			return nil, fmt.Errorf("%q is not an action of group_manage", arguments.Action)
		},
	})
	catalog.Register(&Tool{
		Name: "role_list", Family: FamilyPeople, Risk: RiskRead, Permissions: []models.Permission{models.PermissionRoleManage, models.PermissionGroupManage, models.PermissionUserManage},
		Description: "The roles and the permissions each carries, and every permission that exists with what it means.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			result, err := execute(ctx, call, `query { ListRoles { id name description permissions } ListPermissions { key kind widens } }`, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"roles": result["ListRoles"], "permissions": result["ListPermissions"]})
		},
	})
	catalog.Register(&Tool{
		Name: "role_manage", Family: FamilyPeople, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionRoleManage},
		Description: "Make, change or delete a role and its permissions. Deleting asks first.",
		Parameters: object(map[string]any{
			"action":      enumProperty("what to do", "create", "update", "delete"),
			"role_id":     stringProperty("for update and delete: the role"),
			"name":        stringProperty("the name"),
			"description": stringProperty("what it is for"),
			"permissions": arrayProperty("the permissions, replacing the current ones", stringProperty("a permission such as mail:read")),
		}, "action"),
		RiskOf: func(arguments json.RawMessage) Risk {
			if strings.Contains(string(arguments), `"delete"`) {
				return RiskDestructive
			}
			return RiskWrite
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Action      string   `json:"action"`
				RoleID      string   `json:"role_id"`
				Name        *string  `json:"name"`
				Description *string  `json:"description"`
				Permissions []string `json:"permissions"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{}
			if arguments.Name != nil {
				variables["name"] = *arguments.Name
			}
			if arguments.Description != nil {
				variables["description"] = *arguments.Description
			}
			if arguments.Permissions != nil {
				variables["permissions"] = arguments.Permissions
			}
			switch arguments.Action {
			case "create":
				if arguments.Name == nil {
					return nil, fmt.Errorf("a role needs a name")
				}
				if variables["permissions"] == nil {
					variables["permissions"] = []string{}
				}
				result, err := execute(ctx, call, `mutation ($name: String!, $description: String, $permissions: [String!]!) { CreateRole(name: $name, description: $description, permissions: $permissions) { id name } }`, variables)
				if err != nil {
					return nil, err
				}
				return jsonResult(result["CreateRole"])
			case "update":
				variables["roleId"] = arguments.RoleID
				if _, err := execute(ctx, call, `mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) { UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) { id } }`, variables); err != nil {
					return nil, err
				}
				return textResult("changed the role"), nil
			case "delete":
				if _, err := execute(ctx, call, `mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }`, map[string]any{"roleId": arguments.RoleID}); err != nil {
					return nil, err
				}
				return textResult("deleted the role"), nil
			}
			return nil, fmt.Errorf("%q is not an action of role_manage", arguments.Action)
		},
	})
	catalog.Register(&Tool{
		Name: "audit_log", Family: FamilyPeople, Risk: RiskRead, Permissions: []models.Permission{models.PermissionAuditRead},
		Description: "Who changed what, and when: the audit trail, newest first, narrowed by resource, actor or time.",
		Parameters: object(map[string]any{
			"resource_type": stringProperty("domain, alias, credential, user, group, role, settings, mailbox, agent…"),
			"resource_id":   stringProperty("one resource"),
			"actor_user_id": stringProperty("one person's doings"),
			"since":         stringProperty("an ISO date"),
			"limit":         integerProperty("how many, 30 by default"),
		}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				ResourceType string `json:"resource_type"`
				ResourceID   string `json:"resource_id"`
				ActorUserID  string `json:"actor_user_id"`
				Since        string `json:"since"`
				Limit        int    `json:"limit"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{"first": 30}
			if arguments.Limit > 0 {
				variables["first"] = arguments.Limit
			}
			for key, value := range map[string]string{"resourceType": arguments.ResourceType, "resourceId": arguments.ResourceID, "actorUserId": arguments.ActorUserID} {
				if value != "" {
					variables[key] = value
				}
			}
			if arguments.Since != "" {
				since, err := parseTime(arguments.Since, Location(call.Run.Owner()), time.Now())
				if err != nil {
					return nil, err
				}
				variables["since"] = since.UTC().Format(time.RFC3339)
			}
			result, err := execute(ctx, call, `query ($resourceType: String, $resourceId: String, $actorUserId: String, $since: DateTime, $first: Int) { ListAuditEvents(resourceType: $resourceType, resourceId: $resourceId, actorUserId: $actorUserId, since: $since, first: $first) { total events { id createdAt actorKind actorLabel resourceType resourceId resourceLabel action } } }`, variables)
			if err != nil {
				return nil, err
			}
			return jsonResult(result["ListAuditEvents"])
		},
	})
}

// --- Family 5: the server ---------------------------------------------------

func registerServerTools(catalog *Catalog) {
	manage := []models.Permission{models.PermissionServerManage}
	catalog.Register(&Tool{
		Name: "server_status", Family: FamilyServer, Risk: RiskRead, Permissions: manage,
		Description: "This server: version, instance, uptime, whether a restart is pending, and whether a newer release exists.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			result, err := execute(ctx, call, `query { GetServerStatus { instance version commit startedAt uptimeSeconds pendingRestart } GetUpgrade { current latest available notes url checkedAt error } GetServerAddresses { ipv4 ipv6 } }`, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"status": result["GetServerStatus"], "upgrade": result["GetUpgrade"], "addresses": result["GetServerAddresses"]})
		},
	})
	catalog.Register(&Tool{
		Name: "settings_get", Family: FamilyServer, Risk: RiskRead, Permissions: manage,
		Description: "The server's settings, with secrets redacted, by section: smtp, submission, imap, relay, antispam, antivirus, certificates, storage, sso, agent and the rest.",
		Parameters:  object(map[string]any{"section": stringProperty("one section; all when absent")}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Section string `json:"section"`
			}](call)
			if err != nil {
				return nil, err
			}
			// Read from the configuration itself, redacted: the same view the
			// settings page shows, and the person holds server:manage or the
			// tool is not offered.
			redacted, err := call.Run.agent.settings.Configuration().Redact()
			if err != nil {
				return nil, err
			}
			encoded, err := yaml.Marshal(redacted)
			if err != nil {
				return nil, err
			}
			var settings map[string]any
			if err := yaml.Unmarshal(encoded, &settings); err != nil {
				return nil, err
			}
			delete(settings, "database")
			if section := strings.TrimSpace(arguments.Section); section != "" {
				value, ok := settings[section]
				if !ok {
					return nil, fmt.Errorf("there is no settings section %q", section)
				}
				return jsonResult(map[string]any{section: value})
			}
			return jsonResult(settings)
		},
	})
	catalog.Register(&Tool{
		Name: "settings_update", Family: FamilyServer, Risk: RiskWrite, Permissions: manage,
		Description: "Change one section of the server's settings. Give the section and the fields to set, exactly as settings_get shows them; a secret left out or shown redacted is kept.",
		Parameters:  object(map[string]any{"section": stringProperty("the section: smtp, submission, imap, relay, antispam, antivirus, certificates, storage, sso, proxy, upgrade, session, passkey, listen, identity, geoip, resolver, agent, s3, route53"), "values": map[string]any{"type": "object", "description": "the fields to set"}}, "section", "values"),
		Preview: func(arguments json.RawMessage) string {
			return "Change the server settings: " + strings.TrimSpace(string(arguments))
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Section string         `json:"section"`
				Values  map[string]any `json:"values"`
			}](call)
			if err != nil {
				return nil, err
			}
			section := strings.TrimSpace(arguments.Section)
			inputs := map[string]string{"s3": "S3ParametersInput", "route53": "Route53ParametersInput", "antivirus": "ServiceParametersInput", "antispam": "AntispamParametersInput", "relay": "RelayParametersInput", "submission": "SubmissionParametersInput", "imap": "IMAPParametersInput", "sso": "SSOParametersInput", "proxy": "ProxyParametersInput", "upgrade": "UpgradeParametersInput", "certificates": "CertificateParametersInput", "smtp": "SMTPParametersInput", "resolver": "ResolverParametersInput", "session": "SessionParametersInput", "passkey": "PasskeyParametersInput", "listen": "ListenParametersInput", "identity": "IdentityParametersInput", "storage": "StorageParametersInput", "geoip": "GeoIPParametersInput", "agent": "AgentParametersInput"}
			input, ok := inputs[section]
			if !ok {
				return nil, fmt.Errorf("there is no settings section %q", section)
			}
			document := fmt.Sprintf(`mutation ($values: %s!) { UpdateSettings(%s: $values) { agent { enabled } } }`, input, section)
			if _, err := execute(ctx, call, document, map[string]any{"values": arguments.Values}); err != nil {
				return nil, err
			}
			return textResult("changed the %s settings", section), nil
		},
	})
	catalog.Register(&Tool{
		Name: "server_upgrade", Family: FamilyServer, Risk: RiskOutward, Permissions: manage,
		Description: "Apply a newer release of the server, or restart it. The server goes away for a moment; it always asks first.",
		Parameters:  object(map[string]any{"action": enumProperty("upgrade to the latest, or restart", "upgrade", "restart"), "version": stringProperty("for upgrade: a version, the latest by default")}, "action"),
		Preview: func(arguments json.RawMessage) string {
			return "Upgrade or restart the server: " + strings.TrimSpace(string(arguments))
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Action  string `json:"action"`
				Version string `json:"version"`
			}](call)
			if err != nil {
				return nil, err
			}
			switch arguments.Action {
			case "upgrade":
				variables := map[string]any{}
				if arguments.Version != "" {
					variables["version"] = arguments.Version
				}
				result, err := execute(ctx, call, `mutation ($version: String) { ApplyUpgrade(version: $version) { current latest attemptedAt error } }`, variables)
				if err != nil {
					return nil, err
				}
				return jsonResult(result["ApplyUpgrade"])
			case "restart":
				if _, err := execute(ctx, call, `mutation { RestartServer { started instance supervision } }`, nil); err != nil {
					return nil, err
				}
				return textResult("restarting"), nil
			}
			return nil, fmt.Errorf("%q is not an action of server_upgrade", arguments.Action)
		},
	})
}

// --- Family 6: the person's own account -------------------------------------

func registerAccountTools(catalog *Catalog) {
	catalog.Register(&Tool{
		Name: "account_get", Family: FamilyAccount, Risk: RiskRead,
		Description: "The person's own account: name, username, notification address, language, zone, and their sessions, API tokens and passkeys.",
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			result, err := execute(ctx, call, `query { GetCurrentUser { id username name email locale timezone timezoneMode } ListTokens { id name created expires lastUsed revoked } ListSessions { id current created expires lastUsed ip userAgent } ListPasskeys { id name createdAt } }`, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(map[string]any{"account": result["GetCurrentUser"], "tokens": result["ListTokens"], "sessions": result["ListSessions"], "passkeys": result["ListPasskeys"]})
		},
	})
	catalog.Register(&Tool{
		Name: "account_update", Family: FamilyAccount, Risk: RiskWrite,
		Description: "Change the person's own name, notification address, language or time zone.",
		Parameters: object(map[string]any{
			"name":          stringProperty("what to call them"),
			"email":         stringProperty("where notifications go"),
			"locale":        stringProperty("the language of the dashboard: en, ja, zh"),
			"timezone":      stringProperty("an IANA zone"),
			"timezone_mode": enumProperty("follow the browser, or keep the zone", "auto", "fixed"),
		}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Name         *string `json:"name"`
				Email        *string `json:"email"`
				Locale       *string `json:"locale"`
				Timezone     *string `json:"timezone"`
				TimezoneMode *string `json:"timezone_mode"`
			}](call)
			if err != nil {
				return nil, err
			}
			variables := map[string]any{"userId": call.Run.Owner().ID}
			for key, value := range map[string]*string{"name": arguments.Name, "email": arguments.Email, "locale": arguments.Locale, "timezone": arguments.Timezone, "timezoneMode": arguments.TimezoneMode} {
				if value != nil {
					variables[key] = *value
				}
			}
			if len(variables) == 1 {
				return nil, fmt.Errorf("nothing to change")
			}
			if _, err := execute(ctx, call, `mutation ($userId: String!, $name: String, $email: String, $locale: String, $timezone: String, $timezoneMode: String) { UpdateUser(userId: $userId, name: $name, email: $email, locale: $locale, timezone: $timezone, timezoneMode: $timezoneMode) { id } }`, variables); err != nil {
				return nil, err
			}
			return textResult("changed the account"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "token_manage", Family: FamilyAccount, Risk: RiskWrite,
		Description: "The person's API tokens: make one (shown once, to them, exactly) or revoke one.",
		Parameters:  object(map[string]any{"action": enumProperty("create or revoke", "create", "revoke"), "name": stringProperty("for create: what the token is for"), "token_id": stringProperty("for revoke: the token"), "lifetime": stringProperty("for create: how long it lives, such as 30d")}, "action"),
		RiskOf: func(arguments json.RawMessage) Risk {
			if strings.Contains(string(arguments), `"revoke"`) {
				return RiskDestructive
			}
			return RiskWrite
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Action   string `json:"action"`
				Name     string `json:"name"`
				TokenID  string `json:"token_id"`
				Lifetime string `json:"lifetime"`
			}](call)
			if err != nil {
				return nil, err
			}
			switch arguments.Action {
			case "create":
				variables := map[string]any{"name": arguments.Name}
				if arguments.Lifetime != "" {
					variables["lifetime"] = arguments.Lifetime
				}
				result, err := execute(ctx, call, `mutation ($name: String!, $lifetime: String) { CreateToken(name: $name, lifetime: $lifetime) { token { id name expires } secret } }`, variables)
				if err != nil {
					return nil, err
				}
				answer, err := jsonResult(result["CreateToken"])
				if err != nil {
					return nil, err
				}
				answer.ShowVerbatim = true
				return answer, nil
			case "revoke":
				if _, err := execute(ctx, call, `mutation ($tokenId: String!) { DeleteToken(tokenId: $tokenId) }`, map[string]any{"tokenId": arguments.TokenID}); err != nil {
					return nil, err
				}
				return textResult("revoked the token"), nil
			}
			return nil, fmt.Errorf("%q is not an action of token_manage", arguments.Action)
		},
	})
	catalog.Register(&Tool{
		Name: "session_revoke", Family: FamilyAccount, Risk: RiskWrite,
		Description: "Sign the person out elsewhere: one session, or every session but this one.",
		Parameters:  object(map[string]any{"session_id": stringProperty("one session, from account_get; all of them when absent")}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				SessionID string `json:"session_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			if arguments.SessionID != "" {
				if _, err := execute(ctx, call, `mutation ($sessionId: String!) { RevokeSession(sessionId: $sessionId) }`, map[string]any{"sessionId": arguments.SessionID}); err != nil {
					return nil, err
				}
				return textResult("revoked the session"), nil
			}
			if _, err := execute(ctx, call, `mutation { RevokeAllSessions { authenticated username } }`, nil); err != nil {
				return nil, err
			}
			return textResult("revoked every other session"), nil
		},
	})
	catalog.Register(&Tool{
		Name: "app_password_manage", Family: FamilyAccount, Risk: RiskWrite, Permissions: []models.Permission{models.PermissionMailboxManage},
		Description: "App passwords for a mailbox, which mail programs sign in with: list, make (shown once, exactly) or remove.",
		Parameters:  object(map[string]any{"action": enumProperty("what to do", "list", "create", "remove"), "mailbox": stringProperty("the mailbox, by name or id"), "name": stringProperty("for create: which program"), "app_password_id": stringProperty("for remove: the app password")}, "action"),
		RiskOf: func(arguments json.RawMessage) Risk {
			if strings.Contains(string(arguments), `"remove"`) {
				return RiskDestructive
			}
			if strings.Contains(string(arguments), `"list"`) {
				return RiskRead
			}
			return RiskWrite
		},
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Action        string `json:"action"`
				Mailbox       string `json:"mailbox"`
				Name          string `json:"name"`
				AppPasswordID string `json:"app_password_id"`
			}](call)
			if err != nil {
				return nil, err
			}
			views, err := grantedMailboxes(ctx, call.Run.settings.Operations)
			if err != nil {
				return nil, err
			}
			switch arguments.Action {
			case "list":
				view, err := findMailbox(views, arguments.Mailbox)
				if err != nil {
					return nil, err
				}
				result, err := execute(ctx, call, `query ($mailboxId: String!) { ListMailboxAppPasswords(mailboxId: $mailboxId) { id name createdAt lastUsedAt } }`, map[string]any{"mailboxId": view.Mailbox.ID})
				if err != nil {
					return nil, err
				}
				return jsonResult(map[string]any{"mailbox": view.Mailbox.Name, "app_passwords": result["ListMailboxAppPasswords"]})
			case "create":
				view, err := findMailbox(views, arguments.Mailbox)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(arguments.Name) == "" {
					return nil, fmt.Errorf("an app password needs a name: the program it is for")
				}
				result, err := execute(ctx, call, `mutation ($mailboxId: String!, $name: String!) { CreateMailboxAppPassword(mailboxId: $mailboxId, name: $name) { password username appPassword { id name } } }`, map[string]any{"mailboxId": view.Mailbox.ID, "name": arguments.Name})
				if err != nil {
					return nil, err
				}
				answer, err := jsonResult(result["CreateMailboxAppPassword"])
				if err != nil {
					return nil, err
				}
				answer.ShowVerbatim = true
				return answer, nil
			case "remove":
				if _, err := execute(ctx, call, `mutation ($appPasswordId: String!) { DeleteMailboxAppPassword(appPasswordId: $appPasswordId) }`, map[string]any{"appPasswordId": arguments.AppPasswordID}); err != nil {
					return nil, err
				}
				return textResult("removed the app password"), nil
			}
			return nil, fmt.Errorf("%q is not an action of app_password_manage", arguments.Action)
		},
	})
	catalog.Register(&Tool{
		Name: "access_explain", Family: FamilyAccount, Core: true, Risk: RiskRead,
		Description: "What the person may do on this server, in words, and why a tool is or is not available to them. Use it before saying that something cannot be done.",
		Parameters:  object(map[string]any{"tool": stringProperty("a tool to explain; every permission when absent")}),
		Run: func(ctx context.Context, call *Call) (*Result, error) {
			arguments, err := decodeArguments[struct {
				Tool string `json:"tool"`
			}](call)
			if err != nil {
				return nil, err
			}
			run := call.Run
			permissions := run.settings.Operations.Permissions()
			configuration := run.agent.settings.Configuration()
			if name := strings.TrimSpace(arguments.Tool); name != "" {
				tool := run.agent.catalog.Get(name)
				if tool == nil {
					return textResult("there is no tool named %q", name), nil
				}
				reasons := []string{}
				if !allowedByPermissions(tool, permissions) {
					needs := make([]string, 0, len(tool.Permissions))
					for _, permission := range tool.Permissions {
						needs = append(needs, string(permission))
					}
					reasons = append(reasons, "the person lacks a permission it needs: one of "+strings.Join(needs, ", "))
				}
				if listed(configuration.Agent.Tools.Disabled, tool) {
					reasons = append(reasons, "the operator switched it off for agents on this server")
				}
				if run.settings.ReadOnly && tool.Risk != RiskRead {
					reasons = append(reasons, "this conversation is read-only")
				}
				if len(reasons) == 0 {
					return jsonResult(map[string]any{"tool": name, "available": true, "risk": tool.Risk, "asks_first": NeedsConfirmation(tool, nil, &configuration.Agent.Tools, run.settings.Agent)})
				}
				return jsonResult(map[string]any{"tool": name, "available": false, "because": reasons})
			}
			return jsonResult(map[string]any{"may": permissionWords(permissions), "offered_tools": len(run.offered), "switched_off_by_operator": configuration.Agent.Tools.Disabled})
		},
	})
}
