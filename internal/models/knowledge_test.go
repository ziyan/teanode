package models_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A format the daemon cannot read is a typo the person should hear about
// while they are still looking at what they typed, not hours later as a
// scan that failed on a word nobody can see any more.
func TestAFormatThatIsNotAFormatIsRefused(t *testing.T) {
	t.Parallel()

	source := &models.AgentKnowledgeSource{
		Kind: models.SourceArchive,
		Name: "notes",
		Specification: models.AgentKnowledgeSpecification{
			Computer: "laptop",
			Path:     "~/.teanode/records/notes",
			Format:   "recrods",
		},
	}
	err := source.Validate()
	if err == nil {
		t.Fatalf("%q was accepted as a format", source.Specification.Format)
	}
	want := `"recrods" is not a format: files, journal or records`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the message does not say what the formats are: %s", err)
	}

	// Every format the program reads, and an empty one for a source that
	// never says, are all accepted.
	for _, format := range append([]string{""}, models.AgentKnowledgeFormats...) {
		source.Specification.Format = format
		if err := source.Validate(); err != nil {
			t.Errorf("format %q was refused: %s", format, err)
		}
	}
}
