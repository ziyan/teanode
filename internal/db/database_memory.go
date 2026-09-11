package db

import (
	"encoding/json"
	"fmt"
	"github.com/lib/pq"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// MemoryOperation is what the agent keeps between conversations: memories,
// corrections, schedules, and a conversation's task list.
type MemoryOperation interface {
	CreateAgentMemory(memory *models.AgentMemory) (*models.AgentMemory, error)
	GetAgentMemory(memoryId string) (*models.AgentMemory, error)
	UpdateAgentMemory(memoryId string, modify func(*models.AgentMemory) error) (*models.AgentMemory, error)
	DeleteAgentMemory(memoryId string) error

	// ListAgentMemories is an agent's memories, pinned first, then by last
	// use; for an audience when one is given.
	ListAgentMemories(agentId string, audience models.AgentAudience, limit int) ([]*models.AgentMemory, error)

	// SearchAgentMemories is the memories whose title, content or tags
	// contain the words.
	SearchAgentMemories(agentId, query string, limit int) ([]*models.AgentMemory, error)

	// RecallAgentMemories is the memories any one of the words touches,
	// for putting what is already known in front of a turn. Where the
	// search above narrows with every word, this widens: a sentence is a
	// handful of chances to remember, not a filter.
	RecallAgentMemories(agentId string, words []string, limit int) ([]*models.AgentMemory, error)

	// ListAgentMemoriesWithVector is every memory this model has given a
	// vector to, for ranking a turn against; ListAgentMemoriesWithoutVector
	// is the ones still waiting for one. PutAgentMemoryVector writes it.
	ListAgentMemoriesWithVector(agentId, model string) ([]*models.AgentMemory, error)
	ListAgentMemoriesWithoutVector(agentId, model string, limit int) ([]*models.AgentMemory, error)
	PutAgentMemoryVector(memoryId, model string, vector []float32) error

	// TouchAgentMemories marks memories used now.
	TouchAgentMemories(memoryIds []string, at time.Time) error

	// DeleteAgentMemories forgets everything an agent remembers.
	DeleteAgentMemories(agentId string) (int64, error)

	CreateAgentFeedback(feedback *models.AgentFeedback) (*models.AgentFeedback, error)
	ListAgentFeedback(agentId string, kinds []models.AgentFeedbackKind, limit int) ([]*models.AgentFeedback, error)
	ScavengeAgentFeedback(before time.Time) (int64, error)

	CreateAgentSchedule(schedule *models.AgentSchedule) (*models.AgentSchedule, error)
	GetAgentSchedule(scheduleId string) (*models.AgentSchedule, error)
	UpdateAgentSchedule(scheduleId string, modify func(*models.AgentSchedule) error) (*models.AgentSchedule, error)
	DeleteAgentSchedule(scheduleId string) error
	ListAgentSchedules(agentId string) ([]*models.AgentSchedule, error)

	// ListDueAgentSchedules is every enabled schedule whose next run is at
	// or before the moment, locked for the caller.
	ListDueAgentSchedules(now time.Time, limit int) ([]*models.AgentSchedule, error)

	CreateAgentTodo(todo *models.AgentTodo) (*models.AgentTodo, error)
	UpdateAgentTodo(todoId string, modify func(*models.AgentTodo) error) (*models.AgentTodo, error)
	DeleteAgentTodo(conversationId, todoId string) error
	ListAgentTodos(conversationId string) ([]*models.AgentTodo, error)
}

type agentMemoryModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	ModifiedAt time.Time  `gorm:"column:modified_at"`
	AgentID    string     `gorm:"column:agent_id"`
	Title      string     `gorm:"column:title"`
	Content    string     `gorm:"column:content"`
	Tags       []byte     `gorm:"column:tags;type:jsonb"`
	AppliesTo  []byte     `gorm:"column:applies_to;type:jsonb"`
	Pinned     bool       `gorm:"column:pinned"`
	UsedAt     *time.Time `gorm:"column:used_at"`
	// Vector is what the memory means, and VectorModel what said so: a
	// vector is only comparable with others from the same model.
	Vector      pq.Float32Array `gorm:"column:vector;type:real[]"`
	VectorModel string          `gorm:"column:vector_model"`
}

