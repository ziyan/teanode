package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
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
	// Audited by name only. The sealed value is never written into an
	// audit row: the whole point of sealing it is that it exists in one
	// place, and an audit trail is read by more people than that.
	return self.applyMutation(models.AuditResourceAgent, secret.AgentID, models.AuditActionUpdate,
		nil, &noteAboutSkillSecret{Skill: row.Skill, Key: row.Key, Set: true},
		func(tx *gorm.DB) error {
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "agent_id"}, {Name: "skill"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"modified_at", "value"}),
			}).Create(row).Error
		})
}

// noteAboutSkillSecret is what an audit row says about one of these: that
// a person set or forgot a named value, never the value itself.
type noteAboutSkillSecret struct {
	Skill string `json:"skill"`
	Key   string `json:"key,omitempty"`
	Set   bool   `json:"set"`
}

// DeleteAgentSkillSecret forgets one value. An empty key forgets every
// value this person filled in for that skill.
func (self *transaction) DeleteAgentSkillSecret(agentId, skill, key string) error {
	lowered := strings.ToLower(strings.TrimSpace(skill))
	trimmed := strings.TrimSpace(key)
	return self.applyMutation(models.AuditResourceAgent, agentId, models.AuditActionUpdate,
		&noteAboutSkillSecret{Skill: lowered, Key: trimmed, Set: true}, nil,
		func(tx *gorm.DB) error {
			query := tx.Where("\"agent_id\" = ? AND \"skill\" = ?", agentId, lowered)
			if trimmed != "" {
				query = query.Where("\"key\" = ?", trimmed)
			}
			return query.Delete(&agentSkillSecretModel{}).Error
		})
}

// SweepAgentSkillSecrets forgets what everybody filled in for a skill,
// for when the skill itself is taken off the server. Sealed values that
// outlive their skill would otherwise come back the day somebody
// installs it again, under a version that may ask for a different key.
func (self *transaction) SweepAgentSkillSecrets(skill string) error {
	return self.tx.Where("\"skill\" = ?", strings.ToLower(strings.TrimSpace(skill))).
		Delete(&agentSkillSecretModel{}).Error
}

// SweepAgentSkillSecretsExcept forgets what everybody filled in for a
// skill apart from the keys named, for when a new version of the skill no
// longer asks for one of them.
func (self *transaction) SweepAgentSkillSecretsExcept(skill string, keep map[string]bool) error {
	query := self.tx.Where("\"skill\" = ?", strings.ToLower(strings.TrimSpace(skill)))
	if len(keep) > 0 {
		keys := make([]string, 0, len(keep))
		for key := range keep {
			keys = append(keys, key)
		}
		query = query.Where("\"key\" NOT IN ?", keys)
	}
	return query.Delete(&agentSkillSecretModel{}).Error
}
