// Package memorycheck is the memory check as the agent runs it in
// conversation: drafting questions from what it remembers about the
// person, putting them one at a time, and recording what the person says.
// The questions it keeps are the person's evaluation set, which a weekly
// run grades the agent against. See
// docs/planning/agent-speaks-first-execplan.md.
package memorycheck

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// draftShares is how many facts of each sort a draft offers: mostly facts
// as they stand, one with a date to ask the when of, and one that changed.
var draftShares = []struct {
	factsToCheck string
	factCount    int
}{
	{db.FactsToCheckStated, 3},
	{db.FactsToCheckDated, 1},
	{db.FactsToCheckChanged, 1},
}

// overlayLasts is how long after the last question was put a check is
// still open in the conversation's overlay.
const overlayLasts = 6 * time.Hour

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "memory_check", Family: tools.FamilyGeneral, Risk: tools.RiskWrite,
				Description: "The memory check: you tell the person things you remember about them, one at a time, and they say whether you are right, so you learn how well your memory answers. Never quiz them. `draft` gives you up to five facts from their own pages to ask about. `ask` records a question as you put it, with the answer you believe. `record` records their reply to it. `add` records a question they supply, usually about something that changed, with its answer. `filed` marks a question whose correction you filed into memory with their consent. `list` is what was asked and not yet answered.",
				Parameters: tools.Object(map[string]any{
					"action":          tools.EnumProperty("what to do", "draft", "ask", "record", "add", "filed", "list"),
					"question_id":     tools.StringProperty("for record and filed: the question, as ask or add gave it"),
					"question":        tools.StringProperty("for ask and add: the plain question the fact answers, for grading later (\"where do you live?\"), not the sentence you said to them"),
					"answer":          tools.StringProperty("for ask: the answer you believe; for record with corrected, the answer they gave; for add, the answer"),
					"outdated_answer": tools.StringProperty("for a question about something that changed: what used to be true"),
					"question_kind":   tools.EnumProperty("for ask and add: what the question tests, direct by default", kindNames()...),
					"fact_ids":        tools.ArrayProperty("for ask: the facts the question came from, by the ids draft gave", tools.StringProperty("a fact id")),
					"reply":           tools.EnumProperty("for record: confirmed when your answer was right, corrected when they gave another, dropped when they would rather not keep the question, unsure when they do not know", "confirmed", "corrected", "dropped", "unsure"),
				}, "action"),
				Preview: tools.PreviewOf(func(call arguments) string {
					switch strings.TrimSpace(call.Action) {
					case "ask":
						return "Record a memory check question"
					case "record":
						return "Record your answer to a memory check question"
					case "add":
						return "Add your question to the memory check"
					case "filed":
						return "Mark a memory check answer as filed"
					}
					return "Look at the memory check"
				}),
				RiskOf:  riskOf,
				Run:     run,
				Overlay: overlay,
			},
		}
	})
}

// riskOf is a read for draft and list, which change nothing.
func riskOf(raw json.RawMessage) tools.Risk {
	var call arguments
	if json.Unmarshal(raw, &call) == nil && (call.Action == "draft" || call.Action == "list") {
		return tools.RiskRead
	}
	return tools.RiskWrite
}

func kindNames() []string {
	names := make([]string, 0, len(models.EvaluationQuestionKinds))
	for _, kind := range models.EvaluationQuestionKinds {
		names = append(names, string(kind))
	}
	return names
}