func (agentMemoryModel) TableName() string { return "agent_memory" }

func memoryToModel(memory *models.AgentMemory) (*agentMemoryModel, error) {
	tags := memory.Tags
	if tags == nil {
		tags = []string{}
	}
	appliesTo := memory.AppliesTo
	if appliesTo == nil {
		appliesTo = []models.AgentAudience{}
	}
	encodedTags, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}
	encodedAudiences, err := json.Marshal(appliesTo)
	if err != nil {
		return nil, err
	}
	return &agentMemoryModel{ID: memory.ID, CreatedAt: memory.CreatedAt, ModifiedAt: memory.ModifiedAt, AgentID: memory.AgentID, Title: memory.Title, Content: memory.Content, Tags: encodedTags, AppliesTo: encodedAudiences, Pinned: memory.Pinned, UsedAt: memory.UsedAt, Vector: pq.Float32Array(memory.Vector), VectorModel: memory.VectorModel}, nil
}

func (self *agentMemoryModel) toModel() (*models.AgentMemory, error) {
	memory := &models.AgentMemory{ID: self.ID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, AgentID: self.AgentID, Title: self.Title, Content: self.Content, Pinned: self.Pinned, UsedAt: self.UsedAt, Vector: []float32(self.Vector), VectorModel: self.VectorModel, Tags: []string{}, AppliesTo: []models.AgentAudience{}}
	if len(self.Tags) > 0 {
		if err := json.Unmarshal(self.Tags, &memory.Tags); err != nil {
			return nil, err
		}
	}
	if len(self.AppliesTo) > 0 {
		if err := json.Unmarshal(self.AppliesTo, &memory.AppliesTo); err != nil {
			return nil, err
		}
	}
	return memory, nil
}

