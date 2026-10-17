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

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		share := shareWithBen(t, tx, world)
		waiting, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallWaitingForApproval, 20000))
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		now := time.Now()
		first, isChanged, err := tx.AnswerSkillShareCall(waiting.ID, models.SkillShareCallRan, "go ahead", now)
		if err != nil || !isChanged || first.CallState != models.SkillShareCallRan || first.DecidedAt == nil || first.DecisionNote != "go ahead" {
			t.Fatalf("the first answer: %+v %v %v", first, isChanged, err)
		}
		second, isChanged, err := tx.AnswerSkillShareCall(waiting.ID, models.SkillShareCallRan, "again", now)
		if err != nil || isChanged || second.DecisionNote != "go ahead" {
			t.Fatalf("the second answer changed the call: %+v %v %v", second, isChanged, err)
		}
		if _, isChanged, _ := tx.AnswerSkillShareCall(waiting.ID, models.SkillShareCallDeclined, "no", now); isChanged {
			t.Fatal("a declined answer changed a call already approved")
		}
		if _, _, err := tx.AnswerSkillShareCall(waiting.ID, models.SkillShareCallFailed, "", now); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("an answer that is neither yes nor no: %v", err)
		}

		late, err := tx.CreateSkillShareCall(callOf(share, models.SkillShareCallWaitingForApproval, 20000))
		if err != nil {
			t.Fatalf("CreateSkillShareCall: %s", err)
		}
		afterTheDay := now.Add(models.SkillShareCallWaitFor + time.Minute)
		if _, isChanged, _ := tx.AnswerSkillShareCall(late.ID, models.SkillShareCallRan, "", afterTheDay); isChanged {
			t.Fatal("a call was approved after its day")
		}
		if declined, isChanged, _ := tx.AnswerSkillShareCall(late.ID, models.SkillShareCallDeclined, "", afterTheDay); !isChanged || declined.CallState != models.SkillShareCallDeclined {
			t.Fatalf("a late no: %+v %v", declined, isChanged)
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
