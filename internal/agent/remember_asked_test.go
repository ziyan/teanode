package agent_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// What the person asked to be remembered is filed whole, past the number a
// conversation may otherwise file; what they did not ask for still stops
// there.
//
// They ask for a list of thirty statements to be kept. The run files the
// thirty, marked as asked, twenty more of its own choosing, and ten it
// marks as asked though they quote the assistant: all thirty are kept,
// and fifteen of the other thirty, since a mark the model wrote on words
// the person did not say earns nothing.
// theirReply finds the assistant's lines in the run's prompt, as
// theirSentence finds the person's.
var theirReply = regexp.MustCompile(`\[([a-zA-Z0-9]+)\] you: (.+)`)

func TestWhatThePersonAskedToRememberIsFiledWhole(t *testing.T) {
	world := newRememberWorld(t, func(prompt string) string {
		if !strings.Contains(prompt, "What to file") {
			return `{"facts":[]}`
		}
		for _, said := range theirSentence.FindAllStringSubmatch(prompt, -1) {
			if !strings.Contains(said[2], "Please remember") {
				continue
			}
			var facts []string
			for number := 1; number <= 30; number++ {
				facts = append(facts, fmt.Sprintf(`{"path":"things/lake-%d","node_kind":"thing","node_name":"Lake %d","kind":"fact","text":"Lake %d is %d metres deep.","quote":"Lake %d is %d metres deep","message_id":%q,"is_asked_to_remember":true}`,
					number, number, number, number*10, number, number*10, said[1]))
			}
			for number := 1; number <= 20; number++ {
				facts = append(facts, fmt.Sprintf(`{"path":"things/river-%d","node_kind":"thing","node_name":"River %d","kind":"fact","text":"River %d was mentioned.","quote":"Please remember","message_id":%q}`,
					number, number, number, said[1]))
			}
			for _, assistant := range theirReply.FindAllStringSubmatch(prompt, -1) {
				for number := 1; number <= 10; number++ {
					facts = append(facts, fmt.Sprintf(`{"path":"things/hill-%d","node_kind":"thing","node_name":"Hill %d","kind":"fact","text":"Hill %d was named.","quote":"I will remember them","message_id":%q,"is_asked_to_remember":true}`,
						number, number, number, assistant[1]))
				}
			}
			return `{"facts":[` + strings.Join(facts, ",") + `],"links":[],"supersedes":[]}`
		}
		return `{"facts":[]}`
	})

	var list strings.Builder
	list.WriteString("Please remember these facts:")
	for number := 1; number <= 30; number++ {
		fmt.Fprintf(&list, " Lake %d is %d metres deep.", number, number*10)
	}
	world.say(t, "user", list.String())
	world.say(t, "assistant", "I will remember them.")
	world.remember(t)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFactsLearnedSince(world.agent.ID, time.Time{}, 1000)
		if err != nil {
			t.Fatalf("ListAgentFactsLearnedSince: %s", err)
		}
		lakes, others := 0, 0
		for _, fact := range facts {
			switch {
			case strings.HasPrefix(fact.Text, "Lake "):
				lakes++
			case strings.HasPrefix(fact.Text, "River "), strings.HasPrefix(fact.Text, "Hill "):
				others++
			}
		}
		if lakes != 30 {
			t.Fatalf("every statement they asked to keep is kept: %d of 30", lakes)
		}
		if others != 15 {
			t.Fatalf("what they did not ask for stops at fifteen: %d", others)
		}
	})
}
