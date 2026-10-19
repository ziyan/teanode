package computer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The agents a Claude Code session started, read from the files Claude Code
// writes beside the session's history.
//
// A session hands parts of a request to agents of its own, and an agent can
// start agents under it. Each one keeps its history next to the session's:
// for ~/.claude/projects/<folder>/<id>.jsonl, a folder
// ~/.claude/projects/<folder>/<id>/subagents/ holds agent-<agent>.jsonl and
// a small agent-<agent>.meta.json, which says what kind of agent it is, the
// task its parent gave it and which agent started it. Nothing here asks
// Claude Code anything or installs a hook: the files have everything, and a
// hook would only see the agents started after it was put in.
//
// Older versions of Claude Code never write that an agent finished, so
// whether one still runs is decided from its history, as subagentStateOf
// says.

// The states of a session's agent.
const (
	HerdrSubagentStateRunning = "running"
	HerdrSubagentStateDone    = "done"
	HerdrSubagentStateStopped = "stopped"
)

// The bounds of reading the agents.
const (
	// subagentsMostReturned is how many agents a read gives, the newest.
	subagentsMostReturned = 100
	// subagentsRunningWithin is how recently an agent's history was written
	// for it to be taken as running when nothing else says.
	subagentsRunningWithin = 120 * time.Second
	// subagentsTailBytes is how much of an agent's history is read from its
	// end for when it was last written and how its last reply stopped.
	subagentsTailBytes = 256 << 10
	// subagentsSessionChunkBytes is how much of the session's own history is
	// read from its end for its model; the chunk doubles while the last
	// person turn is not in it.
	subagentsSessionChunkBytes = 1 << 20
	// subagentsLineBytes is the longest line of an agent's history that is
	// read, and subagentsScanBytes how much of the file is read for the tools
	// it called.
	subagentsLineBytes = 16 << 20
	subagentsScanBytes = 32 << 20
	// subagentsMostToolNames is how many tools an agent is said to have used.
	subagentsMostToolNames = 5
)

// HerdrSubagent is one agent a session started, or one an agent started.
type HerdrSubagent struct {
	SubagentID string `json:"subagentId"`
	// ParentSubagentID is the agent that started this one; empty when the
	// session itself did.
	ParentSubagentID string `json:"parentSubagentId,omitempty"`
	// SubagentType is the kind of agent, such as general-purpose or Explore.
	SubagentType string `json:"subagentType"`
	SubagentName string `json:"subagentName,omitempty"`
	// TaskText is the one-line task its parent gave it.
	TaskText      string `json:"taskText"`
	ModelName     string `json:"modelName"`
	SubagentState string `json:"subagentState"`
	// SpawnDepth is 1 for an agent the session started, 2 for one that
	// agent started, and so on.
	SpawnDepth int        `json:"spawnDepth"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	// EndedAt is when its history was last written; nil while it runs.
	EndedAt *time.Time `json:"endedAt,omitempty"`
	// ToolNames are the tools it called, the most used first; a tool of a
	// connected server is named by the server.
	ToolNames     []string `json:"toolNames"`
	ToolCallCount int      `json:"toolCallCount"`
}

// HerdrSubagentsResult is the agents of a session's current request.
type HerdrSubagentsResult struct {
	HerdrSession     *HerdrSession `json:"herdrSession"`
	SessionModelName string        `json:"sessionModelName"`
	// RequestStartedAt is when the person last spoke to the session.
	RequestStartedAt *time.Time       `json:"requestStartedAt,omitempty"`
	Subagents        []*HerdrSubagent `json:"subagents"`
	// IsTruncated says there were more agents than subagentsMostReturned.
	IsTruncated bool `json:"isTruncated"`
}

// subagentMeta is what Claude Code writes in agent-<agent>.meta.json; it
// writes more, which is not read.
type subagentMeta struct {
	AgentType     string `json:"agentType"`
	Description   string `json:"description"`
	Model         string `json:"model"`
	Name          string `json:"name"`
	ParentAgentID string `json:"parentAgentId"`
	SpawnDepth    int    `json:"spawnDepth"`
	Finished      bool   `json:"finished"`
	StoppedByUser bool   `json:"stoppedByUser"`
}

// subagentRecord is one line of a history file, as far as the agents need
// it. The message is decoded only for a reply, since other lines hold other
// things under that name.
type subagentRecord struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   *time.Time      `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
}

