package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCalendarSaveRequestReplaySurvivesChangesAndDeletion(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	arguments.RequestID = "retained-create"
	var eventId string
	for attempt := range 2 {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			saved, err := resolver.SaveCalendarEvent(ctx, arguments)
			if err != nil || saved == nil {
				test.Fatalf("save=%+v, %v", saved, err)
			}
			if attempt == 0 {
				eventId = saved.ID
			} else if saved.ID != eventId {
				test.Fatal("replay created another event")
			}
			receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
			if err != nil || receipt == nil || receipt.ObjectID != eventId || receipt.IsMissing {
				test.Fatalf("receipt=%+v, %v", receipt, err)
			}
		})
	}
	if sender.acceptCount != 1 || sender.commitCount != 1 {
		test.Fatalf("accepts=%d commits=%d", sender.acceptCount, sender.commitCount)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		edit := arguments
		edit.RequestID = "later-edit"
		edit.ID = eventId
		edit.Summary = new("Later title")
		if _, err := resolver.SaveCalendarEvent(ctx, edit); err != nil {
			test.Fatal(err)
		}
		replay, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil || replay == nil || replay.Summary != "Later title" {
			test.Fatalf("replay overwrote later content: %+v, %v", replay, err)
		}
		conflict := arguments
		conflict.Summary = new("Changed retry")
		if _, err := resolver.SaveCalendarEvent(ctx, conflict); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("changed request=%v", err)
		}
		if err := transaction.DeleteCalendar(principal.User.ID, arguments.CalendarID); err != nil {
			test.Fatal(err)
		}
	})
	priorAcceptCount := sender.acceptCount
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		replay, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil || replay != nil {
			test.Fatalf("deleted result=%+v, %v", replay, err)
		}
		receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
		if err != nil || receipt == nil || !receipt.IsMissing || receipt.ObjectID != eventId {
			test.Fatalf("deleted completion=%+v, %v", receipt, err)
		}
	})
	if sender.acceptCount != priorAcceptCount {
		test.Fatal("deleted result replay sent more mail")
	}
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.SaveCalendarEvent(ctx, arguments); err == nil {
			test.Fatal("replay ignored revoked send permission")
		}
		if _, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID}); err == nil {
			test.Fatal("lookup ignored revoked send permission")
		}
	})
}

func TestCalendarSaveRequestRollbackAndPersonalPermission(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	arguments.RequestID = "retry-after-failure"
	sender.acceptError = errors.New("fixture acceptance failure")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.SaveCalendarEvent(ctx, arguments); err == nil {
			test.Fatal("acceptance unexpectedly succeeded")
		}
		receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
		if err != nil || receipt != nil {
			test.Fatalf("failed receipt=%+v, %v", receipt, err)
		}
	})
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`); count != "0" {
		test.Fatalf("events=%s", count)
	}
	sender.acceptError = nil
	arguments.Attendees = nil
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	for range 2 {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			saved, err := resolver.SaveCalendarEvent(ctx, arguments)
			if err != nil || saved == nil {
				test.Fatalf("personal save=%+v, %v", saved, err)
			}
			receipt, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
			if err != nil || receipt == nil {
				test.Fatalf("personal receipt=%+v, %v", receipt, err)
			}
			other := &api.Principal{User: &models.User{ID: "another-owner"}, Permissions: principal.Permissions}
			otherContext := api.ContextWithPrincipal(ctx, other)
			if receipt, err := resolver.GetCalendarRequest(otherContext, CalendarRequestArguments{RequestID: arguments.RequestID}); err != nil || receipt != nil {
				test.Fatalf("other account=%+v, %v", receipt, err)
			}
		})
	}
	if sender.commitCount != 0 {
		test.Fatal("personal event sent mail")
	}
}

func TestCalendarRequestSchemaSupportsLegacyAndIdentifiedSave(test *testing.T) {
	resolver := &graph{schema: buildSchemaForValidation(test)}
	for _, document := range []string{
		client.DocumentSaveCalendarEvent,
		client.DocumentSaveCalendarEventWithRequest,
		client.DocumentGetCalendarRequest,
		`mutation ($requestId: String!) { CancelCalendarRequest(requestId: $requestId) { requestId isMissing } }`,
		client.DocumentDeleteCalendarEvent,
		client.DocumentDeleteCalendarEventWithRequest,
	} {
		if _, rejected := resolver.prepareGraphRequest(&graphRequest{Query: document}); rejected != nil {
			test.Fatalf("schema rejected request: %+v", rejected)
		}
	}
}
