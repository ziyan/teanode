package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// What an evaluation answers from. The recall evaluation says whether the
// facts a question needs reach the model; this says whether the answer is
// right, and comparing the three says what extracted memory is worth over
// the raw sources it was read from.
const (
	AnswerFromMemory  = "memory"  // the facts recall carries, as a turn would get them
	AnswerFromSources = "sources" // passages from the documents, as the search tool finds them
	AnswerFromBoth    = "both"    // both, the way a turn that searches has them

	// AnswerFromAgent is a turn of the agent itself, with its tools, the
	// way a person asking in a conversation is answered; "agent@high"
	// asks it to think at that effort.
	AnswerFromAgent = "agent"
)

// The verdicts a graded answer can have. See prompts/evaluate_grade.txt.
const (
	AnswerCorrect  = "correct"
	AnswerPartial  = "partial"
	AnswerStale    = "stale"
	AnswerNotKnown = "not_known"
	AnswerInvented = "invented"
	AnswerWrong    = "wrong"

	// AnswerMissed is "not known" where the person gave an answer: what
	// the answer was given did not hold the fact, which is recall's
	// failure rather than the model's, and is told apart from a wrong
	// answer so the two can be worked on apart.
	AnswerMissed = "missed"

	// AnswerUngraded is a grader whose answer could not be read. It counts
	// as nothing, rather than as a wrong answer the model did not give.
	AnswerUngraded = "ungraded"
)

// evaluationPassages is how many passages an answer from the sources is
// given: what one search by the agent's own tool would return.
const evaluationPassages = 8

// evaluationPassageRunes cuts each passage as a turn's tool result would.
const evaluationPassageRunes = 1200

// AnswerEvaluation is one question answered and graded.
type AnswerEvaluation struct {
	AnswerText    string
	AnswerVerdict string
	VerdictReason string

	// FactCount and PassageCount are what the answer was given.
	FactCount    int
	PassageCount int

	// What the answer and its grading cost, together, and how long the
	// answer took, which is what a person waits for; the grading is not.
	Cost             float64
	AnswerDurationMS int64
}

// EvaluateAnswer answers a question from one kind of context, with the
// model a conversation uses, and grades the answer against the one the
// person gave. Both calls are runs of kind evaluate, so they are listed
// and priced like any other call and never touch the conversations or
// the graph.
func (self *Agent) EvaluateAnswer(ctx context.Context, found *models.Agent, owner *models.User, question, expectedAnswer, outdatedAnswer, answerFrom string) (*AnswerEvaluation, error) {
	question = strings.TrimSpace(question)
	if question == "" || strings.TrimSpace(expectedAnswer) == "" {
		return nil, fmt.Errorf("a question and its expected answer are both needed")
	}
	effort, research, isAgent := agentEffortOf(answerFrom)
	if !isAgent && answerFrom != AnswerFromMemory && answerFrom != AnswerFromSources && answerFrom != AnswerFromBoth {
		return nil, fmt.Errorf("answer from %q: memory, sources, both, or agent (agent@low, agent@medium, agent@high, each with +research)", answerFrom)
	}
	if isAgent {
		started := time.Now()
		answered, err := self.answerAsAgent(ctx, found, owner, question, effort, research)
		if err != nil {
			return nil, err
		}
		return self.gradeAnswer(ctx, found, owner, &AnswerEvaluation{
			AnswerText: answered.Text, AnswerDurationMS: time.Since(started).Milliseconds(),
		}, question, expectedAnswer, outdatedAnswer, answered)
	}
	evaluation := &AnswerEvaluation{}
	var memory, passages []string
	factCount := 0
	if answerFrom == AnswerFromMemory || answerFrom == AnswerFromBoth {
		// What a turn carries before any recall: the self page, always
		// first. Left out, a question the self page answers was graded as
		// a miss that no turn would have made.
		for _, line := range self.selfPageLines(ctx, found, owner) {
			memory = append(memory, models.PathSelf+": "+strings.TrimPrefix(line, "- "))
		}
		recalled, err := self.RecallForQuestion(ctx, found, owner, question)
		if err != nil {
			return nil, err
		}
		for _, page := range recalled {
			if page.Summary != "" {
				memory = append(memory, page.Path+": "+page.Summary)
			}
			for _, fact := range page.Facts {
				memory = append(memory, fmt.Sprintf("%s: %s", page.Path, fact.Line()))
				factCount++
			}
		}
	}
	if answerFrom == AnswerFromSources || answerFrom == AnswerFromBoth {
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			searched, err := indexed.Search(ctx, tx, self.KnowledgeMeaning(found.ID), found.ID, indexed.Query{Words: question, Limit: evaluationPassages})
			if err != nil {
				return err
			}
			for _, passage := range searched.Passages {
				passages = append(passages, passage.Cite()+"\n"+cutRunes(passage.Text, evaluationPassageRunes))
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	// Facts recall carried, as before the self page and the openings were
	// given too, so the count stays comparable with earlier runs.
	evaluation.FactCount, evaluation.PassageCount = factCount, len(passages)

	run := self.runFor(found, owner, nil, "")
	prompt, err := render("evaluate_answer.txt", map[string]any{
		"PersonName":        personName(owner),
		"KnowledgeLanguage": languageName(KnowledgeLanguage(found, owner)),
		"Question":          question,
		"Memory":            memory,
		"Passages":          passages,
	})
	if err != nil {
		return nil, err
	}
	started := time.Now()
	answered, err := self.oneShot(ctx, run, "Answered an evaluation question from "+answerFrom, prompt, models.AgentJobEvaluate, config.AgentWorkAsk)
	if err != nil {
		return nil, err
	}
	evaluation.AnswerDurationMS = time.Since(started).Milliseconds()
	evaluation.AnswerText = strings.TrimSpace(answered.Text)
	return self.gradeAnswer(ctx, found, owner, evaluation, question, expectedAnswer, outdatedAnswer, answered)
}

// gradeAnswer grades an answer against the one the person gave, and adds
// what answering and grading cost.
func (self *Agent) gradeAnswer(ctx context.Context, found *models.Agent, owner *models.User, evaluation *AnswerEvaluation, question, expectedAnswer, outdatedAnswer string, answered *thought) (*AnswerEvaluation, error) {
	run := self.runFor(found, owner, nil, "")

	// "not known" needs no grader either way: it is right where the
	// person's answer is "not known" too, and a miss everywhere else.
	if isNotKnown(evaluation.AnswerText) {
		evaluation.AnswerVerdict, evaluation.VerdictReason = AnswerMissed, "it said not known"
		if isNotKnown(expectedAnswer) {
			evaluation.AnswerVerdict, evaluation.VerdictReason = AnswerNotKnown, "not known, as expected"
		}
		evaluation.Cost = self.costOfRuns(ctx, answered)
		return evaluation, nil
	}

	grading, err := render("evaluate_grade.txt", map[string]any{
		"PersonName":     personName(owner),
		"Question":       question,
		"ExpectedAnswer": strings.TrimSpace(expectedAnswer),
		"OutdatedAnswer": strings.TrimSpace(outdatedAnswer),
		"AnswerText":     evaluation.AnswerText,
	})
	if err != nil {
		return nil, err
	}
	graded, err := self.oneShot(ctx, run, "Graded an evaluation answer", grading, models.AgentJobEvaluate, config.AgentWorkAsk)
	if err != nil {
		return nil, err
	}
	evaluation.Cost = self.costOfRuns(ctx, answered, graded)
	verdict := readModelAnswer[struct {
		AnswerVerdict string `json:"answerVerdict"`
		VerdictReason string `json:"verdictReason"`
	}](graded.Text, "answerVerdict")
	evaluation.AnswerVerdict, evaluation.VerdictReason = AnswerUngraded, verdict.Problem
	if verdict.IsValid {
		switch verdict.Value.AnswerVerdict {
		case AnswerCorrect, AnswerPartial, AnswerStale, AnswerNotKnown, AnswerInvented, AnswerWrong:
			evaluation.AnswerVerdict, evaluation.VerdictReason = verdict.Value.AnswerVerdict, verdict.Value.VerdictReason
		default:
			evaluation.VerdictReason = fmt.Sprintf("the grader answered %q", verdict.Value.AnswerVerdict)
		}
	}
	return evaluation, nil
}

// isNotKnown says whether an answer is the prompt's "not known", allowing
// for the case and the full stop a model adds.
func isNotKnown(text string) bool {
	return strings.Trim(strings.ToLower(strings.TrimSpace(text)), ".!") == "not known"
}

// costOfRuns is what these calls cost, as their runs recorded it: the
// price is put on each message as it is written, from the provider's
// configured prices, so it is read back rather than worked out twice.
func (self *Agent) costOfRuns(ctx context.Context, thoughts ...*thought) float64 {
	ids := make([]string, 0, len(thoughts))
	for _, each := range thoughts {
		if each != nil && each.Conversation != nil {
			ids = append(ids, each.Conversation.ID)
		}
	}
	var cost float64
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		usage, err := tx.SumAgentRunUsage(ids)
		for _, each := range usage {
			cost += each.Cost
		}
		return err
	}); err != nil {
		log.Debugf("cannot read what an evaluation cost: %s", err)
	}
	return cost
}

