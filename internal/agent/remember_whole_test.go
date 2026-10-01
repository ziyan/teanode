package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A long fact and a long quote are stored whole, as the run wrote them.
//
// Both were cut to a length where they were stored, a fact at a thousand
// characters and a quote at four hundred, and the end of the sentence was
// gone for every reader with nothing to say it had been written.
func TestALongFactAndItsQuoteAreStoredWhole(t *testing.T) {
	quote := strings.Repeat("the shed has a shelf for every tool we own, ", 12) + "and the last shelf holds the ladder"
	text := strings.Repeat("The shed keeps the garden tools in order on labelled shelves. ", 24) + "The last shelf holds the ladder."
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		for _, said := range theirSentence.FindAllStringSubmatch(prompt, -1) {
			if !strings.Contains(said[2], "the ladder") {
				continue
			}
			return fmt.Sprintf(`{"facts":[
				{"path":"things/shed","node_kind":"thing","node_name":"Shed","kind":"fact",
				 "text":%q,"quote":%q,"message_id":%q}
			],"links":[],"supersedes":[]}`, text, quote, said[1])
		}
		return `{"facts":[]}`
	})

	world.say(t, "user", quote+".")
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "things/shed")
		if err != nil || node == nil {
			t.Fatalf("the page was made: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("the fact is kept: %v %s", facts, err)
		}
		if facts[0].Text != text {
			t.Fatalf("the fact is stored whole, %d characters of %d", len(facts[0].Text), len(text))
		}
		if len(facts[0].Evidence) != 1 || facts[0].Evidence[0].Quote != quote {
			t.Fatalf("and its quote: %+v", facts[0].Evidence)
		}
	})
}
