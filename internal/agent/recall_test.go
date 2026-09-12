package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The words a turn is looked up by: long enough to mean something, no
// repeats, and not the words every sentence has.
func TestRecallWords(t *testing.T) {
	words := recallWords("Could you please tell Maria about the RAID controller, the raid controller again?")
	joined := strings.Join(words, ",")
	for _, wanted := range []string{"maria", "raid", "controller"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("%q should be looked up by %q", joined, wanted)
		}
	}
	for _, unwanted := range []string{"could", "please", "about", "again", "the"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("%q says nothing about the turn, in %q", unwanted, joined)
		}
	}
	if strings.Count(joined, "raid") != 1 {
		t.Fatalf("a word said twice is looked up once: %q", joined)
	}
	if len(recallWords(strings.Repeat("controller alternator distributor carburettor ", 10))) > recallWordsPerAsk {
		t.Fatal("one message does not search for everything")
	}
	if len(recallWords("ok ta yes")) != 0 {
		t.Fatal("short words are not worth a search")
	}
}

// What a memory is worth to a turn is how much of the turn it touches.
func TestRecallScore(t *testing.T) {
	memory := &models.AgentMemory{Title: "The RAID controller", Content: "The controller on pycad is failing.", Tags: []string{"hardware"}}
	if score := recallScore(memory, []string{"raid", "controller", "pycad"}); score != 3 {
		t.Fatalf("every word it touches counts: %d", score)
	}
	if score := recallScore(memory, []string{"hardware"}); score != 1 {
		t.Fatalf("a tag counts too: %d", score)
	}
	if score := recallScore(memory, []string{"sailing"}); score != 0 {
		t.Fatalf("an untouched memory scores nothing: %d", score)
	}
}

// Nearest ranks by meaning and keeps only what is near enough to be
// about the same thing.
func TestNearestMemories(t *testing.T) {
	boat := &models.AgentMemory{ID: "m1", Title: "Kittiwake", Vector: []float32{1, 0, 0}}
	sails := &models.AgentMemory{ID: "m2", Title: "The sails", Vector: []float32{0.9, 0.2, 0}}
	tax := &models.AgentMemory{ID: "m3", Title: "The tax return", Vector: []float32{0, 1, 0}}
	found := nearest([]float32{1, 0.05, 0}, []*models.AgentMemory{tax, sails, boat}, 5)
	if len(found) != 2 || found[0].ID != "m1" || found[1].ID != "m2" {
		t.Fatalf("the two about boats, the nearest first: %v", found)
	}
	if kept := nearest([]float32{1, 0, 0}, []*models.AgentMemory{tax}, 5); len(kept) != 0 {
		t.Fatalf("nothing near enough is nothing: %v", kept)
	}
	if kept := nearest([]float32{0, 0, 0}, []*models.AgentMemory{boat}, 5); len(kept) != 0 {
		t.Fatal("a vector of nothing ranks nothing")
	}
	if kept := nearest([]float32{1, 0}, []*models.AgentMemory{boat}, 5); len(kept) != 0 {
		t.Fatal("vectors of different lengths are not comparable")
	}
	if len(nearest([]float32{1, 0, 0}, []*models.AgentMemory{boat, sails}, 1)) != 1 {
		t.Fatal("the limit is kept")
	}
}

// Two ways of saying one thing are the same thing.
func TestSimilarity(t *testing.T) {
	if score := similarity([]float32{1, 0}, []float32{1, 0}); score < 0.999 {
		t.Fatalf("a vector is itself: %v", score)
	}
	if score := similarity([]float32{1, 0}, []float32{0, 1}); score > 0.001 {
		t.Fatalf("and not its opposite: %v", score)
	}
	if score := similarity([]float32{1, 0}, []float32{1, 0, 0}); score != 0 {
		t.Fatalf("different lengths cannot be compared: %v", score)
	}
}

// What is embedded of a memory is what it is called, what it says and
// what it was tagged with, and never more than a model will take.
func TestMemoryText(t *testing.T) {
	text := memoryText(&models.AgentMemory{Title: "Kittiwake", Content: "the neighbour's boat", Tags: []string{"boats", "neighbours"}})
	for _, wanted := range []string{"Kittiwake", "neighbour's boat", "boats", "neighbours"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("%q is missing from %q", wanted, text)
		}
	}
	long := memoryText(&models.AgentMemory{Title: "x", Content: strings.Repeat("y", memoryEmbedCharacters*2)})
	if len(long) > memoryEmbedCharacters {
		t.Fatalf("a long memory is cut: %d", len(long))
	}
}

// A memory a person addressed to sorting alone is not read out in a
// conversation because a word of it turned up in what they said.
func TestRecallKeepsToItsAudience(t *testing.T) {
	forSorting := &models.AgentMemory{ID: "m1", Title: "Newsletters", AppliesTo: []models.AgentAudience{models.AudienceTriage}}
	forTalking := &models.AgentMemory{ID: "m2", Title: "Newsletters", AppliesTo: []models.AgentAudience{models.AudienceAsk}}
	if forSorting.Addressed(models.AudienceAsk) {
		t.Fatal("a memory for sorting is not for the conversation")
	}
	if !forTalking.Addressed(models.AudienceAsk) {
		t.Fatal("and one for the conversation is")
	}
}
