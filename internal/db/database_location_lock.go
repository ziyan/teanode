package db

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// LocationLockOperation orders the writers of one person's location.
type LocationLockOperation interface {
	// LockLocation waits for, and then holds until the transaction ends, the
	// lock on one person's location records. A phone that sends a batch again
	// while the first send is still being stored, and a check-in at the same
	// moment as a report, would otherwise both find no visit for a key or no
	// place at a spot, and each make one.
	LockLocation(userId string) error
}

func (self *transaction) LockLocation(userId string) error {
	digest := sha256.Sum256([]byte("location\x00" + userId))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	return self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error
}
