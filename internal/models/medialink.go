package models

import "time"

// MediaLink is one picture's address in one sent message.
//
// A template names a picture once; every message sent from it gets an address
// of its own for that picture. That is what makes an open detectable — a fetch
// of this address can only have come from this message — and it is why the
// token is random rather than an identifier that sorts.
//
// What an open is worth is worth saying wherever this is read. A fetch means a
// mail program asked for the picture, which is weaker than a person reading
// the message in both directions: Apple's Mail Privacy Protection fetches
// every picture before anybody has seen anything, and most programs fetch none
// until the reader asks. It is a floor with false positives in it.
type MediaLink struct {
	// Token is the address, and the primary key. Sixteen random bytes in
	// base32, not a ULID: these are reachable by anybody with no session, and
	// one that can be guessed from another lets a stranger fetch somebody
	// else's picture and mark their message opened.
	Token string `json:"token,omitempty"`

	// Timestamp when the address was created, which is when the message was
	// sent.
	CreatedAt time.Time `json:"createdAt,omitempty"`

	// Timestamp when the row was last written, which is the last fetch.
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`

	// The Media this address resolves to.
	MediaID string `json:"mediaId,omitempty"`

	// EnvelopeID is the message this address was put in, by the identifier it
	// is given as it is composed — the same one the stored mail records as
	// its envelope.
	//
	// One address per message, not per recipient. The rewrite happens once,
	// while the body is being built, and every recipient is handed the same
	// bytes. A message sent to three people that comes back opened says the
	// message was opened, not which of them opened it.
	EnvelopeID string `json:"envelopeId,omitempty"`

	// OpenedAt is the first fetch, LastOpenedAt the most recent, and
	// OpenCount how many there have been. Zero and empty until one happens.
	OpenedAt     *time.Time `json:"openedAt,omitempty"`
	LastOpenedAt *time.Time `json:"lastOpenedAt,omitempty"`
	OpenCount    int64      `json:"openCount,omitempty"`

	// IP and UserAgent are from the most recent fetch. For Gmail they are
	// Google's proxy rather than the reader's, which is worth remembering
	// before reading anything into them.
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
}
