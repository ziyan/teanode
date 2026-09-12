package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type agentSkillSecretModel struct {
	AgentID    string    `gorm:"column:agent_id;primaryKey"`
	Skill      string    `gorm:"column:skill;primaryKey"`
	Key        string    `gorm:"column:key;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	Value      string    `gorm:"column:value"`
}

func (agentSkillSecretModel) TableName() string { return "agent_skill_secret" }

func (self *agentSkillSecretModel) toModel() *models.AgentSkillSecret {
	return &models.AgentSkillSecret{
		AgentID: self.AgentID, Skill: self.Skill, Key: self.Key,
		CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt, Value: self.Value,
	}
}

// ListAgentSkillSecrets is every value this person has filled in, for
// every skill.
func (self *transaction) ListAgentSkillSecrets(agentId string) ([]*models.AgentSkillSecret, error) {
	var found []agentSkillSecretModel
	if err := self.tx.Where("\"agent_id\" = ?", agentId).Order("\"skill\" ASC, \"key\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	secrets := make([]*models.AgentSkillSecret, 0, len(found))
	for index := range found {
		secrets = append(secrets, found[index].toModel())
	}
	return secrets, nil
}

// PutAgentSkillSecret writes one person's value, replacing what was there.
func (self *transaction) PutAgentSkillSecret(secret *models.AgentSkillSecret) error {
	if secret == nil || secret.AgentID == "" || strings.TrimSpace(secret.Skill) == "" || strings.TrimSpace(secret.Key) == "" {
		return fmt.Errorf("db: a skill secret needs an agent, a skill and a key")
	}
	now := time.Now()
	row := &agentSkillSecretModel{
		AgentID: secret.AgentID, Skill: strings.ToLower(strings.TrimSpace(secret.Skill)),
		Key: strings.TrimSpace(secret.Key), CreatedAt: now, ModifiedAt: now, Value: secret.Value,
	}
	return self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "skill"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"modified_at", "value"}),
	}).Create(row).Error
}

// DeleteAgentSkillSecret forgets one value. An empty key forgets every
// value this person filled in for that skill.
func (self *transaction) DeleteAgentSkillSecret(agentId, skill, key string) error {
	query := self.tx.Where("\"agent_id\" = ? AND \"skill\" = ?", agentId, strings.ToLower(strings.TrimSpace(skill)))
	if trimmed := strings.TrimSpace(key); trimmed != "" {
		query = query.Where("\"key\" = ?", trimmed)
	}
	return query.Delete(&agentSkillSecretModel{}).Error
}
