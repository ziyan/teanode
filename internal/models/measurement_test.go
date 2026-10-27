package models

import "testing"

// A chosen system wins over any language. When none is chosen only a region
// can make it US: a language alone says nothing about where somebody is.
func TestUsesUSUnits(t *testing.T) {
	for _, scenario := range []struct {
		measurementSystem, locale string
		isUS                      bool
	}{
		{"metric", "en-US", false},
		{"us", "ja-JP", true},
		{"us", "", true},
		{"", "en-US", true},
		{"", "en_US.UTF-8", true},
		{"", "en_US.UTF-8@euro", true},
		{"", "zh-Hant-US", true},
		{"", "es-LR", true},
		{"", "en-GB", false},
		{"", "ja-JP", false},
		{"", "en", false},
		{"", "US", false},
		{"", "", false},
		{"", "not a locale", false},
	} {
		if got := UsesUSUnits(scenario.measurementSystem, scenario.locale); got != scenario.isUS {
			t.Errorf("UsesUSUnits(%q, %q) = %v, expected %v", scenario.measurementSystem, scenario.locale, got, scenario.isUS)
		}
	}
}

func TestAUserReadsTheirChosenLanguageThenTheOneTheirBrowserSaid(t *testing.T) {
	user := &User{Locale: "en-GB", LocaleSeen: "en-US"}
	if user.UsesUSUnits() {
		t.Error("the language they chose has no US region, but they read US units")
	}
	user.Locale = ""
	if !user.UsesUSUnits() {
		t.Error("their browser said a US language and they chose none, but they read metric")
	}
	if (*User)(nil).UsesUSUnits() {
		t.Error("nobody reads US units")
	}
}

func TestTheUnitsConvert(t *testing.T) {
	if got := FahrenheitFromCelsius(100); got != 212 {
		t.Errorf("100 degrees Celsius is %v", got)
	}
	if got := FahrenheitFromCelsius(-40); got != -40 {
		t.Errorf("-40 degrees Celsius is %v", got)
	}
	if got := MilesFromMeters(1609.344); got != 1 {
		t.Errorf("1609.344 meters is %v miles", got)
	}
}
