package agent

import (
	"strings"
	"testing"
)

// Each surface says where the turn is, how to write for it, and whether
// it draws suggested replies, from its one entry.
func TestSurfaces(t *testing.T) {
	for _, testCase := range []struct {
		name                string
		hasSituationLine    bool
		overlayHas          string
		hasSuggestedReplies bool
	}{
		{"drawer", true, "Markdown renders", true},
		{"phone", true, "no tables", true},
		{"cli", true, "A terminal", false},
		{"api", true, "A terminal", false},
		{"mail", true, "first line is its subject", false},
		{"telegram", true, "Telegram", false},
		{"discord", true, "Discord", false},
		{speakFirstSurfacePrefix + "morning", true, "Markdown renders", true},
		{backgroundSurface, true, "", false},
		{"schedule", true, "", false},
		{"", false, "", false},
	} {
		found := surfaceOf(testCase.name)
		if (found.situationLine != "") != testCase.hasSituationLine {
			t.Errorf("%q: situation line %q", testCase.name, found.situationLine)
		}
		if testCase.overlayHas == "" && found.overlay != "" || !strings.Contains(found.overlay, testCase.overlayHas) {
			t.Errorf("%q: overlay %q, want it to hold %q", testCase.name, found.overlay, testCase.overlayHas)
		}
		if found.hasSuggestedReplies != testCase.hasSuggestedReplies {
			t.Errorf("%q: suggested replies %v", testCase.name, found.hasSuggestedReplies)
		}
	}
	// A chat app cannot draw a table, and its links to a message or a page
	// open in the dashboard.
	for _, name := range []string{"telegram", "discord"} {
		if overlay := surfaceOf(name).overlay; !strings.Contains(overlay, "no tables") || !strings.Contains(overlay, "opens in the dashboard") {
			t.Errorf("%s: %q", name, overlay)
		}
	}
	// Only the dashboard's own drawer can be moved to a page of it.
	for _, name := range []string{"drawer", "phone"} {
		if !surfaceOf(name).canShowPages {
			t.Errorf("%s cannot show a page", name)
		}
	}
	for _, name := range []string{"extension", "telegram", "discord", "cli", "api", "mail", "mcp", "schedule", backgroundSurface, speakFirstSurfacePrefix + "morning", ""} {
		if surfaceOf(name).canShowPages {
			t.Errorf("%q can show a page", name)
		}
	}
}

// The prompt names a tool only where the round has it: said plainly when
// it is sent, with how to load it when it waits behind tool_search, and
// not at all when it is not offered. Nothing is said of tool_search when
// nothing waits behind it.
func TestThePromptNamesOnlyTheToolsTheRoundHas(t *testing.T) {
	base := map[string]any{
		"AgentName": "Bertie", "PersonName": "Alice", "ServerName": "mail.example.org",
		"Language": "English", "Situation": "Alice, somewhere.", "ThisMonth": "2026/09",
	}
	for _, short := range []bool{false, true} {
		render := func(tools map[string]string, deferred []string) string {
			data := map[string]any{"Short": short, "Tools": tools, "Deferred": deferred}
			for key, value := range base {
				data[key] = value
			}
			text, err := render("ask.txt", data)
			if err != nil {
				t.Fatalf("render: %s", err)
			}
			return text
		}

		bare := render(map[string]string{}, nil)
		for _, absent := range []string{"`tool_search`", "`knowledge`", "`memory`", "`conversation`", "`note`", "`web_search`", "`subscription`", "`schedule`", "tables only"} {
			if strings.Contains(bare, absent) {
				t.Errorf("short=%v: a round without the tool names %s", short, absent)
			}
		}

		full := render(map[string]string{"memory": "sent", "conversation": "sent", "knowledge": "sent"}, []string{"calendar — a calendar"})
		for _, present := range []string{"`tool_search`", "`knowledge`", "`memory`", "`conversation`"} {
			if !strings.Contains(full, present) {
				t.Errorf("short=%v: a round with it should name %s", short, present)
			}
		}
		if strings.Contains(full, "load it with `tool_search`") || strings.Contains(full, "loading it with `tool_search`") {
			t.Errorf("short=%v: a sent tool is not to be loaded", short)
		}

		waiting := render(map[string]string{"memory": "sent", "knowledge": "deferred"}, []string{"knowledge — their sources"})
		if !strings.Contains(waiting, "`knowledge`") || !strings.Contains(waiting, "`tool_search`") {
			t.Errorf("short=%v: a deferred tool is named with how to load it", short)
		}
	}
}
