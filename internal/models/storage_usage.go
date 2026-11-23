package models

import "time"

// StorageMeasureKind says what a stored sample measures.
type StorageMeasureKind string

const (
	// StorageMeasureDatabase is the whole database, under the key "total".
	StorageMeasureDatabase StorageMeasureKind = "database"

	// StorageMeasureDatabaseArea is the tables of one area, such as mail or
	// photos, with their indexes.
	StorageMeasureDatabaseArea StorageMeasureKind = "databaseArea"

	// StorageMeasureFileKind is one kind of file in the blob store, such as
	// photo originals.
	StorageMeasureFileKind StorageMeasureKind = "fileKind"

	// StorageMeasureDisk is a disk the server measured: the bytes in use,
	// with its total and free bytes beside them.
	StorageMeasureDisk StorageMeasureKind = "disk"
)

// StorageDatabaseTotalKey is the key of the one database sample of a day.
const StorageDatabaseTotalKey = "total"

// The kinds of file the blob store holds, as keys of a file kind sample and
// of the rows the API answers.
const (
	StorageFileKindPhotoOriginal     = "photoOriginal"
	StorageFileKindPhotoPreview      = "photoPreview"
	StorageFileKindRecordingOriginal = "recordingOriginal"
	StorageFileKindRecordingDerived  = "recordingDerived"
	StorageFileKindMail              = "mail"
	StorageFileKindAgentAttachment   = "agentAttachment"
)

// StorageFileKinds is every kind of file, in the order they are listed.
var StorageFileKinds = []string{
	StorageFileKindPhotoOriginal,
	StorageFileKindPhotoPreview,
	StorageFileKindRecordingOriginal,
	StorageFileKindRecordingDerived,
	StorageFileKindMail,
	StorageFileKindAgentAttachment,
}

// StorageUsageSample is one measurement taken on one UTC day.
type StorageUsageSample struct {
	SampledOn     time.Time          `json:"sampledOn"`
	MeasureKind   StorageMeasureKind `json:"measureKind"`
	MeasureKey    string             `json:"measureKey"`
	MeasuredBytes int64              `json:"measuredBytes"`

	// DiskTotalBytes and DiskFreeBytes are set for a disk and nil for
	// everything else.
	DiskTotalBytes *int64    `json:"diskTotalBytes,omitempty"`
	DiskFreeBytes  *int64    `json:"diskFreeBytes,omitempty"`
	SampledAt      time.Time `json:"sampledAt"`
}

// StorageDiskWarning is the warning last sent about a disk.
type StorageDiskWarning struct {
	DiskLabel       string    `json:"diskLabel"`
	WarningDays     int       `json:"warningDays"`
	ProjectedFullOn time.Time `json:"projectedFullOn"`
	SentAt          time.Time `json:"sentAt"`
}

// StorageRelationSize is the size of one table with its indexes and its
// out-of-line storage.
type StorageRelationSize struct {
	TableName  string
	TotalBytes int64
}

// StorageFileKindSize is what the rows say one kind of file holds.
type StorageFileKindSize struct {
	FileKind  string
	SizeBytes int64
}

// StoragePersonFileSize is what one person's files of one kind hold.
type StoragePersonFileSize struct {
	UserID    string
	FileKind  string
	SizeBytes int64
}
