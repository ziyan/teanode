package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

func TestGenerateRandom(t *testing.T) {
	t.Parallel()
	if data := security.GenerateRandom(10); len(data) != 10 {
		t.Fatal()
	}
}

func TestGenerateRandomHexString(t *testing.T) {
	t.Parallel()
	if data := security.GenerateRandomHexString(10); len(data) != 20 {
		t.Fatal()
	}
}

func TestGenerateRandomBase64String(t *testing.T) {
	t.Parallel()
	security.GenerateRandomBase64String(1)
}
