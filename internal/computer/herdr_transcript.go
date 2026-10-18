package computer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The history files the coding agents write, read from their end.
//
// Herdr names each pane's session by the coding agent's own identifier,
// and the identifier names the file: Claude Code writes
// ~/.claude/projects/<folder>/<id>.jsonl, Codex
// ~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl. The folder is not
// derived from the pane's directory (a session started in a worktree keeps
// the worktree's folder), so the file is looked up by its identifier.
//
// The files run to a hundred megabytes, most of it tool output, and only
// the last turns are wanted, so they are read backwards, a chunk at a time.
// A line that cannot be read is skipped: the screen is always there to
// fall back on.

// The bounds of a read.
const (
	transcriptFirstChunkBytes = 1 << 20
	transcriptMostChunkBytes  = 16 << 20
	transcriptTurnCharacters  = 4000
	transcriptToolCharacters  = 200
	// transcriptToolDetailCharacters is the most of a tool's input, and of
	// its answer, a read gives.
	transcriptToolDetailCharacters = 4000
	// transcriptLifecycleBytes is how much of a Codex file's end is read
	// for whether a turn runs and whether a question waits, which is said
	// near its end.
	transcriptLifecycleBytes = 512 << 10
)

// The roles of a turn.
const (
	HerdrTurnRolePerson = "person"
	HerdrTurnRoleAgent  = "agent"
	HerdrTurnRoleTool   = "tool"
)

// HerdrTurn is one thing said or done in a coding session: the person's
// words, the agent's, or a tool the agent called, in a line.
type HerdrTurn struct {
	HerdrTurnRole string     `json:"herdrTurnRole"`
	TurnText      string     `json:"turnText"`
	TurnAt        *time.Time `json:"turnAt,omitempty"`
	// ToolInputText and ToolResultText are a tool turn's whole input and
	// what the tool answered, each cut at transcriptToolDetailCharacters;
	// only when a read asks for them.
	ToolInputText  string `json:"toolInputText,omitempty"`
	ToolResultText string `json:"toolResultText,omitempty"`
	// HasToolDetail says a tool turn has an input or a result to show,
	// whether or not the read asked for them.
	HasToolDetail bool `json:"hasToolDetail,omitempty"`

	// toolCallID ties a tool turn to the result that answers it, which a
	// later line of the history file holds.
	toolCallID string
	// isToolResult says this is no turn of its own but the result of the
	// tool turn with toolCallID, to be put on it.
	isToolResult bool
}

// toolTurn is a tool call as a turn: the line that names it, its whole
// input, and the id its result will carry.
func toolTurn(line, inputText, callId string, at *time.Time) []*HerdrTurn {
	turns := oneTurn(HerdrTurnRoleTool, line, at)
	for _, turn := range turns {
		turn.ToolInputText = cutDetail(inputText)
		turn.toolCallID = callId
	}
	return turns
}

// toolResult is what a tool answered, for its call's turn.
func toolResult(callId, text string) []*HerdrTurn {
	if callId == "" {
		return nil
	}
	return []*HerdrTurn{{toolCallID: callId, ToolResultText: cutDetail(strings.TrimSpace(text)), isToolResult: true}}
}

// cutDetail cuts a tool's input or answer to what a reader is shown, and
// says when it did.
func cutDetail(text string) string {
	if len([]rune(text)) <= transcriptToolDetailCharacters {
		return text
	}
	return string([]rune(text)[:transcriptToolDetailCharacters]) + "\n[cut]"
}

// prettyInput is a tool's input as a person reads it: its JSON indented.
func prettyInput(input json.RawMessage) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, input, "", "  "); err != nil {
		return string(input)
	}
	return indented.String()
}

// attachToolResults puts each result on the tool turn it answers, and
// leaves the results out of the turns.
func attachToolResults(turns []*HerdrTurn) []*HerdrTurn {
	callOf := map[string]*HerdrTurn{}
	kept := turns[:0]
	for _, turn := range turns {
		if turn.isToolResult {
			if call := callOf[turn.toolCallID]; call != nil {
				call.ToolResultText = turn.ToolResultText
			}
			continue
		}
		if turn.toolCallID != "" {
			callOf[turn.toolCallID] = turn
		}
		kept = append(kept, turn)
	}
	return kept
}

var codingSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// findTranscript is the history file of a coding session, or empty when it
// has none yet.
func findTranscript(home, codingAgentKind, codingSessionId string) string {
	if !codingSessionIDPattern.MatchString(codingSessionId) {
		return ""
	}
	var pattern string
	switch codingAgentKind {
	case CodingAgentKindClaude:
		pattern = filepath.Join(home, ".claude", "projects", "*", codingSessionId+".jsonl")
	case CodingAgentKindCodex:
		pattern = filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*-"+codingSessionId+".jsonl")
	default:
		return ""
	}
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		return ""
	}
	// The newest, when a session was resumed into a second file.
	sort.Slice(matches, func(left, right int) bool {
		leftInfo, leftErr := os.Stat(matches[left])
		rightInfo, rightErr := os.Stat(matches[right])
		if leftErr != nil || rightErr != nil {
			return matches[left] > matches[right]
		}
		return leftInfo.ModTime().After(rightInfo.ModTime())
	})
	return matches[0]
}

// tailLines are the whole lines in the last byteCount bytes of a file, and
// whether the file holds more before them.
func tailLines(file io.ReaderAt, size, byteCount int64) ([][]byte, bool, error) {
	offset := max(size-byteCount, 0)
	data := make([]byte, size-offset)
	if _, err := file.ReadAt(data, offset); err != nil && err != io.EOF {
		return nil, false, err
	}
	if offset > 0 {
		// The first line is cut by the chunk's edge.
		if index := bytes.IndexByte(data, '\n'); index >= 0 {
			data = data[index+1:]
		} else {
			data = nil
		}
	}
	return bytes.Split(data, []byte("\n")), offset > 0, nil
}

// readTranscriptTail is the last turnCount turns of a history file, oldest
// first, and whether there were more before them.
func readTranscriptTail(path, codingAgentKind string, turnCount int) ([]*HerdrTurn, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	return readTurnsFrom(file, info.Size(), codingAgentKind, turnCount)
}

func readTurnsFrom(file io.ReaderAt, size int64, codingAgentKind string, turnCount int) ([]*HerdrTurn, bool, error) {
	turnCount = max(turnCount, 1)
	for chunk := int64(transcriptFirstChunkBytes); ; chunk *= 2 {
		lines, isCut, err := tailLines(file, size, chunk)
		if err != nil {
			return nil, false, err
		}
		var turns []*HerdrTurn
		for _, line := range lines {
			turns = append(turns, turnsOfLine(codingAgentKind, line)...)
		}
		turns = attachToolResults(turns)
		if len(turns) >= turnCount || !isCut || chunk >= transcriptMostChunkBytes {
			isTruncated := isCut || len(turns) > turnCount
			if len(turns) > turnCount {
				turns = turns[len(turns)-turnCount:]
			}
			if turns == nil {
				turns = []*HerdrTurn{}
			}
			return turns, isTruncated, nil
		}
	}
}

// turnsOfLine is what one line of a history file says, as turns.
func turnsOfLine(codingAgentKind string, line []byte) []*HerdrTurn {
	if len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	if codingAgentKind == CodingAgentKindCodex {
		return codexTurnsOfLine(line)
	}
	return claudeTurnsOfLine(line)
}

// claudeInjected says a person's line was written by Claude Code, not
// typed: a reminder, a command's echo, a hook's output.
var claudeInjected = regexp.MustCompile(`^\s*<(system-reminder|command-|local-command|bash-|user-prompt-submit-hook|task-notification)`)

