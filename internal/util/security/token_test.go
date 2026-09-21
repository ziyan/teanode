package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

func TestEncodeAndDecodeToken(t *testing.T) {
	t.Parallel()
	secret := []byte("test_secret")
	id := security.NewULID()
	key := "0123456789abcdef"
	value := security.EncodeToken("test", id, key, secret)
	t.Logf("token: %s", value)
	decodedId, decodedKey, err := security.DecodeToken("test", value, secret)
	if err != nil {
		t.Fatalf("failed to decode token: %s", err)
	}
	if decodedId != id {
		t.Fatalf("%q != %q", decodedId, id)
	}
	if decodedKey != key {
		t.Fatalf("%q != %q", decodedKey, key)
	}
}

func TestEncodeTokenAsCode(t *testing.T) {
	t.Parallel()
	secret := []byte("test_secret")
	id := security.NewULID()
	key := "0123456789abcdef"
	code := security.EncodeTokenAsCode("test", id, key, secret)
	t.Logf("code: %s", code)
	if len(code) != 6 {
		t.Fatalf("failed to encode token as code: %q", code)
	}
}

func BenchmarkEncodeToken(b *testing.B) {
	b.StopTimer()

	secret := []byte("test_secret")
	id := security.NewULID()
	key := "0123456789abcdef"

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		security.EncodeToken("test", id, key, secret)
	}

	b.StopTimer()
}

func BenchmarkEncodeTokenAsCode(b *testing.B) {
	b.StopTimer()

	secret := []byte("test_secret")
	id := security.NewULID()
	key := "0123456789abcdef"

	b.StartTimer()

	for i := 0; i < b.N; i++ {
		security.EncodeTokenAsCode("test", id, key, secret)
	}

	b.StopTimer()
}
