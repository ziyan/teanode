package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func limitOf(minor int64) *int64 { return &minor }

// ada shares her payment skill with ben, who is in a circle with her.
func shareWithBen(t *testing.T, tx db.Transaction, world *shareWorld) *models.SkillShare {
	t.Helper()
	if _, err := tx.EnsureAgent(world.ada.ID); err != nil {
		t.Fatalf("EnsureAgent: %s", err)
	}
	share, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{
		SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.ben.ID,
		PaymentLimitMinor: limitOf(10000), MonthlyLimitMinor: limitOf(40000), CurrencyCode: "USD",
	})
	if err != nil {
		t.Fatalf("ShareSkill: %s", err)
	}
	return share
}

func callOf(share *models.SkillShare, callState models.SkillShareCallState, amount int64) *models.SkillShareCall {
	call := &models.SkillShareCall{
		SkillShareID: share.ID, ToolName: "pay", CallState: callState, CurrencyCode: "usd",
		ToolArguments: map[string]any{"action": "card", "amount_minor": float64(amount)},
	}
	if amount > 0 {
		call.AmountMinor = &amount
	}
	if callState == models.SkillShareCallWaitingForApproval {
		expires := time.Now().Add(models.SkillShareCallWaitFor)
		call.ExpiresAt = &expires
	}
	return call
}

// Sharing needs a circle in common and says not found rather than why; sharing
// again changes the share instead of adding one.
func TestASkillIsSharedOnlyWithinACircle(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		if share.ID == "" || share.OwnerUsername != "ada" || share.GranteeUsername != "ben" || share.CurrencyCode != "usd" ||
			*share.PaymentLimitMinor != 10000 || len(share.ToolNames) != 1 {
			t.Fatalf("the share: %+v", share)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: stranger.ID, IsApprovalRequired: true}); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.ada.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share with herself: %v", err)
		}
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "payments", GranteeUserID: world.ben.ID}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a share of nothing: %v", err)
		}
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.ben.ID, PaymentLimitMinor: limitOf(5)}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a limit with no currency: %v", err)
		}
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.ben.ID, PaymentLimitMinor: limitOf(0), CurrencyCode: "usd"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a limit of nothing: %v", err)
		}

		again, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{
			SkillName: "payments", ToolNames: []string{"pay", "spend_requests"}, GranteeUserID: world.ben.ID, IsApprovalRequired: true,
		})
		if err != nil || again.ID != share.ID || !again.IsApprovalRequired || len(again.ToolNames) != 2 || again.PaymentLimitMinor != nil {
			t.Fatalf("sharing again changes the share: %+v %v", again, err)
		}
		mine, err := tx.ListSkillSharesBy(world.ada.ID)
		if err != nil || len(mine) != 1 {
			t.Fatalf("ListSkillSharesBy: %v %v", mine, err)
		}
		reaching, err := tx.ListSkillSharesReaching(world.ben.ID)
		if err != nil || len(reaching) != 1 || reaching[0].OwnerUsername != "ada" {
			t.Fatalf("ListSkillSharesReaching: %v %v", reaching, err)
		}
		if reaching, _ := tx.ListSkillSharesReaching(world.cora.ID); len(reaching) != 0 {
			t.Fatalf("someone else is reached: %v", reaching)
		}
		if reaching, _ := tx.ListSkillSharesReaching(world.ada.ID); len(reaching) != 0 {
			t.Fatalf("the owner is reached by their own share: %v", reaching)
		}
	})
}

// Leaving the only circle two people share ends the share between them, with
// its calls, in the same transaction; leaving one of two they share does not.
func TestLeavingACircleEndsTheSkillSharesThatNeededIt(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		call, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallRan, 500))
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}

		// A second circle in common keeps it.
		second, err := tx.CreateCircle(&models.Circle{Name: "Also", CircleKind: models.CircleKindFriends}, world.ada.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err := tx.AddCircleMember(second.ID, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if kept, _ := tx.GetSkillShare(share.ID); kept == nil {
			t.Fatal("the share went though the two still share a circle")
		}
		if _, err := tx.RemoveCircleMember(second.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if gone, _ := tx.GetSkillShare(share.ID); gone != nil {
			t.Fatal("the share outlived the last circle the two shared")
		}
		if gone, _ := tx.GetSkillShareCall(call.ID); gone != nil {
			t.Fatal("the share's calls outlived it")
		}
	})
}

