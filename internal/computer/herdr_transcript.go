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
	}
	if err := json.Unmarshal(line, &record); err != nil || record.IsMeta || record.IsSidechain || record.IsCompactSummary {
		return nil
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
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
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
			turns = append(turns, oneTurn(HerdrTurnRoleTool, block.Name+": "+toolInputLine(block.Input), record.Timestamp)...)
		}
	}
	return turns
}

// codexInjected says a person's message was put there by Codex, not typed:
// its environment, the repository's instructions, its skills.
var codexInjected = regexp.MustCompile(`^\s*(<[a-z_]+[\s>]|# AGENTS\.md instructions|## Skills|A skill is a set of local instructions)`)

func codexTurnsOfLine(line []byte) []*HerdrTurn {
	var record struct {
		Type      string     `json:"type"`
		Timestamp *time.Time `json:"timestamp"`
		Payload   struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Input     string `json:"input"`
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
		return oneTurn(HerdrTurnRoleTool, payload.Name+": "+toolInputLine(json.RawMessage(payload.Arguments)), record.Timestamp)
	case "custom_tool_call":
		return oneTurn(HerdrTurnRoleTool, payload.Name+": "+firstCharacters(payload.Input, transcriptToolCharacters), record.Timestamp)
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

// codexLifecycle is what the end of a Codex history file says about now:
// whether a turn runs, and the question waiting for the person, if any.
type codexLifecycle struct {
	isWorking bool
	question  *HerdrQuestion
}

// readCodexLifecycle reads it from the end of the file. A question is
// waiting when Codex asked one with request_user_input and the person has
// said nothing since: the turn ends at the question, and the answer is the
// person's next message.
func readCodexLifecycle(path string) (*codexLifecycle, error) {
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
	for _, line := range lines {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Type      string `json:"type"`
				Role      string `json:"role"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
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
