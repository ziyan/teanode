package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// WatchedMailOperation keeps which messages the watch has looked at in a
// mailbox this server does not host, so that the next look, which
// overlaps this one, sorts none of them twice.
type WatchedMailOperation interface {
	// ListAgentWatchedMailLooked says which of the message ids given the
	// watch has already looked at through this skill.
	ListAgentWatchedMailLooked(agentId, skillName string, watchedMessageIds []string) (map[string]bool, error)

	// AddAgentWatchedMail records a message looked at; one already
	// recorded is left as it is.
	AddAgentWatchedMail(watched *models.AgentWatchedMail) error

	// LatestAgentWatchedMailAt is the date of the newest message looked at
	// through this skill, or nil when there is none.
	LatestAgentWatchedMailAt(agentId, skillName string) (*time.Time, error)

	// DeleteAgentWatchedMailBefore forgets the messages dated before the
	// moment given, which no look reaches back to any more.
	DeleteAgentWatchedMailBefore(agentId, skillName string, before time.Time) error
}

type agentWatchedMailModel struct {
	AgentID          string    `gorm:"column:agent_id;primaryKey"`
	SkillName        string    `gorm:"column:skill_name;primaryKey"`
	WatchedMessageID string    `gorm:"column:watched_message_id;primaryKey"`
	WatchedMessageAt time.Time `gorm:"column:watched_message_at"`
	AlertSignal      string    `gorm:"column:alert_signal"`
	LookedAt         time.Time `gorm:"column:looked_at"`
}

func (agentWatchedMailModel) TableName() string { return "agent_watched_mail" }

func (self *transaction) ListAgentWatchedMailLooked(agentId, skillName string, watchedMessageIds []string) (map[string]bool, error) {
	looked := map[string]bool{}
	if len(watchedMessageIds) == 0 {
		return looked, nil
	}
	var found []string
	if err := self.tx.Model(&agentWatchedMailModel{}).
		Where(`"agent_id" = ? AND "skill_name" = ? AND "watched_message_id" IN ?`, agentId, skillName, watchedMessageIds).
		Pluck("watched_message_id", &found).Error; err != nil {
		return nil, err
	}
	for _, watchedMessageId := range found {
		looked[watchedMessageId] = true
	}
	return looked, nil
}

func (self *transaction) AddAgentWatchedMail(watched *models.AgentWatchedMail) error {
	if watched.AgentID == "" || watched.SkillName == "" || watched.WatchedMessageID == "" {
		return fmt.Errorf("db: a watched message needs an agent, a skill and its id")
	}
	alertSignal := watched.AlertSignal
	if alertSignal == "" {
		alertSignal = models.AlertSignalNone
	}
	lookedAt := watched.LookedAt
	if lookedAt.IsZero() {
		lookedAt = time.Now()
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&agentWatchedMailModel{
		AgentID: watched.AgentID, SkillName: watched.SkillName, WatchedMessageID: watched.WatchedMessageID,
		WatchedMessageAt: watched.WatchedMessageAt, AlertSignal: alertSignal, LookedAt: lookedAt,
	}).Error
}

func (self *transaction) LatestAgentWatchedMailAt(agentId, skillName string) (*time.Time, error) {
	var found []agentWatchedMailModel
	if err := self.tx.Where(`"agent_id" = ? AND "skill_name" = ?`, agentId, skillName).
		Order(`"watched_message_at" DESC`).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	latest := found[0].WatchedMessageAt.In(time.Local)
	return &latest, nil
}

func (self *transaction) DeleteAgentWatchedMailBefore(agentId, skillName string, before time.Time) error {
	return self.tx.Where(`"agent_id" = ? AND "skill_name" = ? AND "watched_message_at" < ?`, agentId, skillName, before).
		Delete(&agentWatchedMailModel{}).Error
}