// The month's total counts calls that ran or failed in this currency in this
// month, a failure after the command acted being no different from one
// before it, and nothing a call that never ran asked for.
func TestTheMonthTotalCountsRanAndFailedCallsOnly(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		for _, call := range []*models.SkillShareCall{
			callOf(share, models.SkillShareCallRan, 1000),
			callOf(share, models.SkillShareCallRan, 250),
			callOf(share, models.SkillShareCallFailed, 9000),
			callOf(share, models.SkillShareCallWaitingForApproval, 8000),
			callOf(share, models.SkillShareCallDeclined, 7000),
			callOf(share, models.SkillShareCallExpired, 6000),
			callOf(share, models.SkillShareCallRefused, 5000),
		} {
			if _, err := tx.CreateSkillShareCall(call); err != nil {
				t.Fatalf("CreateSkillShareCall: %s", err)
			}
		}
		other := callOf(share, models.SkillShareCallRan, 333)
		other.CurrencyCode = "eur"
		if _, err := tx.CreateSkillShareCall(other); err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
		total, err := tx.SkillShareMonthTotal(share.ID, "usd", start, end)
		if err != nil || total != 10250 {
			t.Fatalf("the month's total is %d (%v), want the two calls that ran and the one that failed", total, err)
		}
		if total, _ := tx.SkillShareMonthTotal(share.ID, "usd", end, end.Add(time.Hour)); total != 0 {
			t.Fatalf("another month counts this one's calls: %d", total)
		}
		if total, _ := tx.SkillShareMonthTotal(share.ID, "eur", start, end); total != 333 {
			t.Fatalf("a currency counts another's calls: %d", total)
		}
	})
}

// Two answers at once change the call once; an answer after the day does not
// approve it; declining a call that waited past its day is still a no.
func TestACallIsAnsweredOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var share *models.SkillShare
	var waitingId, lateId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share = shareWithBen(t, tx, world)
		waiting, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallWaitingForApproval, 20000))
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		waitingId = waiting.ID
		late, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallWaitingForApproval, 20000))
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		lateId = late.ID
	})

	// Two answers, from the phone and the drawer, in two transactions at once.
	// The first has answered and not committed when the second starts; the
	// second must wait for it, and then find nothing waiting to answer.
	now := time.Now()
	firstHasAnswered := make(chan struct{})
	mayCommit := make(chan struct{})
	type answered struct {
		call      *models.SkillShareCall
		isChanged bool
		err       error
	}
	firstDone := make(chan answered, 1)
	go func() {
		var result answered
		result.err = database.Transaction(func(tx db.Transaction) error {
			var err error
			result.call, result.isChanged, err = tx.AnswerSkillShareCall(waitingId, models.SkillShareCallRan, "go ahead", now)
			close(firstHasAnswered)
			<-mayCommit
			return err
		})
		firstDone <- result
	}()
	<-firstHasAnswered
	secondDone := make(chan answered, 1)
	go func() {
		var result answered
		result.err = database.Transaction(func(tx db.Transaction) error {
			var err error
			result.call, result.isChanged, err = tx.AnswerSkillShareCall(waitingId, models.SkillShareCallDeclined, "no", now)
			return err
		})
		secondDone <- result
	}()
	select {
	case <-secondDone:
		t.Fatal("the second answer did not wait for the first, which has not committed")
	case <-time.After(300 * time.Millisecond):
	}
	close(mayCommit)
	first, second := <-firstDone, <-secondDone
	if first.err != nil || !first.isChanged || first.call.CallState != models.SkillShareCallRan || first.call.DecidedAt == nil || first.call.DecisionNote != "go ahead" {
		t.Fatalf("the first answer: %+v", first)
	}
	if second.err != nil || second.isChanged || second.call.CallState != models.SkillShareCallRan || second.call.DecisionNote != "go ahead" {
		t.Fatalf("the second answer changed the call, or did not see the first: %+v", second)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		again, isChanged, err := tx.AnswerSkillShareCall(waitingId, models.SkillShareCallRan, "again", now)
		if err != nil || isChanged || again.DecisionNote != "go ahead" {
			t.Fatalf("a later answer changed the call: %+v %v %v", again, isChanged, err)
		}
		if _, _, err := tx.AnswerSkillShareCall(waitingId, models.SkillShareCallFailed, "", now); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("an answer that is neither yes nor no: %v", err)
		}
		afterTheDay := now.Add(models.SkillShareCallWaitFor + time.Minute)
		if _, isChanged, _ := tx.AnswerSkillShareCall(lateId, models.SkillShareCallRan, "", afterTheDay); isChanged {
			t.Fatal("a call was approved after its day")
		}
		if declined, isChanged, _ := tx.AnswerSkillShareCall(lateId, models.SkillShareCallDeclined, "", afterTheDay); !isChanged || declined.CallState != models.SkillShareCallDeclined {
			t.Fatalf("a late no: %+v %v", declined, isChanged)
		}
	})
}

