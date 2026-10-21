package passwords

import "encoding/json"

// ItemKind is what an item is.
type ItemKind string

// The kinds of item.
const (
	ItemKindLogin ItemKind = "login"
	ItemKindCard  ItemKind = "card"
	ItemKindNote  ItemKind = "note"
)

// IsValid reports whether this is one of the kinds.
func (self ItemKind) IsValid() bool {
	switch self {
	case ItemKindLogin, ItemKindCard, ItemKindNote:
		return true
	}
	return false
}

// LoginFields is what a login holds. It is the JSON that is sealed with the
// item key, so the names are the same on every surface. Every key is always
// written, with an empty list or text when there is nothing, so a reader in
// another language never meets a missing key.
type LoginFields struct {
	Name     string   `json:"name"`
	UserName string   `json:"userName"`
	Password string   `json:"password"`
	Sites    []string `json:"sites"`
	// OneTimeCodeURI is an otpauth://totp/... address, or empty.
	OneTimeCodeURI string         `json:"oneTimeCodeUri"`
	Notes          string         `json:"notes"`
	Passkeys       []LoginPasskey `json:"passkeys"`
	Folder         string         `json:"folder"`
}

// LoginPasskey is a passkey kept inside a login. The byte values are standard
// base64 with padding in the JSON.
type LoginPasskey struct {
	CredentialID    []byte `json:"credentialId"`
	RelyingPartyID  string `json:"relyingPartyId"`
	UserHandle      []byte `json:"userHandle"`
	UserName        string `json:"userName"`
	UserDisplayName string `json:"userDisplayName"`
	// PrivateKey is an ES256 key as PKCS#8 DER.
	PrivateKey []byte `json:"privateKey"`
	// CreatedAt is an RFC 3339 time in UTC, such as 2026-01-31T09:30:00Z.
	CreatedAt string `json:"createdAt"`
}

// MarshalJSON writes an empty list, not null, for a login with no sites or
// passkeys.
func (self LoginFields) MarshalJSON() ([]byte, error) {
	type plainFields LoginFields
	fields := plainFields(self)
	if fields.Sites == nil {
		fields.Sites = []string{}
	}
	if fields.Passkeys == nil {
		fields.Passkeys = []LoginPasskey{}
	}
	return json.Marshal(fields)
}

// CardFields is what a card holds. The expiry month and year and the number
// are text, as printed on the card.
type CardFields struct {
	Name           string `json:"name"`
	CardholderName string `json:"cardholderName"`
	Number         string `json:"number"`
	ExpiryMonth    string `json:"expiryMonth"`
	ExpiryYear     string `json:"expiryYear"`
	SecurityCode   string `json:"securityCode"`
	Brand          string `json:"brand"`
	Notes          string `json:"notes"`
	Folder         string `json:"folder"`
}

// NoteFields is what a note holds.
type NoteFields struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Folder string `json:"folder"`
}
