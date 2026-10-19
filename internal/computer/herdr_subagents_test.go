package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The agents are read from files shaped as Claude Code writes them; every
// id, task and name here is invented.

var subagentsNow = time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)

func subagentStamp(secondsBefore int) string {
	return subagentsNow.Add(-time.Duration(secondsBefore) * time.Second).Format("2006-01-02T15:04:05.000Z")
}

// subagentReplyLine is a reply of an agent: text, or the tools it called.
func subagentReplyLine(subagentId string, secondsBefore int, model, stopReason string, toolNames ...string) string {
	var blocks []string
	if len(toolNames) == 0 {
		blocks = append(blocks, `{"type":"text","text":"some words"}`)
	}
	for index, toolName := range toolNames {
		blocks = append(blocks, fmt.Sprintf(`{"type":"tool_use","id":"call-%d","name":%q,"input":{}}`, index, toolName))
	}
	stop := "null"
	if stopReason != "" {
		stop = fmt.Sprintf("%q", stopReason)
	}
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"agentId":%q,"isSidechain":true,"message":{"model":%q,"content":[%s],"stop_reason":%s,"usage":{}}}`,
		subagentStamp(secondsBefore), subagentId, model, strings.Join(blocks, ","), stop)
}

func subagentUserLine(subagentId string, secondsBefore int) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"agentId":%q,"isSidechain":true,"message":{"role":"user","content":"do the task"}}`,
		subagentStamp(secondsBefore), subagentId)
}

type subagentFiles struct {
	t      *testing.T
	folder string
}

// writeSubagent writes an agent's metadata and history.
func (self *subagentFiles) writeSubagent(subagentId string, meta map[string]any, lines ...string) {
	self.t.Helper()
	if err := os.MkdirAll(self.folder, 0o700); err != nil {
		self.t.Fatal(err)
	}
	encodedMeta, err := json.Marshal(meta)
	if err != nil {
		self.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(self.folder, "agent-"+subagentId+".meta.json"), encodedMeta, 0o600); err != nil {
		self.t.Fatal(err)
	}
	historyPath := filepath.Join(self.folder, "agent-"+subagentId+".jsonl")
	if err := os.WriteFile(historyPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		self.t.Fatal(err)
	}
	// The file was last written when its last line was, as Claude Code's are.
	var lastWrittenAt time.Time
	for _, line := range lines {
		var record subagentRecord
		if json.Unmarshal([]byte(line), &record) == nil && record.Timestamp != nil && record.Timestamp.After(lastWrittenAt) {
			lastWrittenAt = *record.Timestamp
		}
	}
	if !lastWrittenAt.IsZero() {
		if err := os.Chtimes(historyPath, lastWrittenAt, lastWrittenAt); err != nil {
			self.t.Fatal(err)
		}
	}
}

// writeSubagentSession writes a Claude Code session's history under a
// temporary home, with the person speaking 600 seconds before subagentsNow
// (and once much earlier), and returns its path and an agent writer.
func writeSubagentSession(t *testing.T, home, codingSessionId string) (string, *subagentFiles) {
	t.Helper()
	path := filepath.Join(home, ".claude", "projects", "-example-project", codingSessionId+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":"an earlier request"}}`, subagentStamp(7200)),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"model":"example-model-old","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}}`, subagentStamp(7100)),
		fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":"look at three files"}}`, subagentStamp(600)),
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"model":"example-model-main","content":[{"type":"text","text":"starting"}],"stop_reason":"end_turn"}}`, subagentStamp(590)),
		// An agent's line among the session's own is not the session's.
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"isSidechain":true,"message":{"model":"example-model-agent","content":[{"type":"text","text":"inside"}],"stop_reason":"end_turn"}}`, subagentStamp(580)),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, &subagentFiles{t: t, folder: filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")}
}

func subagentByID(result *HerdrSubagentsResult, subagentId string) *HerdrSubagent {
	for _, subagent := range result.Subagents {
		if subagent.SubagentID == subagentId {
			return subagent
		}
	}
	return nil
}

