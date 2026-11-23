package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/push"
)

// A day's stack of coding questions is titled with the day in each language
// the apps are in, the same words the migration gives the cards before it.
func TestCodingStackTitleSaysTheDayInEachLanguage(t *testing.T) {
	day := time.Date(2026, time.March, 5, 8, 30, 0, 0, time.UTC)
	for language, expected := range map[string]string{
		push.LanguageEnglish:  "Coding sessions, Thu Mar 5",
		push.LanguageChinese:  "编程会话，3月5日 周四",
		push.LanguageJapanese: "コーディングセッション、3月5日(木)",
	} {
		if got := codingStackTitle(day, language); got != expected {
			t.Errorf("%s: %q, expected %q", language, got, expected)
		}
	}
}
