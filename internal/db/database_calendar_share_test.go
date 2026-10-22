package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A world of four people: ada and ben share the circle "home", ben and cora
// share "club", and dave is in none. Ada's events calendar is the one shared.
type shareWorld struct {
	ada, ben, cora, dave *models.User
	home, club           *models.Circle
	adaCalendar          *models.Calendar
}

func newShareWorld(t *testing.T, tx db.Transaction) *shareWorld {
	t.Helper()
	world := &shareWorld{}
	for username, target := range map[string]**models.User{
		"ada": &world.ada, "ben": &world.ben, "cora": &world.cora, "dave": &world.dave,
	} {
		made, err := tx.CreateUser(&models.User{Username: username})
		if err != nil {
			t.Fatalf("CreateUser %s: %s", username, err)
		}
		*target = made
	}
	var err error
	if world.home, err = tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, world.ada.ID); err != nil {
		t.Fatalf("CreateCircle: %s", err)
	}
	if _, err = tx.AddCircleMember(world.home.ID, world.ben.ID, models.MemberRoleMember, world.ada.ID); err != nil {
		t.Fatalf("AddCircleMember: %s", err)
	}
	if world.club, err = tx.CreateCircle(&models.Circle{Name: "Club", CircleKind: models.CircleKindFriends}, world.ben.ID); err != nil {
		t.Fatalf("CreateCircle: %s", err)
	}
	if _, err = tx.AddCircleMember(world.club.ID, world.cora.ID, models.MemberRoleMember, world.ben.ID); err != nil {
		t.Fatalf("AddCircleMember: %s", err)
	}
	if world.adaCalendar, err = tx.EnsureCalendar(world.ada.ID, models.CalendarEvents, nil); err != nil {
		t.Fatalf("EnsureCalendar: %s", err)
	}
	return world
}

// newCalendarShareWorld is that world with calendar:use given to all four, as
// the ordinary roles give it: a calendar reaches only people who hold it.
func newCalendarShareWorld(t *testing.T, tx db.Transaction) *shareWorld {
	t.Helper()
	world := newShareWorld(t, tx)
	for _, person := range []*models.User{world.ada, world.ben, world.cora, world.dave} {
		dbtest.GrantPermission(t, tx, person.ID, models.PermissionCalendarUse)
	}
	return world
}

func accessOf(t *testing.T, tx db.Transaction, userId string, found *models.Calendar) models.ShareLevel {
	t.Helper()
	level, isOwner, err := tx.CalendarAccess(userId, found)
	if err != nil {
		t.Fatalf("CalendarAccess: %s", err)
	}
	if isOwner != (level == models.ShareLevelOwner) {
		t.Fatalf("isOwner %v does not agree with the level %q", isOwner, level)
	}
	return level
}

// Sharing needs a circle in common, and says not found rather than why.
func TestAShareNeedsACircleInCommon(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)

		// With ben, who shares "home" with ada.
		share, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelTitles,
		})
		if err != nil || share.ID == "" || share.ShareLevel != models.ShareLevelTitles {
			t.Fatalf("a share with someone in a shared circle: %+v %v", share, err)
		}
		// With cora and dave, who share none with her.
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
				CalendarID: world.adaCalendar.ID, GranteeUserID: stranger.ID, ShareLevel: models.ShareLevelBusy,
			}); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		// With a circle she is in, and with one she is not.
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelBusy,
		}); err != nil {
			t.Fatalf("a share with her own circle: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeCircleID: world.club.ID, ShareLevel: models.ShareLevelBusy,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		// Somebody else's calendar is not hers to share, and neither is her
		// own reminders list.
		benCalendar, err := tx.EnsureCalendar(world.ben.ID, models.CalendarEvents, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: benCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelBusy,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("somebody else's calendar: %v", err)
		}
		reminders, err := tx.EnsureCalendar(world.ada.ID, models.CalendarReminders, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: reminders.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelBusy,
		}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a reminders list is never shared: %v", err)
		}
		// A level that cannot be given, a grantee that is both, and herself.
		for name, bad := range map[string]*models.CalendarShare{
			"owner":    {CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelOwner},
			"empty":    {CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID},
			"both":     {CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelBusy},
			"neither":  {CalendarID: world.adaCalendar.ID, ShareLevel: models.ShareLevelBusy},
			"herself":  {CalendarID: world.adaCalendar.ID, GranteeUserID: world.ada.ID, ShareLevel: models.ShareLevelBusy},
			"stranger": {CalendarID: world.adaCalendar.ID, GranteeUserID: "nobody", ShareLevel: models.ShareLevelBusy},
		} {
			if _, err := tx.ShareCalendar(world.ada.ID, bad); err == nil {
				t.Errorf("a share with %s is refused", name)
			}
		}
	})
}

