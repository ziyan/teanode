package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A search lists one owner's hits after another's, so cutting it as it comes
// would show only the first owner's. Interleaved, every owner is in the first
// few, each owner's own order is kept, and nothing is lost.
func TestSharedHitsAreInterleavedByOwner(t *testing.T) {
	t.Parallel()

	hit := func(owner, path string) *models.SharedHit {
		return &models.SharedHit{OwnerUserID: owner, OwnerUsername: owner, Path: path}
	}
	hits := []*models.SharedHit{
		hit("ada", "people/a"), hit("ada", "people/b"), hit("ada", "people/c"), hit("ada", "people/d"),
		hit("ben", "trips/x"),
		hit("cora", "home/y"), hit("cora", "home/z"),
	}
	var order []string
	for _, each := range InterleaveSharedHits(hits) {
		order = append(order, each.Path)
	}
	want := "people/a trips/x home/y people/b home/z people/c people/d"
	if got := strings.Join(order, " "); got != want {
		t.Errorf("the order is %q, want %q", got, want)
	}
	if len(InterleaveSharedHits(nil)) != 0 {
		t.Error("no hits, no order")
	}
}
