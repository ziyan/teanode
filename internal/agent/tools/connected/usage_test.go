package connected

import (
	"testing"
)

// The bill is told beside the estimate only for a row it was read for.
func TestDescribeRowsShowsWhatWasBilled(test *testing.T) {
	billed := 4.5
	rows := describeRows([]*usageRow{
		{Key: "2026-03-01", Cost: 4, Currency: "USD", BilledAmount: &billed, BilledCurrencyCode: "usd"},
		{Key: "2026-03-02", Cost: 1, Currency: "USD"},
	})
	if rows[0]["billed"] != "4.50" || rows[0]["billed_currency"] != "USD" || rows[0]["cost"] != "4.00" {
		test.Errorf("billed row described as %v", rows[0])
	}
	if _, hasBilled := rows[1]["billed"]; hasBilled {
		test.Errorf("a row with no bill was described as billed: %v", rows[1])
	}
}

const storageGigabyte = int64(1) << 30

// The first page carries the totals, the disks and the projection, the next
// pages only rows, and a disk that is not measured is said not to be rather
// than described with sizes it does not have.
func TestDescribeStoragePagesItsRowsAndSaysWhatIsNotMeasured(test *testing.T) {
	usage := &storageUsage{BreakdownBy: "person", DatabaseSizeBytes: 3 * storageGigabyte, FileSizeBytes: 7 * storageGigabyte, TotalSizeBytes: 10 * storageGigabyte}
	for index := 0; index < storagePageSize+5; index++ {
		usage.UsageRows = append(usage.UsageRows, storageUsageRow{RowKey: "person-id", RowLabel: "Alice Example", RowSizeBytes: storageGigabyte})
	}
	fullOn := "2030-04-20"
	total, free := 500*storageGigabyte, 10*storageGigabyte
	usage.Disks = append(usage.Disks,
		storageDisk{DiskLabel: "dataDirectory", IsDiskMeasured: true, DiskTotalBytes: &total, DiskFreeBytes: &free, ProjectedFullOn: &fullOn, IsProjectedFull: true},
		storageDisk{DiskLabel: "objectStore"},
	)

	first := describeStorage(usage, false, 0)
	if len(first["rows"].([]map[string]any)) != storagePageSize || first["next_offset"] != storagePageSize {
		test.Fatalf("first page: %d rows, next_offset %v", len(first["rows"].([]map[string]any)), first["next_offset"])
	}
	if first["total"] != "10.0 GB" || first["growth"].(map[string]any)["note"] == nil {
		test.Errorf("first page totals = %v, growth = %v", first["total"], first["growth"])
	}
	disks := first["disks"].([]map[string]any)
	if disks[0]["warning"] == nil || disks[0]["projected_full_on"] != fullOn || disks[1]["measured"] != false || disks[1]["total"] != nil {
		test.Errorf("disks = %v", disks)
	}

	second := describeStorage(usage, false, storagePageSize)
	if len(second["rows"].([]map[string]any)) != 5 || second["next_offset"] != nil || second["total"] != nil {
		test.Errorf("second page = %v", second)
	}
}
