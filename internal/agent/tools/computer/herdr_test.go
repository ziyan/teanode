package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	deviceComputer "github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
)

// herdrComputer is a fakeComputer whose program watches herdr.
type herdrComputer struct {
	fakeComputer
	// features are what its program does beyond herdr itself.
	features []string
}

func (self *herdrComputer) HasFeature(feature string) bool {
	return feature == deviceComputer.FeatureHerdr || slices.Contains(self.features, feature)
}

func TestHerdrListGivesThoseAskingFirstAPageAtATime(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	var sessions []map[string]any
	for index := range 30 {
		sessions = append(sessions, map[string]any{
			"paneId": fmt.Sprintf("w1:p%d", index), "paneName": fmt.Sprintf("project %02d", index),
			"codingAgentKind": "claude", "herdrSessionState": "idle", "herdrAgentStatus": "idle",
			"transcriptPath": "~/.claude/projects/example/session.jsonl",
		})
	}
	sessions[17]["herdrSessionState"] = "asking"
	encoded, _ := json.Marshal(sessions)
	attached := &herdrComputer{fakeComputer: fakeComputer{answers: map[string]string{"herdr_list": string(encoded)}}}
	ctx := tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration})
	herdr := find(t, "herdr")

	list := func(arguments string) map[string]any {
		t.Helper()
		result, err := herdr.Run(ctx, &tools.Call{Arguments: json.RawMessage(arguments)})
		if err != nil {
			t.Fatal(err)
		}
		content := result.Content
		var answer map[string]any
		if err := json.Unmarshal([]byte(content), &answer); err != nil {
			t.Fatalf("%v: %s", err, content)
		}
		if strings.Contains(content, "transcriptPath") || strings.Contains(content, "herdrAgentStatus") {
			t.Errorf("a listed session carries what read and screen are for: %s", content)
		}
		return answer
	}
	first := list(`{"action":"list"}`)
	page := first["herdrSessions"].([]any)
	if len(page) != 20 || first["totalCount"].(float64) != 30 || first["nextOffset"].(float64) != 20 {
		t.Fatalf("first page: %d sessions, %v", len(page), first)
	}
	if leading := page[0].(map[string]any); leading["paneName"] != "project 17" || leading["computer"] != "laptop" {
		t.Errorf("the session asking is not first: %v", leading)
	}
	if !strings.Contains(first["moreNote"].(string), "10 more") {
		t.Errorf("does not say how many more: %v", first["moreNote"])
	}
	last := list(`{"action":"list","offset":20}`)
	if rest := last["herdrSessions"].([]any); len(rest) != 10 || last["nextOffset"] != nil {
		t.Errorf("last page: %d sessions, %v", len(rest), last)
	}
}

func TestHerdrListFitsItsBudgetAndSurvivesAnyOffset(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	var sessions []map[string]any
	for index := range 30 {
		sessions = append(sessions, map[string]any{
			"paneId": fmt.Sprintf("w1:p%d", index), "paneName": "project", "codingAgentKind": "claude",
			"herdrSessionState": "asking", "paneTitle": strings.Repeat("A long title for a pane ", 3),
			"question": map[string]any{
				"questionFingerprint": "abc123", "herdrQuestionKind": "approval",
				"questionText": strings.Repeat("Do you want to run this command in the project directory? ", 4),
				"options": []map[string]any{
					{"optionNumber": 1, "optionLabel": "Yes", "herdrOptionKind": "choice"},
					{"optionNumber": 2, "optionLabel": "Yes, and do not ask again for this command", "herdrOptionKind": "choice"},
					{"optionNumber": 3, "optionLabel": "No, and tell the agent what to do instead", "herdrOptionKind": "choice"},
				},
			},
		})
	}
	encoded, _ := json.Marshal(sessions)
	attached := &herdrComputer{fakeComputer: fakeComputer{answers: map[string]string{"herdr_list": string(encoded)}}}
	run := &budgetRun{fakeRun: fakeRun{computer: attached, config: configuration}, resultCharacters: 7744}
	ctx := tools.WithRun(context.Background(), run)
	herdr := find(t, "herdr")

	result, err := herdr.Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"list","limit":50}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) > 7744 {
		t.Errorf("a page of %d characters is over its budget", len(result.Content))
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		t.Fatal(err)
	}
	given := len(answer["herdrSessions"].([]any))
	if given == 0 || given >= 30 || answer["nextOffset"].(float64) != float64(given) ||
		!strings.Contains(answer["moreNote"].(string), "and limit 50") {
		t.Errorf("gave %d, then %v", given, answer)
	}
	// Panes of one name keep one order: the page after starts where this
	// one stopped.
	if first := answer["herdrSessions"].([]any)[0].(map[string]any); first["paneId"] != "w1:p0" {
		t.Errorf("the first pane is %v", first["paneId"])
	}
	for _, arguments := range []string{`{"action":"list","offset":9223372036854775800}`, `{"action":"list","offset":31}`} {
		result, err := herdr.Run(ctx, &tools.Call{Arguments: json.RawMessage(arguments)})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Content, "past the end") || !strings.Contains(result.Content, `"herdrSessions":[]`) {
			t.Errorf("%s: %s", arguments, result.Content)
		}
	}
}

// budgetRun is a fakeRun with a result budget, as a run from an MCP client
// has.
type budgetRun struct {
	fakeRun
	resultCharacters int
}

func (self *budgetRun) ResultCharacters() int { return self.resultCharacters }

func TestHerdrFocusAndOpenBesideCarryThePane(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	attached := &herdrComputer{fakeComputer: fakeComputer{answers: map[string]string{
		"herdr_focus": `{"paneId":"w1:p1"}`, "herdr_open": `{"paneId":"w1:p2"}`,
	}}, features: []string{deviceComputer.FeatureHerdrOpenBeside}}
	ctx := tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration})
	herdr := find(t, "herdr")
	for _, arguments := range []string{`{"action":"focus","pane":"w1:p1"}`, `{"action":"open","pane":"w1:p1"}`} {
		if _, err := herdr.Run(ctx, &tools.Call{Arguments: json.RawMessage(arguments)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(attached.asked) != 2 || attached.asked[0] != `herdr_focus {"paneId":"w1:p1"}` ||
		!strings.HasPrefix(attached.asked[1], `herdr_open {"paneId":"w1:p1"`) {
		t.Errorf("asked %q", attached.asked)
	}
	if risk := herdr.RiskOf(json.RawMessage(`{"action":"focus","pane":"w1:p1"}`)); risk != tools.RiskWrite {
		t.Errorf("focus is %v", risk)
	}
	if preview := herdr.Preview(json.RawMessage(`{"action":"focus","pane":"website","computer":"laptop"}`)); preview != "Show pane website on laptop" {
		t.Errorf("preview: %q", preview)
	}
}

func TestHerdrOpenBesideIsRefusedByAProgramTooOldForIt(t *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	attached := &herdrComputer{fakeComputer: fakeComputer{answers: map[string]string{"herdr_open": `{"paneId":"w1:p2"}`}}}
	ctx := tools.WithRun(context.Background(), &fakeRun{computer: attached, config: configuration})
	_, err := find(t, "herdr").Run(ctx, &tools.Call{Arguments: json.RawMessage(`{"action":"open","pane":"w1:p1"}`)})
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Errorf("an old program was asked to open beside a pane: %v", err)
	}
	if len(attached.asked) != 0 {
		t.Errorf("asked %q", attached.asked)
	}
}
