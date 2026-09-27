package agent

import (
	"strings"
	"testing"
)

// The turn's prompt names the model the turn runs on, so the agent can say
// which it is; with no model known it says nothing about one.
func TestThePromptNamesTheModel(t *testing.T) {
	data := map[string]any{"AgentName": "Tea", "PersonName": "Sam", "ServerName": "mail.example.com", "Language": "English", "Short": true}
	data["Model"] = "a-provider:a-model"
	named, err := render("ask.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.SplitN(named, "\n", 2)[0], "`a-provider:a-model`") {
		t.Errorf("the model is not named in the opening line: %s", strings.SplitN(named, "\n", 2)[0])
	}
	data["Model"] = ""
	unnamed, err := render("ask.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unnamed, "runs on the model") {
		t.Error("a prompt with no model known still speaks of one")
	}
}
