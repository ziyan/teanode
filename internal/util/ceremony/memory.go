package ceremony

import (
	"context"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/util/security"
)

// NewMemoryStore parks ceremonies in this process.
//
// The default, because one server is the ordinary case and a challenge that
// does not survive a restart costs one retry. Behind a load balancer with more
// than one instance it is the wrong choice — the browser can come back to a
// different instance than it started with — which is what the Redis store is
// for.
func NewMemoryStore() Store {
	return &memoryStore{parked: make(map[string]parkedCeremony)}
}

type parkedCeremony struct {
	ceremony Ceremony
	expires  time.Time
}

type memoryStore struct {
	mutex  sync.Mutex
	parked map[string]parkedCeremony
}

func (self *memoryStore) Park(ctx context.Context, ceremony *Ceremony) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Swept here rather than by a ticker: the map is small, this runs only
	// when somebody starts a ceremony, and a goroutine to delete a handful of
	// keys is more machinery than the thing deserves.
	now := time.Now()
	for id, found := range self.parked {
		if found.expires.Before(now) {
			delete(self.parked, id)
		}
	}

	ceremonyId := security.NewULID()
	self.parked[ceremonyId] = parkedCeremony{ceremony: *ceremony, expires: now.Add(Lifetime)}
	return ceremonyId, nil
}

func (self *memoryStore) Take(ctx context.Context, ceremonyId string) (*Ceremony, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	found, ok := self.parked[ceremonyId]
	// Deleted whether or not it was still valid: an expired challenge is
	// answered once with a refusal and then gone.
	delete(self.parked, ceremonyId)
	if !ok || found.expires.Before(time.Now()) {
		return nil, ErrNoCeremonyInProgress
	}
	taken := found.ceremony
	return &taken, nil
}