type arguments struct {
	Action         string   `json:"action"`
	QuestionID     string   `json:"question_id"`
	Question       string   `json:"question"`
	Answer         string   `json:"answer"`
	OutdatedAnswer string   `json:"outdated_answer"`
	QuestionKind   string   `json:"question_kind"`
	FactIDs        []string `json:"fact_ids"`
	Reply          string   `json:"reply"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	agent := current.Agent()
	if agent == nil {
		return nil, fmt.Errorf("there is no agent")
	}
	var result *tools.Result
	err = current.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		switch action := strings.ToLower(strings.TrimSpace(asked.Action)); action {
		case "draft":
			result, err = draft(tx, current, agent)
		case "ask":
			result, err = ask(tx, current, agent, asked)
		case "record":
			result, err = record(tx, agent, asked)
		case "add":
			result, err = add(tx, current, agent, asked)
		case "filed":
			result, err = filed(tx, agent, asked)
		case "list":
			result, err = list(tx, agent)
		default:
			err = fmt.Errorf("%q is not draft, ask, record, add, filed or list", action)
		}
		return err
	})
	return result, err
}

// draftedFact is a fact offered for a question.
type draftedFact struct {
	FactID       string `json:"fact_id"`
	Citation     string `json:"citation"`
	FactText     string `json:"text"`
	HappenedAt   string `json:"happened,omitempty"`
	FactsToCheck string `json:"sort"`
	ReplacedBy   string `json:"now_reads,omitempty"`
}

func draft(tx db.Transaction, current tools.Run, agent *models.Agent) (*tools.Result, error) {
	// Facts already asked about are not asked about again.
	questions, err := tx.ListAgentEvaluationQuestions(agent.ID, nil)
	if err != nil {
		return nil, err
	}
	asked := []string{}
	for _, question := range questions {
		asked = append(asked, question.SourceFactIDs...)
	}
	var facts []*models.AgentFact
	sorts := map[string]string{}
	for _, share := range draftShares {
		found, err := tx.ListAgentFactsToCheck(agent.ID, share.factsToCheck, asked, share.factCount)
		if err != nil {
			return nil, err
		}
		for _, fact := range found {
			sorts[fact.ID] = share.factsToCheck
			asked = append(asked, fact.ID)
		}
		facts = append(facts, found...)
	}
	if len(facts) == 0 {
		return tools.JSONResult(map[string]any{"facts": []draftedFact{}, "note": "nothing on their own pages is left to ask about; ask them what has changed lately instead"})
	}
	nodeIds, replacementIds := []string{}, []string{}
	for _, fact := range facts {
		nodeIds = append(nodeIds, fact.NodeID)
		if fact.SupersededBy != "" {
			replacementIds = append(replacementIds, fact.SupersededBy)
		}
	}
	nodes, err := tx.GetAgentNodes(agent.ID, nodeIds)
	if err != nil {
		return nil, err
	}
	pathOf := map[string]string{}
	for _, node := range nodes {
		pathOf[node.ID] = node.Path
	}
	replacements, err := tx.GetAgentFacts(agent.ID, replacementIds)
	if err != nil {
		return nil, err
	}
	replacementText := map[string]string{}
	for _, replacement := range replacements {
		replacementText[replacement.ID] = replacement.Text
	}
	location := tools.Location(current.Owner())
	drafted := make([]draftedFact, 0, len(facts))
	for _, fact := range facts {
		item := draftedFact{
			FactID: fact.ID, Citation: fmt.Sprintf("%s#%d", pathOf[fact.NodeID], fact.Number),
			FactText: fact.Text, FactsToCheck: sorts[fact.ID], ReplacedBy: replacementText[fact.SupersededBy],
		}
		if fact.HappenedAt != nil {
			item.HappenedAt = fact.HappenedAt.In(location).Format("2006-01-02")
		}
		drafted = append(drafted, item)
	}
	return tools.JSONResult(map[string]any{
		"facts": drafted,
		"note":  "Put each to them as what you remember, and ask whether it is right and still true; they should never have to recall anything. Pass over any fact about somebody else's work or that they would have to look up. With ask, record the plain question the fact answers and the answer you believe; for a fact that changed, what it used to be as outdated_answer. One at a time.",
	})
}

func questionKind(name string) (models.EvaluationQuestionKind, error) {
	kind := models.EvaluationQuestionKind(strings.ToLower(strings.TrimSpace(name)))
	if kind == "" {
		return models.EvaluationQuestionDirect, nil
	}
	if !slices.Contains(models.EvaluationQuestionKinds, kind) {
		return "", fmt.Errorf("%q is not one of %s", name, strings.Join(kindNames(), ", "))
	}
	return kind, nil
}

func ask(tx db.Transaction, current tools.Run, agent *models.Agent, asked arguments) (*tools.Result, error) {
	kind, err := questionKind(asked.QuestionKind)
	if err != nil {
		return nil, err
	}
	question, err := tx.CreateAgentEvaluationQuestion(&models.AgentEvaluationQuestion{
		AgentID: agent.ID, QuestionKind: kind, QuestionText: asked.Question,
		ExpectedAnswer: asked.Answer, OutdatedAnswer: asked.OutdatedAnswer,
		QuestionState: models.EvaluationQuestionAsked, SourceFactIDs: asked.FactIDs,
		ConversationID: tools.ConversationIDOf(current),
	})
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"question_id": question.ID, "note": "record their reply with record when it comes"})
	if err != nil {
		return nil, err
	}
	result.Note = "memory check: asked"
	return result, nil
}

func record(tx db.Transaction, agent *models.Agent, asked arguments) (*tools.Result, error) {
	state := models.EvaluationQuestionState(strings.ToLower(strings.TrimSpace(asked.Reply)))
	switch state {
	case models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected, models.EvaluationQuestionDropped, models.EvaluationQuestionUnsure:
	default:
		return nil, fmt.Errorf("reply is confirmed, corrected, dropped or unsure, not %q", asked.Reply)
	}
	now := time.Now()
	question, err := tx.UpdateAgentEvaluationQuestion(agent.ID, strings.TrimSpace(asked.QuestionID), func(question *models.AgentEvaluationQuestion) error {
		if state == models.EvaluationQuestionCorrected {
			if strings.TrimSpace(asked.Answer) == "" {
				return fmt.Errorf("a correction needs the answer they gave")
			}
			question.ExpectedAnswer = asked.Answer
		}
		if strings.TrimSpace(asked.OutdatedAnswer) != "" {
			question.OutdatedAnswer = asked.OutdatedAnswer
			question.QuestionKind = models.EvaluationQuestionChanged
		}
		question.QuestionState, question.AnsweredAt = state, &now
		return nil
	})
	if err != nil {
		return nil, err
	}
	note := "recorded"
	if state == models.EvaluationQuestionCorrected {
		note = "recorded; ask whether they want you to remember the right answer, and if they do, file it with memory and then call filed"
	}
	result, err := tools.JSONResult(map[string]any{"question_id": question.ID, "question_state": string(question.QuestionState), "note": note})
	if err != nil {
		return nil, err
	}
	result.Note = "memory check: " + string(question.QuestionState)
	return result, nil
}

func add(tx db.Transaction, current tools.Run, agent *models.Agent, asked arguments) (*tools.Result, error) {
	kind, err := questionKind(asked.QuestionKind)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(asked.OutdatedAnswer) != "" {
		kind = models.EvaluationQuestionChanged
	}
	now := time.Now()
	// What the person supplied is theirs, answer and all: confirmed as it
	// is recorded.
	question, err := tx.CreateAgentEvaluationQuestion(&models.AgentEvaluationQuestion{
		AgentID: agent.ID, QuestionKind: kind, QuestionText: asked.Question,
		ExpectedAnswer: asked.Answer, OutdatedAnswer: asked.OutdatedAnswer,
		QuestionState: models.EvaluationQuestionConfirmed, SourceFactIDs: []string{},
		ConversationID: tools.ConversationIDOf(current), AnsweredAt: &now,
	})
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{"question_id": question.ID, "note": "added; ask whether they want you to remember it, and if they do, file it with memory and then call filed"})
	if err != nil {
		return nil, err
	}
	result.Note = "memory check: question added"
	return result, nil
}

func filed(tx db.Transaction, agent *models.Agent, asked arguments) (*tools.Result, error) {
	question, err := tx.UpdateAgentEvaluationQuestion(agent.ID, strings.TrimSpace(asked.QuestionID), func(question *models.AgentEvaluationQuestion) error {
		question.IsAnswerFiledAfter = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"question_id": question.ID, "is_answer_filed_after": true})
}

// listedQuestion is a question as list and the overlay give it.
type listedQuestion struct {
	QuestionID     string `json:"question_id"`
	QuestionText   string `json:"question"`
	ExpectedAnswer string `json:"answer_you_gave,omitempty"`
}

func list(tx db.Transaction, agent *models.Agent) (*tools.Result, error) {
	open, err := tx.ListAgentEvaluationQuestions(agent.ID, []models.EvaluationQuestionState{models.EvaluationQuestionAsked})
	if err != nil {
		return nil, err
	}
	listed := make([]listedQuestion, 0, len(open))
	for _, question := range open {
		listed = append(listed, listedQuestion{QuestionID: question.ID, QuestionText: question.QuestionText, ExpectedAnswer: question.ExpectedAnswer})
	}
	all, err := tx.ListAgentEvaluationQuestions(agent.ID, []models.EvaluationQuestionState{models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected})
	if err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"unanswered": listed, "answered_count": len(all)})
}

// overlay is the check in progress in this conversation: the question
// waiting for a reply, and how many have been put, so the thread survives
// from one turn to the next.
func overlay(ctx context.Context) string {
	current, err := tools.RunFrom(ctx)
	if err != nil || current.Agent() == nil || tools.ConversationIDOf(current) == "" {
		return ""
	}
	var questions []*models.AgentEvaluationQuestion
	var began *time.Time
	if err := current.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if questions, err = tx.ListAgentEvaluationQuestions(current.Agent().ID, nil); err != nil {
			return err
		}
		began, err = tx.LastAgentMessageStartingWith(tools.ConversationIDOf(current), models.SpeakFirstMarker+" "+models.MemoryCheckOpening)
		return err
	}); err != nil {
		return ""
	}
	// The current check's questions: those since it began, where it began
	// with a check-in here. Counted over the last hours instead, a check
	// asked for right after one ended was told it had already asked five.
	since := time.Now().Add(-overlayLasts)
	if began != nil && began.After(since) {
		since = *began
	}
	conversationId := tools.ConversationIDOf(current)
	waiting := []string{}
	putCount := 0
	for _, question := range questions {
		if question.ConversationID != conversationId || question.CreatedAt.Before(since) {
			continue
		}
		putCount++
		if question.QuestionState == models.EvaluationQuestionAsked {
			waiting = append(waiting, fmt.Sprintf("%q (question_id %s, the answer you gave: %q)", question.QuestionText, question.ID, question.ExpectedAnswer))
		}
	}
	if putCount == 0 {
		return ""
	}
	lines := []string{"<memory_check>", fmt.Sprintf("A memory check is open here: %d question(s) so far; stop at about five, or whenever they want to.", putCount)}
	if len(waiting) > 0 {
		lines = append(lines, "Waiting for their reply: "+strings.Join(waiting, "; ")+". Record it with memory_check record before anything else.")
	}
	lines = append(lines, "</memory_check>")
	return strings.Join(lines, "\n")
}
