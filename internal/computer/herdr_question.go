package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Questions a coding agent waits on, recognized from its screen.
//
// Claude Code and Codex both stop and draw a form when they need the
// person: a question with numbered options, a command to approve, a plan
// to accept. Herdr's own state misses some of them (a Claude Code
// question form is reported idle), so the screen is read here instead,
// and the screen is also where the answer is pressed, so it is what has to
// match.
//
// A form is the last block of numbered options on the screen, with the
// cursor on one of them and the form's own footer below: a list of numbers
// the agent merely printed has neither, and a form scrolled away is not
// the last block.

// The kinds of question.
const (
	HerdrQuestionKindQuestion     = "question"
	HerdrQuestionKindToolApproval = "toolApproval"
	HerdrQuestionKindPlanApproval = "planApproval"
)

// The kinds of option: one to choose, one that takes typed text, and
// Claude Code's way out of the form into a conversation about it.
const (
	HerdrOptionKindChoice   = "choice"
	HerdrOptionKindFreeText = "freeText"
	HerdrOptionKindChat     = "chat"
)

// The coding agents, as herdr names them.
const (
	CodingAgentKindClaude = "claude"
	CodingAgentKindCodex  = "codex"
)

// HerdrQuestion is a question a coding agent waits on.
type HerdrQuestion struct {
	// QuestionFingerprint names this question as it stands: an answer
	// carrying another is refused, so a late answer from far away never
	// presses keys into whatever came after it.
	QuestionFingerprint string                `json:"questionFingerprint"`
	HerdrQuestionKind   string                `json:"herdrQuestionKind"`
	QuestionText        string                `json:"questionText"`
	IsMultipleChoice    bool                  `json:"isMultipleChoice"`
	Options             []HerdrQuestionOption `json:"options"`
	// IsFromTranscript says the question was read from the history file,
	// not from a form on the screen, and is answered by typing.
	IsFromTranscript bool `json:"isFromTranscript,omitempty"`
}

// HerdrQuestionOption is one option of a question, numbered as the form
// numbers it.
type HerdrQuestionOption struct {
	OptionNumber      int    `json:"optionNumber"`
	OptionLabel       string `json:"optionLabel"`
	OptionDescription string `json:"optionDescription,omitempty"`
	HerdrOptionKind   string `json:"herdrOptionKind"`
}

var (
	// An option line: the cursor perhaps, a number, a tick box perhaps,
	// and the label.
	herdrOptionPattern = regexp.MustCompile(`^(\s*)([❯›>]?)(\s*)(\d{1,2})\.\s+(\[[ x✔✓]\]\s*)?(.*)$`)
	// A rule drawn across the screen: Claude Code draws one above its
	// forms, with a session's name in it sometimes.
	herdrRulePattern = regexp.MustCompile(`^\s*[─━]{8,}`)
)

// herdrFormFooters are what a form says below its options, one of which
// every form has. Matched with the screen's line breaks taken out, since a
// narrow pane breaks the footer across lines.
var herdrFormFooters = []string{
	"Esc to cancel",
	"Enter to select",
	"Enter to confirm",
	"esc to cancel",
	"enter to confirm",
	"enter continue",
	"ctrl+g to edit",
	"Would you like to proceed?",
	"Ready to submit your answers?",
}

