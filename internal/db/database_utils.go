package db

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/util/security"
)

// test if two string slices are equal
func stringSlicesAreEqual(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// test if time is equal
func optionalTimesAreEqual(first, second *time.Time) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return first.Equal(*second)
}

// test if optional reference is equal
func optionalReferencesAreEqual(first *string, second string) bool {
	var aa string
	if first != nil {
		aa = *first
	}
	return aa == second
}

func convertFromUint64Array(values []uint64) []int64 {
	newValues := make([]int64, 0, len(values))
	for _, value := range values {
		newValues = append(newValues, int64(value))
	}
	return newValues
}

// lockRow takes the row for update for the rest of the transaction, so that
// two instances changing it at once take turns. ErrNotFound when there is no
// such row.
func lockRow(tx *gorm.DB, model any, id string) error {
	result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", id).Limit(1).Find(model)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// uniqueStrings drops the empties and the duplicates, keeping the order.
func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

// newID mints an identifier: a lowercase ULID, like every other identifier
// here, sortable by when it was made.
func newID() string {
	return security.NewULID()
}

// RawQueryString reads one string cell, for tests that have to see what is
// stored rather than what is read back. Not part of the Database interface.
func (self *database) RawQueryString(query string) (string, error) {
	var value string
	err := self.db.Raw(query).Scan(&value).Error
	return value, err
}

// RawExec runs one statement, for the same tests. Not part of the interface.
func (self *database) RawExec(statement string) error {
	return self.db.Exec(statement).Error
}
