package security

import (
	"crypto/rand"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// The entropy an identifier is made from.
//
// From crypto/rand, not from a clock-seeded math/rand: this is the default
// identifier for sessions, tokens, credentials, mail, agent runs, attachments
// and the ceremonies a passkey sign-in parks its challenge in, and somebody
// who saw one from a given pool entry could work out the next.
//
// Nothing today rests on a ULID being unguessable -- a session and a token
// each carry a separate secret half from crypto/rand, and every row is scoped
// to its owner -- but the code itself shows the trap: the media link's token
// says in its own comment that it is sixteen random bytes and "not a ULID",
// because one that can be guessed from another lets a stranger fetch somebody
// else's picture. Somebody already had to reason their way around this. The
// reasoning is not needed if the identifiers are not guessable.
var ulidEntropyPool = &sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.Reader, 0)
	},
}

func NewULID() string {
	entropy := ulidEntropyPool.Get()
	defer ulidEntropyPool.Put(entropy)

	return strings.ToLower(ulid.MustNew(ulid.Now(), entropy.(io.Reader)).String())
}

func NewULIDFromTime(t time.Time) string {
	entropy := ulidEntropyPool.Get()
	defer ulidEntropyPool.Put(entropy)

	return strings.ToLower(ulid.MustNew(ulid.Timestamp(t), entropy.(io.Reader)).String())
}

func ValidateULID(id string) error {
	_, err := ulid.ParseStrict(strings.ToUpper(id))
	return err
}

// DerivedULID builds a ULID from a secret and a label, the same one every
// time.
//
// For an identifier that has to be stable, opaque and valid where a ULID is
// required — a DMARC report address published in DNS is the case this exists
// for. It must not change between two checks of the same domain, or the
// dashboard would ask for a new record every time it looked; it must not be
// guessable, because the signature beside it is what stops anybody addressing
// mail there; and it must parse, because the address is refused otherwise.
//
// A ULID is sixteen bytes, of which the first six are normally a timestamp.
// These are not: they are the first sixteen bytes of the signature, so the
// time it appears to carry is meaningless and nothing should read it.
func DerivedULID(secret []byte, label string) string {
	var id ulid.ULID
	copy(id[:], SignString(label, secret))
	return strings.ToLower(id.String())
}