func claudeTurnsOfLine(line []byte) []*HerdrTurn {
	var record struct {
		Type             string     `json:"type"`
		IsMeta           bool       `json:"isMeta"`
		IsSidechain      bool       `json:"isSidechain"`
		IsCompactSummary bool       `json:"isCompactSummary"`
		Timestamp        *time.Time `json:"timestamp"`
		Message          struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		// Attachment is how Claude Code records a prompt typed while a turn
		// was running: queued, then handed to the turn, never written as a
		// message of its own.
		Attachment struct {
			Type        string          `json:"type"`
			IsHumanTurn bool            `json:"humanTurn"`
			Prompt      json.RawMessage `json:"prompt"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(line, &record); err != nil || record.IsMeta || record.IsSidechain || record.IsCompactSummary {
		return nil
	}
	if record.Type == "attachment" {
		// Only what the person typed: a task's notice and another
		// session's message are queued the same way.
		if record.Attachment.Type != "queued_command" || !record.Attachment.IsHumanTurn {
			return nil
		}
		record.Type, record.Message.Content = "user", record.Attachment.Prompt
	}
	if record.Type != "user" && record.Type != "assistant" {
		return nil
	}
	role := HerdrTurnRoleAgent
	if record.Type == "user" {
		role = HerdrTurnRolePerson
	}
	var text string
	if err := json.Unmarshal(record.Message.Content, &text); err == nil {
		if role == HerdrTurnRolePerson && claudeInjected.MatchString(text) {
			return nil
		}
		return oneTurn(role, text, record.Timestamp)
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ID        string          `json:"id"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(record.Message.Content, &blocks); err != nil {
		return nil
	}
	var turns []*HerdrTurn
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if role == HerdrTurnRolePerson && claudeInjected.MatchString(block.Text) {
				continue
			}
			turns = append(turns, oneTurn(role, block.Text, record.Timestamp)...)
		case "tool_use":
			turns = append(turns, toolTurn(block.Name+": "+toolInputLine(block.Input), prettyInput(block.Input), block.ID, record.Timestamp)...)
		case "tool_result":
			turns = append(turns, toolResult(block.ToolUseID, toolResultText(block.Content))...)
		case "image":
			// The picture itself stays on the computer; the turn says one
			// was there, so a prompt about it still reads.
			if role == HerdrTurnRolePerson {
				turns = append(turns, oneTurn(role, "(a picture)", record.Timestamp)...)
			}
		}
	}
	return turns
}

// toolResultText is what a Claude Code tool answered: text, or text blocks
// with pictures among them, which are named rather than shown.
func toolResultText(content json.RawMessage) string {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, "(a picture)")
		}
	}
	return strings.Join(parts, "\n")
}

// codexInjected says a person's message was put there by Codex, not typed:
// its environment, the repository's instructions, its skills.
var codexInjected = regexp.MustCompile(`^\s*(<[a-z_]+[\s>]|# AGENTS\.md instructions|## Skills|A skill is a set of local instructions)`)

func codexTurnsOfLine(line []byte) []*HerdrTurn {
	var record struct {
		Type      string     `json:"type"`
		Timestamp *time.Time `json:"timestamp"`
		Payload   struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			Input     string          `json:"input"`
			CallID    string          `json:"call_id"`
			Output    json.RawMessage `json:"output"`
			Content   []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &record); err != nil || record.Type != "response_item" {
		return nil
	}
	payload := record.Payload
	switch payload.Type {
	case "message":
		role := HerdrTurnRoleAgent
		switch payload.Role {
		case "user":
			role = HerdrTurnRolePerson
		case "assistant":
		default:
			return nil
		}
		var said []string
		for _, content := range payload.Content {
			if role == HerdrTurnRolePerson && codexInjected.MatchString(content.Text) {
				continue
			}
			said = append(said, content.Text)
		}
		return oneTurn(role, strings.Join(said, "\n\n"), record.Timestamp)
	case "function_call":
		return toolTurn(payload.Name+": "+toolInputLine(json.RawMessage(payload.Arguments)), prettyInput(json.RawMessage(payload.Arguments)), payload.CallID, record.Timestamp)
	case "custom_tool_call":
		return toolTurn(payload.Name+": "+firstCharacters(payload.Input, transcriptToolCharacters), payload.Input, payload.CallID, record.Timestamp)
	case "function_call_output", "custom_tool_call_output":
		// The output is text, or an object whose output field is.
		var text string
		if err := json.Unmarshal(payload.Output, &text); err != nil {
			var wrapped struct {
				Output string `json:"output"`
			}
			if json.Unmarshal(payload.Output, &wrapped) == nil {
				text = wrapped.Output
			}
		}
		return toolResult(payload.CallID, text)
	}
	return nil
}