func (self *transaction) CreateAgentMemory(memory *models.AgentMemory) (*models.AgentMemory, error) {
	if err := memory.Validate(); err != nil {
		return nil, err
	}
	if memory.AgentID == "" {
		return nil, fmt.Errorf("db: a memory needs an agent")
	}
	created := *memory
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	model, err := memoryToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) GetAgentMemory(memoryId string) (*models.AgentMemory, error) {
	var found []agentMemoryModel
	if err := self.tx.Where("\"id\" = ?", memoryId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

func (self *transaction) UpdateAgentMemory(memoryId string, modify func(*models.AgentMemory) error) (*models.AgentMemory, error) {
	var found []agentMemoryModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", memoryId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	memory, err := found[0].toModel()
	if err != nil {
		return nil, err
	}
	if err := modify(memory); err != nil {
		return nil, err
	}
	if err := memory.Validate(); err != nil {
		return nil, err
	}
	memory.ID = memoryId
	memory.ModifiedAt = time.Now()
	model, err := memoryToModel(memory)
	if err != nil {
		return nil, err
	}
	if err := self.tx.Save(model).Error; err != nil {
		return nil, err
	}
	return memory, nil
}

func (self *transaction) DeleteAgentMemory(memoryId string) error {
	return self.tx.Where("\"id\" = ?", memoryId).Delete(&agentMemoryModel{}).Error
}

func (self *transaction) memoriesFrom(query *gorm.DB) ([]*models.AgentMemory, error) {
	var found []agentMemoryModel
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	memories := make([]*models.AgentMemory, 0, len(found))
	for index := range found {
		memory, err := found[index].toModel()
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	return memories, nil
}

func (self *transaction) ListAgentMemories(agentId string, audience models.AgentAudience, limit int) ([]*models.AgentMemory, error) {
	query := self.tx.Where("\"agent_id\" = ?", agentId)
	if audience != "" {
		encoded, _ := json.Marshal([]models.AgentAudience{audience})
		query = query.Where("\"applies_to\" @> ?::jsonb", string(encoded))
	}
	query = query.Order("\"pinned\" DESC, \"used_at\" DESC NULLS LAST, \"modified_at\" DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	return self.memoriesFrom(query)
}

func (self *transaction) SearchAgentMemories(agentId, query string, limit int) ([]*models.AgentMemory, error) {
	words := strings.Fields(strings.ToLower(query))
	statement := self.tx.Where("\"agent_id\" = ?", agentId)
	for _, word := range words {
		pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(word, "%", "\\%"), "_", "\\_") + "%"
		statement = statement.Where("(LOWER(\"title\") LIKE ? OR LOWER(\"content\") LIKE ? OR LOWER(\"tags\"::text) LIKE ?)", pattern, pattern, pattern)
	}
	statement = statement.Order("\"pinned\" DESC, \"used_at\" DESC NULLS LAST, \"modified_at\" DESC")
	if limit <= 0 {
		limit = 20
	}
	return self.memoriesFrom(statement.Limit(limit))
}

func (self *transaction) RecallAgentMemories(agentId string, words []string, limit int) ([]*models.AgentMemory, error) {
	if len(words) == 0 {
		return nil, nil
	}
	statement := self.tx.Where("\"agent_id\" = ?", agentId)
	var clauses []string
	var arguments []any
	for _, word := range words {
		pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(word), "%", "\\%"), "_", "\\_") + "%"
		clauses = append(clauses, "(LOWER(\"title\") LIKE ? OR LOWER(\"content\") LIKE ? OR LOWER(\"tags\"::text) LIKE ?)")
		arguments = append(arguments, pattern, pattern, pattern)
	}
	statement = statement.Where(strings.Join(clauses, " OR "), arguments...)
	statement = statement.Order("\"pinned\" DESC, \"used_at\" DESC NULLS LAST, \"modified_at\" DESC")
	if limit <= 0 {
		limit = 20
	}
	return self.memoriesFrom(statement.Limit(limit))
}

func (self *transaction) ListAgentMemoriesWithVector(agentId, model string) ([]*models.AgentMemory, error) {
	statement := self.tx.Where("\"agent_id\" = ? AND \"vector_model\" = ? AND \"vector\" IS NOT NULL", agentId, model)
	return self.memoriesFrom(statement.Order("\"pinned\" DESC, \"modified_at\" DESC"))
}

func (self *transaction) ListAgentMemoriesWithoutVector(agentId, model string, limit int) ([]*models.AgentMemory, error) {
	if limit <= 0 {
		limit = 20
	}
	statement := self.tx.Where("\"agent_id\" = ? AND (\"vector\" IS NULL OR \"vector_model\" <> ?)", agentId, model)
	return self.memoriesFrom(statement.Order("\"pinned\" DESC, \"modified_at\" DESC").Limit(limit))
}

func (self *transaction) PutAgentMemoryVector(memoryId, model string, vector []float32) error {
	if memoryId == "" || model == "" || len(vector) == 0 {
		return fmt.Errorf("db: a memory's vector needs the memory, the model and the vector")
	}
	return self.tx.Model(&agentMemoryModel{}).Where("\"id\" = ?", memoryId).
		Updates(map[string]any{"vector": pq.Float32Array(vector), "vector_model": model}).Error
}

func (self *transaction) TouchAgentMemories(memoryIds []string, at time.Time) error {
	if len(memoryIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentMemoryModel{}).Where("\"id\" IN ?", memoryIds).Update("used_at", at).Error
}

func (self *transaction) DeleteAgentMemories(agentId string) (int64, error) {
	result := self.tx.Where("\"agent_id\" = ?", agentId).Delete(&agentMemoryModel{})
	return result.RowsAffected, result.Error
}

type agentFeedbackModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
	AgentID   string    `gorm:"column:agent_id"`
	MailboxID string    `gorm:"column:mailbox_id"`
	Kind      string    `gorm:"column:kind"`
	MailID    string    `gorm:"column:mail_id"`
	Said      string    `gorm:"column:said"`
}

