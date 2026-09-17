package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A quote that is not in the message it cites is not a quote.
//
// This is the failure an outside review of the code found: a model that
// has understood a conversation writes the gist inside quotation marks,
// and words the person never said are stored beside the fact at full
// confidence, where anybody reading the page takes them for something
// said. The fact is kept, because the reading was real; what it loses is
// the invented words and the claim to have been told.
func TestAQuoteMustOccurInItsMessage(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[]}`
		}
		// The dentist is really in the message; this wording of it is not.
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.",
			 "quote":"I go to Dr Patel, the dentist over on Elm Street","message_id":%q}
		],"links":[],"supersedes":[]}`, said[1])
	})

	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page was made: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("the fact is kept: %v %s", facts, err)
		}
		fact := facts[0]
		if len(fact.Evidence) != 1 || fact.Evidence[0].ID == "" {
			t.Fatalf("the message it was read in is still cited: %+v", fact.Evidence)
		}
		if fact.Evidence[0].Quote != "" {
			t.Fatalf("the words nobody said are gone, not %q", fact.Evidence[0].Quote)
		}
		if !fact.Inferred {
			t.Fatal("and it reads as the agent's own rather than as something said")
		}
		if fact.Confidence != 0.5 {
			t.Fatalf("at half confidence, not %v", fact.Confidence)
		}
	})
}

// A fact citing a message the run never showed the model has nothing
// behind it, so the citation goes with the quote.
//
// An identifier a model made up looks exactly like one it copied, and a
// page citing a message nobody can open is worse than a page that admits
// it worked something out.
func TestAFactCitingNothingHasNoEvidence(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		return `{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.",
			 "quote":"My dentist is Dr Patel on Elm Street","message_id":"nosuchmessage"}
		],"links":[],"supersedes":[]}`
	})

	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page was made: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("the fact is kept: %v %s", facts, err)
		}
		fact := facts[0]
		if len(fact.Evidence) != 0 {
			t.Fatalf("nothing stands behind it: %+v", fact.Evidence)
		}
		if !fact.Inferred || fact.Confidence != 0.5 {
			t.Fatalf("so it reads as worked out, at half confidence: %+v", fact)
		}
	})
}

// The check is about words and not about typography. A transcript is
// typed by people and rendered by programs, and a model copying a line
// out of one straightens the quotation marks and folds the line breaks;
// that is not inventing anything, and a check that called it invention
// would mark most of a night's work as the agent's own guesswork.
func TestTheEvidenceCheckForgivesTypography(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		said := theirMessage.FindStringSubmatch(prompt)
		if len(said) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.",
			 "quote":"MY DENTIST is\n  Dr Patel on Elm Street","message_id":%q}
		],"links":[],"supersedes":[]}`, said[1])
	})

	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel")
		if err != nil || node == nil {
			t.Fatalf("the page was made: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(world.agent.ID, node.ID, false, 10)
		if err != nil || len(facts) != 1 {
			t.Fatalf("the fact was filed: %v %s", facts, err)
		}
		if facts[0].Inferred || facts[0].Evidence[0].Quote == "" {
			t.Fatalf("the quote is the person's words however they were typed: %+v", facts[0])
		}
	})
}
