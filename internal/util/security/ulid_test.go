package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

func TestNewULID(t *testing.T) {
	t.Parallel()
	security.NewULID()
}

func BenchmarkNewULID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		security.NewULID()
	}
}