type subagentReply struct {
	Model      string          `json:"model"`
	StopReason *string         `json:"stop_reason"`
	Content    json.RawMessage `json:"content"`
}

// subagentCandidate is an agent found in the folder, before the tools it
// called are counted.
type subagentCandidate struct {
	subagent    *HerdrSubagent
	historyPath string
}

// readSubagents is the agents of the Claude Code session whose history is
// at transcriptPath, those started for its current request: at or after the
// person's last turn, and those still running from before it. sessionState
// is the session's own, which says an agent without any other sign of
// having ended still works.
func readSubagents(transcriptPath, sessionState string, now time.Time) (*HerdrSubagentsResult, error) {
	sessionModelName, requestStartedAt, err := readSessionRequest(transcriptPath)
	if err != nil {
		return nil, err
	}
	result := &HerdrSubagentsResult{SessionModelName: sessionModelName, RequestStartedAt: requestStartedAt, Subagents: []*HerdrSubagent{}}

	folder := filepath.Join(strings.TrimSuffix(transcriptPath, ".jsonl"), "subagents")
	metaPaths, _ := filepath.Glob(filepath.Join(folder, "agent-*.meta.json"))
	var candidates []*subagentCandidate
	for _, metaPath := range metaPaths {
		// An agent whose history was last written before the request began
		// has nothing to do with it, whatever it says: one of an earlier
		// request left unfinished would otherwise run whenever the session
		// works. A long session has hundreds of these, so this is decided
		// before any of its files is read.
		if requestStartedAt != nil {
			if info, err := os.Stat(strings.TrimSuffix(metaPath, ".meta.json") + ".jsonl"); err == nil && info.ModTime().Before(*requestStartedAt) {
				continue
			}
		}
		candidate := readSubagentCandidate(metaPath, sessionState, now)
		if candidate == nil {
			continue
		}
		isFromEarlier := requestStartedAt != nil && candidate.subagent.StartedAt != nil && candidate.subagent.StartedAt.Before(*requestStartedAt)
		if isFromEarlier && candidate.subagent.SubagentState != HerdrSubagentStateRunning {
			continue
		}
		candidates = append(candidates, candidate)
	}
	// An agent whose history has no time sorts first, as the oldest.
	startOf := func(candidate *subagentCandidate) time.Time {
		if candidate.subagent.StartedAt == nil {
			return time.Time{}
		}
		return *candidate.subagent.StartedAt
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if !startOf(candidates[left]).Equal(startOf(candidates[right])) {
			return startOf(candidates[left]).Before(startOf(candidates[right]))
		}
		return candidates[left].subagent.SubagentID < candidates[right].subagent.SubagentID
	})
	if len(candidates) > subagentsMostReturned {
		candidates = candidates[len(candidates)-subagentsMostReturned:]
		result.IsTruncated = true
	}
	// The tools are counted last, from whole files, and only for those kept.
	for _, candidate := range candidates {
		countSubagentTools(candidate)
		result.Subagents = append(result.Subagents, candidate.subagent)
	}
	return result, nil
}

// readSessionRequest is the model the session last answered with and when
// the person last spoke to it, from the end of its history. The lines of
// agents that appear among the session's own are not the session's.
func readSessionRequest(transcriptPath string) (string, *time.Time, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	var modelName string
	for chunk := int64(subagentsSessionChunkBytes); ; chunk *= 2 {
		lines, isCut, err := tailLines(file, info.Size(), chunk)
		if err != nil {
			return "", nil, err
		}
		var requestStartedAt *time.Time
		for index := len(lines) - 1; index >= 0; index-- {
			line := lines[index]
			if modelName == "" {
				modelName = modelNameOfLine(line)
			}
			if requestStartedAt == nil {
				requestStartedAt = personTurnAtOfLine(line)
			}
			if modelName != "" && requestStartedAt != nil {
				break
			}
		}
		if requestStartedAt != nil || !isCut || chunk >= transcriptMostChunkBytes {
			return modelName, requestStartedAt, nil
		}
	}
}

