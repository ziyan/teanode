package db

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
)

// EvaluationOperation is the person's memory check: the questions on
// record, and (with Milestone 5) the runs that grade the agent on them.
type EvaluationOperation interface {
	// CreateAgentEvaluationQuestion records a question: put to the person,
	// supplied by them, or imported from a file.
	CreateAgentEvaluationQuestion(question *models.AgentEvaluationQuestion) (*models.AgentEvaluationQuestion, error)

	// GetAgentEvaluationQuestion is one of the agent's questions, or nil.
	GetAgentEvaluationQuestion(agentId, questionId string) (*models.AgentEvaluationQuestion, error)

	// UpdateAgentEvaluationQuestion changes one of the agent's questions.
	UpdateAgentEvaluationQuestion(agentId, questionId string, modify func(*models.AgentEvaluationQuestion) error) (*models.AgentEvaluationQuestion, error)

	// ListAgentEvaluationQuestions is the agent's questions, newest first,
	// in any of the states given, or in every state when none are.
	ListAgentEvaluationQuestions(agentId string, questionStates []models.EvaluationQuestionState) ([]*models.AgentEvaluationQuestion, error)

	// ListAgentFactsToCheck is a random few facts from the person's own
	// pages of one sort (FactsToCheckStated, FactsToCheckDated or
	// FactsToCheckChanged), leaving out those given.
	ListAgentFactsToCheck(agentId, factsToCheck string, excludeFactIds []string, limit int) ([]*models.AgentFact, error)

	// CountAgentFactsToCheck is how many stated facts the person's own
	// pages hold.
	CountAgentFactsToCheck(agentId string) (int64, error)
}

