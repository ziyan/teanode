package db

import (
	"fmt"
	"time"

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

	// LatestAgentBurstCandidate is the newest burst candidate of a
	// mailbox with this key made since the moment given, or nil.
	LatestAgentBurstCandidate(agentId, mailboxId, burstKey string, since time.Time) (*models.AgentAlertCandidate, error)

	// ListMailSubjectsFromDomain is the subjects of the messages that
	// arrived in a mailbox since the moment given from an address at the
	// domain, leaving out Junk, Trash, Drafts and Sent: what a burst is
	// counted over. One query over the mailbox's items by the time they
	// were added, which is indexed.
	ListMailSubjectsFromDomain(mailboxId, fromDomain string, since time.Time, limit int) ([]string, error)

	// AdvanceAgentJob brings a queued job of this agent, kind and subject
	// forward to the moment given, when it was waiting for later.
	AdvanceAgentJob(agentId string, kind models.AgentJobKind, subjectId string, notBefore time.Time) error
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
