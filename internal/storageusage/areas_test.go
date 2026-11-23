package storageusage_test

import (
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/storageusage"
)

// A new table must be given an area, even if that area is other. The test
// reads the tables the migrations really create, so a table added to a
// migration and not to the map fails here instead of being counted under
// other without anybody having looked at it.
func TestEveryTableOfTheMigrationsHasAnArea(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var created []string
	if err := database.Transaction(func(tx db.Transaction) error {
		relations, err := tx.MeasureRelationSizes()
		for _, relation := range relations {
			created = append(created, relation.TableName)
		}
		return err
	}); err != nil {
		test.Fatalf("MeasureRelationSizes: %s", err)
	}
	if len(created) < 100 {
		test.Fatalf("only %d tables were found, the migrations create many more", len(created))
	}
	for _, table := range created {
		if _, isListed := storageusage.AreaOf(table); !isListed {
			test.Errorf("the table %q has no area: add it to areaTables in areas.go, under other if it belongs nowhere", table)
		}
	}
	for _, table := range storageusage.ListedTables() {
		if !slices.Contains(created, table) {
			test.Errorf("areaTables lists %q, which the migrations do not create", table)
		}
	}
}

func TestATableIsListedUnderOneAreaOnly(test *testing.T) {
	test.Parallel()
	seen := map[string]storageusage.Area{}
	for _, table := range storageusage.ListedTables() {
		area, _ := storageusage.AreaOf(table)
		seen[table] = area
	}
	// The map is built from slices, so a table written under two areas would
	// silently take the later one. Counting the entries catches it.
	total := 0
	for _, area := range storageusage.Areas {
		total += len(storageusage.TablesOf(area))
	}
	if total != len(seen) {
		test.Errorf("the areas list %d tables between them and %d are distinct: a table is under two areas", total, len(seen))
	}
}
