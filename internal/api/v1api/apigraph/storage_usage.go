package apigraph

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storageusage"
)

// StorageQuery is what the database and the blob store hold, for the
// operator: never the content of a file, and no person's database rows.
type StorageQuery interface {
	// What the database and the files hold now, broken down by area, kind of
	// file or person; the daily samples of the range for the trend; the
	// disks the server measured; and a line fitted through the last thirty
	// days with the day each measured disk fills. Needs server:manage.
	StorageUsage(ctx context.Context, arguments StorageUsageArguments) (*StorageUsageView, error)
}

// The ways StorageUsage breaks the current size down.
const (
	StorageBreakdownArea   = "area"
	StorageBreakdownKind   = "kind"
	StorageBreakdownPerson = "person"
)

// StorageUsageArguments say what range the trend covers and how the current
// size is broken down.
type StorageUsageArguments struct {
	// Since is the first day of the trend; thirty days ago when left out.
	// Until is the last; today when left out.
	Since *time.Time `json:"since"`
	Until *time.Time `json:"until"`

	// By is area (the database, by what its tables are for), kind (the
	// files, by what they are) or person (the files, by owner). Area when
	// left out.
	By *string `json:"by"`
}

// StorageUsageView is one answer of StorageUsage.
type StorageUsageView struct {
	MeasuredAt  time.Time `json:"measuredAt"`
	BreakdownBy string    `json:"breakdownBy"`

	DatabaseSizeBytes int64 `json:"databaseSizeBytes"`

	// FileSizeBytes is what the rows say the blob store holds. A store that
	// moves old files elsewhere can hold less on its own disk than this.
	FileSizeBytes  int64 `json:"fileSizeBytes"`
	TotalSizeBytes int64 `json:"totalSizeBytes"`

	// UsageRows are the breakdown asked for, largest first. The database is
	// broken down by area and the files by kind or by person; the database
	// is not measured per person.
	UsageRows []StorageUsageRow `json:"usageRows"`

	// HistoryDays are the daily samples of the range, oldest first.
	HistoryDays []StorageHistoryDay `json:"historyDays"`

	Disks  []StorageDiskView `json:"disks"`
	Growth StorageGrowthView `json:"growth"`
}

// StorageUsageRow is one row of the breakdown.
type StorageUsageRow struct {
	// RowKey is the area, the kind of file, or the person's ID; empty for the
	// files no person owns.
	RowKey string `json:"rowKey"`

	// RowLabel is a person's name for a person row, and the key otherwise.
	RowLabel string `json:"rowLabel"`

	RowSizeBytes int64 `json:"rowSizeBytes"`

	// RowTableCount is how many tables an area holds; zero for the rest.
	RowTableCount int `json:"rowTableCount"`

	// RowFileSizes split a person's files by kind; empty for the rest.
	RowFileSizes []StorageKeySize `json:"rowFileSizes"`
}

// StorageKeySize is a size under a key.
type StorageKeySize struct {
	SizeKey   string `json:"sizeKey"`
	SizeBytes int64  `json:"sizeBytes"`
}

// StorageHistoryDay is what one day's sample held.
type StorageHistoryDay struct {
	// SampledOn is the day, as YYYY-MM-DD in UTC.
	SampledOn         string `json:"sampledOn"`
	DatabaseSizeBytes int64  `json:"databaseSizeBytes"`
	FileSizeBytes     int64  `json:"fileSizeBytes"`
	TotalSizeBytes    int64  `json:"totalSizeBytes"`

	// BreakdownSizes are that day's areas or kinds, as the breakdown asked
	// for; empty for person, which is not sampled.
	BreakdownSizes []StorageKeySize `json:"breakdownSizes"`
}

// StorageDiskView is a disk the server writes to.
type StorageDiskView struct {
	// DiskLabel is dataDirectory, fileDirectory or objectStore.
	DiskLabel string `json:"diskLabel"`

	// IsDiskMeasured is false for an object store, which the server cannot
	// ask how full it is, and the sizes are then null.
	IsDiskMeasured bool   `json:"isDiskMeasured"`
	DiskTotalBytes *int64 `json:"diskTotalBytes"`
	DiskFreeBytes  *int64 `json:"diskFreeBytes"`
	DiskUsedBytes  *int64 `json:"diskUsedBytes"`

	// ProjectedFullOn is the day the disk fills at the rate a line through
	// its own daily samples gives, as YYYY-MM-DD in UTC; null when the disk is
	// not growing, there is too little history, or it is not measured.
	ProjectedFullOn *string  `json:"projectedFullOn"`
	DaysUntilFull   *float64 `json:"daysUntilFull"`

	// IsProjectedFull is whether the disk fills within thirty days, which
	// is when the operators are warned, and the page shows its banner.
	IsProjectedFull bool `json:"isProjectedFull"`
}

