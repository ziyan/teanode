package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// The answer is read strictly: the first word of a short answer that is
// one emoji, any emoji, with a heart accepted without its variation
// selector, and anything else is no reaction at all.
func TestParseAcknowledgement(t *testing.T) {
	for name, testCase := range map[string]struct {
		answer string
		want   string
	}{
		"the default":                  {"👀", "👀"},
		"with white space":             {"  👍\n", "👍"},
		"a heart":                      {"❤️", "❤️"},
		"a heart without its selector": {"❤", "❤️"},
		"every other one":              {"😂", "😂"},
		"good news":                    {"🎉", "🎉"},
		"a favor":                      {"🙏", "🙏"},
		"surprise":                     {"😮", "😮"},
		"sadness":                      {"😢", "😢"},
		"the first of two":             {"🎉 👀", "🎉"},
		"with a word around it":        {"Reaction: 👍", "👍"},
		"none":                         {"none", ""},
		"None, capitalized":            {"None.", ""},
		"nothing":                      {"", ""},
		"any other emoji":              {"🦄", "🦄"},
		"a flag":                       {"🇯🇵", "🇯🇵"},
		"a skin tone":                  {"👍🏽", "👍🏽"},
		"two emoji in one word":        {"🎉🎉", ""},
		"a word that is not emoji":     {"thumbs", ""},
		"an explanation":               {"I think a thumbs up 👍 is the right reaction to this message", ""},
	} {
		if got := parseAcknowledgement(testCase.answer); got != testCase.want {
			t.Errorf("%s: parseAcknowledgement(%q) = %q, want %q", name, testCase.answer, got, testCase.want)
		}
	}
	// Whatever it returns is something the reaction store accepts.
	for _, answer := range []string{"👀", "❤", "🦄", "🇯🇵", "👍🏽"} {
		if err := models.ValidateReactionEmoji(parseAcknowledgement(answer)); err != nil {
			t.Errorf("%q cannot be a reaction: %s", parseAcknowledgement(answer), err)
		}
	}
}

// Only a person's own message in a conversation they read, on a surface
// that draws reactions, is acknowledged.
func TestShouldAcknowledge(t *testing.T) {
	conversation := func(kind models.AgentConversationKind) *models.AgentConversation {
		return &models.AgentConversation{Kind: kind}
	}
	for name, testCase := range map[string]struct {
		settings AskSettings
		want     bool
	}{
		"the drawer, main":      {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "drawer", Message: "how am I doing this week"}, true},
		"the drawer, named":     {AskSettings{Conversation: conversation(models.AgentConversationNamed), Surface: "drawer", Message: "hi"}, true},
		"the drawer, goal":      {AskSettings{Conversation: conversation(models.AgentConversationGoal), Surface: "drawer", Message: "hi"}, true},
		"a narrow drawer":       {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "phone", Message: "hi"}, true},
		"the extension":         {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "extension", Message: "hi"}, true},
		"the app":               {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "ios", Message: "hi"}, true},
		"a run":                 {AskSettings{Conversation: conversation(models.AgentConversationRun), Surface: "drawer", Message: "hi"}, false},
		"the command line":      {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "cli", Message: "hi"}, false},
		"the api":               {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "api", Message: "hi"}, false},
		"a mail":                {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "mail", Message: "hi"}, false},
		"a call in the app":     {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "ios_voice", Message: "hi"}, false},
		"siri":                  {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "siri", Message: "hi"}, false},
		"voice":                 {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "voice", Message: "hi"}, false},
		"a schedule":            {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "schedule", Message: "hi"}, false},
		"research":              {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "research", Message: "hi"}, false},
		"a background wake":     {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: backgroundSurface, Message: "hi"}, false},
		"a headless turn":       {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "drawer", Headless: true, Message: "hi"}, false},
		"the agent speaking":    {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "drawer", Message: models.ScheduleMarker + " the weekly look"}, false},
		"an answer to a card":   {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "drawer", Message: AnsweringMarker + " Which one?\n\nThe first"}, false},
		"an approval of a card": {AskSettings{Conversation: conversation(models.AgentConversationMain), Surface: "drawer", Message: ApprovedMarker + " Send it?"}, false},
		"no conversation":       {AskSettings{Surface: "drawer", Message: "hi"}, false},
	} {
		if got := shouldAcknowledge(&testCase.settings); got != testCase.want {
			t.Errorf("%s: shouldAcknowledge = %v, want %v", name, got, testCase.want)
		}
	}
}
