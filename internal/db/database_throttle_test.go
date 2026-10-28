package db_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A throttle's turn is taken once per interval, by whichever of several
// takers asks first, and again once the interval is over; a turn taken in
// work that rolled back is given back.
func TestAThrottleIsTakenOncePerInterval(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	now := time.Now()
	take := func(at time.Time) bool {
		test.Helper()
		var isTaken bool
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			isTaken, err = tx.TakeThrottle("notice:album-one:reader-one", 10*time.Minute, at)
			return err
		}); err != nil {
			test.Fatalf("TakeThrottle: %s", err)
		}
		return isTaken
	}

	var waitGroup sync.WaitGroup
	var mutex sync.Mutex
	takenCount := 0
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if take(now) {
				mutex.Lock()
				takenCount++
				mutex.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if takenCount != 1 {
		test.Fatalf("eight takers at once took the turn %d times", takenCount)
	}
	if take(now.Add(9 * time.Minute)) {
		test.Errorf("the turn was taken again inside the interval")
	}
	if !take(now.Add(10 * time.Minute)) {
		test.Errorf("the turn was not free once the interval was over")
	}

	_ = database.Transaction(func(tx db.Transaction) error {
		if _, err := tx.TakeThrottle("notice:album-two:reader-one", time.Hour, now); err != nil {
			test.Fatalf("TakeThrottle: %s", err)
		}
		return context.Canceled
	})
	if err := database.Transaction(func(tx db.Transaction) error {
		isTaken, err := tx.TakeThrottle("notice:album-two:reader-one", time.Hour, now)
		if err == nil && !isTaken {
			test.Errorf("a turn taken in work that rolled back was kept")
		}
		return err
	}); err != nil {
		test.Fatalf("TakeThrottle: %s", err)
	}
}

// A rate limit's bucket is one for every taker: its capacity is spent once,
// whoever spends it, and refills at its rate.
func TestARateLimitBucketIsSharedByEveryTaker(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	now := time.Now()
	take := func(at time.Time) bool {
		test.Helper()
		var isTaken bool
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			isTaken, err = tx.TakeRateLimitToken("sign-in:192.0.2.10", 1.0/60, 5, at)
			return err
		}); err != nil {
			test.Fatalf("TakeRateLimitToken: %s", err)
		}
		return isTaken
	}
	var waitGroup sync.WaitGroup
	var mutex sync.Mutex
	takenCount := 0
	for range 12 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if take(now) {
				mutex.Lock()
				takenCount++
				mutex.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if takenCount != 5 {
		test.Fatalf("twelve takers at once took %d tokens from a bucket of five", takenCount)
	}
	if take(now.Add(30 * time.Second)) {
		test.Errorf("a token was taken before one had refilled")
	}
	if !take(now.Add(61 * time.Second)) {
		test.Errorf("no token after a minute's refill")
	}
	if err := database.Transaction(func(tx db.Transaction) error {
		left, err := tx.CountRateLimitTokens("sign-in:192.0.2.10", 1.0/60, 5, now.Add(61*time.Second))
		if err == nil && left >= 1 {
			test.Errorf("%f tokens left, expected less than one", left)
		}
		untouched, err := tx.CountRateLimitTokens("sign-in:192.0.2.11", 1.0/60, 5, now)
		if err == nil && untouched != 5 {
			test.Errorf("a bucket never taken from holds %f, expected 5", untouched)
		}
		return err
	}); err != nil {
		test.Fatal(err)
	}
}

// A session lock is held by one holder at a time across connections, and
// let go when released.
func TestASessionLockHasOneHolder(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	ctx := context.Background()
	key := db.LockKey("test:one-holder")
	release, isLocked, err := database.TryLockSession(ctx, key)
	if err != nil || !isLocked {
		test.Fatalf("the first holder did not get the lock: %v %s", isLocked, err)
	}
	didRun, err := database.WhileLocked(ctx, key, func(context.Context) error {
		test.Errorf("work ran while another held the lock")
		return nil
	})
	if err != nil || didRun {
		test.Fatalf("WhileLocked: %v %s", didRun, err)
	}
	release()
	didRun, err = database.WhileLocked(ctx, key, func(context.Context) error { return nil })
	if err != nil || !didRun {
		test.Fatalf("the lock was not free after its release: %v %s", didRun, err)
	}
	if db.LockKey("test:one-holder") == db.LockKey("test:another") {
		test.Errorf("two names have one key")
	}
}

