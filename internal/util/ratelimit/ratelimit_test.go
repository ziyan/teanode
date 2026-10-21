package ratelimit_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/ratelimit"
)

func TestFindQuantumAndInterval(t *testing.T) {
	t.Parallel()
	for rate := ratelimit.KiloBytes; rate < 100*ratelimit.MegaBytes; rate += ratelimit.KiloBytes {
		ratelimit.NewBucketWithRate(float64(rate), ratelimit.GigaBytes)
	}
}

// Give puts tokens back, never past the capacity, and ignores a count that is
// not positive.
func TestGive(t *testing.T) {
	t.Parallel()
	bucket := ratelimit.NewBucketWithRate(0.001, 3)
	for range 3 {
		if !bucket.Allow() {
			t.Fatal("a token was not there")
		}
	}
	if bucket.Allow() {
		t.Fatal("a fourth token was there")
	}
	bucket.Give(1)
	if !bucket.Allow() || bucket.Allow() {
		t.Fatal("one token given back was not one token")
	}
	bucket.Give(10)
	if bucket.Available() != 3 {
		t.Fatalf("%d tokens after giving back more than the capacity, want 3", bucket.Available())
	}
	bucket.Give(0)
	bucket.Give(-2)
	if bucket.Available() != 3 {
		t.Fatalf("%d tokens after giving none back, want 3", bucket.Available())
	}
}
