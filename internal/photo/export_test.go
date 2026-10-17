package photo

import (
	"time"

	"github.com/ziyan/teanode/internal/faces"
)

// SetNow replaces the clock, which is how a test lets time pass.
func (self *Store) SetNow(now func() time.Time) { self.now = now }

// SetFaceReader replaces the program that finds faces with a fake, or, given
// an error, makes faces unavailable for the reason it gives.
func (self *Store) SetFaceReader(reader faces.Reader, err error) {
	self.faceFinder.readerOnce.Do(func() {})
	self.faceFinder.reader, self.faceFinder.readerErr = reader, err
}
