package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func calendarProposalFixture(test *testing.T) (db.Database, *graph, *api.Principal, SaveCalendarEventArguments) {
	database, resolver, principal, arguments, _ := calendarEventFixture(test)
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}, {Permission: models.PermissionMailWrite}})
	proposal := models.MailProposal{Kind: "event", Summary: "Proposed", Starts: *arguments.StartsAt, Ends: *arguments.EndsAt}
	encoded, err := json.Marshal(proposal)
	if err != nil {
		test.Fatal(err)
	}
	arguments.RequestID = "proposal-request"
	arguments.ProposalItemID = dbtest.QueryString(test, database, `SELECT id FROM mailbox_item LIMIT 1`)
	arguments.ProposalIndex = new(0)
	arguments.ExpectedProposal = string(encoded)
	arguments.Attendees = nil
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		item, err := transaction.GetItem(arguments.ProposalItemID)
		if err != nil {
			test.Fatal(err)
		}
		mailboxes, err := transaction.ListMailboxes(principal.User.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.PutMailInsight(&models.MailInsight{MailID: item.MailID, MailboxID: mailboxes[0].ID, AgentID: "fixture-agent", Summary: "Unrelated insight", Proposals: []models.MailProposal{proposal}}); err != nil {
			test.Fatal(err)
		}
	})
	return database, resolver, principal, arguments
}

func TestCalendarProposalAcceptanceAndReceiptAreAtomic(test *testing.T) {
	for _, failurePoint := range []string{"status", "receipt", "parent", "none"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, resolver, principal, arguments := calendarProposalFixture(test)
			if failurePoint == "status" {
				dbtest.Exec(test, database, `CREATE FUNCTION refuse_proposal_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture status failure'; END $$`)
				dbtest.Exec(test, database, `CREATE TRIGGER refuse_proposal_update BEFORE UPDATE ON mail_insight FOR EACH ROW EXECUTE FUNCTION refuse_proposal_update()`)
			}
			if failurePoint == "receipt" {
				dbtest.Exec(test, database, `ALTER TABLE calendar_request ADD CONSTRAINT refuse_proposal_receipt CHECK (request_id <> 'proposal-request')`)
			}
			parentError := errors.New("parent rollback")
			err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
				event, err := resolver.SaveCalendarEvent(ctx, arguments)
				if failurePoint == "status" || failurePoint == "receipt" {
					if err == nil || event != nil {
						test.Fatal("failed acceptance succeeded")
					}
					return nil
				}
				if err != nil || event == nil {
					test.Fatalf("accepted=%+v, %v", event, err)
				}
				if failurePoint == "parent" {
					return parentError
				}
				replay, err := resolver.SaveCalendarEvent(ctx, arguments)
				if err != nil || replay == nil || replay.ID != event.ID {
					test.Fatalf("replay=%+v, %v", replay, err)
				}
				duplicate := arguments
				duplicate.RequestID = "second-request"
				if _, err := resolver.SaveCalendarEvent(ctx, duplicate); !errors.Is(err, api.ErrInvalidArguments) {
					test.Fatalf("second acceptance=%v", err)
				}
				return nil
			})
			if failurePoint == "parent" && !errors.Is(err, parentError) || failurePoint != "parent" && err != nil {
				test.Fatal(err)
			}
			expectedCount, expectedStatus := "0", ""
			if failurePoint == "none" {
				expectedCount, expectedStatus = "1", "accepted"
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`); count != expectedCount {
				test.Fatalf("events=%s", count)
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_request`); count != expectedCount {
				test.Fatalf("receipts=%s", count)
			}
			if status := dbtest.QueryString(test, database, `SELECT coalesce(proposals->0->>'status','') FROM mail_insight`); status != expectedStatus {
				test.Fatalf("proposal status=%q", status)
			}
			if summary := dbtest.QueryString(test, database, `SELECT summary FROM mail_insight`); summary != "Unrelated insight" {
				test.Fatalf("insight summary=%q", summary)
			}
		})
	}
}

func TestCalendarProposalRefusesChangedOfferAndRevokedPermission(test *testing.T) {
	database, resolver, principal, arguments := calendarProposalFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		stale := arguments
		stale.ExpectedProposal = `{"kind":"event","summary":"different"}`
		if _, err := resolver.SaveCalendarEvent(ctx, stale); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("stale offer=%v", err)
		}
		if _, err := resolver.SaveCalendarEvent(ctx, arguments); err != nil {
			test.Fatal(err)
		}
	})
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if _, err := resolver.SaveCalendarEvent(ctx, arguments); err == nil {
			test.Fatal("replay ignored mail-write revocation")
		}
		if _, err := resolver.GetCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID}); err == nil {
			test.Fatal("lookup ignored mail-write revocation")
		}
		if _, err := resolver.CancelCalendarRequest(ctx, CalendarRequestArguments{RequestID: arguments.RequestID}); err == nil {
			test.Fatal("cancellation ignored mail-write revocation")
		}
	})
}

func TestCalendarProposalConcurrentAcceptanceCreatesOneEvent(test *testing.T) {
	database, resolver, principal, arguments := calendarProposalFixture(test)
	ctx, cancel := context.WithTimeout(test.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for _, requestId := range []string{"first-acceptance", "second-acceptance"} {
		go func() {
			<-start
			attempt := arguments
			attempt.RequestID = requestId
			outcomes <- database.TransactionContext(ctx, func(transaction db.Transaction) error {
				commandContext := api.ContextWithTransaction(api.ContextWithPrincipal(ctx, principal), transaction)
				_, err := resolver.SaveCalendarEvent(commandContext, attempt)
				return err
			})
		}()
	}
	close(start)
	acceptedCount := 0
	for range 2 {
		err := <-outcomes
		if err == nil {
			acceptedCount++
		} else if !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatal(err)
		}
	}
	if acceptedCount != 1 {
		test.Fatalf("accepted requests=%d", acceptedCount)
	}
	if eventCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`); eventCount != "1" {
		test.Fatalf("events=%s", eventCount)
	}
	if receiptCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_request`); receiptCount != "1" {
		test.Fatalf("receipts=%s", receiptCount)
	}
}
