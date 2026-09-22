package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCalendarDeleteRequestReplaysWithoutAnotherCancellation(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	var eventId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		event, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		eventId = event.ID
	})
	request := DeleteCalendarEventArguments{CalendarID: arguments.CalendarID, ID: eventId, RequestID: "delete-event"}
	for range 2 {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			removed, err := resolver.DeleteCalendarEvent(ctx, request)
			if err != nil || !removed {
				test.Fatalf("delete=%v, %v", removed, err)
			}
			receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: request.RequestID})
			if err != nil || receipt == nil || !receipt.IsMissing || receipt.Operation != "delete" {
				test.Fatalf("completion=%+v, %v", receipt, err)
			}
		})
	}
	if sender.acceptCount != 2 || sender.commitCount != 2 {
		test.Fatalf("invite/cancel accepts=%d commits=%d", sender.acceptCount, sender.commitCount)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteCalendar(principal.User.ID, request.CalendarID); err != nil {
			test.Fatal(err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if removed, err := resolver.DeleteCalendarEvent(ctx, request); err != nil || !removed {
			test.Fatalf("deleted calendar replay=%v, %v", removed, err)
		}
		changed := request
		changed.ID = "another-event"
		if _, err := resolver.DeleteCalendarEvent(ctx, changed); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("changed target=%v", err)
		}
	})
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.DeleteCalendarEvent(ctx, request); err == nil {
			test.Fatal("cancellation replay ignored revoked send permission")
		}
	})
	if sender.acceptCount != 2 {
		test.Fatal("retained replay sent another cancellation")
	}
}

func TestCalendarDeleteReceiptFailureRestoresEventAndMail(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	var eventId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		event, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		eventId = event.ID
	})
	dbtest.Exec(test, database, `ALTER TABLE calendar_request ADD CONSTRAINT refuse_delete_receipt CHECK (operation <> 'delete')`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if removed, err := resolver.DeleteCalendarEvent(ctx, DeleteCalendarEventArguments{CalendarID: arguments.CalendarID, ID: eventId, RequestID: "failed-delete"}); err == nil || removed {
			test.Fatalf("delete=%v, %v", removed, err)
		}
		event, err := transaction.GetCalendarObject(arguments.CalendarID, eventId)
		if err != nil || event == nil {
			test.Fatalf("restored event=%+v, %v", event, err)
		}
	})
	if sender.commitCount != 1 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != "2" {
		test.Fatal("cancellation escaped failed receipt")
	}
}

func TestPersonalCalendarDeleteRequestNeedsNoSendPermission(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	arguments.Attendees = nil
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		event, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		request := DeleteCalendarEventArguments{CalendarID: arguments.CalendarID, ID: event.ID, RequestID: "personal-delete"}
		for range 2 {
			if removed, err := resolver.DeleteCalendarEvent(ctx, request); err != nil || !removed {
				test.Fatalf("personal delete=%v, %v", removed, err)
			}
		}
	})
	if sender.acceptCount != 0 {
		test.Fatal("personal deletion sent mail")
	}
}
