package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A run answers every question the person stands behind from each source,
// keeps each answer, and scores each source with and without the questions
// whose answers were filed afterwards; a question dropped or unanswered
// takes no part. Run again, it asks nothing it already has.
func TestAnEvaluationRunScoresEachSource(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	content, _ := json.Marshal("Not known.")
	provider := scriptedProvider([]string{fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, content)})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var evaluation *models.AgentEvaluationRun
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		for _, question := range []*models.AgentEvaluationQuestion{
			{QuestionText: "What is the boat called?", ExpectedAnswer: "Marigold.", QuestionState: models.EvaluationQuestionConfirmed},
			{QuestionText: "Where is it moored?", ExpectedAnswer: "At the pier.", QuestionState: models.EvaluationQuestionCorrected, IsAnswerFiledAfter: true},
			{QuestionText: "What is its engine number?", ExpectedAnswer: "not known", QuestionKind: models.EvaluationQuestionAbstain, QuestionState: models.EvaluationQuestionConfirmed},
			{QuestionText: "Who painted it?", QuestionState: models.EvaluationQuestionAsked},
			{QuestionText: "What color is the sail?", ExpectedAnswer: "Red.", QuestionState: models.EvaluationQuestionDropped},
		} {
			question.AgentID, question.AnsweredAt = run.Agent.ID, &now
			if _, err := tx.CreateAgentEvaluationQuestion(question); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		if evaluation, err = worker.QueueEvaluation(tx, run.Agent); err != nil {
			t.Fatal(err)
		}
		again, err := worker.QueueEvaluation(tx, run.Agent)
		if err != nil || again.ID != evaluation.ID {
			t.Fatalf("a run under way is the one returned: %v %v", again, err)
		}
	})
	run.Job = &models.AgentJob{Kind: models.AgentJobEvaluate, AgentID: run.Agent.ID, SubjectID: evaluation.ID}
	if err := worker.runEvaluation(t.Context(), run); err != nil {
		t.Fatalf("runEvaluation: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		finished, err := tx.GetAgentEvaluationRun(run.Agent.ID, evaluation.ID)
		if err != nil || finished.FinishedAt == nil || finished.QuestionCount != 3 || len(finished.SourceScores) != 3 {
			t.Fatalf("finished, three questions, three sources: %+v %v", finished, err)
		}
		for _, scored := range finished.SourceScores {
			// Only the abstain question is right; without the filed one,
			// one of two.
			if scored.AnsweredCount != 3 || math.Abs(scored.ScorePercent-100.0/3) > 0.01 || scored.ScorePercentWithoutFiledAfter != 50 || scored.FiledAfterCount != 1 ||
				scored.VerdictCounts[AnswerMissed] != 2 || scored.VerdictCounts[AnswerNotKnown] != 1 {
				t.Fatalf("%s scored %+v", scored.AnswerFrom, scored)
			}
		}
		answers, _ := tx.ListAgentEvaluationAnswers(evaluation.ID)
		if len(answers) != 9 {
			t.Fatalf("three answers a source: %d", len(answers))
		}
	})
}