// Sharing again changes the level instead of adding a second share, and the
// reader gets the highest of what reaches them.
func TestAReaderGetsTheHighestLevelReachingThem(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != "" {
			t.Fatalf("nothing is shared yet: %q", level)
		}
		if level := accessOf(t, tx, world.ada.ID, world.adaCalendar); level != models.ShareLevelOwner {
			t.Fatalf("the owner: %q", level)
		}

		first, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelBusy,
		})
		if err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		second, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelDetails,
		})
		if err != nil || second.ID != first.ID || second.ShareLevel != models.ShareLevelDetails {
			t.Fatalf("sharing again changes the level: %+v %v", second, err)
		}
		shares, err := tx.ListCalendarShares(world.adaCalendar.ID)
		if err != nil || len(shares) != 1 || shares[0].GranteeUsername != "ben" {
			t.Fatalf("one share, for ben: %+v %v", shares, err)
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Fatalf("the direct share: %q", level)
		}

		// And the circle's, which is higher.
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelEdit,
		}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelEdit {
			t.Fatalf("the highest: %q", level)
		}
		if level := accessOf(t, tx, world.cora.ID, world.adaCalendar); level != "" {
			t.Fatalf("cora is in neither: %q", level)
		}

		// Taking the circle's share back leaves the direct one.
		shares, _ = tx.ListCalendarShares(world.adaCalendar.ID)
		for _, share := range shares {
			if share.GranteeCircleID != "" {
				if err := tx.DeleteCalendarShare(share.ID); err != nil {
					t.Fatalf("DeleteCalendarShare: %s", err)
				}
			}
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Fatalf("back to the direct share: %q", level)
		}
		if err := tx.DeleteCalendarShare("nobody"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("deleting nothing: %v", err)
		}
	})
}

// A share to a circle reaches a member only while the owner is still in it,
// and a share to a person only while the two share a circle, whether or not
// anything has cleaned the rows up yet.
func TestSharesStopReachingWhenTheCircleIsGone(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		for _, share := range []*models.CalendarShare{
			{CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelTitles},
			{CalendarID: world.adaCalendar.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelDetails},
		} {
			if _, err := tx.ShareCalendar(world.ada.ID, share); err != nil {
				t.Fatalf("ShareCalendar: %s", err)
			}
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Fatalf("before: %q", level)
		}

		// Ada leaves "home" (ben stays, and is its admin once ada hands it on).
		if _, err := tx.SetCircleMemberRole(world.home.ID, world.ben.ID, models.MemberRoleAdmin); err != nil {
			t.Fatalf("SetCircleMemberRole: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		// Not yet cleaned up, and already not reaching.
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != "" {
			t.Fatalf("after she left, before any cleanup: %q", level)
		}
		reachable, err := tx.ListReachableCalendars(world.ben.ID)
		if err != nil {
			t.Fatalf("ListReachableCalendars: %s", err)
		}
		for _, each := range reachable {
			if each.Calendar.ID == world.adaCalendar.ID {
				t.Fatalf("ada's calendar is still listed for ben")
			}
		}
	})
}

