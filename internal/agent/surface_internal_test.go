package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
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
		{"voice", true, "listening, not reading", true},
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
	// Spoken turns are answered for the ear, and their transcription is not
	// taken on trust.
	voice := surfaceOf("voice")
	for _, said := range []string{"No tables", "the answer first", "misheard"} {
		if !strings.Contains(voice.overlay+voice.situationLine, said) {
			t.Errorf("voice says %q: %q", said, voice.overlay)
		}
	}
	// Only the dashboard's own drawer can be moved to a page of it.
	for _, name := range []string{"drawer", "phone", "voice"} {
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

// After a spoken answer the person cut in on, the model reads how much of
// it was heard before what they said; otherwise their words as they are.
func TestPersonTextSaysHowMuchOfAnInterruptedAnswerWasHeard(t *testing.T) {
	plain := &AskSettings{Message: "And on Friday?"}
	if got := personText(plain); got != "And on Friday?" {
		t.Fatalf("no interruption: %q", got)
	}
	interrupted := &AskSettings{Message: "And on Friday?", InterruptedAnswer: &InterruptedAnswer{
		HeardText:   "Tomorrow you have a dentist at nine.",
		UnheardText: "Then lunch at noon.",
	}}
	got := personText(interrupted)
	for _, part := range []string{"<interrupted>", `Heard: "Tomorrow you have a dentist at nine."`, `Not heard: "Then lunch at noon."`} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
	if !strings.HasSuffix(got, "\n\nAnd on Friday?") {
		t.Fatalf("their own words come last: %q", got)
	}
	// Everything was heard: nothing to say.
	whole := &AskSettings{Message: "Thanks", InterruptedAnswer: &InterruptedAnswer{HeardText: "Done."}}
	if got := personText(whole); got != "Thanks" {
		t.Fatalf("nothing unheard: %q", got)
	}
}

// A spoken turn is answered with the operator's model for calls, where one
// is chosen; a typed turn never is.
func TestASpokenTurnUsesTheCallModel(t *testing.T) {
	configuration := &config.Configuration{}
	if got := voiceModel(configuration, &AskSettings{Surface: "voice"}); got != "" {
		t.Fatalf("none chosen: %q", got)
	}
	configuration.Agent.Voice.AskModel = " spoken:small "
	if got := voiceModel(configuration, &AskSettings{Surface: "voice"}); got != "spoken:small" {
		t.Fatalf("chosen: %q", got)
	}
	if got := voiceModel(configuration, &AskSettings{Surface: "drawer"}); got != "" {
		t.Fatalf("a typed turn: %q", got)
	}
}
