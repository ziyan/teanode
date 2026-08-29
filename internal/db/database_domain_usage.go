package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ziyan/teanode/internal/models"
)

type DomainUsageOperation interface {
	// store new domain usages
	PutDomainUsages(domainUsagesMap map[string][]models.Usage, options *Options) error

	// query and sum up domain usages later than specified timestamp
	SumDomainUsages(domainIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error)

	// query domain usages later than specified timestamp
	QueryDomainUsages(domainIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error)

	// scavenge usages
	ScavengeDomainUsages(options *Options) error
}

type domainUsageModel struct {
	BackendID string `gorm:"column:backend_id;primary_key:true;size:32"`
	DomainID  string `gorm:"primary_key:true;size:32"`
	Interval  uint64 `gorm:"primary_key:true"`
	Timestamp uint64 `gorm:"primary_key:true"`

	Values pq.Int64Array `gorm:"type:bigint[]"`
}

func (self *domainUsageModel) TableName() string {
	return "domain_usage"
}

func (self *transaction) PutDomainUsages(domainUsagesMap map[string][]models.Usage, options *Options) error {
	var intervalTimestamps []string
	for domainId, usages := range domainUsagesMap {
		for _, usage := range usages {
			intervalTimestamps = append(intervalTimestamps, fmt.Sprintf("('%s','%s',%d,%d)", self.database.settings.BackendID, domainId, usage.Interval, usage.Timestamp))
		}
	}
	if len(intervalTimestamps) == 0 {
		return nil
	}

	var existingModels []domainUsageModel
	if err := self.tx.Where(fmt.Sprintf("(backend_id, domain_id, interval, timestamp) IN (%s)", strings.Join(intervalTimestamps, ","))).Find(&existingModels).Error; err != nil {
		return err
	}

	var newModels []domainUsageModel
	for domainId, usages := range domainUsagesMap {
		for _, usage := range usages {
			var found bool
			for index, model := range existingModels {
				if model.DomainID == domainId && model.Interval == usage.Interval && model.Timestamp == usage.Timestamp {
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
				newModels = append(newModels, domainUsageModel{
					BackendID: self.database.settings.BackendID,
					DomainID:  domainId,
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

func (self *transaction) SumDomainUsages(domainIds []string, interval uint64, timestamp uint64, options *Options) ([]models.Usage, error) {
	var results []struct {
		DomainID   string
		Ordinality uint64
		Value      uint64
	}
	if err := self.tx.Raw(`SELECT "domain_id", "ordinality", SUM("unnest") AS "value" FROM "domain_usage", unnest("values") WITH ORDINALITY WHERE "domain_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("domain_id", "ordinality") ORDER BY ("domain_id", "ordinality")`, domainIds, interval, timestamp).Find(&results).Error; err != nil {
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
		usage, ok := usagesMap[result.DomainID]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.DomainID] = usage
	}
	usages := make([]models.Usage, 0, len(domainIds))
	for _, domainId := range domainIds {
		usage, ok := usagesMap[domainId]
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

func (self *transaction) QueryDomainUsages(domainIds []string, interval uint64, timestamp uint64, options *Options) ([][]models.Usage, error) {
	var results []struct {
		DomainID   string
		Timestamp  uint64
		Ordinality uint64
		Value      uint64
	}
	if err := self.tx.Raw(`SELECT "domain_id", "timestamp", "ordinality", SUM("unnest") AS "value" FROM "domain_usage", unnest("values") WITH ORDINALITY WHERE "domain_id" IN (?) AND "interval" = ? AND "timestamp" >= ? GROUP BY ("domain_id", "timestamp", "ordinality") ORDER BY ("domain_id", "timestamp", "ordinality")`, domainIds, interval, timestamp).Find(&results).Error; err != nil {
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
		if _, ok := usagesMap[result.DomainID]; !ok {
			usagesMap[result.DomainID] = make(map[uint64]models.Usage)
		}
		usage, ok := usagesMap[result.DomainID][result.Timestamp]
		if !ok {
			usage = models.Usage{
				Interval:  interval,
				Timestamp: result.Timestamp,
				Values:    make([]uint64, maxOrdinality),
			}
		}
		usage.Values[result.Ordinality-1] = result.Value
		usagesMap[result.DomainID][result.Timestamp] = usage
	}
	usages := make([][]models.Usage, 0, len(domainIds))
	for _, domainId := range domainIds {
		domainUsages := make([]models.Usage, 0, len(usagesMap[domainId]))
		for _, usage := range usagesMap[domainId] {
			domainUsages = append(domainUsages, usage)
		}
		models.SortUsages(domainUsages)
		usages = append(usages, domainUsages)
	}
	return usages, nil
}

func (self *transaction) ScavengeDomainUsages(options *Options) error {
	timestamp := uint64(time.Now().Unix())
	hourlyCutoff := models.DiscretizeTimestamp(models.HourlyInterval, timestamp) - models.HourlyInterval*models.MaxHourlyUsages
	if err := self.tx.Where("\"interval\" = ?", models.HourlyInterval).Where("\"timestamp\" < ?", hourlyCutoff).Delete(&domainUsageModel{}).Error; err != nil {
		return err
	}
	return nil
}
