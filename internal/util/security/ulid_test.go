package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

func BenchmarkNewULID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		security.NewULID()
	}
}
