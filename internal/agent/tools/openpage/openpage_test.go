package openpage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/openpage"
)

func TestPagePath(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ written, want string }{
		{"/settings/knowledge/people/some-person", "/settings/knowledge/people/some-person"},
		{"/settings/knowledge/people/zoë/", "/settings/knowledge/people/zoë"},
		{"/mailbox", "/mailbox"},
		{"/mailbox/starred/item42", "/mailbox/starred/item42"},
		{"/settings/agent/instructions", "/settings/agent/instructions"},
		{"/settings/preference", "/settings/preference"},
		{"memory:projects/example-app#2", "/settings/knowledge/projects/example-app"},
		{"mail:item42", "/mailbox/starred/item42"},
	} {
		got, err := openpage.PagePath(testCase.written)
		if err != nil || got != testCase.want {
			t.Errorf("%q: got %q, %v; want %q", testCase.written, got, err, testCase.want)
		}
	}
	for _, written := range []string{
		"", "/", "https://example.net/settings", "//example.net/settings", "javascript:alert(1)",
		"settings/agent", "/server/about", "/access/users", "/domains", "/mail/abc",
		"/settings/../server", "/settings/./agent", "/settings//agent", "/mailbox?search=x",
		"/mailbox#top", "/settings/agent/a b", "/settings\\agent", "/settingsx", "memory:../server", "mail:a/b",
		"/settings/" + strings.Repeat("a", openpage.PathLength),
	} {
		if got, err := openpage.PagePath(written); err == nil {
			t.Errorf("%q was taken as %q", written, got)
		}
	}
}

// showingRun is a run in the dashboard, which keeps the pages it was asked
// to show.
type showingRun struct {
	tools.Run
	shown []string
}

func (self *showingRun) ShowPage(path string) { self.shown = append(self.shown, path) }

// elsewhereRun is a run anywhere else.
type elsewhereRun struct{ tools.Run }

func call(t *testing.T, run tools.Run, arguments string) (*tools.Result, error) {
	t.Helper()
	var tool *tools.Tool
	for _, candidate := range tools.Build().All() {
		if candidate.Name == "open_page" {
			tool = candidate
		}
	}
	if tool == nil {
		t.Fatal("open_page is not registered")
	}
	if tool.Risk != tools.RiskRead || !tool.DashboardOnly {
		t.Fatalf("open_page reads and is the dashboard's alone: %q, %v", tool.Risk, tool.DashboardOnly)
	}
	return tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_1", Arguments: json.RawMessage(arguments)})
}

func TestOpenPageShowsAPageOfTheDashboard(t *testing.T) {
	t.Parallel()

	run := &showingRun{}
	result, err := call(t, run, `{"path":"/settings/knowledge/people/some-person","reason":"the corrected page"}`)
	if err != nil {
		t.Fatalf("open_page: %s", err)
	}
	if len(run.shown) != 1 || run.shown[0] != "/settings/knowledge/people/some-person" {
		t.Fatalf("shown %q", run.shown)
	}
	if result.Note != "Opened /settings/knowledge/people/some-person: the corrected page" {
		t.Errorf("note %q", result.Note)
	}

	if _, err := call(t, run, `{"path":"https://example.net/"}`); err == nil {
		t.Error("an address off the dashboard was shown")
	}
	if len(run.shown) != 1 {
		t.Errorf("a refused path still moved the dashboard: %q", run.shown)
	}

	if _, err := call(t, &elsewhereRun{}, `{"path":"/mailbox"}`); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("outside the dashboard: %v", err)
	}
}
