package client

import (
	"context"
	"time"
)

// DocumentStorageUsage is what the database and the files hold, for an
// operator.
const DocumentStorageUsage = `query ($since: DateTime, $until: DateTime, $by: String) {
	StorageUsage(since: $since, until: $until, by: $by) {
		measuredAt breakdownBy databaseSizeBytes fileSizeBytes totalSizeBytes
		usageRows { rowKey rowLabel rowSizeBytes rowTableCount rowFileSizes { sizeKey sizeBytes } }
		historyDays { sampledOn databaseSizeBytes fileSizeBytes totalSizeBytes breakdownSizes { sizeKey sizeBytes } }
		disks { diskLabel isDiskMeasured diskTotalBytes diskFreeBytes diskUsedBytes projectedFullOn daysUntilFull isProjectedFull }
		growth { growthSampleCount isGrowthProjectable growthBytesPerDay currentSizeBytes projectedSize30DaysBytes projectedSize90DaysBytes projectedSize365DaysBytes areFilesCounted }
	}
}`

// StorageKeySize is a size under a key.
type StorageKeySize struct {
	SizeKey   string `json:"sizeKey"`
	SizeBytes int64  `json:"sizeBytes"`
}

// StorageUsageRow is one row of the breakdown asked for.
type StorageUsageRow struct {
	RowKey        string           `json:"rowKey"`
	RowLabel      string           `json:"rowLabel"`
	RowSizeBytes  int64            `json:"rowSizeBytes"`
	RowTableCount int              `json:"rowTableCount"`
	RowFileSizes  []StorageKeySize `json:"rowFileSizes"`
}

// StorageHistoryDay is what one day's sample held.
type StorageHistoryDay struct {
	SampledOn         string           `json:"sampledOn"`
	DatabaseSizeBytes int64            `json:"databaseSizeBytes"`
	FileSizeBytes     int64            `json:"fileSizeBytes"`
	TotalSizeBytes    int64            `json:"totalSizeBytes"`
	BreakdownSizes    []StorageKeySize `json:"breakdownSizes"`
}

// StorageDisk is a disk the server writes to.
type StorageDisk struct {
	DiskLabel       string   `json:"diskLabel"`
	IsDiskMeasured  bool     `json:"isDiskMeasured"`
	DiskTotalBytes  *int64   `json:"diskTotalBytes"`
	DiskFreeBytes   *int64   `json:"diskFreeBytes"`
	DiskUsedBytes   *int64   `json:"diskUsedBytes"`
	ProjectedFullOn *string  `json:"projectedFullOn"`
	DaysUntilFull   *float64 `json:"daysUntilFull"`
	IsProjectedFull bool     `json:"isProjectedFull"`
}

// StorageGrowth is the line fitted through the last thirty days.
type StorageGrowth struct {
	GrowthSampleCount         int     `json:"growthSampleCount"`
	IsGrowthProjectable       bool    `json:"isGrowthProjectable"`
	GrowthBytesPerDay         float64 `json:"growthBytesPerDay"`
	CurrentSizeBytes          int64   `json:"currentSizeBytes"`
	ProjectedSize30DaysBytes  int64   `json:"projectedSize30DaysBytes"`
	ProjectedSize90DaysBytes  int64   `json:"projectedSize90DaysBytes"`
	ProjectedSize365DaysBytes int64   `json:"projectedSize365DaysBytes"`
	AreFilesCounted           bool    `json:"areFilesCounted"`
}

// StorageUsage is one answer of the StorageUsage query.
type StorageUsage struct {
	MeasuredAt        time.Time           `json:"measuredAt"`
	BreakdownBy       string              `json:"breakdownBy"`
	DatabaseSizeBytes int64               `json:"databaseSizeBytes"`
	FileSizeBytes     int64               `json:"fileSizeBytes"`
	TotalSizeBytes    int64               `json:"totalSizeBytes"`
	UsageRows         []StorageUsageRow   `json:"usageRows"`
	HistoryDays       []StorageHistoryDay `json:"historyDays"`
	Disks             []StorageDisk       `json:"disks"`
	Growth            StorageGrowth       `json:"growth"`
}

// ReadStorageUsage is what the database and the files hold, broken down by
// area, kind or person, with the trend since a time.
func ReadStorageUsage(ctx context.Context, connection *Client, since *time.Time, by string) (*StorageUsage, error) {
	var result struct {
		StorageUsage *StorageUsage `json:"StorageUsage"`
	}
	variables := map[string]any{}
	if since != nil {
		variables["since"] = since.Format(time.RFC3339)
	}
	if by != "" {
		variables["by"] = by
	}
	if err := connection.Execute(ctx, DocumentStorageUsage, variables, &result); err != nil {
		return nil, err
	}
	return result.StorageUsage, nil
}
