package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// AgentOperation is the agent, its queue and its usage.
type AgentOperation interface {
	GetAgent(agentId string) (*models.Agent, error)
	GetAgentByUser(userId string) (*models.Agent, error)
	CreateAgent(agent *models.Agent) (*models.Agent, error)
	UpdateAgent(agentId string, modify func(*models.Agent) error) (*models.Agent, error)
	DeleteAgent(agentId string) error
	ListAgents(options *Options) ([]*models.Agent, error)

	// EnqueueAgentJob adds a job, unless one for the same agent, kind and
	// subject is already queued or running, in which case that one is
	// returned: the second arrival coalesces into the first.
	EnqueueAgentJob(job *models.AgentJob) (*models.AgentJob, error)

	// ClaimAgentJobs takes up to limit due jobs for this instance, skipping
	// any another instance has locked, and marks them running.
	ClaimAgentJobs(instance string, limit int, now time.Time) ([]*models.AgentJob, error)

	// FinishAgentJob records how a run ended. A retry is status queued with
	// a not_before; anything else is final.
	FinishAgentJob(jobId, claimedBy string, status models.AgentJobStatus, errorMessage string, notBefore *time.Time) error

	// ReleaseStaleAgentJobs puts back jobs claimed before the given time
	// whose instance never finished them.
	ReleaseStaleAgentJobs(before time.Time) (int64, error)

	// CancelAgentJobs cancels what is queued for an agent, or for one of its
	// mailboxes when mailboxId is given.
	CancelAgentJobs(agentId, mailboxId string) (int64, error)

	GetAgentJob(jobId string) (*models.AgentJob, error)
	ListAgentJobs(filter *AgentJobFilter, options *Options) ([]*models.AgentJob, error)
	CountAgentJobs(filter *AgentJobFilter) (int64, error)

	// ScavengeAgentJobs removes finished jobs older than the given time.
	ScavengeAgentJobs(before time.Time) (int64, error)

	// PutAgentUsage adds tokens to the hourly row for this agent, source,
	// model and kind.
	PutAgentUsage(usage *AgentUsage) error

	// SumAgentUsage totals an agent's rows since a time; an empty agentId
	// totals the whole server.
	SumAgentUsage(agentId string, since time.Time) (models.AgentUsageTotals, error)

	// QueryAgentUsage totals rows since a time under one key: "day",
	// "kind", "mailbox" or "model". An empty agentId is the whole server.
	QueryAgentUsage(agentId string, since, until time.Time, by string) ([]models.AgentUsageRow, error)

	// ScavengeAgentUsage removes rows older than the retention.
	ScavengeAgentUsage(before time.Time) (int64, error)

	// TouchUserLocation records where a person is and what they read in, as
	// their browser or command line said. The zone is kept only while the
	// account follows the browser. Not audited: crossing a border is not an
	// administrative change.
	TouchUserLocation(userId, timezone, locale string, at time.Time) error
}

// AgentJobFilter narrows a job listing.
type AgentJobFilter struct {
	AgentID  string
	Statuses []models.AgentJobStatus
	Kinds    []models.AgentJobKind
}

// AgentUsage is one addition to the usage rows.
type AgentUsage struct {
	AgentID   string
	MailboxID string
	Model     string
	Kind      string
	At        time.Time
	Values    []uint64 // models.AgentUsageValueCount long
}

type agentModel struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	ModifiedAt         time.Time  `gorm:"column:modified_at"`
	UserID             string     `gorm:"column:user_id"`
	Name               string     `gorm:"column:name"`
	Enabled            bool       `gorm:"column:enabled"`
	Instructions       string     `gorm:"column:instructions"`
	Language           string     `gorm:"column:language"`
	Voice              []byte     `gorm:"column:voice;type:jsonb"`
	Categories         []byte     `gorm:"column:categories;type:jsonb"`
	Notifications      []byte     `gorm:"column:notifications;type:jsonb"`
	Confirm            []byte     `gorm:"column:confirm;type:jsonb"`
	AskModel           string     `gorm:"column:ask_model"`
	DailyTokens        int64      `gorm:"column:daily_tokens"`
	DailyCost          float64    `gorm:"column:daily_cost"`
	OperatorDisabledAt *time.Time `gorm:"column:operator_disabled_at"`
}

