package photo

import "time"

// SetNow replaces the clock, which is how a test lets time pass.
func (self *Store) SetNow(now func() time.Time) { self.now = now }
