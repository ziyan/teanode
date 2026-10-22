package ratelimit

import (
	"net"
	"sync"
)

// AddressHolds counts what each address is holding at once, such as open
// connections, and refuses an address that already holds its share.
//
// A total ceiling on a listener is taken whole by whoever connects first and
// keeps reconnecting, and everybody else is turned away. This is the other
// half: no one network can hold more than limit of it. The key is the one
// AddressKey gives, so an IPv6 /64 counts as one holder.
type AddressHolds struct {
	limit int

	mutex sync.Mutex
	held  map[string]int
}

// NewAddressHolds returns a count that lets each address hold at most limit.
func NewAddressHolds(limit int) *AddressHolds {
	return &AddressHolds{limit: limit, held: make(map[string]int)}
}

// Acquire takes one hold for the address behind remote. When it is refused,
// the address already holds its limit and nothing was taken. Otherwise the
// returned function gives the hold back, and is to be called once.
func (self *AddressHolds) Acquire(remote net.Addr) (release func(), ok bool) {
	address := remote.String()
	if tcp, isTCP := remote.(*net.TCPAddr); isTCP {
		address = tcp.IP.String()
	}
	key := AddressKey(address)

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.held[key] >= self.limit {
		return nil, false
	}
	self.held[key]++
	return func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()

		// Dropped at zero so the map holds only addresses that are holding
		// something, however many have come and gone.
		if self.held[key]--; self.held[key] <= 0 {
			delete(self.held, key)
		}
	}, true
}
