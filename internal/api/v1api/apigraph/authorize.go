package apigraph

import (
	"context"
	"errors"
	"fmt"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Every resolver authorises itself, because the GraphQL endpoint is reachable
// without a session: logging in happens at the same endpoint as everything
// else. TestEveryOperationAuthorises reads the source and fails when one does
// not call one of the helpers here.
//
// The rule for a refusal is "not found", never "forbidden": "you may not touch
// this" confirms the row exists, which is itself a leak. Only the checks that
// have no row to hide — "you are not signed in" — say so.
//
// An unclaimed server refuses everything here too. Onboarding does not come
// through these helpers: GetSession and CreateFirstAccount never call them,
// which is what makes claiming the server still possible.

// requireSignedIn checks that the caller is somebody: an account, or the
// console. Enough for what is about the caller themselves — their tokens,
// their sessions, their passkeys.
func (self *graph) requireSignedIn(ctx context.Context) (*api.Principal, error) {
	principal := api.ContextPrincipal(ctx)
	if principal == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// requirePermission checks that the caller holds a server or all-domains
// permission.
func (self *graph) requirePermission(ctx context.Context, permission models.Permission) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Has(permission) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireAnyPermission checks that the caller holds a domain permission over
// at least one domain, or everywhere: what opens a page that lists things
// before any one of them is chosen.
func (self *graph) requireAnyPermission(ctx context.Context, permission models.Permission) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.HasAnywhere(permission) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireManagement checks that the caller may see the management side of
// the web UI at all: the server's own addresses, and the like.
func (self *graph) requireManagement(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Manages() {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireDomainPermission checks that the caller holds a domain permission
// over this domain, and returns the domain. A domain the caller may not touch
// reads as not found, which is also what a caller sees for a domain deleted
// since the mail referencing it arrived.
func (self *graph) requireDomainPermission(ctx context.Context, permission models.Permission, domainId string) (*models.Domain, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if domainId == "" || !principal.Permissions.HasOverDomain(permission, domainId) {
		return nil, api.ErrNotFound
	}
	domain, err := self.transaction(ctx).GetDomain(domainId)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, api.ErrNotFound
	}
	return domain, nil
}

// domainsToList resolves a domain filter into the domains to query: the one
// named, when the caller holds the permission over it, or every domain they
// hold it over when none is named.
//
// An empty filter means every domain the caller may see rather than none,
// because a page that shows nothing until a domain is chosen is a page that
// answers no question on the first screen.
func (self *graph) domainsToList(ctx context.Context, permission models.Permission, domainId string) ([]string, error) {
	if domainId != "" {
		domain, err := self.requireDomainPermission(ctx, permission, domainId)
		if err != nil {
			return nil, err
		}
		return []string{domain.ID}, nil
	}
	principal, err := self.requireAnyPermission(ctx, permission)
	if err != nil {
		return nil, err
	}
	domainIds, all := principal.Permissions.DomainsWith(permission)
	if !all {
		return domainIds, nil
	}
	domains, err := self.transaction(ctx).ListDomains()
	if err != nil {
		return nil, err
	}
	domainIds = make([]string, 0, len(domains))
	for _, domain := range domains {
		domainIds = append(domainIds, domain.ID)
	}
	return domainIds, nil
}

// requireAccount is requireSignedIn plus the account it resolved to.
//
// Anything about a person rather than about the server needs the account
// itself: its identifier is what passkeys, sessions and tokens are stored
// against, and the username the request was authenticated as is only a way of
// finding it. The console is not an account.
func (self *graph) requireAccount(ctx context.Context) (*models.User, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal.User, nil
}

// transaction is the one this request runs in.
func (self *graph) transaction(ctx context.Context) db.Transaction {
	return api.ContextTransaction(ctx)
}

// domainExists says whether a row's domain is still configured. Stored mail
// keeps its domain identifier after the domain is deleted, and the API says
// so rather than pretending the mail is gone.
func (self *graph) domainExists(ctx context.Context, domainId string) (bool, error) {
	if domainId == "" {
		return false, nil
	}
	domain, err := self.transaction(ctx).GetDomain(domainId)
	if err != nil {
		return false, err
	}
	return domain != nil, nil
}

// findAccountById is how a stored credential names its account.
func (self *graph) findAccountById(userId string) *models.User {
	if userId == "" {
		return nil
	}
	user, err := self.database.GetUser(userId)
	if err != nil {
		log.Errorf("failed to read the account %q: %s", userId, err)
		return nil
	}
	return user
}

// operatorName is who to name in a log line for a change.
func operatorName(ctx context.Context) string {
	if principal := api.ContextPrincipal(ctx); principal != nil {
		return principal.Username()
	}
	return api.ContextAuthenticatedUsername(ctx)
}

// domainStillExists says whether a row's domain is still configured, for
// the pages that show mail of a domain since deleted.
func (self *graph) domainStillExists(ctx context.Context, domainId string) bool {
	exists, err := self.domainExists(ctx, domainId)
	if err != nil {
		log.Errorf("failed to read the domain %q: %s", domainId, err)
		return false
	}
	return exists
}

// translateError turns what the database says into what the API says.
func translateError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrNotFound):
		return api.ErrNotFound
	case errors.Is(err, db.ErrAlreadyExists):
		return api.ErrAlreadyExists
	case errors.Is(err, db.ErrInvalidArguments):
		return api.ErrInvalidArguments
	}
	var validation models.ValidationErrors
	if errors.As(err, &validation) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, validation.Error())
	}
	return err
}
