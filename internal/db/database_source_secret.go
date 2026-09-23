package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type agentSourceSecretModel struct {
	SourceID   string    `gorm:"column:source_id;primaryKey"`
	Key        string    `gorm:"column:key;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	Value      string    `gorm:"column:value"`
}

func (agentSourceSecretModel) TableName() string { return "agent_source_secret" }

// ListAgentSourceSecrets is every value filled in for one source.
func (self *transaction) ListAgentSourceSecrets(sourceId string) ([]*models.AgentSourceSecret, error) {
	var found []agentSourceSecretModel
	if err := self.tx.Where("\"source_id\" = ?", sourceId).Order("\"key\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	secrets := make([]*models.AgentSourceSecret, 0, len(found))
	for _, row := range found {
		secrets = append(secrets, &models.AgentSourceSecret{
			SourceID: row.SourceID, Key: row.Key, CreatedAt: row.CreatedAt, ModifiedAt: row.ModifiedAt, Value: row.Value,
		})
	}
	return secrets, nil
}

// PutAgentSourceSecret writes one value, replacing what was there. The
// value comes sealed; an audit row names the key, never the value.
func (self *transaction) PutAgentSourceSecret(agentId string, secret *models.AgentSourceSecret) error {
	if secret == nil || secret.SourceID == "" || strings.TrimSpace(secret.Key) == "" {
		return fmt.Errorf("db: a source secret needs a source and a key")
	}
	now := time.Now()
	row := &agentSourceSecretModel{SourceID: secret.SourceID, Key: strings.TrimSpace(secret.Key), CreatedAt: now, ModifiedAt: now, Value: secret.Value}
	return self.applyMutation(models.AuditResourceAgent, agentId, models.AuditActionUpdate,
		nil, &noteAboutSourceSecret{SourceID: row.SourceID, Key: row.Key, Set: true},
		func(tx *gorm.DB) error {
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"modified_at", "value"}),
			}).Create(row).Error
		})
}

// noteAboutSourceSecret is what an audit row says about one of these.
type noteAboutSourceSecret struct {
	SourceID string `json:"sourceId"`
	Key      string `json:"key,omitempty"`
	Set      bool   `json:"set"`
}

// DeleteAgentSourceSecret forgets one value; an empty key forgets every
// value of the source.
func (self *transaction) DeleteAgentSourceSecret(agentId, sourceId, key string) error {
	return self.applyMutation(models.AuditResourceAgent, agentId, models.AuditActionUpdate,
		nil, &noteAboutSourceSecret{SourceID: sourceId, Key: strings.TrimSpace(key), Set: false},
		func(tx *gorm.DB) error {
			query := tx.Where("\"source_id\" = ?", sourceId)
			if strings.TrimSpace(key) != "" {
				query = query.Where("\"key\" = ?", strings.TrimSpace(key))
			}
			return query.Delete(&agentSourceSecretModel{}).Error
		})
}
