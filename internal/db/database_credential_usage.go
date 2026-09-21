package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
)

type CredentialUsageOperation interface {
	// store new credential usages
	PutCredentialUsages(credentialUsagesMap map[string][]models.Usage, options *Options) error

	// query and sum up credential usages later than specified timestamp
	SumCredentialUsages(credentialIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error)

	// query credential usages later than specified timestamp
	QueryCredentialUsages(credentialIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error)

	// scavenge usages
	ScavengeCredentialUsages(options *Options) error
}

type credentialUsageModel struct {
	BackendID    string `gorm:"column:backend_id;primary_key:true;size:32"`
	CredentialID string `gorm:"primary_key:true;size:32"`
	Interval     uint64 `gorm:"primary_key:true"`
	Timestamp    uint64 `gorm:"primary_key:true"`

	Values pq.Int64Array `gorm:"type:bigint[]"`
}

func (self *credentialUsageModel) TableName() string {
	return "credential_usage"
}

func (self *transaction) PutCredentialUsages(credentialUsagesMap map[string][]models.Usage, options *Options) error {
	var intervalTimestamps []string
	for credentialId, usages := range credentialUsagesMap {
		for _, usage := range usages {
			intervalTimestamps = append(intervalTimestamps, fmt.Sprintf("('%s','%s',%d,%d)", self.database.settings.BackendID, credentialId, usage.Interval, usage.Timestamp))
		}
	}
	if len(intervalTimestamps) == 0 {
		return nil
	}

	var existingModels []credentialUsageModel
	if err := self.tx.Where(fmt.Sprintf("(backend_id, credential_id, interval, timestamp) IN (%s)", strings.Join(intervalTimestamps, ","))).Find(&existingModels).Error; err != nil {
		return err
	}

	var newModels []credentialUsageModel
	for credentialId, usages := range credentialUsagesMap {
		for _, usage := range usages {
			var found bool
			for index, model := range existingModels {
				if model.CredentialID == credentialId && model.Interval == usage.Interval && model.Timestamp == usage.Timestamp {
					for valueIndex, value := range usage.Values {
						if valueIndex < len(existingModels[index].Values) {
							existingModels[index].Values[valueIndex] += int64(value)
						} else {
							existingModels[index].Values = append(existingModels[index].Values, int64(value))
						}
					}
					found = true
					break
				}
			}
			if !found {
				newModels = append(newModels, credentialUsageModel{
					BackendID:    self.database.settings.BackendID,
					CredentialID: credentialId,
					Interval:     usage.Interval,
					Timestamp:    usage.Timestamp,
					Values:       convertFromUint64Array(usage.Values),
				})
			}
		}
	}

	for _, model := range existingModels {
		if err := self.tx.Save(&model).Error; err != nil {
			return err
		}
	}
	if len(newModels) > 0 {
		if err := self.tx.Create(&newModels).Error; err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) SumCredentialUsages(credentialIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error) {
	var results []struct {
		CredentialID string
		Ordinality   uint64
		Value        uint64
	}
	if err := self.tx.Raw(`SELECT "credential_id", "ordinality", SUM("unnest") AS "value" FROM "credential_usage", unnest("values") WITH ORDINALITY WHERE "credential_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("credential_id", "ordinality") ORDER BY ("credential_id", "ordinality")`, credentialIds, interval, timestamp).Find(&results).Error; err != nil {
		return nil, err
	}
	var maxOrdinality uint64
	for _, result := range results {
		if result.Ordinality > maxOrdinality {
			maxOrdinality = result.Ordinality
		}
	}
	usagesMap := make(map[string]models.Usage)
	for _, result := range results {
		usage, ok := usagesMap[result.CredentialID]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.CredentialID] = usage
	}
	usages := make([]models.Usage, 0, len(credentialIds))
	for _, credentialId := range credentialIds {
		usage, ok := usagesMap[credentialId]
		if ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: timestamp,
			}
		}
		usages = append(usages, usage)
	}
	return usages, nil
}

func (self *transaction) QueryCredentialUsages(credentialIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error) {
	var results []struct {
		CredentialID string
		Timestamp    uint64
		Ordinality   uint64
		Value        uint64
	}
	if err := self.tx.Raw(`SELECT "credential_id", "timestamp", "ordinality", SUM("unnest") AS "value" FROM "credential_usage", unnest("values") WITH ORDINALITY WHERE "credential_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("credential_id", "timestamp", "ordinality") ORDER BY ("credential_id", "timestamp", "ordinality")`, credentialIds, interval, timestamp).Find(&results).Error; err != nil {
		return nil, err
	}
	var maxOrdinality uint64
	for _, result := range results {
		if result.Ordinality > maxOrdinality {
			maxOrdinality = result.Ordinality
		}
	}
	usagesMap := make(map[string]map[uint64]models.Usage)
	for _, result := range results {
		if _, ok := usagesMap[result.CredentialID]; !ok {
			usagesMap[result.CredentialID] = make(map[uint64]models.Usage)
		}
		usage, ok := usagesMap[result.CredentialID][result.Timestamp]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: result.Timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.CredentialID][result.Timestamp] = usage
	}
	usages := make([][]models.Usage, 0, len(credentialIds))
	for _, credentialId := range credentialIds {
		credentialUsages := make([]models.Usage, 0, len(usagesMap[credentialId]))
		for _, usage := range usagesMap[credentialId] {
			credentialUsages = append(credentialUsages, usage)
		}
		models.SortUsages(credentialUsages)
		usages = append(usages, credentialUsages)
	}
	return usages, nil
}

func (self *transaction) ScavengeCredentialUsages(options *Options) error {
	timestamp := uint64(time.Now().Unix())
	hourlyCutoff := models.DiscretizeTimestamp(models.HourlyInterval, timestamp) - models.HourlyInterval*models.MaxHourlyUsages
	if err := self.tx.Where("\"interval\" = ?", models.HourlyInterval).Where("\"timestamp\" < ?", hourlyCutoff).Delete(&credentialUsageModel{}).Error; err != nil {
		return err
	}
	return nil
}
