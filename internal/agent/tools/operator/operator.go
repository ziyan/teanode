// Package operator is what the operator's tools share: a document run as
// the person with its answer as a map, and the domains the person manages
// as the API lists them. No tool of its own.
package operator

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// Execute runs a document as the person and discards the shape of the answer.
func Execute(ctx context.Context, document string, variables map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := tools.MustRun(ctx).Operations().Execute(ctx, document, variables, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DomainView is a domain as ListDomains returns it, in the parts the tools
// use.
type DomainView struct {
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

// ListDomains is the domains the person manages.
func ListDomains(ctx context.Context) ([]*DomainView, error) {
	var result struct {
		ListDomains []*DomainView `json:"ListDomains"`
	}
	if err := tools.MustRun(ctx).Operations().Execute(ctx, documentListDomains, nil, &result); err != nil {
		return nil, err
	}
	return result.ListDomains, nil
}

// FindDomain resolves a domain by name or id among the ones the person
// manages.
func FindDomain(ctx context.Context, nameOrId string) (*DomainView, error) {
	domains, err := ListDomains(ctx)
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
