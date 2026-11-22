package apigraph

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The operator sees what the database and the files hold, by area, by kind
// and by person, with the trend and the line fitted through it; nobody else
// does. The files are the sums over the tables that describe them, so the
// test stores rows of known sizes and checks each sum, and that mail filed in
// two folders of one mailbox is one message's bytes, not two.
func TestOnlyTheOperatorSeesStorageAndItAddsUpTheFiles(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	const mailSizeBytes, previewSizeBytes, originalSizeBytes = 1000, 200, 5000
	var owner, other *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "storage-owner", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if other, err = tx.CreateUser(&models.User{Username: "storage-other", Name: "Bob Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
	})
	dbtest.Exec(test, database, `INSERT INTO "photo" ("id", "user_id", "created_at", "updated_at", "client_photo_id", "photo_kind", "photo_source", "photo_processing_state")
		VALUES ('storage-photo', '`+owner.ID+`', now(), now(), 'client-one', 'photo', 'phone', 'ready')`)
	dbtest.Exec(test, database, `INSERT INTO "photo_blob" ("blob_sha256", "blob_size_bytes", "created_at") VALUES ('sha-one', 5000, now())`)
	dbtest.Exec(test, database, `INSERT INTO "photo_file" ("photo_id", "photo_file_role", "blob_sha256", "file_size_bytes", "created_at") VALUES ('storage-photo', 'original', 'sha-one', 5000, now())`)
	dbtest.Exec(test, database, `INSERT INTO "photo_preview" ("photo_id", "photo_preview_size", "content_type", "preview_size_bytes", "created_at") VALUES ('storage-photo', 'small', 'image/jpeg', 200, now())`)

	dbtest.Exec(test, database, `INSERT INTO "mail" ("id", "size") VALUES ('storage-mail', 1000)`)
	dbtest.Exec(test, database, `INSERT INTO "mailbox" ("id", "user_id", "name", "created_at", "modified_at") VALUES ('storage-mailbox', '`+owner.ID+`', 'Personal', now(), now())`)
	for _, folder := range []string{"inbox", "archive"} {
		dbtest.Exec(test, database, `INSERT INTO "mailbox_folder" ("id", "created_at", "modified_at", "mailbox_id", "name", "kind", "uid_validity") VALUES ('storage-folder-`+folder+`', now(), now(), 'storage-mailbox', '`+folder+`', '`+folder+`', 1)`)
		dbtest.Exec(test, database, `INSERT INTO "mailbox_item" ("id", "folder_id", "mail_id", "uid", "modseq", "added_at") VALUES ('storage-item-`+folder+`', 'storage-folder-`+folder+`', 'storage-mail', 1, 1, now())`)
	}
	ownerContext := func(principal *api.Principal, transaction db.Transaction) context.Context {
		return api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), transaction)
	}
	operator := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionServerManage}})}
	person := &api.Principal{User: other, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	resolver := &graph{database: database, config: config.NewMemoryStore(&config.Configuration{}), settings: &api.Settings{}}

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := resolver.StorageUsage(ownerContext(person, transaction), StorageUsageArguments{}); err == nil {
			test.Error("a person without server:manage saw the storage")
		}
	})

	byKind, byPerson, byArea := "kind", "person", "area"
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := ownerContext(operator, transaction)
		view, err := resolver.StorageUsage(ctx, StorageUsageArguments{By: &byKind})
		if err != nil {
			test.Fatalf("StorageUsage by kind: %s", err)
		}
		sizes := map[string]int64{}
		for _, row := range view.UsageRows {
			sizes[row.RowKey] = row.RowSizeBytes
		}
		if sizes[models.StorageFileKindMail] != mailSizeBytes || sizes[models.StorageFileKindPhotoOriginal] != originalSizeBytes || sizes[models.StorageFileKindPhotoPreview] != previewSizeBytes || len(sizes) != len(models.StorageFileKinds) {
			test.Errorf("by kind = %v", sizes)
		}
		if view.FileSizeBytes != mailSizeBytes+originalSizeBytes+previewSizeBytes || view.TotalSizeBytes != view.DatabaseSizeBytes+view.FileSizeBytes || view.DatabaseSizeBytes <= 0 {
			test.Errorf("totals: database %d files %d total %d", view.DatabaseSizeBytes, view.FileSizeBytes, view.TotalSizeBytes)
		}

		view, err = resolver.StorageUsage(ctx, StorageUsageArguments{By: &byPerson})
		if err != nil {
			test.Fatalf("StorageUsage by person: %s", err)
		}
		if len(view.UsageRows) != 1 || view.UsageRows[0].RowKey != owner.ID || view.UsageRows[0].RowLabel != "Alice Example" || view.UsageRows[0].RowSizeBytes != mailSizeBytes+originalSizeBytes+previewSizeBytes {
			test.Errorf("by person = %+v", view.UsageRows)
		}

		view, err = resolver.StorageUsage(ctx, StorageUsageArguments{By: &byArea})
		if err != nil {
			test.Fatalf("StorageUsage by area: %s", err)
		}
		var areaBytes int64
		for _, row := range view.UsageRows {
			areaBytes += row.RowSizeBytes
		}
		if areaBytes != view.DatabaseSizeBytes {
			test.Errorf("the areas add up to %d, the database is %d", areaBytes, view.DatabaseSizeBytes)
		}

		wrong := "nonsense"
		if _, err := resolver.StorageUsage(ctx, StorageUsageArguments{By: &wrong}); err == nil {
			test.Error("an unknown breakdown was accepted")
		}
	})
}

