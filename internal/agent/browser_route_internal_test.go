package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// While the person's browser is connected, a browser call that names no
// target goes to it, tab attached or not, and going to a page with no tab
// open yet opens one there: a cart filled in the headless browser is a
// session they never see.
func TestABrowserCallGoesToTheConnectedBrowser(t *testing.T) {
	tool := FullCatalog().Get("browser")
	if tool == nil {
		t.Skip("the browser tool is not built into this catalog")
	}
	configuration := config.Default()
	configuration.Agent.Enabled = true
	worker := &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}
	browser := &fakeTab{agent: worker, agentId: "a1", answers: func(string, json.RawMessage) (bool, string) {
		return true, `{"title":"Caramels","url":"https://shop.example.org/caramels"}`
	}}
	worker.ConnectBrowser("a1", browser, "", "", false)
	run := &AskRun{agent: worker, settings: &AskSettings{Agent: &models.Agent{ID: "a1"}, Surface: "drawer"}, ctx: context.Background()}

	if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_1", Arguments: json.RawMessage(`{"action":"navigate","url":"https://shop.example.org/caramels"}`)}); err != nil {
		t.Fatalf("the call should have gone to the person's browser: %s", err)
	}
	if len(browser.sent) != 1 || browser.sent[0] != "open" {
		t.Fatalf("going to a page with no tab open should open one in their browser, sent %v", browser.sent)
	}
}
