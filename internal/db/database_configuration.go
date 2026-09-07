package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The configuration table holds the server's settings: one row per section,
// as a YAML document, because the sections are read together and never
// queried by their parts, and a column per field would be a migration for
// every new option.
//
// Only settings live here. Domains, aliases, credentials and users are rows
// in tables of their own.

// ErrConfigurationChanged is returned when a change was made against a
// configuration that somebody else has since replaced. The caller reloads and
// tries again; it is not a failure, it is two people editing at once.
var ErrConfigurationChanged = fmt.Errorf("db: the configuration changed while this change was being made")

// ConfigurationOperation reads and writes the settings.
type ConfigurationOperation interface {
	// ConfigurationVersion is what the stored configuration is at, for an
	// instance checking whether its copy is stale.
	ConfigurationVersion() (int64, error)

	// LoadConfiguration reads every section, and the version they were read
	// at.
	LoadConfiguration() (*ConfigurationRows, error)

	// SaveConfiguration replaces the sections, refusing when the caller's
	// copy is stale, and returns the new version.
	SaveConfiguration(rows *ConfigurationRows) (int64, error)
}

// ConfigurationRows is the settings as stored.
type ConfigurationRows struct {
	// Version is what the configuration was when it was read. Passing it
	// back to SaveConfiguration is how a write refuses to overwrite somebody
	// else's.
	Version int64

	// Settings holds each section as a YAML document, keyed by section name.
	//
	// YAML rather than JSON, and not because of taste: a secret is raw
	// bytes, and encoding/json replaces a byte that is not valid UTF-8 with
	// the replacement character. A server secret that came back changed
	// would invalidate every SMTP password on the server, silently. YAML
	// writes such a string as !!binary.
	Settings map[string]string
}

type configurationModel struct {
	Key        string    `gorm:"column:key;primaryKey"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
	Value      string    `gorm:"column:value;type:text"`
}

func (configurationModel) TableName() string { return "configuration" }

type configurationVersionModel struct {
	ID         int       `gorm:"column:id;primaryKey"`
	Version    int64     `gorm:"column:version"`
	ModifiedAt time.Time `gorm:"column:modified_at"`
}

func (configurationVersionModel) TableName() string { return "configuration_version" }

func (self *database) ConfigurationVersion() (int64, error) {
	var version configurationVersionModel
	if err := self.db.First(&version, 1).Error; err != nil {
		return 0, err
	}
	return version.Version, nil
}

func (self *database) LoadConfiguration() (*ConfigurationRows, error) {
	rows := &ConfigurationRows{Settings: map[string]string{}}
	if err := self.db.Transaction(func(tx *gorm.DB) error {
		var version configurationVersionModel
		if err := tx.First(&version, 1).Error; err != nil {
			return err
		}
		rows.Version = version.Version
		var settings []configurationModel
		if err := tx.Find(&settings).Error; err != nil {
			return err
		}
		for _, model := range settings {
			rows.Settings[model.Key] = model.Value
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return rows, nil
}

// SaveConfiguration replaces the stored settings with these.
//
// Inside a transaction that holds the version row: two instances changing
// settings at the same moment take turns rather than overwriting each other,
// and a caller whose copy went stale in between is told so instead of
// silently erasing the other change.
func (self *database) SaveConfiguration(rows *ConfigurationRows) (int64, error) {
	var saved int64
	if err := self.db.Transaction(func(tx *gorm.DB) error {
		var version configurationVersionModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&version, 1).Error; err != nil {
			return err
		}
		// Checked even when the caller says zero, because zero is a real
		// version: it is what a database that has never been configured
		// holds, and it is what the first write is made against.
		if rows.Version != version.Version {
			return fmt.Errorf("%w: the configuration changed to version %d while this change was being made",
				ErrConfigurationChanged, version.Version)
		}
		now := time.Now()
		var existing []configurationModel
		if err := tx.Find(&existing).Error; err != nil {
			return err
		}
		known := make(map[string]bool, len(existing))
		for _, model := range existing {
			known[model.Key] = true
			value, keep := rows.Settings[model.Key]
			if !keep {
				if err := tx.Where("\"key\" = ?", model.Key).Delete(&configurationModel{}).Error; err != nil {
					return err
				}
				continue
			}
			if value == model.Value {
				continue
			}
			if err := tx.Model(&configurationModel{}).Where("\"key\" = ?", model.Key).Updates(map[string]any{"value": value, "modified_at": now}).Error; err != nil {
				return err
			}
		}
		for key, value := range rows.Settings {
			if known[key] {
				continue
			}
			if err := tx.Create(&configurationModel{Key: key, CreatedAt: now, ModifiedAt: now, Value: value}).Error; err != nil {
				return err
			}
		}
		version.Version++
		version.ModifiedAt = now
		if err := tx.Save(&version).Error; err != nil {
			return err
		}
		saved = version.Version
		return nil
	}); err != nil {
		return 0, err
	}
	return saved, nil
}
