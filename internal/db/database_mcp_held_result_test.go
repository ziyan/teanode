package db_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

var testHeldLimits = db.McpHeldResultLimits{
	HeldFor:              30 * time.Minute,
	ResultCountPerHolder: 3,
	ByteCountPerHolder:   100,
	ByteCount:            250,
}

func keepHeld(test *testing.T, database db.Database, holder, id, text string, now time.Time) {
	test.Helper()
	if err := database.Transaction(func(tx db.Transaction) error {
		return tx.KeepMcpHeldResult(&db.McpHeldResult{ID: id, Holder: holder, ResultText: text, IsUntrusted: true}, testHeldLimits, now)
	}); err != nil {
		test.Fatalf("keeping %s: %s", id, err)
	}
}

func findHeld(test *testing.T, database db.Database, holder, id string, notBefore time.Time) *db.McpHeldResult {
	test.Helper()
	var found *db.McpHeldResult
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		found, err = tx.FindMcpHeldResult(holder, id, notBefore)
		return err
	}); err != nil {
		test.Fatalf("finding %s: %s", id, err)
	}
	return found
}

// A held result is found by its holder only, with its text and its marks.
func TestAHeldResultIsFoundByItsHolderOnly(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	now := time.Now()
	keepHeld(test, database, "holder-one", "result-a", "the whole text", now)
	found := findHeld(test, database, "holder-one", "result-a", now.Add(-time.Minute))
	if found == nil || found.ResultText != "the whole text" || !found.IsUntrusted || found.IsError {
		test.Fatalf("found %+v", found)
	}
	if findHeld(test, database, "holder-two", "result-a", now.Add(-time.Minute)) != nil {
		test.Fatal("another holder's result was found")
	}
	if findHeld(test, database, "holder-one", "result-unknown", now.Add(-time.Minute)) != nil {
		test.Fatal("an id never kept was found")
	}
	if findHeld(test, database, "holder-one", "result-a", now.Add(time.Second)) != nil {
		test.Fatal("a result held before the cutoff was found")
	}
}

// Keeping drops what is past its life, and the holder's oldest beyond the
// count and the bytes, without touching another holder's.
func TestKeepingAHeldResultEnforcesTheBounds(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	start := time.Now()
	notBefore := start.Add(-time.Hour)

	keepHeld(test, database, "other", "other-a", "x", start)
	keepHeld(test, database, "holder", "old", "x", start)
	// Past its life by the time the next one is kept.
	later := start.Add(testHeldLimits.HeldFor + time.Second)
	keepHeld(test, database, "holder", "new-1", "x", later)
	if findHeld(test, database, "holder", "old", notBefore) != nil || findHeld(test, database, "other", "other-a", notBefore) != nil {
		test.Fatal("a result past its life was kept")
	}

	// By count: three at most, the oldest goes.
	for index := 2; index <= 4; index++ {
		keepHeld(test, database, "holder", fmt.Sprintf("new-%d", index), "x", later.Add(time.Duration(index)*time.Second))
	}
	if findHeld(test, database, "holder", "new-1", notBefore) != nil {
		test.Fatal("the oldest of too many results was kept")
	}
	for index := 2; index <= 4; index++ {
		if findHeld(test, database, "holder", fmt.Sprintf("new-%d", index), notBefore) == nil {
			test.Fatalf("new-%d was not kept", index)
		}
	}

	// By bytes: 100 for a holder, so a result of 60 pushes out until it fits,
	// and a holder's pushing never touches another holder's.
	keepHeld(test, database, "neighbor", "neighbor-a", "neighbor's", later.Add(time.Second))
	keepHeld(test, database, "holder", "big-1", strings.Repeat("y", 60), later.Add(10*time.Second))
	keepHeld(test, database, "holder", "big-2", strings.Repeat("y", 60), later.Add(11*time.Second))
	if findHeld(test, database, "holder", "big-1", notBefore) != nil || findHeld(test, database, "holder", "big-2", notBefore) == nil {
		test.Fatal("the holder's bytes were not bounded to the newest")
	}
	if findHeld(test, database, "neighbor", "neighbor-a", notBefore) == nil {
		test.Fatal("one holder pushed out another's result")
	}
}

// All holders together hold at most the total bytes, the oldest going first.
func TestKeepingAHeldResultBoundsTheTotalBytes(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	now := time.Now()
	notBefore := now.Add(-time.Hour)
	keepHeld(test, database, "holder-1", "first", strings.Repeat("a", 90), now)
	keepHeld(test, database, "holder-2", "second", strings.Repeat("b", 90), now.Add(time.Second))
	keepHeld(test, database, "holder-3", "third", strings.Repeat("c", 90), now.Add(2*time.Second))
	// 250 allowed: three of 90 do not fit, the oldest goes.
	if findHeld(test, database, "holder-1", "first", notBefore) != nil {
		test.Fatal("the oldest result was kept past the total bound")
	}
	if findHeld(test, database, "holder-2", "second", notBefore) == nil || findHeld(test, database, "holder-3", "third", notBefore) == nil {
		test.Fatal("the newer results were not kept")
	}
}
