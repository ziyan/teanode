package dav_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// propfindSync asks a calendar for what a phone that syncs with
// sync-collection asks it for, and nothing else.
const propfindSync = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:supported-report-set/><d:sync-token/></d:prop></d:propfind>`

var syncTokenElement = regexp.MustCompile(`<sync-token[^>]*>([^<]+)</sync-token>`)

func (self *world) syncToken(t *testing.T) string {
	t.Helper()
	answer := self.ask(t, "PROPFIND", self.calendarPath(), propfindSync, "Depth", "0")
	body := text(t, answer)
	if answer.StatusCode != http.StatusMultiStatus {
		t.Fatalf("asking for the sync-token: %d %s", answer.StatusCode, body)
	}
	if strings.Contains(body, "404") || !strings.Contains(body, "sync-collection") {
		t.Fatalf("a calendar answers its sync-token and its reports, and nothing is missing:\n%s", body)
	}
	found := syncTokenElement.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("no sync-token:\n%s", body)
	}
	return found[1]
}

func (self *world) syncCollection(t *testing.T, token string) (int, string) {
	t.Helper()
	answer := self.ask(t, "REPORT", self.calendarPath(), `<?xml version="1.0"?>
		<d:sync-collection xmlns:d="DAV:"><d:sync-token>`+token+`</d:sync-token><d:sync-level>1</d:sync-level>
		<d:prop><d:getetag/></d:prop></d:sync-collection>`, "Depth", "0")
	return answer.StatusCode, text(t, answer)
}

// A phone that syncs with sync-collection is given a token, every event with
// no token, nothing with the current token, and told to start again with a
// token from before a change.
func TestAPhoneSyncsACalendarWithSyncCollection(t *testing.T) {
	here, done := newWorld(t)
	defer done()

	empty := here.syncToken(t)
	here.putEvent(t, "one", anEvent)
	current := here.syncToken(t)
	if current == empty {
		t.Fatalf("an event added changes the token: %s", current)
	}

	status, body := here.syncCollection(t, "")
	if status != http.StatusMultiStatus || !strings.Contains(body, "one.ics") || !strings.Contains(body, current) {
		t.Fatalf("with no token, every event and the current token: %d %s", status, body)
	}

	status, body = here.syncCollection(t, current)
	if status != http.StatusMultiStatus || strings.Contains(body, "one.ics") || !strings.Contains(body, current) {
		t.Fatalf("with the current token, nothing has changed: %d %s", status, body)
	}

	status, body = here.syncCollection(t, empty)
	if status != http.StatusForbidden || !strings.Contains(body, "valid-sync-token") {
		t.Fatalf("with a token from before the change, start again: %d %s", status, body)
	}

	answer := here.ask(t, http.MethodDelete, here.eventPath("one"), "")
	_ = text(t, answer)
	if after := here.syncToken(t); after == current {
		t.Fatalf("a deletion changes the token: %s", after)
	}
}