// Leaving a circle ends the direct shares between two people who no longer
// share any circle, in both directions, and keeps the ones another circle
// still justifies.
func TestLeavingACircleEndsDirectSharesBetweenStrangers(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		benCalendar, err := tx.EnsureCalendar(world.ben.ID, models.CalendarEvents, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		coraCalendar, err := tx.EnsureCalendar(world.cora.ID, models.CalendarEvents, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		// ada -> ben, ben -> ada (the two directions), and ben <-> cora
		// through "club", which is not affected.
		for _, each := range []struct {
			sharer  string
			shared  *models.Calendar
			grantee string
		}{
			{world.ada.ID, world.adaCalendar, world.ben.ID},
			{world.ben.ID, benCalendar, world.ada.ID},
			{world.ben.ID, benCalendar, world.cora.ID},
			{world.cora.ID, coraCalendar, world.ben.ID},
		} {
			if _, err := tx.ShareCalendar(each.sharer, &models.CalendarShare{
				CalendarID: each.shared.ID, GranteeUserID: each.grantee, ShareLevel: models.ShareLevelTitles,
			}); err != nil {
				t.Fatalf("ShareCalendar: %s", err)
			}
		}
		// And a share by ada to "home", the circle she is about to leave.
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelBusy,
		}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}

		// Ben stays an admin so that ada can leave.
		if _, err := tx.SetCircleMemberRole(world.home.ID, world.ben.ID, models.MemberRoleAdmin); err != nil {
			t.Fatalf("SetCircleMemberRole: %s", err)
		}
		// Leaving ends the shares itself, in the same transaction.
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}

		adaShares, _ := tx.ListCalendarShares(world.adaCalendar.ID)
		if len(adaShares) != 0 {
			t.Errorf("ada's shares, to ben and to the circle she left, are gone: %+v", adaShares)
		}
		benShares, _ := tx.ListCalendarShares(benCalendar.ID)
		if len(benShares) != 1 || benShares[0].GranteeUserID != world.cora.ID {
			t.Errorf("ben's share with ada is gone, with cora stays: %+v", benShares)
		}
		coraShares, _ := tx.ListCalendarShares(coraCalendar.ID)
		if len(coraShares) != 1 {
			t.Errorf("cora's share with ben stays: %+v", coraShares)
		}

		// Ada joins "club" later: the old shares do not come back.
		if _, err := tx.AddCircleMember(world.club.ID, world.ada.ID, models.MemberRoleMember, world.ben.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != "" {
			t.Errorf("a share does not come back when the two meet in another circle: %q", level)
		}
	})
}

// A circle has one calendar; every member reads and writes it and nobody else
// reaches it at all.
func TestACircleCalendarBelongsToItsMembers(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		family, err := tx.CreateCircleCalendar(world.home.ID, "", "Europe/London")
		if err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		if family.CircleID != world.home.ID || family.UserID != "" || family.Name != "Home" ||
			family.CalendarKind != models.CalendarEvents {
			t.Fatalf("a circle calendar belongs to the circle and is called what the circle is: %+v", family)
		}
		if _, err := tx.CreateCircleCalendar(world.home.ID, "Again", ""); !errors.Is(err, db.ErrAlreadyExists) {
			t.Fatalf("a circle has one: %v", err)
		}
		if _, err := tx.CreateCircleCalendar("nobody", "", ""); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("no such circle: %v", err)
		}
		found, err := tx.GetCircleCalendar(world.home.ID)
		if err != nil || found == nil || found.ID != family.ID {
			t.Fatalf("GetCircleCalendar: %v %v", found, err)
		}
		if other, _ := tx.GetCircleCalendar(world.club.ID); other != nil {
			t.Fatalf("club has none: %+v", other)
		}

		for _, member := range []*models.User{world.ada, world.ben} {
			if level := accessOf(t, tx, member.ID, family); level != models.ShareLevelEdit {
				t.Errorf("%s edits the circle's calendar: %q", member.Username, level)
			}
		}
		for _, outsider := range []*models.User{world.cora, world.dave} {
			if level := accessOf(t, tx, outsider.ID, family); level != "" {
				t.Errorf("%s has no access: %q", outsider.Username, level)
			}
		}
		// It is nobody's own calendar, so listing a person's own leaves it out
		// and listing what they reach has it, with the circle's name.
		own, err := tx.ListCalendars(world.ada.ID)
		if err != nil {
			t.Fatalf("ListCalendars: %s", err)
		}
		for _, each := range own {
			if each.ID == family.ID {
				t.Errorf("a circle calendar is not ada's own")
			}
		}
		reachable, err := tx.ListReachableCalendars(world.ben.ID)
		if err != nil {
			t.Fatalf("ListReachableCalendars: %s", err)
		}
		var sawFamily bool
		for _, each := range reachable {
			if each.Calendar.ID == family.ID {
				sawFamily = true
				if each.CircleName != "Home" || each.ShareLevel != models.ShareLevelEdit {
					t.Errorf("the circle calendar as ben reaches it: %+v", each)
				}
			}
		}
		if !sawFamily {
			t.Errorf("ben reaches the circle's calendar")
		}
		if reachable, _ = tx.ListReachableCalendars(world.dave.ID); len(reachable) != 0 {
			t.Errorf("dave has nothing: %+v", reachable)
		}

		// Ben leaves; he stops reaching it. (Ada is the last admin and stays.)
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if level := accessOf(t, tx, world.ben.ID, family); level != "" {
			t.Errorf("a member who left: %q", level)
		}
	})
}