func (agentFeedbackModel) TableName() string { return "agent_feedback" }

func (self *transaction) CreateAgentFeedback(feedback *models.AgentFeedback) (*models.AgentFeedback, error) {
	if feedback.AgentID == "" || feedback.Kind == "" || strings.TrimSpace(feedback.Said) == "" {
		return nil, fmt.Errorf("db: a correction needs an agent, a kind and words")
	}
	created := *feedback
	created.ID = newID()
	created.CreatedAt = time.Now()
	model := &agentFeedbackModel{ID: created.ID, CreatedAt: created.CreatedAt, AgentID: created.AgentID, MailboxID: created.MailboxID, Kind: string(created.Kind), MailID: created.MailID, Said: created.Said}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) ListAgentFeedback(agentId string, kinds []models.AgentFeedbackKind, limit int) ([]*models.AgentFeedback, error) {
	query := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"created_at\" DESC")
	if len(kinds) > 0 {
		names := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			names = append(names, string(kind))
		}
		query = query.Where("\"kind\" IN ?", names)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var found []agentFeedbackModel
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	feedback := make([]*models.AgentFeedback, 0, len(found))
	for _, row := range found {
		feedback = append(feedback, &models.AgentFeedback{ID: row.ID, CreatedAt: row.CreatedAt, AgentID: row.AgentID, MailboxID: row.MailboxID, Kind: models.AgentFeedbackKind(row.Kind), MailID: row.MailID, Said: row.Said})
	}
	return feedback, nil
}

func (self *transaction) ScavengeAgentFeedback(before time.Time) (int64, error) {
	result := self.tx.Where("\"created_at\" < ?", before).Delete(&agentFeedbackModel{})
	return result.RowsAffected, result.Error
}

type agentScheduleModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	ModifiedAt time.Time  `gorm:"column:modified_at"`
	AgentID    string     `gorm:"column:agent_id"`
	Name       string     `gorm:"column:name"`
	Cron       string     `gorm:"column:cron"`
	Prompt     string     `gorm:"column:prompt"`
	Deliver    string     `gorm:"column:deliver"`
	Enabled    bool       `gorm:"column:enabled"`
	LastRunAt  *time.Time `gorm:"column:last_run_at"`
	NextRunAt  *time.Time `gorm:"column:next_run_at"`
}

func (agentScheduleModel) TableName() string { return "agent_schedule" }

func scheduleToModel(schedule *models.AgentSchedule) *agentScheduleModel {
	return &agentScheduleModel{ID: schedule.ID, CreatedAt: schedule.CreatedAt, ModifiedAt: schedule.ModifiedAt, AgentID: schedule.AgentID, Name: schedule.Name, Cron: schedule.Cron, Prompt: schedule.Prompt, Deliver: schedule.Deliver, Enabled: schedule.Enabled, LastRunAt: schedule.LastRunAt, NextRunAt: schedule.NextRunAt}
}

func (self *agentScheduleModel) toModel() *models.AgentSchedule {
	return &models.AgentSchedule{ID: self.ID, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, AgentID: self.AgentID, Name: self.Name, Cron: self.Cron, Prompt: self.Prompt, Deliver: self.Deliver, Enabled: self.Enabled, LastRunAt: self.LastRunAt, NextRunAt: self.NextRunAt}
}

