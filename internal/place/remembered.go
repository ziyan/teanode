package place

import (
	"container/list"
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
	entries map[string]*list.Element

	// order holds the keys, the one remembered longest ago at the front. A
	// key remembered again moves to the back.
	order list.List
}

type rememberedAddress struct {
	key         string
	addressText string
	expiresAt   time.Time
}

// RememberedPosition is the position the way Get and Remember know it, rounded
// to 4 decimals. Ask the provider about this position, so that the address
// kept under a key is exactly the answer to what that key asks.
func RememberedPosition(latitudeDegrees, longitudeDegrees float64) (float64, float64) {
	return math.Round(latitudeDegrees*1e4) / 1e4, math.Round(longitudeDegrees*1e4) / 1e4
}

func rememberedKey(latitudeDegrees, longitudeDegrees float64) string {
	latitudeDegrees, longitudeDegrees = RememberedPosition(latitudeDegrees, longitudeDegrees)
	return fmt.Sprintf("%.4f,%.4f", latitudeDegrees, longitudeDegrees)
}

// Get is the address remembered for the position, and whether there is one. An
// empty address is remembered too: the provider knew nothing there.
func (self *RememberedAddresses) Get(latitudeDegrees, longitudeDegrees float64, now time.Time) (string, bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	element, isRemembered := self.entries[rememberedKey(latitudeDegrees, longitudeDegrees)]
	if !isRemembered {
		return "", false
	}
	entry := element.Value.(rememberedAddress)
	if !now.Before(entry.expiresAt) {
		return "", false
	}
	return entry.addressText, true
}

// Remember keeps the address for the position until a day after now.
func (self *RememberedAddresses) Remember(latitudeDegrees, longitudeDegrees float64, addressText string, now time.Time) {
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.entries == nil {
		self.entries = map[string]*list.Element{}
	}
	key := rememberedKey(latitudeDegrees, longitudeDegrees)
	entry := rememberedAddress{key: key, addressText: addressText, expiresAt: now.Add(rememberedLifetime)}
	if element, isRemembered := self.entries[key]; isRemembered {
		element.Value = entry
		self.order.MoveToBack(element)
		return
	}
	self.entries[key] = self.order.PushBack(entry)
	for self.order.Len() > rememberedMostCount {
		oldest := self.order.Front()
		delete(self.entries, oldest.Value.(rememberedAddress).key)
		self.order.Remove(oldest)
	}
}
