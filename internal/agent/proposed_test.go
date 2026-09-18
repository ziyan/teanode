package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A link the night walked its way to is the agent's own guess, and is
// stored as one.
//
// Before this it was a plain link at half weight, which is to say it was
// a link: nothing downstream reads a weight as doubt, so the agent
// repeated a guess it had made at three in the morning as though
// somebody had told it. Its evidence was filed as a document too, so
// anything that went looking for the document the id named found nothing.
func TestAWalkedLinkIsProposedAndNotStated(t *testing.T) {
	through := []string{"people/alice-chen — Alice Chen", "projects/portal — Portal", "things/gripper — Gripper"}
	link := walkedLink("agent-1", "from-1", "to-1", models.EdgeWorksOn, "  led the controls work  ", through)

	if link.Status != models.EdgeProposed {
		t.Fatalf("a walk proposes rather than states: %q", link.Status)
	}
	if !link.Proposed() {
		t.Fatal("and reads as proposed")
	}
	if link.Weight != 0.5 {
		t.Fatalf("at half the weight of a stated link, not %v", link.Weight)
	}
	if link.Note != "led the controls work" {
		t.Fatalf("carrying the sentence that justifies it: %q", link.Note)
	}
	if len(link.Evidence) != 1 {
		t.Fatalf("with the walk behind it: %v", link.Evidence)
	}
	if link.Evidence[0].Kind != models.EvidenceDream {
		t.Fatalf("under the kind that says the agent worked it out, not %q", link.Evidence[0].Kind)
	}
	if link.Evidence[0].ID != "" {
		t.Fatalf("and no identifier, since there is nothing to look up: %q", link.Evidence[0].ID)
	}
	if !strings.Contains(link.Evidence[0].Quote, "projects/portal") {
		t.Fatalf("the quote is the path it walked: %q", link.Evidence[0].Quote)
	}
}

// Every other writer states, which is what an empty status means: the
// memory tool's link, the ingest's derived links, the dashboard's Link
// dialog all leave it alone and mean it.
func TestAnUnmarkedLinkIsReadAsStated(t *testing.T) {
	stated := &models.AgentEdge{Relation: models.EdgeWorksOn}
	if stated.Proposed() {
		t.Fatal("a link nobody marked is one somebody stated")
	}
}

// A proposed link is hedged wherever it is read out: "perhaps" in front
// so the claim is never flat, and whose guess it was at the end so the
// person knows whom to disagree with.
func TestAProposedLinkReadsAsAGuess(t *testing.T) {
	edge := &models.AgentEdge{
		Relation: models.EdgeRelatedTo,
		FromPath: "people/alice-chen", FromName: "Alice Chen",
		ToPath: "things/gripper", ToName: "Gripper",
		Note:   "she wrote the payload angle check",
		Status: models.EdgeProposed,
	}
	want := "perhaps related to Gripper (things/gripper) — she wrote the payload angle check (the agent's guess)"
	if got := edge.Sentence("people/alice-chen", false); got != want {
		t.Fatalf("from her page: %q", got)
	}

	// The same row from the other end, and the hedge travels with it.
	if got := edge.Sentence("things/gripper", false); !strings.HasPrefix(got, "perhaps related to Alice Chen") {
		t.Fatalf("from the gripper's page: %q", got)
	}

	// And stating it takes the hedge away rather than leaving a link that
	// says "perhaps" about something the person drew themselves.
	stated := *edge
	stated.Status = models.EdgeStated
	if got := stated.Sentence("people/alice-chen", false); strings.Contains(got, "perhaps") {
		t.Fatalf("a stated link is said flat: %q", got)
	}
}

// The relation phrases are written to follow a subject -- "Alice is
// related to Portal" -- and "perhaps" stands where that subject would, so
// the "is" has to come out or the sentence reads "perhaps is related to".
func TestAProposedLinkDropsTheDanglingIs(t *testing.T) {
	for _, each := range []struct {
		relation models.AgentEdgeRelation
		want     string
	}{
		{models.EdgePartOf, "perhaps part of Portal"},
		{models.EdgeWorksOn, "perhaps works on Portal"},
		{models.EdgeLocatedIn, "perhaps in Portal"},
		{models.EdgeAboutPlace, "perhaps about Portal"},
	} {
		edge := &models.AgentEdge{
			Relation: each.relation,
			FromPath: "a", ToPath: "b", ToName: "Portal",
			Status: models.EdgeProposed,
		}
		want := each.want + " (b) (the agent's guess)"
		if got := edge.Sentence("a", false); got != want {
			t.Errorf("%s: got %q, want %q", each.relation, got, want)
		}
	}
}
