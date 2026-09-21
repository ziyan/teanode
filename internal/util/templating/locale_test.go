package templating_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/templating"
)

func TestMatchLocale(t *testing.T) {
	t.Parallel()

	available := []string{"en", "zh-CN", "zh-TW", "pt-BR", "ja"}
	for name, testCase := range map[string]struct {
		requested string
		expected  string
		found     bool
	}{
		"exact":                        {"zh-TW", "zh-TW", true},
		"caseDoesNotMatter":            {"ZH-tw", "zh-TW", true},
		"underscoreIsADash":            {"zh_CN", "zh-CN", true},
		"regionFallsBackToTheLanguage": {"ja-JP", "ja", true},
		"languageTakesTheFirstRegion":  {"zh", "zh-CN", true},
		"unknownRegionTakesAnyRegion":  {"pt-PT", "pt-BR", true},
		"nothingClose":                 {"fr", "", false},
		"empty":                        {"", "", false},
	} {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actual, found := templating.MatchLocale(testCase.requested, available)
			if found != testCase.found || actual != testCase.expected {
				t.Fatalf("expected (%q, %v), got (%q, %v)", testCase.expected, testCase.found, actual, found)
			}
		})
	}
}

func TestValidLocale(t *testing.T) {
	t.Parallel()

	for locale, valid := range map[string]bool{
		"en": true, "zh-CN": true, "pt-BR": true, "sr-Latn-RS": true, "x-custom": false,
		"": false, "English": false, "zh CN": false, "zh_CN": false, "e": false,
	} {
		if templating.ValidLocale(locale) != valid {
			t.Errorf("expected ValidLocale(%q) to be %v", locale, valid)
		}
	}
}
