package db

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type agentSkillModel struct {
	Name        string    `gorm:"column:name;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	Version     string    `gorm:"column:version"`
	Publisher   string    `gorm:"column:publisher"`
	URL         string    `gorm:"column:url"`
	SHA256      string    `gorm:"column:sha256"`
	Description string    `gorm:"column:description"`
	Content     string    `gorm:"column:content"`
	Enabled     bool      `gorm:"column:enabled"`
}

func (agentSkillModel) TableName() string { return "agent_skill" }

func skillToModel(skill *models.AgentSkill) *agentSkillModel {
	return &agentSkillModel{
		Name: skill.Name, CreatedAt: skill.CreatedAt, ModifiedAt: skill.ModifiedAt,
		Version: skill.Version, Publisher: skill.Publisher, URL: skill.URL,
		SHA256: skill.SHA256, Description: skill.Description, Content: skill.Content,
		Enabled: skill.Enabled,
	}
}

func (self *agentSkillModel) toModel() *models.AgentSkill {
	return &models.AgentSkill{
		Name: self.Name, CreatedAt: self.CreatedAt, ModifiedAt: self.ModifiedAt,
		Version: self.Version, Publisher: self.Publisher, URL: self.URL,
		SHA256: self.SHA256, Description: self.Description, Content: self.Content,
		Enabled: self.Enabled,
	}
}

func (self *transaction) ListAgentSkills() ([]*models.AgentSkill, error) {
	var found []agentSkillModel
	if err := self.tx.Order("\"name\" ASC").Find(&found).Error; err != nil {
		return nil, err
	}
	skills := make([]*models.AgentSkill, 0, len(found))
	for index := range found {
		skills = append(skills, found[index].toModel())
	}
	return skills, nil
}

func (self *transaction) GetAgentSkill(name string) (*models.AgentSkill, error) {
	var found []agentSkillModel
	if err := self.tx.Where("\"name\" = ?", strings.ToLower(strings.TrimSpace(name))).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

// PutAgentSkill installs a skill or replaces the one of that name, which
// is what an update is: the name is the key, and a skill is only ever
// present once.
func (self *transaction) PutAgentSkill(skill *models.AgentSkill) (*models.AgentSkill, error) {
	if skill == nil || strings.TrimSpace(skill.Name) == "" {
		return nil, fmt.Errorf("db: a skill needs a name")
	}
	if strings.TrimSpace(skill.Content) == "" {
		return nil, fmt.Errorf("db: a skill needs its content")
	}
	now := time.Now()
	skill.Name = strings.ToLower(strings.TrimSpace(skill.Name))
	if skill.CreatedAt.IsZero() {
		skill.CreatedAt = now
	}
	skill.ModifiedAt = now
	row := skillToModel(skill)
	if err := self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"modified_at", "version", "publisher", "url", "sha256", "description", "content", "enabled",
		}),
	}).Create(row).Error; err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) DeleteAgentSkill(name string) error {
	return self.tx.Where("\"name\" = ?", strings.ToLower(strings.TrimSpace(name))).Delete(&agentSkillModel{}).Error
}
