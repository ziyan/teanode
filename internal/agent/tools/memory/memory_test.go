package memory

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A memory always reaches the conversation, whatever runs the model
// addressed it to, and a name that is not an audience is dropped.
func TestMemoryAudiencesAlwaysIncludeTheConversation(t *testing.T) {
	got := factAudiences([]string{"Triage", " reply ", "nonsense", "ask"})
	want := []models.AgentAudience{models.AudienceAsk, models.AudienceTriage, models.AudienceReply}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("factAudiences = %v, want %v", got, want)
	}
	if got := factAudiences(nil); !reflect.DeepEqual(got, []models.AgentAudience{models.AudienceAsk}) {
		t.Fatalf("an unaddressed memory should reach the conversation, got %v", got)
	}
}

// A page the model opens says which of its links nobody has confirmed.
//
// This is the one place a proposed link reaches a prompt, so it is the
// one place the hedge has to be: read as a bare relation, a link the
// night guessed from a walk was repeated to the person as a fact.
func TestAPageSaysWhichLinksAreProposed(t *testing.T) {
	node := &models.AgentNode{Path: "people/alice-chen", Name: "Alice Chen", Kind: models.NodePerson}
	edges := []*models.AgentEdge{
		{
			Relation: models.EdgeWorksOn, Status: models.EdgeStated,
			FromPath: "people/alice-chen", ToPath: "projects/portal",
		},
		{
			Relation: models.EdgeRelatedTo, Status: models.EdgeProposed,
			FromPath: "people/alice-chen", ToPath: "things/gripper",
		},
		{
			Relation: models.EdgeKnows, Status: models.EdgeProposed,
			FromPath: "people/bo-nakamura", ToPath: "people/alice-chen",
		},
	}
	page := renderPage(node, nil, edges, nil)

	if !strings.Contains(page, "→ works_on projects/portal\n") {
		t.Fatalf("a stated link is said flat:\n%s", page)
	}
	if !strings.Contains(page, "→ perhaps related_to things/gripper (the agent's guess)") {
		t.Fatalf("a proposed link outward is hedged:\n%s", page)
	}
	if !strings.Contains(page, "← people/bo-nakamura perhaps knows this (the agent's guess)") {
		t.Fatalf("and so is one pointing this way:\n%s", page)
	}
}