// modelNameOfLine is the model a session's own reply was made by, or empty.
// Claude Code puts "<synthetic>" on replies it wrote itself.
func modelNameOfLine(line []byte) string {
	if !bytes.Contains(line, []byte(`"model"`)) {
		return ""
	}
	var record subagentRecord
	if err := json.Unmarshal(line, &record); err != nil || record.IsSidechain || record.Type != "assistant" {
		return ""
	}
	var reply subagentReply
	if err := json.Unmarshal(record.Message, &reply); err != nil || strings.HasPrefix(reply.Model, "<") {
		return ""
	}
	return reply.Model
}

// personTurnAtOfLine is when the person spoke, if this line is what they
// said.
func personTurnAtOfLine(line []byte) *time.Time {
	if !bytes.Contains(line, []byte(`"user"`)) && !bytes.Contains(line, []byte(`"attachment"`)) {
		return nil
	}
	for _, turn := range claudeTurnsOfLine(line) {
		if turn.HerdrTurnRole == HerdrTurnRolePerson && turn.TurnAt != nil {
			return turn.TurnAt
		}
	}
	return nil
}

// readSubagentCandidate is the agent whose metadata is at metaPath, or nil
// when its files cannot be read: an agent that is only half written is
// there the next time.
func readSubagentCandidate(metaPath, sessionState string, now time.Time) *subagentCandidate {
	encodedMeta, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}
	var meta subagentMeta
	if err := json.Unmarshal(encodedMeta, &meta); err != nil {
		return nil
	}
	subagentId := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(metaPath), "agent-"), ".meta.json")
	historyPath := strings.TrimSuffix(metaPath, ".meta.json") + ".jsonl"
	file, err := os.Open(historyPath)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil
	}

	startedAt := readSubagentStartedAt(file)
	lastWrittenAt, isReplyEnded := readSubagentEnd(file, info.Size())
	if lastWrittenAt == nil {
		modifiedAt := info.ModTime()
		lastWrittenAt = &modifiedAt
	}
	subagentState := subagentStateOf(&meta, isReplyEnded, sessionState, lastWrittenAt, now)
	subagent := &HerdrSubagent{
		SubagentID:       subagentId,
		ParentSubagentID: meta.ParentAgentID,
		SubagentType:     meta.AgentType,
		SubagentName:     meta.Name,
		TaskText:         strings.TrimSpace(meta.Description),
		ModelName:        meta.Model,
		SubagentState:    subagentState,
		SpawnDepth:       meta.SpawnDepth,
		StartedAt:        startedAt,
		ToolNames:        []string{},
	}
	if subagentState != HerdrSubagentStateRunning {
		subagent.EndedAt = lastWrittenAt
	}
	return &subagentCandidate{subagent: subagent, historyPath: historyPath}
}

// subagentStateOf decides whether an agent runs. What Claude Code wrote in
// the metadata is taken as it is. Otherwise a reply that ended its turn is
// the agent done, whatever the session is doing since. An agent still
// writing, or whose session works or asks, runs. What is left stopped
// without finishing.
func subagentStateOf(meta *subagentMeta, isReplyEnded bool, sessionState string, lastWrittenAt *time.Time, now time.Time) string {
	switch {
	case meta.Finished:
		return HerdrSubagentStateDone
	case meta.StoppedByUser:
		return HerdrSubagentStateStopped
	case isReplyEnded:
		return HerdrSubagentStateDone
	case now.Sub(*lastWrittenAt) <= subagentsRunningWithin, sessionState == HerdrSessionStateWorking, sessionState == HerdrSessionStateAsking:
		return HerdrSubagentStateRunning
	}
	return HerdrSubagentStateStopped
}

// readSubagentStartedAt is the time on the first line of an agent's history
// that has one.
func readSubagentStartedAt(file *os.File) *time.Time {
	var startedAt *time.Time
	scanSubagentHistory(file, func(line []byte) bool {
		var record subagentRecord
		if json.Unmarshal(line, &record) != nil || record.Timestamp == nil {
			return true
		}
		startedAt = record.Timestamp
		return false
	})
	return startedAt
}