func (self *transaction) CreateAgentSchedule(schedule *models.AgentSchedule) (*models.AgentSchedule, error) {
	if err := schedule.Validate(); err != nil {
		return nil, err
	}
	if schedule.AgentID == "" {
		return nil, fmt.Errorf("db: a schedule needs an agent")
	}
	created := *schedule
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	if created.Deliver == "" {
		created.Deliver = "drawer"
	}
	if err := self.tx.Create(scheduleToModel(&created)).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) GetAgentSchedule(scheduleId string) (*models.AgentSchedule, error) {
	var found []agentScheduleModel
	if err := self.tx.Where("\"id\" = ?", scheduleId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) UpdateAgentSchedule(scheduleId string, modify func(*models.AgentSchedule) error) (*models.AgentSchedule, error) {
	var found []agentScheduleModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", scheduleId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	schedule := found[0].toModel()
	if err := modify(schedule); err != nil {
		return nil, err
	}
	if err := schedule.Validate(); err != nil {
		return nil, err
	}
	schedule.ID = scheduleId
	schedule.ModifiedAt = time.Now()
	if err := self.tx.Save(scheduleToModel(schedule)).Error; err != nil {
		return nil, err
	}
	return schedule, nil
}

func (self *transaction) DeleteAgentSchedule(scheduleId string) error {
	return self.tx.Where("\"id\" = ?", scheduleId).Delete(&agentScheduleModel{}).Error
}

func (self *transaction) ListAgentSchedules(agentId string) ([]*models.AgentSchedule, error) {
	var found []agentScheduleModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	schedules := make([]*models.AgentSchedule, 0, len(found))
	for index := range found {
		schedules = append(schedules, found[index].toModel())
	}
	return schedules, nil
}

func (self *transaction) ListDueAgentSchedules(now time.Time, limit int) ([]*models.AgentSchedule, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []agentScheduleModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("\"enabled\" AND \"next_run_at\" IS NOT NULL AND \"next_run_at\" <= ?", now).Order("\"next_run_at\" ASC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	schedules := make([]*models.AgentSchedule, 0, len(found))
	for index := range found {
		schedules = append(schedules, found[index].toModel())
	}
	return schedules, nil
}

type agentTodoModel struct {
	ID             string     `gorm:"column:id;primaryKey"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	ConversationID string     `gorm:"column:conversation_id"`
	Text           string     `gorm:"column:text"`
	DoneAt         *time.Time `gorm:"column:done_at"`
}

func (agentTodoModel) TableName() string { return "agent_todo" }

func (self *agentTodoModel) toModel() *models.AgentTodo {
	return &models.AgentTodo{ID: self.ID, CreatedAt: self.CreatedAt, ConversationID: self.ConversationID, Text: self.Text, DoneAt: self.DoneAt}
}

func (self *transaction) CreateAgentTodo(todo *models.AgentTodo) (*models.AgentTodo, error) {
	if todo.ConversationID == "" || strings.TrimSpace(todo.Text) == "" {
		return nil, fmt.Errorf("db: a todo needs a conversation and words")
	}
	created := *todo
	created.ID = newID()
	created.CreatedAt = time.Now()
	if err := self.tx.Create(&agentTodoModel{ID: created.ID, CreatedAt: created.CreatedAt, ConversationID: created.ConversationID, Text: created.Text, DoneAt: created.DoneAt}).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) UpdateAgentTodo(todoId string, modify func(*models.AgentTodo) error) (*models.AgentTodo, error) {
	var found []agentTodoModel
	if err := self.tx.Where("\"id\" = ?", todoId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	todo := found[0].toModel()
	if err := modify(todo); err != nil {
		return nil, err
	}
	todo.ID = todoId
	if err := self.tx.Save(&agentTodoModel{ID: todo.ID, CreatedAt: todo.CreatedAt, ConversationID: todo.ConversationID, Text: todo.Text, DoneAt: todo.DoneAt}).Error; err != nil {
		return nil, err
	}
	return todo, nil
}

func (self *transaction) DeleteAgentTodo(conversationId, todoId string) error {
	return self.tx.Where("\"id\" = ? AND \"conversation_id\" = ?", todoId, conversationId).Delete(&agentTodoModel{}).Error
}

func (self *transaction) ListAgentTodos(conversationId string) ([]*models.AgentTodo, error) {
	var found []agentTodoModel
	if err := self.tx.Where("\"conversation_id\" = ?", conversationId).Order("\"created_at\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	todos := make([]*models.AgentTodo, 0, len(found))
	for index := range found {
		todos = append(todos, found[index].toModel())
	}
	return todos, nil
}
