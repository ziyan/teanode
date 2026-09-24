package db

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm/clause"

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

	// LastAgentMessageStartingWith is when a conversation's latest message
	// that begins with the words given was written, or nil.
	LastAgentMessageStartingWith(conversationId, opening string) (*time.Time, error)

	// CreateAgentEvaluationRun starts a run of the memory check.
	CreateAgentEvaluationRun(agentId string) (*models.AgentEvaluationRun, error)

	// GetAgentEvaluationRun is one of the agent's runs, or nil.
	GetAgentEvaluationRun(agentId, runId string) (*models.AgentEvaluationRun, error)

	// FinishAgentEvaluationRun writes what a run came to.
	FinishAgentEvaluationRun(run *models.AgentEvaluationRun) error

	// ListAgentEvaluationRuns is the agent's runs, newest first.
	ListAgentEvaluationRuns(agentId string, limit int) ([]*models.AgentEvaluationRun, error)

	// PutAgentEvaluationAnswer records one question answered from one
	// source in a run, replacing an earlier answer to the same.
	PutAgentEvaluationAnswer(answer *models.AgentEvaluationAnswer) (*models.AgentEvaluationAnswer, error)

	// ListAgentEvaluationAnswers is every answer of a run, oldest first.
	ListAgentEvaluationAnswers(runId string) ([]*models.AgentEvaluationAnswer, error)

	// AddAgentTip records a tip given; one given before is left as it was.
	AddAgentTip(tip *models.AgentTip) error

	// ListAgentTips is every tip the agent has given, newest first.
	ListAgentTips(agentId string) ([]*models.AgentTip, error)
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

// A memory check asks the person to confirm what the agent remembers
// about their own life, so it keeps to facts they can confirm without
// looking anything up. Being on a page about somebody is not enough: a
// colleague's page read out of a work archive is the agent's reading,
// not the person's memory, and questions drawn from it were a quiz about
// other people's work.

// toldByPersonCondition is a fact the person themselves gave: said in a
// conversation, or written or corrected by hand.
const toldByPersonCondition = `EXISTS (SELECT 1 FROM jsonb_array_elements("evidence") AS "given" WHERE "given"->>'kind' IN ('conversation', 'person', 'memory'))`

// readFromWorkCondition is a fact with any evidence from the sources work
// comes from: commits, repositories, chat archives, documents.
const readFromWorkCondition = `EXISTS (SELECT 1 FROM jsonb_array_elements("evidence") AS "given" WHERE "given"->>'kind' IN ('commit', 'repository', 'chat', 'document'))`

// checkableCondition is the facts a check may ask about: told by the
// person, on a page about them, the people they know, their things or
// places; or on the self page, a thing or a place and read from nothing
// that work comes from.
const checkableCondition = `(("node_id" IN (SELECT "id" FROM "agent_node" WHERE "agent_id" = ? AND ("path" = 'self' OR "path" LIKE 'self/%' OR "path" LIKE 'people/%' OR "path" LIKE 'things/%' OR "path" LIKE 'places/%')) AND ` + toldByPersonCondition + `)
	OR ("node_id" IN (SELECT "id" FROM "agent_node" WHERE "agent_id" = ? AND ("path" = 'self' OR "path" LIKE 'things/%' OR "path" LIKE 'places/%')) AND NOT ` + readFromWorkCondition + `))`

// ListAgentFactsToCheck is a random few of the facts a memory check may
// ask about, of one sort, stated rather than inferred, and none of those
// given: what the person told the agent first, then the rest.
func (self *transaction) ListAgentFactsToCheck(agentId, factsToCheck string, excludeFactIds []string, limit int) ([]*models.AgentFact, error) {
	if limit <= 0 {
		limit = 5
	}
	query := self.tx.Where(`"agent_id" = ? AND NOT "dormant" AND NOT "inferred"`, agentId).Where(checkableCondition, agentId, agentId)
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
	return self.factsFrom(query.Order(toldByPersonCondition + ` DESC, random()`).Limit(limit))
}

// CountAgentFactsToCheck is how many stated facts a check may ask about:
// whether memory knows enough about the person for one to be worth their
// time.
func (self *transaction) CountAgentFactsToCheck(agentId string) (int64, error) {
	var count int64
	err := self.tx.Model(&agentFactModel{}).
		Where(`"agent_id" = ? AND NOT "dormant" AND NOT "inferred" AND "superseded_by" IS NULL`, agentId).
		Where(checkableCondition, agentId, agentId).Count(&count).Error
	return count, err
}

type agentEvaluationRunModel struct {
	ID            string     `gorm:"column:id;primaryKey"`
	AgentID       string     `gorm:"column:agent_id"`
	StartedAt     time.Time  `gorm:"column:started_at"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
	QuestionCount int        `gorm:"column:question_count"`
	Cost          float64    `gorm:"column:cost"`
	SourceScores  []byte     `gorm:"column:source_scores;type:jsonb"`
}

func (agentEvaluationRunModel) TableName() string { return "agent_evaluation_run" }

func (self *agentEvaluationRunModel) toModel() (*models.AgentEvaluationRun, error) {
	run := &models.AgentEvaluationRun{
		ID: self.ID, AgentID: self.AgentID, StartedAt: self.StartedAt.In(time.Local), FinishedAt: localTime(self.FinishedAt),
		QuestionCount: self.QuestionCount, Cost: self.Cost, SourceScores: []*models.EvaluationSourceScore{},
	}
	if len(self.SourceScores) > 0 {
		if err := json.Unmarshal(self.SourceScores, &run.SourceScores); err != nil {
			return nil, err
		}
	}
	return run, nil
}

type agentEvaluationAnswerModel struct {
	ID            string    `gorm:"column:id;primaryKey"`
	RunID         string    `gorm:"column:run_id"`
	QuestionID    string    `gorm:"column:question_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	AnswerFrom    string    `gorm:"column:answer_from"`
	AnswerVerdict string    `gorm:"column:answer_verdict"`
	VerdictReason string    `gorm:"column:verdict_reason"`
	AnswerText    string    `gorm:"column:answer_text"`
	Cost          float64   `gorm:"column:cost"`
}

