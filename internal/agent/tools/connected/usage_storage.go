package connected

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// storagePageSize is how many rows or days one answer holds; the rest come
// with the offset the answer names.
const storagePageSize = 25

const documentStorage = `query ($since: DateTime, $until: DateTime, $by: String) {
	StorageUsage(since: $since, until: $until, by: $by) {
		measuredAt breakdownBy databaseSizeBytes fileSizeBytes totalSizeBytes
		usageRows { rowKey rowLabel rowSizeBytes rowTableCount rowFileSizes { sizeKey sizeBytes } }
		historyDays { sampledOn databaseSizeBytes fileSizeBytes totalSizeBytes }
		disks { diskLabel isDiskMeasured diskTotalBytes diskFreeBytes diskUsedBytes projectedFullOn daysUntilFull isProjectedFull }
		growth { growthSampleCount isGrowthProjectable growthBytesPerDay currentSizeBytes projectedSize30DaysBytes projectedSize90DaysBytes projectedSize365DaysBytes areFilesCounted }
	}
}`

type storageAnswer struct {
	StorageUsage *storageUsage `json:"StorageUsage"`
}

type storageUsageRow struct {
	RowKey        string `json:"rowKey"`
	RowLabel      string `json:"rowLabel"`
	RowSizeBytes  int64  `json:"rowSizeBytes"`
	RowTableCount int    `json:"rowTableCount"`
	RowFileSizes  []struct {
		SizeKey   string `json:"sizeKey"`
		SizeBytes int64  `json:"sizeBytes"`
	} `json:"rowFileSizes"`
}

type storageDisk struct {
	DiskLabel       string   `json:"diskLabel"`
	IsDiskMeasured  bool     `json:"isDiskMeasured"`
	DiskTotalBytes  *int64   `json:"diskTotalBytes"`
	DiskFreeBytes   *int64   `json:"diskFreeBytes"`
	DiskUsedBytes   *int64   `json:"diskUsedBytes"`
	ProjectedFullOn *string  `json:"projectedFullOn"`
	DaysUntilFull   *float64 `json:"daysUntilFull"`
	IsProjectedFull bool     `json:"isProjectedFull"`
}

type storageUsage struct {
	BreakdownBy       string            `json:"breakdownBy"`
	DatabaseSizeBytes int64             `json:"databaseSizeBytes"`
	FileSizeBytes     int64             `json:"fileSizeBytes"`
	TotalSizeBytes    int64             `json:"totalSizeBytes"`
	UsageRows         []storageUsageRow `json:"usageRows"`
	HistoryDays       []struct {
		SampledOn         string `json:"sampledOn"`
		DatabaseSizeBytes int64  `json:"databaseSizeBytes"`
		FileSizeBytes     int64  `json:"fileSizeBytes"`
		TotalSizeBytes    int64  `json:"totalSizeBytes"`
	} `json:"historyDays"`
	Disks  []storageDisk `json:"disks"`
	Growth struct {
		GrowthSampleCount         int     `json:"growthSampleCount"`
		IsGrowthProjectable       bool    `json:"isGrowthProjectable"`
		GrowthBytesPerDay         float64 `json:"growthBytesPerDay"`
		CurrentSizeBytes          int64   `json:"currentSizeBytes"`
		ProjectedSize30DaysBytes  int64   `json:"projectedSize30DaysBytes"`
		ProjectedSize90DaysBytes  int64   `json:"projectedSize90DaysBytes"`
		ProjectedSize365DaysBytes int64   `json:"projectedSize365DaysBytes"`
		AreFilesCounted           bool    `json:"areFilesCounted"`
	} `json:"growth"`
}

// runStorageUsage answers for whose "storage": what the database and the
// files hold. The first page also carries the totals, the disks and the
// projection; part "history" lists the daily samples instead. Both page with
// offset and next_offset.
func runStorageUsage(ctx context.Context, arguments usageRequest, variables map[string]any) (*tools.Result, error) {
	run := tools.MustRun(ctx)
	by := strings.TrimSpace(arguments.By)
	if by != "" && by != "area" && by != "kind" && by != "person" {
		return nil, fmt.Errorf("by for storage is area, kind or person")
	}
	if by == "" {
		variables["by"] = "area"
	}
	var answer storageAnswer
	if err := run.Operations().Execute(ctx, documentStorage, variables, &answer); err != nil {
		return nil, err
	}
	if answer.StorageUsage == nil {
		return nil, fmt.Errorf("the server did not answer with its storage")
	}
	return tools.JSONResult(describeStorage(answer.StorageUsage, strings.EqualFold(strings.TrimSpace(arguments.Part), "history"), arguments.Offset))
}

