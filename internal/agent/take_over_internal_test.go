package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// browserOnATab is the browser tool with the person's browser connected,
// a tab attached, in a turn of the drawer.
func browserOnATab(t *testing.T, surface string) (*tools.Tool, *Agent, *fakeTab, *AskRun) {
	t.Helper()
	tool := FullCatalog().Get("browser")
	if tool == nil {
		t.Skip("the browser tool is not built into this catalog")
	}
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}
	browser := &fakeTab{agent: worker, agentId: "a1", answers: func(string, json.RawMessage) (bool, string) {
		return true, `{"title":"Sign in","url":"https://bank.example.org/sign-in"}`
	}}
	worker.ConnectBrowser("a1", browser, "Sign in", "https://bank.example.org/sign-in", true)
	worker.SetBrowserCanCast("a1", browser, true)
	run := &AskRun{agent: worker, settings: &AskSettings{Agent: &models.Agent{ID: "a1"}, Surface: surface}, ctx: context.Background()}
	return tool, worker, browser, run
}

func callBrowser(tool *tools.Tool, run *AskRun, arguments string) (*tools.Result, error) {
	return tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_1", Arguments: json.RawMessage(arguments)})
}

// While the person holds their tab the agent reads it but does not act in
// it, and once they hand it back it acts again.
func TestATakenOverTabIsReadNotActedIn(t *testing.T) {
	tool, worker, browser, run := browserOnATab(t, "drawer")
	worker.TakeOverTab("a1")
	for _, arguments := range []string{`{"action":"click","ref":3}`, `{"action":"navigate","url":"https://bank.example.org/"}`, `{"action":"type","ref":3,"text":"hello"}`} {
		if _, err := callBrowser(tool, run, arguments); err == nil || !strings.Contains(err.Error(), "taken over") {
			t.Fatalf("%s while the person held the tab: %v", arguments, err)
		}
	}
	if _, err := callBrowser(tool, run, `{"action":"snapshot"}`); err != nil {
		t.Fatalf("reading a taken over tab: %s", err)
	}
	if len(browser.sent) != 1 || browser.sent[0] != "snapshot" {
		t.Fatalf("only the snapshot should have reached the tab, sent %v", browser.sent)
	}
	worker.HandBackTab("a1")
	if _, err := callBrowser(tool, run, `{"action":"click","ref":3}`); err != nil {
		t.Fatalf("clicking after the tab was handed back: %s", err)
	}
}

// Nobody can take a tab over in a turn nobody is present for.
func TestAskingToTakeOverNeedsSomebodyPresent(t *testing.T) {
	tool, _, _, run := browserOnATab(t, "mail")
	if _, err := callBrowser(tool, run, `{"action":"ask_to_take_over","text":"Sign in to the bank"}`); err == nil || !strings.Contains(err.Error(), "nobody is present") {
		t.Fatalf("asking in a mail turn: %v", err)
	}
	tool, _, _, run = browserOnATab(t, "drawer")
	if _, err := callBrowser(tool, run, `{"action":"ask_to_take_over"}`); err == nil || !strings.Contains(err.Error(), "what the person should do") {
		t.Fatalf("asking without saying what for: %v", err)
	}
}

func TestTakeOverAnswerLine(t *testing.T) {
	cases := map[string]string{
		models.InteractionHandedBack + "\nSigned in": "They took the tab over and handed it back after your turn had ended. They said: Signed in The page has changed: take a snapshot before you act.",
		models.InteractionHandedBack:                 "They took the tab over and handed it back after your turn had ended. The page has changed: take a snapshot before you act.",
		"I cannot right now":                         "They replied instead of taking the tab over: I cannot right now",
	}
	for answer, want := range cases {
		if got := takeOverAnswerLine(answer, true); got != want {
			t.Errorf("%q:\n got %q\nwant %q", answer, got, want)
		}
	}
}

// A request to take the tab over that nobody answers while the turn waits
// is left open, and handing the tab back later starts a new turn that
// carries the request and what the person said.
func TestATakeOverHandedBackLateCarriesOnInANewTurn(t *testing.T) {
	saved := interactionLiveWait
	interactionLiveWait = 200 * time.Millisecond
	defer func() { interactionLiveWait = saved }()

	database, release := dbtest.AcquireDatabase(t)
	defer release()
	arguments, _ := json.Marshal(map[string]any{"action": "ask_to_take_over", "target": "tab", "text": "Sign in to the bank with the code sent to your phone"})
	askRound := fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_sign_in","function":{"name":"browser","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, string(arguments))
	provider := scriptedProvider([]string{askRound, saidByModel("I will wait for you."), saidByModel("Signed in, carrying on.")})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)
	browser := &fakeTab{agent: worker, agentId: run.Agent.ID, answers: func(string, json.RawMessage) (bool, string) { return true, `{}` }}
	worker.ConnectBrowser(run.Agent.ID, browser, "Sign in", "https://bank.example.org/sign-in", true)
	worker.SetBrowserCanCast(run.Agent.ID, browser, true)

	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, Surface: "drawer", LastAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	})
	operations := &digestSplitOperations{}
	turn, err := worker.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation, Message: "pay the water bill", Surface: "drawer"})
	if err != nil {
		t.Fatal(err)
	}
	drainTurn(turn)

	var interaction *models.AgentInteraction
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		open, err := tx.ListOpenAgentInteractions(conversation.ID)
		if err != nil || len(open) != 1 || open[0].InteractionKind != models.InteractionTakeOver || open[0].ToolName != "browser" {
			t.Fatalf("the request is kept open as a take-over card: %+v %v", open, err)
		}
		interaction = open[0]
		if isClaimed, err := tx.ClaimAgentInteraction(interaction.ID, models.InteractionHandedBack+"\nSigned in"); err != nil || !isClaimed {
			t.Fatalf("handing back claims it: %v %v", isClaimed, err)
		}
	})
	resumed, err := worker.ResumeInteraction(&ResumeSettings{Interaction: interaction, Answer: models.InteractionHandedBack + "\nSigned in", Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation}, "[chat about it]")
	if err != nil || resumed == nil {
		t.Fatalf("ResumeInteraction: %v", err)
	}
	drainTurn(resumed)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		messages, _ := tx.ListAgentMessages(conversation.ID, nil)
		var isCarried bool
		for _, message := range messages {
			if message.Role == "user" && strings.HasPrefix(message.Content, AnsweringMarker+" Sign in to the bank") && strings.Contains(message.Content, "handed it back after your turn had ended. They said: Signed in") {
				isCarried = true
			}
		}
		if !isCarried {
			t.Fatalf("a new turn carries the request and the hand-back on: %+v", messages)
		}
	})
}

// An extension too old to stream the tab cannot be handed over, and the
// agent is told so rather than left waiting on a card nobody can act on.
func TestAskingToTakeOverAnOldExtensionsTabIsRefused(t *testing.T) {
	tool, worker, browser, run := browserOnATab(t, "drawer")
	worker.SetBrowserCanCast("a1", browser, false)
	if _, err := callBrowser(tool, run, `{"action":"ask_to_take_over","text":"Sign in to the bank"}`); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("asking with an old extension: %v", err)
	}
}
