package storageusage

import (
	"math"
	"sort"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

const (
	// ProjectionWindowDays is how far back the fit looks: the last thirty
	// days of daily samples, or fewer when there is less history.
	ProjectionWindowDays = 30

	// MinimumProjectionSamples is how few daily samples a line is not fitted
	// through: two points always make a line, and that line says nothing.
	MinimumProjectionSamples = 3

	// The days of room left at which an operator is warned about a disk.
	FirstWarningDays  = 30
	SecondWarningDays = 7
)

// DayTotal is everything the server holds on one UTC day: the database and,
// when they are on a disk the server measures, the files.
type DayTotal struct {
	Day       time.Time
	SizeBytes int64
}

// DayTotals adds each day's samples up: the database's total and, when
// isFilesCounted, every kind of file. A day that has neither is left out.
// Oldest first.
func DayTotals(samples []models.StorageUsageSample, isFilesCounted bool) []DayTotal {
	byDay := map[time.Time]int64{}
	for _, sample := range samples {
		isDatabase := sample.MeasureKind == models.StorageMeasureDatabase && sample.MeasureKey == models.StorageDatabaseTotalKey
		isFile := sample.MeasureKind == models.StorageMeasureFileKind && isFilesCounted
		if isDatabase || isFile {
			byDay[sample.SampledOn.UTC()] += sample.MeasuredBytes
		}
	}
	totals := make([]DayTotal, 0, len(byDay))
	for day, sizeBytes := range byDay {
		totals = append(totals, DayTotal{Day: day, SizeBytes: sizeBytes})
	}
	sort.Slice(totals, func(left, right int) bool { return totals[left].Day.Before(totals[right].Day) })
	return totals
}

// Growth is a line fitted through the recent daily totals.
type Growth struct {
	// SampleCount is how many daily totals the line was fitted through.
	SampleCount int

	// IsProjectable is false when there were too few samples, or all on one
	// day, for a line.
	IsProjectable bool

	// GrowthBytesPerDay is the slope of the line; negative when the server
	// is shrinking.
	GrowthBytesPerDay float64

	// CurrentSizeBytes is the newest total, from which the projections go
	// forward at the fitted rate.
	CurrentSizeBytes int64
	In30DaysBytes    int64
	In90DaysBytes    int64
	In365DaysBytes   int64
}

// Fit fits a least-squares line through the daily totals of the last
// ProjectionWindowDays days up to now.
func Fit(totals []DayTotal, now time.Time) Growth {
	windowStart := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -(ProjectionWindowDays - 1))
	var window []DayTotal
	for _, total := range totals {
		if !total.Day.Before(windowStart) && !total.Day.After(now.UTC()) {
			window = append(window, total)
		}
	}
	growth := Growth{SampleCount: len(window)}
	if len(window) > 0 {
		growth.CurrentSizeBytes = window[len(window)-1].SizeBytes
		growth.In30DaysBytes, growth.In90DaysBytes, growth.In365DaysBytes = growth.CurrentSizeBytes, growth.CurrentSizeBytes, growth.CurrentSizeBytes
	}
	if len(window) < MinimumProjectionSamples {
		return growth
	}
	first := window[0].Day
	var sumX, sumY float64
	for _, total := range window {
		sumX += total.Day.Sub(first).Hours() / 24
		sumY += float64(total.SizeBytes)
	}
	count := float64(len(window))
	meanX, meanY := sumX/count, sumY/count
	var covariance, variance float64
	for _, total := range window {
		offsetX := total.Day.Sub(first).Hours()/24 - meanX
		covariance += offsetX * (float64(total.SizeBytes) - meanY)
		variance += offsetX * offsetX
	}
	if variance == 0 {
		return growth
	}
	growth.IsProjectable = true
	growth.GrowthBytesPerDay = covariance / variance
	growth.In30DaysBytes = projectedBytes(growth.CurrentSizeBytes, growth.GrowthBytesPerDay, 30)
	growth.In90DaysBytes = projectedBytes(growth.CurrentSizeBytes, growth.GrowthBytesPerDay, 90)
	growth.In365DaysBytes = projectedBytes(growth.CurrentSizeBytes, growth.GrowthBytesPerDay, 365)
	return growth
}

func projectedBytes(currentBytes int64, bytesPerDay float64, days int) int64 {
	return max(0, currentBytes+int64(math.Round(bytesPerDay*float64(days))))
}

// FullOn is the day a disk with freeBytes of room fills at this growth, and
// how many days away that is. It is nil when the totals are not projectable,
// when the server is not growing, or when the disk is not measured.
func (self Growth) FullOn(freeBytes int64, isMeasured bool, now time.Time) (*time.Time, float64) {
	if !isMeasured || !self.IsProjectable || self.GrowthBytesPerDay <= 0 {
		return nil, 0
	}
	daysUntilFull := float64(freeBytes) / self.GrowthBytesPerDay
	if daysUntilFull > 100*365 {
		return nil, 0
	}
	day := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, int(math.Ceil(daysUntilFull)))
	return &day, daysUntilFull
}

// WarningDaysFor is the warning a disk that fills in daysUntilFull is due:
// seven, thirty, or zero for none.
func WarningDaysFor(daysUntilFull float64) int {
	switch {
	case daysUntilFull <= SecondWarningDays:
		return SecondWarningDays
	case daysUntilFull <= FirstWarningDays:
		return FirstWarningDays
	default:
		return 0
	}
}