// What a reader is given of an event is decided in one place, by the level.
func TestAReaderIsGivenWhatTheirLevelAllows(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		const detailed = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//TeaNode//EN\r\nBEGIN:VEVENT\r\n" +
			"UID:dentist\r\nDTSTAMP:20260912T120000Z\r\nDTSTART:20260914T100000Z\r\n" +
			"DTEND:20260914T110000Z\r\nSUMMARY:Dentist\r\nLOCATION:Elm Street\r\nDESCRIPTION:Bring the form\r\n" +
			"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Dentist\r\nEND:VALARM\r\n" +
			"END:VEVENT\r\nEND:VCALENDAR\r\n"
		kept, err := tx.PutCalendarObject(&models.CalendarObject{
			CalendarID: world.adaCalendar.ID, UID: "dentist", ETag: "owner-version", Data: detailed,
			Summary: "Dentist", Location: "Elm Street", StartsAt: at(14, 10), EndsAt: at(14, 11),
		}, nil)
		if err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}

		owner, err := tx.ReadCalendarObject(world.adaCalendar.ID, kept.ID, models.ShareLevelOwner)
		if err != nil || owner.Data != detailed || owner.ETag != "owner-version" {
			t.Fatalf("the owner gets the stored event: %+v %v", owner, err)
		}
		editor, _ := tx.ReadCalendarObject(world.adaCalendar.ID, kept.ID, models.ShareLevelEdit)
		if editor.Data != detailed {
			t.Errorf("an editor gets the stored event")
		}
		details, _ := tx.ReadCalendarObject(world.adaCalendar.ID, kept.ID, models.ShareLevelDetails)
		if !strings.Contains(details.Data, "Bring the form") || !strings.Contains(details.Data, "SUMMARY:Dentist") ||
			strings.Contains(details.Data, "VALARM") || strings.Contains(details.Data, "X-TEANODE-BUSY") {
			t.Errorf("details are everything but the alarm:\n%s", details.Data)
		}
		titles, _ := tx.ReadCalendarObject(world.adaCalendar.ID, kept.ID, models.ShareLevelTitles)
		if !strings.Contains(titles.Data, "SUMMARY:Dentist") || strings.Contains(titles.Data, "Bring the form") ||
			titles.Summary != "Dentist" || titles.Location != "Elm Street" {
			t.Errorf("titles are the title and the place:\n%s", titles.Data)
		}
		busy, _ := tx.ReadCalendarObject(world.adaCalendar.ID, kept.ID, models.ShareLevelBusy)
		if strings.Contains(busy.Data, "Dentist") || strings.Contains(busy.Data, "Elm Street") ||
			busy.Summary != "Busy" || busy.Location != "" || !strings.Contains(busy.Data, "DTSTART:20260914T100000Z") {
			t.Errorf("busy is when, and nothing else:\n%s", busy.Data)
		}
		if missing, err := tx.ReadCalendarObject(world.adaCalendar.ID, "nothing", models.ShareLevelTitles); err != nil || missing != nil {
			t.Errorf("an event that is not there: %v %v", missing, err)
		}
		all, err := tx.ReadCalendarObjects(world.adaCalendar.ID, models.ShareLevelTitles)
		if err != nil || len(all) != 1 || strings.Contains(all[0].Data, "Bring the form") {
			t.Errorf("a listing is redacted too: %+v %v", all, err)
		}
		// The stored event is untouched by anybody's reading of it.
		again, _ := tx.GetCalendarObject(world.adaCalendar.ID, kept.ID)
		if again.Data != detailed {
			t.Errorf("reading must not change what is stored")
		}
	})
}

