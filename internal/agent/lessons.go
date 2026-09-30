package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Lessons from verified work.
//
// When the agent does work by running commands, what worked and what to
// avoid is worth keeping for the next time the same kind of work comes up.
// It is kept as a fact of kind lesson, under lessons/<topic>, and only
// where a command's result showed it: the model names the commands that
// verified a lesson, and the code keeps the lesson only when one of them
// ended with exit code 0. What the assistant said about its work, however
// confident, is not evidence, so a lesson never comes from a conversation
// without a command that bears it out. Lessons recalled into a turn are in
// the prompt only, never in the transcript, so a turn that repeats one
// without running anything cannot file it again.

// The bounds of the lessons pass.
const (
	// lessonCount is how many lessons one window may file.
	lessonCount = 3

	// lessonCallArgumentLength and lessonCallOutputLength are how much of
	// a command and of what it printed the model is shown; the end of the
	// output, where failures and summaries are.
	lessonCallArgumentLength = 400
	lessonCallOutputLength   = 800

	// lessonTranscriptLength bounds what the lessons pass is shown of a
	// window: a long stretch of work keeps its latest commands, where the
	// approach that worked is.
	lessonTranscriptLength = 40000

	// lessonFieldLength bounds each part of a lesson.
	lessonFieldLength = 400

	// lessonsRoot is where lessons are filed, a page for each topic.
	lessonsRoot = "lessons"

	// lessonDuplicateScore is how near in meaning an existing lesson has
	// to be for a new one to be the same lesson said again.
	lessonDuplicateScore = 0.92

	// recallLessons is how many lessons a turn is shown, and
	// recallLessonTokens what they may cost.
	recallLessons      = 2
	recallLessonTokens = 300
)

// lessonCall is one command the conversation ran and how it ended.
type lessonCall struct {
	Number    int
	ToolName  string
	Arguments string

	// ExitCode is the command's exit code; IsFinished says it ended, as
	// opposed to going on in the background or answering with no code.
	ExitCode   int
	IsFinished bool
	Output     string
}

// hasSucceeded says the command ended, with exit code 0.
func (self *lessonCall) hasSucceeded() bool {
	return self != nil && self.IsFinished && self.ExitCode == 0
}

// lessonCallsOf pairs each tool call in the messages with its result, and
// keeps the ones whose result carries an exit code: the commands.
func lessonCallsOf(messages []*models.AgentMessage) ([]*lessonCall, map[string]*lessonCall) {
	byToolCallId := map[string]*lessonCall{}
	var calls []*lessonCall
	pending := map[string]models.AgentToolCall{}
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			pending[call.ID] = call
		}
		if message.Role != string(llm.RoleTool) || message.ToolCallID == "" {
			continue
		}
		call, isKnown := pending[message.ToolCallID]
		if !isKnown {
			continue
		}
		exitCode, hasExitCode, isBackground, output := readCommandResult(message.Content)
		if !hasExitCode && !isBackground {
			continue
		}
		if runes := []rune(strings.TrimSpace(output)); len(runes) > lessonCallOutputLength {
			output = "…" + string(runes[len(runes)-lessonCallOutputLength:])
		}
		found := &lessonCall{
			Number: len(calls) + 1, ToolName: call.Name, Arguments: cutRunes(call.Arguments, lessonCallArgumentLength),
			ExitCode: exitCode, IsFinished: hasExitCode && !isBackground, Output: strings.TrimSpace(output),
		}
		calls = append(calls, found)
		byToolCallId[message.ToolCallID] = found
	}
	return calls, byToolCallId
}

// lastRunes is the end of a text, so many characters of it, marked as cut
// where it was.
func lastRunes(text string, count int) string {
	runes := []rune(text)
	if len(runes) <= count {
		return text
	}
	return "[the work before this is left out]\n" + string(runes[len(runes)-count:])
}

// exitCodePattern finds a command's exit code in a result too long to have
// been kept whole: the answer is cut to size, which leaves JSON that does not
// parse, and the code comes before the output it printed.
var exitCodePattern = regexp.MustCompile(`"(?:exitCode|exit_code)"\s*:\s*(-?\d+)`)

