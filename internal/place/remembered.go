package place

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	// rememberedLifetime is how long an address is kept.
	rememberedLifetime = 24 * time.Hour

	// rememberedMostCount is how many addresses are kept; the oldest is
	// dropped for a new one past it.
	rememberedMostCount = 4096
)

// RememberedAddresses holds the street addresses a provider gave, by position
// rounded to 4 decimals (about ten meters), for a day, so that opening the
// same photo again does not ask the provider again. It is in memory only and
// safe for concurrent use; the zero value is ready to use.
type RememberedAddresses struct {
	lock    sync.Mutex
	entries map[string]rememberedAddress

	// order lists the keys oldest first.
	order []string
}

type rememberedAddress struct {
	addressText string
	expiresAt   time.Time
}

func rememberedKey(latitudeDegrees, longitudeDegrees float64) string {
	return fmt.Sprintf("%.4f,%.4f", math.Round(latitudeDegrees*1e4)/1e4, math.Round(longitudeDegrees*1e4)/1e4)
}

// Get is the address remembered for the position, and whether there is one. An
// empty address is remembered too: the provider knew nothing there.
func (self *RememberedAddresses) Get(latitudeDegrees, longitudeDegrees float64, now time.Time) (string, bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	entry, isRemembered := self.entries[rememberedKey(latitudeDegrees, longitudeDegrees)]
	if !isRemembered || !now.Before(entry.expiresAt) {
		return "", false
	}
	return entry.addressText, true
}

// Remember keeps the address for the position until a day after now.
func (self *RememberedAddresses) Remember(latitudeDegrees, longitudeDegrees float64, addressText string, now time.Time) {
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.entries == nil {
		self.entries = map[string]rememberedAddress{}
	}
	key := rememberedKey(latitudeDegrees, longitudeDegrees)
	if _, isRemembered := self.entries[key]; !isRemembered {
		self.order = append(self.order, key)
	}
	self.entries[key] = rememberedAddress{addressText: addressText, expiresAt: now.Add(rememberedLifetime)}
	for len(self.order) > rememberedMostCount {
		delete(self.entries, self.order[0])
		self.order = self.order[1:]
	}
}
