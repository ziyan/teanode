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
	"unicode/utf8"

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
	// minimumLessonCount is how many lessons one call may file however
	// little it read, and lessonRunes how much more of the conversation one
	// more lesson may be filed for: a part of a long working session read
	// whole can teach more than a short exchange.
	minimumLessonCount = 3
	lessonRunes        = 5000

	// The head and the tail of a long command, and of what it printed, are
	// what the model is shown, with how much was left out between them said
	// where it was: the head says what ran, and the end of the output is
	// where failures and summaries are.
	lessonCallArgumentHeadLength = 300
	lessonCallArgumentTailLength = 100
	lessonCallOutputHeadLength   = 400
	lessonCallOutputTailLength   = 800

	// lessonPartRunes is how much of a window one call reads. A longer
	// window is read in consecutive parts of about this size, split where
	// one message ends and the next begins, each in a call of its own, so
	// the start of a long working session is read as well as its end. The
	// size the dream reads a batch of documents at.
	lessonPartRunes = digestBatchRunes

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
		found := &lessonCall{
			Number: len(calls) + 1, ToolName: call.Name,
			Arguments: headAndTail(call.Arguments, lessonCallArgumentHeadLength, lessonCallArgumentTailLength, "the command"),
			ExitCode:  exitCode, IsFinished: hasExitCode && !isBackground,
			Output: headAndTail(strings.TrimSpace(output), lessonCallOutputHeadLength, lessonCallOutputTailLength, "the output"),
		}
		calls = append(calls, found)
		byToolCallId[message.ToolCallID] = found
	}
	return calls, byToolCallId
}

