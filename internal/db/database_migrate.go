package db

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/db/migrations"
)

// Database model for migration records.
type migrationModel struct {
	ID string `gorm:"primary_key:true;size:256"`

	MigratedAt time.Time
	ReverseSQL string `gorm:"type:text"`
}

func (self *migrationModel) TableName() string {
	return "migration"
}

// UnknownMigrations names the migrations this database has applied that this
// binary does not carry. In other words: what Migrate would revert.
//
// Asked separately, and before Migrate, by the one caller that needs to know
// the answer before acting on it. Reverting is how a deliberate downgrade
// works here — see docs/coding/database-migrations.md — and it is exactly
// wrong when the downgrade is not deliberate, which is what a start that has
// just refused a staged upgrade is.
//
// A database with no migration table at all is a fresh one, and answers none
// rather than failing: it is about to be created.
func (self *database) UnknownMigrations() ([]string, error) {
	if !self.db.Migrator().HasTable(&migrationModel{}) {
		return nil, nil
	}

	// The identifiers and nothing else.
	//
	// Reading the whole model would select every column the current one has,
	// and this runs before Migrate's AutoMigrate has had a chance to add a
	// column an older binary's table is missing — so a table written before
	// ReverseSQL existed would fail the select, and this function's caller
	// turns that into a refusal to start. Asking for one column that has been
	// the primary key since the beginning cannot fail that way, and asking
	// for it does not change anything, which is what a question should do.
	var identifiers []string
	if err := self.db.Model(&migrationModel{}).Pluck("id", &identifiers).Error; err != nil {
		return nil, err
	}

	known := make(map[string]struct{})
	for _, migration := range migrations.Migrations() {
		known[migration.ID] = struct{}{}
	}

	var unknown []string
	for _, identifier := range identifiers {
		if _, ok := known[identifier]; !ok {
			unknown = append(unknown, identifier)
		}
	}
	sort.Strings(unknown)
	return unknown, nil
}

func (self *database) Migrate() error {
	if err := self.db.AutoMigrate(&migrationModel{}); err != nil {
		log.Errorf("failed to migrate database: %s", err)
		return err
	}
	var existingModels []migrationModel
	if err := self.db.Find(&existingModels).Error; err != nil {
		log.Errorf("failed to query for migrations: %s", err)
		return err
	}
	existingModelsMap := make(map[string]migrationModel)
	for _, migrationModel := range existingModels {
		existingModelsMap[migrationModel.ID] = migrationModel
	}

	currentMigrations := migrations.Migrations()
	currentMigrationIds := make(map[string]struct{}, len(currentMigrations))
	for _, migration := range currentMigrations {
		currentMigrationIds[migration.ID] = struct{}{}
	}

	var unknownMigrationIds []string
	for migrationId := range existingModelsMap {
		if _, ok := currentMigrationIds[migrationId]; !ok {
			unknownMigrationIds = append(unknownMigrationIds, migrationId)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(unknownMigrationIds)))
	for _, migrationId := range unknownMigrationIds {
		model := existingModelsMap[migrationId]
		if strings.TrimSpace(model.ReverseSQL) == "" {
			panic(fmt.Sprintf("db: missing reverse sql for migration %s", migrationId))
		}
		log.Debugf("reverting database migration: %s", migrationId)
		if err := self.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(model.ReverseSQL).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", migrationId).Delete(&migrationModel{}).Error; err != nil {
				return err
			}
			return nil
		}); err != nil {
			log.Errorf("failed to revert database migration: %s: %s", migrationId, err)
			return err
		}
		log.Noticef("reverted database migration: %s", migrationId)
	}

	for _, migration := range currentMigrations {
		if existingModel, ok := existingModelsMap[migration.ID]; ok {
			log.Debugf("database migration %s already done at %s", migration.ID, existingModel.MigratedAt)
			continue
		}

		log.Debugf("migrating database: %s", migration.ID)
		if err := self.db.Transaction(func(tx *gorm.DB) error {
			if migration.SQL != "" {
				if err := tx.Exec(migration.SQL).Error; err != nil {
					return err
				}
			}
			if err := tx.Create(&migrationModel{
				ID:         migration.ID,
				MigratedAt: time.Now().In(time.Local),
				ReverseSQL: migration.ReverseSQL,
			}).Error; err != nil {
				return err
			}
			return nil
		}); err != nil {
			log.Errorf("failed to migrate database: %s: %s", migration.ID, err)
			return err
		}
		log.Noticef("migrated database: %s", migration.ID)
	}
	return nil
}
