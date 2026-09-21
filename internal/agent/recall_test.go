package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Reciprocal rank fusion ranks by position, not by score, which is the
// point: a full-text rank and a cosine are not on the same scale and
// cannot be added. A row two searches both found beats one only the best
// search found, in first place.
func TestFusionPrefersWhatTwoSearchesAgree(t *testing.T) {
	page := func(id string) *models.AgentNode { return &models.AgentNode{ID: id, Path: "topics/" + id} }

	byMeaning := []*models.AgentNode{page("a"), page("b"), page("c")}
	byWords := []*models.AgentNode{page("d"), page("b")}

	ranked := fuseNodes(10, byMeaning, byWords)
	if len(ranked) != 4 {
		t.Fatalf("every row of both lists, once each: %d", len(ranked))
	}
	if ranked[0].ID != "b" {
		t.Fatalf("what both found comes first, and got %q", ranked[0].ID)
	}
	// "a" was first in one list, "d" first in the other; the tie is
	// broken steadily rather than at random.
	if ranked[1].ID != "a" && ranked[1].ID != "d" {
		t.Fatalf("then the two firsts: %q", ranked[1].ID)
	}
	if shorter := fuseNodes(2, byMeaning, byWords); len(shorter) != 2 {
		t.Fatalf("the limit is kept: %d", len(shorter))
	}
}

func TestFusionOfFacts(t *testing.T) {
	fact := func(id string) *models.AgentFact { return &models.AgentFact{ID: id, Text: id} }
	ranked := fuseFacts(10, []*models.AgentFact{fact("x"), fact("y")}, []*models.AgentFact{fact("y")})
	if len(ranked) != 2 || ranked[0].ID != "y" {
		t.Fatalf("what both found comes first: %v", ranked)
	}
}

// Two facts can be near in meaning and be about two different people. The
// cosine alone would merge them and lose one, so a shared name or number
// is required where either sentence has one.
func TestATwinHasToNameTheSameThing(t *testing.T) {
	if !sharesAName("Reports to Alice on the platform team.", "Now works for Alice.") {
		t.Fatalf("the same name in both: a twin")
	}
	if sharesAName("The boat next door is Marigold.", "The boat next door is Puffin.") {
		t.Fatalf("two names, two facts, however near they sit")
	}
	if !sharesAName("Repainted every spring.", "Gets a coat of paint each spring.") {
		t.Fatalf("neither names anything, so the cosine decides alone")
	}
	if sharesAName("The invoice was 4200.", "The invoice was 3100.") {
		t.Fatalf("two numbers are two facts")
	}
	// The first word of a sentence is capitalized because it is first,
	// which says nothing about what it names.
	if !sharesAName("They moved to Tokyo.", "Tokyo is where they live now.") {
		t.Fatalf("Tokyo is in both: %v %v", properNouns("They moved to Tokyo."), properNouns("Tokyo is where they live now."))
	}
	// Except the page's own name, which is a name for certain. Both of
	// these open with it and share nothing else, and a page fills up with
	// exactly this shape if they are not recognized as one fact.
	if !sharesAName("Marigold is the neighbour's sailing boat.",
		"Marigold belongs to the neighbour next door.", "Marigold") {
		t.Fatalf("the page's own name counts wherever it appears")
	}
	// And knowing the page's name does not make two different facts one:
	// the other names in them still have to agree.
	if sharesAName("Marigold was repainted by Alice.",
		"Marigold was repainted by Bob.", "Marigold") {
		t.Fatalf("two people, two facts, however near they sit")
	}
}

// A negation is the one pair of sentences a cosine and a name check
// cannot keep apart: the same subject, the same names, the opposite
// meaning. Nothing is folded across one, and neither the name check nor
// the similarity is asked about it.
func TestANegationIsSeen(t *testing.T) {
	if !negates("She prefers tea.", "She no longer prefers tea.") {
		t.Fatalf("one of the two says the opposite")
	}
	if !negates("They drink coffee.", "They don't drink coffee.") {
		t.Fatalf("the contraction is a negation too")
	}
	// At either end of the sentence, where a token written with the
	// spaces around it would otherwise miss it.
	if !negates("Never took the job.", "Took the job in March.") {
		t.Fatalf("the first word counts")
	}
	if !negates("The standing order stopped.", "The standing order runs monthly.") {
		t.Fatalf("the last word counts, punctuation and all")
	}
	// Both negated is not the case this catches: those are two wordings
	// of one statement, and folding them is right.
	if negates("She never drinks tea.", "She has never drunk tea.") {
		t.Fatalf("both say the same thing the same way round")
	}
	if negates("She prefers tea.", "Tea is what she prefers.") {
		t.Fatalf("neither is a negation")
	}
	// A word that merely contains one of the tokens is not one: "another"
	// and "nothing" are in half the sentences a graph holds.
	if negates("Another invoice arrived.", "The invoice arrived on Tuesday.") {
		t.Fatalf("a token has to be a word")
	}
}

// A page's line in the index says where it is, what it is called, and
// enough of what it says to be worth the tokens -- inside a width, so a
// long summary cannot push the rest of the index out of the prompt.
func TestIndexLineFitsItsWidth(t *testing.T) {
	node := &models.AgentNode{
		Path: "people/alice-chen", Name: "Alice Chen",
		Summary: "Runs the platform team at Acme. Joined in 2019 from a company nobody has heard of, and is the person to ask about anything that touches the fleet.",
	}
	line := node.IndexLine(120)
	if len(line) > 124 {
		t.Fatalf("within its width: %d characters", len(line))
	}
	if !contains(line, "people/alice-chen") || !contains(line, "Alice Chen") {
		t.Fatalf("the path and the name: %q", line)
	}
	if !contains(line, "Runs the platform team") {
		t.Fatalf("and the first sentence: %q", line)
	}
	// A page whose name is its last segment does not say it twice.
	plain := &models.AgentNode{Path: "topics/kernel", Name: "kernel"}
	if line := plain.IndexLine(120); line != "topics/kernel" {
		t.Fatalf("no need to repeat the segment: %q", line)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

// A fact says what it is when it is shown: that it was inferred rather
// than said, and when it was true.
func TestAFactSaysWhatItIs(t *testing.T) {
	plain := &models.AgentFact{Text: "Runs the platform team."}
	if plain.Line() != "Runs the platform team." {
		t.Fatalf("a plain fact is its sentence: %q", plain.Line())
	}
	guessed := &models.AgentFact{Text: "Wrote the payload angle check.", Inferred: true}
	if !contains(guessed.Line(), "inferred") {
		t.Fatalf("an inference says so: %q", guessed.Line())
	}
}
