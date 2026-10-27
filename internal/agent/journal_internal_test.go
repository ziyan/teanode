package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// journalProvider is a model that answers every prompt with what answer says
// of it, and keeps the prompts as the model was sent them: the last message of
// the person in each request, as the text it holds.
func journalProvider(t *testing.T, answer func(prompt string) string) (*httptest.Server, func() []string) {
	t.Helper()
	var mutex sync.Mutex
	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var asked struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &asked)
		prompt := ""
		for _, message := range asked.Messages {
			if message.Role != "user" {
				continue
			}
			var text string
			if err := json.Unmarshal(message.Content, &text); err != nil {
				var parts []struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(message.Content, &parts)
				for _, part := range parts {
					text += part.Text
				}
			}
			prompt = text
		}
		mutex.Lock()
		prompts = append(prompts, prompt)
		mutex.Unlock()
		content, _ := json.Marshal(answer(prompt))
		if asked.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{}}\n\ndata: [DONE]\n\n", content)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{}}`, content)
	}))
	t.Cleanup(provider.Close)
	return provider, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), prompts...)
	}
}

// journalFixture is Alice, who keeps a journal, her agent, and a model that
// answers with a plain page for any day or period.
type journalFixture struct {
	database db.Database
	worker   *Agent
	run      *Run
	prompts  func() []string
}

const plainPage = `{"headline": "A quiet day", "narrative": "You wrote a note.", "choices": [], "coverPhotoId": ""}`

func newJournalFixture(t *testing.T, answer func(prompt string) string, permissions ...models.Permission) *journalFixture {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	if answer == nil {
		answer = func(string) string { return plainPage }
	}
	provider, prompts := journalProvider(t, answer)
	worker, run := digestSplitWorldWith(t, database, provider.URL, nil)
	if len(permissions) == 0 {
		permissions = []models.Permission{models.PermissionJournalUse, models.PermissionAgentUse}
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, run.Owner.ID, permissions...)
	})
	return &journalFixture{database: database, worker: worker, run: run, prompts: prompts}
}

// dayAgo is the day count days before today, in Alice's calendar.
func (self *journalFixture) dayAgo(count int) string {
	day, err := journal.AddDays(journal.Today(self.run.Owner, time.Now()), -count)
	if err != nil {
		panic(err)
	}
	return day
}

// words stores what Alice wrote on a day, which also makes the day.
func (self *journalFixture) words(t *testing.T, day, words string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		// A save names the time the words were last saved, once there are
		// words to save over.
		var base *time.Time
		if existing, err := tx.GetJournalDay(self.run.Owner.ID, day); err == nil {
			base = existing.WordsModifiedAt
		}
		if _, err := tx.SetJournalWords(self.run.Owner.ID, day, journal.Zone(self.run.Owner).String(), words, base, time.Now()); err != nil {
			t.Fatalf("SetJournalWords: %s", err)
		}
	})
}

func (self *journalFixture) day(t *testing.T, day string) *models.JournalDay {
	t.Helper()
	var found *models.JournalDay
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if found, err = tx.GetJournalDay(self.run.Owner.ID, day); err != nil {
			t.Fatalf("GetJournalDay %s: %s", day, err)
		}
	})
	return found
}

func (self *journalFixture) period(t *testing.T, periodKind models.JournalPeriodKind, first string) *models.JournalPeriod {
	t.Helper()
	var found *models.JournalPeriod
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if found, err = tx.GetJournalPeriod(self.run.Owner.ID, periodKind, first); err != nil {
			t.Fatalf("GetJournalPeriod %s %s: %s", periodKind, first, err)
		}
	})
	return found
}

// dayPrompts and periodPrompts are the prompts asked so far that wrote a day
// and that wrote a week, a month or a year.
func (self *journalFixture) dayPrompts() []string {
	return promptsContaining(self.prompts(), "'s day, ")
}

func (self *journalFixture) periodPrompts() []string {
	var found []string
	for _, prompt := range self.prompts() {
		if strings.Contains(prompt, "Write the journal page for") && !strings.Contains(prompt, "'s day, ") {
			found = append(found, prompt)
		}
	}
	return found
}

func promptsContaining(prompts []string, text string) []string {
	var found []string
	for _, prompt := range prompts {
		if strings.Contains(prompt, text) {
			found = append(found, prompt)
		}
	}
	return found
}

// asking is a journalAsk that answers what answer says of each prompt, and
// keeps them.
type asking struct {
	prompts []string
	answer  func(call int, prompt string) string
}

func (self *asking) ask(_ context.Context, _, prompt string) (string, error) {
	self.prompts = append(self.prompts, prompt)
	return self.answer(len(self.prompts), prompt), nil
}
