package db

import (
	"time"
)

// throttleModel is the last time something that happens at most once in a
// while happened, whichever instance did it.
type throttleModel struct {
	ThrottleKey string    `gorm:"column:throttle_key;primaryKey"`
	TakenAt     time.Time `gorm:"column:taken_at"`
}

func (throttleModel) TableName() string { return "throttle" }

// rateLimitBucketModel is a token bucket every instance counts together.
type rateLimitBucketModel struct {
	LimiterKey string    `gorm:"column:limiter_key;primaryKey"`
	TokenCount float64   `gorm:"column:token_count"`
	RefilledAt time.Time `gorm:"column:refilled_at"`
}

func (rateLimitBucketModel) TableName() string { return "rate_limit_bucket" }

// TakeThrottle takes the turn of something that happens at most once every
// interval, and says whether it was free: false when it was taken less than
// interval before now, by this instance or any other. It is part of the
// transaction, so a turn taken by work that rolls back is given back.
//
// A turn another transaction is taking at this moment counts as taken, and is
// not waited for: that transaction may be a long sweep holding the row until
// it commits, and every other instance would stand still behind it.
func (self *transaction) TakeThrottle(throttleKey string, interval time.Duration, now time.Time) (bool, error) {
	isLocked, err := self.TryAdvisoryLock(LockKey("throttle:" + throttleKey))
	if err != nil || !isLocked {
		return false, err
	}
	var takenKeys []string
	err = self.tx.Raw(`INSERT INTO "throttle" ("throttle_key", "taken_at") VALUES (?, ?)
		ON CONFLICT ("throttle_key") DO UPDATE SET "taken_at" = excluded."taken_at"
		WHERE "throttle"."taken_at" <= ?
		RETURNING "throttle_key"`, throttleKey, now, now.Add(-interval)).Scan(&takenKeys).Error
	return len(takenKeys) > 0, err
}

// DeleteThrottlesTakenBefore forgets the turns taken before a time, which no
// interval in use reaches back to.
func (self *transaction) DeleteThrottlesTakenBefore(before time.Time) (int64, error) {
	result := self.tx.Where(`"taken_at" < ?`, before).Delete(&throttleModel{})
	return result.RowsAffected, result.Error
}

// TakeRateLimitToken takes a token from the bucket of a limiter key, which
// refills at ratePerSecond and holds at most capacity, and says whether there
// was one. Every instance takes from the same bucket. A clock behind another
// instance's refills nothing rather than taking tokens away.
func (self *transaction) TakeRateLimitToken(limiterKey string, ratePerSecond float64, capacity int64, now time.Time) (bool, error) {
	if capacity < 1 {
		return false, nil
	}
	var tokenCounts []float64
	refilled := `LEAST(?::double precision, "rate_limit_bucket"."token_count" + GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - "rate_limit_bucket"."refilled_at"))) * ?::double precision)`
	err := self.tx.Raw(`INSERT INTO "rate_limit_bucket" ("limiter_key", "token_count", "refilled_at") VALUES (?, ?, ?)
		ON CONFLICT ("limiter_key") DO UPDATE SET "token_count" = `+refilled+` - 1, "refilled_at" = GREATEST("rate_limit_bucket"."refilled_at", ?::timestamptz)
		WHERE `+refilled+` >= 1
		RETURNING "token_count"`,
		limiterKey, float64(capacity-1), now,
		float64(capacity), now, ratePerSecond, now,
		float64(capacity), now, ratePerSecond).Scan(&tokenCounts).Error
	return len(tokenCounts) > 0, err
}

// SpendRateLimitTokens takes tokenCount tokens from the bucket of a limiter
// key whether or not it holds them, so that it goes into debt: a caller that
// did more work than one token pays for it with a longer wait. The debt is at
// most the bucket's capacity.
func (self *transaction) SpendRateLimitTokens(limiterKey string, ratePerSecond float64, capacity, tokenCount int64, now time.Time) error {
	if tokenCount <= 0 || capacity < 1 {
		return nil
	}
	refilled := `LEAST(?::double precision, "rate_limit_bucket"."token_count" + GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - "rate_limit_bucket"."refilled_at"))) * ?::double precision)`
	return self.tx.Exec(`INSERT INTO "rate_limit_bucket" ("limiter_key", "token_count", "refilled_at") VALUES (?, GREATEST(?::double precision, ?::double precision), ?)
		ON CONFLICT ("limiter_key") DO UPDATE SET "token_count" = GREATEST(?::double precision, `+refilled+` - ?::double precision),
			"refilled_at" = GREATEST("rate_limit_bucket"."refilled_at", ?::timestamptz)`,
		limiterKey, float64(-capacity), float64(capacity-tokenCount), now,
		float64(-capacity), float64(capacity), now, ratePerSecond, float64(tokenCount), now).Error
}

// CountRateLimitTokens is how many tokens the bucket of a limiter key holds
// now, without taking one: capacity for a key never taken from.
func (self *transaction) CountRateLimitTokens(limiterKey string, ratePerSecond float64, capacity int64, now time.Time) (float64, error) {
	var bucket rateLimitBucketModel
	result := self.tx.Where(`"limiter_key" = ?`, limiterKey).Limit(1).Find(&bucket)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return float64(capacity), nil
	}
	elapsed := now.Sub(bucket.RefilledAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return min(float64(capacity), bucket.TokenCount+elapsed*ratePerSecond), nil
}

// DeleteRateLimitBucketsRefilledBefore forgets the buckets last taken from
// before a time. Every limiter in use refills in less than a day, so a bucket
// untouched for longer is full, the same as one never taken from.
func (self *transaction) DeleteRateLimitBucketsRefilledBefore(before time.Time) (int64, error) {
	result := self.tx.Where(`"refilled_at" < ?`, before).Delete(&rateLimitBucketModel{})
	return result.RowsAffected, result.Error
}
