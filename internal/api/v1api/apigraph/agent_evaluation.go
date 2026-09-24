package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AgentEvaluationQuery reads the caller's memory check.
type AgentEvaluationQuery interface {
	// The questions of the caller's memory check, newest first, in the
	// states asked for or in every state. Needs agent:use.
	ListAgentEvaluationQuestions(ctx context.Context, arguments ListAgentEvaluationQuestionsArguments) ([]*models.AgentEvaluationQuestion, error)

	// The runs that graded the caller's agent against those questions,
	// newest first, with their scores. Needs agent:use.
	ListAgentEvaluationRuns(ctx context.Context, arguments ListAgentEvaluationRunsArguments) ([]*models.AgentEvaluationRun, error)

	// Every answer of one run: each question from each source, with its
	// verdict. Needs agent:use.
	ListAgentEvaluationAnswers(ctx context.Context, arguments ListAgentEvaluationAnswersArguments) ([]*models.AgentEvaluationAnswer, error)
}

// AgentEvaluationMutation changes it.
type AgentEvaluationMutation interface {
	// Change a question of the caller's memory check: its words, its
	// answer, what used to be true, or its state; drop it by setting the
	// state to dropped. Needs agent:use.
	UpdateAgentEvaluationQuestion(ctx context.Context, arguments UpdateAgentEvaluationQuestionArguments) (*models.AgentEvaluationQuestion, error)

	// Add questions to the caller's memory check as confirmed, from a
	// question file; one whose words are already on record is skipped.
	// Says how many were added. Needs agent:use.
	ImportAgentEvaluationQuestions(ctx context.Context, arguments ImportAgentEvaluationQuestionsArguments) (int, error)

	// Grade the caller's agent against their questions now, rather than
	// at the week's end; a run already under way is the one returned.
	// Needs agent:use.
	EvaluateAgentMemoryNow(ctx context.Context) (*models.AgentEvaluationRun, error)
}

// ListAgentEvaluationRunsArguments bound the listing.
type ListAgentEvaluationRunsArguments struct {
	First int `json:"first" graphapi:"nullable"`
}

// ListAgentEvaluationAnswersArguments name the run.
type ListAgentEvaluationAnswersArguments struct {
	RunID string `json:"runId"`
}

// ListAgentEvaluationQuestionsArguments narrow the listing to some states.
type ListAgentEvaluationQuestionsArguments struct {
	QuestionStates []string `json:"questionStates" graphapi:"nullable"`
}

// UpdateAgentEvaluationQuestionArguments are what may change; what is
// left out is kept.
type UpdateAgentEvaluationQuestionArguments struct {
	QuestionID     string  `json:"questionId"`
	QuestionKind   *string `json:"questionKind" graphapi:"nullable"`
	QuestionText   *string `json:"questionText" graphapi:"nullable"`
	ExpectedAnswer *string `json:"expectedAnswer" graphapi:"nullable"`
	OutdatedAnswer *string `json:"outdatedAnswer" graphapi:"nullable"`
	QuestionState  *string `json:"questionState" graphapi:"nullable"`
}

// ImportAgentEvaluationQuestionsArguments are the questions of a file.
type ImportAgentEvaluationQuestionsArguments struct {
	Questions []ImportedEvaluationQuestion `json:"questions"`
}

// ImportedEvaluationQuestion is one question as a question file has it.
type ImportedEvaluationQuestion struct {
	QuestionKind   string `json:"questionKind" graphapi:"nullable"`
	QuestionText   string `json:"questionText"`
	ExpectedAnswer string `json:"expectedAnswer"`
	OutdatedAnswer string `json:"outdatedAnswer" graphapi:"nullable"`
}

func (self *graph) ListAgentEvaluationQuestions(ctx context.Context, arguments ListAgentEvaluationQuestionsArguments) ([]*models.AgentEvaluationQuestion, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]models.EvaluationQuestionState, 0, len(arguments.QuestionStates))
	for _, state := range arguments.QuestionStates {
		states = append(states, models.EvaluationQuestionState(strings.TrimSpace(state)))
	}
	return self.transaction(ctx).ListAgentEvaluationQuestions(found.ID, states)
}

