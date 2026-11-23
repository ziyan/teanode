package db

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// StorageUsageOperation measures what the database and the blob store hold,
// and keeps the daily samples the operator's trend is drawn from.
//
// The measures read sizes the database already keeps (pg_class and the size
// columns of the tables that describe files) and never read the files
// themselves. Per-person database size is not measured: it would count the
// rows of the largest tables one by one.
type StorageUsageOperation interface {
	// MeasureDatabaseSizeBytes is the size of the whole database.
	MeasureDatabaseSizeBytes() (int64, error)

	// MeasureRelationSizes is the size of every table of the public schema
	// with its indexes and out-of-line storage, by table name.
	MeasureRelationSizes() ([]models.StorageRelationSize, error)

	// MeasureFileKindSizes is what the rows say the blob store holds of each
	// kind of file. A kind with nothing stored is 0, so the answer always
	// has every kind of models.StorageFileKinds.
	MeasureFileKindSizes() ([]models.StorageFileKindSize, error)

	// MeasurePersonFileSizes is the same, by owner: the files of a person
	// under their account. Mail is counted for the owner of each mailbox it
	// is filed in, once however many folders hold it.
	MeasurePersonFileSizes() ([]models.StoragePersonFileSize, error)

	// ListStorageUserNames is the display name and username of every
	// account, by id, to name the people a measure is for.
	ListStorageUserNames() (map[string][2]string, error)

	// InsertStorageUsageSamples writes samples, leaving alone one already
	// there for the same day, kind and key. It reports how many it wrote.
	InsertStorageUsageSamples(samples []models.StorageUsageSample) (int64, error)

	// HasStorageUsageSamples says whether anything was sampled on the UTC
	// day the time falls on.
	HasStorageUsageSamples(day time.Time) (bool, error)

	// ListStorageUsageSamples is the samples from the UTC day since falls on
	// up to and including the UTC day until falls on; a zero until has no
	// end. Oldest first.
	ListStorageUsageSamples(since, until time.Time) ([]models.StorageUsageSample, error)

	// DeleteStorageUsageSamplesBefore removes samples before the UTC day the
	// time falls on.
	DeleteStorageUsageSamplesBefore(day time.Time) error

	// GetStorageDiskWarning is the warning last sent about a disk, or nil.
	GetStorageDiskWarning(diskLabel string) (*models.StorageDiskWarning, error)

	// PutStorageDiskWarning records a warning sent, replacing the one before.
	PutStorageDiskWarning(warning *models.StorageDiskWarning) error

	// DeleteStorageDiskWarning forgets the warning about a disk, so the next
	// one is sent as a first.
	DeleteStorageDiskWarning(diskLabel string) error
}

type storageUsageSampleModel struct {
	SampledOn      time.Time `gorm:"column:sampled_on;primaryKey"`
	MeasureKind    string    `gorm:"column:measure_kind;primaryKey"`
	MeasureKey     string    `gorm:"column:measure_key;primaryKey"`
	MeasuredBytes  int64     `gorm:"column:measured_bytes"`
	DiskTotalBytes *int64    `gorm:"column:disk_total_bytes"`
	DiskFreeBytes  *int64    `gorm:"column:disk_free_bytes"`
	SampledAt      time.Time `gorm:"column:sampled_at"`
}

func (storageUsageSampleModel) TableName() string { return "storage_usage_sample" }

type storageDiskWarningModel struct {
	DiskLabel       string    `gorm:"column:disk_label;primaryKey"`
	WarningDays     int       `gorm:"column:warning_days"`
	ProjectedFullOn time.Time `gorm:"column:projected_full_on"`
	SentAt          time.Time `gorm:"column:sent_at"`
}

func (storageDiskWarningModel) TableName() string { return "storage_disk_warning" }

func (self *transaction) MeasureDatabaseSizeBytes() (int64, error) {
	var sizeBytes int64
	if err := self.tx.Raw(`SELECT pg_database_size(current_database())`).Scan(&sizeBytes).Error; err != nil {
		return 0, err
	}
	return sizeBytes, nil
}

