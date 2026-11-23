package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storageusage"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

const (
	// storageSampleEvery is how often a tick looks for today's sample. The
	// sample is one a UTC day, so this only decides how soon after midnight
	// the new day's is taken, and how soon a server that was down at
	// midnight takes it.
	storageSampleEvery = time.Hour

	// storageSampleLockKey is the advisory lock a sample holds for its
	// transaction, so two servers sharing a database do not both measure and
	// both warn the operators. An arbitrary constant; "storageu" in ASCII.
	storageSampleLockKey int64 = 0x73746f7261676575
)

// sampleStorageInBackground takes the day's storage sample without holding
// the tick: sizing the database is a few catalog reads, but they are the
// database's. One at a time; a tick that finds the last still going leaves
// it. It runs whether or not the agent is on, because the page and the
// warning are the operator's and do not need a model.
func (self *Agent) sampleStorageInBackground(ctx context.Context, now time.Time) {
	if now.Sub(self.lastStorageSample) < storageSampleEvery || !self.samplingStorage.CompareAndSwap(false, true) {
		return
	}
	self.lastStorageSample = now
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		defer self.samplingStorage.Store(false)
		if err := self.SampleStorage(ctx, now); err != nil && ctx.Err() == nil {
			log.Warningf("cannot sample the server's storage: %s", err)
		}
	}()
}

// SampleStorage writes today's sample unless one is there, from this
// instance or another, then warns the operators of a disk that is projected
// to fill. A server that has never sampled takes its first sample here, so
// the page is not empty after an upgrade.
func (self *Agent) SampleStorage(ctx context.Context, now time.Time) error {
	configuration := self.settings.Configuration()
	disks := storageusage.Disks(configuration)
	var due []storageusage.Warning
	err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(storageSampleLockKey)
		if err != nil {
			return err
		}
		if !isLocked {
			log.Debugf("another server is sampling the storage; this sample is dropped")
			return nil
		}
		hasSample, err := tx.HasStorageUsageSamples(now)
		if err != nil {
			return err
		}
		if !hasSample {
			snapshot, err := storageusage.Measure(tx, false)
			if err != nil {
				return err
			}
			if _, err := tx.InsertStorageUsageSamples(storageusage.Samples(snapshot, disks, now)); err != nil {
				return err
			}
			if err := tx.DeleteStorageUsageSamplesBefore(now.Add(-storageusage.SampleRetention)); err != nil {
				return err
			}
		}
		samples, err := tx.ListStorageUsageSamples(now.AddDate(0, 0, -storageusage.ProjectionWindowDays), now)
		if err != nil {
			return err
		}
		previous := map[string]*models.StorageDiskWarning{}
		for _, disk := range disks {
			warning, err := tx.GetStorageDiskWarning(disk.DiskLabel)
			if err != nil {
				return err
			}
			if warning != nil {
				previous[disk.DiskLabel] = warning
			}
		}
		var recovered []string
		due, recovered = storageusage.DueWarnings(samples, disks, previous, now)
		for _, diskLabel := range recovered {
			if err := tx.DeleteStorageDiskWarning(diskLabel); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, warning := range due {
		self.tellOperatorsOfFullDisk(ctx, warning, disks)
	}
	return nil
}

// storageWarningSummary is what the operators are told about a disk.
func storageWarningSummary(warning storageusage.Warning, disk storageusage.Disk) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "The %s disk is projected to fill in about %d days, on %s.",
		storageDiskWords(warning.DiskLabel), int(warning.DaysUntilFull+0.5), warning.FullOn.Format("2006-01-02"))
	fmt.Fprintf(&builder, "\n\nIt has %s free of %s. What is on it has been growing by %s a day over the last %d daily samples.",
		storageusage.DescribeBytes(disk.DiskFreeBytes), storageusage.DescribeBytes(disk.DiskTotalBytes),
		storageusage.DescribeBytes(int64(warning.Growth.GrowthBytesPerDay)), warning.Growth.SampleCount)
	builder.WriteString("\n\nThe Disk usage tab of the Server page, and `teanode server storage`, show what is taking the room.")
	return builder.String()
}

func storageDiskWords(diskLabel string) string {
	switch diskLabel {
	case storageusage.DiskDataDirectory:
		return "data directory"
	case storageusage.DiskFileDirectory:
		return "file storage"
	default:
		return diskLabel
	}
}

// tellOperatorsOfFullDisk says it to everyone who holds server:manage and has
// an agent, in their main conversation, as the daily price check does. The
// warning is remembered only once somebody was told, or when there is nobody
// to tell, so a turn that was running in every conversation does not lose
// it: the next sample says it again.
func (self *Agent) tellOperatorsOfFullDisk(ctx context.Context, warning storageusage.Warning, disks []storageusage.Disk) {
	var disk storageusage.Disk
	for _, candidate := range disks {
		if candidate.DiskLabel == warning.DiskLabel {
			disk = candidate
		}
	}
	var agentIds []string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		var err error
		agentIds, err = operatorAgentIds(tx)
		return err
	}); err != nil {
		log.Warningf("cannot find the operators to warn of the %s disk: %s", warning.DiskLabel, err)
		return
	}
	summary := storageWarningSummary(warning, disk)
	checkIn := models.AlertMarker + " The server projects that one of its disks will fill soon; you told the operator. This is not the person speaking. What you said follows."
	isTold := len(agentIds) == 0
	for _, agentId := range agentIds {
		if err := self.tellAgent(ctx, self.settings.Database, agentId, checkIn, summary); err != nil {
			if errors.Is(err, errTurnRunning) {
				log.Warningf("the %s disk warning was not told to agent %s: a turn was running in its conversation", warning.DiskLabel, agentId)
				continue
			}
			log.Warningf("cannot tell agent %s of the %s disk warning: %s", agentId, warning.DiskLabel, err)
			continue
		}
		isTold = true
	}
	if !isTold {
		return
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.PutStorageDiskWarning(&models.StorageDiskWarning{
			DiskLabel: warning.DiskLabel, WarningDays: warning.WarningDays, ProjectedFullOn: warning.FullOn, SentAt: time.Now(),
		})
	}); err != nil {
		log.Warningf("cannot remember the %s disk warning: %s", warning.DiskLabel, err)
	}
}
