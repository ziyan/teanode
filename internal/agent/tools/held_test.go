package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// heldRun is a run with a database and an agent, which is all a held result
// needs of one.
type heldRun struct {
	fakeRun
	database db.Database
	agent    *models.Agent
}

func (self *heldRun) Database() db.Database { return self.database }
func (self *heldRun) Agent() *models.Agent  { return self.agent }

// A result within a page comes back as it is.
func TestAShortResultIsNotPaged(t *testing.T) {
	run := &heldRun{agent: &models.Agent{ID: "agent-one"}}
	if got := PageResult(context.Background(), run, "short", false, false); got != "short" {
		t.Fatalf("got %q", got)
	}
}

// A long result is shown a page at a time, never cut inside a character, the
// first page says how long the whole is, and reading on to the end gives back
// every character of it.
func TestALongResultIsReadOnToTheEnd(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	run := &heldRun{database: database, agent: &models.Agent{ID: "agent-one"}}
	var builder strings.Builder
	for lineNumber := 0; builder.Len() < 3*ResultCharacters; lineNumber++ {
		fmt.Fprintf(&builder, "line %d: café ☕ résumé\n", lineNumber)
	}
	whole := builder.String()

	page := PageResult(context.Background(), run, whole, true, false)
	more := regexp.MustCompile(`\n\[the result is (\d+) characters in all and (\d+) more follow: call result_more with result_id "([0-9a-f]+)" and offset (\d+)\]$`)
	found := more.FindStringSubmatch(page)
	if found == nil {
		t.Fatalf("the first page does not say how to read on: ...%q", page[max(0, len(page)-200):])
	}
	if total, _ := strconv.Atoi(found[1]); total != utf8.RuneCountInString(whole) {
		t.Errorf("the page says the result is %d characters, it is %d", total, utf8.RuneCountInString(whole))
	}
	if len(page) > ResultCharacters {
		t.Errorf("the first page is %d bytes, past a page of %d", len(page), ResultCharacters)
	}
	read := strings.TrimSuffix(page, found[0])
	id, offset := found[3], found[4]
	for pages := 0; offset != ""; pages++ {
		if pages > 20 {
			t.Fatal("reading on does not end")
		}
		number, _ := strconv.Atoi(offset)
		if number != len(read) {
			t.Fatalf("the next offset is %d, but %d bytes have been read", number, len(read))
		}
		next, isUntrusted, err := ReadResultMore(context.Background(), run, id, number)
		if err != nil {
			t.Fatal(err)
		}
		if !isUntrusted {
			t.Error("a page of a result from outside lost its mark")
		}
		if !utf8.ValidString(next) {
			t.Fatal("a page was cut inside a character")
		}
		tail := regexp.MustCompile(`\n\[(\d+) more characters: call result_more with result_id "[0-9a-f]+" and offset (\d+)\]$`).FindStringSubmatch(next)
		offset = ""
		if tail != nil {
			next, offset = strings.TrimSuffix(next, tail[0]), tail[2]
		}
		read += next
	}
	if read != whole {
		t.Fatalf("reading on gave %d bytes, want %d", len(read), len(whole))
	}
}

// Another agent's id reads nothing, and an offset outside the result says so.
func TestAHeldResultIsOnlyReadByItsAgent(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	run := &heldRun{database: database, agent: &models.Agent{ID: "agent-one"}}
	page := PageResult(context.Background(), run, strings.Repeat("x", 3*ResultCharacters), false, false)
	id := regexp.MustCompile(`result_id "([0-9a-f]+)"`).FindStringSubmatch(page)[1]

	other := &heldRun{database: database, agent: &models.Agent{ID: "agent-two"}}
	if _, _, err := ReadResultMore(context.Background(), other, id, 0); err == nil {
		t.Error("another agent read a held result")
	}
	if _, _, err := ReadResultMore(context.Background(), run, id, 10*ResultCharacters); err == nil {
		t.Error("an offset past the end was accepted")
	}
}

// A result that cannot be held says so rather than promising a way on.
func TestAResultThatCannotBeHeldSaysSo(t *testing.T) {
	run := &heldRun{agent: &models.Agent{ID: "agent-one"}}
	page := PageResult(context.Background(), run, strings.Repeat("x", 3*ResultCharacters), false, false)
	if !strings.Contains(page, "too many to hold for later") || strings.Contains(page, "result_id") {
		t.Fatalf("got ...%q", page[max(0, len(page)-250):])
	}
}
