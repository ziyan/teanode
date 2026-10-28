package db_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func claimNotices(test *testing.T, database db.Database, now time.Time, reclaimAfter time.Duration) []*db.LocationVisitNotice {
	test.Helper()
	var claimed []*db.LocationVisitNotice
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		claimed, err = tx.ClaimDueLocationVisitNotices(10, reclaimAfter, now)
		return err
	}); err != nil {
		test.Fatalf("claiming: %s", err)
	}
	return claimed
}

// A notice is claimed once it is due, by one claimer, comes back after the
// claimer is given up on, and is gone once deleted.
func TestAVisitNoticeIsClaimedOnceWhenDue(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	userId := dbtest.CreateUser(test, database, "notice-ana")
	now := time.Now()
	if err := database.Transaction(func(tx db.Transaction) error {
		return tx.CreateLocationVisitNotice(&db.LocationVisitNotice{
			UserID: userId, ClientVisitKey: "visit-1", IsArrival: true,
			HappenedAt: now, DueAt: now.Add(2 * time.Minute),
		}, now)
	}); err != nil {
		test.Fatal(err)
	}
	if claimed := claimNotices(test, database, now.Add(time.Minute), 5*time.Minute); len(claimed) != 0 {
		test.Fatalf("a notice not yet due was claimed: %+v", claimed)
	}
	claimed := claimNotices(test, database, now.Add(3*time.Minute), 5*time.Minute)
	if len(claimed) != 1 || claimed[0].UserID != userId || claimed[0].ClientVisitKey != "visit-1" || !claimed[0].IsArrival {
		test.Fatalf("claimed %+v", claimed)
	}
	if again := claimNotices(test, database, now.Add(4*time.Minute), 5*time.Minute); len(again) != 0 {
		test.Fatalf("a claimed notice was claimed again within the claim's life: %+v", again)
	}
	back := claimNotices(test, database, now.Add(9*time.Minute), 5*time.Minute)
	if len(back) != 1 || back[0].ID != claimed[0].ID {
		test.Fatalf("a notice whose claimer went away did not come back: %+v", back)
	}
	if err := database.Transaction(func(tx db.Transaction) error { return tx.DeleteLocationVisitNotice(back[0].ID) }); err != nil {
		test.Fatal(err)
	}
	if gone := claimNotices(test, database, now.Add(time.Hour), 5*time.Minute); len(gone) != 0 {
		test.Fatalf("a told notice was claimed: %+v", gone)
	}
}

// Claimers at the same moment get a notice between them once, not each.
func TestConcurrentClaimersGetANoticeOnce(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	userId := dbtest.CreateUser(test, database, "notice-ben")
	now := time.Now()
	const noticeCount = 6
	if err := database.Transaction(func(tx db.Transaction) error {
		for range noticeCount {
			if err := tx.CreateLocationVisitNotice(&db.LocationVisitNotice{
				UserID: userId, ClientVisitKey: "visit", HappenedAt: now, DueAt: now,
			}, now); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	const claimerCount = 4
	claimedIds := make(chan string, noticeCount*claimerCount)
	var group sync.WaitGroup
	for range claimerCount {
		group.Add(1)
		go func() {
			defer group.Done()
			// Each holds its transaction open a moment after claiming, so
			// that the others claim while the rows are locked.
			if err := database.Transaction(func(tx db.Transaction) error {
				claimed, err := tx.ClaimDueLocationVisitNotices(2, 5*time.Minute, now)
				for _, notice := range claimed {
					claimedIds <- notice.ID
				}
				time.Sleep(200 * time.Millisecond)
				return err
			}); err != nil {
				test.Errorf("claiming: %s", err)
			}
		}()
	}
	group.Wait()
	close(claimedIds)
	seen := map[string]bool{}
	for id := range claimedIds {
		if seen[id] {
			test.Fatalf("notice %s was claimed twice", id)
		}
		seen[id] = true
	}
	if len(seen) != noticeCount {
		test.Fatalf("%d notices claimed of %d", len(seen), noticeCount)
	}
}
