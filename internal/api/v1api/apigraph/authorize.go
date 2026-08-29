package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

// requireOperator checks that the caller may use the API.
//
// There is one operator now, not a set of tenants: whoever can log into the
// dashboard administers every configured domain.
//
// An unclaimed server refuses this too. It used to allow everything while no
// account existed, on the reasoning that onboarding has to be reachable by
// somebody who cannot log in yet — but that opened the whole API rather than
// the two operations onboarding needs, so a server between first start and
// being claimed would hand its domains, aliases and signing selectors to
// whoever asked, and then let them claim it. The onboarding path does not come
// through here: GetSession and CreateFirstAccount never call this, which is
// what makes claiming the server still possible.
func (self *graph) requireOperator(ctx context.Context) error {
	if api.ContextAuthenticatedUsername(ctx) == "" {
		return api.ErrNotLoggedIn
	}
	return nil
}

// requireDomain checks the caller may use the API and that the domain is
// configured. A domain identifier that is not in the configuration reads as
// not found, which is also what a caller sees for a domain deleted since the
// mail referencing it arrived.
func (self *graph) requireDomain(ctx context.Context, domainId string) (*config.Domain, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	domain := self.config.Current().FindDomainByID(domainId)
	if domain == nil {
		return nil, api.ErrNotFound
	}
	return domain, nil
}

// domainsToList resolves a domain filter into the identifiers to query.
//
// An empty filter means every configured domain rather than none, because a
// dashboard that shows nothing until a domain is chosen is a dashboard that
// answers no question on the first screen.
func (self *graph) domainsToList(ctx context.Context, domainId string) ([]string, error) {
	if domainId != "" {
		domain, err := self.requireDomain(ctx, domainId)
		if err != nil {
			return nil, err
		}
		return []string{domain.ID}, nil
	}

	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	domainIds := make([]string, 0, len(configuration.Domains))
	for _, domain := range configuration.Domains {
		domainIds = append(domainIds, domain.ID)
	}
	return domainIds, nil
}

// requireAccount is requireOperator plus the account it resolved to.
//
// Anything about a person rather than about the server needs the account
// itself: its identifier is what passkeys, sessions and tokens are stored
// against, and the username the request was authenticated as is only a way of
// finding it.
func (self *graph) requireAccount(ctx context.Context) (*config.User, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	username := api.ContextAuthenticatedUsername(ctx)
	user := self.config.Current().FindUser(username)
	if user == nil {
		// A server with no accounts, reached over a socket that needs none.
		// There is nobody to register a passkey for.
		return nil, api.ErrNotLoggedIn
	}
	return user, nil
}

// findAccountByID is how a stored credential names its account.
func (self *graph) findAccountByID(userId string) *config.User {
	if userId == "" {
		return nil
	}
	for _, user := range self.config.Current().Users {
		if user != nil && user.ID == userId {
			return user
		}
	}
	return nil
}
