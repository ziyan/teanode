package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestInvitationRequestReplayPreservesLaterAnswer(test *testing.T) {
	database, resolver, principal, itemId, original, sender := invitationAnswerFixture(test)
	first := AnswerMailInvitationArguments{RequestID: "first-answer", ItemID: itemId, Answer: "ACCEPTED"}
	for _, step := range []struct {
		request       AnswerMailInvitationArguments
		participation string
		replyCount    int
	}{
		{first, calendar.Accepted, 1},
		{first, calendar.Accepted, 1},
		{AnswerMailInvitationArguments{RequestID: "second-answer", ItemID: itemId, Answer: "DECLINED"}, calendar.Declined, 2},
		{first, calendar.Declined, 2},
	} {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			view, err := resolver.AnswerMailInvitation(ctx, step.request)
			if err != nil || view == nil || view.Participation != step.participation {
				test.Fatalf("answer=%+v, %v", view, err)
			}
		})
		if sender.acceptCount != step.replyCount || sender.commitCount != step.replyCount {
			test.Fatalf("replies=%d commits=%d", sender.acceptCount, sender.commitCount)
		}
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		changed := first
		changed.Answer = "TENTATIVE"
		if _, err := resolver.AnswerMailInvitation(ctx, changed); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("changed answer retry=%v", err)
		}
		if err := transaction.DeleteCalendar(principal.User.ID, original.CalendarID); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		view, err := resolver.AnswerMailInvitation(ctx, first)
		if err != nil || view == nil || view.Participation != "" {
			test.Fatalf("deleted event replay=%+v, %v", view, err)
		}
		receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: first.RequestID})
		if err != nil || receipt == nil || !receipt.IsMissing || receipt.Operation != "answer" {
			test.Fatalf("completion=%+v, %v", receipt, err)
		}
	})
	if sender.acceptCount != 2 {
		test.Fatal("deleted event replay sent another reply")
	}
	for _, permission := range []models.Permission{models.PermissionMailRead, models.PermissionMailSend, models.PermissionCalendarUse} {
		var grants []models.Grant
		for _, available := range []models.Permission{models.PermissionMailRead, models.PermissionMailSend, models.PermissionCalendarUse} {
			if available != permission {
				grants = append(grants, models.Grant{Permission: available})
			}
		}
		principal.Permissions = models.NewEffectivePermissions(grants)
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			if _, err := resolver.AnswerMailInvitation(ctx, first); err == nil {
				test.Fatalf("replay ignored removed %s", permission)
			}
			if _, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: first.RequestID}); err == nil {
				test.Fatalf("lookup ignored removed %s", permission)
			}
		})
	}
}

func TestInvitationRequestReceiptFailureRollsBackReplyAndAnswer(test *testing.T) {
	database, resolver, principal, itemId, original, sender := invitationAnswerFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE calendar_request ADD CONSTRAINT reject_answer CHECK (operation <> 'answer')`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.AnswerMailInvitation(ctx, AnswerMailInvitationArguments{RequestID: "failed-answer", ItemID: itemId, Answer: "ACCEPTED"}); err == nil {
			test.Fatal("receipt unexpectedly committed")
		}
		event, err := transaction.GetCalendarObject(original.CalendarID, original.ID)
		if err != nil || event == nil || event.Data != original.Data {
			test.Fatalf("rolled-back event=%+v, %v", event, err)
		}
	})
	if sender.commitCount != 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != "1" {
		test.Fatal("reply escaped receipt failure")
	}
}
