package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Shared knowledge is read where it lives and never copied. A conversation in
// which the agent read a page somebody else shared and told the person what it
// said teaches the filing run nothing it may write down: a fact whose only
// evidence is that text is refused, counted on the run, and its page is not
// made. What the person said in the same conversation is filed as always.
func TestAFactRestingOnASharedPageIsNotFiled(t *testing.T) {
	const sharedWords = "Next appointment is Tuesday at three o'clock with Dr Okafor at the clinic."
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		person := theirMessage.FindStringSubmatch(prompt)
		agentSaid := agentMessage.FindAllStringSubmatch(prompt, -1)
		if len(person) < 2 || len(agentSaid) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.",
			 "quote":"My dentist is Dr Patel on Elm Street","message_id":%q},
			{"path":"people/a-grandparent","node_kind":"person","node_name":"A Grandparent","kind":"fact",
			 "text":"The next appointment is on Tuesday at three with Dr Okafor.",
			 "quote":"Next appointment is Tuesday at three o'clock with Dr Okafor","message_id":%q}
		],"links":[],"supersedes":[]}`, person[1], agentSaid[len(agentSaid)-1][1])
	})
	world.say(t, "user", "When is grandma's next appointment?")
	world.say(t, string("tool"), tools.SharedPageMarker+"\npeople/a-grandparent (shared by Ada Example)\n#3 "+sharedWords)
	world.say(t, "assistant", "Grandma's "+strings.ToLower(sharedWords[:1])+sharedWords[1:]+" (people/a-grandparent#3 (shared by Ada Example))")

	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel"); err != nil || node == nil {
			t.Fatalf("what the person said is filed: %v %v", node, err)
		}
		if node, err := tx.GetAgentNode(world.agent.ID, "people/a-grandparent"); err != nil || node != nil {
			t.Fatalf("what a shared page said is not, and its page is not made: %v %v", node, err)
		}
		runs, err := tx.ListAgentConversations(world.agent.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatalf("ListAgentConversations: %s", err)
		}
		for _, run := range runs {
			if strings.Contains(run.Title, "Filed 1 thing") && strings.Contains(run.Title, "1 not written down because they came from a page another person shared") {
				return
			}
		}
		t.Fatalf("no run counts the refusal: %v", titlesOf(runs))
	})
}

// A person who says it themselves has said it: the same sentence in a shared
// page does not take it from them.
func TestWhatThePersonSaidIsFiledWhateverElseWasRead(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		person := theirMessage.FindStringSubmatch(prompt)
		if len(person) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-okafor","node_kind":"person","node_name":"Dr Okafor","kind":"fact",
			 "text":"Sees grandma on Tuesday at three.",
			 "quote":"Tuesday at three o'clock with Dr Okafor","message_id":%q}
		],"links":[],"supersedes":[]}`, person[1])
	})
	world.say(t, "user", "Remember that grandma sees Tuesday at three o'clock with Dr Okafor.")
	world.say(t, "tool", tools.SharedPageMarker+"\n#3 Next appointment is Tuesday at three o'clock with Dr Okafor.")
	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if node, err := tx.GetAgentNode(world.agent.ID, "people/dr-okafor"); err != nil || node == nil {
			t.Fatalf("the person's own words are filed: %v %v", node, err)
		}
	})
}

// A tool result with no marker is the person's own pages, and a fact from what
// they said is not touched by any of this.
func TestAConversationWithNoSharedPageIsFiledAsBefore(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		person := theirMessage.FindStringSubmatch(prompt)
		if !strings.Contains(prompt, "What to file") || len(person) < 2 {
			return `{"facts":[]}`
		}
		return fmt.Sprintf(`{"facts":[
			{"path":"people/dr-patel","node_kind":"person","node_name":"Dr Patel","kind":"fact",
			 "text":"Their dentist, on Elm Street.",
			 "quote":"My dentist is Dr Patel on Elm Street","message_id":%q}
		],"links":[],"supersedes":[]}`, person[1])
	})
	world.say(t, "tool", "people/dr-patel (person)\n#1 Their dentist, on Elm Street.")
	world.remember(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if node, err := tx.GetAgentNode(world.agent.ID, "people/dr-patel"); err != nil || node == nil {
			t.Fatalf("filed: %v %v", node, err)
		}
	})
}
