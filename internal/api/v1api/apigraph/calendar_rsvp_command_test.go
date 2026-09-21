package apigraph

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestInvitationAnswerAndReplyAcceptanceCommitTogether(test *testing.T) {
	for _, failurePoint := range []string{"acceptance", "parent", "permission", "none"} {
		test.Run(failurePoint, func(test *testing.T) {
			database, resolver, principal, eventArguments, sender := calendarEventFixture(test)
			principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}, {Permission: models.PermissionMailRead}, {Permission: models.PermissionMailSend}})
			if failurePoint == "permission" {
				principal.Permissions = models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCalendarUse}, {Permission: models.PermissionMailRead}})
			}
			itemId := dbtest.QueryString(test, database, `SELECT id FROM mailbox_item LIMIT 1`)
			var original *models.CalendarObject
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				startsAt := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
				parsed, err := calendar.Build(nil, &calendar.Fields{Summary: new("Invited meeting"), StartsAt: &startsAt, EndsAt: new(startsAt.Add(time.Hour)), Organizer: "organizer@example.net", Attendees: new([]calendar.Attendee{{Address: "sender@example.com", Participation: "NEEDS-ACTION"}})})
				if err != nil {
					test.Fatal(err)
				}
				occurrences, indexedUntil, err := calendar.Indexed(parsed)
				if err != nil {
					test.Fatal(err)
				}
				rows := make([]models.Occurrence, 0, len(occurrences))
				for _, occurrence := range occurrences {
					rows = append(rows, models.Occurrence{StartsAt: occurrence.StartsAt, EndsAt: occurrence.EndsAt, AllDay: occurrence.AllDay})
				}
				original, err = transaction.PutCalendarObject(&models.CalendarObject{CalendarID: eventArguments.CalendarID, UID: parsed.UID, ETag: calendar.ETag(parsed.Data), Data: string(parsed.Data), Summary: parsed.Summary, StartsAt: parsed.StartsAt, EndsAt: parsed.EndsAt, IndexedUntil: &indexedUntil}, rows)
				if err != nil {
					test.Fatal(err)
				}
				mailboxes, err := transaction.ListMailboxes(principal.User.ID)
				if err != nil {
					test.Fatal(err)
				}
				item, err := transaction.GetItem(itemId)
				if err != nil {
					test.Fatal(err)
				}
				invitation, err := transaction.NoteCalendarInvitation(&models.CalendarInvitation{UserID: principal.User.ID, MailboxID: mailboxes[0].ID, ItemID: item.ID, MailID: item.MailID})
				if err != nil {
					test.Fatal(err)
				}
				invitation.Status = models.CalendarInvitationRead
				invitation.Method = models.CalendarMethodRequest
				invitation.UID = parsed.UID
				invitation.CalendarID = original.CalendarID
				invitation.ObjectID = original.ID
				invitation.Organizer = parsed.Organizer
				if err := transaction.FinishCalendarInvitation(invitation); err != nil {
					test.Fatal(err)
				}
			})
			interrupted := errors.New("fixture interrupted")
			if failurePoint == "acceptance" {
				sender.acceptError = interrupted
			}
			err := database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
				ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), principal), transaction)
				answered, err := resolver.AnswerMailInvitation(ctx, AnswerMailInvitationArguments{ItemID: itemId, Answer: "accepted"})
				if failurePoint == "acceptance" || failurePoint == "permission" {
					if err == nil || answered != nil {
						test.Fatalf("failed answer=%+v, %v", answered, err)
					}
					return nil
				}
				if err != nil || answered == nil || answered.Participation != calendar.Accepted {
					test.Fatalf("answer=%+v, %v", answered, err)
				}
				if sender.commitCount != 0 {
					test.Fatal("reply escaped before commit")
				}
				if failurePoint == "parent" {
					return interrupted
				}
				return nil
			})
			if failurePoint == "parent" && !errors.Is(err, interrupted) || failurePoint != "parent" && err != nil {
				test.Fatal(err)
			}
			expectedMail, expectedCommits := "1", 0
			if failurePoint == "none" {
				expectedMail, expectedCommits = "2", 1
			}
			if dbtest.QueryString(test, database, `SELECT count(*)::text FROM mail`) != expectedMail || sender.commitCount != expectedCommits {
				test.Fatal("reply acceptance escaped answer rollback")
			}
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				stored, err := transaction.GetCalendarObject(original.CalendarID, original.ID)
				if err != nil {
					test.Fatal(err)
				}
				if failurePoint != "none" && stored.Data != original.Data {
					test.Fatal("answer escaped rollback")
				}
			})
		})
	}
}
