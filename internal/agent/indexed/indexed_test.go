package indexed

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A name pasted out of a log is looked up exactly before anything is
// ranked, so the predicate that tells one from an ordinary word decides
// whether "who wrote mwesexecutor.py" answers with a file or with twenty
// vaguely related passages.
func TestAnIdentifierIsToldFromAnOrdinaryWord(test *testing.T) {
	test.Parallel()

	for word, wanted := range map[string]bool{
		"ResetPayloadAngularOffset": true,
		"mwesexecutor.py":           true,
		"reset_payload":             true,
		"deployment":                false,
		"the":                       false,
		"Tuesday":                   false,
		".hidden":                   false,
	} {
		if got := LooksLikeSymbol(word); got != wanted {
			test.Errorf("%q looks like an identifier: %v, want %v", word, got, wanted)
		}
	}
}

// Ranked by position rather than by score, because a full-text rank and a
// cosine are not on one scale: a passage both searches found beats one
// that only the better search put first.
func TestWhatBothSearchesFoundComesFirst(test *testing.T) {
	test.Parallel()

	byMeaning := []*models.AgentChunk{{ID: "only-meaning"}, {ID: "both"}}
	byWords := []*models.AgentChunk{{ID: "only-words"}, {ID: "both"}}

	ranked := fuse(3, byMeaning, byWords)
	if len(ranked) != 3 {
		test.Fatalf("three passages were found between them, and the fusion kept %d", len(ranked))
	}
	if ranked[0].chunk.ID != "both" {
		test.Errorf("the passage both searches found comes first, and %q did", ranked[0].chunk.ID)
	}
	if ranked[0].score <= ranked[1].score {
		test.Errorf("and scores higher than the one only one search found: %v against %v",
			ranked[0].score, ranked[1].score)
	}

	// One search alone is scored on the same scale, so a deployment
	// without an embedding model hands back numbers that mean what the
	// others mean.
	alone := rank(byWords)
	if alone[0].score <= alone[1].score {
		test.Errorf("the first of one list scores highest: %v against %v", alone[0].score, alone[1].score)
	}
	if alone[0].score >= ranked[0].score {
		test.Errorf("and below what two searches agreeing is worth: %v against %v", alone[0].score, ranked[0].score)
	}
}
