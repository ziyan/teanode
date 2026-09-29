package memory

import (
	"reflect"
	"strings"
	"testing"
	"time"

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
			FromPath: "people/alice-chen", ToPath: "things/latch",
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
	if !strings.Contains(page, "→ perhaps related_to things/latch (the agent's guess)") {
		t.Fatalf("a proposed link outward is hedged:\n%s", page)
	}
	if !strings.Contains(page, "← people/bo-nakamura perhaps knows this (the agent's guess)") {
		t.Fatalf("and so is one pointing this way:\n%s", page)
	}
}

// A page the model opens carries its overview under its opening and above
// its facts, with when it was written; a page with none carries nothing.
func TestAPageCarriesItsOverview(t *testing.T) {
	writtenAt := time.Date(2030, time.January, 2, 10, 0, 0, 0, time.UTC)
	node := &models.AgentNode{
		Path: "places/allotment", Name: "Allotment", Kind: models.NodePlace, Summary: "A plot by the river.",
		Overview: "## What it is\n\nFour beds and a shed.", OverviewWrittenAt: &writtenAt,
	}
	facts := []*models.AgentFact{{Number: 1, Kind: models.FactPlain, Text: "The shed leaks."}}
	page := renderPage(node, facts, nil, nil)
	opening := strings.Index(page, "A plot by the river.")
	overview := strings.Index(page, "Overview, written 2 January 2030:\n\n## What it is\n\nFour beds and a shed.")
	fact := strings.Index(page, "#1 The shed leaks.")
	if opening < 0 || overview < opening || fact < overview {
		t.Errorf("the overview is under the opening and above the facts:\n%s", page)
	}
	node.Overview = ""
	if strings.Contains(renderPage(node, facts, nil, nil), "Overview") {
		t.Errorf("a page with no overview says nothing about one")
	}
}

// A theme's reflections are shown apart from its facts, under the
// overview, each with its kind and what it cites; and a reflection is not
// a kind the model may file.
func TestAPageCarriesItsReflectionsApart(t *testing.T) {
	node := &models.AgentNode{Path: "themes/allotments", Name: "Allotments", Kind: models.NodeTopic, Overview: "## What it is\n\nTwo plots."}
	facts := []*models.AgentFact{
		{Number: 1, Kind: models.FactPlain, Text: "The plots share a water butt."},
		{Number: 2, Kind: models.FactReflection, Text: "Both plots flood every spring.", Inferred: true, Evidence: []models.Evidence{
			{Kind: models.EvidenceDream, Quote: models.ReflectionEvidencePrefix + "pattern"},
			{Kind: models.EvidenceMemory, ID: "one", Quote: "places/plot-east#3"},
			{Kind: models.EvidenceMemory, ID: "two", Quote: "places/plot-west"},
		}},
	}
	page := renderPage(node, facts, nil, nil)
	reflection := strings.Index(page, "#2 Both plots flood every spring. [pattern] (citing places/plot-east#3, places/plot-west)")
	overview := strings.Index(page, "Two plots.")
	fact := strings.Index(page, "#1 The plots share a water butt.")
	if reflection < overview || fact < reflection {
		t.Errorf("the reflection is under the overview and apart from the facts:\n%s", page)
	}
	if strings.Contains(joinKinds(), "reflection") {
		t.Errorf("the kinds offered to the model include reflection: %s", joinKinds())
	}
}
