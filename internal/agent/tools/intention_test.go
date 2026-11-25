package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEveryToolIsOfferedAnIntentionItNeedNotGive(t *testing.T) {
	parameters := Object(map[string]any{"query": map[string]any{"type": "string"}}, "query")
	offered := WithIntention(parameters, "Japanese")
	properties := offered["properties"].(map[string]any)
	if _, isOffered := properties[IntentionArgument]; !isOffered {
		t.Fatalf("no intention offered: %v", properties)
	}
	if _, isQuery := properties["query"]; !isQuery {
		t.Fatalf("the tool's own arguments went: %v", properties)
	}
	for _, required := range offered["required"].([]string) {
		if required == IntentionArgument {
			t.Fatalf("the intention is required")
		}
	}
	if described := properties[IntentionArgument].(map[string]any)["description"].(string); !strings.Contains(described, "in Japanese") {
		t.Fatalf("the person's language is not named: %s", described)
	}
	if _, isChanged := parameters["properties"].(map[string]any)[IntentionArgument]; isChanged {
		t.Fatalf("the tool's own parameters were changed")
	}
	if WithIntention(nil, "")["properties"].(map[string]any)[IntentionArgument] == nil {
		t.Fatalf("a tool with no parameters is offered none")
	}
}

func TestTheIntentionIsTakenOffTheCall(t *testing.T) {
	stripped, intention := SplitIntention(json.RawMessage(`{"query":"boiler","intention":"  Looking for the boiler invoice. "}`))
	if string(stripped) != `{"query":"boiler"}` || intention != "Looking for the boiler invoice" {
		t.Fatalf("split into %s and %q", stripped, intention)
	}
	for _, arguments := range []string{`{"query":"boiler"}`, `[]`, `not json`, ``} {
		if stripped, intention := SplitIntention(json.RawMessage(arguments)); string(stripped) != arguments || intention != "" {
			t.Fatalf("%s came back as %s and %q", arguments, stripped, intention)
		}
	}
	// Words not fit to show are dropped, and still taken off the call.
	if stripped, intention := SplitIntention(json.RawMessage(`{"intention":7}`)); string(stripped) != `{}` || intention != "" {
		t.Fatalf("a number came back as %s and %q", stripped, intention)
	}
}

func TestOnlyAShortPlainLineIsShown(t *testing.T) {
	for _, words := range []string{
		"",
		strings.Repeat("long ", 30),
		"See https://example.com",
		"**Searching**",
		"Reading <b>mail</b>",
		"Running mail_search",
	} {
		if cleaned := CleanIntention(words); cleaned != "" {
			t.Fatalf("%q was shown as %q", words, cleaned)
		}
	}
	if cleaned := CleanIntention("Checking\nthe   weather"); cleaned != "Checking the weather" {
		t.Fatalf("folded into %q", cleaned)
	}
	if cleaned := CleanIntention("正在查看明天的天气"); cleaned != "正在查看明天的天气" {
		t.Fatalf("other languages are kept: %q", cleaned)
	}
}