// StorageGrowthView is the line fitted through the last thirty days.
type StorageGrowthView struct {
	GrowthSampleCount   int  `json:"growthSampleCount"`
	IsGrowthProjectable bool `json:"isGrowthProjectable"`

	// GrowthBytesPerDay is the slope of a least-squares line through the
	// daily totals of the database and the files; negative when it shrinks.
	GrowthBytesPerDay float64 `json:"growthBytesPerDay"`

	CurrentSizeBytes          int64 `json:"currentSizeBytes"`
	ProjectedSize30DaysBytes  int64 `json:"projectedSize30DaysBytes"`
	ProjectedSize90DaysBytes  int64 `json:"projectedSize90DaysBytes"`
	ProjectedSize365DaysBytes int64 `json:"projectedSize365DaysBytes"`

	// AreFilesCounted is whether the files are in the totals: they are when
	// they are on a disk this server can measure, and not when they are in
	// an object store.
	AreFilesCounted bool `json:"areFilesCounted"`
}

const storageDateLayout = "2006-01-02"

func (self *graph) StorageUsage(ctx context.Context, arguments StorageUsageArguments) (*StorageUsageView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	breakdownBy := StorageBreakdownArea
	if arguments.By != nil && strings.TrimSpace(*arguments.By) != "" {
		breakdownBy = strings.TrimSpace(*arguments.By)
	}
	if breakdownBy != StorageBreakdownArea && breakdownBy != StorageBreakdownKind && breakdownBy != StorageBreakdownPerson {
		return nil, fmt.Errorf("by must be %s, %s or %s", StorageBreakdownArea, StorageBreakdownKind, StorageBreakdownPerson)
	}

	tx := self.transaction(ctx)
	now := time.Now()
	snapshot, err := storageusage.Measure(tx)
	if err != nil {
		return nil, err
	}
	view := &StorageUsageView{
		MeasuredAt: now, BreakdownBy: breakdownBy,
		DatabaseSizeBytes: snapshot.DatabaseSizeBytes, FileSizeBytes: snapshot.FileSizeBytes,
		TotalSizeBytes: snapshot.DatabaseSizeBytes + snapshot.FileSizeBytes,
	}
	switch breakdownBy {
	case StorageBreakdownArea:
		for _, area := range snapshot.Areas {
			view.UsageRows = append(view.UsageRows, StorageUsageRow{
				RowKey: string(area.Area), RowLabel: string(area.Area), RowSizeBytes: area.SizeBytes, RowTableCount: area.TableCount, RowFileSizes: []StorageKeySize{},
			})
		}
	case StorageBreakdownKind:
		for _, kind := range sortedFileKinds(snapshot.FileKinds) {
			view.UsageRows = append(view.UsageRows, StorageUsageRow{
				RowKey: kind.FileKind, RowLabel: kind.FileKind, RowSizeBytes: kind.SizeBytes, RowFileSizes: []StorageKeySize{},
			})
		}
	case StorageBreakdownPerson:
		names, err := tx.ListStorageUserNames()
		if err != nil {
			return nil, err
		}
		for _, person := range snapshot.People {
			row := StorageUsageRow{RowKey: person.UserID, RowSizeBytes: person.SizeBytes, RowFileSizes: []StorageKeySize{}}
			if found, isKnown := names[person.UserID]; isKnown {
				row.RowLabel = found[0]
				if row.RowLabel == "" {
					row.RowLabel = found[1]
				}
			}
			for _, kind := range person.FileKinds {
				row.RowFileSizes = append(row.RowFileSizes, StorageKeySize{SizeKey: kind.FileKind, SizeBytes: kind.SizeBytes})
			}
			view.UsageRows = append(view.UsageRows, row)
		}
	}
	if view.UsageRows == nil {
		view.UsageRows = []StorageUsageRow{}
	}

	configuration := self.config.Current()
	isFilesCounted := !storageusage.IsObjectStoreOnly(configuration)

	since := now.AddDate(0, 0, -storageusage.ProjectionWindowDays)
	if arguments.Since != nil {
		since = *arguments.Since
	}
	var until time.Time
	if arguments.Until != nil {
		until = *arguments.Until
	}
	samples, err := tx.ListStorageUsageSamples(since, until)
	if err != nil {
		return nil, err
	}
	view.HistoryDays = storageHistoryDays(samples, breakdownBy)

	// The line is always the last thirty days, whatever range is drawn.
	recent, err := tx.ListStorageUsageSamples(now.AddDate(0, 0, -storageusage.ProjectionWindowDays), now)
	if err != nil {
		return nil, err
	}
	growth := storageusage.Fit(storageusage.DayTotals(recent, isFilesCounted), now)
	view.Growth = StorageGrowthView{
		GrowthSampleCount: growth.SampleCount, IsGrowthProjectable: growth.IsProjectable, GrowthBytesPerDay: growth.GrowthBytesPerDay,
		CurrentSizeBytes: growth.CurrentSizeBytes, ProjectedSize30DaysBytes: growth.In30DaysBytes,
		ProjectedSize90DaysBytes: growth.In90DaysBytes, ProjectedSize365DaysBytes: growth.In365DaysBytes,
		AreFilesCounted: isFilesCounted,
	}
	view.Disks = []StorageDiskView{}
	for _, disk := range storageusage.Disks(configuration) {
		diskView := StorageDiskView{DiskLabel: disk.DiskLabel, IsDiskMeasured: disk.IsMeasured}
		if disk.IsMeasured {
			totalBytes, freeBytes, usedBytes := disk.DiskTotalBytes, disk.DiskFreeBytes, disk.DiskUsedBytes()
			diskView.DiskTotalBytes, diskView.DiskFreeBytes, diskView.DiskUsedBytes = &totalBytes, &freeBytes, &usedBytes
			diskGrowth := storageusage.Fit(storageusage.DiskDayTotals(recent, disk.DiskLabel), now)
			if fullOn, daysUntilFull := diskGrowth.FullOn(disk.DiskFreeBytes, true, now); fullOn != nil {
				fullOnText := fullOn.Format(storageDateLayout)
				diskView.ProjectedFullOn, diskView.DaysUntilFull = &fullOnText, &daysUntilFull
				diskView.IsProjectedFull = storageusage.WarningDaysFor(daysUntilFull) > 0
			}
		}
		view.Disks = append(view.Disks, diskView)
	}
	return view, nil
}

