package storageusage

import (
	"sort"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AreaSize is the size of the tables of one area.
type AreaSize struct {
	Area       Area
	TableCount int
	SizeBytes  int64
}

// FileKindSize is what the rows say one kind of file holds.
type FileKindSize struct {
	FileKind  string
	SizeBytes int64
}

// PersonFiles is what one person's files hold, by kind. The database is not
// measured per person.
type PersonFiles struct {
	// UserID is empty for the files no account owns: mail that is in no
	// mailbox, and what the per-person sums do not add up to because a photo
	// kept once is counted for each person who has it.
	UserID    string
	FileKinds []FileKindSize
	SizeBytes int64
}

// Snapshot is what the database and the blob store hold at one moment.
type Snapshot struct {
	DatabaseSizeBytes int64
	Areas             []AreaSize
	FileKinds         []FileKindSize
	FileSizeBytes     int64
	People            []PersonFiles
}

// Measure reads the sizes the database keeps. It scans no row of a large
// table: table sizes come from the catalog, and each file kind is a sum over
// the size column of a table that has one row per file.
func Measure(tx db.Transaction) (*Snapshot, error) {
	snapshot := &Snapshot{}
	var err error
	if snapshot.DatabaseSizeBytes, err = tx.MeasureDatabaseSizeBytes(); err != nil {
		return nil, err
	}
	relations, err := tx.MeasureRelationSizes()
	if err != nil {
		return nil, err
	}
	snapshot.Areas = AreaSizesOf(relations, snapshot.DatabaseSizeBytes)

	fileSizes, err := tx.MeasureFileKindSizes()
	if err != nil {
		return nil, err
	}
	snapshot.FileKinds = FileKindsOf(fileSizes)
	for _, size := range snapshot.FileKinds {
		snapshot.FileSizeBytes += size.SizeBytes
	}

	personSizes, err := tx.MeasurePersonFileSizes()
	if err != nil {
		return nil, err
	}
	snapshot.People = PeopleOf(personSizes, snapshot.FileKinds)
	return snapshot, nil
}

// AreaSizesOf adds the tables up by area, every area present and the largest
// first. What the database holds beyond its tables (the catalog, write-ahead
// log pending, sequences) is counted as other, so that the areas add up to
// the size of the database.
func AreaSizesOf(relations []models.StorageRelationSize, databaseSizeBytes int64) []AreaSize {
	byArea := map[Area]*AreaSize{}
	for _, area := range Areas {
		byArea[area] = &AreaSize{Area: area}
	}
	var tableBytes int64
	for _, relation := range relations {
		area, _ := AreaOf(relation.TableName)
		byArea[area].TableCount++
		byArea[area].SizeBytes += relation.TotalBytes
		tableBytes += relation.TotalBytes
	}
	if rest := databaseSizeBytes - tableBytes; rest > 0 {
		byArea[AreaOther].SizeBytes += rest
	}
	sizes := make([]AreaSize, 0, len(Areas))
	for _, area := range Areas {
		sizes = append(sizes, *byArea[area])
	}
	sort.SliceStable(sizes, func(left, right int) bool { return sizes[left].SizeBytes > sizes[right].SizeBytes })
	return sizes
}

// FileKindsOf puts the measured kinds in the order of models.StorageFileKinds,
// every kind present.
func FileKindsOf(measured []models.StorageFileKindSize) []FileKindSize {
	bySize := map[string]int64{}
	for _, size := range measured {
		bySize[size.FileKind] += size.SizeBytes
	}
	kinds := make([]FileKindSize, 0, len(models.StorageFileKinds))
	for _, fileKind := range models.StorageFileKinds {
		kinds = append(kinds, FileKindSize{FileKind: fileKind, SizeBytes: bySize[fileKind]})
	}
	return kinds
}

// PeopleOf groups the per-person sums into one row a person, the largest
// first, and adds a row with no person for what the totals hold beyond the
// people. A kind that the people add up to more than the total (a photo that
// two accounts both hold is stored once) leaves nothing over for that kind.
func PeopleOf(measured []models.StoragePersonFileSize, totals []FileKindSize) []PersonFiles {
	byUser := map[string]map[string]int64{}
	attributed := map[string]int64{}
	for _, size := range measured {
		if byUser[size.UserID] == nil {
			byUser[size.UserID] = map[string]int64{}
		}
		byUser[size.UserID][size.FileKind] += size.SizeBytes
		attributed[size.FileKind] += size.SizeBytes
	}
	unowned := map[string]int64{}
	for _, total := range totals {
		if rest := total.SizeBytes - attributed[total.FileKind]; rest > 0 {
			unowned[total.FileKind] = rest
		}
	}
	if len(unowned) > 0 {
		byUser[""] = unowned
	}
	people := make([]PersonFiles, 0, len(byUser))
	for userID, sizes := range byUser {
		person := PersonFiles{UserID: userID, FileKinds: FileKindsOf(kindSizesOf(sizes))}
		for _, size := range person.FileKinds {
			person.SizeBytes += size.SizeBytes
		}
		people = append(people, person)
	}
	sort.Slice(people, func(left, right int) bool {
		if people[left].SizeBytes != people[right].SizeBytes {
			return people[left].SizeBytes > people[right].SizeBytes
		}
		return people[left].UserID < people[right].UserID
	})
	return people
}

func kindSizesOf(sizes map[string]int64) []models.StorageFileKindSize {
	measured := make([]models.StorageFileKindSize, 0, len(sizes))
	for fileKind, sizeBytes := range sizes {
		measured = append(measured, models.StorageFileKindSize{FileKind: fileKind, SizeBytes: sizeBytes})
	}
	return measured
}
