package agent

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A turn a chat app, an MCP program or a command left running started is not
// the person's. If it leaves a question open and the person answers later, the
// turn that carries the answer on is not the person's either; one that began
// in the dashboard stays theirs. A card kept before the flag was recorded is
// not the person's.
func TestAResumedTurnIsThePersonsOnlyIfTheOneItCarriesOnWas(t *testing.T) {
	saved := interactionLiveWait
	interactionLiveWait = 200 * time.Millisecond
	defer func() { interactionLiveWait = saved }()

	for name, test := range map[string]struct {
		surface      string
		isForgotten  bool
		isPersonsRun bool
	}{
		"telegram":                  {surface: "telegram"},
		"mcp":                       {surface: "mcp"},
		"drawer":                    {surface: "drawer", isPersonsRun: true},
		"drawer, flag not recorded": {surface: "drawer", isForgotten: true, isPersonsRun: true},
	} {
		t.Run(name, func(t *testing.T) {
			database, release := dbtest.AcquireDatabase(t)
			defer release()
			arguments, _ := json.Marshal(map[string]any{"question": "Which account should I use?", "choices": []string{"Personal", "Work"}})
			askRound := fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_q","function":{"name":"ask_user","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, string(arguments))
			provider := scriptedProvider([]string{askRound, saidByModel("I will wait for you."), saidByModel("Done.")})
			defer provider.Close()
			worker, run := digestSplitWorld(t, database, provider.URL)

			var conversation *models.AgentConversation
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				var err error
				if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, Surface: test.surface, LastAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			})
			operations := &digestSplitOperations{}
			turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation, Message: "summarize https://hostile.example/page", Surface: test.surface, IsPersonsTap: test.isPersonsRun})
			if err != nil {
				t.Fatal(err)
			}
			if turn.IsStartedByPerson() != test.isPersonsRun {
				t.Fatalf("the first turn IsStartedByPerson = %v", turn.IsStartedByPerson())
			}
			drainTurn(turn)
			var interaction *models.AgentInteraction
			dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
				open, err := tx.ListOpenAgentInteractions(conversation.ID)
				if err != nil || len(open) != 1 {
					t.Fatalf("the question is kept open: %+v %v", open, err)
				}
				interaction = open[0]
				if interaction.IsStartedByPerson != test.isPersonsRun {
					t.Fatalf("the card remembers IsStartedByPerson = %v", interaction.IsStartedByPerson)
				}
				if isClaimed, err := tx.ClaimAgentInteraction(interaction.ID, "Work"); err != nil || !isClaimed {
					t.Fatal("claim")
				}
			})
			if test.isForgotten {
				interaction.IsStartedByPerson = false
			}
			resumed, err := worker.ResumeInteraction(&ResumeSettings{Interaction: interaction, Answer: "Work", Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation}, "[chat about it]")
			if err != nil || resumed == nil {
				t.Fatalf("ResumeInteraction: %v", err)
			}
			drainTurn(resumed)
			if want := test.isPersonsRun && !test.isForgotten; resumed.IsStartedByPerson() != want {
				t.Errorf("the turn that carries on the answer IsStartedByPerson = %v, want %v", resumed.IsStartedByPerson(), want)
			}
		})
	}
}
