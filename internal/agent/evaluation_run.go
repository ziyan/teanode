package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A run of the memory check grades the agent against every question the
// person stands behind, from memory, from the sources and from both, and
// keeps the scores. It is queued weekly, and sooner after a check that
// added enough questions to move the score.
const (
	evaluationEvery          = time.Hour
	evaluationApart          = 7 * 24 * time.Hour
	evaluationNewAnswerCount = 5

	// evaluationLongest is how long one run may take: two model calls a
	// question and source, three sources, and a set of a few dozen
	// questions. A run cut short goes on where it stopped when it is
	// tried again.
	evaluationLongest = time.Hour
)

// evaluationSources are what each question is answered from, in the order
// the scores are listed.
var evaluationSources = []string{AnswerFromMemory, AnswerFromSources, AnswerFromBoth}

// queueEvaluating queues a run for each agent that is due one.
func (self *Agent) queueEvaluating(ctx context.Context, now time.Time) {
	if now.Sub(self.lastEvaluate) < evaluationEvery || self.settings.Registry == nil {
		return
	}
	self.lastEvaluate = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to evaluate: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Enabled || agent.OperatorDisabledAt != nil {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			isDue, err := evaluationDue(tx, agent, now)
			if err != nil || !isDue {
				return err
			}
			_, err = self.QueueEvaluation(tx, agent)
			return err
		}); err != nil {
			log.Warningf("cannot say whether agent %q is due an evaluation: %s", agent.ID, err)
		}
	}
}

// evaluationDue says whether an agent should be graded now: it has
// questions, nothing of the kind or a dream is under way, and the last run
// was a week ago or enough questions were answered since.
func evaluationDue(tx db.Transaction, agent *models.Agent, now time.Time) (bool, error) {
	open, err := tx.CountAgentJobs(&db.AgentJobFilter{
		AgentID:  agent.ID,
		Kinds:    []models.AgentJobKind{models.AgentJobEvaluate, models.AgentJobDream},
		Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
	})
	if err != nil || open > 0 {
		return false, err
	}
	questions, err := tx.ListAgentEvaluationQuestions(agent.ID, []models.EvaluationQuestionState{models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected})
	if err != nil || len(questions) == 0 {
		return false, err
	}
	runs, err := tx.ListAgentEvaluationRuns(agent.ID, 1)
	if err != nil {
		return false, err
	}
	if len(runs) == 0 {
		return true, nil
	}
	last := runs[0].StartedAt
	if now.Sub(last) >= evaluationApart {
		return true, nil
	}
	newCount := 0
	for _, question := range questions {
		if question.AnsweredAt != nil && question.AnsweredAt.After(last) {
			newCount++
		}
	}
	return newCount >= evaluationNewAnswerCount, nil
}

// QueueEvaluation starts a run of the memory check and queues the job that
// fills it; one already queued or running is left to run, and is what is
// returned.
func (self *Agent) QueueEvaluation(tx db.Transaction, agent *models.Agent) (*models.AgentEvaluationRun, error) {
	jobs, err := tx.ListAgentJobs(&db.AgentJobFilter{
		AgentID:  agent.ID,
		Kinds:    []models.AgentJobKind{models.AgentJobEvaluate},
		Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
	}, nil)
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		return tx.GetAgentEvaluationRun(agent.ID, jobs[0].SubjectID)
	}
	run, err := tx.CreateAgentEvaluationRun(agent.ID)
	if err != nil {
		return nil, err
	}
	if _, err := self.Enqueue(tx, models.AgentJobEvaluate, agent.ID, "", run.ID); err != nil {
		return nil, err
	}
	return run, nil
}

// runEvaluation answers every question the person stands behind from each
// source, grades each answer, and writes the run's scores. An answer
// already in the run is not asked again, so a run cut short by its
// deadline goes on where it stopped; one cut short by the budget is
// finished with what it has.
func (self *Agent) runEvaluation(ctx context.Context, run *Run) error {
	var evaluation *models.AgentEvaluationRun
	var questions []*models.AgentEvaluationQuestion
	isDone := map[string]bool{}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if evaluation, err = tx.GetAgentEvaluationRun(run.Agent.ID, run.Job.SubjectID); err != nil || evaluation == nil || evaluation.FinishedAt != nil {
			evaluation = nil
			return err
		}
		if questions, err = tx.ListAgentEvaluationQuestions(run.Agent.ID, []models.EvaluationQuestionState{models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected}); err != nil {
			return err
		}
		answers, err := tx.ListAgentEvaluationAnswers(evaluation.ID)
		for _, answer := range answers {
			isDone[answer.QuestionID+"/"+answer.AnswerFrom] = true
		}
		return err
	}); err != nil || evaluation == nil {
		return err
	}

