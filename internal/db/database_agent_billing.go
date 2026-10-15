package db

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// AgentBillingOperation keeps what the daily check of a provider's prices
// against its bill last did.
type AgentBillingOperation interface {
	// PutAgentBillingCheck writes a provider's last check, replacing the
	// one before it.
	PutAgentBillingCheck(check *models.AgentBillingCheck) error

	// GetAgentBillingCheck is a provider's last check, or nil when it has
	// not been checked.
	GetAgentBillingCheck(providerName string) (*models.AgentBillingCheck, error)

	// RecordAgentPriceChange writes an audit event for a model's price
	// that the check changed. The configuration itself is saved through
	// the configuration store, not here; this is only the record of it.
	RecordAgentPriceChange(providerName, model string, before, after any) error

	// PutAgentBilledDays writes what a provider billed per model and day,
	// replacing a row already there for the same provider, day and model.
	// The days' ProviderName is ignored; providerName says whose they are.
	PutAgentBilledDays(providerName string, days []models.AgentBilledDay) error

	// DeleteAgentBilledDaysBefore removes billed days before the UTC day
	// the time falls on.
	DeleteAgentBilledDaysBefore(day time.Time) error

	// ListAgentBilledDays is the billed days from the UTC day since falls
	// on up to, not including, the UTC day until falls on; a zero until
	// has no end. Oldest first.
	ListAgentBilledDays(since, until time.Time) ([]*models.AgentBilledDay, error)
}

type agentBillingCheckModel struct {
	ProviderName string    `gorm:"column:provider_name;primaryKey"`
	CheckedAt    time.Time `gorm:"column:checked_at"`
	ErrorMessage string    `gorm:"column:error_message"`
	ComparedJSON []byte    `gorm:"column:compared_json"`
	ChangedJSON  []byte    `gorm:"column:changed_json"`
}

func (agentBillingCheckModel) TableName() string { return "agent_billing_check" }

func (self *transaction) PutAgentBillingCheck(check *models.AgentBillingCheck) error {
	if check == nil || check.ProviderName == "" {
		return fmt.Errorf("db: a billing check needs a provider")
	}
	compared, changed := check.Compared, check.Changed
	if compared == nil {
		compared = []models.AgentBilledPrice{}
	}
	if changed == nil {
		changed = []models.AgentPriceChange{}
	}
	comparedJSON, err := json.Marshal(compared)
	if err != nil {
		return err
	}
	changedJSON, err := json.Marshal(changed)
	if err != nil {
		return err
	}
	return self.tx.Exec(
		`INSERT INTO "agent_billing_check" ("provider_name", "checked_at", "error_message", "compared_json", "changed_json")
		VALUES (?, ?, ?, ?::jsonb, ?::jsonb)
		ON CONFLICT ("provider_name") DO UPDATE SET "checked_at" = EXCLUDED."checked_at",
		"error_message" = EXCLUDED."error_message", "compared_json" = EXCLUDED."compared_json", "changed_json" = EXCLUDED."changed_json"`,
		check.ProviderName, check.CheckedAt, check.ErrorMessage, string(comparedJSON), string(changedJSON)).Error
}

func (self *transaction) GetAgentBillingCheck(providerName string) (*models.AgentBillingCheck, error) {
	var row agentBillingCheckModel
	result := self.tx.Where("\"provider_name\" = ?", providerName).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	check := &models.AgentBillingCheck{
		ProviderName: row.ProviderName,
		CheckedAt:    row.CheckedAt,
		ErrorMessage: row.ErrorMessage,
		Compared:     []models.AgentBilledPrice{},
		Changed:      []models.AgentPriceChange{},
	}
	if err := json.Unmarshal(row.ComparedJSON, &check.Compared); err != nil {
		return nil, fmt.Errorf("db: cannot read what the check of %s compared: %w", providerName, err)
	}
	if err := json.Unmarshal(row.ChangedJSON, &check.Changed); err != nil {
		return nil, fmt.Errorf("db: cannot read what the check of %s changed: %w", providerName, err)
	}
	return check, nil
}

func (self *transaction) RecordAgentPriceChange(providerName, model string, before, after any) error {
	resourceId := fmt.Sprintf("agent.providers/%s/%s", providerName, model)
	return self.applyMutation(models.AuditResourceConfiguration, resourceId, models.AuditActionUpdate, before, after, func(*gorm.DB) error {
		return nil
	})
}

type agentBilledDayModel struct {
	ProviderName string    `gorm:"column:provider_name;primaryKey"`
	BilledOn     time.Time `gorm:"column:billed_on;primaryKey"`
	Model        string    `gorm:"column:model;primaryKey"`
	BilledAmount float64   `gorm:"column:billed_amount"`
	CurrencyCode string    `gorm:"column:currency_code"`
}

func (agentBilledDayModel) TableName() string { return "agent_billed_day" }

func (self *transaction) PutAgentBilledDays(providerName string, days []models.AgentBilledDay) error {
	if providerName == "" {
		return fmt.Errorf("db: billed days need a provider")
	}
	for _, day := range days {
		if err := self.tx.Exec(
			`INSERT INTO "agent_billed_day" ("provider_name", "billed_on", "model", "billed_amount", "currency_code")
			VALUES (?, (? AT TIME ZONE 'UTC')::date, ?, ?, ?)
			ON CONFLICT ("provider_name", "billed_on", "model") DO UPDATE SET
			"billed_amount" = EXCLUDED."billed_amount", "currency_code" = EXCLUDED."currency_code"`,
			providerName, day.BilledOn.UTC(), day.Model, day.BilledAmount, day.CurrencyCode).Error; err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) DeleteAgentBilledDaysBefore(day time.Time) error {
	return self.tx.Exec(`DELETE FROM "agent_billed_day" WHERE "billed_on" < (? AT TIME ZONE 'UTC')::date`, day.UTC()).Error
}

func (self *transaction) ListAgentBilledDays(since, until time.Time) ([]*models.AgentBilledDay, error) {
	query := self.tx.Model(&agentBilledDayModel{}).Where(`"billed_on" >= (? AT TIME ZONE 'UTC')::date`, since.UTC())
	if !until.IsZero() {
		query = query.Where(`"billed_on" < (? AT TIME ZONE 'UTC')::date`, until.UTC())
	}
	var rows []agentBilledDayModel
	if err := query.Order(`"billed_on", "provider_name", "model"`).Find(&rows).Error; err != nil {
		return nil, err
	}
	days := make([]*models.AgentBilledDay, 0, len(rows))
	for _, row := range rows {
		days = append(days, &models.AgentBilledDay{
			ProviderName: row.ProviderName,
			BilledOn:     time.Date(row.BilledOn.Year(), row.BilledOn.Month(), row.BilledOn.Day(), 0, 0, 0, 0, time.UTC),
			Model:        row.Model,
			BilledAmount: row.BilledAmount,
			CurrencyCode: row.CurrencyCode,
		})
	}
	return days, nil
}
