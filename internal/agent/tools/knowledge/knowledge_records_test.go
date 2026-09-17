package knowledge

import (
	"testing"

	"github.com/ziyan/teanode/internal/computer"
)

// A records folder is one with a refresh script or records in it; an
// export's own folder is neither, and pointing records at it reads nothing.
func TestARecordsFolderHasAScriptOrRecords(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    bool
	}{
		{"a script", []string{"refresh", ".refresh.log"}, true},
		{"records", []string{"engineering.jsonl", "sales.ndjson"}, true},
		{"an export", []string{"pages", "comments", "spaces.json", "users.json"}, false},
		{"nothing", nil, false},
	}
	for _, testCase := range cases {
		entries := make([]computer.Entry, 0, len(testCase.entries))
		for _, name := range testCase.entries {
			entries = append(entries, computer.Entry{Name: name})
		}
		if got := recordsFolderLooksReady(entries); got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}