// oneTurn is a turn of text, cut to size, or none when there is no text.
func oneTurn(role, text string, at *time.Time) []*HerdrTurn {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len([]rune(text)) > transcriptTurnCharacters {
		text = string([]rune(text)[:transcriptTurnCharacters]) + " [cut]"
	}
	return []*HerdrTurn{{HerdrTurnRole: role, TurnText: text, TurnAt: at}}
}

// toolInputLine is a tool's input in a line: its command or path when it
// has one, its JSON otherwise.
func toolInputLine(input json.RawMessage) string {
	var fields map[string]any
	if err := json.Unmarshal(input, &fields); err == nil {
		for _, name := range []string{"command", "cmd", "file_path", "path", "pattern", "description", "prompt"} {
			switch value := fields[name].(type) {
			case string:
				return firstCharacters(value, transcriptToolCharacters)
			case []any:
				parts := make([]string, 0, len(value))
				for _, part := range value {
					parts = append(parts, fmt.Sprint(part))
				}
				return firstCharacters(strings.Join(parts, " "), transcriptToolCharacters)
			}
		}
	}
	return firstCharacters(string(input), transcriptToolCharacters)
}

func firstCharacters(text string, most int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= most {
		return text
	}
	return string([]rune(text)[:most]) + "…"
}

// claudeAnswerOf is what the person answered a question Claude Code asked,
// as Claude Code wrote it into its history: the answers of the latest
// AskUserQuestion whose question is in the text shown, or empty.
func claudeAnswerOf(path, questionText string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	lines, _, err := tailLines(file, info.Size(), transcriptLifecycleBytes)
	if err != nil {
		return ""
	}
	shown := strings.Join(strings.Fields(questionText), " ")
	for index := len(lines) - 1; index >= 0; index-- {
		if !bytes.Contains(lines[index], []byte(`"answers"`)) {
			continue
		}
		var record struct {
			ToolUseResult struct {
				Answers map[string]string `json:"answers"`
			} `json:"toolUseResult"`
		}
		if json.Unmarshal(lines[index], &record) != nil {
			continue
		}
		var said []string
		for question, answer := range record.ToolUseResult.Answers {
			if strings.Contains(shown, strings.Join(strings.Fields(question), " ")) {
				said = append(said, answer)
			}
		}
		if len(said) > 0 {
			sort.Strings(said)
			return strings.Join(said, ", ")
		}
	}
	return ""
}

// codexLifecycle is what the end of a Codex history file says about now:
// whether a turn runs, and the question waiting for the person, if any.
type codexLifecycle struct {
	isWorking bool
	question  *HerdrQuestion
}

// codexLifecycleRead is a lifecycle read from a file as it stood, kept so
// that a file that has not changed since is not read again at every look.
type codexLifecycleRead struct {
	byteCount  int64
	modifiedAt time.Time
	lifecycle  *codexLifecycle
}

var (
	codexLifecycleMutex sync.Mutex
	codexLifecycleReads = map[string]*codexLifecycleRead{}
)

