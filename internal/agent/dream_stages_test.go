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
		"Recent":     []string{"agreed to send the Rivermouth team a revised drawing"},
		"Most":       8,
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	for _, wanted := range []string{"Alice", "projects/portal", "Rivermouth", "At most 8 questions"} {
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

// The prompt that decides what is worth opening is spending the person's
// money, so it has to say so, name what is worth it and what is not, and
// leave the model a way to answer "none of these" -- which is the right
// answer for most batches of a chat archive.
func TestTheAttachmentPromptSaysWhatIsWorthTheMoney(t *testing.T) {
	prompt, err := render("attachments.txt", map[string]any{
		"PersonName": "Alice",
		"Count":      3,
		"Most":       1,
		"Items":      "[doc-1] shot.png — 412 kB — image/png — in #support\n    bob: look at this",
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	for _, wanted := range []string{
		"Alice", "shot.png", "#support", "look at this",
		"their money", "avatar", "logo", "meme", "few kilobytes",
		`{"open": [{"id":`, "An empty list is the right answer",
	} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("the prompt says %q:\n%s", wanted, prompt)
		}
	}
}

// The prompt that reads a picture asks for what a person wants months
// later: what it shows, and the text in it read out rather than
// summarised, with the words that carry meaning named one by one.
func TestThePicturePromptAsksForTheWordsInTheImage(t *testing.T) {
	prompt, err := render("picture.txt", map[string]any{
		"PersonName": "Alice",
		"Name":       "shot.png",
		"Where":      "in #support, thread the container will not start",
		"Said":       "look at this, it dies the moment it starts",
		"Why":        "the failing container in the #support thread",
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	for _, wanted := range []string{
		"Alice", "shot.png", "#support", "look at this",
		"word for\nword", "do not summarise", "Error messages", "Timestamps",
		"Container is empty", "Leave out the furniture",
	} {
		if !strings.Contains(prompt, wanted) {
			t.Fatalf("the prompt says %q:\n%s", wanted, prompt)
		}
	}

	// With nothing said around the picture, the section is absent rather
	// than empty: a heading with nothing under it reads as a message that
	// said nothing rather than as a record that kept nothing.
	quiet, err := render("picture.txt", map[string]any{
		"PersonName": "Alice", "Name": "shot.png",
	})
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	if strings.Contains(quiet, "What was said in the message") {
		t.Fatalf("a picture nobody said anything about leaves the heading out:\n%s", quiet)
	}
}

func TestReviseWordingSaysTheOldLinesTheNewWay(t *testing.T) {
	cases := map[string]string{
		"1 commits by 1 people, August 2015 to August 2015.":     "1 commits by 1 person, August 2015.",
		"63 commits by 4 people, October 2014 to April 2016.":    "63 commits by 4 people, October 2014 to April 2016.",
		"Ziyan wrote 5 of them, November 2012 to November 2012.": "Ziyan wrote 5 of the commits, November 2012.",
		"Ziyan wrote 389 of them, April 2013 to January 2015.":   "Ziyan wrote 389 of the commits, April 2013 to January 2015.",
		"Written in Go.": "Written in Go.",
		"Its readme says: a test, July 2026 to July 2026 it ran.": "Its readme says: a test, July 2026 to July 2026 it ran.",
	}
	for old, want := range cases {
		if got := reviseWording(old); got != want {
			t.Errorf("%q: got %q, want %q", old, got, want)
		}
	}
}
