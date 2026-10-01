package agent

import (
	"strconv"
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
	kept := verifyLessons(answer, calls, "conversation-1", minimumLessonCount)
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

// A result as the transcript keeps it: fenced as data from outside, and cut
// mid-JSON when long. The exit code is read either way, and the terminal's
// exit_code as well as the shell's exitCode.
func TestACommandResultIsReadAsItIsStored(t *testing.T) {
	long := strings.Repeat("compiling a module\n", 3000)
	cut := untrustedOpen + "\n" + `{"exitCode":0,"stdout":"` + long[:20000] + "\n[cut here: the result goes on]\n" + untrustedClose
	for name, expected := range map[string]struct {
		content                   string
		exitCode                  int
		hasExitCode, isBackground bool
	}{
		"fenced":        {untrustedOpen + "\n" + `{"exitCode":2,"stderr":"no such file"}` + "\n" + untrustedClose, 2, true, false},
		"cut":           {cut, 0, true, false},
		"terminal":      {untrustedOpen + "\n" + `{"exit_code":0,"screen":"$ make\nok"}` + "\n" + untrustedClose, 0, true, false},
		"background":    {untrustedOpen + "\n" + `{"backgroundId":"b1","stdout":"listening"}` + "\n" + untrustedClose, 0, false, true},
		"not a command": {untrustedOpen + "\n" + `{"pages":[]}` + "\n" + untrustedClose, 0, false, false},
		"plain text":    {"the page was saved", 0, false, false},
	} {
		exitCode, hasExitCode, isBackground, _ := readCommandResult(expected.content)
		if exitCode != expected.exitCode || hasExitCode != expected.hasExitCode || isBackground != expected.isBackground {
			t.Errorf("%s: exit code %d (%v), background %v", name, exitCode, hasExitCode, isBackground)
		}
	}
}

// The lesson window starts just after the previous mark, and runs through
// the results answering the calls of the last message read.
func TestTheLessonWindowCoversTheCommandsAtItsEnds(t *testing.T) {
	messages := lessonWindow()
	window := lessonWindowOf(messages, "m1", messages[5])
	if len(window) != 6 || window[0].ID != "m2" || window[len(window)-1].ID != "m7" {
		var ids []string
		for _, message := range window {
			ids = append(ids, message.ID)
		}
		t.Errorf("the window is %v, want m2 through m7", ids)
	}
	if whole := lessonWindowOf(messages, "", messages[len(messages)-1]); len(whole) != len(messages) {
		t.Errorf("with no mark the window is %d messages, want all %d", len(whole), len(messages))
	}
}

// A long output is shown as its head and its tail, with how many characters
// were left out between them said where they were; a short one is whole.
func TestALongCommandOutputShowsItsHeadAndTail(t *testing.T) {
	output := "running the test suite\n" + strings.Repeat("ok package\n", 500) + "FAIL: two tests failed"
	window := []*models.AgentMessage{
		{ID: "m1", Role: string(llm.RoleAssistant), ToolCalls: []models.AgentToolCall{{ID: "c1", Name: "shell", Arguments: `{"command":"make test"}`}}},
		{ID: "m2", Role: string(llm.RoleTool), ToolCallID: "c1", Content: `{"exitCode":2,"stdout":` + strconv.Quote(output) + `}`},
	}
	calls, byToolCallId := lessonCallsOf(window)
	if len(calls) != 1 {
		t.Fatalf("%d commands", len(calls))
	}
	leftOutCount := len([]rune(output)) - lessonCallOutputHeadLength - lessonCallOutputTailLength
	transcript := lessonTranscript(window, byToolCallId)
	for _, said := range []string{"running the test suite", "FAIL: two tests failed", "(exit code 2)",
		"[" + strconv.Itoa(leftOutCount) + " characters of the output left out here]"} {
		if !strings.Contains(transcript, said) {
			t.Errorf("the transcript does not say %q:\n%s", said, transcript)
		}
	}
	if short := headAndTail("built in 12s", lessonCallOutputHeadLength, lessonCallOutputTailLength, "the output"); short != "built in 12s" {
		t.Errorf("a short output became %q", short)
	}
}

// longSession is a working session too long for one call: a command that
// worked at its start, then many commands with long output, then one more
// that worked at its end.
func longSession(commandCount int) []*models.AgentMessage {
	messages := []*models.AgentMessage{{ID: "start", Role: string(llm.RoleUser), Content: "set up the mail relay"}}
	for number := 1; number <= commandCount; number++ {
		callId := "c" + strconv.Itoa(number)
		messages = append(messages,
			&models.AgentMessage{ID: "call" + strconv.Itoa(number), Role: string(llm.RoleAssistant),
				ToolCalls: []models.AgentToolCall{{ID: callId, Name: "shell", Arguments: `{"command":"tail -n 500 relay.log"}`}}},
			&models.AgentMessage{ID: "result" + strconv.Itoa(number), Role: string(llm.RoleTool), ToolCallID: callId,
				Content: `{"exitCode":0,"stdout":` + strconv.Quote(strings.Repeat("relay accepted a message\n", 200)) + `}`})
	}
	return append(messages, &models.AgentMessage{ID: "end", Role: string(llm.RoleAssistant), Content: "The relay is set up."})
}

// A window longer than one call is split into consecutive parts where one
// message ends and the next begins, each about a call's size, each with its
// own commands, and together they are the whole window.
func TestALongWindowIsReadInParts(t *testing.T) {
	window := longSession(80)
	calls, byToolCallId := lessonCallsOf(window)
	parts := lessonParts(window, byToolCallId, lessonPartRunes)
	if len(parts) < 2 {
		t.Fatalf("%d parts, want the window split", len(parts))
	}
	var transcripts []string
	callCount := 0
	previousEnd := 0
	for index, part := range parts {
		if runes := len([]rune(part.Transcript)); runes > lessonPartRunes {
			t.Errorf("part %d is %d characters, more than a call holds", index+1, runes)
		}
		if part.EndIndex <= previousEnd {
			t.Errorf("part %d ends at %d, before the previous part's end %d", index+1, part.EndIndex, previousEnd)
		}
		for _, call := range part.Calls {
			if call.Number != callCount+1 {
				t.Errorf("part %d has command %d, want %d", index+1, call.Number, callCount+1)
			}
			callCount++
		}
		previousEnd = part.EndIndex
		transcripts = append(transcripts, part.Transcript)
	}
	if callCount != len(calls) || previousEnd != len(window) {
		t.Errorf("the parts have %d of %d commands and end at %d of %d messages", callCount, len(calls), previousEnd, len(window))
	}
	if strings.Join(transcripts, "\n\n") != lessonTranscript(window, byToolCallId) {
		t.Error("the parts together are not the whole window")
	}
	if !strings.HasPrefix(parts[0].Transcript, "them: set up the mail relay") || !strings.HasSuffix(parts[len(parts)-1].Transcript, "you: The relay is set up.") {
		t.Error("the parts do not start and end where the window does")
	}

	// A lesson read from the second part may cite only the commands in it.
	var answer lessonAnswer
	for _, verifiedBy := range []int{1, parts[1].Calls[0].Number} {
		answer.Lessons = append(answer.Lessons, struct {
			AppliesWhen     string `json:"appliesWhen"`
			Approach        string `json:"approach"`
			Avoid           string `json:"avoid"`
			Verification    string `json:"verification"`
			Scope           string `json:"scope"`
			VerifiedByCalls []int  `json:"verifiedByCalls"`
			Topic           string `json:"topic"`
		}{AppliesWhen: "checking the relay", Approach: "read the end of relay.log", VerifiedByCalls: []int{verifiedBy}, Topic: "mail relay"})
	}
	kept := verifyLessons(answer, parts[1].Calls, "conversation-1", lessonsAllowedFor(parts[1].Transcript))
	if len(kept) != 1 || !strings.Contains(kept[0].Evidence[0].Quote, "tail -n 500") {
		t.Errorf("kept %+v, want only the lesson citing a command of this part", kept)
	}
}

// What a call may file grows with what it read, and never falls below the
// three a short exchange may file.
func TestTheLessonsAllowedGrowWithWhatWasRead(t *testing.T) {
	if allowed := lessonsAllowedFor("a short exchange"); allowed != minimumLessonCount {
		t.Errorf("a short exchange may file %d lessons, want %d", allowed, minimumLessonCount)
	}
	if allowed := lessonsAllowedFor(strings.Repeat("x", lessonPartRunes)); allowed != lessonPartRunes/lessonRunes || allowed <= minimumLessonCount {
		t.Errorf("a whole part may file %d lessons", allowed)
	}
}