func (agentModel) TableName() string { return "agent" }

type agentJobModel struct {
	ID         string     `gorm:"column:id;primaryKey"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	AgentID    string     `gorm:"column:agent_id"`
	MailboxID  string     `gorm:"column:mailbox_id"`
	Kind       string     `gorm:"column:kind"`
	SubjectID  string     `gorm:"column:subject_id"`
	Status     string     `gorm:"column:status"`
	Attempts   int        `gorm:"column:attempts"`
	NotBefore  *time.Time `gorm:"column:not_before"`
	ClaimedAt  *time.Time `gorm:"column:claimed_at"`
	ClaimedBy  string     `gorm:"column:claimed_by"`
	Error      string     `gorm:"column:error"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
}

func (agentJobModel) TableName() string { return "agent_job" }

type agentUsageModel struct {
	BackendID string        `gorm:"column:backend_id;primaryKey;size:32"`
	AgentID   string        `gorm:"column:agent_id;primaryKey;size:32"`
	MailboxID string        `gorm:"column:mailbox_id;primaryKey;size:32"`
	Model     string        `gorm:"column:model;primaryKey;size:200"`
	Kind      string        `gorm:"column:kind;primaryKey;size:16"`
	Interval  uint64        `gorm:"column:interval;primaryKey"`
	Timestamp uint64        `gorm:"column:timestamp;primaryKey"`
	Values    pq.Int64Array `gorm:"column:values;type:bigint[]"`
}

func (agentUsageModel) TableName() string { return "agent_usage" }

func agentFromModel(model *agentModel) (*models.Agent, error) {
	agent := &models.Agent{
		ID:           model.ID,
		CreatedAt:    model.CreatedAt.In(time.Local),
		ModifiedAt:   model.ModifiedAt.In(time.Local),
		UserID:       model.UserID,
		Name:         model.Name,
		Enabled:      model.Enabled,
		Instructions: model.Instructions,
		Language:     model.Language,
		Categories:   []models.AgentCategory{},
		Confirm:      []string{},
		AskModel:     model.AskModel,
		DailyTokens:  model.DailyTokens,
		DailyCost:    model.DailyCost,
	}
	if model.OperatorDisabledAt != nil {
		at := model.OperatorDisabledAt.In(time.Local)
		agent.OperatorDisabledAt = &at
	}
	if err := decodeJSON(model.Voice, &agent.Voice); err != nil {
		return nil, fmt.Errorf("db: cannot read the voice of agent %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.Categories, &agent.Categories); err != nil {
		return nil, fmt.Errorf("db: cannot read the categories of agent %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.Notifications, &agent.Notifications); err != nil {
		return nil, fmt.Errorf("db: cannot read the notifications of agent %q: %w", model.ID, err)
	}
	if err := decodeJSON(model.Confirm, &agent.Confirm); err != nil {
		return nil, fmt.Errorf("db: cannot read the confirm list of agent %q: %w", model.ID, err)
	}
	if agent.Categories == nil {
		agent.Categories = []models.AgentCategory{}
	}
	if agent.Confirm == nil {
		agent.Confirm = []string{}
	}
	return agent, nil
}

func agentToModel(agent *models.Agent) (*agentModel, error) {
	model := &agentModel{
		ID:                 agent.ID,
		CreatedAt:          agent.CreatedAt,
		ModifiedAt:         agent.ModifiedAt,
		UserID:             agent.UserID,
		Name:               agent.Name,
		Enabled:            agent.Enabled,
		Instructions:       agent.Instructions,
		Language:           agent.Language,
		AskModel:           agent.AskModel,
		DailyTokens:        agent.DailyTokens,
		DailyCost:          agent.DailyCost,
		OperatorDisabledAt: agent.OperatorDisabledAt,
	}
	var err error
	if model.Voice, err = encodeJSON(agent.Voice); err != nil {
		return nil, err
	}
	categories := agent.Categories
	if categories == nil {
		categories = []models.AgentCategory{}
	}
	if model.Categories, err = json.Marshal(categories); err != nil {
		return nil, err
	}
	if model.Notifications, err = encodeJSON(agent.Notifications); err != nil {
		return nil, err
	}
	confirm := agent.Confirm
	if confirm == nil {
		confirm = []string{}
	}
	if model.Confirm, err = json.Marshal(confirm); err != nil {
		return nil, err
	}
	return model, nil
}

// decodeJSON reads an optional JSON column; empty and "null" leave the
// target as it is.
func decodeJSON(data []byte, target any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, target)
}

// encodeJSON writes an optional value; a nil pointer becomes SQL NULL.
func encodeJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(encoded) == "null" {
		return nil, nil
	}
	return encoded, nil
}