// Invitations that arrive by mail land in the person's own calendar of events,
// found with EnsureCalendar, which can never be a circle's calendar or one
// shared with them, however many of those they reach and even when they have
// no calendar of their own yet.
func TestEnsureCalendarNeverPicksACircleOrSharedCalendar(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		if _, err := tx.CreateCircleCalendar(world.home.ID, "", ""); err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{
			CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelEdit,
		}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}

		// Ben has a circle's calendar and ada's, at edit, and none of his own.
		own, err := tx.EnsureCalendar(world.ben.ID, models.CalendarEvents, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		if own.UserID != world.ben.ID || own.CircleID != "" || own.ID == world.adaCalendar.ID {
			t.Fatalf("what an invitation to ben lands in is his own: %+v", own)
		}
		again, err := tx.EnsureCalendar(world.ben.ID, models.CalendarEvents, nil)
		if err != nil || again.ID != own.ID {
			t.Fatalf("and it is the same one each time: %+v %v", again, err)
		}
		found, err := tx.FindCalendar(world.ben.ID, models.CalendarEvents)
		if err != nil || found == nil || found.ID != own.ID {
			t.Fatalf("FindCalendar: %+v %v", found, err)
		}
		if nothing, err := tx.FindCalendar(world.dave.ID, models.CalendarEvents); err != nil || nothing != nil {
			t.Fatalf("FindCalendar makes nothing: %+v %v", nothing, err)
		}
		// A person's own list has only their own, and the reminders list is
		// never anybody else's to reach.
		mine, err := tx.ListCalendars(world.ben.ID)
		if err != nil || len(mine) != 1 || mine[0].ID != own.ID {
			t.Fatalf("ListCalendars: %+v %v", mine, err)
		}
		reminders, err := tx.EnsureCalendar(world.ada.ID, models.CalendarReminders, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		if level := accessOf(t, tx, world.ben.ID, reminders); level != "" {
			t.Fatalf("a reminders list is nobody else's: %q", level)
		}
		reachable, _ := tx.ListReachableCalendars(world.ben.ID)
		for _, each := range reachable {
			if each.Calendar.ID == reminders.ID {
				t.Fatalf("a reminders list is not reachable")
			}
		}
	})
}

// A disabled account's calendar stops reaching the people it was shared with,
// whether it was shared with a person or with a circle, and reaches them again
// when the account is switched back on.
func TestADisabledOwnersCalendarStopsReachingReaders(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelTitles}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{CalendarID: world.adaCalendar.ID, GranteeCircleID: world.home.ID, ShareLevel: models.ShareLevelDetails}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Fatalf("before: %q", level)
		}
		set := func(isDisabled bool) {
			t.Helper()
			if _, err := tx.UpdateUser(world.ada.ID, func(owner *models.User) error {
				owner.DisabledAt = nil
				if isDisabled {
					now := time.Now()
					owner.DisabledAt = &now
				}
				return nil
			}); err != nil {
				t.Fatalf("UpdateUser: %s", err)
			}
		}
		set(true)
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != "" {
			t.Errorf("a disabled owner's calendar reaches ben at %q", level)
		}
		reachable, err := tx.ListReachableCalendars(world.ben.ID)
		if err != nil {
			t.Fatalf("ListReachableCalendars: %s", err)
		}
		for _, each := range reachable {
			if each.Calendar.ID == world.adaCalendar.ID {
				t.Errorf("a disabled owner's calendar is listed for ben")
			}
		}
		if level := accessOf(t, tx, world.ada.ID, world.adaCalendar); level != models.ShareLevelOwner {
			t.Errorf("the owner's own access: %q", level)
		}
		set(false)
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Errorf("after switching the account back on: %q", level)
		}
	})
}

