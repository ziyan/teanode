package autoacme

import (
	"context"
	"sync"
	"time"
)

// challengeLifetime is how long an answer to a challenge is kept if nothing
// removes it. An order is given at most a few minutes, so this is generous;
// it only matters when an instance stops in the middle of an order and never
// cleans up.
const challengeLifetime = 15 * time.Minute

// ChallengeStore keeps the answers to the challenges a certificate authority
// is about to check, where every instance can read them.
//
// The authority's request is made to the name being authorized, so behind a
// load balancer it reaches whichever instance the balancer picks, not the one
// that ordered. An answer held in the memory of the instance that ordered
// would be a 404 (or the wrong certificate) on every other instance, and the
// order would fail. Keeping the answers in the database the instances share
// lets any of them give it.
type ChallengeStore interface {
	// Put stores the key authorization of a challenge, replacing one under
	// the same key. The key is the token for "http-01" and the lower case
	// domain name for "tls-alpn-01".
	Put(ctx context.Context, challengeKind, challengeKey, keyAuthorization string, expiresAt time.Time) error

	// Delete removes the answer under a key. Not an error when there is none.
	Delete(ctx context.Context, challengeKey string) error

	// Find returns the key authorization stored under a key for a kind of
	// challenge, and false when there is none or it has expired.
	Find(ctx context.Context, challengeKind, challengeKey string) (string, bool, error)
}

// memoryChallengeStore keeps the answers in this process only. It is what a
// manager uses when the caller gives no ChallengeStore, which is right for a
// single instance and wrong for several.
type memoryChallengeStore struct {
	mutex   sync.Mutex
	entries map[string]memoryChallenge
}

type memoryChallenge struct {
	challengeKind    string
	keyAuthorization string
	expiresAt        time.Time
}

func newMemoryChallengeStore() *memoryChallengeStore {
	return &memoryChallengeStore{entries: make(map[string]memoryChallenge)}
}

func (self *memoryChallengeStore) Put(ctx context.Context, challengeKind, challengeKey, keyAuthorization string, expiresAt time.Time) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.entries[challengeKey] = memoryChallenge{
		challengeKind:    challengeKind,
		keyAuthorization: keyAuthorization,
		expiresAt:        expiresAt,
	}
	return nil
}

func (self *memoryChallengeStore) Delete(ctx context.Context, challengeKey string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	delete(self.entries, challengeKey)
	return nil
}

func (self *memoryChallengeStore) Find(ctx context.Context, challengeKind, challengeKey string) (string, bool, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	entry, ok := self.entries[challengeKey]
	if !ok || entry.challengeKind != challengeKind || !time.Now().Before(entry.expiresAt) {
		return "", false, nil
	}
	return entry.keyAuthorization, true, nil
}
