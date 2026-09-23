package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type agentSourceTypeModel struct {
	Name        string    `gorm:"column:name;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Version     string    `gorm:"column:version"`
	Publisher   string    `gorm:"column:publisher"`
	URL         string    `gorm:"column:url"`
	SHA256      string    `gorm:"column:sha256"`
	Description string    `gorm:"column:description"`
	Content     string    `gorm:"column:content"`
	IsLocal     bool      `gorm:"column:is_local"`
}

func (agentSourceTypeModel) TableName() string { return "agent_source_type" }

func (self *agentSourceTypeModel) toModel() *models.AgentSourceType {
	return &models.AgentSourceType{
		Name: self.Name, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Version: self.Version, Publisher: self.Publisher, URL: self.URL,
		SHA256: self.SHA256, Description: self.Description, Content: self.Content,
		IsLocal: self.IsLocal,
	}
}

func (self *transaction) ListAgentSourceTypes() ([]*models.AgentSourceType, error) {
	var found []agentSourceTypeModel
	if err := self.tx.Order("\"name\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	sourceTypes := make([]*models.AgentSourceType, 0, len(found))
	for index := range found {
		sourceTypes = append(sourceTypes, found[index].toModel())
	}
	return sourceTypes, nil
}

func (self *transaction) GetAgentSourceType(name string) (*models.AgentSourceType, error) {
	var found []agentSourceTypeModel
	if err := self.tx.Where("\"name\" = ?", strings.ToLower(strings.TrimSpace(name))).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// PutAgentSourceType installs a type or replaces the one of that name,
// which is what an update is.
func (self *transaction) PutAgentSourceType(sourceType *models.AgentSourceType) (*models.AgentSourceType, error) {
	if sourceType == nil || strings.TrimSpace(sourceType.Name) == "" {
		return nil, fmt.Errorf("db: a source type needs a name")
	}
	if strings.TrimSpace(sourceType.Content) == "" {
		return nil, fmt.Errorf("db: a source type needs its content")
	}
	now := time.Now()
	row := &agentSourceTypeModel{
		Name: strings.ToLower(strings.TrimSpace(sourceType.Name)), CreatedAt: sourceType.CreatedAt, ModifiedAt: now,
		Version: sourceType.Version, Publisher: sourceType.Publisher, URL: sourceType.URL,
		SHA256: sourceType.SHA256, Description: sourceType.Description, Content: sourceType.Content,
		IsLocal: sourceType.IsLocal,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"modified_at", "version", "publisher", "url", "sha256", "description", "content", "is_local",
		}),
	}).Create(row).Error; err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) DeleteAgentSourceType(name string) error {
	return self.tx.Where("\"name\" = ?", strings.ToLower(strings.TrimSpace(name))).Delete(&agentSourceTypeModel{}).Error
}

// CountAgentSourcesOfType is how many sources, of anybody's, are of a type:
// a type in use is not taken away from under them.
func (self *transaction) CountAgentSourcesOfType(name string) (int64, error) {
	var count int64
	err := self.tx.Model(&agentSourceModel{}).
		Where("\"specification\"->>'type' = ?", strings.ToLower(strings.TrimSpace(name))).
		Count(&count).Error
	return count, err
}
