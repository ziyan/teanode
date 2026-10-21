package agent

import (
	"testing"
	"time"
)

// followSubagent says whether the run called a private tool, so that the
// subagent tool can tell the parent turn: listing, reading a note and making
// up a password in Passwords are, its other actions are not, and Health
// always is.
func TestFollowSubagentSaysWhetherAPrivateToolWasCalled(t *testing.T) {
	for name, testCase := range map[string]struct {
		calls []Event
		want  bool
	}{
		"no tool called":           {nil, false},
		"another tool":             {[]Event{{Kind: EventToolCall, Tool: "mail_search", Arguments: `{"action":"list"}`}}, false},
		"another passwords action": {[]Event{{Kind: EventToolCall, Tool: "passwords", Arguments: `{"action":"status"}`}}, false},
		"listing passwords":        {[]Event{{Kind: EventToolCall, Tool: "mail_search"}, {Kind: EventToolCall, Tool: "passwords", Arguments: `{"action":"list"}`}}, true},
		"reading a note":           {[]Event{{Kind: EventToolCall, Tool: "passwords_read_note"}}, true},
		"health":                   {[]Event{{Kind: EventToolCall, Tool: "health", Arguments: `{"action":"summary"}`}}, true},
	} {
		run := batchRun()
		for _, event := range testCase.calls {
			run.emit(event)
		}
		run.emit(Event{Kind: EventMessage, Text: "an invented answer"})
		run.mutex.Lock()
		run.finished = true
		run.mutex.Unlock()

		answer, calls, hasPrivateCall, err := followSubagent(t.Context(), run, "run-conversation", time.Second)
		run.cancel()
		if err != nil || answer != "an invented answer" || calls != len(testCase.calls) {
			t.Errorf("%s: followSubagent = %q, %d, %v", name, answer, calls, err)
		}
		if hasPrivateCall != testCase.want {
			t.Errorf("%s: hasPrivateCall = %v, want %v", name, hasPrivateCall, testCase.want)
		}
	}
}
