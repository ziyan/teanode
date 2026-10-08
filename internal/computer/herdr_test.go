package computer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHerdr answers herdr's socket from what a test sets, and records what
// it was asked to press and type.
type fakeHerdr struct {
	mutex    sync.Mutex
	agents   []map[string]any
	screens  map[string]string
	pressed  []string
	typed    []string
	prompted []string
	// afterKeys, when set, changes the screens once keys are pressed.
	afterKeys func(paneId string, keys []string)
}

func startFakeHerdr(t *testing.T, home string) *fakeHerdr {
	t.Helper()
	fake := &fakeHerdr{screens: map[string]string{}}
	path := filepath.Join(home, ".config", "herdr", "herdr.sock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go fake.serve(connection)
		}
	}()
	return fake
}

func (self *fakeHerdr) serve(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		return
	}
	var request struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal(line, &request)
	self.mutex.Lock()
	var result any = map[string]any{"type": "ok"}
	var failure map[string]any
	target, _ := request.Params["target"].(string)
	switch request.Method {
	case "agent.list":
		result = map[string]any{"type": "agent_list", "agents": self.agents}
	case "agent.read":
		result = map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": target, "text": self.screens[target]}}
	case "agent.send_keys":
		var keys []string
		for _, key := range request.Params["keys"].([]any) {
			keys = append(keys, key.(string))
		}
		self.pressed = append(self.pressed, strings.Join(keys, " "))
		if self.afterKeys != nil {
			self.afterKeys(target, keys)
		}
	case "pane.send_text":
		self.typed = append(self.typed, request.Params["text"].(string))
	case "agent.prompt":
		self.prompted = append(self.prompted, request.Params["text"].(string))
	default:
		failure = map[string]any{"code": "invalid_request", "message": "unknown variant " + request.Method}
	}
	self.mutex.Unlock()
	answer := map[string]any{"id": request.ID, "result": result}
	if failure != nil {
		// As herdr refuses a request it cannot read: without its id.
		answer = map[string]any{"id": "", "error": failure}
	}
	data, _ := json.Marshal(answer)
	_, _ = connection.Write(append(data, '\n'))
}

func (self *fakeHerdr) setAgent(paneId, codingAgentKind, herdrStatus, codingSessionId, screen string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	agent := map[string]any{"pane_id": paneId, "agent": codingAgentKind, "agent_status": herdrStatus,
		"cwd": "~/src/example", "terminal_title_stripped": "Example work"}
	if codingSessionId != "" {
		agent["agent_session"] = map[string]any{"value": codingSessionId}
	}
	self.agents = slices.DeleteFunc(self.agents, func(each map[string]any) bool { return each["pane_id"] == paneId })
	self.agents = append(self.agents, agent)
	self.screens[paneId] = screen
}

func (self *fakeHerdr) recorded() (pressed, typed, prompted []string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.pressed), slices.Clone(self.typed), slices.Clone(self.prompted)
}

func listForTest(t *testing.T, herdr *Herdr) []*HerdrSession {
	t.Helper()
	listed, err := RunHerdr(context.Background(), herdr, "herdr_list", &HerdrArguments{})
	if err != nil {
		t.Fatal(err)
	}
	return listed.([]*HerdrSession)
}

