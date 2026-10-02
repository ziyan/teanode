package models

import "time"

// Session is one signed-in browser.
//
// It is a row rather than a signed cookie so that it can be listed and ended
// on its own. The cookie carries an identifier and a secret; this is
// everything the server knows about it.
type Session struct {
	// ID of the Session, and the half of the cookie that is not secret
	ID string `json:"id,omitempty"`

	// Timestamp when the Session was created, which is when somebody logged in
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Session was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The User this Session belongs to. By identifier, so that renaming an
	// account does not sign it out of itself.
	UserID string `json:"userId,omitempty"`

	// The name that account signs in with, filled in by the queries that
	// resolve it. Not a column: the username can change and this must not be
	// a second copy of it that goes stale.
	Username string `json:"username,omitempty"`

	// When it stops working, whatever else happens
	ExpiresAt time.Time `json:"expiresAt,omitempty"`

	// When it was last used. Not written on every request: see the note on
	// db.TouchInterval.
	UsedAt time.Time `json:"usedAt,omitempty"`

	// When it was ended, by logging out or by being revoked from the list.
	// Set rather than deleted, so the list can say so before the row is swept.
	RevokedAt time.Time `json:"revokedAt,omitempty"`

	// Where it was last used from, so a person can recognize their own
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`

	// ImpersonatorUserID is the operator who signed in as this account,
	// for a session that is an impersonation, and ImpersonatorSessionID
	// the operator's own session, which it lasts no longer than.
	// ImpersonatorUsername is looked up, like Username.
	ImpersonatorUserID    string `json:"impersonatorUserId,omitempty"`
	ImpersonatorSessionID string `json:"impersonatorSessionId,omitempty"`
	ImpersonatorUsername  string `json:"impersonatorUsername,omitempty"`
}

// IsImpersonation says whether an operator is behind this session rather
// than the account it belongs to.
func (self *Session) IsImpersonation() bool {
	return self != nil && self.ImpersonatorUserID != ""
}

// Active reports whether this Session would authenticate a request now.
func (self *Session) Active(now time.Time) bool {
	if self == nil {
		return false
	}
	if !self.RevokedAt.IsZero() {
		return false
	}
	return self.ExpiresAt.IsZero() || self.ExpiresAt.After(now)
}
