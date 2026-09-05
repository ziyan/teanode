// Package ceremony parks the state of a challenge-response exchange between
// its two halves.
//
// A WebAuthn ceremony is two requests. The first mints a challenge and hands
// it to the browser; the second brings back what the authenticator signed. The
// server has to remember what it asked in between, and it has to forget it
// afterwards — a challenge that answers twice is a replay.
//
// That state is a small blob with three properties: it expires, it is read
// exactly once, and losing it costs nothing but asking again. In one process
// that is a map with a deadline. Across several it is a Redis key with a TTL,
// where GETDEL is "return it and delete it" as one step — the single-use rule
// enforced by the store rather than by every caller remembering to delete.
//
// Not a table. It was going to be one, with an expiry column and a sweep, and
// that is a row lock and an explicit delete to get what a map or GETDEL gives,
// plus an index to find what nothing would otherwise clean up. A challenge
// that is lost when the server restarts costs one retry.
package ceremony

import (
	"context"
	"errors"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("ceremony") //nolint:unused

// ErrNoCeremonyInProgress means finish was called without a begin, twice, or
// too late. One error for all three on purpose: a caller holding an identifier
// that does not resolve has nothing to do differently in each case, and saying
// which would tell somebody guessing whether they were close.
var ErrNoCeremonyInProgress = errors.New("ceremony: no ceremony is in progress")

// Lifetime bounds how long a challenge stays answerable. Long enough for
// somebody to find their security key and touch it, short enough that one left
// open in a tab is not still answerable tomorrow.
const Lifetime = 5 * time.Minute

// Ceremony is a challenge waiting to be answered.
type Ceremony struct {
	// Username is set when the ceremony is registering a credential for
	// somebody already signed in, and empty for a sign-in — where who is
	// signing in is exactly what the ceremony establishes.
	Username string `json:"username,omitempty"`

	// SessionData is the WebAuthn library's own state, kept opaque here. This
	// package is about the parking, not about what is parked.
	SessionData string `json:"sessionData,omitempty"`
}

// Store parks ceremonies.
type Store interface {
	// Park records a ceremony and returns the identifier that reclaims it.
	Park(ctx context.Context, ceremony *Ceremony) (string, error)

	// Take returns a parked ceremony and forgets it, so one challenge answers
	// exactly one attempt.
	Take(ctx context.Context, ceremonyId string) (*Ceremony, error)
}
