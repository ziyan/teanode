package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storageusage"
)

type storageFixture struct {
	database        db.Database
	configuration   *config.Configuration
	worker          *agent.Agent
	operatorAgentID string
	memberAgentID   string
}

func newStorageFixture(test *testing.T) *storageFixture {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)

	configuration := config.Default()
	configuration.Server.DataDirectory = test.TempDir()
	store := config.NewMemoryStore(configuration)
	fixture := &storageFixture{database: database, configuration: configuration}
	fixture.worker = agent.New(&agent.Settings{Database: database, Configuration: store.Current, Instance: "test", Tick: time.Hour})

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "operators", Permissions: []models.Permission{models.PermissionServerManage}})
		if err != nil {
			test.Fatalf("CreateRole: %s", err)
		}
		operator, err := tx.CreateUser(&models.User{Username: "storage-operator"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		member, err := tx.CreateUser(&models.User{Username: "storage-member"})
		if err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "operators", UserIDs: []string{operator.ID}, RoleIDs: []string{role.ID}}); err != nil {
			test.Fatalf("CreateGroup: %s", err)
		}
		operatorAgent, err := tx.CreateAgent(&models.Agent{UserID: operator.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		memberAgent, err := tx.CreateAgent(&models.Agent{UserID: member.ID, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		fixture.operatorAgentID, fixture.memberAgentID = operatorAgent.ID, memberAgent.ID
	})
	return fixture
}

func (self *storageFixture) told(test *testing.T, agentID string) []string {
	test.Helper()
	var texts []string
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		conversations, err := tx.ListAgentConversations(agentID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if err != nil {
			test.Fatalf("ListAgentConversations: %s", err)
		}
		for _, conversation := range conversations {
			messages, err := tx.ListAgentMessages(conversation.ID, nil)
			if err != nil {
				test.Fatalf("ListAgentMessages: %s", err)
			}
			for _, message := range messages {
				if message.Role == "assistant" {
					texts = append(texts, message.Content)
				}
			}
		}
	})
	return texts
}

func (self *storageFixture) samples(test *testing.T) []models.StorageUsageSample {
	test.Helper()
	var samples []models.StorageUsageSample
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		var err error
		if samples, err = tx.ListStorageUsageSamples(time.Now().AddDate(-1, 0, 0), time.Time{}); err != nil {
			test.Fatalf("ListStorageUsageSamples: %s", err)
		}
	})
	return samples
}

// growingBy writes a total for today and the days before it that grows by
// bytesPerDay a day, as though the server had been filling its disk.
func (self *storageFixture) growingBy(test *testing.T, days int, bytesPerDay int64) {
	test.Helper()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var samples []models.StorageUsageSample
	for daysAgo := days - 1; daysAgo >= 0; daysAgo-- {
		day := today.AddDate(0, 0, -daysAgo)
		usedBytes, totalBytes, freeBytes := int64(days-daysAgo)*bytesPerDay, int64(1)<<50, int64(1)<<40
		samples = append(samples,
			models.StorageUsageSample{
				SampledOn: day, MeasureKind: models.StorageMeasureDatabase, MeasureKey: models.StorageDatabaseTotalKey,
				MeasuredBytes: usedBytes, SampledAt: day,
			},
			models.StorageUsageSample{
				SampledOn: day, MeasureKind: models.StorageMeasureDisk, MeasureKey: storageusage.DiskDataDirectory,
				MeasuredBytes: usedBytes, DiskTotalBytes: &totalBytes, DiskFreeBytes: &freeBytes, SampledAt: day,
			})
	}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		if _, err := tx.InsertStorageUsageSamples(samples); err != nil {
			test.Fatalf("InsertStorageUsageSamples: %s", err)
		}
	})
}

func freeBytesOf(test *testing.T, configuration *config.Configuration) int64 {
	test.Helper()
	for _, disk := range storageusage.Disks(configuration) {
		if disk.IsMeasured {
			return disk.DiskFreeBytes
		}
	}
	test.Skip("this platform cannot measure a disk")
	return 0
}