func (self *graph) UpdateAgentEvaluationQuestion(ctx context.Context, arguments UpdateAgentEvaluationQuestionArguments) (*models.AgentEvaluationQuestion, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := self.writing(ctx).UpdateAgentEvaluationQuestion(found.ID, strings.TrimSpace(arguments.QuestionID), func(question *models.AgentEvaluationQuestion) error {
		if arguments.QuestionKind != nil {
			question.QuestionKind = models.EvaluationQuestionKind(strings.TrimSpace(*arguments.QuestionKind))
		}
		if arguments.QuestionText != nil {
			question.QuestionText = *arguments.QuestionText
		}
		if arguments.ExpectedAnswer != nil {
			question.ExpectedAnswer = *arguments.ExpectedAnswer
		}
		if arguments.OutdatedAnswer != nil {
			question.OutdatedAnswer = *arguments.OutdatedAnswer
		}
		if arguments.QuestionState != nil {
			state := models.EvaluationQuestionState(strings.TrimSpace(*arguments.QuestionState))
			if state != question.QuestionState && state != models.EvaluationQuestionDropped && question.AnsweredAt == nil {
				now := time.Now()
				question.AnsweredAt = &now
			}
			question.QuestionState = state
		}
		return nil
	})
	if errors.Is(err, db.ErrNotFound) {
		return nil, api.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return updated, nil
}

func (self *graph) ImportAgentEvaluationQuestions(ctx context.Context, arguments ImportAgentEvaluationQuestionsArguments) (int, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return 0, err
	}
	tx := self.writing(ctx)
	existing, err := tx.ListAgentEvaluationQuestions(found.ID, nil)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, question := range existing {
		known[strings.ToLower(strings.TrimSpace(question.QuestionText))] = true
	}
	now := time.Now()
	addedCount := 0
	for _, imported := range arguments.Questions {
		key := strings.ToLower(strings.TrimSpace(imported.QuestionText))
		if key == "" || known[key] {
			continue
		}
		if _, err := tx.CreateAgentEvaluationQuestion(&models.AgentEvaluationQuestion{
			AgentID: found.ID, QuestionKind: models.EvaluationQuestionKind(strings.TrimSpace(imported.QuestionKind)),
			QuestionText: imported.QuestionText, ExpectedAnswer: imported.ExpectedAnswer, OutdatedAnswer: imported.OutdatedAnswer,
			QuestionState: models.EvaluationQuestionConfirmed, SourceFactIDs: []string{}, AnsweredAt: &now,
		}); err != nil {
			return 0, fmt.Errorf("%w: %q: %s", api.ErrInvalidArguments, imported.QuestionText, err)
		}
		known[key] = true
		addedCount++
	}
	return addedCount, nil
}

func (self *graph) ListAgentEvaluationRuns(ctx context.Context, arguments ListAgentEvaluationRunsArguments) ([]*models.AgentEvaluationRun, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListAgentEvaluationRuns(found.ID, arguments.First)
}

func (self *graph) ListAgentEvaluationAnswers(ctx context.Context, arguments ListAgentEvaluationAnswersArguments) ([]*models.AgentEvaluationAnswer, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	run, err := tx.GetAgentEvaluationRun(found.ID, strings.TrimSpace(arguments.RunID))
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, api.ErrNotFound
	}
	return tx.ListAgentEvaluationAnswers(run.ID)
}

func (self *graph) EvaluateAgentMemoryNow(ctx context.Context) (*models.AgentEvaluationRun, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, fmt.Errorf("%w: no agent worker runs on this server", api.ErrInvalidArguments)
	}
	tx := self.writing(ctx)
	questions, err := tx.ListAgentEvaluationQuestions(found.ID, []models.EvaluationQuestionState{models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected})
	if err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w: there are no answered questions to grade against yet", api.ErrInvalidArguments)
	}
	return worker.QueueEvaluation(tx, found)
}