func describeStorageBytes(sizeBytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(sizeBytes)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index < 2 {
		return fmt.Sprintf("%.0f %s", size, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

// describeStorage puts the answer in words the model can pass on: each size
// written out beside its bytes, the disks that fill soon first.
func describeStorage(usage *storageUsage, isHistory bool, offset int) map[string]any {
	described := map[string]any{"whose": "the whole server"}
	if offset < 0 {
		offset = 0
	}
	page := func(total int) (int, int) {
		start := min(offset, total)
		return start, min(start+storagePageSize, total)
	}
	if isHistory {
		start, end := page(len(usage.HistoryDays))
		days := []map[string]any{}
		for _, day := range usage.HistoryDays[start:end] {
			days = append(days, map[string]any{
				"day": day.SampledOn, "database": describeStorageBytes(day.DatabaseSizeBytes), "files": describeStorageBytes(day.FileSizeBytes),
				"total": describeStorageBytes(day.TotalSizeBytes), "total_bytes": day.TotalSizeBytes,
			})
		}
		described["history"] = days
		if end < len(usage.HistoryDays) {
			described["next_offset"] = end
		}
		return described
	}

	start, end := page(len(usage.UsageRows))
	rows := []map[string]any{}
	for _, row := range usage.UsageRows[start:end] {
		entry := map[string]any{"size": describeStorageBytes(row.RowSizeBytes), "size_bytes": row.RowSizeBytes}
		switch usage.BreakdownBy {
		case "person":
			if row.RowKey == "" {
				entry["person"] = "files no account owns"
			} else {
				entry["person"] = row.RowLabel
				entry["person_id"] = row.RowKey
			}
			files := map[string]string{}
			for _, kind := range row.RowFileSizes {
				if kind.SizeBytes > 0 {
					files[kind.SizeKey] = describeStorageBytes(kind.SizeBytes)
				}
			}
			entry["files"] = files
		case "kind":
			entry["file_kind"] = row.RowKey
		default:
			entry["area"] = row.RowKey
			entry["tables"] = row.RowTableCount
		}
		rows = append(rows, entry)
	}
	described["by"] = usage.BreakdownBy
	described["rows"] = rows
	if end < len(usage.UsageRows) {
		described["next_offset"] = end
	}
	if offset > 0 {
		return described
	}

	described["database"] = describeStorageBytes(usage.DatabaseSizeBytes)
	described["files"] = describeStorageBytes(usage.FileSizeBytes)
	described["total"] = describeStorageBytes(usage.TotalSizeBytes)
	described["files_note"] = "what the rows say the blob store holds; a store that moves old files elsewhere can hold less on its own disk"
	disks := []map[string]any{}
	for _, disk := range usage.Disks {
		entry := map[string]any{"disk": disk.DiskLabel}
		if !disk.IsDiskMeasured || disk.DiskTotalBytes == nil || disk.DiskFreeBytes == nil {
			entry["measured"] = false
			disks = append(disks, entry)
			continue
		}
		entry["measured"] = true
		entry["total"] = describeStorageBytes(*disk.DiskTotalBytes)
		entry["free"] = describeStorageBytes(*disk.DiskFreeBytes)
		if disk.ProjectedFullOn != nil {
			entry["projected_full_on"] = *disk.ProjectedFullOn
		}
		if disk.IsProjectedFull {
			entry["warning"] = "projected to fill within 30 days"
		}
		disks = append(disks, entry)
	}
	described["disks"] = disks
	growth := map[string]any{"samples": usage.Growth.GrowthSampleCount}
	if usage.Growth.IsGrowthProjectable {
		growth["per_day"] = describeStorageBytes(int64(usage.Growth.GrowthBytesPerDay))
		growth["in_30_days"] = describeStorageBytes(usage.Growth.ProjectedSize30DaysBytes)
		growth["in_90_days"] = describeStorageBytes(usage.Growth.ProjectedSize90DaysBytes)
		growth["in_365_days"] = describeStorageBytes(usage.Growth.ProjectedSize365DaysBytes)
		if !usage.Growth.AreFilesCounted {
			growth["note"] = "the files are in an object store and are not counted"
		}
	} else {
		growth["note"] = "fewer than three daily samples, so no projection yet"
	}
	described["growth"] = growth
	return described
}
