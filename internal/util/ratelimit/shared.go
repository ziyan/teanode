package ratelimit

import (
	"context"
	"strings"
	"sync"
	"time"
)

// TokenStore is what a shared bucket asks of a database transaction: db's
// Transaction has both methods. It is declared here so that this package does
// not import the database.
type TokenStore interface {
	TakeRateLimitToken(limiterKey string, ratePerSecond float64, capacity int64, now time.Time) (bool, error)
	SpendRateLimitTokens(limiterKey string, ratePerSecond float64, capacity, tokenCount int64, now time.Time) error
	CountRateLimitTokens(limiterKey string, ratePerSecond float64, capacity int64, now time.Time) (float64, error)
}

// Transact runs work in a transaction that commits when work returns nil.
type Transact func(ctx context.Context, work func(TokenStore) error) error

// Through adapts a database's TransactionContext to Transact:
//
//	ratelimit.Through(database.TransactionContext)
func Through[Transaction TokenStore](run func(context.Context, func(Transaction) error) error) Transact {
	return func(ctx context.Context, work func(TokenStore) error) error {
		return run(ctx, func(transaction Transaction) error { return work(transaction) })
	}
}

// sharedTimeout is how long one count of a shared bucket may take. A caller
// that is guessing a password waits at most this long for the database, and is
// refused when it does not answer.
const sharedTimeout = 5 * time.Second

// sharedKeyLongest is the longest key kept. The column holds 512 characters,
// and a key made of a name somebody typed can be any length.
const sharedKeyLongest = 400

// shared is the part of a Registry whose buckets live in the database. Every
// instance of the server running against that database takes from the same
// buckets, so a limit on guessing a password is the limit and not the limit
// times the number of instances.
type shared struct {
	// name is what the limiter is called, which starts every key so that two
	// limiters never take from one bucket.
	name     string
	rate     float64
	capacity int64
	transact Transact

	// availableByKey remembers, for availableFresh, that a key had tokens:
	// a phone syncing a thousand cards or photos with the right password
	// asks before each one, and need not ask the database each time. A
	// refusal is never remembered, and a take here forgets the key.
	availableMutex sync.Mutex
	availableByKey map[string]availableCount
}

// availableCount is a count of tokens and when it was read.
type availableCount struct {
	tokenCount int64
	readAt     time.Time
}

const (
	// availableFresh is how long a count of tokens left is answered from.
	availableFresh = time.Second

	// availableKeysMost bounds the counts remembered.
	availableKeysMost = 4096
)

func (self *shared) rememberAvailable(key string, tokenCount int64, now time.Time) {
	self.availableMutex.Lock()
	defer self.availableMutex.Unlock()
	if self.availableByKey == nil || len(self.availableByKey) >= availableKeysMost {
		self.availableByKey = map[string]availableCount{}
	}
	self.availableByKey[key] = availableCount{tokenCount: tokenCount, readAt: now}
}

func (self *shared) rememberedAvailable(key string, now time.Time) (int64, bool) {
	self.availableMutex.Lock()
	defer self.availableMutex.Unlock()
	remembered, isRemembered := self.availableByKey[key]
	if !isRemembered || now.Sub(remembered.readAt) >= availableFresh {
		return 0, false
	}
	return remembered.tokenCount, true
}

func (self *shared) forgetAvailable(key string) {
	self.availableMutex.Lock()
	defer self.availableMutex.Unlock()
	delete(self.availableByKey, key)
}

// NewSharedRegistry returns a registry whose buckets are counted in the
// database by every instance together, refilling at rate tokens per second
// and holding at most capacity. It is used where the Registry from NewRegistry
// is, with these differences:
//
//   - Nothing is held in memory, so there is no limit on keys and nothing to
//     sweep. The database forgets a bucket that has refilled.
//   - Every count is a round trip. Use it for the limits that guard a
//     password, and keep NewRegistry for abuse limits that do not.
//   - It fails closed. When the database cannot be reached a token cannot be
//     taken and the count of tokens left is zero, so the caller is refused.
//     A limit that opens when the database is down would let anybody who can
//     make it fail guess without limit.
//   - Give does nothing: a token taken cannot be put back.
//
// The name starts every key, so that limiters sharing the table stay apart.
func NewSharedRegistry(name string, rate float64, capacity int64, transact Transact) *Registry {
	return &Registry{
		rate:     rate,
		capacity: capacity,
		shared:   &shared{name: name, rate: rate, capacity: capacity, transact: transact},
	}
}

// sharedBucket is one key of a shared registry.
type sharedBucket struct {
	*shared
	key string
}

func (self *shared) bucket(key string) *Bucket {
	name := self.name + ":" + key
	if len(name) > sharedKeyLongest {
		// Every key a caller makes is short (an address, a username of at
		// most 64 characters, an account's id); a longer one is a name
		// somebody typed that is nobody's, and sharing a bucket with
		// another such name costs nobody anything.
		name = strings.ToValidUTF8(name[:sharedKeyLongest], "")
	}
	return &Bucket{remote: &sharedBucket{shared: self, key: name}}
}

// takeUpTo takes up to count tokens in one transaction and says how many it
// got. It takes none when the database cannot be reached.
func (self *sharedBucket) takeUpTo(count int64) int64 {
	if count <= 0 {
		return 0
	}
	defer self.forgetAvailable(self.key)
	count = min(count, self.capacity)
	ctx, cancel := context.WithTimeout(context.Background(), sharedTimeout)
	defer cancel()

	var taken int64
	if err := self.transact(ctx, func(store TokenStore) error {
		taken = 0
		now := time.Now()
		// Read first: a refused caller, a flood of them, takes no row lock.
		tokenCount, err := store.CountRateLimitTokens(self.key, self.rate, self.capacity, now)
		if err != nil || tokenCount < 1 {
			return err
		}
		for taken < count {
			isTaken, err := store.TakeRateLimitToken(self.key, self.rate, self.capacity, now)
			if err != nil {
				return err
			}
			if !isTaken {
				break
			}
			taken++
		}
		return nil
	}); err != nil {
		log.Errorf("cannot take from the shared limit %q, refusing: %s", self.key, err)
		return 0
	}
	return taken
}

// spend takes count tokens whether or not they are there, going into debt,
// and does nothing more when the database cannot be reached: the attempt it
// pays for was already let through.
func (self *sharedBucket) spend(count int64) {
	defer self.forgetAvailable(self.key)
	ctx, cancel := context.WithTimeout(context.Background(), sharedTimeout)
	defer cancel()
	if err := self.transact(ctx, func(store TokenStore) error {
		return store.SpendRateLimitTokens(self.key, self.rate, self.capacity, count, time.Now())
	}); err != nil {
		log.Errorf("cannot take from the shared limit %q: %s", self.key, err)
	}
}

// available is how many whole tokens are left, zero when the database cannot
// be reached.
func (self *sharedBucket) available() int64 {
	now := time.Now()
	if tokenCount, isRemembered := self.rememberedAvailable(self.key, now); isRemembered {
		return tokenCount
	}
	ctx, cancel := context.WithTimeout(context.Background(), sharedTimeout)
	defer cancel()

	var tokenCount float64
	if err := self.transact(ctx, func(store TokenStore) error {
		var err error
		tokenCount, err = store.CountRateLimitTokens(self.key, self.rate, self.capacity, time.Now())
		return err
	}); err != nil {
		log.Errorf("cannot count the shared limit %q, refusing: %s", self.key, err)
		return 0
	}
	available := max(0, int64(tokenCount))
	if available > 0 {
		self.rememberAvailable(self.key, available, now)
	}
	return available
}
