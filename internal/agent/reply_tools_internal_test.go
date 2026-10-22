package agent

import "testing"

// A drafting run reads a stranger's message with the person's mail, memory and
// diary in reach and nobody watching, so it cannot fetch a page: an address it
// builds can carry what it read to whoever runs that server.
func TestReplyRunCannotFetchAPage(t *testing.T) {
	if replyTools["web_fetch"] {
		t.Fatal("the drafting run holds web_fetch")
	}
	for _, name := range []string{"mail_read", "calendar", "memory"} {
		if !replyTools[name] {
			t.Errorf("the drafting run lost %s", name)
		}
	}
}