func TestAClaudeQuestionHerdrCallsIdleIsAsking(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-single"))
	fake.setAgent("w1:p2", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	fake.setAgent("w1:p3", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-working"))
	sessions := listForTest(t, NewHerdr(home))
	if len(sessions) != 3 {
		t.Fatalf("listed %d", len(sessions))
	}
	if sessions[0].HerdrSessionState != HerdrSessionStateAsking || sessions[0].Question == nil || sessions[0].HerdrAgentStatus != "idle" {
		t.Errorf("the pane with a form: %+v", sessions[0])
	}
	if sessions[1].HerdrSessionState != HerdrSessionStateIdle || sessions[1].Question != nil {
		t.Errorf("the idle pane: %+v", sessions[1])
	}
	if sessions[2].HerdrSessionState != HerdrSessionStateWorking {
		t.Errorf("the pane that says esc to interrupt: %+v", sessions[2])
	}
}

const codexHistoryAsking = `{"type":"event_msg","payload":{"type":"task_started"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>cwd</environment_context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Pick a fruit for the example."}]}}
{"type":"response_item","payload":{"type":"function_call","name":"request_user_input_async","call_id":"call-1","arguments":"{\"questions\":[{\"title\":\"Which fruit should we pick?\",\"options\":[\"Apple\",\"Banana\"]}]}"}}
{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"{\"accepted\":true}"}}
{"type":"event_msg","payload":{"type":"task_complete"}}
`

func writeCodexHistory(t *testing.T, home, codingSessionId, text string) string {
	t.Helper()
	path := filepath.Join(home, ".codex", "sessions", "2026", "01", "02", "rollout-2026-01-02T03-04-05-"+codingSessionId+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestACodexQuestionIsReadFromItsHistoryAndAnsweredByTyping(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	writeCodexHistory(t, home, "0000-aaaa", codexHistoryAsking)
	fake.setAgent("w1:p1", CodingAgentKindCodex, "idle", "0000-aaaa", readHerdrFixture(t, "codex-idle"))
	herdr := NewHerdr(home)
	session := listForTest(t, herdr)[0]
	if session.HerdrSessionState != HerdrSessionStateAsking || session.Question == nil || !session.Question.IsFromTranscript {
		t.Fatalf("%+v", session)
	}
	if got := strings.Join(optionLabels(session.Question), "|"); got != "Apple|Banana|Type something" {
		t.Errorf("options %s", got)
	}
	answer, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{
		PaneID: "w1:p1", QuestionFingerprint: session.Question.QuestionFingerprint, OptionNumbers: []int{2},
	})
	if err != nil {
		t.Fatal(err)
	}
	pressed, typed, _ := fake.recorded()
	if !slices.Equal(typed, []string{"Banana"}) || !slices.Equal(pressed, []string{"enter"}) {
		t.Errorf("typed %v, pressed %v", typed, pressed)
	}
	if answer.(*HerdrAnswerResult).AnsweredWith != "2. Banana" {
		t.Errorf("%+v", answer)
	}
}

func TestACodexQuestionTheyAnsweredIsNoLongerWaiting(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	writeCodexHistory(t, home, "0000-bbbb", codexHistoryAsking+
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Banana"}]}}`+"\n")
	fake.setAgent("w1:p1", CodingAgentKindCodex, "idle", "0000-bbbb", readHerdrFixture(t, "codex-idle"))
	if session := listForTest(t, NewHerdr(home))[0]; session.Question != nil || session.HerdrSessionState != HerdrSessionStateIdle {
		t.Errorf("%+v", session)
	}
}

func TestACodexQuestionItsToolRefusedIsNotWaiting(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	writeCodexHistory(t, home, "0000-cccc", strings.Replace(codexHistoryAsking, `"output":"{\"accepted\":true}"`, `"output":"request_user_input is unavailable in this mode"`, 1))
	fake.setAgent("w1:p1", CodingAgentKindCodex, "idle", "0000-cccc", readHerdrFixture(t, "codex-idle"))
	if session := listForTest(t, NewHerdr(home))[0]; session.Question != nil {
		t.Errorf("%+v", session.Question)
	}
}

func TestSendIsRefusedWhileAskingAndWhileWorkingUnlessQueued(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-single"))
	fake.setAgent("w1:p2", CodingAgentKindClaude, "working", "", readHerdrFixture(t, "claude-working"))
	fake.setAgent("w1:p3", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	herdr := NewHerdr(home)
	ctx := context.Background()
	if _, err := RunHerdr(ctx, herdr, "herdr_send", &HerdrArguments{PaneID: "w1:p1", Text: "go on"}); err == nil || !strings.Contains(err.Error(), "asking a question") {
		t.Errorf("asking: %v", err)
	}
	if _, err := RunHerdr(ctx, herdr, "herdr_send", &HerdrArguments{PaneID: "w1:p2", Text: "go on"}); err == nil || !strings.Contains(err.Error(), "is working") {
		t.Errorf("working: %v", err)
	}
	if _, err := RunHerdr(ctx, herdr, "herdr_send", &HerdrArguments{PaneID: "w1:p2", Text: "and then this", ShouldQueue: true}); err != nil {
		t.Errorf("queued: %v", err)
	}
	if _, err := RunHerdr(ctx, herdr, "herdr_send", &HerdrArguments{PaneID: "w1:p3", Text: "go on"}); err != nil {
		t.Errorf("idle: %v", err)
	}
	if _, _, prompted := fake.recorded(); !slices.Equal(prompted, []string{"and then this", "go on"}) {
		t.Errorf("prompted %v", prompted)
	}
}

func TestAnAnswerToAQuestionThatChangedPressesNothing(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-single"))
	herdr := NewHerdr(home)
	stale := listForTest(t, herdr)[0].Question.QuestionFingerprint
	// Answered at the keyboard, and a second question came.
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-multi"))
	_, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{PaneID: "w1:p1", QuestionFingerprint: stale, OptionNumbers: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "already answered or has changed") {
		t.Errorf("%v", err)
	}
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	if _, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{PaneID: "w1:p1", QuestionFingerprint: stale, OptionNumbers: []int{1}}); err == nil {
		t.Error("an answer to nothing was taken")
	}
	if pressed, typed, _ := fake.recorded(); len(pressed)+len(typed) != 0 {
		t.Errorf("pressed %v, typed %v", pressed, typed)
	}
}

func TestAnAnswerPressesTheOptionsNumberAndTheQuestionGoes(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	idle := readHerdrFixture(t, "claude-idle")
	fake.afterKeys = func(paneId string, keys []string) { fake.screens[paneId] = idle }
	herdr := NewHerdr(home)
	question := listForTest(t, herdr)[0].Question
	answer, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{PaneID: "w1:p1", QuestionFingerprint: question.QuestionFingerprint, OptionNumbers: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	if pressed, _, _ := fake.recorded(); !slices.Equal(pressed, []string{"4"}) {
		t.Errorf("pressed %v", pressed)
	}
	if result := answer.(*HerdrAnswerResult); !result.IsAnswerAccepted || result.AnsweredWith != "4. No" {
		t.Errorf("%+v", result)
	}
}

func TestTheKeysThatAnswerEachKindOfQuestion(t *testing.T) {
	single := recognizeQuestion(CodingAgentKindClaude, readHerdrFixture(t, "claude-single"))
	several := recognizeQuestion(CodingAgentKindClaude, readHerdrFixture(t, "claude-multi"))
	describe := func(steps []herdrStep) string {
		var said []string
		for _, step := range steps {
			if step.text != "" {
				said = append(said, "type:"+step.text)
			} else {
				said = append(said, strings.Join(step.keys, "+"))
			}
		}
		return strings.Join(said, " ")
	}
	if hasControlCharacters("one\ntwo", true) || !hasControlCharacters("one\ntwo", false) || !hasControlCharacters("go\x1b[A", true) {
		t.Error("control characters are told apart wrongly")
	}
	cases := []struct {
		question      *HerdrQuestion
		optionNumbers []int
		freeText      string
		want          string
	}{
		{single, []int{2}, "", "2"},
		{single, nil, "a pear", "4 type:a pear enter"},
		{single, []int{4}, "a pear", "4 type:a pear enter"},
		{several, []int{1, 3}, "", "1 3 right"},
	}
	for _, each := range cases {
		steps, _, err := answerSteps(each.question, each.optionNumbers, each.freeText)
		if err != nil {
			t.Errorf("%v: %v", each.optionNumbers, err)
			continue
		}
		if got := describe(steps); got != each.want {
			t.Errorf("%v %q: got %q, want %q", each.optionNumbers, each.freeText, got, each.want)
		}
	}
	for _, refused := range []struct {
		optionNumbers []int
		freeText      string
	}{{nil, ""}, {[]int{9}, ""}, {[]int{1, 2}, ""}, {[]int{4}, ""}} {
		if _, _, err := answerSteps(single, refused.optionNumbers, refused.freeText); err == nil {
			t.Errorf("%v %q was taken", refused.optionNumbers, refused.freeText)
		}
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	reader    io.ReaderAt
	byteCount int64
}

func (self *countingReader) ReadAt(data []byte, offset int64) (int, error) {
	count, err := self.reader.ReadAt(data, offset)
	self.byteCount += int64(count)
	return count, err
}

func TestALargeHistoryIsReadFromItsEndOnly(t *testing.T) {
	var history strings.Builder
	output := strings.Repeat("x", 4000)
	for history.Len() < 20<<20 {
		fmt.Fprintf(&history, `{"type":"user","message":{"content":[{"type":"tool_result","content":%q}]}}`+"\n", output)
	}
	history.WriteString(`{"type":"user","message":{"content":"Add a test for the empty case."}}` + "\n")
	history.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"Adding it."},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n")
	history.WriteString(`{"type":"user","message":{"content":"<system-reminder>injected</system-reminder>"}}` + "\n")
	data := []byte(history.String())
	reader := &countingReader{reader: strings.NewReader(string(data))}
	turns, isTruncated, err := readTurnsFrom(reader, int64(len(data)), CodingAgentKindClaude, 3)
	if err != nil {
		t.Fatal(err)
	}
	if reader.byteCount > transcriptMostChunkBytes*2 {
		t.Errorf("read %d bytes of %d", reader.byteCount, len(data))
	}
	var said []string
	for _, turn := range turns {
		said = append(said, turn.HerdrTurnRole+": "+turn.TurnText)
	}
	if got := strings.Join(said, " | "); got != "person: Add a test for the empty case. | agent: Adding it. | tool: Bash: go test ./..." || !isTruncated {
		t.Errorf("%s (truncated %v)", got, isTruncated)
	}
}

func TestWithoutHerdrRunningTheActionsSaySo(t *testing.T) {
	_, err := RunHerdr(context.Background(), NewHerdr(t.TempDir()), "herdr_list", &HerdrArguments{})
	if !errors.Is(err, errHerdrNotRunning) {
		t.Errorf("%v", err)
	}
}

func TestAQuestionIsToldWhenItComesAndGoesAndAgainUntilAcknowledged(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	herdr := NewHerdr(home)
	var mutex sync.Mutex
	var told []*HerdrEvent
	stop := herdr.listen(func(event *HerdrEvent) {
		mutex.Lock()
		told = append(told, event)
		mutex.Unlock()
	})
	listForTest(t, herdr)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-single"))
	listForTest(t, herdr)
	listForTest(t, herdr)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	listForTest(t, herdr)
	stop()
	mutex.Lock()
	if len(told) != 2 || told[0].HerdrEventKind != HerdrEventKindAsking || told[1].HerdrEventKind != HerdrEventKindAnswered ||
		told[1].HerdrSession.Question.QuestionFingerprint != told[0].HerdrSession.Question.QuestionFingerprint {
		t.Fatalf("told %+v", told)
	}
	answeredId := told[1].HerdrEventID
	told = nil
	mutex.Unlock()

	// A reconnect hears again what was not acknowledged, but not a question
	// that was answered while nobody listened; once acknowledged, nothing.
	again := []string{}
	herdr.listen(func(event *HerdrEvent) { again = append(again, event.HerdrEventKind) })()
	if !slices.Equal(again, []string{HerdrEventKindAnswered}) {
		t.Errorf("said again %v", again)
	}
	herdr.acknowledge([]string{answeredId})
	again = []string{}
	herdr.listen(func(event *HerdrEvent) { again = append(again, event.HerdrEventKind) })()
	if len(again) != 0 {
		t.Errorf("after the acknowledgement %v", again)
	}
}

func TestAnUnacknowledgedQuestionIsSaidAgainOnReconnect(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-single"))
	herdr := NewHerdr(home)
	listForTest(t, herdr)
	again := []string{}
	herdr.listen(func(event *HerdrEvent) { again = append(again, event.HerdrEventKind) })()
	if !slices.Equal(again, []string{HerdrEventKindAsking}) {
		t.Errorf("said again %v", again)
	}
}

func TestAQuestionToldBeforeARestartIsNotToldAgain(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	before := NewHerdr(home)
	var told []*HerdrEvent
	stop := before.listen(func(event *HerdrEvent) { told = append(told, event) })
	first := listForTest(t, before)[0].Question.QuestionFingerprint
	stop()
	before.acknowledge([]string{told[0].HerdrEventID})

	after := NewHerdr(home)
	var toldAfter []string
	defer after.listen(func(event *HerdrEvent) { toldAfter = append(toldAfter, event.HerdrEventKind) })()
	if again := listForTest(t, after)[0].Question.QuestionFingerprint; again != first {
		t.Errorf("the fingerprint changed across a restart")
	}
	if len(toldAfter) != 0 {
		t.Errorf("told again after a restart: %v", toldAfter)
	}
}

func TestAScreenThatCouldNotBeReadKeepsItsQuestion(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	herdr := NewHerdr(home)
	first := listForTest(t, herdr)[0].Question.QuestionFingerprint
	// A screen that fails to read, then one read mid-redraw with no form.
	herdr.mutex.Lock()
	unread := &HerdrSession{PaneID: "w1:p1", CodingAgentKind: CodingAgentKindClaude, isScreenUnread: true}
	var events []*HerdrEvent
	herdr.recordLocked(unread, &events)
	herdr.mutex.Unlock()
	if unread.Question == nil || unread.Question.QuestionFingerprint != first {
		t.Errorf("an unread screen lost its question: %+v", unread.Question)
	}
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	listForTest(t, herdr)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	if again := listForTest(t, herdr)[0].Question.QuestionFingerprint; again != first {
		t.Error("a form missed for one look came back as another question")
	}
}

func TestTheSameQuestionAskedAgainIsAnotherQuestion(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	herdr := NewHerdr(home)
	first := listForTest(t, herdr)[0].Question.QuestionFingerprint
	if again := listForTest(t, herdr)[0].Question.QuestionFingerprint; again != first {
		t.Fatal("a question still waiting changed its fingerprint")
	}
	// Answered, and gone for two looks, then asked again.
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	listForTest(t, herdr)
	listForTest(t, herdr)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-tool"))
	second := listForTest(t, herdr)[0].Question.QuestionFingerprint
	if second == first {
		t.Fatal("the same approval asked again kept the first one's fingerprint")
	}
	_, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{PaneID: "w1:p1", QuestionFingerprint: first, OptionNumbers: []int{1}})
	if err == nil {
		t.Error("an answer to the first approval was pressed into the second")
	}
	if pressed, _, _ := fake.recorded(); len(pressed) != 0 {
		t.Errorf("pressed %v", pressed)
	}
}

func TestAWatchedSessionIsToldOnceWhenItStopsWorking(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "working", "", readHerdrFixture(t, "claude-working"))
	herdr := NewHerdr(home)
	var told []*HerdrEvent
	defer herdr.listen(func(event *HerdrEvent) { told = append(told, event) })()
	origin := json.RawMessage(`{"conversationId":"c1"}`)
	if _, err := RunHerdr(context.Background(), herdr, "herdr_watch", &HerdrArguments{PaneID: "w1:p1", Origin: origin}); err != nil {
		t.Fatal(err)
	}
	if session := listForTest(t, herdr)[0]; !session.IsWatched {
		t.Errorf("%+v", session)
	}
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	listForTest(t, herdr)
	listForTest(t, herdr)
	if len(told) != 1 || told[0].HerdrEventKind != HerdrEventKindSettled || string(told[0].Origin) != string(origin) {
		t.Errorf("told %+v", told)
	}
}

func TestSetupAddsItsHooksBesideOthersAndRemovalPutsThingsBack(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `{
  "model": "example",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "bash '~/example/state-hook' session",
            "timeout": 10
          }
        ]
      }
    ]
  },
  "theme": "dark"
}
`
	if err := os.WriteFile(settingsPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := setUpHooks(home, false)
	if err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(settingsPath)
	text := string(installed)
	if !result.IsInstalled || !strings.Contains(text, "state-hook' session") || strings.Count(text, "teanode-herdr-hook") != len(hookEventNames) {
		t.Fatalf("%+v\n%s", result, text)
	}
	if strings.Index(text, `"model"`) > strings.Index(text, `"theme"`) {
		t.Errorf("the keys moved:\n%s", text)
	}
	if backup, _ := os.ReadFile(settingsPath + hookBackupSuffix); string(backup) != original {
		t.Errorf("no copy kept")
	}
	// Twice is the same as once.
	if _, err := setUpHooks(home, false); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(settingsPath); string(again) != text {
		t.Errorf("a second setup changed it")
	}
	if _, err := setUpHooks(home, true); err != nil {
		t.Fatal(err)
	}
	removed, _ := os.ReadFile(settingsPath)
	if string(removed) != original {
		t.Errorf("removal left\n%s", removed)
	}
}

func TestTheHooksReportDecidesWorkingAndIdle(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, hookEventsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	report := func(eventName, codingSessionId string, at int64) string {
		return fmt.Sprintf(`{"hookEventName":%q,"sessionId":%q,"reportedAt":%d}`+"\n", eventName, codingSessionId, at)
	}
	lines := report("UserPromptSubmit", "one", now) + report("Stop", "two", now) + report("PreToolUse", "one", now) +
		report("UserPromptSubmit", "old", now-int64(hookWorkingFresh.Seconds())-60)
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	reports := readHookReports(home)
	for codingSessionId, want := range map[string]string{"one": hookStateWorking, "two": hookStateIdle, "old": hookStateNone, "none": hookStateNone} {
		if got := hookStateOf(reports, codingSessionId, time.Now()); got != want {
			t.Errorf("%s: %q, want %q", codingSessionId, got, want)
		}
	}
}

func TestTheHookScriptReportsOnlyTheEventAndTheSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}
	home := t.TempDir()
	if _, err := setUpHooks(home, false); err != nil {
		t.Fatal(err)
	}
	input := "{\n  \"session_id\": \"0000-dddd\",\n  \"prompt\": \"a secret prompt\",\n  \"tool_input\": {\"session_id\": \"0000-eeee\"}\n}\n"
	command := exec.Command("sh", filepath.Join(home, hookScriptPath), "UserPromptSubmit")
	command.Env = append(os.Environ(), "HOME="+home)
	command.Stdin = strings.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("the hook said %q: %v", output, err)
	}
	path := filepath.Join(home, hookEventsPath)
	written, _ := os.ReadFile(path)
	if strings.Contains(string(written), "secret") {
		t.Errorf("the hook kept what it was given: %s", written)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the events file is not the person's alone: %v", info.Mode())
	}
	if got := hookStateOf(readHookReports(home), "0000-dddd", time.Now()); got != hookStateWorking {
		t.Errorf("%q from %s", got, written)
	}
}

func TestARequestHerdrCannotReadIsRefusedInHerdrsWords(t *testing.T) {
	home := t.TempDir()
	startFakeHerdr(t, home)
	err := newHerdrClient(home).call(context.Background(), "agent.unknown", map[string]any{}, nil)
	var refused *herdrError
	if !errors.As(err, &refused) || refused.Code != "invalid_request" {
		t.Errorf("%v", err)
	}
}

func TestAFormThatWaitsForEnterAfterTheNumberGetsIt(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindCodex, "blocked", "", readHerdrFixture(t, "codex-trust"))
	idle := readHerdrFixture(t, "codex-idle")
	// The number only moves the cursor; enter answers.
	fake.afterKeys = func(paneId string, keys []string) {
		if slices.Equal(keys, []string{"enter"}) {
			fake.screens[paneId] = idle
		}
	}
	herdr := NewHerdr(home)
	question := listForTest(t, herdr)[0].Question
	if question == nil || question.CursorOptionNumber != 1 {
		t.Fatalf("%+v", question)
	}
	answer, err := RunHerdr(context.Background(), herdr, "herdr_answer", &HerdrArguments{PaneID: "w1:p1", QuestionFingerprint: question.QuestionFingerprint, OptionNumbers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if pressed, _, _ := fake.recorded(); !slices.Equal(pressed, []string{"1", "enter"}) {
		t.Errorf("pressed %v", pressed)
	}
	if !answer.(*HerdrAnswerResult).IsAnswerAccepted {
		t.Error("not accepted")
	}
}

func TestAWatchOutlivesARestartAndSaysWhenItsSessionFinished(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "working", "", readHerdrFixture(t, "claude-working"))
	before := NewHerdr(home)
	origin := json.RawMessage(`{"conversationId":"c1"}`)
	if _, err := RunHerdr(context.Background(), before, "herdr_watch", &HerdrArguments{PaneID: "w1:p1", Origin: origin}); err != nil {
		t.Fatal(err)
	}
	// The program restarts; the session finished meanwhile.
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	after := NewHerdr(home)
	var told []*HerdrEvent
	defer after.listen(func(event *HerdrEvent) { told = append(told, event) })()
	listForTest(t, after)
	listForTest(t, after)
	if len(told) != 1 || told[0].HerdrEventKind != HerdrEventKindSettled || string(told[0].Origin) != string(origin) {
		t.Errorf("told %+v", told)
	}
	if again := NewHerdr(home); len(again.watches) != 0 {
		t.Errorf("a watch told was kept: %+v", again.watches)
	}
}
