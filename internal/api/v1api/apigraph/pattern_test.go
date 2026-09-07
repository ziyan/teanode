package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// An empty pattern is a catch-all, which is how most domains are set up: one
// alias that takes whatever the others did not match. The API refused to
// create one while the server it was running on held two dozen, and while
// updating an alias into one was allowed.
func TestAnEmptyPatternIsACatchAllRatherThanMissing(t *testing.T) {
	t.Parallel()

	if err := validatePattern(""); err != nil {
		t.Errorf("an empty pattern was refused: %s", err)
	}

	// And the configuration layer agrees about what it means.
	alias := &models.Alias{Pattern: ""}
	if !alias.IsCatchAll() {
		t.Error("config does not read an empty pattern as a catch-all, so the two layers disagree")
	}
	if (&models.Alias{Pattern: "^hello$"}).IsCatchAll() {
		t.Error("a real pattern was read as a catch-all")
	}
}

// A pattern that is present still has to compile, or mail silently stops
// matching an alias somebody believes is working.
func TestAPatternThatCannotCompileIsRefused(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"^(unclosed", "*", "a{2,1}", "[z-a]"} {
		err := validatePattern(pattern)
		if err == nil {
			t.Errorf("%q was accepted but is not a valid regular expression", pattern)
			continue
		}
		if !errors.Is(err, api.ErrInvalidArguments) {
			t.Errorf("%q was refused with %v, want it to wrap ErrInvalidArguments", pattern, err)
		}
	}
	for _, pattern := range []string{"^hello$", "^(sales|support)$", ".*", "^user\\+.*$"} {
		if err := validatePattern(pattern); err != nil {
			t.Errorf("%q is a valid pattern but was refused: %s", pattern, err)
		}
	}
}
