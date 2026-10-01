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

// A remembered fact keeps a date given to the day as a day, and its
// evidence says when the message it came from was said, so a prompt can
// say both: when it happened and when it was learned.
func TestARememberedFactKnowsWhenItHappenedAndWhenItWasSaid(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		for _, said := range theirSentence.FindAllStringSubmatch(prompt, -1) {
			if !strings.Contains(said[2], "exhibit") {
				continue
			}
			return fmt.Sprintf(`{"facts":[
				{"path":"self","node_kind":"self","node_name":"","kind":"event",
				 "text":"Went to the glass exhibit.","happened":"2023-01-14",
				 "quote":"I went to the glass exhibit","message_id":%q}
			],"links":[],"supersedes":[]}`, said[1])
		}
		return `{"facts":[]}`
	})

	before := time.Now().Add(-time.Minute)
	world.say(t, "user", "I went to the glass exhibit on Saturday.")
	world.say(t, "assistant", "Noted.")
	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, models.PathSelf)
		if err != nil || node == nil {
			t.Fatalf("the self page: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range facts {
			if !strings.Contains(fact.Text, "glass exhibit") {
				continue
			}
			if fact.HappenedPrecision != models.HappenedDay || fact.HappenedText() != "14 Jan 2023" {
				t.Fatalf("a day is kept as a day: %q %q", fact.HappenedPrecision, fact.HappenedText())
			}
			said := fact.SaidAt()
			if said == nil || said.Before(before) {
				t.Fatalf("the evidence says when the message was said: %v", said)
			}
			return
		}
		t.Fatalf("the fact was filed: %+v", facts)
	})
}
