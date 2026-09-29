package db

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// AlertOperation is what might be worth telling a person about unasked,
// and what they were told.
type AlertOperation interface {
	// CreateAgentAlertCandidate adds a candidate, waiting.
	CreateAgentAlertCandidate(candidate *models.AgentAlertCandidate) (*models.AgentAlertCandidate, error)

	// ListWaitingAgentAlertCandidates is the candidates neither told nor
	// dropped, oldest first.
	ListWaitingAgentAlertCandidates(agentId string, limit int) ([]*models.AgentAlertCandidate, error)

	// ListAgentAlertCandidatesByID is these candidates of the agent,
	// whatever became of them, oldest first.
	ListAgentAlertCandidatesByID(agentId string, candidateIds []string) ([]*models.AgentAlertCandidate, error)

	// LatestAgentBurstCandidate is the newest burst candidate of a
	// mailbox with this key made since the moment given, or nil.
	LatestAgentBurstCandidate(agentId, mailboxId, burstKey string, since time.Time) (*models.AgentAlertCandidate, error)

	// ListMailSubjectsFromDomain is the subjects of the messages that
	// arrived in a mailbox since the moment given from an address at the
	// domain, leaving out Junk, Trash, Drafts and Sent: what a burst is
	// counted over. One query over the mailbox's items by the time they
	// were added, which is indexed.
	ListMailSubjectsFromDomain(mailboxId, fromDomain string, since time.Time, limit int) ([]string, error)

	// LockWaitingAgentAlertCandidates takes these candidates for telling
	// or dropping, holding them until the transaction ends, and returns
	// those still waiting: a run that finds one already told does not
	// tell it again.
	LockWaitingAgentAlertCandidates(agentId string, candidateIds []string) ([]*models.AgentAlertCandidate, error)

	// MarkAgentAlertCandidatesAlerted records the alert that told the
	// person about these candidates.
	MarkAgentAlertCandidatesAlerted(candidateIds []string, alertId string) error

	// DropAgentAlertCandidates records that these candidates were not
	// worth telling, and why.
	DropAgentAlertCandidates(candidateIds []string, dropReason string, at time.Time) error

	// CreateAgentAlert records what the person was told.
	CreateAgentAlert(alert *models.AgentAlert) (*models.AgentAlert, error)

	// ListAgentAlertsSince is what the person was told since the moment
	// given, newest first.
	ListAgentAlertsSince(agentId string, since time.Time) ([]*models.AgentAlert, error)

	// AdvanceAgentJob brings a queued job of this agent, kind and subject
	// forward to the moment given, when it was waiting for later.
	AdvanceAgentJob(agentId string, kind models.AgentJobKind, subjectId string, notBefore time.Time) error

	// GetAgentAlert is one alert of the agent, or nil.
	GetAgentAlert(agentId, alertId string) (*models.AgentAlert, error)

	// ListRecentAgentAlerts is the agent's latest alerts, newest first.
	ListRecentAgentAlerts(agentId string, limit int) ([]*models.AgentAlert, error)

	// CreateAgentAlertMute keeps a mute. One with the same scope and
	// target already there is returned as it is rather than kept twice.
	CreateAgentAlertMute(mute *models.AgentAlertMute) (*models.AgentAlertMute, error)

	// ListAgentAlertMutes is the agent's mutes, newest first.
	ListAgentAlertMutes(agentId string) ([]*models.AgentAlertMute, error)

	// DeleteAgentAlertMute removes one mute of the agent, saying whether
	// there was one.
	DeleteAgentAlertMute(agentId, muteId string) (bool, error)
}

