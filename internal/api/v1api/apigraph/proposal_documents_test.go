package apigraph

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Proposal cards declare their mutations separately from the full editors.
// Validate those actual documents so a renamed argument cannot break acceptance.
func TestProposalCardDocumentsMatchSchema(test *testing.T) {
	pattern := regexp.MustCompile("(?s)`\\s*((?:query|mutation|subscription)\\b[^`]*)`")
	graph := &graph{schema: buildSchemaForValidation(test)}
	for _, sourcePath := range []string{"components/proposalCard.tsx", "hooks/useCalendarMutation.ts"} {
		sourceText, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "web", "src", sourcePath))
		if err != nil {
			test.Fatal(err)
		}
		documents := pattern.FindAllStringSubmatch(string(sourceText), -1)
		if len(documents) < 4 {
			test.Fatalf("found only %d documents in %s", len(documents), sourcePath)
		}
		for _, document := range documents {
			if _, rejected := graph.prepareGraphRequest(&graphRequest{Query: document[1]}); rejected != nil {
				test.Errorf("%s document rejected: %v", sourcePath, rejected.Errors)
			}
		}
	}
}