// The first sample is taken when none was: the database, each area, each kind
// of file and the disk of the data directory, once for the day however many
// times the job runs.
func TestTheFirstSampleIsTakenOnceADay(test *testing.T) {
	fixture := newStorageFixture(test)
	if err := fixture.worker.SampleStorage(context.Background(), time.Now()); err != nil {
		test.Fatalf("SampleStorage: %s", err)
	}
	first := fixture.samples(test)
	counts := map[models.StorageMeasureKind]int{}
	for _, sample := range first {
		counts[sample.MeasureKind]++
	}
	if counts[models.StorageMeasureDatabase] != 1 || counts[models.StorageMeasureDatabaseArea] != len(storageusage.Areas) || counts[models.StorageMeasureFileKind] != len(models.StorageFileKinds) {
		test.Errorf("samples by kind = %v", counts)
	}
	for _, sample := range first {
		if sample.MeasureKind == models.StorageMeasureDisk && (sample.DiskTotalBytes == nil || sample.DiskFreeBytes == nil || sample.MeasuredBytes != *sample.DiskTotalBytes-*sample.DiskFreeBytes) {
			test.Errorf("disk sample %+v does not say its total and free bytes", sample)
		}
	}
	if err := fixture.worker.SampleStorage(context.Background(), time.Now()); err != nil {
		test.Fatalf("SampleStorage again: %s", err)
	}
	if again := fixture.samples(test); len(again) != len(first) {
		test.Errorf("a second run the same day wrote %d samples more", len(again)-len(first))
	}
}

// A disk projected to fill within thirty days is told to the operators, and
// not to anybody else, once; closer than seven days it is told again; when
// space is freed the warning is forgotten so that the next one is a first.
func TestAFillingDiskIsToldToTheOperatorsOnceAtThirtyDaysAndOnceAtSeven(test *testing.T) {
	fixture := newStorageFixture(test)
	freeBytes := freeBytesOf(test, fixture.configuration)
	ctx := context.Background()

	// Twenty days of room at this rate.
	fixture.growingBy(test, 5, freeBytes/20)
	for run := 0; run < 2; run++ {
		if err := fixture.worker.SampleStorage(ctx, time.Now()); err != nil {
			test.Fatalf("SampleStorage: %s", err)
		}
	}
	told := fixture.told(test, fixture.operatorAgentID)
	if len(told) != 1 || !strings.Contains(told[0], "projected to fill in about") {
		test.Fatalf("the operator was told %q, want one warning", told)
	}
	if others := fixture.told(test, fixture.memberAgentID); len(others) != 0 {
		test.Errorf("a person who is not an operator was told %q", others)
	}

	// Closer: replace the samples with growth that fills it in about five
	// days.
	dbtest.Exec(test, fixture.database, `DELETE FROM "storage_usage_sample"`)
	fixture.growingBy(test, 5, freeBytes/5)
	if err := fixture.worker.SampleStorage(ctx, time.Now()); err != nil {
		test.Fatalf("SampleStorage: %s", err)
	}
	if told := fixture.told(test, fixture.operatorAgentID); len(told) != 2 {
		test.Fatalf("after coming within seven days the operator was told %d times, want 2", len(told))
	}
	if err := fixture.worker.SampleStorage(ctx, time.Now()); err != nil {
		test.Fatalf("SampleStorage: %s", err)
	}
	if told := fixture.told(test, fixture.operatorAgentID); len(told) != 2 {
		test.Errorf("the seven day warning was repeated: %d messages", len(told))
	}

	// Space freed: no longer growing, the warning is forgotten, and when it
	// fills again the operator is told as though for the first time.
	dbtest.Exec(test, fixture.database, `DELETE FROM "storage_usage_sample"`)
	fixture.growingBy(test, 5, 0)
	if err := fixture.worker.SampleStorage(ctx, time.Now()); err != nil {
		test.Fatalf("SampleStorage: %s", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(tx db.Transaction) {
		warning, err := tx.GetStorageDiskWarning(storageusage.DiskDataDirectory)
		if err != nil || warning != nil {
			test.Errorf("the warning was not forgotten: %+v, %v", warning, err)
		}
	})
}