// agentEffortOf reads "agent", "agent@high" or "agent@high+research":
// whether an answer is the agent's own turn, at what effort, and whether
// it is given the research procedure.
func agentEffortOf(answerFrom string) (string, bool, bool) {
	answerFrom, research := strings.CutSuffix(answerFrom, "+research")
	name, effort, _ := strings.Cut(answerFrom, "@")
	if name != AnswerFromAgent {
		return "", false, false
	}
	switch effort {
	case "", llm.EffortLow, llm.EffortMedium, llm.EffortHigh:
		return effort, research, true
	}
	return "", false, false
}

// answerAsAgent asks the agent the question in a turn of its own, with its
// tools and the prompt a person's turn gets, headless and unable to change
// anything, and answers with what it said.
func (self *Agent) answerAsAgent(ctx context.Context, found *models.Agent, owner *models.User, question, effort string, research bool) (*thought, error) {
	var conversation *models.AgentConversation
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: found.ID, Kind: models.AgentConversationRun,
			Title: "Answered an evaluation question as the agent", JobKind: string(models.AgentJobEvaluate),
			Surface: string(models.AgentJobEvaluate), LastAt: time.Now(),
		})
		return err
	}); err != nil {
		return nil, err
	}
	operations, err := self.operations(ctx, owner)
	if err != nil {
		return nil, err
	}
	turn, err := self.Ask(&AskSettings{
		Agent: found, Owner: owner, Operations: operations, Conversation: conversation,
		Message: question, Surface: string(models.AgentJobEvaluate), ReadOnly: true, Headless: true,
		UsageKind: string(models.AgentJobEvaluate), Work: config.AgentWorkAsk, Effort: effort, Research: research,
	})
	if err != nil {
		return nil, err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	said, failure := "", ""
	for event := range events {
		if ctx.Err() != nil {
			turn.Stop()
			break
		}
		switch event.Kind {
		case EventMessage:
			said = event.Text
		case EventError:
			failure = event.Error
		}
	}
	answered := &thought{Conversation: conversation, Text: strings.TrimSpace(said), Usage: turn.Usage()}
	if failure != "" {
		return answered, fmt.Errorf("the agent's turn failed: %s", failure)
	}
	return answered, nil
}