type agentAlertCandidateModel struct {
	ID              string     `gorm:"column:id;primaryKey"`
	AgentID         string     `gorm:"column:agent_id"`
	MailboxID       string     `gorm:"column:mailbox_id"`
	MailID          string     `gorm:"column:mail_id"`
	CandidateKind   string     `gorm:"column:candidate_kind"`
	AlertSignal     string     `gorm:"column:alert_signal"`
	CandidateReason string     `gorm:"column:candidate_reason"`
	BurstKey        string     `gorm:"column:burst_key"`
	BurstCount      int        `gorm:"column:burst_count"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	AlertID         string     `gorm:"column:alert_id"`
	DroppedAt       *time.Time `gorm:"column:dropped_at"`
	DropReason      string     `gorm:"column:drop_reason"`
}

func (agentAlertCandidateModel) TableName() string { return "agent_alert_candidate" }

func (self *agentAlertCandidateModel) toModel() *models.AgentAlertCandidate {
	return &models.AgentAlertCandidate{
		ID: self.ID, AgentID: self.AgentID, MailboxID: self.MailboxID, MailID: self.MailID,
		CandidateKind: models.AlertCandidateKind(self.CandidateKind), AlertSignal: self.AlertSignal, CandidateReason: self.CandidateReason,
		BurstKey: self.BurstKey, BurstCount: self.BurstCount, CreatedAt: self.CreatedAt.In(time.Local),
		AlertID: self.AlertID, DroppedAt: self.DroppedAt, DropReason: self.DropReason,
	}
}

func (self *transaction) CreateAgentAlertCandidate(candidate *models.AgentAlertCandidate) (*models.AgentAlertCandidate, error) {
	if candidate.AgentID == "" || candidate.CandidateKind == "" {
		return nil, fmt.Errorf("db: an alert candidate needs an agent and a kind")
	}
	alertSignal := candidate.AlertSignal
	if alertSignal == "" {
		alertSignal = models.AlertSignalNone
	}
	model := &agentAlertCandidateModel{
		ID: newID(), AgentID: candidate.AgentID, MailboxID: candidate.MailboxID, MailID: candidate.MailID,
		CandidateKind: string(candidate.CandidateKind), AlertSignal: alertSignal, CandidateReason: candidate.CandidateReason,
		BurstKey: candidate.BurstKey, BurstCount: candidate.BurstCount, CreatedAt: time.Now(),
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel(), nil
}

func (self *transaction) ListWaitingAgentAlertCandidates(agentId string, limit int) ([]*models.AgentAlertCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	var found []agentAlertCandidateModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"alert_id\" = '' AND \"dropped_at\" IS NULL", agentId).
		Order("\"created_at\" ASC, \"id\" ASC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	candidates := make([]*models.AgentAlertCandidate, 0, len(found))
	for index := range found {
		candidates = append(candidates, found[index].toModel())
	}
	return candidates, nil
}

func (self *transaction) ListAgentAlertCandidatesByID(agentId string, candidateIds []string) ([]*models.AgentAlertCandidate, error) {
	candidates := []*models.AgentAlertCandidate{}
	if len(candidateIds) == 0 {
		return candidates, nil
	}
	var found []agentAlertCandidateModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"id\" IN ?", agentId, candidateIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	for index := range found {
		candidates = append(candidates, found[index].toModel())
	}
	return candidates, nil
}

func (self *transaction) LatestAgentBurstCandidate(agentId, mailboxId, burstKey string, since time.Time) (*models.AgentAlertCandidate, error) {
	var found []agentAlertCandidateModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"mailbox_id\" = ? AND \"burst_key\" = ? AND \"created_at\" >= ?", agentId, mailboxId, burstKey, since).
		Order("\"created_at\" DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListMailSubjectsFromDomain(mailboxId, fromDomain string, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 500
	}
	var subjects []string
	err := self.tx.Raw(`SELECT "mail"."subject" FROM "mailbox_item"
		JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
		JOIN "mail" ON "mail"."id" = "mailbox_item"."mail_id"
		WHERE "mailbox_folder"."mailbox_id" = ? AND "mailbox_folder"."kind" NOT IN ('junk', 'trash', 'drafts', 'sent')
		  AND "mailbox_item"."deleted" = false AND "mailbox_item"."added_at" >= ?
		  AND lower(split_part("mail"."from", '@', 2)) = ?
		ORDER BY "mailbox_item"."added_at" DESC LIMIT ?`, mailboxId, since, fromDomain, limit).Scan(&subjects).Error
	if subjects == nil {
		subjects = []string{}
	}
	return subjects, err
}

func (self *transaction) AdvanceAgentJob(agentId string, kind models.AgentJobKind, subjectId string, notBefore time.Time) error {
	return self.tx.Model(&agentJobModel{}).
		Where("\"agent_id\" = ? AND \"kind\" = ? AND \"subject_id\" = ? AND \"status\" = ? AND \"not_before\" > ?", agentId, string(kind), subjectId, string(models.AgentJobQueued), notBefore).
		Update("not_before", notBefore).Error
}

func (self *transaction) LockWaitingAgentAlertCandidates(agentId string, candidateIds []string) ([]*models.AgentAlertCandidate, error) {
	candidates := []*models.AgentAlertCandidate{}
	if len(candidateIds) == 0 {
		return candidates, nil
	}
	var found []agentAlertCandidateModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("\"agent_id\" = ? AND \"id\" IN ? AND \"alert_id\" = '' AND \"dropped_at\" IS NULL", agentId, candidateIds).
		Order("\"created_at\" ASC, \"id\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	for index := range found {
		candidates = append(candidates, found[index].toModel())
	}
	return candidates, nil
}

func (self *transaction) MarkAgentAlertCandidatesAlerted(candidateIds []string, alertId string) error {
	if len(candidateIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentAlertCandidateModel{}).Where("\"id\" IN ?", candidateIds).Update("alert_id", alertId).Error
}

func (self *transaction) DropAgentAlertCandidates(candidateIds []string, dropReason string, at time.Time) error {
	if len(candidateIds) == 0 {
		return nil
	}
	return self.tx.Model(&agentAlertCandidateModel{}).
		Where("\"id\" IN ? AND \"alert_id\" = '' AND \"dropped_at\" IS NULL", candidateIds).
		Updates(map[string]any{"dropped_at": at, "drop_reason": dropReason}).Error
}

type agentAlertModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	AgentID        string    `gorm:"column:agent_id"`
	SubjectKey     string    `gorm:"column:subject_key"`
	AlertText      string    `gorm:"column:alert_text"`
	IsUrgent       bool      `gorm:"column:is_urgent"`
	CandidateIDs   []byte    `gorm:"column:candidate_ids;type:jsonb"`
	ConversationID string    `gorm:"column:conversation_id"`
	MessageID      string    `gorm:"column:message_id"`
	SentAt         time.Time `gorm:"column:sent_at"`
}

func (agentAlertModel) TableName() string { return "agent_alert" }

func (self *agentAlertModel) toModel() (*models.AgentAlert, error) {
	alert := &models.AgentAlert{
		ID: self.ID, AgentID: self.AgentID, SubjectKey: self.SubjectKey, AlertText: self.AlertText, IsUrgent: self.IsUrgent,
		CandidateIDs: []string{}, ConversationID: self.ConversationID, MessageID: self.MessageID, SentAt: self.SentAt.In(time.Local),
	}
	if err := decodeJSON(self.CandidateIDs, &alert.CandidateIDs); err != nil {
		return nil, fmt.Errorf("db: cannot read the candidates of alert %q: %w", self.ID, err)
	}
	return alert, nil
}

func (self *transaction) CreateAgentAlert(alert *models.AgentAlert) (*models.AgentAlert, error) {
	if alert.AgentID == "" {
		return nil, fmt.Errorf("db: an alert needs an agent")
	}
	candidateIds := alert.CandidateIDs
	if candidateIds == nil {
		candidateIds = []string{}
	}
	encoded, err := json.Marshal(candidateIds)
	if err != nil {
		return nil, err
	}
	sentAt := alert.SentAt
	if sentAt.IsZero() {
		sentAt = time.Now()
	}
	model := &agentAlertModel{
		ID: newID(), AgentID: alert.AgentID, SubjectKey: alert.SubjectKey, AlertText: alert.AlertText, IsUrgent: alert.IsUrgent,
		CandidateIDs: encoded, ConversationID: alert.ConversationID, MessageID: alert.MessageID, SentAt: sentAt,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	return model.toModel()
}

func (self *transaction) ListAgentAlertsSince(agentId string, since time.Time) ([]*models.AgentAlert, error) {
	var found []agentAlertModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"sent_at\" >= ?", agentId, since).Order("\"sent_at\" DESC").Find(&found).Error; err != nil {
		return nil, err
	}
	alerts := make([]*models.AgentAlert, 0, len(found))
	for index := range found {
		alert, err := found[index].toModel()
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

func (self *transaction) GetAgentAlert(agentId, alertId string) (*models.AgentAlert, error) {
	var found []agentAlertModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"id\" = ?", agentId, alertId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel()
}

func (self *transaction) ListRecentAgentAlerts(agentId string, limit int) ([]*models.AgentAlert, error) {
	if limit <= 0 {
		limit = 50
	}
	var found []agentAlertModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"sent_at\" DESC, \"id\" DESC").Limit(limit).Find(&found).Error; err != nil {
		return nil, err
	}
	alerts := make([]*models.AgentAlert, 0, len(found))
	for index := range found {
		alert, err := found[index].toModel()
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

type agentAlertMuteModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	AgentID    string    `gorm:"column:agent_id"`
	MuteScope  string    `gorm:"column:mute_scope"`
	MuteTarget string    `gorm:"column:mute_target"`
	AlertID    string    `gorm:"column:alert_id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (agentAlertMuteModel) TableName() string { return "agent_alert_mute" }

func (self *agentAlertMuteModel) toModel() *models.AgentAlertMute {
	return &models.AgentAlertMute{
		ID: self.ID, AgentID: self.AgentID, MuteScope: models.AlertMuteScope(self.MuteScope), MuteTarget: self.MuteTarget,
		AlertID: self.AlertID, CreatedAt: self.CreatedAt.In(time.Local),
	}
}

func (self *transaction) CreateAgentAlertMute(mute *models.AgentAlertMute) (*models.AgentAlertMute, error) {
	if mute.AgentID == "" || !mute.MuteScope.IsValid() || mute.MuteTarget == "" {
		return nil, fmt.Errorf("db: a mute needs an agent, a scope and a target")
	}
	model := &agentAlertMuteModel{
		ID: newID(), AgentID: mute.AgentID, MuteScope: string(mute.MuteScope), MuteTarget: mute.MuteTarget,
		AlertID: mute.AlertID, CreatedAt: time.Now(),
	}
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model).Error; err != nil {
		return nil, err
	}
	var found []agentAlertMuteModel
	if err := self.tx.Where("\"agent_id\" = ? AND \"mute_scope\" = ? AND \"mute_target\" = ?", model.AgentID, model.MuteScope, model.MuteTarget).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("db: the mute was not kept")
	}
	return found[0].toModel(), nil
}

func (self *transaction) ListAgentAlertMutes(agentId string) ([]*models.AgentAlertMute, error) {
	var found []agentAlertMuteModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"created_at\" DESC, \"id\" DESC").Find(&found).Error; err != nil {
		return nil, err
	}
	mutes := make([]*models.AgentAlertMute, 0, len(found))
	for index := range found {
		mutes = append(mutes, found[index].toModel())
	}
	return mutes, nil
}

func (self *transaction) DeleteAgentAlertMute(agentId, muteId string) (bool, error) {
	result := self.tx.Where("\"agent_id\" = ? AND \"id\" = ?", agentId, muteId).Delete(&agentAlertMuteModel{})
	return result.RowsAffected > 0, result.Error
}