type agentEvaluationQuestionModel struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	AgentID            string         `gorm:"column:agent_id"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	ModifiedAt         time.Time      `gorm:"column:modified_at"`
	QuestionKind       string         `gorm:"column:question_kind"`
	QuestionText       string         `gorm:"column:question_text"`
	ExpectedAnswer     string         `gorm:"column:expected_answer"`
	OutdatedAnswer     string         `gorm:"column:outdated_answer"`
	QuestionState      string         `gorm:"column:question_state"`
	SourceFactIDs      pq.StringArray `gorm:"column:source_fact_ids;type:varchar(32)[]"`
	IsAnswerFiledAfter bool           `gorm:"column:is_answer_filed_after"`
	ConversationID     string         `gorm:"column:conversation_id"`
	AnsweredAt         *time.Time     `gorm:"column:answered_at"`
}

func (agentEvaluationQuestionModel) TableName() string { return "agent_evaluation_question" }

func (self *agentEvaluationQuestionModel) toModel() *models.AgentEvaluationQuestion {
	sourceFactIds := []string(self.SourceFactIDs)
	if sourceFactIds == nil {
		sourceFactIds = []string{}
	}
	return &models.AgentEvaluationQuestion{
		ID: self.ID, AgentID: self.AgentID,
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
		QuestionKind: models.EvaluationQuestionKind(self.QuestionKind), QuestionText: self.QuestionText,
		ExpectedAnswer: self.ExpectedAnswer, OutdatedAnswer: self.OutdatedAnswer,
		QuestionState: models.EvaluationQuestionState(self.QuestionState), SourceFactIDs: sourceFactIds,
		IsAnswerFiledAfter: self.IsAnswerFiledAfter, ConversationID: self.ConversationID,
		AnsweredAt: localTime(self.AnsweredAt),
	}
}

func agentEvaluationQuestionToModel(question *models.AgentEvaluationQuestion) *agentEvaluationQuestionModel {
	sourceFactIds := question.SourceFactIDs
	if sourceFactIds == nil {
		sourceFactIds = []string{}
	}
	return &agentEvaluationQuestionModel{
		ID: question.ID, AgentID: question.AgentID, CreatedAt: question.CreatedAt, ModifiedAt: question.ModifiedAt,
		QuestionKind: string(question.QuestionKind), QuestionText: question.QuestionText,
		ExpectedAnswer: question.ExpectedAnswer, OutdatedAnswer: question.OutdatedAnswer,
		QuestionState: string(question.QuestionState), SourceFactIDs: pq.StringArray(sourceFactIds),
		IsAnswerFiledAfter: question.IsAnswerFiledAfter, ConversationID: question.ConversationID,
		AnsweredAt: question.AnsweredAt,
	}
}

// validEvaluationQuestion trims a question and says what is wrong with it.
func validEvaluationQuestion(question *models.AgentEvaluationQuestion) error {
	question.QuestionText = strings.TrimSpace(question.QuestionText)
	question.ExpectedAnswer = strings.TrimSpace(question.ExpectedAnswer)
	question.OutdatedAnswer = strings.TrimSpace(question.OutdatedAnswer)
	if question.QuestionText == "" {
		return fmt.Errorf("db: a question needs words")
	}
	if question.QuestionKind == "" {
		question.QuestionKind = models.EvaluationQuestionDirect
	}
	if !slices.Contains(models.EvaluationQuestionKinds, question.QuestionKind) {
		return fmt.Errorf("db: %q is not a kind of question", question.QuestionKind)
	}
	if question.QuestionState == "" {
		question.QuestionState = models.EvaluationQuestionAsked
	}
	switch question.QuestionState {
	case models.EvaluationQuestionAsked, models.EvaluationQuestionConfirmed, models.EvaluationQuestionCorrected,
		models.EvaluationQuestionDropped, models.EvaluationQuestionUnsure:
	default:
		return fmt.Errorf("db: %q is not a state a question can be in", question.QuestionState)
	}
	// A question whose answer the person stands behind has one.
	if question.QuestionState.IsEvaluated() && question.ExpectedAnswer == "" {
		return fmt.Errorf("db: a %s question needs its answer", question.QuestionState)
	}
	return nil
}

func (self *transaction) CreateAgentEvaluationQuestion(question *models.AgentEvaluationQuestion) (*models.AgentEvaluationQuestion, error) {
	created := *question
	if created.AgentID == "" {
		return nil, fmt.Errorf("db: a question needs its agent")
	}
	if err := validEvaluationQuestion(&created); err != nil {
		return nil, err
	}
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if err := self.tx.Create(agentEvaluationQuestionToModel(&created)).Error; err != nil {
		return nil, err
	}
	return self.GetAgentEvaluationQuestion(created.AgentID, created.ID)
}

func (self *transaction) GetAgentEvaluationQuestion(agentId, questionId string) (*models.AgentEvaluationQuestion, error) {
	var found []agentEvaluationQuestionModel
	if err := self.tx.Where(`"id" = ? AND "agent_id" = ?`, questionId, agentId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) UpdateAgentEvaluationQuestion(agentId, questionId string, modify func(*models.AgentEvaluationQuestion) error) (*models.AgentEvaluationQuestion, error) {
	var locked agentEvaluationQuestionModel
	if err := lockRow(self.tx, &locked, questionId); err != nil {
		return nil, err
	}
	if locked.AgentID != agentId {
		return nil, ErrNotFound
	}
	question := locked.toModel()
	if err := modify(question); err != nil {
		return nil, err
	}
	if err := validEvaluationQuestion(question); err != nil {
		return nil, err
	}
	// Who it belongs to and when it was made are not the modifier's.
	question.ID, question.AgentID, question.CreatedAt = locked.ID, locked.AgentID, locked.CreatedAt
	question.ModifiedAt = time.Now()
	if err := self.tx.Save(agentEvaluationQuestionToModel(question)).Error; err != nil {
		return nil, err
	}
	return self.GetAgentEvaluationQuestion(agentId, questionId)
}

func (self *transaction) ListAgentEvaluationQuestions(agentId string, questionStates []models.EvaluationQuestionState) ([]*models.AgentEvaluationQuestion, error) {
	query := self.tx.Where(`"agent_id" = ?`, agentId)
	if len(questionStates) > 0 {
		states := make([]string, 0, len(questionStates))
		for _, state := range questionStates {
			states = append(states, string(state))
		}
		query = query.Where(`"question_state" IN ?`, states)
	}
	var found []agentEvaluationQuestionModel
	if err := query.Order(`"created_at" DESC, "id" DESC`).Find(&found).Error; err != nil {
		return nil, err
	}
	questions := make([]*models.AgentEvaluationQuestion, 0, len(found))
	for index := range found {
		questions = append(questions, found[index].toModel())
	}
	return questions, nil
}

// The facts a memory check asks about, by what they are.
const (
	// FactsToCheckStated are facts as they stand.
	FactsToCheckStated = "stated"
	// FactsToCheckDated are events with a date, which a question can ask
	// the when of.
	FactsToCheckDated = "dated"
	// FactsToCheckChanged are facts a later one replaced, which a question
	// can ask about as something that changed.
	FactsToCheckChanged = "changed"
)

// ownPagesCondition keeps to the pages about the person and their world:
// themselves, the people they know, their things and places. Work and
// sources are left out: a memory check is about what the person can
// answer without looking anything up.
const ownPagesCondition = `"node_id" IN (SELECT "id" FROM "agent_node" WHERE "agent_id" = ? AND ("path" = 'self' OR "path" LIKE 'self/%' OR "path" LIKE 'people/%' OR "path" LIKE 'things/%' OR "path" LIKE 'places/%'))`

// ListAgentFactsToCheck is a random few of the facts on the person's own
// pages that a memory check could ask about: of one sort, stated rather
// than inferred, and none of those given.
func (self *transaction) ListAgentFactsToCheck(agentId, factsToCheck string, excludeFactIds []string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 5
	}
	query := self.tx.Where(`"agent_id" = ? AND NOT "dormant" AND NOT "inferred"`, agentId).Where(ownPagesCondition, agentId)
	switch factsToCheck {
	case FactsToCheckStated:
		query = query.Where(`"superseded_by" IS NULL`)
	case FactsToCheckDated:
		query = query.Where(`"superseded_by" IS NULL AND "happened_at" IS NOT NULL`)
	case FactsToCheckChanged:
		query = query.Where(`"superseded_by" IS NOT NULL`)
	default:
		return nil, fmt.Errorf("db: %q is not a sort of fact to check", factsToCheck)
	}
	if excluded := uniqueStrings(excludeFactIds); len(excluded) > 0 {
		query = query.Where(`"id" NOT IN ?`, excluded)
	}
	return self.factsFrom(query.Order(`random()`).Limit(limit))
}

// CountAgentFactsToCheck is how many stated facts the person's own pages
// hold: whether memory knows enough about them for a check to be worth
// their time.
func (self *transaction) CountAgentFactsToCheck(agentId string) (int64, error) {
	var count int64
	err := self.tx.Model(&agentFactModel{}).
		Where(`"agent_id" = ? AND NOT "dormant" AND NOT "inferred" AND "superseded_by" IS NULL`, agentId).
		Where(ownPagesCondition, agentId).Count(&count).Error
	return count, err
}
