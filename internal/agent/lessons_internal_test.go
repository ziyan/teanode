package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// lessonWindow is a stretch of work: a build that failed, then one that
// worked, then a command left running in the background.
func lessonWindow() []*models.AgentMessage {
	return []*models.AgentMessage{
		{ID: "m1", Role: string(llm.RoleUser), Content: "build the dashboard"},
		{ID: "m2", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "shell", Arguments: `{"command":"npm run build"}`}}},
		{ID: "m3", Role: string(llm.RoleTool), ToolCallID: "c1", Content: `{"exitCode":1,"stderr":"engine node 20 required"}`},
		{ID: "m4", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c2", Name: "shell", Arguments: `{"command":"nvm use 20 && npm run build"}`}}},
		{ID: "m5", Role: string(llm.RoleTool), ToolCallID: "c2", Content: `{"exitCode":0,"stdout":"built in 12s"}`},
		{ID: "m6", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c3", Name: "shell", Arguments: `{"command":"npm run serve"}`}}},
		{ID: "m7", Role: string(llm.RoleTool), ToolCallID: "c3", Content: `{"backgroundId":"b1","stdout":"listening"}`},
		{ID: "m8", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c4", Name: "memory", Arguments: `{}`}}},
		{ID: "m9", Role: string(llm.RoleTool), ToolCallID: "c4", Content: `{"pages":[]}`},
		{ID: "m10", Role: string(llm.RoleAssistant), Content: "Built it with Node 20."},
	}
}

// The commands are the tool calls whose result carries an exit code or a
// background id, numbered in order, each with how it ended; a tool that
// runs no command is not one.
func TestTheCommandsOfAStretchOfWork(t *testing.T) {
	calls, byToolCallId := lessonCallsOf(lessonWindow())
	if len(calls) != 3 || byToolCallId["c4"] != nil {
		t.Fatalf("%d commands: %+v", len(calls), calls)
	}
	if calls[0].hasSucceeded() || !calls[1].hasSucceeded() || calls[2].hasSucceeded() || calls[2].IsFinished {
		t.Errorf("how they ended: %+v %+v %+v", calls[0], calls[1], calls[2])
	}
	transcript := lessonTranscript(lessonWindow(), byToolCallId)
	for _, said := range []string{"them: build the dashboard", "[command 1] shell", "(exit code 1)", "[command 2]", "built in 12s", "(still running in the background)", "you: Built it with Node 20."} {
		if !strings.Contains(transcript, said) {
			t.Errorf("the transcript does not say %q:\n%s", said, transcript)
		}
	}
}

// A lesson is kept only when a command it names ended with exit code 0: one
// naming the failed build, the running server, a command that is not
// there, or none, is the model's word alone.
func TestALessonIsKeptOnlyWhenACommandBearsItOut(t *testing.T) {
	calls, _ := lessonCallsOf(lessonWindow())
	var answer lessonAnswer
	for _, verifiedBy := range [][]int{{2}, {1}, {3}, {9}, nil, {1, 2}} {
		answer.Lessons = append(answer.Lessons, struct {
			AppliesWhen     string `json:"appliesWhen"`
			Approach        string `json:"approach"`
			Avoid           string `json:"avoid"`
			Verification    string `json:"verification"`
			Scope           string `json:"scope"`
			VerifiedByCalls []int  `json:"verifiedByCalls"`
			Topic           string `json:"topic"`
		}{AppliesWhen: "building the web dashboard", Approach: "switch to Node 20 with nvm first", Avoid: "the default Node 18",
			Verification: "the build finished", Scope: "this laptop", VerifiedByCalls: verifiedBy, Topic: "Web Dashboard Build"})
	}
	kept := verifyLessons(answer, calls, "conversation-1")
	if len(kept) != 2 {
		t.Fatalf("kept %d lessons, want the two a successful command bears out: %+v", len(kept), kept)
	}
	lesson := kept[0]
	if lesson.Path != "lessons/web-dashboard-build" || len(lesson.Evidence) != 1 || lesson.Evidence[0].ID != "conversation-1" ||
		!strings.Contains(lesson.Evidence[0].Quote, "nvm use 20") || !strings.HasSuffix(lesson.Evidence[0].Quote, "exit code 0") {
		t.Errorf("the lesson: %+v", lesson)
	}
	if lesson.Text != "When building the web dashboard: switch to Node 20 with nvm first. Avoid: the default Node 18. Verified by: the build finished. Held for: this laptop." {
		t.Errorf("the lesson says %q", lesson.Text)
	}
}
