package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A research run fetches only an address a web_search in the same run
// returned, however the address is spelled, and nothing it or a page made up.
func TestResearchRunFetchesOnlyWhatASearchReturned(t *testing.T) {
	configuration := config.Default()
	worker := &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}
	var fetched []string
	search := &Tool{Name: "web_search", Risk: tools.RiskRead, Run: func(context.Context, *tools.Call) (*tools.Result, error) {
		return &tools.Result{Content: `{"results":[]}`, SearchedAddresses: []string{"https://Docs.example.org/guide/", "https://news.example.net/story?id=7"}}, nil
	}}
	fetch := &Tool{Name: "web_fetch", Risk: tools.RiskRead, Run: func(_ context.Context, call *tools.Call) (*tools.Result, error) {
		fetched = append(fetched, string(call.Arguments))
		return tools.TextResult("a page"), nil
	}}
	offered := []*Tool{search, fetch}
	newTurn := func(isLimited bool) *AskRun {
		return &AskRun{agent: worker, settings: &AskSettings{
			Agent: &models.Agent{ID: "agent01"}, Owner: &models.User{Username: "robin"},
			Conversation: &models.AgentConversation{ID: "conversation01"}, Headless: true, IsFetchLimitedToSearched: isLimited,
		}}
	}
	call := func(turn *AskRun, name, arguments string) string {
		return turn.runTool(context.Background(), configuration, offered, nil, llm.ToolCall{ID: "call01", Name: name, Arguments: arguments}).content
	}

	turn := newTurn(true)
	if said := call(turn, "web_fetch", `{"url":"https://docs.example.org/guide"}`); !strings.Contains(said, "web_search") || len(fetched) != 0 {
		t.Fatalf("before any search nothing is fetched: %q, %v", said, fetched)
	}
	call(turn, "web_search", `{"query":"example guide"}`)
	for _, address := range []string{"https://docs.example.org/guide", "https://docs.example.org/guide/", "HTTPS://DOCS.example.org/guide#top", "https://news.example.net/story?id=7"} {
		if said := call(turn, "web_fetch", `{"url":"`+address+`"}`); said != "a page" {
			t.Errorf("%s was returned by the search but refused: %q", address, said)
		}
	}
	fetched = nil
	for _, address := range []string{"https://evil.example.com/?data=secret", "https://docs.example.org/guide/other", "https://news.example.net/story?id=8", "https://docs.example.org.evil.example.com/guide", ""} {
		if said := call(turn, "web_fetch", `{"url":"`+address+`"}`); !strings.Contains(said, "error") || len(fetched) != 0 {
			t.Errorf("%q was not returned by a search but was fetched: %q", address, said)
		}
	}
	if said := call(turn, "web_fetch", `not json`); !strings.Contains(said, "error") || len(fetched) != 0 {
		t.Errorf("a call that cannot be read is refused: %q", said)
	}

	// Another turn has not searched, and one not held to this fetches freely.
	if said := call(newTurn(true), "web_fetch", `{"url":"https://docs.example.org/guide"}`); !strings.Contains(said, "error") {
		t.Errorf("a search in another turn does not count: %q", said)
	}
	if said := call(newTurn(false), "web_fetch", `{"url":"https://anywhere.example.com/"}`); said != "a page" {
		t.Errorf("a turn not held to searched addresses fetches as before: %q", said)
	}
}
