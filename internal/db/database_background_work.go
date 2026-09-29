package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// BackgroundWorkOperation keeps the agent's background work: the surveys
// and subagents it started and did not wait for, what came of each, and
// whether the conversation that started it has been told.
type BackgroundWorkOperation interface {
	// CreateAgentBackgroundWork records a piece of work, queued.
	CreateAgentBackgroundWork(work *models.AgentBackgroundWork) (*models.AgentBackgroundWork, error)

	// GetAgentBackgroundWork is one of the agent's, or nil.
	GetAgentBackgroundWork(agentId, workId string) (*models.AgentBackgroundWork, error)

	// ListAgentBackgroundWork is the agent's work, newest first.
	ListAgentBackgroundWork(agentId string, limit int) ([]*models.AgentBackgroundWork, error)

	// StartAgentBackgroundWork marks a piece of work running, and says
	// whether it is still to run: false when it was stopped or has
	// finished. One already running, whose job was claimed again after a
	// restart, runs again.
	StartAgentBackgroundWork(workId string, at time.Time) (bool, error)

	// FinishAgentBackgroundWork writes how a running piece of work ended:
	// its status, result, runs and error. It says whether it did, which
	// it does not for work that was stopped while it ran.
	FinishAgentBackgroundWork(work *models.AgentBackgroundWork) (bool, error)

	// StopAgentBackgroundWork marks one of the agent's stopped, when it is
	// queued or running, and returns it as it stands after. ErrNotFound
	// when the agent has no such work.
	StopAgentBackgroundWork(agentId, workId string, at time.Time) (*models.AgentBackgroundWork, error)

	// MarkAgentBackgroundWorkWoken notes that the conversation was told.
	MarkAgentBackgroundWorkWoken(workIds []string, at time.Time) error

	// ClaimAgentBackgroundWorkWake takes the waking of the conversation
	// for a piece of finished work, and says whether the caller has it:
	// false when the conversation has been told, or when somebody else
	// claimed it at or after claimExpiredBefore. Only the claimer wakes,
	// so two instances never both do.
	ClaimAgentBackgroundWorkWake(workId string, at, claimExpiredBefore time.Time) (bool, error)

	// ReleaseAgentBackgroundWorkWakes lets go of claims whose wake was
	// given up, for the sweep to take again without waiting for them to
	// expire.
	ReleaseAgentBackgroundWorkWakes(workIds []string) error

	// ListAgentBackgroundWorkToWake is the work that finished between the
	// two moments, done or failed, whose conversation should be woken and
	// has not been, and which nobody has claimed since claimExpiredBefore:
	// what a wake lost to a restart left behind.
	ListAgentBackgroundWorkToWake(finishedAfter, finishedBefore, claimExpiredBefore time.Time, limit int) ([]*models.AgentBackgroundWork, error)

	// FailStaleAgentBackgroundWork marks work still queued or running that
	// was made before the moment as failed, with the reason given.
	FailStaleAgentBackgroundWork(createdBefore time.Time, reason string) (int64, error)

	// ScavengeAgentBackgroundWork removes finished work made before the
	// moment.
	ScavengeAgentBackgroundWork(before time.Time) (int64, error)
}