// readSubagentEnd is when the last line of an agent's history that can be
// read was written, and whether the last reply in its end ended its turn.
// The end may be a line only partly written, which is passed over.
func readSubagentEnd(file *os.File, size int64) (*time.Time, bool) {
	lines, _, err := tailLines(file, size, subagentsTailBytes)
	if err != nil {
		return nil, false
	}
	var lastWrittenAt *time.Time
	isReplyFound, isReplyEnded := false, false
	for index := len(lines) - 1; index >= 0; index-- {
		var record subagentRecord
		if len(bytes.TrimSpace(lines[index])) == 0 || json.Unmarshal(lines[index], &record) != nil {
			continue
		}
		if lastWrittenAt == nil {
			lastWrittenAt = record.Timestamp
		}
		// Something handed to the agent after its last reply, such as the
		// end of a command it left running, starts it again: that reply no
		// longer ends anything.
		if !isReplyFound && record.Type == "user" {
			isReplyFound = true
		}
		if !isReplyFound && record.Type == "assistant" {
			isReplyFound = true
			var reply subagentReply
			if json.Unmarshal(record.Message, &reply) == nil {
				isReplyEnded = reply.StopReason != nil && *reply.StopReason == "end_turn"
			}
		}
		if lastWrittenAt != nil && isReplyFound {
			break
		}
	}
	return lastWrittenAt, isReplyEnded
}

// countSubagentTools reads an agent's history through once for the tools it
// called, and for the model it answered with: the metadata holds the model
// asked for, often a short name such as "sonnet", and is kept only when no
// reply names one.
func countSubagentTools(candidate *subagentCandidate) {
	file, err := os.Open(candidate.historyPath)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	subagent := candidate.subagent
	callCounts := map[string]int{}
	hasReplyModel := false
	scanSubagentHistory(file, func(line []byte) bool {
		isToolCall := bytes.Contains(line, []byte(`"tool_use"`))
		if !isToolCall && hasReplyModel {
			return true
		}
		var record subagentRecord
		if json.Unmarshal(line, &record) != nil || record.Type != "assistant" {
			return true
		}
		var reply subagentReply
		if json.Unmarshal(record.Message, &reply) != nil {
			return true
		}
		if !hasReplyModel && reply.Model != "" && !strings.HasPrefix(reply.Model, "<") {
			subagent.ModelName = reply.Model
			hasReplyModel = true
		}
		var blocks []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if json.Unmarshal(reply.Content, &blocks) != nil {
			return true
		}
		for _, block := range blocks {
			if block.Type == "tool_use" && block.Name != "" {
				callCounts[toolNameOfCall(block.Name)]++
				subagent.ToolCallCount++
			}
		}
		return true
	})
	toolNames := make([]string, 0, len(callCounts))
	for toolName := range callCounts {
		toolNames = append(toolNames, toolName)
	}
	sort.Slice(toolNames, func(left, right int) bool {
		if callCounts[toolNames[left]] != callCounts[toolNames[right]] {
			return callCounts[toolNames[left]] > callCounts[toolNames[right]]
		}
		return toolNames[left] < toolNames[right]
	})
	subagent.ToolNames = toolNames[:min(len(toolNames), subagentsMostToolNames)]
}

// toolNameOfCall is a tool as a person reads it: a tool of a connected
// server, called mcp__<server>__<tool>, is the server.
func toolNameOfCall(name string) string {
	if parts := strings.SplitN(name, "__", 3); len(parts) == 3 && parts[0] == "mcp" && parts[1] != "" {
		return parts[1]
	}
	return name
}

// scanSubagentHistory hands each line of a history file from its start to
// handle, until it returns false, a line is longer than subagentsLineBytes,
// or subagentsScanBytes were read.
func scanSubagentHistory(file *os.File, handle func(line []byte) bool) {
	if _, err := file.Seek(0, 0); err != nil {
		return
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), subagentsLineBytes)
	var readBytes int64
	for scanner.Scan() {
		line := scanner.Bytes()
		readBytes += int64(len(line)) + 1
		if readBytes > subagentsScanBytes || !handle(line) {
			return
		}
	}
}
