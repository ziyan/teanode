package models

import "time"

// Passkey is a credential an authenticator holds on somebody's behalf.
//
// The private half never leaves the authenticator — the phone, the laptop's
// secure enclave, the security key on a keyring — so there is no shared secret
// here to leak, phish or reuse. What is stored is the public half and enough
// to recognise it again.
type Passkey struct {
	// ID of the Passkey, stable for its lifetime
	ID string `json:"id,omitempty"`

	// Timestamp when the Passkey was registered
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the Passkey was last modified
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The User this Passkey signs in as
	UserID string `json:"userId,omitempty"`

	// What the person calls this authenticator, for example "phone"
	Name string `json:"name,omitempty"`

	// CredentialID is what the authenticator returns to identify itself. Not
	// the primary key: it is chosen by the authenticator and is bytes rather
	// than a name, and it has to be looked up by on sign-in, which is what the
	// unique index is for.
	CredentialID []byte `json:"-"`

	// PublicKey verifies what the authenticator signs.
	PublicKey []byte `json:"-"`

	// AttestationType is how the authenticator vouched for itself, if at all.
	AttestationType string `json:"attestationType,omitempty"`

	// Transports are how the browser can reach this authenticator: usb, nfc,
	// ble, internal, hybrid. Passed back at sign-in so the browser can offer
	// the right thing.
	Transports []string `json:"transports,omitempty"`

	// AAGUID identifies the make and model of the authenticator. Zero for
	// most passkeys, which deliberately do not say.
	AAGUID []byte `json:"-"`

	// SignCount is the authenticator's own counter. A count that does not
	// advance means two authenticators are answering for one credential,
	// which means it has been cloned.
	SignCount int64 `json:"-"`

	// BackupEligible says the credential may be synced to other devices;
	// BackupState says it currently is. Together they are the difference
	// between a passkey on one security key and one in a password manager.
	BackupEligible bool `json:"backupEligible,omitempty"`
	BackupState    bool `json:"backupState,omitempty"`

	// When it was last used to sign in, and from where
	UsedAt    time.Time `json:"usedAt,omitempty"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`
}
