package agent

import (
	"math"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// Decay: how much less a thing is worth putting in front of the model
// because of when it was true.
//
// Two things use it, for two reasons. The nightly run uses it to decide
// what leaves the index, which is about the size of a prompt. Recall uses
// it to order what a turn found, which is about being right: a question
// about a person should surface where they work now before where they
// worked in 2019, and both are true, so a filter cannot do it and an
// ordering can.
//
// What does not decay is the point of the rule. A preference and a
// decision are asked for by name and are what the person actually wants
// remembered -- "never answer the landlord automatically" does not become
// less true in March. A period page is addressed by its date, so decaying
// it would mean last year's page could never be found. Those three are
// exempt, and everything else fades.

// The half-lives. A thing is worth half as much once this long has
// passed, a quarter at twice that, and never nothing: what decays to a
// small number still ranks above what did not match at all.
const (
	// statusHalfLife is for what is true until it changes -- where
	// somebody works, what a project is doing. Three months, because
	// that is about how long a stated arrangement lasts before somebody
	// would want to check it.
	statusHalfLife = 90 * 24 * time.Hour

	// eventHalfLife is for what happened, which does not stop being true
	// and does stop being what somebody means. A year.
	eventHalfLife = 365 * 24 * time.Hour

	// howToHalfLife is for a way of doing something. Slow, because a
	// method is worth keeping until it is wrong, and when it is wrong it
	// is superseded rather than aged out.
	howToHalfLife = 3 * 365 * 24 * time.Hour

	// decayFloor is the least a thing can be worth. Never zero: a fact
	// from eleven years ago that answers the question is still the answer,
	// and reaching zero would mean it could not be found at all.
	decayFloor = 0.15
)

// halfLifeOf is how quickly a fact of this kind loses its claim on the
// prompt, and whether it loses it at all.
func halfLifeOf(kind models.AgentFactKind) (time.Duration, bool) {
	switch kind {
	case models.FactPreference, models.FactDecision:
		// What the person wants and what they settled. Asked for by
		// name, and the whole reason anything is kept.
		return 0, false
	case models.FactEvent:
		return eventHalfLife, true
	case models.FactHowTo:
		return howToHalfLife, true
	}
	return statusHalfLife, true
}

// decayOf is the weight a thing keeps, between decayFloor and one.
func decayOf(age time.Duration, halfLife time.Duration) float64 {
	if halfLife <= 0 || age <= 0 {
		return 1
	}
	weight := math.Pow(0.5, age.Seconds()/halfLife.Seconds())
	if weight < decayFloor {
		return decayFloor
	}
	return weight
}

// decayOfFact is how much a fact's age costs it.
//
// Aged from when it was true where that is known, and from when it was
// written otherwise. Those are different questions: a note written today
// about something that happened in 2019 is about 2019, and ranking it as
// though it were today's news would put the wrong answer first.
//
// Being used resets nothing but does count: something the person keeps
// coming back to is current whatever its date says, which is the one
// signal available that the dates do not carry.
func decayOfFact(fact *models.AgentFact, now time.Time) float64 {
	halfLife, decays := halfLifeOf(fact.Kind)
	if !decays {
		return 1
	}
	when := fact.CreatedAt
	if fact.HappenedAt != nil {
		when = *fact.HappenedAt
	}
	weight := decayOf(now.Sub(when), halfLife)
	if fact.UsedAt != nil {
		// Half the distance back towards one for something wanted
		// lately, so use lifts a stale fact without letting it pretend
		// to be new.
		used := decayOf(now.Sub(*fact.UsedAt), statusHalfLife)
		weight += (1 - weight) * used * 0.5
	}
	if fact.Inferred {
		// An answer the agent put together rather than was told. Worth
		// having, and worth ranking under something somebody said.
		weight *= 0.85
	}
	return weight
}

// decayOfNode is how much a page's age costs it. A period page does not
// decay: it is addressed by its date, and a page for June 2023 that
// faded would be one nobody could ever ask for.
func decayOfNode(node *models.AgentNode, now time.Time) float64 {
	if node.Kind == models.NodePeriod || node.Pinned {
		return 1
	}
	when := node.ModifiedAt
	if node.UsedAt != nil && node.UsedAt.After(when) {
		when = *node.UsedAt
	}
	return decayOf(now.Sub(when), statusHalfLife)
}

// decayOfEdge is how much a link's age costs it, and whether it has gone
// stale enough to be said in the past tense.
//
// Somebody who left a project two years ago did work on it. The link is
// true and saying "works on" is not, so a stale link is kept, ranked
// lower, and read as "worked on".
func decayOfEdge(edge *models.AgentEdge, now time.Time) (float64, bool) {
	when := edge.CreatedAt
	if edge.HappenedAt != nil {
		when = *edge.HappenedAt
	}
	if edge.UsedAt != nil && edge.UsedAt.After(when) {
		when = *edge.UsedAt
	}
	weight := decayOf(now.Sub(when), eventHalfLife)
	// Past the second half-life a link is old enough that stating it in
	// the present tense is a claim nobody checked.
	return weight, now.Sub(when) > 2*eventHalfLife
}