// readCommandResult reads how a command ended from a tool result as the
// transcript keeps it: fenced as data from outside, and cut when it was
// long. The shell says exitCode and the terminal exit_code; a command still
// running says backgroundId instead. The output is what it printed, or the
// result's text when it could not be read as JSON.
func readCommandResult(content string) (exitCode int, hasExitCode, isBackground bool, output string) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, untrustedOpen) {
		content = strings.TrimPrefix(content, untrustedOpen)
		if index := strings.LastIndex(content, untrustedClose); index >= 0 {
			content = content[:index]
		}
		content = strings.TrimSpace(content)
	}
	var result map[string]any
	if json.Unmarshal([]byte(content), &result) == nil {
		for _, key := range []string{"exitCode", "exit_code"} {
			if code, isNumber := result[key].(float64); isNumber {
				exitCode, hasExitCode = int(code), true
			}
		}
		_, isBackground = result["backgroundId"]
		for _, key := range []string{"stdout", "output", "screen", "stderr"} {
			if text, isText := result[key].(string); isText && strings.TrimSpace(text) != "" {
				output += strings.TrimSpace(text) + "\n"
			}
		}
		return exitCode, hasExitCode, isBackground, output
	}
	if !strings.HasPrefix(content, "{") {
		return 0, false, false, ""
	}
	if found := exitCodePattern.FindStringSubmatch(content); found != nil {
		if code, err := strconv.Atoi(found[1]); err == nil {
			exitCode, hasExitCode = code, true
		}
	}
	isBackground = strings.Contains(content, `"backgroundId"`)
	return exitCode, hasExitCode, isBackground, content
}

// lessonTranscript is the window as the lessons pass reads it: what was
// said, and each command with its number, what it printed and how it
// ended.
func lessonTranscript(messages []*models.AgentMessage, byToolCallId map[string]*lessonCall) string {
	var builder strings.Builder
	for _, message := range messages {
		switch message.Role {
		case string(llm.RoleUser):
			if text := strings.TrimSpace(message.Content); text != "" {
				builder.WriteString("them: " + unclosable(cutRunes(text, rememberMessageCharacters)) + "\n\n")
			}
		case string(llm.RoleAssistant):
			if text := strings.TrimSpace(message.Content); text != "" {
				builder.WriteString("you: " + unclosable(cutRunes(text, rememberMessageCharacters)) + "\n\n")
			}
		case string(llm.RoleTool):
			call := byToolCallId[message.ToolCallID]
			if call == nil {
				continue
			}
			ended := "still running in the background"
			if call.IsFinished {
				ended = "exit code " + strconv.Itoa(call.ExitCode)
			}
			fmt.Fprintf(&builder, "[command %d] %s %s\n%s\n(%s)\n\n", call.Number, call.ToolName,
				unclosable(call.Arguments), unclosable(call.Output), ended)
		}
	}
	return strings.TrimSpace(builder.String())
}

// lessonAnswer is what the model said the work taught.
type lessonAnswer struct {
	Lessons []struct {
		AppliesWhen     string `json:"appliesWhen"`
		Approach        string `json:"approach"`
		Avoid           string `json:"avoid"`
		Verification    string `json:"verification"`
		Scope           string `json:"scope"`
		VerifiedByCalls []int  `json:"verifiedByCalls"`
		Topic           string `json:"topic"`
	} `json:"lessons"`
}

// verifiedLesson is a lesson the code kept: its text, the page it goes on,
// and the commands that bear it out.
type verifiedLesson struct {
	Text     string
	Path     string
	Evidence []models.Evidence
}

