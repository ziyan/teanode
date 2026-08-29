package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
)

type AliasUsageOperation interface {
	// store new alias usages
	PutAliasUsages(aliasUsagesMap map[string][]models.Usage, options *Options) error

	// query and sum up alias usages later than specified timestamp
	SumAliasUsages(aliasIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error)

	// query alias usages later than specified timestamp
	QueryAliasUsages(aliasIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error)

	// scavenge usages
	ScavengeAliasUsages(options *Options) error
}

type aliasUsageModel struct {
	BackendID string `gorm:"column:backend_id;primary_key:true;size:32"`
	AliasID   string `gorm:"primary_key:true;size:32"`
	Interval  uint64 `gorm:"primary_key:true"`
	Timestamp uint64 `gorm:"primary_key:true"`

	Values pq.Int64Array `gorm:"type:bigint[]"`
}

func (self *aliasUsageModel) TableName() string {
	return "alias_usage"
}

func (self *transaction) PutAliasUsages(aliasUsagesMap map[string][]models.Usage, options *Options) error {
	var intervalTimestamps []string
	for aliasId, usages := range aliasUsagesMap {
		for _, usage := range usages {
			intervalTimestamps = append(intervalTimestamps, fmt.Sprintf("('%s','%s',%d,%d)", self.database.settings.BackendID, aliasId, usage.Interval, usage.Timestamp))
		}
	}
	if len(intervalTimestamps) == 0 {
		return nil
	}

	var existingModels []aliasUsageModel
	if err := self.tx.Where(fmt.Sprintf("(backend_id, alias_id, interval, timestamp) IN (%s)", strings.Join(intervalTimestamps, ","))).Find(&existingModels).Error; err != nil {
		return err
	}

	var newModels []aliasUsageModel
	for aliasId, usages := range aliasUsagesMap {
		for _, usage := range usages {
			var found bool
			for index, model := range existingModels {
				if model.AliasID == aliasId && model.Interval == usage.Interval && model.Timestamp == usage.Timestamp {
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
				newModels = append(newModels, aliasUsageModel{
					BackendID: self.database.settings.BackendID,
					AliasID:   aliasId,
					Interval:  usage.Interval,
					Timestamp: usage.Timestamp,
					Values:    convertFromUint64Array(usage.Values),
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

func (self *transaction) SumAliasUsages(aliasIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error) {
	var results []struct {
		AliasID    string
		Ordinality uint64
		Value      uint64
	}
	if err := self.tx.Raw(`SELECT "alias_id", "ordinality", SUM("unnest") AS "value" FROM "alias_usage", unnest("values") WITH ORDINALITY WHERE "alias_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("alias_id", "ordinality") ORDER BY ("alias_id", "ordinality")`, aliasIds, interval, timestamp).Find(&results).Error; err != nil {
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
		usage, ok := usagesMap[result.AliasID]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.AliasID] = usage
	}
	usages := make([]models.Usage, 0, len(aliasIds))
	for _, aliasId := range aliasIds {
		usage, ok := usagesMap[aliasId]
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

func (self *transaction) QueryAliasUsages(aliasIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error) {
	var results []struct {
		AliasID    string
		Timestamp  uint64
		Ordinality uint64
		Value      uint64
	}
	if err := self.tx.Raw(`SELECT "alias_id", "timestamp", "ordinality", SUM("unnest") AS "value" FROM "alias_usage", unnest("values") WITH ORDINALITY WHERE "alias_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("alias_id", "timestamp", "ordinality") ORDER BY ("alias_id", "timestamp", "ordinality")`, aliasIds, interval, timestamp).Find(&results).Error; err != nil {
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
		if _, ok := usagesMap[result.AliasID]; !ok {
			usagesMap[result.AliasID] = make(map[uint64]models.Usage)
		}
		usage, ok := usagesMap[result.AliasID][result.Timestamp]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: result.Timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.AliasID][result.Timestamp] = usage
	}
	usages := make([][]models.Usage, 0, len(aliasIds))
	for _, aliasId := range aliasIds {
		aliasUsages := make([]models.Usage, 0, len(usagesMap[aliasId]))
		for _, usage := range usagesMap[aliasId] {
			aliasUsages = append(aliasUsages, usage)
		}
		models.SortUsages(aliasUsages)
		usages = append(usages, aliasUsages)
	}
	return usages, nil
}

func (self *transaction) ScavengeAliasUsages(options *Options) error {
	timestamp := uint64(time.Now().Unix())
	hourlyCutoff := models.DiscretizeTimestamp(models.HourlyInterval, timestamp) - models.HourlyInterval*models.MaxHourlyUsages
	if err := self.tx.Where("\"interval\" = ?", models.HourlyInterval).Where("\"timestamp\" < ?", hourlyCutoff).Delete(&aliasUsageModel{}).Error; err != nil {
		return err
	}
	return nil
}
