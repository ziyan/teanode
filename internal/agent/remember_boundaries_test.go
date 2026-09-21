package agent_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An answer that retires a fact has to put something in its place.
//
// Striking whatever was named in `supersedes` went round the guard the
// automatic fold has. A run whose quote could not be found in anything it
// was shown is marked inferred at half confidence exactly so that it
// cannot win an argument with its source -- and then this path let it
// retire the line the person had stated anyway.
func TestARetirementNeedsAReplacementThatStandsUp(t *testing.T) {
	stated := "My dentist is Dr Patel on Elm Street."
	round := 0
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[],"links":[],"supersedes":[]}`
		}
		round++
		if round == 1 {
			return fmt.Sprintf(`{"facts":[
				{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
				 "text":"Their dentist is on Elm Street.",
				 "quote":%q,"message_id":%q}
			],"links":[],"supersedes":[]}`, stated, said[1])
		}
		// The second round replaces it with something it did not read:
		// the quote is not in the message, so the fact will be marked
		// inferred, and it asks for the stated one to be struck.
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist is on Oak Street.",
			 "quote":"I have moved to the dentist on Oak Street","message_id":%q}
		],"links":[],"supersedes":[{"path":"people/dr-patel","number":1}]}`, said[1])
	})

	world.say(t, "user", stated)
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	world.say(t, "user", "Something unrelated about the weather.")
	world.say(t, "assistant", "Quite.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page exists: %v %s", node, err)
		}
		all, err := tx.ListAgentFacts(world.agent.ID, node.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		var stood *models.AgentFact
		for _, fact := range all {
			if strings.Contains(fact.Text, "Elm") {
				stood = fact
			}
		}
		if stood == nil {
			t.Fatalf("the stated fact is still on the page: %v", lines(all))
		}
		if stood.Dormant || stood.SupersededBy != "" {
			t.Fatalf("a fact the person stated was retired by one that cited words nobody said: %+v", stood)
		}
	})
}

// And an answer that retires without filing anything retires nothing.
//
// A page could be emptied by an answer carrying no facts at all, which is
// the cheapest possible way for a bad round to do lasting damage.
func TestARetirementWithNothingFiledRetiresNothing(t *testing.T) {
	stated := "My dentist is Dr Patel on Elm Street."
	round := 0
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[],"links":[],"supersedes":[]}`
		}
		round++
		if round == 1 {
			return fmt.Sprintf(`{"facts":[
				{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
				 "text":"Their dentist is on Elm Street.",
				 "quote":%q,"message_id":%q}
			],"links":[],"supersedes":[]}`, stated, said[1])
		}
		return `{"facts":[],"links":[],"supersedes":[{"path":"people/dr-patel","number":1}]}`
	})

	world.say(t, "user", stated)
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	world.say(t, "user", "Something unrelated.")
	world.say(t, "assistant", "Quite.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page exists: %v %s", node, err)
		}
		live, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(live) != 1 {
			t.Fatalf("the page still states what it was told, not %d facts: %v", len(live), lines(live))
		}
	})
}

// The same words about two different days are two events.
//
// An annual inspection completed in 2024 and the same inspection completed
// in 2025 are written the same way by anybody describing them. Matching on
// the words alone did not write the second row at all -- so it was not
// folded behind the first, which would at least have kept it; it was never
// filed, and the page went on saying the older date.
func TestTheSameWordsOnTwoDaysAreTwoEvents(t *testing.T) {
	round := 0
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[],"links":[],"supersedes":[]}`
		}
		round++
		when := "2024-05-02"
		if round > 1 {
			when = "2025-05-02"
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"things/the-boiler","node_kind":"thing","node_name":"The boiler","kind":"event",
			 "text":"Completed the annual inspection.","happened":%q,
			 "quote":"we completed the annual inspection","message_id":%q}
		],"links":[],"supersedes":[]}`, when, said[1])
	})

	world.say(t, "user", "This year we completed the annual inspection on the boiler.")
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	world.say(t, "user", "And again: we completed the annual inspection today.")
	world.say(t, "assistant", "Noted.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "things/the-boiler")
		if err != nil || node == nil {
			t.Fatalf("the page exists: %v %s", node, err)
		}
		all, err := tx.ListAgentFacts(world.agent.ID, node.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		years := map[int]bool{}
		for _, fact := range all {
			if fact.HappenedAt != nil {
				years[fact.HappenedAt.Year()] = true
			}
		}
		if !years[2024] || !years[2025] {
			t.Fatalf("both inspections are on the page; it holds %v:\n%s", years, strings.Join(lines(all), "\n"))
		}
	})
}

func lines(facts []*models.AgentFact) []string {
	said := make([]string, 0, len(facts))
	for _, fact := range facts {
		when := "no date"
		if fact.HappenedAt != nil {
			when = fact.HappenedAt.Format("2006-01-02")
		}
		said = append(said, fmt.Sprintf("#%d [%s] %s (dormant=%t)", fact.Number, when, fact.Text, fact.Dormant))
	}
	return said
}
