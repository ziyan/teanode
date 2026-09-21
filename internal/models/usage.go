package models

import (
	"sort"
	"time"
)

var (
	HourlyInterval uint64 = 3600 // every hour

	MaxHourlyUsages uint64 = 53 * 7 * 24 // save 53 weeks worth of hourly usage
)

// Usage is a record of resource usage.
type Usage struct {
	// Unix timestamp, seconds since epoch
	Timestamp uint64 `json:"timestamp,omitempty"`

	// Interval length in seconds
	Interval uint64 `json:"interval,omitempty"`

	// Values recorded
	Values []uint64 `json:"values,omitempty"`
}

func DiscretizeTimestamp(interval, timestamp uint64) uint64 {
	return timestamp / interval * interval
}

func NewUsages(now time.Time, values []uint64) []Usage {
	timestamp := uint64(now.Unix())
	newUsages := make([]Usage, 0, 3)
	for _, interval := range []uint64{
		HourlyInterval,
	} {
		copiedValues := make([]uint64, len(values))
		copy(copiedValues, values)
		newUsages = append(newUsages, Usage{
			Timestamp: DiscretizeTimestamp(interval, timestamp),
			Interval:  interval,
			Values:    copiedValues,
		})
	}
	return newUsages
}

func MergeUsages(existingUsages, newUsages []Usage) []Usage {
	if len(existingUsages) == 0 {
		return newUsages
	}
	if len(newUsages) == 0 {
		return existingUsages
	}

	// separate by interval
	usagesMap := buildUsagesMap(existingUsages)

	// sort
	for _, usages := range usagesMap {
		SortUsages(usages)
	}

	// upsert
	for _, newUsage := range newUsages {
		newUsage := newUsage
		usages := usagesMap[newUsage.Interval]
		index := sort.Search(len(usages), func(i int) bool {
			return usages[i].Timestamp >= newUsage.Timestamp
		})
		if index < len(usages) && usages[index].Timestamp == newUsage.Timestamp {
			for valueIndex, value := range newUsage.Values {
				if valueIndex < len(usages[index].Values) {
					usages[index].Values[valueIndex] += value
				} else {
					usages[index].Values = append(usages[index].Values, value)
				}
			}
		} else {
			usages = append(usages, newUsage)
			copy(usages[index+1:], usages[index:])
			usages[index] = newUsage
			usagesMap[newUsage.Interval] = usages
		}
	}

	filterUsagesMap(usagesMap, MaxHourlyUsages)
	return mergeUsagesMap(usagesMap)
}

func SortUsages(usages []Usage) {
	sort.Slice(usages, func(i, j int) bool {
		return usages[i].Timestamp < usages[j].Timestamp
	})
}

func buildUsagesMap(usages []Usage) map[uint64][]Usage {
	count := len(usages)

	usagesMap := make(map[uint64][]Usage)
	for _, usage := range usages {
		if _, ok := usagesMap[usage.Interval]; !ok {
			usagesMap[usage.Interval] = make([]Usage, 0, count)
		}
		usagesMap[usage.Interval] = append(usagesMap[usage.Interval], usage)
	}

	return usagesMap
}

func filterUsagesMap(usagesMap map[uint64][]Usage, maxHourlyUsages uint64) {
	for interval, usages := range usagesMap {
		var maxUsages uint64
		switch interval {
		case HourlyInterval: // every hour
			maxUsages = maxHourlyUsages
		}
		if len(usages) > int(maxUsages) {
			// assume already sorted
			usagesMap[interval] = usages[len(usages)-int(maxUsages):]
		}
	}
}

func mergeUsagesMap(usagesMap map[uint64][]Usage) []Usage {
	var count int
	for _, usages := range usagesMap {
		count += len(usages)
	}
	mergedUsages := make([]Usage, 0, count)
	for _, usages := range usagesMap {
		mergedUsages = append(mergedUsages, usages...)
	}
	return mergedUsages
}

func FilterUsages(usages []Usage, maxHourlyUsages uint64) []Usage {
	if len(usages) == 0 {
		return nil
	}
	usagesMap := buildUsagesMap(usages)
	filterUsagesMap(usagesMap, maxHourlyUsages)
	return mergeUsagesMap(usagesMap)
}

func SmoothUsages(usages []Usage, fromTimestamp, toTimestamp, interval uint64) Usage {
	discretizedFromTimestamp := DiscretizeTimestamp(interval, fromTimestamp)
	discretizedToTimestamp := DiscretizeTimestamp(interval, toTimestamp)
	result := Usage{
		Timestamp: discretizedToTimestamp,
		Interval:  interval,
	}
	ticks := int64(discretizedToTimestamp-discretizedFromTimestamp) / int64(interval)
	if ticks <= 0 {
		return result
	}
	factor1 := float64(2) / float64(1+ticks)
	factor2 := 1.0 - factor1
	for timestamp := discretizedFromTimestamp; timestamp <= discretizedToTimestamp; timestamp += interval {
		var usage Usage
		for _, u := range usages {
			if u.Timestamp == timestamp {
				usage = u
				break
			}
		}
		for valueIndex, value := range usage.Values {
			if valueIndex < len(result.Values) {
				result.Values[valueIndex] = uint64(float64(value)*factor1 + float64(result.Values[valueIndex])*factor2)
			} else {
				result.Values = append(result.Values, uint64(float64(value)*factor1))
			}
		}
	}
	return result
}