type agentBackgroundWorkModel struct {
	ID              string     `gorm:"column:id;primaryKey"`
	AgentID         string     `gorm:"column:agent_id"`
	ConversationID  string     `gorm:"column:conversation_id"`
	WorkKind        string     `gorm:"column:work_kind"`
	Title           string     `gorm:"column:title"`
	WorkRequest     []byte     `gorm:"column:work_request;type:jsonb"`
	IsPersonPresent bool       `gorm:"column:is_person_present"`
	WorkStatus      string     `gorm:"column:work_status"`
	ResultText      string     `gorm:"column:result_text"`
	RunIDs          []byte     `gorm:"column:run_ids;type:jsonb"`
	ErrorMessage    string     `gorm:"column:error_message"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	StartedAt       *time.Time `gorm:"column:started_at"`
	FinishedAt      *time.Time `gorm:"column:finished_at"`
	WokenAt         *time.Time `gorm:"column:woken_at"`
	WakeClaimedAt   *time.Time `gorm:"column:wake_claimed_at"`
}

func (agentBackgroundWorkModel) TableName() string { return "agent_background_work" }

func (self *agentBackgroundWorkModel) toModel() *models.AgentBackgroundWork {
	work := &models.AgentBackgroundWork{
		ID: self.ID, AgentID: self.AgentID, ConversationID: self.ConversationID,
		WorkKind: models.AgentBackgroundWorkKind(self.WorkKind), Title: self.Title,
		IsPersonPresent: self.IsPersonPresent, WorkStatus: models.AgentBackgroundWorkStatus(self.WorkStatus),
		ResultText: self.ResultText, RunIDs: []string{}, ErrorMessage: self.ErrorMessage,
		CreatedAt: self.CreatedAt.In(time.Local), StartedAt: localTime(self.StartedAt),
		FinishedAt: localTime(self.FinishedAt), WokenAt: localTime(self.WokenAt),
		WakeClaimedAt: localTime(self.WakeClaimedAt),
	}
	if len(self.WorkRequest) > 0 {
		if err := json.Unmarshal(self.WorkRequest, &work.WorkRequest); err != nil {
			log.Warningf("the request of background work %q could not be read: %s", self.ID, err)
		}
	}
	if len(self.RunIDs) > 0 {
		if err := json.Unmarshal(self.RunIDs, &work.RunIDs); err != nil {
			log.Warningf("the runs of background work %q could not be read: %s", self.ID, err)
		}
	}
	return work
}

func (self *transaction) CreateAgentBackgroundWork(work *models.AgentBackgroundWork) (*models.AgentBackgroundWork, error) {
	if work.AgentID == "" {
		return nil, fmt.Errorf("%w: background work needs its agent", ErrInvalidArguments)
	}
	switch work.WorkKind {
	case models.BackgroundWorkSurvey, models.BackgroundWorkSubagent:
	default:
		return nil, fmt.Errorf("%w: %q is not a kind of background work", ErrInvalidArguments, work.WorkKind)
	}
	request, err := json.Marshal(work.WorkRequest)
	if err != nil {
		return nil, err
	}
	model := &agentBackgroundWorkModel{
		ID: newID(), AgentID: work.AgentID, ConversationID: work.ConversationID,
		WorkKind: string(work.WorkKind), Title: strings.TrimSpace(work.Title), WorkRequest: request,
		IsPersonPresent: work.IsPersonPresent, WorkStatus: string(models.BackgroundWorkQueued),
		RunIDs: []byte("[]"), CreatedAt: time.Now(),
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel(), nil
}

func (self *transaction) GetAgentBackgroundWork(agentId, workId string) (*models.AgentBackgroundWork, error) {
	var found []agentBackgroundWorkModel
	if err := self.tx.Where(`"id" = ? AND "agent_id" = ?`, workId, agentId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListAgentBackgroundWork(agentId string, limit int) ([]*models.AgentBackgroundWork, error) {
	if limit <= 0 {
		limit = 20
	}
	var found []agentBackgroundWorkModel
	if err := self.tx.Where(`"agent_id" = ?`, agentId).Order(`"created_at" DESC, "id" DESC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	return backgroundWorkOf(found), nil
}

