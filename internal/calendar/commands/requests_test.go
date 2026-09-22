package commands

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestCalendarRequestConcurrentReplayAndRetention(test *testing.T) {
	database, principal, storedCalendar := calendarCommandFixture(test)
	request := RequestIdentity{RequestID: "retained-request", CalendarID: storedCalendar.ID, Operation: "save", Content: []byte(`{"title":"Meeting"}`)}
	var executionCount atomic.Int32
	var callbackCount atomic.Int32
	execute := func(ctx context.Context, transaction db.Transaction) (RequestResult, error) {
		executionCount.Add(1)
		_, err := New(transaction).Update(ctx, principal, UpdateRequest{ID: storedCalendar.ID, Name: "Accepted"})
		if err != nil {
			return RequestResult{}, err
		}
		transaction.AfterCommit(func() { callbackCount.Add(1) })
		return RequestResult{ObjectID: "event-result"}, nil
	}
	const workerCount = 8
	outcomes := make(chan *RequestOutcome, workerCount)
	failures := make(chan error, workerCount)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			outcome, err := New(database).ExecuteRequest(test.Context(), principal, request, execute)
			outcomes <- outcome
			failures <- err
		})
	}
	workers.Wait()
	close(outcomes)
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	freshCount := 0
	for outcome := range outcomes {
		if outcome == nil || outcome.Receipt.ObjectID != "event-result" {
			test.Fatalf("outcome=%+v", outcome)
		}
		if !outcome.IsReplay {
			freshCount++
		}
	}
	if executionCount.Load() != 1 || callbackCount.Load() != 1 || freshCount != 1 {
		test.Fatalf("executions=%d callbacks=%d fresh=%d", executionCount.Load(), callbackCount.Load(), freshCount)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteCalendar(principal.User.ID, storedCalendar.ID); err != nil {
			test.Fatal(err)
		}
	})
	outcome, err := New(database).ExecuteRequest(test.Context(), principal, request, nil)
	if err != nil || outcome == nil || !outcome.IsReplay || outcome.Receipt.ObjectID != "event-result" {
		test.Fatalf("retained replay=%+v, %v", outcome, err)
	}
	for _, changed := range []RequestIdentity{
		{RequestID: request.RequestID, CalendarID: request.CalendarID, Operation: "save", Content: []byte(`{"title":"Changed"}`)},
		{RequestID: request.RequestID, CalendarID: request.CalendarID, Operation: "delete", Content: request.Content},
		{RequestID: request.RequestID, CalendarID: "another-calendar", Operation: request.Operation, Content: request.Content},
	} {
		if _, err := New(database).ExecuteRequest(test.Context(), principal, changed, nil); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("changed identity=%v", err)
		}
	}
	for _, denied := range []*access.Principal{nil, {User: principal.User, Permissions: models.NewEffectivePermissions(nil)}, {User: &models.User{ID: "different-owner"}, Permissions: principal.Permissions}} {
		if _, err := New(database).ExecuteRequest(test.Context(), denied, request, nil); !errors.Is(err, db.ErrNotFound) {
			test.Fatalf("denied replay=%v", err)
		}
	}
}

func TestCalendarRequestRollback(test *testing.T) {
	for _, failureKind := range []string{"action", "receipt", "parent"} {
		test.Run(failureKind, func(test *testing.T) {
			database, principal, storedCalendar := calendarCommandFixture(test)
			request := RequestIdentity{RequestID: "rollback-request", CalendarID: storedCalendar.ID, Operation: "answer", Content: []byte(`{"participation":"accepted"}`)}
			if failureKind == "receipt" {
				dbtest.Exec(test, database, `ALTER TABLE calendar_request ADD CONSTRAINT reject_fixture CHECK (request_id <> 'rollback-request')`)
			}
			expectedError := errors.New("injected failure")
			var callbackCount atomic.Int32
			err := database.TransactionContext(test.Context(), func(parent db.Transaction) error {
				_, err := New(parent).ExecuteRequest(test.Context(), principal, request, func(ctx context.Context, transaction db.Transaction) (RequestResult, error) {
					_, err := New(transaction).Update(ctx, principal, UpdateRequest{ID: storedCalendar.ID, Name: "Must roll back"})
					if err != nil {
						return RequestResult{}, err
					}
					transaction.AfterCommit(func() { callbackCount.Add(1) })
					if failureKind == "action" {
						return RequestResult{}, expectedError
					}
					return RequestResult{ObjectID: "event-result"}, nil
				})
				if failureKind == "parent" {
					if err != nil {
						test.Fatal(err)
					}
					return expectedError
				}
				if err == nil {
					test.Fatal("command unexpectedly succeeded")
				}
				// A failed command must leave its caller usable and able to commit.
				_, err = parent.GetCalendar(storedCalendar.ID)
				return err
			})
			if failureKind == "parent" {
				if !errors.Is(err, expectedError) {
					test.Fatal(err)
				}
			} else if err != nil {
				test.Fatal(err)
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				kept, err := transaction.GetCalendar(storedCalendar.ID)
				if err != nil || kept == nil || kept.Name != "Original" {
					test.Fatalf("calendar=%+v, %v", kept, err)
				}
				receipt, err := transaction.GetCalendarRequest(principal.User.ID, request.RequestID)
				if err != nil || receipt != nil {
					test.Fatalf("receipt=%+v, %v", receipt, err)
				}
			})
			if callbackCount.Load() != 0 {
				test.Fatalf("rolled-back callbacks=%d", callbackCount.Load())
			}
		})
	}
}

func TestCalendarRequestReadDoesNotWaitAndLockCancels(test *testing.T) {
	database, principal, storedCalendar := calendarCommandFixture(test)
	request := RequestIdentity{RequestID: "pending-request", CalendarID: storedCalendar.ID, Operation: "save", Content: []byte(`{}`)}
	hasLock := make(chan struct{})
	canFinish := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := New(database).ExecuteRequest(test.Context(), principal, request, func(_ context.Context, _ db.Transaction) (RequestResult, error) {
			close(hasLock)
			<-canFinish
			return RequestResult{ObjectID: "event-result"}, nil
		})
		finished <- err
	}()
	<-hasLock
	defer func() {
		close(canFinish)
		if err := <-finished; err != nil {
			test.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(test.Context(), time.Second)
	defer cancel()
	err := database.TransactionContext(ctx, func(transaction db.Transaction) error {
		receipt, err := transaction.GetCalendarRequest(principal.User.ID, request.RequestID)
		if receipt != nil {
			test.Error("uncommitted receipt was visible")
		}
		return err
	})
	if err != nil {
		test.Fatalf("nonblocking lookup=%v", err)
	}
	lockContext, cancelLock := context.WithTimeout(test.Context(), 50*time.Millisecond)
	defer cancelLock()
	if _, err := New(database).ExecuteRequest(lockContext, principal, request, nil); err == nil {
		test.Fatal("waiting request ignored cancellation")
	}
}