// Two calls deciding against the month's limit at once take turns on the
// share's row (LockSkillShare), so the second reads the total with the first
// counted and is turned back; without it both read the same total and both
// run, and the owner's limit is passed.
func TestTwoCallsRacingTheMonthlyLimitOnlyOneRuns(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var share *models.SkillShare
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share = shareWithBen(t, tx, world)
	})
	// The month's limit is 40000 and each call asks for 25000.
	const amount = int64(25000)
	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	decide := func(tx db.Transaction, hasLocked chan<- struct{}, mayContinue <-chan struct{}) (models.SkillShareCallState, error) {
		locked, err := tx.LockSkillShare(share.ID)
		if err != nil {
			return "", err
		}
		if hasLocked != nil {
			close(hasLocked)
			<-mayContinue
		}
		total, err := tx.SkillShareMonthTotal(locked.ID, locked.CurrencyCode, start, end)
		if err != nil {
			return "", err
		}
		callState := models.SkillShareCallRan
		if total+amount > *locked.MonthlyLimitMinor {
			callState = models.SkillShareCallWaitingForApproval
		}
		call, err := tx.CreateSkillShareCall(callOf(locked, callState, amount))
		if err != nil {
			return "", err
		}
		return call.CallState, nil
	}

	firstHasLocked := make(chan struct{})
	mayContinue := make(chan struct{})
	type decision struct {
		callState models.SkillShareCallState
		err       error
	}
	firstDone := make(chan decision, 1)
	go func() {
		var result decision
		result.err = database.Transaction(func(tx db.Transaction) error {
			var err error
			result.callState, err = decide(tx, firstHasLocked, mayContinue)
			return err
		})
		firstDone <- result
	}()
	<-firstHasLocked
	secondDone := make(chan decision, 1)
	go func() {
		var result decision
		result.err = database.Transaction(func(tx db.Transaction) error {
			var err error
			result.callState, err = decide(tx, nil, nil)
			return err
		})
		secondDone <- result
	}()
	select {
	case <-secondDone:
		t.Fatal("the second call did not wait for the first to finish deciding")
	case <-time.After(300 * time.Millisecond):
	}
	close(mayContinue)
	first, second := <-firstDone, <-secondDone
	if first.err != nil || second.err != nil {
		t.Fatalf("the decisions: %+v %+v", first, second)
	}
	if first.callState != models.SkillShareCallRan || second.callState != models.SkillShareCallWaitingForApproval {
		t.Fatalf("the first call is %s and the second %s; want the first to run and the second to wait", first.callState, second.callState)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		total, err := tx.SkillShareMonthTotal(share.ID, "usd", start, end)
		if err != nil || total != amount {
			t.Errorf("the month's total is %d (%v); want %d, one call", total, err, amount)
		}
	})
}

