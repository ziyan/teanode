package toolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// searchRun is a run with a long catalog: what tool_search reads and loads,
// and nothing else.
type searchRun struct {
	tools.Run
	offered []*tools.Tool
	loaded  map[string]bool
}

func (self *searchRun) Offered() []*tools.Tool  { return self.offered }
func (self *searchRun) Loaded() map[string]bool { return self.loaded }
func (self *searchRun) Load(name string)        { self.loaded[name] = true }

func catalog() *searchRun {
	run := &searchRun{loaded: map[string]bool{}}
	run.offered = append(run.offered, &tools.Tool{Name: "mail_search", Core: true, Description: "Search the person's mail in their mailboxes"})
	run.offered = append(run.offered, &tools.Tool{Name: "folder_list", Description: "The folders of a mailbox"})
	run.offered = append(run.offered, &tools.Tool{Name: "tool_search", Core: true, Description: "Search for more tools by what they do"})
	run.offered = append(run.offered, &tools.Tool{Name: "datetime", Core: true, Description: "The date and the time in a time zone"})
	for index := 0; index < tools.DeferralThreshold; index++ {
		run.offered = append(run.offered, &tools.Tool{Name: fmt.Sprintf("other_%d", index), Description: "Something unrelated"})
	}
	return run
}

func search(t *testing.T, run *searchRun, query string) string {
	t.Helper()
	arguments, _ := json.Marshal(map[string]any{"query": query})
	result, err := runToolSearch(tools.WithRun(context.Background(), run), &tools.Call{Arguments: arguments})
	if err != nil {
		t.Fatalf("tool_search: %s", err)
	}
	return result.Content
}

// A search for a tool the model holds already says so, whether or not
// something else is loaded beside it, so that it calls the tool instead of
// searching again.
func TestASearchNamesTheToolsTheModelHasAlready(t *testing.T) {
	run := catalog()
	if content := search(t, run, "mail_search"); !strings.Contains(content, "you have mail_search already") {
		t.Fatalf("searching for a tool held: %s", content)
	}
	content := search(t, run, "search mail folders")
	if !run.loaded["folder_list"] {
		t.Fatalf("folder_list was not loaded: %s", content)
	}
	if !strings.Contains(content, `"alreadyAvailable":["mail_search"]`) {
		t.Fatalf("the tool held is not named: %s", content)
	}
}

// A tool held is named back only when the query names it: a word like "a"
// or "the" is in every description, and tool_search is never the answer.
func TestASearchDoesNotNameToolsItDidNotAskFor(t *testing.T) {
	run := catalog()
	content := search(t, run, "send a message to the team on a chat app")
	if strings.Contains(content, "already") {
		t.Errorf("a search for nothing held named tools held: %s", content)
	}
	content = search(t, run, "search tool for the mail")
	if strings.Contains(content, "tool_search") || !strings.Contains(content, "mail_search") {
		t.Errorf("named tool_search, or not the tool held: %s", content)
	}
}