// recognizeQuestion finds the form a coding agent waits on at the bottom of
// its screen.
func recognizeQuestion(codingAgentKind, screenText string) *HerdrQuestion {
	lines := strings.Split(strings.ReplaceAll(screenText, "\r", ""), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	// The last option line, and the block of options it ends.
	last := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if herdrOptionPattern.MatchString(lines[index]) {
			last = index
			break
		}
	}
	if last < 0 {
		return nil
	}
	// What is below the options is the form's own: its footer, a rule, a
	// last option kept apart. A composer under them means the list was
	// printed and the agent waits for a prompt, not an answer.
	below := strings.Join(lines[last+1:], " ")
	if len(lines)-last > 8 || !hasFooter(below+" "+strings.Join(lines[max(last-30, 0):last], " ")) {
		return nil
	}
	// Up from the last option to the first: options, the lines indented
	// under them, the rules and blank lines between them, and "Submit"
	// under the options of a form that takes several.
	first := last
	hasCursor := false
	labelColumn := 0
	for index := last; index >= 0; index-- {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if match := herdrOptionPattern.FindStringSubmatch(line); match != nil {
			first = index
			labelColumn = optionLabelColumn(match)
			if match[2] != "" {
				hasCursor = true
			}
			continue
		}
		if trimmed == "" || trimmed == "Submit" || herdrRulePattern.MatchString(line) || isContinuation(line, labelColumn) {
			continue
		}
		break
	}
	if !hasCursor {
		return nil
	}
	// Down again, now that the block is known: what is indented under an
	// option is its description on a question form, and the rest of its
	// label on an approval, which has none.
	isQuestionForm := strings.Contains(below, "Enter to select") || strings.Contains(below, "to navigate")
	type parsed struct {
		number      int
		label       []string
		detail      []string
		isTicked    bool
		labelColumn int
	}
	var options []*parsed
	// The lines after the last option that are indented under it are
	// still its own.
	end := last + 1
	for end < len(lines) && strings.TrimSpace(lines[end]) != "" && isContinuation(lines[end], labelColumn) {
		end++
	}
	for _, line := range lines[first:end] {
		if match := herdrOptionPattern.FindStringSubmatch(line); match != nil {
			number, _ := strconv.Atoi(match[4])
			options = append(options, &parsed{number: number, label: []string{strings.TrimSpace(match[6])},
				isTicked: match[5] != "", labelColumn: optionLabelColumn(match)})
			continue
		}
		trimmed := strings.TrimSpace(line)
		current := options[len(options)-1]
		// A key hint under an option ("shift+tab to approve with this
		// feedback") says how to press it, not what it is.
		if trimmed == "" || trimmed == "Submit" || herdrRulePattern.MatchString(line) || !isContinuation(line, current.labelColumn) ||
			strings.HasPrefix(trimmed, "shift+tab") {
			continue
		}
		if isQuestionForm {
			current.detail = append(current.detail, trimmed)
		} else {
			current.label = append(current.label, trimmed)
		}
	}
	if len(options) < 2 {
		return nil
	}

	question := &HerdrQuestion{}
	for _, option := range options {
		label := strings.Join(option.label, " ")
		kind := HerdrOptionKindChoice
		switch {
		case strings.HasPrefix(label, "Type something"):
			kind = HerdrOptionKindFreeText
			label = "Type something"
		case strings.HasPrefix(label, "Tell Claude what to change"):
			kind = HerdrOptionKindFreeText
		case strings.HasPrefix(label, "Chat about this"):
			kind = HerdrOptionKindChat
		}
		if option.isTicked {
			question.IsMultipleChoice = true
		}
		question.Options = append(question.Options, HerdrQuestionOption{
			OptionNumber: option.number, OptionLabel: label,
			OptionDescription: strings.Join(option.detail, " "), HerdrOptionKind: kind,
		})
	}
	question.QuestionText = questionTextAbove(lines, first)
	text := question.QuestionText
	switch {
	case strings.Contains(text, "Would you like to proceed?") && strings.Contains(text, "plan"):
		question.HerdrQuestionKind = HerdrQuestionKindPlanApproval
	case strings.Contains(text, "Do you want to") || strings.Contains(text, "Would you like to run") ||
		strings.Contains(text, "Would you like to make") || strings.Contains(text, "Would you like to allow"):
		question.HerdrQuestionKind = HerdrQuestionKindToolApproval
	default:
		question.HerdrQuestionKind = HerdrQuestionKindQuestion
	}
	question.QuestionFingerprint = questionFingerprint(question)
	return question
}

// hasFooter says the text holds a form's footer.
func hasFooter(text string) bool {
	collapsed := strings.Join(strings.Fields(text), " ")
	for _, footer := range herdrFormFooters {
		if strings.Contains(collapsed, footer) {
			return true
		}
	}
	return false
}

// optionLabelColumn is the column an option's label starts at.
func optionLabelColumn(match []string) int {
	return len([]rune(match[1] + match[2] + match[3] + match[4] + ". " + match[5]))
}

// isContinuation says a line is indented under an option, to its label's
// column or further.
func isContinuation(line string, labelColumn int) bool {
	indent := len([]rune(line)) - len([]rune(strings.TrimLeft(line, " ")))
	return indent >= labelColumn-1 && labelColumn > 2
}

// questionTextAbove is what a form says above its first option: back to
// the rule Claude Code draws over its forms, or to where Codex starts its
// question, without the rules, the tab bar and the form's header. A plan
// to approve is kept whole, since it is what is being approved.
func questionTextAbove(lines []string, first int) string {
	start := max(first-14, 0)
	for index := first - 1; index >= max(first-40, 0); index-- {
		trimmed := strings.TrimSpace(lines[index])
		if herdrRulePattern.MatchString(lines[index]) {
			start = index + 1
			break
		}
		if strings.HasPrefix(trimmed, "Would you like to") || strings.HasPrefix(trimmed, "Do you want to") {
			start = index
		}
	}
	// A plan sits above a second rule, under "Ready to code?".
	for index := start - 1; index >= max(start-60, 0); index-- {
		if strings.TrimSpace(lines[index]) == "Ready to code?" {
			start = index + 1
			break
		}
	}
	var said []string
	for _, line := range lines[start:first] {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "│"))
		switch {
		case trimmed == "", herdrRulePattern.MatchString(line), strings.HasPrefix(trimmed, "╌"),
			strings.HasPrefix(trimmed, "←"), strings.HasPrefix(trimmed, "☐"), strings.HasPrefix(trimmed, "☒"),
			strings.HasPrefix(trimmed, "Tip:"), strings.HasPrefix(trimmed, "— choose"):
			continue
		}
		said = append(said, trimmed)
	}
	return strings.Join(said, "\n")
}

// questionFingerprint names a question by what it asks and offers.
func questionFingerprint(question *HerdrQuestion) string {
	hash := sha256.New()
	hash.Write([]byte(question.HerdrQuestionKind + "\n" + question.QuestionText + "\n"))
	for _, option := range question.Options {
		hash.Write([]byte(strconv.Itoa(option.OptionNumber) + ". " + option.OptionLabel + "\n"))
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}
