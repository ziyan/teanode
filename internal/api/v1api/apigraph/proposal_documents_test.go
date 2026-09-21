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
	sourceText, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "web", "src", "components", "proposalCard.tsx"))
	if err != nil {
		test.Fatal(err)
	}
	pattern := regexp.MustCompile("(?s)`\\s*((?:query|mutation|subscription)\\b[^`]*)`")
	documents := pattern.FindAllStringSubmatch(string(sourceText), -1)
	if len(documents) < 5 {
		test.Fatalf("found only %d proposal documents", len(documents))
	}
	graph := &graph{schema: buildSchemaForValidation(test)}
	for _, document := range documents {
		if _, rejected := graph.prepareGraphRequest(&graphRequest{Query: document[1]}); rejected != nil {
			test.Errorf("proposal document rejected: %v", rejected.Errors)
		}
	}
}