// A turn is running until it is cut or its instance goes quiet; then it is
// claimed by one claimer, once.
func TestACutTurnIsClaimedOnce(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	userId := dbtest.CreateUser(test, database, "turn-owner")
	now := time.Now()
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		agent, err := tx.CreateAgent(&models.Agent{UserID: userId, Enabled: true})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: now}); err != nil {
			test.Fatalf("CreateAgentConversation: %s", err)
		}
		cutAt := now
		for _, turn := range []*models.AgentTurn{
			{RunID: "run-running", Instance: "first", StartedAt: now, HeartbeatAt: now},
			{RunID: "run-cut", Instance: "first", StartedAt: now, HeartbeatAt: now, CutAt: &cutAt},
			{RunID: "run-quiet", Instance: "second", StartedAt: now.Add(-time.Hour), HeartbeatAt: now.Add(-5 * time.Minute)},
		} {
			turn.ConversationID = conversation.ID
			if err := tx.PutAgentTurn(turn); err != nil {
				test.Fatalf("PutAgentTurn: %s", err)
			}
		}
	})
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		live, err := tx.ListLiveAgentTurns(conversation.ID, now.Add(-90*time.Second))
		if err != nil || len(live) != 1 || live[0].RunID != "run-running" {
			test.Fatalf("the running turn is the live one: %+v %v", live, err)
		}
	})

	var waitGroup sync.WaitGroup
	var mutex sync.Mutex
	claimedRunIds := map[string]int{}
	for range 4 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = database.Transaction(func(tx db.Transaction) error {
				claimed, err := tx.ClaimCutAgentTurns(now.Add(-90*time.Second), 10)
				if err != nil {
					test.Errorf("ClaimCutAgentTurns: %s", err)
				}
				mutex.Lock()
				defer mutex.Unlock()
				for _, turn := range claimed {
					claimedRunIds[turn.RunID]++
				}
				return err
			})
		}()
	}
	waitGroup.Wait()
	if len(claimedRunIds) != 2 || claimedRunIds["run-cut"] != 1 || claimedRunIds["run-quiet"] != 1 {
		test.Fatalf("the cut and the quiet turn were claimed once each: %v", claimedRunIds)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if turn, err := tx.GetAgentTurn("run-running"); err != nil || turn == nil {
			test.Fatalf("the running turn was left: %+v %v", turn, err)
		}
		if turn, err := tx.GetAgentTurn("run-cut"); err != nil || turn != nil {
			test.Fatalf("a claimed turn is forgotten: %+v %v", turn, err)
		}
	})
}

// Spending more than a bucket holds puts it into debt, at most its capacity
// deep, so that work that cost several tokens waits for all of them.
func TestSpendingFromARateLimitBucketGoesIntoDebt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	// As the database keeps it, so that no sliver of a second refills.
	now := time.Now().Truncate(time.Microsecond)
	if err := database.Transaction(func(tx db.Transaction) error {
		return tx.SpendRateLimitTokens("credential:192.0.2.20", 1, 4, 6, now)
	}); err != nil {
		test.Fatalf("SpendRateLimitTokens: %s", err)
	}
	count := func(at time.Time) float64 {
		test.Helper()
		var tokenCount float64
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			tokenCount, err = tx.CountRateLimitTokens("credential:192.0.2.20", 1, 4, at)
			return err
		}); err != nil {
			test.Fatalf("CountRateLimitTokens: %s", err)
		}
		return tokenCount
	}
	if got := count(now); got != -2 {
		test.Fatalf("spending six of four leaves %f, expected -2", got)
	}
	if err := database.Transaction(func(tx db.Transaction) error {
		return tx.SpendRateLimitTokens("credential:192.0.2.20", 1, 4, 20, now)
	}); err != nil {
		test.Fatalf("SpendRateLimitTokens: %s", err)
	}
	if got := count(now); got != -4 {
		test.Fatalf("the debt goes no deeper than the capacity: %f", got)
	}
	if got := count(now.Add(5 * time.Second)); got != 1 {
		test.Fatalf("five seconds refill five tokens from -4: %f", got)
	}
}
