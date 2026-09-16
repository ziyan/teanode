package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The two prompts the night's generative half uses. Both are asked of a
// cheap model with no conversation around them, so everything they need
// has to be in the text -- including the instruction to answer "no",
// which is the answer most walks deserve.
func TestAssociatePromptAsksForTheHonestNo(t *testing.T) {
	relations := make([]string, 0, len(models.AgentEdgeRelations))
	for _, relation := range models.AgentEdgeRelations {
		relations = append(relations, string(relation))
	}
	prompt, err := render("associate.txt", map[string]any{
		"PersonName": "Alice",
		"From":       "people/alice-chen — Alice Chen: a controls engineer",
		"To":         "things/gripper — Gripper: the payload gripper",
		"Through":    []string{"projects/portal — Portal", "things/gripper — Gripper"},
		"Relations":  strings.Join(relations, ", "),
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	for _, wanted := range []string{"Alice Chen", "Gripper", "works_on", `{"related": false}`} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("the prompt says %q:\n%s", wanted, prompt)
		}
	}
	if !strings.Contains(prompt, "ordinary answer") {
		t.Fatalf("a walk that found nothing must be told that is fine:\n%s", prompt)
	}
}

// Rehearsal without anything to rehearse against is a prompt that invents
// questions about the world; the index is what keeps it about the person.
func TestRehearsePromptIsAboutThePersonsOwnLife(t *testing.T) {
	prompt, err := render("rehearse.txt", map[string]any{
		"PersonName": "Alice",
		"Index":      []string{"projects/portal — Portal: the customer-facing site"},
		"Recent":     []string{"agreed to send the Osaka team a revised drawing"},
		"Most":       8,
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	for _, wanted := range []string{"Alice", "projects/portal", "Osaka", "At most 8 questions"} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("the prompt says %q:\n%s", wanted, prompt)
		}
	}

	// And with nothing recent, the section is absent rather than empty:
	// a heading with nothing under it reads as a gap in the person's week
	// rather than as a gap in the prompt.
	quiet, err := render("rehearse.txt", map[string]any{
		"PersonName": "Alice",
		"Index":      []string{"projects/portal — Portal"},
		"Most":       8,
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	if strings.Contains(quiet, "What changed in the last week") {
		t.Fatalf("a quiet week leaves the heading out:\n%s", quiet)
	}
}
