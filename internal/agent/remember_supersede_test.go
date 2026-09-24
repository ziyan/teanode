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

// retireAfterSaying files the dentist on a first round, then lets the
// second round answer with whatever second returns, given the person's
// and the agent's message identifiers, and answers with the page's facts
// as they stand afterwards, struck ones included.
func retireAfterSaying(t *testing.T, second func(theirs, ours string) string) []*models.AgentFact {
	t.Helper()
	stated := "My dentist is Dr Patel on Elm Street."
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[],"links":[],"supersedes":[]}`
		}
		if strings.Contains(prompt, "Oak Street") {
			reply := agentMessage.FindAllStringSubmatch(prompt, -1)
			ours := ""
			if len(reply) > 0 {
				ours = reply[len(reply)-1][1]
			}
			theirs := theirMessage.FindAllStringSubmatch(prompt, -1)
			return second(theirs[len(theirs)-1][1], ours)
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist is on Elm Street.","quote":%q,"message_id":%q}
		],"links":[],"supersedes":[]}`, stated, said[1])
	})
	world.say(t, "user", stated)
	world.say(t, "assistant", "Noted.")
	world.remember(t)
	world.say(t, "user", "Dr Patel moved to Oak Street, and Dr Patel sees patients on Tuesdays.")
	world.say(t, "assistant", "Quite.")
	world.rememberAgain(t, time.Now().Add(2*time.Hour))

	var all []*models.AgentFact
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page exists: %v %s", node, err)
		}
		if all, err = tx.ListAgentFacts(world.agent.ID, node.ID, true, 50); err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
	})
	return all
}

// isStanding says whether the fact saying these words is still stated.
func isStanding(facts []*models.AgentFact, words string) bool {
	for _, fact := range facts {
		if strings.Contains(fact.Text, words) {
			return !fact.Dormant && fact.SupersededBy == ""
		}
	}
	return false
}

const twoFacts = `{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
	 "text":"Dr Patel sees patients on Tuesdays.","quote":"Dr Patel sees patients on Tuesdays","message_id":%q},
	{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"%s",
	 "text":"Their dentist is on Oak Street.","quote":"Dr Patel moved to Oak Street","message_id":%q}`

// A supersession that names the fact replacing the line retires it.
func TestASupersessionRetiresTheLineItsReplacementReplaces(t *testing.T) {
	facts := retireAfterSaying(t, func(theirs, ours string) string {
		return fmt.Sprintf(`{"facts":[`+twoFacts+`],"links":[],
			"supersedes":[{"path":"people/dr-patel","number":1,"replaced_by":2}]}`, theirs, "fact", theirs)
	})
	if isStanding(facts, "Elm Street") || !isStanding(facts, "Oak Street") {
		t.Fatalf("Oak Street replaces Elm Street:\n%s", linesOf(facts))
	}
}

// Filing something else on the page is not a replacement. The old rule
// took any fact the answer filed on the page as the stand-in, so a line
// about the dentist's hours retired where the dentist is.
func TestAnotherFactOnThePageDoesNotRetireALine(t *testing.T) {
	facts := retireAfterSaying(t, func(theirs, ours string) string {
		return fmt.Sprintf(`{"facts":[`+twoFacts+`],"links":[],
			"supersedes":[{"path":"people/dr-patel","number":1}]}`, theirs, "fact", theirs)
	})
	if !isStanding(facts, "Elm Street") {
		t.Fatalf("with no replacement named, the line stays:\n%s", linesOf(facts))
	}
}

// A replacement of another kind, an event standing in for a fact, is the
// answer naming the wrong line, and both stay.
func TestAReplacementOfAnotherKindRetiresNothing(t *testing.T) {
	facts := retireAfterSaying(t, func(theirs, ours string) string {
		return fmt.Sprintf(`{"facts":[`+twoFacts+`],"links":[],
			"supersedes":[{"path":"people/dr-patel","number":1,"replaced_by":2}]}`, theirs, "event", theirs)
	})
	if !isStanding(facts, "Elm Street") {
		t.Fatalf("an event does not replace a fact:\n%s", linesOf(facts))
	}
}

// A retraction quotes the person, never the agent. Quoting the agent's own
// "Quite." retired a line with nothing in its place.
func TestARetractionQuotingTheAgentRetiresNothing(t *testing.T) {
	facts := retireAfterSaying(t, func(theirs, ours string) string {
		return fmt.Sprintf(`{"facts":[],"links":[],
			"supersedes":[{"path":"people/dr-patel","number":1,"quote":"Quite.","message_id":%q}]}`, ours)
	})
	if !isStanding(facts, "Elm Street") {
		t.Fatalf("the agent's own words retract nothing:\n%s", linesOf(facts))
	}
}
