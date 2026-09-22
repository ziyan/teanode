package commands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCancelledCalendarRequestCannotRunLater(test *testing.T) {
	database, principal, storedCalendar := calendarCommandFixture(test)
	for range 2 {
		receipt, err := New(database).CancelRequest(test.Context(), principal, "cancelled-request")
		if err != nil || receipt != nil {
			test.Fatalf("cancellation=%+v, %v", receipt, err)
		}
	}
	_, err := New(database).ExecuteRequest(test.Context(), principal, RequestIdentity{RequestID: "cancelled-request", CalendarID: storedCalendar.ID, Operation: "save", Content: []byte(`{}`)}, func(context.Context, db.Transaction) (RequestResult, error) {
		test.Error("cancelled action executed")
		return RequestResult{ObjectID: "event"}, nil
	})
	if !errors.Is(err, db.ErrInvalidArguments) {
		test.Fatalf("late request=%v", err)
	}
}

func TestCalendarCancellationWaitsForCommitOrRollback(test *testing.T) {
	for _, shouldCommit := range []bool{false, true} {
		test.Run(map[bool]string{false: "rollback", true: "commit"}[shouldCommit], func(test *testing.T) {
			database, principal, storedCalendar := calendarCommandFixture(test)
			request := RequestIdentity{RequestID: "racing-request", CalendarID: storedCalendar.ID, Operation: "save", Content: []byte(`{}`)}
			hasExecuted := make(chan struct{})
			canFinish := make(chan struct{})
			executionDone := make(chan error, 1)
			rollbackError := errors.New("roll back parent")
			go func() {
				executionDone <- database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
					_, err := New(transaction).ExecuteRequest(test.Context(), principal, request, func(context.Context, db.Transaction) (RequestResult, error) {
						return RequestResult{ObjectID: "event"}, nil
					})
					close(hasExecuted)
					<-canFinish
					if err != nil {
						return err
					}
					if !shouldCommit {
						return rollbackError
					}
					return nil
				})
			}()
			<-hasExecuted
			type cancellationResult struct {
				receipt *models.CalendarRequestReceipt
				err     error
			}
			cancellationDone := make(chan cancellationResult, 1)
			ctx, cancel := context.WithTimeout(test.Context(), 5*time.Second)
			defer cancel()
			go func() {
				receipt, err := New(database).CancelRequest(ctx, principal, request.RequestID)
				cancellationDone <- cancellationResult{receipt, err}
			}()
			select {
			case result := <-cancellationDone:
				close(canFinish)
				<-executionDone
				test.Fatalf("cancellation did not wait: %+v", result)
			case <-time.After(50 * time.Millisecond):
			}
			close(canFinish)
			err := <-executionDone
			if shouldCommit && err != nil || !shouldCommit && !errors.Is(err, rollbackError) {
				test.Fatal(err)
			}
			cancelled := <-cancellationDone
			if cancelled.err != nil || (cancelled.receipt != nil) != shouldCommit {
				test.Fatalf("resolution=%+v", cancelled)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				isCancelled, err := transaction.IsCalendarRequestCancelled(principal.User.ID, request.RequestID)
				if err != nil || isCancelled == shouldCommit {
					test.Fatalf("cancelled=%v, %v", isCancelled, err)
				}
			})
		})
	}
}

func TestCalendarCancellationRollsBackWithItsCaller(test *testing.T) {
	database, principal, _ := calendarCommandFixture(test)
	rollbackError := errors.New("roll back cancellation")
	err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		if _, err := New(transaction).CancelRequest(test.Context(), principal, "rolled-back"); err != nil {
			return err
		}
		return rollbackError
	})
	if !errors.Is(err, rollbackError) {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		isCancelled, err := transaction.IsCalendarRequestCancelled(principal.User.ID, "rolled-back")
		if err != nil || isCancelled {
			test.Fatalf("cancelled=%v, %v", isCancelled, err)
		}
	})
}
