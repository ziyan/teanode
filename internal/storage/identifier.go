package storage

import (
	"fmt"
	"strings"
)

// validateIdentifier keeps local shard paths and object keys interchangeable.
func validateIdentifier(identifier string) error {
	if len(identifier) < 2 || strings.ContainsAny(identifier, "/\\.\x00") {
		return fmt.Errorf("storage: %q is not a usable identifier", identifier)
	}
	return nil
}
