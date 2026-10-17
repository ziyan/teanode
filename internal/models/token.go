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

	// ClientID is the program a person authorized to hold this token, when
	// one was. Empty on a token somebody minted for themselves, which is
	// every token that existed before programs could be authorized.
	ClientID string `json:"clientId,omitempty"`

	// Resource is what this token is good for. Empty means what it has
	// always meant: this server, the way a token minted by hand is. A token
	// naming a resource is refused anywhere else, so one issued for the
	// agent tools endpoint cannot be replayed against the rest of the API.
	Resource string `json:"resource,omitempty"`

	// ClientKind says the token is the TeaNode phone app's own sign-in
	// (ClientKindApp), made in a place where a person was tapping: a connect
	// code made on the dashboard or in the app, or the app's consent
	// approved there. Empty on every other token, including one a program
	// got through a connect code or an approval that a command line token
	// made, because that sign-in could have been made by a program running
	// on the person's computer. Only a token of this kind is taken as a
	// person's tap (see CredentialKind).
	ClientKind TokenClientKind `json:"clientKind,omitempty"`
}

// TokenClientKind is the marker on a token that its holder is the phone app.
type TokenClientKind string

// ClientKindApp is the phone app's own sign-in.
const ClientKindApp TokenClientKind = "app"

// CredentialKind is what a request signed in with, as far as telling a
// person's tap from a program's call goes.
type CredentialKind string

const (
	// CredentialSession is the dashboard's cookie session, which also had to
	// pass the cross-site check for a request that changes something.
	CredentialSession CredentialKind = "session"

	// CredentialApp is a token of the phone app's own sign-in.
	CredentialApp CredentialKind = "app"

	// CredentialToken is any other token: one minted for the command line
	// or an API client, or one a program was authorized for.
	CredentialToken CredentialKind = "token"
)

// IsInteractive says whether the credential is one a person's own tap on a
// screen arrives with: the dashboard in a browser, or the phone app. A command
// line or an agent running on the person's computer holds a token of the other
// kind, so it never arrives as a tap.
func (self CredentialKind) IsInteractive() bool {
	return self == CredentialSession || self == CredentialApp
}

// RefreshWindow is how long after its access expires a program's token can
// still be renewed.
//
// A program that is still in use renews when its access runs out, and many
// only renew once a request has been refused, so the renewal has to outlive
// the access. One that nobody has run for two months past that has been
// forgotten, and has to be approved again.
const RefreshWindow = 60 * 24 * time.Hour

// Refreshable says whether a token can still be renewed: not revoked, and
// not further past its expiry than RefreshWindow.
func (self *Token) Refreshable(now time.Time) bool {
	if self == nil || !self.RevokedAt.IsZero() {
		return false
	}
	return self.ExpiresAt.IsZero() || now.Before(self.ExpiresAt.Add(RefreshWindow))
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
