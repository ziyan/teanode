package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	deviceComputer "github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
)

// herdrComputer is a fakeComputer whose program watches herdr.
type herdrComputer struct{ fakeComputer }

func (self *herdrComputer) HasFeature(feature string) bool {
	return feature == deviceComputer.FeatureHerdr
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
	attached := &herdrComputer{fakeComputer{answers: map[string]string{"herdr_list": string(encoded)}}}
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
