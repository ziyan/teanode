package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What the person wants and what they settled do not fade. Everything
// else does, and nothing ever reaches zero.
func TestWhatDoesAndDoesNotDecay(t *testing.T) {
	now := time.Now()
	longAgo := now.AddDate(-5, 0, 0)

	for _, kind := range []models.AgentFactKind{models.FactPreference, models.FactDecision} {
		fact := &models.AgentFact{Kind: kind, CreatedAt: longAgo}
		if weight := decayOfFact(fact, now); weight != 1 {
			t.Fatalf("a %s does not fade: %v", kind, weight)
		}
	}

	stale := &models.AgentFact{Kind: models.FactPlain, CreatedAt: longAgo}
	weight := decayOfFact(stale, now)
	if weight >= 0.2 {
		t.Fatalf("five years should cost an ordinary fact most of its weight: %v", weight)
	}
	if weight < decayFloor {
		t.Fatalf("and never all of it: %v", weight)
	}

	fresh := &models.AgentFact{Kind: models.FactPlain, CreatedAt: now}
	if decayOfFact(fresh, now) <= weight {
		t.Fatalf("today outranks five years ago")
	}
}

// A note written today about 2019 is about 2019. Ranking it as today's
// news would put the wrong answer first.
func TestAFactIsAgedFromWhenItWasTrue(t *testing.T) {
	now := time.Now()
	old := now.AddDate(-6, 0, 0)
	aboutThePast := &models.AgentFact{Kind: models.FactEvent, CreatedAt: now, HappenedAt: &old}
	aboutNow := &models.AgentFact{Kind: models.FactEvent, CreatedAt: now}
	if decayOfFact(aboutThePast, now) >= decayOfFact(aboutNow, now) {
		t.Fatalf("what happened six years ago ranks under what happened today")
	}
}

// Something the person keeps coming back to is current whatever its date
// says -- but it does not get to pretend to be new.
func TestBeingWantedLiftsAStaleFact(t *testing.T) {
	now := time.Now()
	old := now.AddDate(-3, 0, 0)
	lately := now.AddDate(0, 0, -2)
	forgotten := &models.AgentFact{Kind: models.FactPlain, CreatedAt: old}
	wanted := &models.AgentFact{Kind: models.FactPlain, CreatedAt: old, UsedAt: &lately}
	fresh := &models.AgentFact{Kind: models.FactPlain, CreatedAt: now}

	if decayOfFact(wanted, now) <= decayOfFact(forgotten, now) {
		t.Fatalf("being wanted lately counts for something")
	}
	if decayOfFact(wanted, now) >= decayOfFact(fresh, now) {
		t.Fatalf("and not for everything")
	}
}

// An answer the agent worked out ranks under one somebody said.
func TestAnInferenceRanksUnderSomethingSaid(t *testing.T) {
	now := time.Now()
	said := &models.AgentFact{Kind: models.FactPlain, CreatedAt: now}
	guessed := &models.AgentFact{Kind: models.FactPlain, CreatedAt: now, Inferred: true}
	if decayOfFact(guessed, now) >= decayOfFact(said, now) {
		t.Fatalf("an inference is worth less than a statement")
	}
}

// A period page is addressed by its date. One that faded would be a page
// nobody could ever ask for.
func TestAPeriodPageDoesNotFade(t *testing.T) {
	now := time.Now()
	old := now.AddDate(-4, 0, 0)
	period := &models.AgentNode{Kind: models.NodePeriod, ModifiedAt: old}
	if decayOfNode(period, now) != 1 {
		t.Fatalf("time/2021 is found by asking for 2021")
	}
	pinned := &models.AgentNode{Kind: models.NodeTopic, ModifiedAt: old, Pinned: true}
	if decayOfNode(pinned, now) != 1 {
		t.Fatalf("a pinned page is pinned")
	}
	ordinary := &models.AgentNode{Kind: models.NodeTopic, ModifiedAt: old}
	if decayOfNode(ordinary, now) >= 1 {
		t.Fatalf("an ordinary page four years untouched sinks")
	}
}

// A link that has gone stale is kept, ranked lower, and read in the past
// tense: somebody who left a project two years ago did work on it.
func TestAStaleLinkIsSaidInThePastTense(t *testing.T) {
	now := time.Now()
	recent := &models.AgentEdge{Relation: models.EdgeWorksOn, CreatedAt: now, FromPath: "a", ToPath: "b", ToName: "Portal"}
	weight, stale := decayOfEdge(recent, now)
	if stale || weight < 0.9 {
		t.Fatalf("a link made today is current: %v %v", weight, stale)
	}
	if got := recent.Sentence("a", stale); got != "works on Portal (b)" {
		t.Fatalf("and reads in the present tense: %q", got)
	}

	old := now.AddDate(-3, 0, 0)
	ancient := &models.AgentEdge{Relation: models.EdgeWorksOn, CreatedAt: old, FromPath: "a", ToPath: "b", ToName: "Portal"}
	weight, stale = decayOfEdge(ancient, now)
	if !stale {
		t.Fatalf("three years is stale")
	}
	if weight <= 0 {
		t.Fatalf("and still worth keeping: %v", weight)
	}
	if got := ancient.Sentence("a", stale); got != "worked on Portal (b)" {
		t.Fatalf("read in the past tense: %q", got)
	}

	// Reading about somebody's old project does not put them back on it.
	// Use counted towards the age here until the night's co-activation
	// pass started writing it, at which point a relation nobody had
	// checked in three years came back sounding current.
	read := *ancient
	read.UsedAt = &now
	if _, stale = decayOfEdge(&read, now); !stale {
		t.Fatal("having been read lately does not make an old link current again")
	}
}

// An edge is read from whichever end the reader is standing at, and
// carries what the link is actually about.
func TestAnEdgeReadsFromEitherEnd(t *testing.T) {
	edge := &models.AgentEdge{
		Relation: models.EdgeWorksOn,
		FromPath: "people/alice-chen", FromName: "Alice Chen",
		ToPath: "projects/portal", ToName: "Portal",
		Note: "led the API rewrite",
	}
	if got := edge.Sentence("people/alice-chen", false); got != "works on Portal (projects/portal) — led the API rewrite" {
		t.Fatalf("from her page: %q", got)
	}
	if got := edge.Sentence("projects/portal", false); got != "is worked on by Alice Chen (people/alice-chen) — led the API rewrite" {
		t.Fatalf("from the project's page: %q", got)
	}
}