func (agentEvaluationAnswerModel) TableName() string { return "agent_evaluation_answer" }

func (self *agentEvaluationAnswerModel) toModel() *models.AgentEvaluationAnswer {
	return &models.AgentEvaluationAnswer{
		ID: self.ID, RunID: self.RunID, QuestionID: self.QuestionID, CreatedAt: self.CreatedAt.In(time.Local),
		AnswerFrom: self.AnswerFrom, AnswerVerdict: self.AnswerVerdict, VerdictReason: self.VerdictReason,
		AnswerText: self.AnswerText, Cost: self.Cost,
	}
}

func (self *transaction) CreateAgentEvaluationRun(agentId string) (*models.AgentEvaluationRun, error) {
	model := &agentEvaluationRunModel{ID: newID(), AgentID: agentId, StartedAt: time.Now(), SourceScores: []byte("[]")}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel()
}

func (self *transaction) GetAgentEvaluationRun(agentId, runId string) (*models.AgentEvaluationRun, error) {
	var found []agentEvaluationRunModel
	if err := self.tx.Where(`"id" = ? AND "agent_id" = ?`, runId, agentId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

func (self *transaction) FinishAgentEvaluationRun(run *models.AgentEvaluationRun) error {
	scores, err := json.Marshal(run.SourceScores)
	if err != nil {
		return err
	}
	return self.tx.Model(&agentEvaluationRunModel{}).Where(`"id" = ? AND "agent_id" = ?`, run.ID, run.AgentID).Updates(map[string]any{
		"finished_at": run.FinishedAt, "question_count": run.QuestionCount, "cost": run.Cost, "source_scores": scores,
	}).Error
}

func (self *transaction) ListAgentEvaluationRuns(agentId string, limit int) ([]*models.AgentEvaluationRun, error) {
	if limit <= 0 {
		limit = 20
	}
	var found []agentEvaluationRunModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"started_at" DESC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	runs := make([]*models.AgentEvaluationRun, 0, len(found))
	for index := range found {
		run, err := found[index].toModel()
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (self *transaction) PutAgentEvaluationAnswer(answer *models.AgentEvaluationAnswer) (*models.AgentEvaluationAnswer, error) {
	model := &agentEvaluationAnswerModel{
		ID: newID(), RunID: answer.RunID, QuestionID: answer.QuestionID, CreatedAt: time.Now(),
		AnswerFrom: answer.AnswerFrom, AnswerVerdict: answer.AnswerVerdict, VerdictReason: answer.VerdictReason,
		AnswerText: answer.AnswerText, Cost: answer.Cost,
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}, {Name: "question_id"}, {Name: "answer_from"}},
		DoUpdates: clause.AssignmentColumns([]string{"answer_verdict", "verdict_reason", "answer_text", "cost", "created_at"}),
	}).Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel(), nil
}

func (self *transaction) ListAgentEvaluationAnswers(runId string) ([]*models.AgentEvaluationAnswer, error) {
	var found []agentEvaluationAnswerModel
	if err := self.tx.Where(`"run_id" = ?`, runId).Order(`"created_at" ASC, "id" ASC`).Find(&found).Error; err != nil {
		return nil, err
	}
	answers := make([]*models.AgentEvaluationAnswer, 0, len(found))
	for index := range found {
		answers = append(answers, found[index].toModel())
	}
	return answers, nil
}

type agentTipModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	AgentID        string    `gorm:"column:agent_id"`
	TipKey         string    `gorm:"column:tip_key"`
	GivenAt        time.Time `gorm:"column:given_at"`
	ConversationID string    `gorm:"column:conversation_id"`
}

func (agentTipModel) TableName() string { return "agent_tip" }

func (self *transaction) AddAgentTip(tip *models.AgentTip) error {
	givenAt := tip.GivenAt
	if givenAt.IsZero() {
		givenAt = time.Now()
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&agentTipModel{
		ID: newID(), AgentID: tip.AgentID, TipKey: tip.TipKey, GivenAt: givenAt, ConversationID: tip.ConversationID,
	}).Error
}

func (self *transaction) ListAgentTips(agentId string) ([]*models.AgentTip, error) {
	var found []agentTipModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"given_at" DESC`).Find(&found).Error; err != nil {
		return nil, err
	}
	tips := make([]*models.AgentTip, 0, len(found))
	for _, model := range found {
		tips = append(tips, &models.AgentTip{ID: model.ID, AgentID: model.AgentID, TipKey: model.TipKey, GivenAt: model.GivenAt.In(time.Local), ConversationID: model.ConversationID})
	}
	return tips, nil
}

func (self *transaction) LastAgentMessageStartingWith(conversationId, opening string) (*time.Time, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(opening)
	var last []time.Time
	if err := self.tx.Model(&agentMessageModel{}).
		Where(`"conversation_id" = ? AND "content" LIKE ?`, conversationId, escaped+"%").
		Order(`"created_at" DESC`).Limit(1).Pluck("created_at", &last).Error; err != nil {
		return nil, err
	}
	if len(last) == 0 {
		return nil, nil
	}
	return &last[0], nil
}
