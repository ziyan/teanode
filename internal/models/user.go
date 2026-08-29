package models

import "time"

// User is somebody who may administer this server.
//
// Keyed by an identifier rather than by the name they sign in with. The name
// can change, and a key that changes is not a key: sessions, API tokens and
// passkeys all name an account, and every one of them would have had to be
// rewritten by a rename.
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

	// PasswordHash is a bcrypt hash, and never leaves the server.
	PasswordHash string `json:"-"`

	// Email receives notifications, such as a domain whose DNS records have
	// stopped resolving. Optional.
	Email string `json:"email,omitempty"`
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
