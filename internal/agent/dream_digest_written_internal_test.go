package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A document is the person's own words when a record source marked it
// @you or a source wrote their username or name as its author; anybody
// else's, or one with no author, is not.
func TestWrittenByThePersonIsTheirOwnDocuments(t *testing.T) {
	owner := &models.User{Username: "river", Name: "River Example"}
	documents := []*models.AgentDocument{
		{ID: "marked", Metadata: map[string]any{"author": "@you"}},
		{ID: "username", Metadata: map[string]any{"author": "River"}},
		{ID: "name", Metadata: map[string]any{"author": "river example"}},
		{ID: "somebody", Metadata: map[string]any{"author": "build lead"}},
		{ID: "nobody"},
	}
	written := writtenByThePerson(documents, owner)
	for _, id := range []string{"marked", "username", "name"} {
		if !written[id] {
			t.Fatalf("%s is the person's own", id)
		}
	}
	if written["somebody"] || written["nobody"] || len(written) != 3 {
		t.Fatalf("only the person's own documents: %v", written)
	}
}
