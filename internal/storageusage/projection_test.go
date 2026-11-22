package storageusage_test

import (
	"math"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storageusage"
)

const gigabyte = int64(1) << 30

var today = time.Date(2030, 3, 31, 12, 0, 0, 0, time.UTC)

func dayTotals(sizesGigabytes ...int64) []storageusage.DayTotal {
	totals := make([]storageusage.DayTotal, 0, len(sizesGigabytes))
	for index, size := range sizesGigabytes {
		daysAgo := len(sizesGigabytes) - 1 - index
		totals = append(totals, storageusage.DayTotal{Day: today.Truncate(24*time.Hour).AddDate(0, 0, -daysAgo), SizeBytes: size * gigabyte})
	}
	return totals
}

func TestAFitThroughSteadyGrowthGivesItsSlopeAndTheSizesAhead(test *testing.T) {
	test.Parallel()
	growth := storageusage.Fit(dayTotals(100, 102, 104, 106, 108), today)
	if !growth.IsProjectable || growth.SampleCount != 5 {
		test.Fatalf("growth = %+v, want a fit through 5 samples", growth)
	}
	if math.Abs(growth.GrowthBytesPerDay-float64(2*gigabyte)) > 1 {
		test.Errorf("GrowthBytesPerDay = %f, want 2 GiB", growth.GrowthBytesPerDay)
	}
	if growth.CurrentSizeBytes != 108*gigabyte || growth.In30DaysBytes != 168*gigabyte || growth.In90DaysBytes != 288*gigabyte || growth.In365DaysBytes != 838*gigabyte {
		test.Errorf("sizes ahead = %d, %d, %d, %d", growth.CurrentSizeBytes, growth.In30DaysBytes, growth.In90DaysBytes, growth.In365DaysBytes)
	}
}

func TestFewerThanThreeSamplesAreNotProjected(test *testing.T) {
	test.Parallel()
	for _, count := range []int{0, 1, 2} {
		sizes := []int64{100, 110}[:min(count, 2)]
		growth := storageusage.Fit(dayTotals(sizes...), today)
		if growth.IsProjectable || growth.GrowthBytesPerDay != 0 {
			test.Errorf("%d samples: growth = %+v, want none", count, growth)
		}
		if fullOn, _ := growth.FullOn(10*gigabyte, true, today); fullOn != nil {
			test.Errorf("%d samples: a fill date %s was given", count, fullOn)
		}
	}
}

func TestOnlyTheLastThirtyDaysAreFitted(test *testing.T) {
	test.Parallel()
	// Ten days that fell steeply, then thirty steady ones: the steep days are
	// outside the window and must not tilt the line.
	sizes := []int64{}
	for index := 0; index < 10; index++ {
		sizes = append(sizes, int64(500-index*40))
	}
	for index := 0; index < 30; index++ {
		sizes = append(sizes, int64(200+index))
	}
	growth := storageusage.Fit(dayTotals(sizes...), today)
	if growth.SampleCount != 30 {
		test.Fatalf("SampleCount = %d, want 30", growth.SampleCount)
	}
	if math.Abs(growth.GrowthBytesPerDay-float64(gigabyte)) > 1 {
		test.Errorf("GrowthBytesPerDay = %f, want 1 GiB", growth.GrowthBytesPerDay)
	}
}

func TestAShrinkingServerNeverFills(test *testing.T) {
	test.Parallel()
	growth := storageusage.Fit(dayTotals(100, 98, 96, 94), today)
	if growth.GrowthBytesPerDay >= 0 {
		test.Fatalf("GrowthBytesPerDay = %f, want a shrinking server", growth.GrowthBytesPerDay)
	}
	if fullOn, _ := growth.FullOn(10*gigabyte, true, today); fullOn != nil {
		test.Errorf("a fill date %s was given for a shrinking server", fullOn)
	}
	if growth.In365DaysBytes < 0 {
		test.Errorf("In365DaysBytes = %d, a size is never negative", growth.In365DaysBytes)
	}
}

func TestADiskFillsWhenItsFreeSpaceIsUsedAtTheFittedRate(test *testing.T) {
	test.Parallel()
	growth := storageusage.Fit(dayTotals(100, 102, 104, 106), today)
	fullOn, daysUntilFull := growth.FullOn(40*gigabyte, true, today)
	if fullOn == nil {
		test.Fatal("no fill date")
	}
	if math.Abs(daysUntilFull-20) > 0.001 {
		test.Errorf("daysUntilFull = %f, want 20", daysUntilFull)
	}
	if want := time.Date(2030, 4, 20, 0, 0, 0, 0, time.UTC); !fullOn.Equal(want) {
		test.Errorf("fullOn = %s, want %s", fullOn, want)
	}
	if notMeasured, _ := growth.FullOn(40*gigabyte, false, today); notMeasured != nil {
		test.Errorf("a disk that is not measured was given a fill date %s", notMeasured)
	}
}

