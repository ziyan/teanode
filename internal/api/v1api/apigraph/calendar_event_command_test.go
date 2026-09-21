package apigraph

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type eventAcceptanceMailer struct {
	mailer.Mailer
	acceptError error
	acceptCount int
	commitCount int
}

func (self *eventAcceptanceMailer) AcceptSubmission(_ context.Context, transaction db.Transaction, envelope *mailparse.Envelope, message *mailer.Message) (*models.Mail, error) {
	if envelope.MailboxID == "" {
		return nil, errors.New("invitation has no sending mailbox")
	}
	self.acceptCount++
	stored, err := transaction.CreateMail(&models.Mail{Subject: message.Subject}, nil)
	if err != nil {
		return nil, err
	}
	transaction.AfterCommit(func() { self.commitCount++ })
	if self.acceptError != nil {
		return nil, self.acceptError
	}
	return stored, nil
}

func calendarEventFixture(test *testing.T) (db.Database, *graph, *api.Principal, SaveCalendarEventArguments, *eventAcceptanceMailer) {
	test.Helper()
	database, resolver, principal, _, _ := submissionAPIFixture(test)
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}, {Permission: models.PermissionMailSend}})
	arguments := SaveCalendarEventArguments{Summary: new("Fixture meeting"), StartsAt: new("2030-01-02T10:00:00Z"), EndsAt: new("2030-01-02T11:00:00Z"), Attendees: new([]string{"guest@example.net"})}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		storedCalendar, err := transaction.CreateCalendar(&models.Calendar{UserID: principal.User.ID, Name: "Fixture"})
		if err != nil {
			test.Fatal(err)
		}
		arguments.CalendarID = storedCalendar.ID
	})
	sender := &eventAcceptanceMailer{}
	resolver.mailer = sender
	return database, resolver, principal, arguments, sender
}

func TestCalendarEventAndInvitationAcceptanceRollBackTogether(test *testing.T) {
	for _, failurePoint := range []string{"acceptance", "parent", "none"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, resolver, principal, arguments, sender := calendarEventFixture(test)
			interrupted := errors.New("fixture interrupted")
			if failurePoint == "acceptance" {
				sender.acceptError = interrupted
			}
			err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
				saved, err := resolver.SaveCalendarEvent(ctx, arguments)
				if failurePoint == "acceptance" {
					if !errors.Is(err, interrupted) || saved != nil {
						test.Fatalf("acceptance failure=%+v, %v", saved, err)
					}
					return nil
				}
				if err != nil || saved == nil {
					test.Fatalf("save=%+v, %v", saved, err)
				}
				if sender.commitCount != 0 {
					test.Fatal("notification escaped before commit")
				}
				if failurePoint == "parent" {
					return interrupted
				}
				return nil
			})
			if failurePoint == "parent" && !errors.Is(err, interrupted) || failurePoint != "parent" && err != nil {
				test.Fatal(err)
			}
			expectedEvents, expectedMail, expectedCommits := "0", "1", 0
			if failurePoint == "none" {
				expectedEvents, expectedMail, expectedCommits = "1", "2", 1
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`); count != expectedEvents {
				test.Fatalf("events=%s", count)
			}
			if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`); count != expectedMail {
				test.Fatalf("mail=%s", count)
			}
			if sender.acceptCount != 1 || sender.commitCount != expectedCommits {
				test.Fatalf("accepts=%d, commits=%d", sender.acceptCount, sender.commitCount)
			}
		})
	}
}

func TestCalendarCancellationRollsBackWhenDeletionFails(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	var saved *CalendarEventView
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		var err error
		saved, err = resolver.SaveCalendarEvent(ctx, arguments)
		if err != nil {
			test.Fatal(err)
		}
	})
	dbtest.Exec(test, database, `CREATE FUNCTION refuse_event_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture deletion refused'; END $$`)
	dbtest.Exec(test, database, `CREATE TRIGGER refuse_event_delete BEFORE DELETE ON calendar_object FOR EACH ROW EXECUTE FUNCTION refuse_event_delete()`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if removed, err := resolver.DeleteCalendarEvent(ctx, CalendarEventArguments{CalendarID: arguments.CalendarID, ID: saved.ID}); err == nil || removed {
			test.Fatalf("delete=%v, %v", removed, err)
		}
	})
	if sender.commitCount != 1 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != "2" || dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`) != "1" {
		test.Fatal("failed deletion committed cancellation or removed event")
	}
	dbtest.Exec(test, database, `DROP TRIGGER refuse_event_delete ON calendar_object`)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if removed, err := resolver.DeleteCalendarEvent(ctx, CalendarEventArguments{CalendarID: arguments.CalendarID, ID: saved.ID}); err != nil || !removed {
			test.Fatalf("delete=%v, %v", removed, err)
		}
	})
	if sender.commitCount != 2 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`) != "0" {
		test.Fatal("deletion and cancellation did not commit together")
	}
}

func TestCalendarUnchangedSaveAndUIDReplayDoNotInviteAgain(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	var saved *CalendarEventView
	for iteration := range 3 {
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
			request := arguments
			if iteration == 1 {
				request.ID = saved.ID
			}
			if iteration == 2 {
				request = SaveCalendarEventArguments{CalendarID: arguments.CalendarID, File: saved.File}
			}
			updated, err := resolver.SaveCalendarEvent(ctx, request)
			if err != nil {
				test.Fatal(err)
			}
			if saved != nil && saved.ID != updated.ID {
				test.Fatal("UID replay created another event")
			}
			saved = updated
		})
	}
	if sender.acceptCount != 1 || sender.commitCount != 1 {
		test.Fatalf("unchanged event reinvited: accepts=%d, commits=%d", sender.acceptCount, sender.commitCount)
	}
}

func TestCalendarUseDoesNotAuthorizeInvitationMail(test *testing.T) {
	database, resolver, principal, arguments, sender := calendarEventFixture(test)
	principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
		if saved, err := resolver.SaveCalendarEvent(ctx, arguments); err == nil || saved != nil {
			test.Fatalf("invitation permission=%+v, %v", saved, err)
		}
		arguments.Attendees = nil
		if saved, err := resolver.SaveCalendarEvent(ctx, arguments); err != nil || saved == nil {
			test.Fatalf("personal event=%+v, %v", saved, err)
		}
	})
	if sender.acceptCount != 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM calendar_object`) != "1" {
		test.Fatal("mail permission failure accepted mail or retained the meeting")
	}
}