func (self *transaction) StartAgentBackgroundWork(workId string, at time.Time) (bool, error) {
	result := self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" = ? AND "work_status" IN ?`, workId, []string{string(models.BackgroundWorkQueued), string(models.BackgroundWorkRunning)}).
		Updates(map[string]any{"work_status": string(models.BackgroundWorkRunning), "started_at": at})
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) FinishAgentBackgroundWork(work *models.AgentBackgroundWork) (bool, error) {
	if !work.WorkStatus.IsFinished() {
		return false, fmt.Errorf("%w: %q is not how background work ends", ErrInvalidArguments, work.WorkStatus)
	}
	runIds := work.RunIDs
	if runIds == nil {
		runIds = []string{}
	}
	written, err := json.Marshal(runIds)
	if err != nil {
		return false, err
	}
	finishedAt := time.Now()
	if work.FinishedAt != nil {
		finishedAt = *work.FinishedAt
	}
	result := self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" = ? AND "work_status" = ?`, work.ID, string(models.BackgroundWorkRunning)).
		Updates(map[string]any{
			"work_status": string(work.WorkStatus), "result_text": work.ResultText, "run_ids": written,
			"error_message": work.ErrorMessage, "finished_at": finishedAt,
		})
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) StopAgentBackgroundWork(agentId, workId string, at time.Time) (*models.AgentBackgroundWork, error) {
	if err := self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" = ? AND "agent_id" = ? AND "work_status" IN ?`, workId, agentId, []string{string(models.BackgroundWorkQueued), string(models.BackgroundWorkRunning)}).
		Updates(map[string]any{"work_status": string(models.BackgroundWorkStopped), "finished_at": at}).Error; err != nil {
		return nil, err
	}
	work, err := self.GetAgentBackgroundWork(agentId, workId)
	if err != nil {
		return nil, err
	}
	if work == nil {
		return nil, ErrNotFound
	}
	return work, nil
}

func (self *transaction) MarkAgentBackgroundWorkWoken(workIds []string, at time.Time) error {
	if len(workIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" IN ? AND "woken_at" IS NULL`, uniqueStrings(workIds)).
		Update("woken_at", at).Error
}

func (self *transaction) ClaimAgentBackgroundWorkWake(workId string, at, claimExpiredBefore time.Time) (bool, error) {
	result := self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" = ? AND "woken_at" IS NULL AND ("wake_claimed_at" IS NULL OR "wake_claimed_at" < ?)`, workId, claimExpiredBefore).
		Update("wake_claimed_at", at)
	return result.RowsAffected == 1, result.Error
}

func (self *transaction) ReleaseAgentBackgroundWorkWakes(workIds []string) error {
	if len(workIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"id" IN ? AND "woken_at" IS NULL`, uniqueStrings(workIds)).
		Update("wake_claimed_at", nil).Error
}

func (self *transaction) ListAgentBackgroundWorkToWake(finishedAfter, finishedBefore, claimExpiredBefore time.Time, limit int) ([]*models.AgentBackgroundWork, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []agentBackgroundWorkModel
	if err := self.tx.
		Where(`"work_status" IN ? AND "conversation_id" <> '' AND "is_person_present" AND "woken_at" IS NULL`,
			[]string{string(models.BackgroundWorkDone), string(models.BackgroundWorkFailed)}).
		Where(`"finished_at" > ? AND "finished_at" <= ?`, finishedAfter, finishedBefore).
		Where(`("wake_claimed_at" IS NULL OR "wake_claimed_at" < ?)`, claimExpiredBefore).
		Order(`"finished_at" ASC`).Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	return backgroundWorkOf(found), nil
}

func (self *transaction) FailStaleAgentBackgroundWork(createdBefore time.Time, reason string) (int64, error) {
	result := self.tx.Model(&agentBackgroundWorkModel{}).
		Where(`"work_status" IN ? AND "created_at" < ?`, []string{string(models.BackgroundWorkQueued), string(models.BackgroundWorkRunning)}, createdBefore).
		Updates(map[string]any{"work_status": string(models.BackgroundWorkFailed), "error_message": reason, "finished_at": time.Now()})
	return result.RowsAffected, result.Error
}

func (self *transaction) ScavengeAgentBackgroundWork(before time.Time) (int64, error) {
	result := self.tx.Where(`"work_status" IN ? AND "created_at" < ?`,
		[]string{string(models.BackgroundWorkDone), string(models.BackgroundWorkFailed), string(models.BackgroundWorkStopped)}, before).
		Delete(&agentBackgroundWorkModel{})
	return result.RowsAffected, result.Error
}

func backgroundWorkOf(found []agentBackgroundWorkModel) []*models.AgentBackgroundWork {
	works := make([]*models.AgentBackgroundWork, 0, len(found))
	for index := range found {
		works = append(works, found[index].toModel())
	}
	return works
}
