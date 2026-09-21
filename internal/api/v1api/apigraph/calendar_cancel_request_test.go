package apigraph

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestCalendarCancellationAPIStopsFreshRequestAndReturnsCompletion(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	arguments.RequestID = "stopped"
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		receipt, err := resolver.CancelCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
		if err != nil || receipt != nil {
			test.Fatalf("cancelled=%+v, %v", receipt, err)
		}
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.SaveCalendarEvent(ctx, arguments); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("late save=%v", err)
		}
		arguments.RequestID = "completed"
		event, err := resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
		receipt, err := resolver.CancelCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID})
		if err != nil || receipt == nil || receipt.ObjectID != event.ID || receipt.IsMissing {
			test.Fatalf("already completed=%+v, %v", receipt, err)
		}
	})
	if sender.acceptCount != 1 || sender.commitCount != 1 {
		test.Fatalf("accepted=%d committed=%d", sender.acceptCount, sender.commitCount)
	}
}
