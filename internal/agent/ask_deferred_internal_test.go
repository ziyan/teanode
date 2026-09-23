package agent

import (
	"reflect"
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