// headAndTail is a long text as its first and last so many characters,
// with how many characters of what were left out between them said where
// they were, so the model knows the two ends are not one piece. A text
// that fits is returned whole.
func headAndTail(text string, headLength, tailLength int, what string) string {
	runes := []rune(text)
	if len(runes) <= headLength+tailLength {
		return text
	}
	leftOutCount := len(runes) - headLength - tailLength
	return string(runes[:headLength]) + fmt.Sprintf("\n[%d characters of %s left out here]\n", leftOutCount, what) +
		string(runes[len(runes)-tailLength:])
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

// lessonEntryOf is one message as the lessons pass reads it: what was
// said, or a command with its number, what it printed and how it ended.
// A message with nothing to show is empty.
func lessonEntryOf(message *models.AgentMessage, byToolCallId map[string]*lessonCall) string {
	switch message.Role {
	case string(llm.RoleUser):
		if text := strings.TrimSpace(message.Content); text != "" {
			return "them: " + unclosable(text) + "\n\n"
		}
	case string(llm.RoleAssistant):
		if text := strings.TrimSpace(message.Content); text != "" {
			return "you: " + unclosable(text) + "\n\n"
		}
	case string(llm.RoleTool):
		call := byToolCallId[message.ToolCallID]
		if call == nil {
			return ""
		}
		ended := "still running in the background"
		if call.IsFinished {
			ended = "exit code " + strconv.Itoa(call.ExitCode)
		}
		return fmt.Sprintf("[command %d] %s %s\n%s\n(%s)\n\n", call.Number, call.ToolName,
			unclosable(call.Arguments), unclosable(call.Output), ended)
	}
	return ""
}

// lessonTranscript is the window as the lessons pass reads it, whole.
func lessonTranscript(messages []*models.AgentMessage, byToolCallId map[string]*lessonCall) string {
	var builder strings.Builder
	for _, message := range messages {
		builder.WriteString(lessonEntryOf(message, byToolCallId))
	}
	return strings.TrimSpace(builder.String())
}

// lessonPart is a stretch of the window read in one call: its transcript,
// the commands in it, which are the only ones that may verify a lesson
// read from it, and EndIndex, the index in the window just past its last
// message, which is how far the window has been read once it is answered.
type lessonPart struct {
	Transcript string
	Calls      []*lessonCall
	EndIndex   int
}

// lessonParts splits the window into consecutive parts of about partRunes
// each, where one message ends and the next begins. Nothing is left out:
// a message longer than a part is a part of its own.
func lessonParts(messages []*models.AgentMessage, byToolCallId map[string]*lessonCall, partRunes int) []lessonPart {
	var parts []lessonPart
	var builder strings.Builder
	builderRunes := 0
	var calls []*lessonCall
	for index, message := range messages {
		entry := lessonEntryOf(message, byToolCallId)
		if entry == "" {
			continue
		}
		entryRunes := utf8.RuneCountInString(entry)
		if builderRunes > 0 && builderRunes+entryRunes > partRunes {
			parts = append(parts, lessonPart{Transcript: strings.TrimSpace(builder.String()), Calls: calls, EndIndex: index})
			builder.Reset()
			builderRunes = 0
			calls = nil
		}
		builder.WriteString(entry)
		builderRunes += entryRunes
		if message.Role == string(llm.RoleTool) {
			calls = append(calls, byToolCallId[message.ToolCallID])
		}
	}
	if builderRunes > 0 {
		parts = append(parts, lessonPart{Transcript: strings.TrimSpace(builder.String()), Calls: calls, EndIndex: len(messages)})
	}
	return parts
}

// hasAnySucceeded says whether one of the commands ended with exit code 0.
func hasAnySucceeded(calls []*lessonCall) bool {
	for _, call := range calls {
		if call.hasSucceeded() {
			return true
		}
	}
	return false
}

// lessonsAllowedFor is how many lessons a call that read this transcript
// may file: more for more work read, never fewer than minimumLessonCount.
func lessonsAllowedFor(transcript string) int {
	return max(minimumLessonCount, utf8.RuneCountInString(transcript)/lessonRunes)
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

// verifyLessons keeps the lessons a command bears out, as many as are
// allowed: at least one of the commands each names, among the calls the
// model was shown, ended with exit code 0. A lesson naming none, or only
// commands that failed, are still running or were in another part of the
// conversation, is the model's word alone and is dropped.
func verifyLessons(answer lessonAnswer, calls []*lessonCall, conversationId string, allowedCount int) []verifiedLesson {
	byNumber := make(map[int]*lessonCall, len(calls))
	for _, call := range calls {
		byNumber[call.Number] = call
	}
	var kept []verifiedLesson
	for _, lesson := range answer.Lessons {
		if len(kept) >= allowedCount {
			break
		}
		// Kept whole: a lesson is what a later turn is shown, and an
		// approach cut where it was filed lost its last step for good.
		appliesWhen := strings.TrimSpace(lesson.AppliesWhen)
		approach := strings.TrimSpace(lesson.Approach)
		if appliesWhen == "" || approach == "" {
			continue
		}
		var evidence []models.Evidence
		for _, number := range lesson.VerifiedByCalls {
			call := byNumber[number]
			if !call.hasSucceeded() || len(evidence) >= 2 {
				continue
			}
			evidence = append(evidence, models.Evidence{Kind: models.EvidenceConversation, ID: conversationId,
				Quote: call.ToolName + " " + call.Arguments + " → exit code 0"})
		}
		if len(evidence) == 0 {
			continue
		}
		text := "When " + strings.TrimSuffix(appliesWhen, ".") + ": " + strings.TrimSuffix(approach, ".") + "."
		if avoid := strings.TrimSpace(lesson.Avoid); avoid != "" {
			text += " Avoid: " + strings.TrimSuffix(avoid, ".") + "."
		}
		if verification := strings.TrimSpace(lesson.Verification); verification != "" {
			text += " Verified by: " + strings.TrimSuffix(verification, ".") + "."
		}
		if scope := strings.TrimSpace(lesson.Scope); scope != "" {
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
// command succeeded is not read at all. A window longer than one call holds
// is read in consecutive parts, each in a call of its own that is told
// which part it is; a part in which no command succeeded is not asked
// about. An answer that cannot be read loses that part's lessons alone and
// the parts after it are still read. It says how many lessons it filed and
// how many messages of the window it read: all of them, or, when a part's
// call or the filing of its lessons failed, those before that part, so the
// caller keeps the rest for the next run.
func (self *Agent) readLessons(ctx context.Context, run *Run, conversation *models.AgentConversation, window []*models.AgentMessage) (filedCount, readCount int, err error) {
	calls, byToolCallId := lessonCallsOf(window)
	if !hasAnySucceeded(calls) {
		return 0, len(window), nil
	}
	parts := lessonParts(window, byToolCallId, lessonPartRunes)
	// Shared by the parts, so a lesson the work taught twice in one
	// session is filed once.
	var accepted []*meaning
	for index, part := range parts {
		if hasAnySucceeded(part.Calls) {
			partFiledCount, err := self.readLessonPart(ctx, run, conversation, part, index+1, len(parts), &accepted)
			filedCount += partFiledCount
			if err != nil {
				if len(parts) > 1 {
					err = fmt.Errorf("part %d of %d: %w", index+1, len(parts), err)
				}
				return filedCount, readCount, err
			}
		}
		readCount = part.EndIndex
	}
	return filedCount, len(window), nil
}

// readLessonPart asks about one part of a window and files the lessons a
// command in that part bears out, skipping any nearly the same in meaning
// as one filed before or accepted earlier in this pass.
func (self *Agent) readLessonPart(ctx context.Context, run *Run, conversation *models.AgentConversation, part lessonPart, partNumber, partCount int, accepted *[]*meaning) (int, error) {
	allowedCount := lessonsAllowedFor(part.Transcript)
	prompt, err := render("lessons.txt", map[string]any{
		"PersonName":        personName(run.Owner),
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"Transcript":        part.Transcript,
		"PartNumber":        partNumber,
		"PartCount":         partCount,
		"LessonCount":       allowedCount,
	})
	if err != nil {
		return 0, err
	}
	title := "Reading lessons from " + chatName(conversation)
	if partCount > 1 {
		title += fmt.Sprintf(", part %d of %d", partNumber, partCount)
	}
	thinking, err := self.oneShot(ctx, run, title, prompt, models.AgentJobRemember, config.AgentWorkScan)
	if err != nil {
		return 0, fmt.Errorf("asking the model: %w", err)
	}
	answer := readModelAnswer[lessonAnswer](thinking.Text, "lessons")
	if !answer.IsValid {
		// Read and answered, though with nothing usable: asking again is
		// not worth holding back the filing of the whole conversation for.
		log.Warningf("the lessons from conversation %s, part %d of %d, were not readable: %s", conversation.ID, partNumber, partCount, answer.Problem)
		return 0, nil
	}
	lessons := verifyLessons(answer.Value, part.Calls, conversation.ID, allowedCount)

	filed := 0
	for _, lesson := range lessons {
		lessonMeaning, isKnown := self.isLessonKnown(ctx, run.Agent.ID, lesson.Text, *accepted)
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
		*accepted = append(*accepted, lessonMeaning)
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
	if lines := self.lessonLines(ctx, words); len(lines) > 0 {
		self.Recall(lessonsHeading + "\n" + strings.Join(lines, "\n"))
	}
}

// lessonsHeading is what the lessons a turn is shown are introduced with.
const lessonsHeading = "Lessons from earlier work, each borne out by a command that worked:"

// lessonLines are the lessons a turn with these words is shown, one line
// each, within their budget: the same for a turn and for an evaluation
// that answers as a turn would.
func (self *AskRun) lessonLines(ctx context.Context, words string) []string {
	question := self.meaningOfQuestion(ctx, "recall", words)
	if question == nil {
		return nil
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
		return nil
	}
	return lines
}

// LessonsForQuestion is the lessons a turn asking this would be shown.
func (self *Agent) LessonsForQuestion(ctx context.Context, found *models.Agent, owner *models.User, question string) []string {
	run := &AskRun{agent: self, settings: &AskSettings{Agent: found, Owner: owner, Message: question}, promptMemories: map[string]bool{}}
	run.ctx = ctx
	return run.lessonLines(ctx, question)
}
