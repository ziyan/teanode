package models

import "time"

// Token authenticates a program the way a Session authenticates a browser.
//
// It belongs to an operator and acts as them, so removing the account removes
// its tokens. It used to live inside the account in the configuration, which
// meant recording when it was last used would have rewritten the whole
// configuration and had every instance reload it. When a token was last used
// is data, not a setting.
type Token struct {
	// ID of the Token, and the half of the token string that is not secret.
	// It appears in the log, so a token can be traced back to a row here and
	// revoked.
	ID string `json:"id,omitempty"`

	// Timestamp when the Token was created
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Token was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The User this Token belongs to. By identifier, so that renaming an
	// account does not revoke what it issued.
	UserID string `json:"userId,omitempty"`

	// The name that account signs in with, filled in by the queries that
	// resolve it. Not a column, for the same reason as on a Session.
	Username string `json:"username,omitempty"`

	// What holds it, for example "laptop". Not unique.
	Name string `json:"name,omitempty"`

	// When it stops working. Zero means it does not expire.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`

	// When it was last used, and from where
	UsedAt    time.Time `json:"usedAt,omitempty"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`

	// When it was revoked. Set rather than deleted, so the list can say so.
	RevokedAt time.Time `json:"revokedAt,omitempty"`
}

// Active reports whether this Token would authenticate a request now.
func (self *Token) Active(now time.Time) bool {
	if self == nil {
		return false
	}
	if !self.RevokedAt.IsZero() {
		return false
	}
	return self.ExpiresAt.IsZero() || self.ExpiresAt.After(now)
}
