package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// WatchedItemOperation keeps which items a skill's watch has looked at, in
// which version, so that the next look, which overlaps this one, judges
// none of them twice, and an item that changed is judged again.
type WatchedItemOperation interface {
	// ListAgentWatchedItemsLooked says which of the items given, each by
	// its id and version, the watch has already looked at; the answer is
	// keyed by WatchedItemKey.
	ListAgentWatchedItemsLooked(agentId, skillName, watchName string, watchedItemIds []string) (map[string]bool, error)

	// AddAgentWatchedItem records an item looked at; one already recorded
	// in that version is left as it is.
	AddAgentWatchedItem(watched *models.AgentWatchedItem) error

	// LatestAgentWatchedItemAt is when the newest item the watch looked at
	// happened, or nil when it has looked at none.
	LatestAgentWatchedItemAt(agentId, skillName, watchName string) (*time.Time, error)

	// DeleteAgentWatchedItemsBefore forgets the items that happened before
	// the moment given, which no look reaches back to any more.
	DeleteAgentWatchedItemsBefore(agentId, skillName, watchName string, before time.Time) error
}

// WatchedItemKey is how ListAgentWatchedItemsLooked names an item in one
// version.
func WatchedItemKey(watchedItemId, watchedItemVersion string) string {
	return watchedItemId + "\n" + watchedItemVersion
}

type agentWatchedItemModel struct {
	AgentID            string    `gorm:"column:agent_id;primaryKey"`
	SkillName          string    `gorm:"column:skill_name;primaryKey"`
	WatchName          string    `gorm:"column:watch_name;primaryKey"`
	WatchedItemID      string    `gorm:"column:watched_item_id;primaryKey"`
	WatchedItemVersion string    `gorm:"column:watched_item_version;primaryKey"`
	WatchedItemAt      time.Time `gorm:"column:watched_item_at"`
	AlertSignal        string    `gorm:"column:alert_signal"`
	LookedAt           time.Time `gorm:"column:looked_at"`
}

func (agentWatchedItemModel) TableName() string { return "agent_watched_item" }

func (self *transaction) ListAgentWatchedItemsLooked(agentId, skillName, watchName string, watchedItemIds []string) (map[string]bool, error) {
	looked := map[string]bool{}
	if len(watchedItemIds) == 0 {
		return looked, nil
	}
	var found []agentWatchedItemModel
	if err := self.tx.Select("watched_item_id", "watched_item_version").
		Where(`"agent_id" = ? AND "skill_name" = ? AND "watch_name" = ? AND "watched_item_id" IN ?`, agentId, skillName, watchName, watchedItemIds).
		Find(&found).Error; err != nil {
		return nil, err
	}
	for _, model := range found {
		looked[WatchedItemKey(model.WatchedItemID, model.WatchedItemVersion)] = true
	}
	return looked, nil
}

func (self *transaction) AddAgentWatchedItem(watched *models.AgentWatchedItem) error {
	if watched.AgentID == "" || watched.SkillName == "" || watched.WatchName == "" || watched.WatchedItemID == "" {
		return fmt.Errorf("db: a watched item needs an agent, a skill, a watch and its id")
	}
	alertSignal := watched.AlertSignal
	if alertSignal == "" {
		alertSignal = models.AlertSignalNone
	}
	lookedAt := watched.LookedAt
	if lookedAt.IsZero() {
		lookedAt = time.Now()
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&agentWatchedItemModel{
		AgentID: watched.AgentID, SkillName: watched.SkillName, WatchName: watched.WatchName,
		WatchedItemID: watched.WatchedItemID, WatchedItemVersion: watched.WatchedItemVersion,
		WatchedItemAt: watched.WatchedItemAt, AlertSignal: alertSignal, LookedAt: lookedAt,
	}).Error
}

func (self *transaction) LatestAgentWatchedItemAt(agentId, skillName, watchName string) (*time.Time, error) {
	var found []agentWatchedItemModel
	if err := self.tx.Where(`"agent_id" = ? AND "skill_name" = ? AND "watch_name" = ?`, agentId, skillName, watchName).
		Order(`"watched_item_at" DESC`).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	latest := found[0].WatchedItemAt.In(time.Local)
	return &latest, nil
}

func (self *transaction) DeleteAgentWatchedItemsBefore(agentId, skillName, watchName string, before time.Time) error {
	return self.tx.Where(`"agent_id" = ? AND "skill_name" = ? AND "watch_name" = ? AND "watched_item_at" < ?`, agentId, skillName, watchName, before).
		Delete(&agentWatchedItemModel{}).Error
}
