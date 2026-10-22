package ratelimit

import (
	"sync"
	"time"
)

// Registry holds one bucket per key, created when the key is first seen.
//
// The keys here are remote addresses, which is the difference between this and
// a registry keyed by account: an account list is bounded by how many accounts
// exist, and an address list is bounded by nothing. A caller with a /64 of
// IPv6 has more addresses than the process has memory, so a registry that only
// ever adds entries is itself the denial of service it was added to prevent.
//
// Eviction is what makes it safe, and the rule is chosen so that it cannot be
// gamed: a bucket is only dropped once it has refilled completely, at which
// point it is indistinguishable from one that has never been used. Dropping a
// full bucket forgives nothing. Dropping a depleted one would forgive
// everything, and an attacker who could provoke it would have found a way to
// clear their own limit.
type Registry struct {
	rate     float64
	capacity int64

	// idle is how long a full bucket is kept before it is dropped. Keeping it
	// a while costs one small allocation and saves rebuilding the bucket for a
	// caller who is simply active.
	idle time.Duration

	// limit caps how many keys are held at once, so that memory is bounded
	// even while an attack is in progress and no bucket has refilled yet.
	limit int

	// overflow is the one bucket every caller shares once the table is full,
	// made once so that its tokens are spent across calls.
	overflow *Bucket

	mutex   sync.Mutex
	buckets map[string]*entry

	// sweptAt is when the full buckets were last swept out, so that the sweep
	// runs on a timer rather than on every call.
	sweptAt time.Time
}

type entry struct {
	bucket *Bucket
	seenAt time.Time
}

// NewRegistry returns a registry whose buckets refill at rate tokens per
// second and hold at most capacity, keeping at most limit keys and forgetting
// a full bucket after idle.
func NewRegistry(rate float64, capacity int64, limit int, idle time.Duration) *Registry {
	return &Registry{
		rate:     rate,
		capacity: capacity,
		idle:     idle,
		limit:    limit,
		overflow: NewBucketWithRate(rate, capacity),
		buckets:  make(map[string]*entry),
		sweptAt:  time.Now(),
	}
}

// Allow takes a token for key and reports whether there was one to take.
func (self *Registry) Allow(key string) bool {
	return self.For(key).Allow()
}

// For returns the bucket for key, creating it if this is the first time the
// key has been seen.
func (self *Registry) For(key string) *Bucket {
	now := time.Now()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.sweep(now)

	if existing, ok := self.buckets[key]; ok {
		existing.seenAt = now
		return existing.bucket
	}

	// At the limit, and nothing swept: every bucket held is one somebody has
	// spent tokens from. Callers past the table share one bucket, so together
	// they are allowed a burst and no more. A bucket made fresh for each call
	// would allow every attempt, and filling the table is all it would take to
	// get there. Sharing means an attack that fills the table also limits the
	// strangers who arrive during it, which is the cost of not letting the map
	// grow.
	if self.limit > 0 && len(self.buckets) >= self.limit {
		return self.overflow
	}

	created := &entry{bucket: NewBucketWithRate(self.rate, self.capacity), seenAt: now}
	self.buckets[key] = created
	return created.bucket
}

// Len returns how many keys are held. For tests and for reporting.
func (self *Registry) Len() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return len(self.buckets)
}

// sweep drops the buckets that have refilled and gone quiet. It is called with
// the mutex held.
func (self *Registry) sweep(now time.Time) {
	if now.Sub(self.sweptAt) < self.idle {
		return
	}
	self.sweptAt = now

	for key, held := range self.buckets {
		if now.Sub(held.seenAt) < self.idle {
			continue
		}
		if !held.bucket.full(now) {
			continue
		}
		delete(self.buckets, key)
	}
}