func TestDayTotalsAddTheDatabaseAndTheFilesOfEachDay(test *testing.T) {
	test.Parallel()
	day := time.Date(2030, 3, 30, 0, 0, 0, 0, time.UTC)
	samples := []models.StorageUsageSample{
		{SampledOn: day, MeasureKind: models.StorageMeasureDatabase, MeasureKey: "total", MeasuredBytes: 100},
		{SampledOn: day, MeasureKind: models.StorageMeasureDatabaseArea, MeasureKey: "mail", MeasuredBytes: 40},
		{SampledOn: day, MeasureKind: models.StorageMeasureFileKind, MeasureKey: "mail", MeasuredBytes: 7},
		{SampledOn: day, MeasureKind: models.StorageMeasureFileKind, MeasureKey: "photoOriginal", MeasuredBytes: 30},
		{SampledOn: day, MeasureKind: models.StorageMeasureDisk, MeasureKey: "dataDirectory", MeasuredBytes: 999},
	}
	if totals := storageusage.DayTotals(samples, true); len(totals) != 1 || totals[0].SizeBytes != 137 {
		test.Errorf("with files: %+v, want one day of 137 (areas and disks are not added in)", totals)
	}
	if totals := storageusage.DayTotals(samples, false); len(totals) != 1 || totals[0].SizeBytes != 100 {
		test.Errorf("without files: %+v, want one day of 100", totals)
	}
}

func TestAWarningIsSentOnceAtThirtyDaysAndOnceMoreAtSeven(test *testing.T) {
	test.Parallel()
	disk := storageusage.Disk{DiskLabel: storageusage.DiskDataDirectory, IsMeasured: true, DiskTotalBytes: 500 * gigabyte, DiskFreeBytes: 40 * gigabyte}
	growth := storageusage.Fit(dayTotals(100, 102, 104, 106), today)
	previous := map[string]*models.StorageDiskWarning{}

	due, recovered := storageusage.DueWarnings(growth, []storageusage.Disk{disk}, previous, today)
	if len(due) != 1 || due[0].WarningDays != 30 || len(recovered) != 0 {
		test.Fatalf("first: due %+v recovered %v, want one warning at 30 days", due, recovered)
	}
	previous[disk.DiskLabel] = &models.StorageDiskWarning{DiskLabel: disk.DiskLabel, WarningDays: 30}
	if due, _ := storageusage.DueWarnings(growth, []storageusage.Disk{disk}, previous, today); len(due) != 0 {
		test.Errorf("the next day: due %+v, the same warning was repeated", due)
	}

	disk.DiskFreeBytes = 10 * gigabyte
	due, _ = storageusage.DueWarnings(growth, []storageusage.Disk{disk}, previous, today)
	if len(due) != 1 || due[0].WarningDays != 7 {
		test.Fatalf("at 5 days: due %+v, want one warning at 7 days", due)
	}
	previous[disk.DiskLabel] = &models.StorageDiskWarning{DiskLabel: disk.DiskLabel, WarningDays: 7}
	if due, _ := storageusage.DueWarnings(growth, []storageusage.Disk{disk}, previous, today); len(due) != 0 {
		test.Errorf("after the 7 day warning: due %+v, the same warning was repeated", due)
	}

	disk.DiskFreeBytes = 400 * gigabyte
	due, recovered = storageusage.DueWarnings(growth, []storageusage.Disk{disk}, previous, today)
	if len(due) != 0 || len(recovered) != 1 || recovered[0] != disk.DiskLabel {
		test.Errorf("after space was freed: due %+v recovered %v, want the disk forgotten", due, recovered)
	}
}

func TestADiskThatIsNotMeasuredIsNeverWarnedAbout(test *testing.T) {
	test.Parallel()
	growth := storageusage.Fit(dayTotals(100, 200, 300, 400), today)
	due, recovered := storageusage.DueWarnings(growth, []storageusage.Disk{{DiskLabel: storageusage.DiskObjectStore}}, nil, today)
	if len(due) != 0 || len(recovered) != 0 {
		test.Errorf("due %+v recovered %v for an object store", due, recovered)
	}
}
