package storageusage

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// SampleRetention is how long the daily samples are kept: a year of trend,
// and a margin for a year view that starts a few weeks back.
const SampleRetention = 400 * 24 * time.Hour

// Samples is what a day's measurement writes: the database and each area,
// each kind of file, and each measured disk.
func Samples(snapshot *Snapshot, disks []Disk, now time.Time) []models.StorageUsageSample {
	day := now.UTC().Truncate(24 * time.Hour)
	samples := []models.StorageUsageSample{{
		SampledOn: day, MeasureKind: models.StorageMeasureDatabase, MeasureKey: models.StorageDatabaseTotalKey,
		MeasuredBytes: snapshot.DatabaseSizeBytes, SampledAt: now,
	}}
	for _, area := range snapshot.Areas {
		samples = append(samples, models.StorageUsageSample{
			SampledOn: day, MeasureKind: models.StorageMeasureDatabaseArea, MeasureKey: string(area.Area),
			MeasuredBytes: area.SizeBytes, SampledAt: now,
		})
	}
	for _, fileKind := range snapshot.FileKinds {
		samples = append(samples, models.StorageUsageSample{
			SampledOn: day, MeasureKind: models.StorageMeasureFileKind, MeasureKey: fileKind.FileKind,
			MeasuredBytes: fileKind.SizeBytes, SampledAt: now,
		})
	}
	for _, disk := range disks {
		if !disk.IsMeasured {
			continue
		}
		totalBytes, freeBytes := disk.DiskTotalBytes, disk.DiskFreeBytes
		samples = append(samples, models.StorageUsageSample{
			SampledOn: day, MeasureKind: models.StorageMeasureDisk, MeasureKey: disk.DiskLabel,
			MeasuredBytes: disk.DiskUsedBytes(), DiskTotalBytes: &totalBytes, DiskFreeBytes: &freeBytes, SampledAt: now,
		})
	}
	return samples
}

// Warning is a warning to send about a disk.
type Warning struct {
	DiskLabel     string
	WarningDays   int
	DaysUntilFull float64
	FullOn        time.Time

	// Growth is the disk's own, the line its fill date comes from.
	Growth Growth
}

// DueWarnings says which warnings to send now and which disks have recovered.
// Each disk's fill date comes from a line through its own samples. A disk
// projected to fill within thirty days is warned about once, and again when
// it comes within seven; previous is the warning last sent for each disk. A
// disk no longer projected to fill within thirty days is in recovered, so
// that its next warning is sent as a first.
func DueWarnings(samples []models.StorageUsageSample, disks []Disk, previous map[string]*models.StorageDiskWarning, now time.Time) (due []Warning, recovered []string) {
	for _, disk := range disks {
		if !disk.IsMeasured {
			continue
		}
		growth := Fit(DiskDayTotals(samples, disk.DiskLabel), now)
		fullOn, daysUntilFull := growth.FullOn(disk.DiskFreeBytes, disk.IsMeasured, now)
		warningDays := 0
		if fullOn != nil {
			warningDays = WarningDaysFor(daysUntilFull)
		}
		sent := previous[disk.DiskLabel]
		switch {
		case warningDays == 0:
			if sent != nil {
				recovered = append(recovered, disk.DiskLabel)
			}
		case sent == nil || warningDays < sent.WarningDays:
			due = append(due, Warning{DiskLabel: disk.DiskLabel, WarningDays: warningDays, DaysUntilFull: daysUntilFull, FullOn: *fullOn, Growth: growth})
		}
	}
	return due, recovered
}