// A calendar reaches only people who may use calendars, and only from an owner
// who may: taking calendar:use from a role switches calendars off for those
// people, and what they kept is no longer read by others. A share is still
// listed to its owner, so that they can end it.
func TestACalendarReachesOnlyPeopleWhoMayUseCalendars(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		benCalendar, err := tx.EnsureCalendar(world.ben.ID, models.CalendarEvents, nil)
		if err != nil {
			t.Fatalf("EnsureCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ada.ID, &models.CalendarShare{CalendarID: world.adaCalendar.ID, GranteeUserID: world.ben.ID, ShareLevel: models.ShareLevelDetails}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		if _, err := tx.ShareCalendar(world.ben.ID, &models.CalendarShare{CalendarID: benCalendar.ID, GranteeUserID: world.ada.ID, ShareLevel: models.ShareLevelDetails}); err != nil {
			t.Fatalf("ShareCalendar: %s", err)
		}
		// Only ada may use calendars: ben cannot read hers, and she cannot
		// read his.
		dbtest.GrantPermission(t, tx, world.ada.ID, models.PermissionCalendarUse)
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != "" {
			t.Errorf("a reader without calendar:use reaches ada at %q", level)
		}
		if level := accessOf(t, tx, world.ada.ID, benCalendar); level != "" {
			t.Errorf("an owner without calendar:use reaches ada at %q", level)
		}
		reachable, err := tx.ListReachableCalendars(world.ada.ID)
		if err != nil {
			t.Fatalf("ListReachableCalendars: %s", err)
		}
		for _, each := range reachable {
			if each.Calendar.ID == benCalendar.ID {
				t.Errorf("the calendar of an owner without calendar:use is listed")
			}
		}
		if shares, err := tx.ListCalendarShares(benCalendar.ID); err != nil || len(shares) != 1 {
			t.Errorf("the owner's own list of shares: %+v %v", shares, err)
		}
		// Once ben may as well, both reach as before.
		dbtest.GrantPermission(t, tx, world.ben.ID, models.PermissionCalendarUse)
		if level := accessOf(t, tx, world.ben.ID, world.adaCalendar); level != models.ShareLevelDetails {
			t.Errorf("both may use calendars, ben reaches ada at %q", level)
		}
		if level := accessOf(t, tx, world.ada.ID, benCalendar); level != models.ShareLevelDetails {
			t.Errorf("both may use calendars, ada reaches ben at %q", level)
		}
	})
}

// Deleting a circle's calendar takes the events with it and leaves the people's
// own calendars alone, and a circle with none has nothing to delete.
func TestDeletingACircleCalendarTakesItsEventsAndNothingElse(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newCalendarShareWorld(t, tx)
		if err := tx.DeleteCircleCalendar(world.home.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a circle with no calendar: %v", err)
		}
		if err := tx.DeleteCircleCalendar("nobody"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("no such circle: %v", err)
		}
		family, err := tx.CreateCircleCalendar(world.home.ID, "", "")
		if err != nil {
			t.Fatalf("CreateCircleCalendar: %s", err)
		}
		if _, err := tx.PutCalendarObject(&models.CalendarObject{
			ID: "play", CalendarID: family.ID, UID: "play", ETag: "e", Data: "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n", Summary: "School play",
		}, nil); err != nil {
			t.Fatalf("PutCalendarObject: %s", err)
		}
		if err := tx.DeleteCircleCalendar(world.home.ID); err != nil {
			t.Fatalf("DeleteCircleCalendar: %s", err)
		}
		if found, err := tx.GetCircleCalendar(world.home.ID); err != nil || found != nil {
			t.Errorf("the calendar is still there: %+v %v", found, err)
		}
		if object, err := tx.GetCalendarObject(family.ID, "play"); err != nil || object != nil {
			t.Errorf("its event is still there: %+v %v", object, err)
		}
		if kept, err := tx.GetCalendar(world.adaCalendar.ID); err != nil || kept == nil {
			t.Errorf("ada's own calendar went with it: %+v %v", kept, err)
		}
		// And the circle may have another.
		if _, err := tx.CreateCircleCalendar(world.home.ID, "", ""); err != nil {
			t.Errorf("a new one: %s", err)
		}
	})
}