// verifyLessons keeps the lessons a command bears out: at least one of the
// commands each names ended with exit code 0. A lesson naming none, or only
// commands that failed or are still running, is the model's word alone and
// is dropped.
func verifyLessons(answer lessonAnswer, calls []*lessonCall, conversationId string) []verifiedLesson {
	var kept []verifiedLesson
	for _, lesson := range answer.Lessons {
		if len(kept) >= lessonCount {
			break
		}
		appliesWhen := cutRunes(strings.TrimSpace(lesson.AppliesWhen), lessonFieldLength)
		approach := cutRunes(strings.TrimSpace(lesson.Approach), lessonFieldLength)
		if appliesWhen == "" || approach == "" {
			continue
		}
		var evidence []models.Evidence
		for _, number := range lesson.VerifiedByCalls {
			if number < 1 || number > len(calls) || !calls[number-1].hasSucceeded() || len(evidence) >= 2 {
				continue
			}
			call := calls[number-1]
			evidence = append(evidence, models.Evidence{Kind: models.EvidenceConversation, ID: conversationId,
				Quote: cutRunes(call.ToolName+" "+call.Arguments, 200) + " → exit code 0"})
		}
		if len(evidence) == 0 {
			continue
		}
		text := "When " + strings.TrimSuffix(appliesWhen, ".") + ": " + strings.TrimSuffix(approach, ".") + "."
		if avoid := cutRunes(strings.TrimSpace(lesson.Avoid), lessonFieldLength); avoid != "" {
			text += " Avoid: " + strings.TrimSuffix(avoid, ".") + "."
		}
		if verification := cutRunes(strings.TrimSpace(lesson.Verification), lessonFieldLength); verification != "" {
			text += " Verified by: " + strings.TrimSuffix(verification, ".") + "."
		}
		if scope := cutRunes(strings.TrimSpace(lesson.Scope), lessonFieldLength); scope != "" {
			text += " Held for: " + strings.TrimSuffix(scope, ".") + "."
		}
		topic := models.Slug(lesson.Topic)
		if topic == "" {
			topic = "general"
		}
		kept = append(kept, verifiedLesson{Text: text, Path: lessonsRoot + "/" + topic, Evidence: evidence})
	}
	return kept
}

// readLessons reads a window of a conversation for lessons from the work
// done in it, and files the ones a command bears out. A window in which no
// command succeeded is not read at all. It says how many it filed; a
// failure is the lessons' own and leaves the rest of the conversation's
// filing as it was.
func (self *Agent) readLessons(ctx context.Context, run *Run, conversation *models.AgentConversation, window []*models.AgentMessage) (int, error) {
	calls, byToolCallId := lessonCallsOf(window)
	isAnySucceeded := false
	for _, call := range calls {
		if call.hasSucceeded() {
			isAnySucceeded = true
		}
	}
	if !isAnySucceeded {
		return 0, nil
	}
	prompt, err := render("lessons.txt", map[string]any{
		"PersonName":        personName(run.Owner),
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"Transcript":        lastRunes(lessonTranscript(window, byToolCallId), lessonTranscriptLength),
	})
	if err != nil {
		return 0, err
	}
	thinking, err := self.oneShot(ctx, run, "Reading lessons from "+chatName(conversation), prompt, models.AgentJobRemember, config.AgentWorkScan)
	if err != nil {
		return 0, fmt.Errorf("asking the model: %w", err)
	}
	answer := readModelAnswer[lessonAnswer](thinking.Text, "lessons")
	if !answer.IsValid {
		return 0, fmt.Errorf("the lessons were not readable: %s", answer.Problem)
	}
	lessons := verifyLessons(answer.Value, calls, conversation.ID)

	filed := 0
	var accepted []*meaning
	for _, lesson := range lessons {
		lessonMeaning, isKnown := self.isLessonKnown(ctx, run.Agent.ID, lesson.Text, accepted)
		if isKnown {
			continue
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			page, err := tx.GetAgentNode(run.Agent.ID, lesson.Path)
			if err != nil {
				return err
			}
			if page == nil {
				name := strings.ReplaceAll(models.LastSegment(lesson.Path), "-", " ")
				if page, err = tx.PutAgentNode(&models.AgentNode{
					AgentID: run.Agent.ID, Path: lesson.Path, Kind: models.NodeTopic, Name: name,
					Summary: "What worked, and what to avoid, when doing " + name + ".",
				}); err != nil {
					return err
				}
			}
			now := time.Now()
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: run.Agent.ID, NodeID: page.ID, Kind: models.FactLesson, Text: lesson.Text,
				HappenedAt: &now, Confidence: 0.8, Inferred: true, Evidence: lesson.Evidence,
				Audiences: []models.AgentAudience{models.AudienceAsk},
			})
			if err != nil {
				return err
			}
			// Its vector now, from the meaning already worked out, so the
			// next conversation's check sees it without waiting for a dream.
			if lessonMeaning != nil {
				return tx.PutAgentFactVector(run.Agent.ID, fact.ID, lessonMeaning.ModelName, lessonMeaning.Vector)
			}
			return nil
		}); err != nil {
			return filed, err
		}
		accepted = append(accepted, lessonMeaning)
		filed++
	}
	return filed, nil
}

