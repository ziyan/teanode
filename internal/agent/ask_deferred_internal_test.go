package agent

import (
	"reflect"
	"strings"
	"testing"
)

// A skill's tools not loaded are one line in the prompt, naming each of
// them, and any other tool keeps a line of its own.
func TestDeferredSkillToolsShareALine(t *testing.T) {
	lines := deferredCatalog([]*Tool{
		{Name: "calendar", Description: "The person's calendar. More about it."},
		{Name: "skill__weather__get_weather", Description: "Current weather."},
		{Name: "skill__code_host__list_issues", Description: "Issues."},
		{Name: "skill__weather__get_forecast", Description: "A forecast."},
	})
	wanted := []string{
		"calendar — The person's calendar.",
		"skill__weather__* — the weather skill: get_weather, get_forecast",
		"skill__code_host__* — the code_host skill: list_issues",
	}
	if !reflect.DeepEqual(lines, wanted) {
		t.Errorf("the catalog was %q", lines)
	}
}

// A tool not loaded is one short line, however long its first sentence is:
// the definition arrives with tool_search, and the line is only there so
// the model knows the tool exists.
func TestDeferredLineIsShort(t *testing.T) {
	lines := deferredCatalog([]*Tool{
		{Name: "finance", Description: "The person's money: " + strings.Repeat("their accounts, cards, loans and brokerages, ", 12) + "and more. Another sentence."},
		{Name: "calendar", Description: "The person's calendar."},
	})
	if len([]rune(lines[0])) > deferredLineCharacters || !strings.HasPrefix(lines[0], "finance — The person's money: their accounts") || !strings.HasSuffix(lines[0], "…") {
		t.Errorf("a long first sentence was not shortened at a word: %q", lines[0])
	}
	if lines[1] != "calendar — The person's calendar." {
		t.Errorf("a short one was changed: %q", lines[1])
	}
}