func TestTheAgentsOfARequestAreATreeWithStatesModelsAndTools(t *testing.T) {
	// The metadata names the model asked for by a short name; the replies name it in full, which is what is shown.
	path, files := writeSubagentSession(t, t.TempDir(), "session-aaaa")
	files.writeSubagent("a0000000000000001", map[string]any{
		"agentType": "Explore", "description": "Look at the first file", "model": "example-short", "toolUseId": "call-1", "spawnDepth": 1, "finished": true,
	},
		subagentUserLine("a0000000000000001", 570),
		subagentReplyLine("a0000000000000001", 560, "example-model-small", "tool_use", "Read", "Grep"),
		subagentReplyLine("a0000000000000001", 540, "example-model-small", "tool_use", "Read", "mcp__examplesearch__find", "mcp__examplesearch__fetch"),
		subagentReplyLine("a0000000000000001", 500, "example-model-small", "end_turn"),
	)
	files.writeSubagent("a0000000000000002", map[string]any{
		"agentType": "general-purpose", "description": "Write the second summary", "name": "writer", "spawnDepth": 1, "stoppedByUser": true,
	},
		subagentUserLine("a0000000000000002", 570),
		subagentReplyLine("a0000000000000002", 300, "example-model-main", "tool_use", "Bash"),
	)
	files.writeSubagent("a0000000000000003", map[string]any{
		"agentType": "general-purpose", "description": "Check the third file", "spawnDepth": 1, "requestShape": "background",
	},
		subagentUserLine("a0000000000000003", 100),
		subagentReplyLine("a0000000000000003", 10, "example-model-main", "tool_use", "Read"),
	)
	files.writeSubagent("a0000000000000004", map[string]any{
		"agentType": "Explore", "description": "Look under the third file", "spawnDepth": 2, "parentAgentId": "a0000000000000003",
	},
		subagentUserLine("a0000000000000004", 90),
		subagentReplyLine("a0000000000000004", 80, "example-model-small", "end_turn"),
	)
	// From an earlier request: dropped, though the person spoke after it.
	files.writeSubagent("a0000000000000005", map[string]any{
		"agentType": "Explore", "description": "Something from before", "spawnDepth": 1, "finished": true,
	},
		subagentUserLine("a0000000000000005", 7000),
		subagentReplyLine("a0000000000000005", 6900, "example-model-small", "end_turn"),
	)
	// From an earlier request but still writing: kept as running.
	files.writeSubagent("a0000000000000006", map[string]any{
		"agentType": "general-purpose", "description": "A long job from before", "spawnDepth": 1,
	},
		subagentUserLine("a0000000000000006", 3000),
		subagentReplyLine("a0000000000000006", 20, "example-model-main", "tool_use", "Bash"),
	)

	result, err := readSubagents(path, HerdrSessionStateWorking, subagentsNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionModelName != "example-model-main" {
		t.Errorf("the session's model is %q", result.SessionModelName)
	}
	if result.RequestStartedAt == nil || !result.RequestStartedAt.Equal(subagentsNow.Add(-600*time.Second)) {
		t.Errorf("the request started at %v", result.RequestStartedAt)
	}
	var ids []string
	for _, subagent := range result.Subagents {
		ids = append(ids, subagent.SubagentID)
	}
	// By the time each began.
	if want := []string{"a0000000000000006", "a0000000000000001", "a0000000000000002", "a0000000000000003", "a0000000000000004"}; !slices.Equal(ids, want) {
		t.Fatalf("the agents are %v, want %v", ids, want)
	}
	if result.IsTruncated {
		t.Error("truncated")
	}

	done := subagentByID(result, "a0000000000000001")
	if done.SubagentState != HerdrSubagentStateDone || done.SubagentType != "Explore" || done.TaskText != "Look at the first file" || done.ModelName != "example-model-small" || done.ParentSubagentID != "" || done.SpawnDepth != 1 {
		t.Errorf("the done agent: %+v", done)
	}
	if done.StartedAt == nil || !done.StartedAt.Equal(subagentsNow.Add(-570*time.Second)) || done.EndedAt == nil || !done.EndedAt.Equal(subagentsNow.Add(-500*time.Second)) {
		t.Errorf("the done agent ran from %v to %v", done.StartedAt, done.EndedAt)
	}
	// An agent of a connected server is the server; Read is the most used.
	if want := []string{"Read", "examplesearch", "Grep"}; !slices.Equal(done.ToolNames, want) || done.ToolCallCount != 5 {
		t.Errorf("the done agent's tools: %v, %d calls", done.ToolNames, done.ToolCallCount)
	}

	stopped := subagentByID(result, "a0000000000000002")
	if stopped.SubagentState != HerdrSubagentStateStopped || stopped.SubagentName != "writer" || stopped.EndedAt == nil {
		t.Errorf("the stopped agent: %+v", stopped)
	}
	// Its metadata names no model, so its first reply does.
	if stopped.ModelName != "example-model-main" {
		t.Errorf("the stopped agent's model: %q", stopped.ModelName)
	}

	running := subagentByID(result, "a0000000000000003")
	if running.SubagentState != HerdrSubagentStateRunning || running.EndedAt != nil || !slices.Equal(running.ToolNames, []string{"Read"}) {
		t.Errorf("the running agent: %+v", running)
	}
	nested := subagentByID(result, "a0000000000000004")
	if nested.ParentSubagentID != "a0000000000000003" || nested.SpawnDepth != 2 || nested.SubagentState != HerdrSubagentStateDone {
		t.Errorf("the nested agent: %+v", nested)
	}
	if subagentByID(result, "a0000000000000005") != nil {
		t.Error("an agent of an earlier request is shown")
	}
	if earlier := subagentByID(result, "a0000000000000006"); earlier.SubagentState != HerdrSubagentStateRunning {
		t.Errorf("the long job from before: %+v", earlier)
	}
}

// Older versions of Claude Code never write that an agent finished.
func TestAnAgentWithoutAFinishedMarkIsJudgedByItsHistory(t *testing.T) {
	path, files := writeSubagentSession(t, t.TempDir(), "session-bbbb")
	meta := func(task string) map[string]any {
		return map[string]any{"agentType": "general-purpose", "description": task, "spawnDepth": 1}
	}
	// Ended its turn long ago: done, whatever the session does.
	files.writeSubagent("a0000000000000011", meta("ended"),
		subagentUserLine("a0000000000000011", 500), subagentReplyLine("a0000000000000011", 400, "example-model-main", "end_turn"))
	// Silent for ten minutes without ending its turn.
	files.writeSubagent("a0000000000000012", meta("silent"),
		subagentUserLine("a0000000000000012", 590), subagentReplyLine("a0000000000000012", 500, "example-model-main", "tool_use", "Read"))
	// Wrote a moment ago, and a final reply is not yet in.
	files.writeSubagent("a0000000000000013", meta("writing"),
		subagentUserLine("a0000000000000013", 200), subagentReplyLine("a0000000000000013", 30, "example-model-main", "tool_use", "Read"))
	// Ended its turn, then was handed the end of a command it left running a
	// moment ago, and works again.
	files.writeSubagent("a0000000000000014", meta("resumed"),
		subagentUserLine("a0000000000000014", 300), subagentReplyLine("a0000000000000014", 290, "example-model-main", "end_turn"),
		subagentUserLine("a0000000000000014", 20))

	states := func(sessionState string) map[string]string {
		result, err := readSubagents(path, sessionState, subagentsNow)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		for _, subagent := range result.Subagents {
			found[subagent.TaskText] = subagent.SubagentState
		}
		return found
	}
	idle := states(HerdrSessionStateIdle)
	if idle["ended"] != HerdrSubagentStateDone || idle["silent"] != HerdrSubagentStateStopped || idle["writing"] != HerdrSubagentStateRunning || idle["resumed"] != HerdrSubagentStateRunning {
		t.Errorf("with the session idle: %v", idle)
	}
	for _, sessionState := range []string{HerdrSessionStateWorking, HerdrSessionStateAsking} {
		busy := states(sessionState)
		if busy["ended"] != HerdrSubagentStateDone || busy["silent"] != HerdrSubagentStateRunning || busy["writing"] != HerdrSubagentStateRunning {
			t.Errorf("with the session %s: %v", sessionState, busy)
		}
	}
}

func TestALineStillBeingWrittenIsPassedOver(t *testing.T) {
	path, files := writeSubagentSession(t, t.TempDir(), "session-cccc")
	files.writeSubagent("a0000000000000021", map[string]any{"agentType": "Explore", "description": "Half written", "spawnDepth": 1},
		subagentUserLine("a0000000000000021", 300),
		subagentReplyLine("a0000000000000021", 60, "example-model-main", "end_turn"),
		`{"type":"assistant","timestamp":"2030-01-02T11:59:5`)
	// A metadata file that is not JSON yet, and one without a history.
	if err := os.WriteFile(filepath.Join(files.folder, "agent-a0000000000000022.meta.json"), []byte(`{"agentType":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(files.folder, "agent-a0000000000000023.meta.json"), []byte(`{"agentType":"Explore"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := readSubagents(path, HerdrSessionStateIdle, subagentsNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Subagents) != 1 {
		t.Fatalf("%d agents", len(result.Subagents))
	}
	only := result.Subagents[0]
	if only.SubagentState != HerdrSubagentStateDone || only.EndedAt == nil || !only.EndedAt.Equal(subagentsNow.Add(-60*time.Second)) {
		t.Errorf("%+v", only)
	}
}

func TestOnlyTheNewestHundredAgentsAreGiven(t *testing.T) {
	path, files := writeSubagentSession(t, t.TempDir(), "session-dddd")
	for index := 0; index < subagentsMostReturned+5; index++ {
		subagentId := fmt.Sprintf("a%016d", index)
		// One second apart, the first of them oldest.
		files.writeSubagent(subagentId, map[string]any{"agentType": "Explore", "description": fmt.Sprintf("task %d", index), "spawnDepth": 1, "finished": true},
			subagentUserLine(subagentId, 500-index), subagentReplyLine(subagentId, 400-index, "example-model-main", "end_turn"))
	}
	result, err := readSubagents(path, HerdrSessionStateIdle, subagentsNow)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsTruncated || len(result.Subagents) != subagentsMostReturned {
		t.Fatalf("truncated %v, %d agents", result.IsTruncated, len(result.Subagents))
	}
	if first, last := result.Subagents[0].TaskText, result.Subagents[len(result.Subagents)-1].TaskText; first != "task 5" || last != "task 104" {
		t.Errorf("from %q to %q", first, last)
	}
}

func TestASessionWithNoAgentsFolderHasNoAgents(t *testing.T) {
	path, _ := writeSubagentSession(t, t.TempDir(), "session-eeee")
	result, err := readSubagents(path, HerdrSessionStateIdle, subagentsNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.Subagents == nil || len(result.Subagents) != 0 || result.SessionModelName != "example-model-main" {
		t.Errorf("%+v", result)
	}
	if _, err := readSubagents(filepath.Join(t.TempDir(), "missing.jsonl"), HerdrSessionStateIdle, subagentsNow); err == nil {
		t.Error("a history that is not there was read")
	}
}

// The action reaches the reader through a pane, and a Codex session or one
// without a history answers with no agents rather than a refusal.
func TestTheSubagentsActionAnswersForEachKindOfSession(t *testing.T) {
	home := t.TempDir()
	fake := startFakeHerdr(t, home)
	_, files := writeSubagentSession(t, home, "session-ffff")
	files.writeSubagent("a0000000000000031", map[string]any{"agentType": "Explore", "description": "Look around", "spawnDepth": 1, "finished": true},
		subagentUserLine("a0000000000000031", 500), subagentReplyLine("a0000000000000031", 400, "example-model-main", "end_turn"))
	writeCodexHistory(t, home, "0000-bbbb", codexHistoryAsking)
	fake.setAgent("w1:p1", CodingAgentKindClaude, "idle", "session-ffff", readHerdrFixture(t, "claude-idle"))
	fake.setAgent("w1:p2", CodingAgentKindCodex, "idle", "0000-bbbb", readHerdrFixture(t, "codex-idle"))
	fake.setAgent("w1:p3", CodingAgentKindClaude, "idle", "", readHerdrFixture(t, "claude-idle"))
	herdr := NewHerdr(home)

	// The time is the clock's here, so the agent's age is not asserted.
	answer, err := RunHerdr(context.Background(), herdr, "herdr_subagents", &HerdrArguments{PaneID: "w1:p1"})
	if err != nil {
		t.Fatal(err)
	}
	claude := answer.(*HerdrSubagentsResult)
	if claude.HerdrSession.PaneID != "w1:p1" || len(claude.Subagents) != 1 || claude.Subagents[0].SubagentID != "a0000000000000031" || claude.SessionModelName != "example-model-main" {
		t.Errorf("%+v", claude)
	}
	for _, paneId := range []string{"w1:p2", "w1:p3"} {
		answer, err := RunHerdr(context.Background(), herdr, "herdr_subagents", &HerdrArguments{PaneID: paneId})
		if err != nil {
			t.Fatal(err)
		}
		if empty := answer.(*HerdrSubagentsResult); empty.HerdrSession.PaneID != paneId || empty.Subagents == nil || len(empty.Subagents) != 0 {
			t.Errorf("%s: %+v", paneId, empty)
		}
	}
	if _, err := RunHerdr(context.Background(), herdr, "herdr_subagents", &HerdrArguments{}); err == nil {
		t.Error("an action with no pane was answered")
	}
}

func TestOnlyAConnectedServersNameIsKept(t *testing.T) {
	for name, want := range map[string]string{"Read": "Read", "mcp__examplesearch__find": "examplesearch", "mcp__examplesearch": "mcp__examplesearch", "mcp____find": "mcp____find"} {
		if got := toolNameOfCall(name); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
	}
}
