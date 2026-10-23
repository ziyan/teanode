package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An editor is shown an event its owner marked private only as busy, so they
// may neither change nor remove it: the owner can do both, and an editor can
// still change an ordinary event.
func TestAnEditorCannotChangeOrRemoveAPrivateEvent(test *testing.T) {
	database, ownerPrincipal, ownerCalendar := calendarCommandFixture(test)
	editorId := dbtest.CreateUser(test, database, "calendar-editor")
	editorPrincipal := &access.Principal{User: &models.User{ID: editorId}, Permissions: ownerPrincipal.Permissions}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		dbtest.GrantPermission(test, transaction, editorId, models.PermissionCalendarUse)
		circle, err := transaction.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, ownerPrincipal.User.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AddCircleMember(circle.ID, editorId, models.MemberRoleMember, ownerPrincipal.User.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.ShareCalendar(ownerPrincipal.User.ID, &models.CalendarShare{CalendarID: ownerCalendar.ID, GranteeUserID: editorId, ShareLevel: models.ShareLevelEdit}); err != nil {
			test.Fatal(err)
		}
	})
	commands := New(database)
	ctx := test.Context()
	starts := time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)
	save := func(principal *access.Principal, id, summary string, isPrivate bool) (*models.CalendarObject, error) {
		return commands.SaveEvent(ctx, principal, EventRequest{CalendarID: ownerCalendar.ID, ID: id}, func(_ context.Context, _ db.Transaction, existing *models.CalendarObject) (*calendar.Parsed, error) {
			var base []byte
			if existing != nil {
				base = []byte(existing.Data)
			}
			return calendar.Build(base, &calendar.Fields{Summary: &summary, StartsAt: &starts, IsPrivate: &isPrivate})
		}, nil)
	}
	secret, err := save(ownerPrincipal, "", "Surprise party", true)
	if err != nil {
		test.Fatal(err)
	}
	plain, err := save(ownerPrincipal, "", "Library", false)
	if err != nil {
		test.Fatal(err)
	}

	if _, err := save(editorPrincipal, secret.ID, "Busy", true); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "private to its owner") {
		test.Fatalf("an editor changing a private event: %v", err)
	}
	if err := commands.DeleteEvent(ctx, editorPrincipal, EventRequest{CalendarID: ownerCalendar.ID, ID: secret.ID}, nil); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "private to its owner") {
		test.Fatalf("an editor removing a private event: %v", err)
	}
	// Writing the same identifier without naming the event is the same event.
	if _, err := commands.SaveEvent(ctx, editorPrincipal, EventRequest{CalendarID: ownerCalendar.ID}, func(_ context.Context, _ db.Transaction, _ *models.CalendarObject) (*calendar.Parsed, error) {
		return calendar.Parse([]byte(strings.Replace(secret.Data, "Surprise party", "Busy", 1)))
	}, nil); !errors.Is(err, ErrRefused) {
		test.Fatalf("an editor writing over a private event by its identifier: %v", err)
	}
	if kept, err := save(editorPrincipal, plain.ID, "Library moved", false); err != nil || !strings.Contains(kept.Data, "Library moved") {
		test.Fatalf("an editor changes an ordinary event: %v", err)
	}
	if _, err := save(ownerPrincipal, secret.ID, "Surprise party, moved", true); err != nil {
		test.Fatalf("its owner changes it: %v", err)
	}
	if err := commands.DeleteEvent(ctx, ownerPrincipal, EventRequest{CalendarID: ownerCalendar.ID, ID: secret.ID}, nil); err != nil {
		test.Fatalf("and removes it: %v", err)
	}
}
