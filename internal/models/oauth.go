package models

import "time"

// OAuthClient is a program that introduced itself.
//
// It holds no secret. A program running on somebody's own machine cannot keep
// one, so registering is open and proves nothing: a client can do nothing at
// all until a person approves it, and what protects the exchange is the PKCE
// challenge rather than anything the client knows.
//
// Name is whatever the program called itself and is shown to the person
// approving. It is not to be trusted, and the page says so.
type OAuthClient struct {
	ID         string    `json:"id,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	Name string `json:"name,omitempty"`

	// RedirectURIs are the addresses an approval may be sent to. Checked when
	// the client registers and again when somebody approves it.
	RedirectURIs []string `json:"redirectUris,omitempty"`

	// ApprovedAt is when somebody last approved this client, so a
	// registration nobody used can be swept without touching one in service.
	ApprovedAt time.Time `json:"approvedAt,omitempty"`
}

// AllowsRedirect says whether an address is one this client registered.
//
// Compared whole rather than by prefix. A prefix match lets somebody who
// registered "https://example.com/cb" collect at
// "https://example.com/cb.evil.test", which is the shape of most of the
// redirect attacks this check exists to stop.
func (self *OAuthClient) AllowsRedirect(address string) bool {
	for _, allowed := range self.RedirectURIs {
		if allowed == address {
			return true
		}
	}
	return false
}

// OAuthAuthorization is the few minutes between a person approving and the
// program collecting.
//
// It is deleted the moment it is spent, in the same transaction that mints the
// token, so a replayed approval finds nothing and a crash between the two
// cannot leave an approval that was spent and issued nothing.
type OAuthAuthorization struct {
	ID        string    `json:"id,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`

	ClientID string `json:"clientId,omitempty"`
	UserID   string `json:"userId,omitempty"`

	// RedirectURI is where this approval is to be sent, which has to match
	// again when it is collected.
	RedirectURI string `json:"redirectUri,omitempty"`

	// CodeChallenge is the hash the program sent when it asked. It collects
	// by presenting the original.
	CodeChallenge string `json:"codeChallenge,omitempty"`

	// Resource is what the token will be good for.
	Resource string `json:"resource,omitempty"`

	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// OAuthDeviceAuthorization is a pairing in progress (RFC 8628): a device
// showing a short code, waiting for a person signed in somewhere else to
// approve it.
//
// It is approved or refused once, and deleted when its token is collected.
type OAuthDeviceAuthorization struct {
	ID        string    `json:"id,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`

	ClientID string `json:"clientId,omitempty"`

	// UserCode is what the person types, without its dash.
	UserCode string `json:"userCode,omitempty"`

	// DeviceName is what the device calls itself. Claimed, not verified.
	DeviceName string `json:"deviceName,omitempty"`

	// Resource is what the token will be good for: empty for the whole API.
	Resource string `json:"resource,omitempty"`

	// UserID is who approved it, empty until somebody does.
	UserID    string    `json:"userId,omitempty"`
	IsDenied  bool      `json:"isDenied,omitempty"`
	DecidedAt time.Time `json:"decidedAt,omitempty"`
	PolledAt  time.Time `json:"polledAt,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// IsDecided says whether a person has approved or refused it.
func (self *OAuthDeviceAuthorization) IsDecided() bool {
	return self.UserID != "" || self.IsDenied
}
