package db

import (
	"time"
)

// McpHeldResultOperation is the long tool results read on with result_more (mcp_held_result).
type McpHeldResultOperation interface {
	// KeepMcpHeldResult holds a result for its holder and enforces the limits
	// in the same transaction: what was held longer than limits.HeldFor goes,
	// then the holder's oldest results until the new one fits in the holder's
	// count and bytes, then the oldest of everybody's until it fits in the
	// total bytes. The result is written last, so it is never the one dropped.
	KeepMcpHeldResult(held *McpHeldResult, limits McpHeldResultLimits, now time.Time) error

	// FindMcpHeldResult is the result held under id for holder, nil when there
	// is none, it was held before notBefore, or it is another holder's: the
	// same as an id that never existed.
	FindMcpHeldResult(holder, id string, notBefore time.Time) (*McpHeldResult, error)

	// DeleteMcpHeldResultsStoredBefore forgets the results held since before
	// a time, which nobody can read on any more.
	DeleteMcpHeldResultsStoredBefore(before time.Time) (int64, error)
}

// McpHeldResult is one tool's whole text, whose it is, and what kind of
// answer it was.
type McpHeldResult struct {
	ID     string `gorm:"column:id;primaryKey"`
	Holder string `gorm:"column:holder"`

	// ResultText is the whole text, whose offsets result_more is given.
	ResultText string `gorm:"column:result_text"`

	// IsUntrusted is text from outside, which every page wraps, and IsError a
	// tool's failure, which every page is marked as.
	IsUntrusted bool      `gorm:"column:is_untrusted"`
	IsError     bool      `gorm:"column:is_error"`
	StoredAt    time.Time `gorm:"column:stored_at"`
}

func (McpHeldResult) TableName() string { return "mcp_held_result" }

// McpHeldResultLimits are the bounds of what is held, enforced by
// KeepMcpHeldResult.
type McpHeldResultLimits struct {
	// HeldFor is how long a result is held after it was stored.
	HeldFor time.Duration

	// ResultCountPerHolder and ByteCountPerHolder bound what one holder has
	// held at once; ByteCount bounds everything held, for every holder.
	ResultCountPerHolder int
	ByteCountPerHolder   int
	ByteCount            int
}

func (self *transaction) KeepMcpHeldResult(held *McpHeldResult, limits McpHeldResultLimits, now time.Time) error {
	// Keeps are one at a time: the bounds are counted from what is stored, so
	// two keeps counting at once would each leave room for the other's result.
	if err := self.tx.Exec(`SELECT pg_advisory_xact_lock(?)`, LockKey("mcp-held-result")).Error; err != nil {
		return err
	}
	if err := self.tx.Where(`"stored_at" < ?`, now.Add(-limits.HeldFor)).Delete(&McpHeldResult{}).Error; err != nil {
		return err
	}
	newByteCount := len(held.ResultText)
	// Newest first, the results that fit are the first ones; the rest, the
	// oldest, go.
	if err := self.tx.Exec(`DELETE FROM "mcp_held_result" WHERE "id" IN (
		SELECT "id" FROM (
			SELECT "id", ROW_NUMBER() OVER newest AS "position", SUM(octet_length("result_text")) OVER newest AS "cumulative_bytes"
			FROM "mcp_held_result" WHERE "holder" = ?
			WINDOW newest AS (ORDER BY "stored_at" DESC, "id" DESC)
		) AS "ranked" WHERE "position" > ? OR "cumulative_bytes" > ?)`,
		held.Holder, limits.ResultCountPerHolder-1, limits.ByteCountPerHolder-newByteCount).Error; err != nil {
		return err
	}
	if err := self.tx.Exec(`DELETE FROM "mcp_held_result" WHERE "id" IN (
		SELECT "id" FROM (
			SELECT "id", SUM(octet_length("result_text")) OVER newest AS "cumulative_bytes"
			FROM "mcp_held_result"
			WINDOW newest AS (ORDER BY "stored_at" DESC, "id" DESC)
		) AS "ranked" WHERE "cumulative_bytes" > ?)`,
		limits.ByteCount-newByteCount).Error; err != nil {
		return err
	}
	held.StoredAt = now
	return self.tx.Create(held).Error
}

func (self *transaction) FindMcpHeldResult(holder, id string, notBefore time.Time) (*McpHeldResult, error) {
	var found []McpHeldResult
	if err := self.tx.Where(`"id" = ? AND "holder" = ? AND "stored_at" >= ?`, id, holder, notBefore).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

func (self *transaction) DeleteMcpHeldResultsStoredBefore(before time.Time) (int64, error) {
	result := self.tx.Where(`"stored_at" < ?`, before).Delete(&McpHeldResult{})
	return result.RowsAffected, result.Error
}