// The sweep moves calls past their day to expired, once, and leaves the rest.
func TestWaitingCallsExpireAfterTheirDay(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		fresh, _ := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallWaitingForApproval, 100))
		old := callOf(share, models.SkillShareCallWaitingForApproval, 200)
		past := time.Now().Add(-time.Minute)
		old.ExpiresAt = &past
		stale, err := tx.CreateSkillShareCall(old)
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		expired, err := tx.ExpireSkillShareCalls(time.Now())
		if err != nil || len(expired) != 1 || expired[0].ID != stale.ID || expired[0].CallState != models.SkillShareCallExpired || expired[0].DecidedAt == nil {
			t.Fatalf("the sweep: %+v %v", expired, err)
		}
		if again, _ := tx.ExpireSkillShareCalls(time.Now()); len(again) != 0 {
			t.Fatalf("a call expired twice: %+v", again)
		}
		if still, _ := tx.GetSkillShareCall(fresh.ID); still.CallState != models.SkillShareCallWaitingForApproval {
			t.Fatalf("a call still within its day: %+v", still)
		}
	})
}

// An identifier counts as made through the share only when it appears, as a
// whole word, in the text of the output of one of its calls that ran: the
// output of a command is one piece of text with the identifier inside it.
func TestAnIdentifierMustHaveComeOutOfTheSharesOwnCalls(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		ran := callOf(share, models.SkillShareCallRan, 500)
		ran.ToolOutput = map[string]any{"text": `{"id":"req_001abcde","status":"pending_approval"}`, "files": []any{map[string]any{"name": "req_other_in_a_name"}}}
		failed := callOf(share, models.SkillShareCallFailed, 500)
		failed.ToolOutput = map[string]any{"text": "req_002failed"}
		for _, call := range []*models.SkillShareCall{ran, failed} {
			if _, err := tx.CreateSkillShareCall(call); err != nil {
				t.Fatalf("CreateSkillShareCall: %s", err)
			}
		}
		for identifier, want := range map[string]bool{
			"req_001abcde":        true,
			"req_other_in_a_name": true,
			"req_002failed":       false, // only a call that ran made anything
			"req_001abcd":         false, // part of a longer word
			"req_001abcdef":       false,
			"status":              false, // the name of a field, not a request
			"pending_approval":    true,
			"abc":                 false, // too short to count
			"":                    false,
		} {
			if got, err := tx.HasSkillShareOutputWith(share.ID, identifier); err != nil || got != want {
				t.Errorf("%q: %v %v, want %v", identifier, got, err, want)
			}
		}

		// Another share's outputs do not count.
		if _, err := tx.EnsureAgent(world.ben.ID); err != nil {
			t.Fatalf("EnsureAgent: %s", err)
		}
		bens, err := tx.ShareSkill(world.ben.ID, &models.SkillShare{SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.ada.ID, IsApprovalRequired: true})
		if err != nil {
			t.Fatalf("ShareSkill: %s", err)
		}
		if got, _ := tx.HasSkillShareOutputWith(bens.ID, "req_001abcde"); got {
			t.Error("an identifier from another share counts")
		}
	})
}

