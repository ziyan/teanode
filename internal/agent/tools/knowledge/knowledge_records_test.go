package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/models"
)

// A records folder is one with a script to print its records, a refresh
// script to fill it, or records in it; an export's own folder is none of
// those, and pointing records at it reads nothing.
func TestARecordsFolderHasAScriptOrRecords(t *testing.T) {
	cases := []struct {
		name    string
		entries []computer.Entry
		want    bool
	}{
		{"a refresh", []computer.Entry{{Name: "refresh"}, {Name: ".refresh.log"}}, true},
		{"a records script", []computer.Entry{{Name: "records", Kind: "file"}}, true},
		{"records", []computer.Entry{{Name: "engineering.jsonl"}, {Name: "sales.ndjson"}}, true},
		{"an export", []computer.Entry{{Name: "pages"}, {Name: "comments"}, {Name: "spaces.json"}, {Name: "users.json"}}, false},
		// A folder of folders is what an export looks like, and one of
		// them being called records does not make its parent one.
		{"a folder called records", []computer.Entry{{Name: "records", Kind: "directory"}, {Name: "users.json"}}, false},
		{"nothing", nil, false},
	}
	for _, testCase := range cases {
		if got := recordsFolderLooksReady(testCase.entries); got != testCase.want {
			t.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// listingRun is a person with one computer attached and nothing else: the
// two questions lookBeforeAdding asks are all it has to answer.
type listingRun struct {
	tools.Run
	computer tools.Computer
}

func (self *listingRun) AttachedComputers() []tools.Computer { return []tools.Computer{self.computer} }
func (self *listingRun) ComputersAllowed() bool              { return true }
func (self *listingRun) ComputersUnattended() bool           { return false }

// listingComputer answers the probe with an empty page, the way a daemon
// over an allowed folder does, and the listing with whatever the folder
// is meant to hold.
type listingComputer struct {
	entries []computer.Entry
}

func (self *listingComputer) Name() string        { return "gen7" }
func (self *listingComputer) System() string      { return "linux" }
func (self *listingComputer) Home() string        { return "/home/person" }
func (self *listingComputer) Description() string { return "" }

func (self *listingComputer) Ask(ctx context.Context, action string, arguments any, wait time.Duration) (json.RawMessage, error) {
	switch action {
	case "scan":
		return json.Marshal(&computer.ScanResult{})
	case "filesystem":
		return json.Marshal(struct {
			Entries []computer.Entry `json:"entries"`
		}{Entries: self.entries})
	}
	return nil, fmt.Errorf("no action %q here", action)
}

// The check before a source is made accepts a folder holding only the
// script that prints its records. Without this, the one shape that costs
// the person no disk at all was the one shape the tool refused to add.
func TestAddingAcceptsAFolderWithOnlyARecordsScript(t *testing.T) {
	run := &listingRun{computer: &listingComputer{entries: []computer.Entry{
		{Name: "records", Kind: "file"},
	}}}
	if err := lookBeforeAdding(context.Background(), run, "gen7", "~/chat-records", models.FormatRecords); err != nil {
		t.Fatalf("a folder with a records script is a records folder: %s", err)
	}

	// And the export it reads is still not one, with what to do instead.
	export := &listingRun{computer: &listingComputer{entries: []computer.Entry{
		{Name: "posts", Kind: "directory"}, {Name: "users.json", Kind: "file"},
	}}}
	err := lookBeforeAdding(context.Background(), export, "gen7", "~/chat-archive", models.FormatRecords)
	if err == nil {
		t.Fatalf("an export is not a records folder")
	}
	if !strings.Contains(err.Error(), "records script") || !strings.Contains(err.Error(), "nothing is copied") {
		t.Fatalf("and the way in is a script that reads it where it lies: %s", err)
	}
}
