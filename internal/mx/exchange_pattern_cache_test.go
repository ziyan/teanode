package mx

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Every pattern a domain manager ever saved stayed compiled until the process
// ended. The cache is emptied when it holds more than maximumCachedPatterns,
// and what it held is compiled again on the next message, so a pattern still
// matches after that.
func TestThePatternCacheIsEmptiedPastItsLimit(t *testing.T) {
	t.Parallel()

	exchange := &exchange{}
	for index := range maximumCachedPatterns + 10 {
		alias := &models.Alias{ID: "alias-1", Pattern: fmt.Sprintf("^name%d$", index)}
		compiled := exchange.pattern(alias)
		if compiled == nil || !compiled.MatchString(fmt.Sprintf("NAME%d", index)) {
			t.Fatalf("pattern %d did not compile or match", index)
		}
	}

	held := 0
	exchange.directory.patterns.Range(func(_, _ any) bool {
		held++
		return true
	})
	if held > maximumCachedPatterns {
		t.Errorf("%d patterns are held, want at most %d", held, maximumCachedPatterns)
	}

	// One that was held when the cache was emptied is compiled again.
	if compiled := exchange.pattern(&models.Alias{ID: "alias-1", Pattern: "^name0$"}); compiled == nil || !compiled.MatchString("name0") {
		t.Errorf("a pattern from before the cache was emptied no longer matches")
	}
}