// A call decided while its grantee's conversation was not told is found by
// the sweep; only one claimer has it; a told one is not found again.
func TestADecidedCallIsToldOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		conversation, err := tx.MainAgentConversation(mustAgent(t, tx, world.ben).ID, "drawer")
		if err != nil {
			t.Fatalf("MainAgentConversation: %s", err)
		}
		call := callOf(share, models.SkillShareCallWaitingForApproval, 20000)
		call.GranteeConversationID = conversation.ID
		made, err := tx.CreateSkillShareCall(call)
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		now := time.Now()
		if found, _ := tx.ListSkillShareCallsToWake(now.Add(-time.Hour), now.Add(time.Hour), now.Add(-time.Hour), 10); len(found) != 0 {
			t.Fatalf("a call nobody answered is to be told: %v", found)
		}
		if _, _, err := tx.AnswerSkillShareCall(made.ID, models.SkillShareCallDeclined, "", now); err != nil {
			t.Fatalf("AnswerSkillShareCall: %s", err)
		}
		found, err := tx.ListSkillShareCallsToWake(now.Add(-time.Hour), now.Add(time.Hour), now.Add(-time.Hour), 10)
		if err != nil || len(found) != 1 || found[0].ID != made.ID {
			t.Fatalf("the call to tell: %v %v", found, err)
		}
		stale := now.Add(-30 * time.Minute)
		if isClaimed, _ := tx.ClaimSkillShareCallWake(made.ID, now, stale); !isClaimed {
			t.Fatal("the first claim failed")
		}
		if isClaimed, _ := tx.ClaimSkillShareCallWake(made.ID, now, stale); isClaimed {
			t.Fatal("two callers have the telling")
		}
		if found, _ := tx.ListSkillShareCallsToWake(now.Add(-time.Hour), now.Add(time.Hour), stale, 10); len(found) != 0 {
			t.Fatalf("a claimed call is found: %v", found)
		}
		if err := tx.ReleaseSkillShareCallWakes([]string{made.ID}); err != nil {
			t.Fatalf("Release: %s", err)
		}
		if isClaimed, _ := tx.ClaimSkillShareCallWake(made.ID, now, stale); !isClaimed {
			t.Fatal("a released claim cannot be taken")
		}
		if err := tx.MarkSkillShareCallsWoken([]string{made.ID}, now); err != nil {
			t.Fatalf("MarkSkillShareCallsWoken: %s", err)
		}
		if found, _ := tx.ListSkillShareCallsToWake(now.Add(-time.Hour), now.Add(time.Hour), now.Add(time.Hour), 10); len(found) != 0 {
			t.Fatalf("a told call is told again: %v", found)
		}
	})
}

func mustAgent(t *testing.T, tx db.Transaction, user *models.User) *models.Agent {
	t.Helper()
	found, err := tx.EnsureAgent(user.ID)
	if err != nil {
		t.Fatalf("EnsureAgent: %s", err)
	}
	return found
}

// A share's calls list the ones that wait first.
func TestWaitingCallsAreListedFirst(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		var states []models.SkillShareCallState
		for _, state := range []models.SkillShareCallState{models.SkillShareCallRan, models.SkillShareCallWaitingForApproval, models.SkillShareCallDeclined} {
			if _, err := tx.CreateSkillShareCall(callOf(share, state, 100)); err != nil {
				t.Fatalf("CreateSkillShareCall: %s", err)
			}
		}
		calls, err := tx.ListSkillShareCalls(share.ID, "", 10)
		if err != nil || len(calls) != 3 {
			t.Fatalf("ListSkillShareCalls: %v %v", calls, err)
		}
		for _, call := range calls {
			states = append(states, call.CallState)
		}
		if states[0] != models.SkillShareCallWaitingForApproval || states[1] != models.SkillShareCallDeclined {
			t.Fatalf("the order is %v", states)
		}
		only, _ := tx.ListSkillShareCalls(share.ID, models.SkillShareCallRan, 10)
		if len(only) != 1 || only[0].CallState != models.SkillShareCallRan {
			t.Fatalf("a state narrows them: %v", only)
		}
	})
}

// Taking a skill off the server ends the shares of its tools, and only those.
func TestRemovingASkillEndsItsShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		other, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{SkillName: "music", ToolNames: []string{"play"}, GranteeUserID: world.ben.ID, IsApprovalRequired: true})
		if err != nil {
			t.Fatalf("ShareSkill: %s", err)
		}
		if _, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallRan, 500)); err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		if err := tx.DeleteAgentSkill(" Payments "); err != nil {
			t.Fatalf("DeleteAgentSkill: %s", err)
		}
		if gone, _ := tx.GetSkillShare(share.ID); gone != nil {
			t.Error("the share outlived its skill")
		}
		if kept, _ := tx.GetSkillShare(other.ID); kept == nil {
			t.Error("the share of another skill went")
		}
	})
}
