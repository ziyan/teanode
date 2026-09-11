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
