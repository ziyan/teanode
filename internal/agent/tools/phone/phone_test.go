package phone_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/phone"
)

// showingRun is a run that can move the dashboard, read in it or not, and
// keeps the pages it was asked to show.
type showingRun struct {
	tools.Run
	canShowPages bool
	shown        []string
}

func (self *showingRun) CanShowPages() bool   { return self.canShowPages }
func (self *showingRun) ShowPage(path string) { self.shown = append(self.shown, path) }

func run(t *testing.T, run tools.Run, toolName string) *tools.Result {
	t.Helper()
	for _, tool := range phone.Tools() {
		if tool.Name == toolName {
			result, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_1"})
			if err != nil {
				t.Fatalf("%s: %s", toolName, err)
			}
			return result
		}
	}
	t.Fatalf("%s is not a phone action", toolName)
	return nil
}

// In the dashboard the page opens; anywhere else (a chat app, the command
// line, Siri, a turn the agent started) the person is told where to go,
// and nothing claims a page was opened.
func TestThePagesOpenOnlyWhereTheDashboardIs(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		toolName, path, elsewhere string
	}{
		{"phone_pair", phone.ConnectPath, "Connect a phone"},
		{"phone_setup_profile", phone.SetupProfilePath, "Built-in apps"},
	} {
		dashboard := &showingRun{canShowPages: true}
		shown := run(t, dashboard, testCase.toolName)
		if len(dashboard.shown) != 1 || dashboard.shown[0] != testCase.path || !strings.Contains(shown.Content, `"shown": "`+testCase.path+`"`) {
			t.Errorf("%s in the dashboard: shown %q, %s", testCase.toolName, dashboard.shown, shown.Content)
		}

		elsewhere := &showingRun{canShowPages: false}
		told := run(t, elsewhere, testCase.toolName)
		if len(elsewhere.shown) != 0 {
			t.Errorf("%s moved a dashboard nobody reads: %q", testCase.toolName, elsewhere.shown)
		}
		if !strings.Contains(told.Content, `"shown": false`) || !strings.Contains(told.Content, testCase.elsewhere) || strings.Contains(told.Content, "is open") {
			t.Errorf("%s elsewhere says where to go: %s", testCase.toolName, told.Content)
		}
		if told.Note != "" {
			t.Errorf("%s elsewhere notes a page opened: %q", testCase.toolName, told.Note)
		}
	}
}

func TestProfileHandoffDoesNotRequireMailboxServiceOrExposeCredentials(test *testing.T) {
	for _, tool := range phone.Tools() {
		if tool.Name != "phone_setup_profile" {
			continue
		}
		if len(tool.Services) != 0 || tool.Risk != tools.RiskRead {
			test.Fatal("account setup was gated by hosted Mail or changed its read-only handoff")
		}
		response := run(test, &showingRun{canShowPages: false}, tool.Name)
		if !strings.Contains(response.Content, "do not require a mailbox") || strings.Contains(response.Content, "downloadLink") {
			test.Fatal("handoff lost mailbox-free guidance or returned a private profile link")
		}
		return
	}
	test.Fatal("profile handoff missing")
}