func (self *transaction) MeasureRelationSizes() ([]models.StorageRelationSize, error) {
	var rows []struct {
		TableName  string
		TotalBytes int64
	}
	if err := self.tx.Raw(`SELECT relname AS table_name, pg_total_relation_size(oid) AS total_bytes
		FROM pg_class WHERE relkind IN ('r', 'p') AND relnamespace = 'public'::regnamespace ORDER BY relname`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sizes := make([]models.StorageRelationSize, 0, len(rows))
	for _, row := range rows {
		sizes = append(sizes, models.StorageRelationSize{TableName: row.TableName, TotalBytes: row.TotalBytes})
	}
	return sizes, nil
}

// Each file kind is a sum over the column that holds its size. None of the
// tables is read for anything else, so a sum is a scan of a narrow table
// (the largest, the photo blobs, has a row per file and not per photo).
//
// Photo originals are the blobs, which are kept once however many photos
// name them; a photo's previews are rows of their own. A recording's
// original is the media the phone uploaded, counted once it is complete,
// and its derived bytes (audio chunks, speaker samples) are blobs the
// recording quota counts on top of it, so the two do not overlap.
const (
	storageFileKindsQuery = `SELECT 'photoOriginal' AS file_kind, COALESCE(SUM("blob_size_bytes"), 0) AS size_bytes FROM "photo_blob"
		UNION ALL SELECT 'photoPreview', COALESCE(SUM("preview_size_bytes"), 0) FROM "photo_preview"
		UNION ALL SELECT 'recordingOriginal', COALESCE(SUM("original_size_bytes"), 0) FROM "recording_media" WHERE "recording_upload_state" = 'complete'
		UNION ALL SELECT 'recordingDerived', COALESCE(SUM("blob_size_bytes"), 0) FROM "recording_blob"
		UNION ALL SELECT 'mail', COALESCE(SUM("size"), 0) FROM "mail"
		UNION ALL SELECT 'agentAttachment', COALESCE(SUM("size"), 0) FROM "agent_attachment"`

	storagePersonFilesQuery = `SELECT "photo"."user_id" AS user_id, 'photoOriginal' AS file_kind, SUM("photo_file"."file_size_bytes") AS size_bytes
			FROM "photo_file" JOIN "photo" ON "photo"."id" = "photo_file"."photo_id" GROUP BY "photo"."user_id"
		UNION ALL SELECT "photo"."user_id", 'photoPreview', SUM("photo_preview"."preview_size_bytes")
			FROM "photo_preview" JOIN "photo" ON "photo"."id" = "photo_preview"."photo_id" GROUP BY "photo"."user_id"
		UNION ALL SELECT "recording"."user_id", 'recordingOriginal', SUM("recording_media"."original_size_bytes")
			FROM "recording_media" JOIN "recording" ON "recording"."id" = "recording_media"."recording_id"
			WHERE "recording_media"."recording_upload_state" = 'complete' GROUP BY "recording"."user_id"
		UNION ALL SELECT "recording"."user_id", 'recordingDerived', SUM("recording_blob"."blob_size_bytes")
			FROM "recording_blob" JOIN "recording" ON "recording"."id" = "recording_blob"."recording_id" GROUP BY "recording"."user_id"
		UNION ALL SELECT "owned"."user_id", 'mail', SUM("mail"."size")
			FROM (SELECT DISTINCT "mailbox"."user_id" AS user_id, "mailbox_item"."mail_id" AS mail_id
				FROM "mailbox_item"
				JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
				JOIN "mailbox" ON "mailbox"."id" = "mailbox_folder"."mailbox_id") AS "owned"
			JOIN "mail" ON "mail"."id" = "owned"."mail_id" GROUP BY "owned"."user_id"
		UNION ALL SELECT "agent"."user_id", 'agentAttachment', SUM("agent_attachment"."size")
			FROM "agent_attachment" JOIN "agent" ON "agent"."id" = "agent_attachment"."agent_id" GROUP BY "agent"."user_id"`
)

func (self *transaction) MeasureFileKindSizes() ([]models.StorageFileKindSize, error) {
	var rows []struct {
		FileKind  string
		SizeBytes int64
	}
	if err := self.tx.Raw(storageFileKindsQuery).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sizes := make([]models.StorageFileKindSize, 0, len(rows))
	for _, row := range rows {
		sizes = append(sizes, models.StorageFileKindSize{FileKind: row.FileKind, SizeBytes: row.SizeBytes})
	}
	return sizes, nil
}

func (self *transaction) MeasurePersonFileSizes() ([]models.StoragePersonFileSize, error) {
	var rows []struct {
		UserID    string
		FileKind  string
		SizeBytes int64
	}
	if err := self.tx.Raw(storagePersonFilesQuery).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sizes := make([]models.StoragePersonFileSize, 0, len(rows))
	for _, row := range rows {
		sizes = append(sizes, models.StoragePersonFileSize{UserID: row.UserID, FileKind: row.FileKind, SizeBytes: row.SizeBytes})
	}
	return sizes, nil
}

func (self *transaction) ListStorageUserNames() (map[string][2]string, error) {
	var rows []struct {
		ID       string
		Name     string
		Username string
	}
	if err := self.tx.Raw(`SELECT "id", COALESCE("name", '') AS name, "username" FROM "user"`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make(map[string][2]string, len(rows))
	for _, row := range rows {
		names[row.ID] = [2]string{row.Name, row.Username}
	}
	return names, nil
}

func (self *transaction) InsertStorageUsageSamples(samples []models.StorageUsageSample) (int64, error) {
	var written int64
	for _, sample := range samples {
		result := self.tx.Exec(
			`INSERT INTO "storage_usage_sample" ("sampled_on", "measure_kind", "measure_key", "measured_bytes", "disk_total_bytes", "disk_free_bytes", "sampled_at")
			VALUES ((? AT TIME ZONE 'UTC')::date, ?, ?, ?, ?, ?, ?)
			ON CONFLICT ("sampled_on", "measure_kind", "measure_key") DO NOTHING`,
			sample.SampledOn.UTC(), string(sample.MeasureKind), sample.MeasureKey, sample.MeasuredBytes, sample.DiskTotalBytes, sample.DiskFreeBytes, sample.SampledAt.UTC())
		if result.Error != nil {
			return written, result.Error
		}
		written += result.RowsAffected
	}
	return written, nil
}

func (self *transaction) HasStorageUsageSamples(day time.Time) (bool, error) {
	var count int64
	if err := self.tx.Raw(`SELECT count(*) FROM "storage_usage_sample" WHERE "sampled_on" = (? AT TIME ZONE 'UTC')::date`, day.UTC()).Scan(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (self *transaction) ListStorageUsageSamples(since, until time.Time) ([]models.StorageUsageSample, error) {
	query := self.tx.Model(&storageUsageSampleModel{}).Where(`"sampled_on" >= (? AT TIME ZONE 'UTC')::date`, since.UTC())
	if !until.IsZero() {
		query = query.Where(`"sampled_on" <= (? AT TIME ZONE 'UTC')::date`, until.UTC())
	}
	var rows []storageUsageSampleModel
	if err := query.Order(`"sampled_on", "measure_kind", "measure_key"`).Find(&rows).Error; err != nil {
		return nil, err
	}
	samples := make([]models.StorageUsageSample, 0, len(rows))
	for _, row := range rows {
		samples = append(samples, models.StorageUsageSample{
			SampledOn:      time.Date(row.SampledOn.Year(), row.SampledOn.Month(), row.SampledOn.Day(), 0, 0, 0, 0, time.UTC),
			MeasureKind:    models.StorageMeasureKind(row.MeasureKind),
			MeasureKey:     row.MeasureKey,
			MeasuredBytes:  row.MeasuredBytes,
			DiskTotalBytes: row.DiskTotalBytes,
			DiskFreeBytes:  row.DiskFreeBytes,
			SampledAt:      row.SampledAt,
		})
	}
	return samples, nil
}

func (self *transaction) DeleteStorageUsageSamplesBefore(day time.Time) error {
	return self.tx.Exec(`DELETE FROM "storage_usage_sample" WHERE "sampled_on" < (? AT TIME ZONE 'UTC')::date`, day.UTC()).Error
}

func (self *transaction) GetStorageDiskWarning(diskLabel string) (*models.StorageDiskWarning, error) {
	var row storageDiskWarningModel
	result := self.tx.Where(`"disk_label" = ?`, diskLabel).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &models.StorageDiskWarning{
		DiskLabel:       row.DiskLabel,
		WarningDays:     row.WarningDays,
		ProjectedFullOn: time.Date(row.ProjectedFullOn.Year(), row.ProjectedFullOn.Month(), row.ProjectedFullOn.Day(), 0, 0, 0, 0, time.UTC),
		SentAt:          row.SentAt,
	}, nil
}

func (self *transaction) PutStorageDiskWarning(warning *models.StorageDiskWarning) error {
	return self.tx.Exec(
		`INSERT INTO "storage_disk_warning" ("disk_label", "warning_days", "projected_full_on", "sent_at")
		VALUES (?, ?, (? AT TIME ZONE 'UTC')::date, ?)
		ON CONFLICT ("disk_label") DO UPDATE SET "warning_days" = EXCLUDED."warning_days",
		"projected_full_on" = EXCLUDED."projected_full_on", "sent_at" = EXCLUDED."sent_at"`,
		warning.DiskLabel, warning.WarningDays, warning.ProjectedFullOn.UTC(), warning.SentAt.UTC()).Error
}

func (self *transaction) DeleteStorageDiskWarning(diskLabel string) error {
	return self.tx.Exec(`DELETE FROM "storage_disk_warning" WHERE "disk_label" = ?`, diskLabel).Error
}
