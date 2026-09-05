package db

import (
	"time"
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