func agentJobFromModel(model *agentJobModel) *models.AgentJob {
	job := &models.AgentJob{
		ID:        model.ID,
		CreatedAt: model.CreatedAt.In(time.Local),
		AgentID:   model.AgentID,
		MailboxID: model.MailboxID,
		Kind:      models.AgentJobKind(model.Kind),
		SubjectID: model.SubjectID,
		Status:    models.AgentJobStatus(model.Status),
		Attempts:  model.Attempts,
		ClaimedBy: model.ClaimedBy,
		Error:     model.Error,
	}
	for source, target := range map[*time.Time]**time.Time{model.NotBefore: &job.NotBefore, model.ClaimedAt: &job.ClaimedAt, model.FinishedAt: &job.FinishedAt} {
		if source != nil {
			local := source.In(time.Local)
			*target = &local
		}
	}
	return job
}

func (self *transaction) GetAgent(agentId string) (*models.Agent, error) {
	var model agentModel
	result := self.tx.Where("\"id\" = ?", agentId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return agentFromModel(&model)
}

func (self *transaction) GetAgentByUser(userId string) (*models.Agent, error) {
	var model agentModel
	result := self.tx.Where("\"user_id\" = ?", userId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return agentFromModel(&model)
}

func (self *transaction) CreateAgent(agent *models.Agent) (*models.Agent, error) {
	if err := agent.Validate(); err != nil {
		return nil, err
	}
	existing, err := self.GetAgentByUser(agent.UserID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAlreadyExists
	}
	created := *agent
	created.ID = newID()
	created.CreatedAt = time.Now()
	created.ModifiedAt = created.CreatedAt
	model, err := agentToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceAgent, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(model).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAgent(created.ID)
}

func (self *transaction) UpdateAgent(agentId string, modify func(*models.Agent) error) (*models.Agent, error) {
	if err := lockRow(self.tx, &agentModel{}, agentId); err != nil {
		return nil, err
	}
	before, err := self.GetAgent(agentId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.Categories = append([]models.AgentCategory(nil), before.Categories...)
	after.Confirm = append([]string(nil), before.Confirm...)
	if before.Voice != nil {
		voice := *before.Voice
		after.Voice = &voice
	}
	if before.Notifications != nil {
		notifications := *before.Notifications
		after.Notifications = &notifications
	}
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	after.ID, after.UserID, after.CreatedAt = before.ID, before.UserID, before.CreatedAt
	after.ModifiedAt = time.Now()
	model, err := agentToModel(&after)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceAgent, agentId, models.AuditActionUpdate, before, &after, func(tx *gorm.DB) error {
		return tx.Model(&agentModel{}).Where("\"id\" = ?", agentId).Updates(map[string]any{
			"modified_at": model.ModifiedAt, "name": model.Name, "enabled": model.Enabled,
			"instructions": model.Instructions, "language": model.Language,
			"voice": model.Voice, "categories": model.Categories, "notifications": model.Notifications,
			"confirm": model.Confirm, "ask_model": model.AskModel, "daily_tokens": model.DailyTokens, "daily_cost": model.DailyCost,
			"operator_disabled_at": model.OperatorDisabledAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	return self.GetAgent(agentId)
}

func (self *transaction) DeleteAgent(agentId string) error {
	before, err := self.GetAgent(agentId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceAgent, agentId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		if err := tx.Where("\"agent_id\" = ?", agentId).Delete(&agentUsageModel{}).Error; err != nil {
			return err
		}
		return tx.Where("\"id\" = ?", agentId).Delete(&agentModel{}).Error
	})
}

func (self *transaction) ListAgents(options *Options) ([]*models.Agent, error) {
	var found []agentModel
	query := self.tx.Order("\"created_at\" ASC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	agents := make([]*models.Agent, 0, len(found))
	for index := range found {
		agent, err := agentFromModel(&found[index])
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (self *transaction) EnqueueAgentJob(job *models.AgentJob) (*models.AgentJob, error) {
	if job.AgentID == "" || job.Kind == "" {
		return nil, fmt.Errorf("db: a job needs an agent and a kind")
	}
	// One open job per agent, kind and subject: the index agent_job_open
	// is unique over the open statuses, so two deliveries of one thread
	// queuing at once insert one row between them, and the loser reads it.
	open := func() (*agentJobModel, error) {
		var existing agentJobModel
		result := self.tx.Where("\"agent_id\" = ? AND \"kind\" = ? AND \"subject_id\" = ? AND \"status\" IN ?", job.AgentID, string(job.Kind), job.SubjectID, []string{string(models.AgentJobQueued), string(models.AgentJobRunning)}).Limit(1).Find(&existing)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected > 0 {
			return &existing, nil
		}
		return nil, nil
	}
	if existing, err := open(); err != nil || existing != nil {
		if err != nil {
			return nil, err
		}
		return agentJobFromModel(existing), nil
	}
	model := &agentJobModel{
		ID:        newID(),
		CreatedAt: time.Now(),
		AgentID:   job.AgentID,
		MailboxID: job.MailboxID,
		Kind:      string(job.Kind),
		SubjectID: job.SubjectID,
		Status:    string(models.AgentJobQueued),
		NotBefore: job.NotBefore,
	}
	result := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		if existing, err := open(); err != nil || existing != nil {
			if err != nil {
				return nil, err
			}
			return agentJobFromModel(existing), nil
		}
	}
	return agentJobFromModel(model), nil
}

func (self *transaction) ClaimAgentJobs(instance string, limit int, now time.Time) ([]*models.AgentJob, error) {
	if limit <= 0 {
		return nil, nil
	}
	var due []agentJobModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("\"status\" = ? AND (\"not_before\" IS NULL OR \"not_before\" <= ?)", string(models.AgentJobQueued), now).
		Order("\"created_at\" ASC").Limit(limit).Find(&due).Error; err != nil {
		return nil, err
	}
	jobs := make([]*models.AgentJob, 0, len(due))
	for index := range due {
		if err := self.tx.Model(&agentJobModel{}).Where("\"id\" = ?", due[index].ID).Updates(map[string]any{
			"status": string(models.AgentJobRunning), "claimed_at": now, "claimed_by": instance, "attempts": gorm.Expr("\"attempts\" + 1"),
		}).Error; err != nil {
			return nil, err
		}
		due[index].Status = string(models.AgentJobRunning)
		due[index].ClaimedAt = &now
		due[index].ClaimedBy = instance
		due[index].Attempts++
		jobs = append(jobs, agentJobFromModel(&due[index]))
	}
	return jobs, nil
}

func (self *transaction) FinishAgentJob(jobId, claimedBy string, status models.AgentJobStatus, errorMessage string, notBefore *time.Time) error {
	updates := map[string]any{"status": string(status), "error": errorMessage, "not_before": notBefore}
	if status == models.AgentJobQueued {
		updates["claimed_at"] = nil
		updates["claimed_by"] = ""
	} else {
		updates["finished_at"] = time.Now()
	}
	// Only the instance that holds the job finishes it: a run that outlived
	// its claim and was handed to another instance must not overwrite what
	// that instance is doing. An empty claimant is a retry by hand.
	query := self.tx.Model(&agentJobModel{}).Where("\"id\" = ?", jobId)
	if claimedBy != "" {
		query = query.Where("\"status\" = ? AND \"claimed_by\" = ?", string(models.AgentJobRunning), claimedBy)
	}
	return query.Updates(updates).Error
}

func (self *transaction) ReleaseStaleAgentJobs(before time.Time) (int64, error) {
	result := self.tx.Model(&agentJobModel{}).Where("\"status\" = ? AND \"claimed_at\" < ?", string(models.AgentJobRunning), before).Updates(map[string]any{
		"status": string(models.AgentJobQueued), "claimed_at": nil, "claimed_by": "",
	})
	return result.RowsAffected, result.Error
}

func (self *transaction) CancelAgentJobs(agentId, mailboxId string) (int64, error) {
	query := self.tx.Model(&agentJobModel{}).Where("\"agent_id\" = ? AND \"status\" = ?", agentId, string(models.AgentJobQueued))
	if mailboxId != "" {
		query = query.Where("\"mailbox_id\" = ?", mailboxId)
	}
	result := query.Updates(map[string]any{"status": string(models.AgentJobCancelled), "finished_at": time.Now()})
	return result.RowsAffected, result.Error
}

func (self *transaction) GetAgentJob(jobId string) (*models.AgentJob, error) {
	var model agentJobModel
	result := self.tx.Where("\"id\" = ?", jobId).Limit(1).Find(&model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return agentJobFromModel(&model), nil
}

func (self *transaction) agentJobQuery(filter *AgentJobFilter) *gorm.DB {
	query := self.tx.Model(&agentJobModel{})
	if filter == nil {
		return query
	}
	if filter.AgentID != "" {
		query = query.Where("\"agent_id\" = ?", filter.AgentID)
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statuses = append(statuses, string(status))
		}
		query = query.Where("\"status\" IN ?", statuses)
	}
	if len(filter.Kinds) > 0 {
		kinds := make([]string, 0, len(filter.Kinds))
		for _, kind := range filter.Kinds {
			kinds = append(kinds, string(kind))
		}
		query = query.Where("\"kind\" IN ?", kinds)
	}
	return query
}

func (self *transaction) ListAgentJobs(filter *AgentJobFilter, options *Options) ([]*models.AgentJob, error) {
	var found []agentJobModel
	query := self.agentJobQuery(filter).Order("\"created_at\" DESC, \"id\" DESC")
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	if err := query.Find(&found).Error; err != nil {
		return nil, err
	}
	jobs := make([]*models.AgentJob, 0, len(found))
	for index := range found {
		jobs = append(jobs, agentJobFromModel(&found[index]))
	}
	return jobs, nil
}

func (self *transaction) CountAgentJobs(filter *AgentJobFilter) (int64, error) {
	var count int64
	err := self.agentJobQuery(filter).Count(&count).Error
	return count, err
}

func (self *transaction) ScavengeAgentJobs(before time.Time) (int64, error) {
	result := self.tx.Where("\"status\" IN ? AND \"finished_at\" < ?", []string{string(models.AgentJobDone), string(models.AgentJobDead), string(models.AgentJobCancelled)}, before).Delete(&agentJobModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) PutAgentUsage(usage *AgentUsage) error {
	if usage == nil || usage.AgentID == "" {
		return nil
	}
	timestamp := models.DiscretizeTimestamp(models.HourlyInterval, uint64(usage.At.Unix()))
	values := make(pq.Int64Array, models.AgentUsageValueCount)
	for index, value := range usage.Values {
		if index < len(values) {
			values[index] = int64(value)
		}
	}
	// One statement, so two runs writing the same hour add up rather than
	// one of them failing: the row is inserted, or its values are summed
	// element by element with what arrived.
	return self.tx.Exec(`INSERT INTO "agent_usage" ("backend_id", "agent_id", "mailbox_id", "model", "kind", "interval", "timestamp", "values")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT ("backend_id", "agent_id", "mailbox_id", "model", "kind", "interval", "timestamp") DO UPDATE SET "values" = ARRAY(
			SELECT COALESCE(mine.value, 0) + COALESCE(theirs.value, 0)
			FROM unnest("agent_usage"."values") WITH ORDINALITY AS mine(value, position)
			FULL OUTER JOIN unnest(EXCLUDED."values") WITH ORDINALITY AS theirs(value, position) USING (position)
			ORDER BY position)`,
		self.database.settings.BackendID, usage.AgentID, usage.MailboxID, usage.Model, usage.Kind, models.HourlyInterval, timestamp, values).Error
}

func totalsFromOrdinals(rows []struct {
	Key        string
	Ordinality uint64
	Value      int64
}) map[string]models.AgentUsageTotals {
	totals := map[string]models.AgentUsageTotals{}
	for _, row := range rows {
		total := totals[row.Key]
		switch row.Ordinality - 1 {
		case models.AgentUsagePromptTokens:
			total.PromptTokens += row.Value
		case models.AgentUsageCompletionTokens:
			total.CompletionTokens += row.Value
		case models.AgentUsageCacheReadTokens:
			total.CacheReadTokens += row.Value
		case models.AgentUsageCacheWriteTokens:
			total.CacheWriteTokens += row.Value
		case models.AgentUsageCalls:
			total.Calls += row.Value
		}
		totals[row.Key] = total
	}
	return totals
}

func (self *transaction) SumAgentUsage(agentId string, since time.Time) (models.AgentUsageTotals, error) {
	rows, err := self.QueryAgentUsage(agentId, since, time.Time{}, "")
	if err != nil {
		return models.AgentUsageTotals{}, err
	}
	var totals models.AgentUsageTotals
	for _, row := range rows {
		totals.PromptTokens += row.Totals.PromptTokens
		totals.CompletionTokens += row.Totals.CompletionTokens
		totals.CacheReadTokens += row.Totals.CacheReadTokens
		totals.CacheWriteTokens += row.Totals.CacheWriteTokens
		totals.Calls += row.Totals.Calls
	}
	return totals, nil
}

func (self *transaction) QueryAgentUsage(agentId string, since, until time.Time, by string) ([]models.AgentUsageRow, error) {
	keyExpression := "''"
	switch by {
	case "day":
		keyExpression = `to_char(to_timestamp("timestamp"), 'YYYY-MM-DD')`
	case "kind":
		keyExpression = `"kind"`
	case "mailbox":
		keyExpression = `"mailbox_id"`
	case "model":
		keyExpression = `"model"`
	case "agent":
		keyExpression = `"agent_id"`
	case "":
	default:
		return nil, fmt.Errorf("db: %q is not a usage grouping", by)
	}
	var where []string
	var arguments []any
	where = append(where, `"interval" = ?`, `"timestamp" >= ?`)
	arguments = append(arguments, models.HourlyInterval, models.DiscretizeTimestamp(models.HourlyInterval, uint64(since.Unix())))
	if !until.IsZero() {
		where = append(where, `"timestamp" < ?`)
		arguments = append(arguments, uint64(until.Unix()))
	}
	if agentId != "" {
		where = append(where, `"agent_id" = ?`)
		arguments = append(arguments, agentId)
	}
	var rows []struct {
		Key        string
		Ordinality uint64
		Value      int64
	}
	query := fmt.Sprintf(`SELECT %s AS "key", "ordinality", SUM("unnest") AS "value" FROM "agent_usage", unnest("values") WITH ORDINALITY WHERE %s GROUP BY "key", "ordinality" ORDER BY "key", "ordinality"`, keyExpression, strings.Join(where, " AND "))
	if err := self.tx.Raw(query, arguments...).Find(&rows).Error; err != nil {
		return nil, err
	}
	totals := totalsFromOrdinals(rows)
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sortStrings(keys)
	result := make([]models.AgentUsageRow, 0, len(keys))
	for _, key := range keys {
		result = append(result, models.AgentUsageRow{Key: key, Totals: totals[key]})
	}
	return result, nil
}

func (self *transaction) ScavengeAgentUsage(before time.Time) (int64, error) {
	result := self.tx.Where("\"timestamp\" < ?", uint64(before.Unix())).Delete(&agentUsageModel{})
	return result.RowsAffected, result.Error
}

func (self *transaction) TouchUserLocation(userId, timezone, locale string, at time.Time) error {
	if userId == "" {
		return nil
	}
	updates := map[string]any{"timezone_seen_at": at}
	if locale != "" {
		updates["locale_seen"] = locale
	}
	if timezone != "" {
		// Only while the account follows the browser; a pinned zone stays.
		updates["timezone"] = gorm.Expr(`CASE WHEN "timezone_mode" = 'fixed' THEN "timezone" ELSE ? END`, timezone)
	}
	return self.tx.Model(&userModel{}).Where("\"id\" = ?", userId).Updates(updates).Error
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for at := index; at > 0 && values[at] < values[at-1]; at-- {
			values[at], values[at-1] = values[at-1], values[at]
		}
	}
}
