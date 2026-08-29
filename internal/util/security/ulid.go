package security

import (
	"io"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var ulidEntropyPool = &sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
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
