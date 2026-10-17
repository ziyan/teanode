package access

import "github.com/ziyan/teanode/internal/models"

// Principal is who a request is made by, established once per request: the
// account, and what it may do, resolved from its groups. Nil for a request
// nobody is signed in to.
type Principal struct {
	// IsServiceEnabled checks live deployment policy; nil preserves standalone callers.
	IsServiceEnabled func(models.Service) bool

	// User is the account, or nil for the console, the command line run on
	// the server itself with the local token, which is not an account.
	User *models.User

	// Permissions is what the caller may do, resolved once from the
	// database for this request and never cached across requests.
	Permissions *models.EffectivePermissions

	// Console says the caller is the host itself, which may do everything.
	Console bool

	// Impersonator is the operator signed in as User, when the request is
	// an impersonation: User is whose account it is, Impersonator who is
	// really there.
	Impersonator *models.User

	// CredentialKind is what the request signed in with. It is how an
	// operation that must be a person's own tap tells one from a program
	// that holds the person's command line token. Empty for a request that
	// did not come in over HTTP with a credential, such as the agent's own
	// operations, which are therefore never taken as a tap.
	CredentialKind models.CredentialKind
}

// IsPersonsTap says whether the request is the person's own tap on a screen:
// signed in by the dashboard's session or by the phone app's token, and not
// an operator signed in as the person.
func (self *Principal) IsPersonsTap() bool {
	return self != nil && self.User != nil && self.Impersonator == nil && self.CredentialKind.IsInteractive()
}

// IsImpersonation says whether an operator is signed in as the account
// rather than its owner.
func (self *Principal) IsImpersonation() bool {
	return self != nil && self.Impersonator != nil
}

// UserID is the account's identifier, or empty for the console.
func (self *Principal) UserID() string {
	if self == nil || self.User == nil {
		return ""
	}
	return self.User.ID
}

// Username is what to call the caller in a log line.
func (self *Principal) Username() string {
	if self == nil {
		return ""
	}
	if self.User != nil {
		return self.User.Username
	}
	if self.Console {
		return "console"
	}
	return ""
}

// RequireService checks service policy independently of permissions.
func (self *Principal) RequireService(service models.Service) error {
	if self != nil && self.IsServiceEnabled != nil && !self.IsServiceEnabled(service) {
		return &models.ServiceUnavailableError{Service: service}
	}
	return nil
}
