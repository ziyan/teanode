package models

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is somebody with an account on this server: a person who signs in to
// the web UI, reads a mailbox, and holds whatever permissions the groups they
// are in give them.
//
// Keyed by an identifier rather than by the name they sign in with. The name
// can change, and a key that changes is not a key: sessions, API tokens,
// passkeys, mailboxes and group memberships all name an account, and every one
// of them would have had to be rewritten by a rename.
type User struct {
	// ID of the User, stable for its lifetime
	ID string `json:"id,omitempty"`

	// Timestamp when the User was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the User was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// Username they sign in with. Unique without regard to case.
	Username string `json:"username,omitempty"`

	// Name is what to call this person, when they have said. The username is
	// what they sign in with, which is not always something to greet somebody
	// by. Optional.
	Name string `json:"name,omitempty"`

	// PasswordHash is a bcrypt hash, and never leaves the server. Empty for
	// a user who signs in only with a passkey or through an identity
	// provider.
	PasswordHash string `json:"-"`

	// Email receives notifications, such as a domain whose DNS records have
	// stopped resolving. Optional, and not a mailbox address: a mailbox may
	// have several.
	Email string `json:"email,omitempty"`

	// DisabledAt, when set, means this person cannot sign in. Their mail and
	// their memberships are kept, so enabling them again restores everything.
	DisabledAt *time.Time `json:"disabledAt,omitempty"`

	// Locale is the language the web UI greets them in, when they chose one.
	Locale string `json:"locale,omitempty"`

	// GroupIDs are the groups this person is in. Loaded with the user.
	GroupIDs []string `json:"groupIds,omitempty"`
}

// DisplayName is what to call this person: the name they gave, or the name
// they sign in with when they have not given one.
func (self *User) DisplayName() string {
	if self == nil {
		return ""
	}
	if self.Name != "" {
		return self.Name
	}
	return self.Username
}

// Disabled reports whether this person may not sign in.
func (self *User) Disabled() bool {
	return self != nil && self.DisabledAt != nil
}

// Validate reports everything wrong with the user.
func (self *User) Validate() error {
	var errors ValidationErrors
	if self.Username == "" {
		errors.add("username", "required")
	}
	if self.PasswordHash != "" {
		if _, err := bcrypt.Cost([]byte(self.PasswordHash)); err != nil {
			errors.add("passwordHash", "is not a bcrypt hash (%s)", err)
		}
	}
	if self.Email != "" && !IsEmailAddress(self.Email) {
		errors.add("email", "%q is not an email address", self.Email)
	}
	return errors.ErrOrNil()
}

// RedactForAudit is the user as an audit row records it: without the hash.
func (self *User) RedactForAudit() any {
	if self == nil {
		return nil
	}
	redacted := *self
	redacted.PasswordHash = ""
	return &redacted
}

// UserIdentity is an account's identity at an identity provider: the
// provider's name in this server's settings, and the subject the provider
// calls the person. Looked up on every single sign-on; created on the first.
type UserIdentity struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	UserID     string    `json:"userId"`
	Provider   string    `json:"provider"`
	Subject    string    `json:"subject"`
	Email      string    `json:"email,omitempty"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}
