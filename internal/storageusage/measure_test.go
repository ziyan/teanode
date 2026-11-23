package storageusage_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storageusage"
)

func TestAreasAddUpToTheSizeOfTheDatabase(test *testing.T) {
	test.Parallel()
	relations := []models.StorageRelationSize{
		{TableName: "mail", TotalBytes: 300},
		{TableName: "mailbox_item", TotalBytes: 100},
		{TableName: "photo", TotalBytes: 50},
		{TableName: "agent_chunk_vector", TotalBytes: 900},
		{TableName: "a_table_nobody_listed", TotalBytes: 5},
	}
	areas := storageusage.AreaSizesOf(relations, 1500)
	var total int64
	sizes := map[storageusage.Area]int64{}
	for _, area := range areas {
		total += area.SizeBytes
		sizes[area.Area] = area.SizeBytes
	}
	if total != 1500 {
		test.Errorf("the areas add up to %d, want the 1500 of the database", total)
	}
	if sizes[storageusage.AreaMail] != 400 || sizes[storageusage.AreaSearch] != 900 || sizes[storageusage.AreaPhotos] != 50 {
		test.Errorf("sizes = %v", sizes)
	}
	// An unlisted table, and what is in the database outside any table, are
	// both counted under other.
	if sizes[storageusage.AreaOther] != 150 {
		test.Errorf("other = %d, want 150", sizes[storageusage.AreaOther])
	}
	if areas[0].Area != storageusage.AreaSearch {
		test.Errorf("the largest area is first: got %s", areas[0].Area)
	}
	if len(areas) != len(storageusage.Areas) {
		test.Errorf("%d areas, want every one of %d", len(areas), len(storageusage.Areas))
	}
}

func TestEveryKindOfFileIsPresentEvenWhenNothingIsStored(test *testing.T) {
	test.Parallel()
	kinds := storageusage.FileKindsOf([]models.StorageFileKindSize{{FileKind: models.StorageFileKindMail, SizeBytes: 9}})
	if len(kinds) != len(models.StorageFileKinds) {
		test.Fatalf("%d kinds, want %d", len(kinds), len(models.StorageFileKinds))
	}
	for _, kind := range kinds {
		if (kind.FileKind == models.StorageFileKindMail) != (kind.SizeBytes == 9) {
			test.Errorf("kind %s has %d", kind.FileKind, kind.SizeBytes)
		}
	}
}

func TestFilesNoPersonOwnsAreShownApartAndNeverNegative(test *testing.T) {
	test.Parallel()
	totals := []storageusage.FileKindSize{
		{FileKind: models.StorageFileKindMail, SizeBytes: 100},
		{FileKind: models.StorageFileKindPhotoOriginal, SizeBytes: 50},
	}
	people := storageusage.PeopleOf([]models.StoragePersonFileSize{
		{UserID: "first", FileKind: models.StorageFileKindMail, SizeBytes: 60},
		{UserID: "second", FileKind: models.StorageFileKindMail, SizeBytes: 10},
		// Two people hold the same photo, kept once: together they are more
		// than the total, which leaves nothing for nobody.
		{UserID: "first", FileKind: models.StorageFileKindPhotoOriginal, SizeBytes: 40},
		{UserID: "second", FileKind: models.StorageFileKindPhotoOriginal, SizeBytes: 40},
	}, totals)
	if len(people) != 3 {
		test.Fatalf("people = %+v, want two people and the files nobody owns", people)
	}
	if people[0].UserID != "first" || people[0].SizeBytes != 100 {
		test.Errorf("largest = %+v", people[0])
	}
	var unowned *storageusage.PersonFiles
	for index := range people {
		if people[index].UserID == "" {
			unowned = &people[index]
		}
	}
	if unowned == nil || unowned.SizeBytes != 30 {
		test.Errorf("unowned = %+v, want 30 bytes of mail and no photos", unowned)
	}
}