// readCodexLifecycle reads it from the end of the file. A question is
// waiting when Codex asked one with request_user_input, the call was taken
// to wait for the person ({"accepted":true}), and the person has said
// nothing since: the turn ends at the question, and the answer is the
// person's next message. A call answered with anything else (refused in
// this mode, or answered in a picker in the terminal) waits on nobody.
func readCodexLifecycle(path string) (*codexLifecycle, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	codexLifecycleMutex.Lock()
	cached := codexLifecycleReads[path]
	codexLifecycleMutex.Unlock()
	if cached != nil && cached.byteCount == info.Size() && cached.modifiedAt.Equal(info.ModTime()) {
		return cached.lifecycle, nil
	}
	lifecycle, err := readCodexLifecycleNow(path)
	if err != nil {
		return nil, err
	}
	codexLifecycleMutex.Lock()
	if len(codexLifecycleReads) > 256 {
		codexLifecycleReads = map[string]*codexLifecycleRead{}
	}
	codexLifecycleReads[path] = &codexLifecycleRead{byteCount: info.Size(), modifiedAt: info.ModTime(), lifecycle: lifecycle}
	codexLifecycleMutex.Unlock()
	return lifecycle, nil
}

func readCodexLifecycleNow(path string) (*codexLifecycle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	lines, _, err := tailLines(file, info.Size(), transcriptLifecycleBytes)
	if err != nil {
		return nil, err
	}
	lifecycle := &codexLifecycle{}
	questionCallId := ""
	for _, line := range lines {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string          `json:"type"`
				Role      string          `json:"role"`
				Name      string          `json:"name"`
				Arguments string          `json:"arguments"`
				CallID    string          `json:"call_id"`
				Output    json.RawMessage `json:"output"`
				Content   []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		payload := record.Payload
		switch {
		case record.Type == "event_msg" && payload.Type == "task_started":
			lifecycle.isWorking = true
		case record.Type == "event_msg" && (payload.Type == "task_complete" || payload.Type == "turn_aborted"):
			lifecycle.isWorking = false
		case record.Type == "response_item" && payload.Type == "function_call" && strings.HasPrefix(payload.Name, "request_user_input"):
			lifecycle.question = codexQuestionOf(payload.Arguments)
			questionCallId = payload.CallID
		case record.Type == "response_item" && payload.Type == "function_call_output" && payload.CallID != "" && payload.CallID == questionCallId:
			var output string
			if json.Unmarshal(payload.Output, &output) != nil {
				output = string(payload.Output)
			}
			if !strings.Contains(strings.Join(strings.Fields(output), ""), `"accepted":true`) {
				lifecycle.question = nil
			}
		case record.Type == "response_item" && payload.Type == "message" && payload.Role == "user":
			for _, content := range payload.Content {
				if !codexInjected.MatchString(content.Text) {
					lifecycle.question = nil
					break
				}
			}
		}
	}
	return lifecycle, nil
}

// codexQuestionOf is the question request_user_input asked, from its
// arguments: {"questions":[{"title","options":[...]}]}. Several questions
// are asked as one, their titles joined.
func codexQuestionOf(arguments string) *HerdrQuestion {
	var asked struct {
		Questions []struct {
			Title    string `json:"title"`
			Question string `json:"question"`
			Options  []any  `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(arguments), &asked); err != nil || len(asked.Questions) == 0 {
		return nil
	}
	question := &HerdrQuestion{HerdrQuestionKind: HerdrQuestionKindQuestion, IsFromTranscript: true}
	var titles []string
	for _, each := range asked.Questions {
		title := strings.TrimSpace(each.Title)
		if title == "" {
			title = strings.TrimSpace(each.Question)
		}
		titles = append(titles, title)
		for _, option := range each.Options {
			label := ""
			switch value := option.(type) {
			case string:
				label = value
			case map[string]any:
				label, _ = value["label"].(string)
			}
			if label = strings.TrimSpace(label); label != "" {
				question.Options = append(question.Options, HerdrQuestionOption{
					OptionNumber: len(question.Options) + 1, OptionLabel: label, HerdrOptionKind: HerdrOptionKindChoice,
				})
			}
		}
	}
	question.QuestionText = strings.Join(titles, "\n")
	// Answered by typing, so anything may be typed.
	question.Options = append(question.Options, HerdrQuestionOption{
		OptionNumber: len(question.Options) + 1, OptionLabel: "Type something", HerdrOptionKind: HerdrOptionKindFreeText,
	})
	question.QuestionFingerprint = questionFingerprint(question)
	return question
}
