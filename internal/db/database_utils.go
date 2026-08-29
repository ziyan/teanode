package db

import (
	"time"
)

// test if two string slices are equal
func stringSlicesAreEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// test if time is equal
func optionalTimesAreEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// test if optional reference is equal
func optionalReferencesAreEqual(a *string, b string) bool {
	var aa string
	if a != nil {
		aa = *a
	}
	return aa == b
}

func convertFromUint64Array(values []uint64) []int64 {
	newValues := make([]int64, 0, len(values))
	for _, value := range values {
		newValues = append(newValues, int64(value))
	}
	return newValues
}
