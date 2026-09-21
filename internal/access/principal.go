package access

import "github.com/ziyan/teanode/internal/models"

// Principal is who a request is made by, established once per request: the
// account, and what it may do, resolved from its groups. Nil for a request
// nobody is signed in to.
type Principal struct {
	// User is the account, or nil for the console, the command line run on
	// the server itself with the local token, which is not an account.
	User *models.User

	// Permissions is what the caller may do, resolved once from the
	// database for this request and never cached across requests.
	Permissions *models.EffectivePermissions

	// Console says the caller is the host itself, which may do everything.
	Console bool
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
