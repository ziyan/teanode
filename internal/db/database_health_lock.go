package db

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// HealthLockOperation orders the writers of one person's Health data.
type HealthLockOperation interface {
	// LockHealth waits for, and then holds until the transaction ends, the
	// lock on one person's Health records. A phone that sends a batch again
	// while the first send is still being stored, and an erase at the same
	// moment as a report, would otherwise interleave: the erase would leave
	// the rows the report was about to write.
	LockHealth(userId string) error
}

func (self *transaction) LockHealth(userId string) error {
	digest := sha256.Sum256([]byte("health\x00" + userId))
	lockKey := int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
	return self.tx.Exec("SELECT pg_advisory_xact_lock(?)", lockKey).Error
}
