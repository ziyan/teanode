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