answering:
	for _, question := range questions {
		for _, answerFrom := range evaluationSources {
			if isDone[question.ID+"/"+answerFrom] {
				continue
			}
			if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
				return RequireBudget(tx, run.Configuration(), run.Agent, run.Owner, time.Now())
			}); err != nil {
				log.Noticef("the evaluation of agent %q stops where it is: %s", run.Agent.ID, err)
				break answering
			}
			answer := &models.AgentEvaluationAnswer{RunID: evaluation.ID, QuestionID: question.ID, AnswerFrom: answerFrom}
			evaluated, err := self.EvaluateAnswer(ctx, run.Agent, run.Owner, question.QuestionText, question.ExpectedAnswer, question.OutdatedAnswer, answerFrom)
			if err != nil {
				if ctx.Err() != nil {
					return err
				}
				// One question that cannot be answered does not end the
				// run: it is ungraded, and counts as nothing.
				answer.AnswerVerdict, answer.VerdictReason = AnswerUngraded, err.Error()
			} else {
				answer.AnswerVerdict, answer.VerdictReason, answer.AnswerText, answer.Cost = evaluated.AnswerVerdict, evaluated.VerdictReason, evaluated.AnswerText, evaluated.Cost
			}
			if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
				_, err := tx.PutAgentEvaluationAnswer(answer)
				return err
			}); err != nil {
				return err
			}
		}
	}

	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		answers, err := tx.ListAgentEvaluationAnswers(evaluation.ID)
		if err != nil {
			return err
		}
		scoreEvaluationRun(evaluation, questions, answers)
		now := time.Now()
		evaluation.FinishedAt = &now
		if err := tx.FinishAgentEvaluationRun(evaluation); err != nil {
			return err
		}
		log.Noticef("evaluated agent %q: %s", run.Agent.ID, describeEvaluationRun(evaluation))
		return nil
	})
}

// scoreEvaluationRun fills in a run's totals from its answers.
func scoreEvaluationRun(evaluation *models.AgentEvaluationRun, questions []*models.AgentEvaluationQuestion, answers []*models.AgentEvaluationAnswer) {
	questionOf := map[string]*models.AgentEvaluationQuestion{}
	for _, question := range questions {
		questionOf[question.ID] = question
	}
	type tally struct {
		score, scoreWithoutFiledAfter  float64
		answeredCount, filedAfterCount int
		verdictCounts                  map[string]int
	}
	tallies := map[string]*tally{}
	for _, answerFrom := range evaluationSources {
		tallies[answerFrom] = &tally{verdictCounts: map[string]int{}}
	}
	answered := map[string]bool{}
	evaluation.Cost = 0
	for _, answer := range answers {
		evaluation.Cost += answer.Cost
		question, total := questionOf[answer.QuestionID], tallies[answer.AnswerFrom]
		if question == nil || total == nil {
			continue
		}
		answered[question.ID] = true
		score := models.EvaluationAnswerScore(question.QuestionKind, answer.AnswerVerdict)
		total.answeredCount++
		total.score += score
		total.verdictCounts[answer.AnswerVerdict]++
		if question.IsAnswerFiledAfter {
			total.filedAfterCount++
		} else {
			total.scoreWithoutFiledAfter += score
		}
	}
	evaluation.QuestionCount = len(answered)
	evaluation.SourceScores = []*models.EvaluationSourceScore{}
	for _, answerFrom := range evaluationSources {
		total := tallies[answerFrom]
		if total.answeredCount == 0 {
			continue
		}
		scored := &models.EvaluationSourceScore{
			AnswerFrom: answerFrom, AnsweredCount: total.answeredCount, FiledAfterCount: total.filedAfterCount,
			ScorePercent: total.score / float64(total.answeredCount) * 100, VerdictCounts: total.verdictCounts,
		}
		if withoutCount := total.answeredCount - total.filedAfterCount; withoutCount > 0 {
			scored.ScorePercentWithoutFiledAfter = total.scoreWithoutFiledAfter / float64(withoutCount) * 100
		}
		evaluation.SourceScores = append(evaluation.SourceScores, scored)
	}
}

// describeEvaluationRun is a run's scores in a line, for a log.
func describeEvaluationRun(evaluation *models.AgentEvaluationRun) string {
	line := fmt.Sprintf("%d question(s)", evaluation.QuestionCount)
	for _, scored := range evaluation.SourceScores {
		line += fmt.Sprintf(", %s %.0f%%", scored.AnswerFrom, scored.ScorePercent)
	}
	return line
}