// The trend is the samples of the range, one entry a day, and the line fitted
// through the last thirty days gives the day a measured disk fills. The
// samples are written for the days before today so that the fit is through
// steady growth of one gigabyte a day.
func TestTheTrendIsTheDailySamplesAndTheLineIsFittedThroughThem(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	owner := dbtest.CreateUser(test, database, "storage-trend")
	today := time.Now().UTC().Truncate(24 * time.Hour)
	const gigabyte = int64(1) << 30
	var samples []models.StorageUsageSample
	for daysAgo := 9; daysAgo >= 0; daysAgo-- {
		day := today.AddDate(0, 0, -daysAgo)
		samples = append(samples,
			models.StorageUsageSample{SampledOn: day, MeasureKind: models.StorageMeasureDatabase, MeasureKey: "total", MeasuredBytes: (100 - int64(daysAgo)) * gigabyte, SampledAt: day},
			models.StorageUsageSample{SampledOn: day, MeasureKind: models.StorageMeasureDatabaseArea, MeasureKey: "mail", MeasuredBytes: gigabyte, SampledAt: day},
		)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.InsertStorageUsageSamples(samples); err != nil {
			test.Fatal(err)
		}
	})
	operator := &api.Principal{User: &models.User{ID: owner}, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionServerManage}})}
	// Files are in an object store, so only the database is on a disk the
	// server could measure.
	configuration := &config.Configuration{}
	configuration.Storage.S3.Enabled = true
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{}}

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), operator), transaction)
		view, err := resolver.StorageUsage(ctx, StorageUsageArguments{})
		if err != nil {
			test.Fatalf("StorageUsage: %s", err)
		}
		if len(view.HistoryDays) != 10 || view.HistoryDays[9].SampledOn != today.Format("2006-01-02") {
			test.Fatalf("history = %+v", view.HistoryDays)
		}
		if len(view.HistoryDays[0].BreakdownSizes) != 1 || view.HistoryDays[0].BreakdownSizes[0].SizeKey != "mail" {
			test.Errorf("the first day's breakdown = %+v", view.HistoryDays[0].BreakdownSizes)
		}
		growth := view.Growth
		if !growth.IsGrowthProjectable || growth.GrowthSampleCount != 10 || growth.AreFilesCounted {
			test.Fatalf("growth = %+v", growth)
		}
		if delta := growth.GrowthBytesPerDay - float64(gigabyte); delta > 1 || delta < -1 {
			test.Errorf("GrowthBytesPerDay = %f, want one gigabyte", growth.GrowthBytesPerDay)
		}
		if growth.ProjectedSize30DaysBytes != 130*gigabyte {
			test.Errorf("ProjectedSize30DaysBytes = %d, want 130 gigabytes", growth.ProjectedSize30DaysBytes)
		}
		// With no data directory configured nothing is measured, and an
		// object store is said not to be, rather than guessed at.
		for _, disk := range view.Disks {
			if disk.IsDiskMeasured || disk.DiskTotalBytes != nil || disk.ProjectedFullOn != nil || disk.IsProjectedFull {
				test.Errorf("disk %+v was given sizes or a fill date though it is not measured", disk)
			}
		}
	})
}
