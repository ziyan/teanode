package push

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/secretbox"
)

// What a phone gives its server instead of its device token.
//
// The phone asks the relay to register its token and gets back a sealed
// registration: the token, which Apple environment it is for and when it was
// made, encrypted and authenticated under a key only the relay has. The relay
// keeps nothing. A server holds the sealed registration and presents it with
// every notification; the relay opens it to learn where to send. So only the
// server a phone handed its registration to can reach that phone, and a
// registration the relay did not make is refused.

// Platforms a registration is for: the phone's operating system, which says
// whether the relay reaches it through APNs or through Firebase Cloud
// Messaging.
const (
	PlatformIOS     = "ios"
	PlatformAndroid = "android"
)

// Registration is what a sealed registration holds.
type Registration struct {
	DeviceToken string `json:"deviceToken"`
	IsSandbox   bool   `json:"isSandbox"`
	PushType    string `json:"pushType"`

	// Platform is PlatformIOS or PlatformAndroid. A registration sealed
	// before Android was served has none, and is an iPhone's.
	Platform  string    `json:"platform,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ResolvedPlatform is Platform, or PlatformIOS when it is empty.
func (self *Registration) ResolvedPlatform() string {
	if self.Platform == "" {
		return PlatformIOS
	}
	return self.Platform
}

// registrationLifetime is how long a registration is good for. The app
// registers again each time it starts, so a phone in use never reaches it;
// a registration copied out of a server's database or a backup stops working
// within it.
const registrationLifetime = 180 * 24 * time.Hour

// registrationLabel binds the sealing key to this one use of the APNs key.
const registrationLabel = "teanode push relay: registrations"

// Sealer seals and opens registrations.
type Sealer struct {
	keyID string
	box   *secretbox.Box
}

// NewSealer builds a sealer from the relay's APNs key, the one secret only
// the relay has, rather than from the server secret, which an operator may
// rotate for reasons of their own. The key's identifier is written in front
// of every registration: a relay given another key answers the old
// registrations as gone, so servers clear them and the phones register
// again.
func NewSealer(keyID string, key *ecdsa.PrivateKey) (*Sealer, error) {
	secret, err := key.Bytes()
	if err != nil {
		return nil, err
	}
	return NewSealerFromSecret(keyID, secret)
}

// NewSealerFromSecret builds a sealer from a secret only the relay has and
// the identifier written in front of every registration. A relay with an APNs
// key seals with it (NewSealer), so that adding Android to it leaves the
// iPhones' registrations valid; a relay for Android alone seals with its
// Firebase service account's key.
func NewSealerFromSecret(keyID string, secret []byte) (*Sealer, error) {
	if keyID == "" || strings.Contains(keyID, ".") {
		return nil, fmt.Errorf("push: %q is not a key identifier", keyID)
	}
	box, err := secretbox.New(secret, registrationLabel)
	if err != nil {
		return nil, err
	}
	return &Sealer{keyID: keyID, box: box}, nil
}

// Seal writes a registration as the opaque text a phone hands its server.
func (self *Sealer) Seal(registration *Registration) (string, error) {
	plain, err := json.Marshal(registration)
	if err != nil {
		return "", err
	}
	sealed, err := self.box.Seal(plain)
	if err != nil {
		return "", err
	}
	return self.keyID + "." + sealed, nil
}

// Open reads a sealed registration. One sealed under another key, or past
// its lifetime, is ErrGone: the server should forget it and wait for the
// phone to register again. One that does not open is ErrRefused.
func (self *Sealer) Open(sealed string, now time.Time) (*Registration, error) {
	keyID, rest, isFound := strings.Cut(sealed, ".")
	if !isFound {
		return nil, fmt.Errorf("%w: that is not a registration", ErrRefused)
	}
	if keyID != self.keyID {
		return nil, fmt.Errorf("%w: the registration was made with a key the relay no longer uses", ErrGone)
	}
	plain, err := self.box.Open(rest)
	if err != nil {
		return nil, fmt.Errorf("%w: that registration was not made by this relay", ErrRefused)
	}
	var registration Registration
	if err := json.Unmarshal(plain, &registration); err != nil || registration.DeviceToken == "" {
		return nil, fmt.Errorf("%w: that registration cannot be read", ErrRefused)
	}
	if now.Sub(registration.CreatedAt) > registrationLifetime {
		return nil, fmt.Errorf("%w: the registration has expired", ErrGone)
	}
	return &registration, nil
}