// isLessonKnown says whether a lesson nearly the same in meaning is already
// filed, or was accepted earlier in this pass, so that the same work done
// twice keeps one lesson; and gives the lesson's meaning, to store with it.
func (self *Agent) isLessonKnown(ctx context.Context, agentId, text string, accepted []*meaning) (*meaning, bool) {
	question := self.meaningOf(ctx, agentId, "remember", text)
	if question == nil {
		return nil, false
	}
	for _, other := range accepted {
		if other != nil && other.ModelName == question.ModelName && cosineOf(other.Vector, question.Vector) >= lessonDuplicateScore {
			return question, true
		}
	}
	var scores []db.Scored
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		scores, err = tx.Nearest(db.AgentFactTable, agentId, question.ModelName, question.Vector, 1, db.VectorQuery{
			Floor: lessonDuplicateScore,
			Where: []string{`EXISTS (SELECT 1 FROM "agent_fact" WHERE "agent_fact"."id" = "agent_fact_vector"."fact_id"` +
				` AND "agent_fact"."kind" = 'lesson' AND NOT "agent_fact"."dormant")`},
		})
		return err
	}); err != nil {
		return question, false
	}
	return question, len(scores) > 0
}

// cosineOf is how alike two vectors are, one for the same direction.
func cosineOf(first, second []float32) float64 {
	if len(first) != len(second) || len(first) == 0 {
		return 0
	}
	var dot, firstNorm, secondNorm float64
	for index := range first {
		dot += float64(first[index]) * float64(second[index])
		firstNorm += float64(first[index]) * float64(first[index])
		secondNorm += float64(second[index]) * float64(second[index])
	}
	if firstNorm == 0 || secondNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(firstNorm) * math.Sqrt(secondNorm))
}

// recallLessons puts the lessons nearest the turn's words in front of the
// model, apart from the rest of recall and under a budget of their own:
// what worked the last time this kind of work came up.
func (self *AskRun) recallLessons(ctx context.Context, words string) {
	question := self.meaningOfQuestion(ctx, "recall", words)
	if question == nil {
		return
	}
	agentId := self.settings.Agent.ID
	var lines []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		scores, err := tx.Nearest(db.AgentFactTable, agentId, question.ModelName, question.Vector, recallLessons, db.VectorQuery{
			Floor: meaningFloorGraph,
			Where: []string{`EXISTS (SELECT 1 FROM "agent_fact" WHERE "agent_fact"."id" = "agent_fact_vector"."fact_id"` +
				` AND "agent_fact"."kind" = 'lesson' AND NOT "agent_fact"."dormant" AND COALESCE("agent_fact"."superseded_by", '') = '')`},
		})
		if err != nil {
			return err
		}
		facts, err := tx.GetAgentFacts(agentId, idsOf(scores))
		if err != nil {
			return err
		}
		paths, err := pathsOfFacts(tx, agentId, facts)
		if err != nil {
			return err
		}
		spent := 0
		for _, fact := range facts {
			line := "- " + fact.Reference(paths[fact.NodeID]) + " " + fact.Line()
			if cost := llm.EstimateTokens(line); spent+cost <= recallLessonTokens {
				spent += cost
				lines = append(lines, line)
			}
		}
		return nil
	}); err != nil {
		log.Debugf("cannot recall lessons: %s", err)
		return
	}
	if len(lines) > 0 {
		self.Recall("Lessons from earlier work, each borne out by a command that worked:\n" + strings.Join(lines, "\n"))
	}
}