// sortedFileKinds is the kinds of file, largest first.
func sortedFileKinds(kinds []storageusage.FileKindSize) []storageusage.FileKindSize {
	sorted := slices.Clone(kinds)
	sort.SliceStable(sorted, func(left, right int) bool { return sorted[left].SizeBytes > sorted[right].SizeBytes })
	return sorted
}

// storageHistoryDays folds the samples into one entry a day, with the
// breakdown the caller asked for beside the totals.
func storageHistoryDays(samples []models.StorageUsageSample, breakdownBy string) []StorageHistoryDay {
	days := []StorageHistoryDay{}
	indexByDay := map[string]int{}
	for _, sample := range samples {
		dayText := sample.SampledOn.UTC().Format(storageDateLayout)
		index, isPresent := indexByDay[dayText]
		if !isPresent {
			index = len(days)
			indexByDay[dayText] = index
			days = append(days, StorageHistoryDay{SampledOn: dayText, BreakdownSizes: []StorageKeySize{}})
		}
		day := &days[index]
		switch sample.MeasureKind {
		case models.StorageMeasureDatabase:
			day.DatabaseSizeBytes += sample.MeasuredBytes
		case models.StorageMeasureFileKind:
			day.FileSizeBytes += sample.MeasuredBytes
			if breakdownBy == StorageBreakdownKind {
				day.BreakdownSizes = append(day.BreakdownSizes, StorageKeySize{SizeKey: sample.MeasureKey, SizeBytes: sample.MeasuredBytes})
			}
		case models.StorageMeasureDatabaseArea:
			if breakdownBy == StorageBreakdownArea {
				day.BreakdownSizes = append(day.BreakdownSizes, StorageKeySize{SizeKey: sample.MeasureKey, SizeBytes: sample.MeasuredBytes})
			}
		}
	}
	for index := range days {
		days[index].TotalSizeBytes = days[index].DatabaseSizeBytes + days[index].FileSizeBytes
	}
	return days
}
