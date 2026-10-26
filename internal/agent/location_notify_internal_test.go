package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// locationNotifyFixture is the phone fixture's person (robin, with one phone)
// and a second person (sam) in a circle with them, who shares their location
// with robin.
type locationNotifyFixture struct {
	*phoneFixture
	sharer *models.User
}

func newLocationNotifyFixture(t *testing.T, shareLevel models.LocationShareLevel) *locationNotifyFixture {
	t.Helper()
	fixture := &locationNotifyFixture{phoneFixture: newPhoneFixture(t)}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if fixture.sharer, err = tx.CreateUser(&models.User{Username: "sam"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, fixture.sharer.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err := tx.AddCircleMember(circle.ID, fixture.owner.ID, models.MemberRoleMember, fixture.sharer.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		// The person who is told and the one who shares can use Location, as the
		// ordinary roles allow.
		role, err := tx.CreateRole(&models.Role{Name: "Location users", Permissions: []models.Permission{models.PermissionLocationUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Location users", RoleIDs: []string{role.ID}, UserIDs: []string{fixture.owner.ID, fixture.sharer.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		if shareLevel != "" {
			if _, err := tx.ShareLocation(fixture.sharer.ID, &models.LocationShare{GranteeUserID: fixture.owner.ID, ShareLevel: shareLevel}); err != nil {
				t.Fatalf("ShareLocation: %s", err)
			}
		}
	})
	return fixture
}

// visitTo makes a place of the sharer's and a visit to it.
func (self *locationNotifyFixture) visitTo(t *testing.T, place *models.Place) *models.PlaceVisit {
	t.Helper()
	var made *models.Place
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		place.LatitudeDegrees, place.LongitudeDegrees = 51.5, -0.1
		var err error
		if made, err = tx.CreatePlace(self.sharer.ID, place); err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
	})
	arrivedAt := time.Now()
	departedAt := arrivedAt
	return &models.PlaceVisit{ID: "a-visit", UserID: self.sharer.ID, PlaceID: made.ID, ArrivedAt: &arrivedAt, DepartedAt: &departedAt}
}

func (self *locationNotifyFixture) told() int {
	self.worker.waitGroup.Wait()
	return len(self.relay.to("sealed:a-registration"))
}

// An arrival at a place the person named is told to the phone of everyone the
// location is shared with: the owner's name for the title, the place in the
// words, and the owner's id so that the app opens Location with them.
func TestAnArrivalAtANamedPlaceIsToldToTheSharedPhones(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})

	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 1 {
		t.Fatalf("the phone was sent %d notifications, not one", fixture.told())
	}
	content := fixture.read(t, fixture.relay.to("sealed:a-registration")[0])
	if content.NotificationKind != models.NotificationLocation || content.Title != "sam" || content.Body != "arrived at Corner Cafe" || content.LocationUserID != fixture.sharer.ID {
		t.Errorf("the phone was told %+v", content)
	}

	// A departure is another notice, and says so.
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, false)
	if fixture.told() != 2 {
		t.Fatalf("the phone was sent %d notifications, not two", fixture.told())
	}
	if content := fixture.read(t, fixture.relay.to("sealed:a-registration")[1]); content.Body != "left Corner Cafe" {
		t.Errorf("the departure says %q", content.Body)
	}
}

// A labeled place the person did not name is called by its label, and the
// visit may carry its place or leave it to be looked up.
func TestALabeledPlaceTheOwnerDidNotNameIsCalledByItsLabel(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelHistory)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "12 Example Street", PlaceLabel: models.PlaceLabelHome})

	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, false)
	if fixture.told() != 1 {
		t.Fatalf("the phone was sent %d notifications, not one", fixture.told())
	}
	if content := fixture.read(t, fixture.relay.to("sealed:a-registration")[0]); content.Body != "left home" {
		t.Errorf("the phone was told %q", content.Body)
	}
}

// A place the server only found a name for is nobody else's business.
func TestAnUnnamedUnlabeledPlaceIsToldToNobody(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Example Road"})

	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 0 {
		t.Errorf("the phone was sent %d notifications for a place nobody named", fixture.told())
	}
}

// Only people the location is shared with, and only phones that chose the
// kind, are told.
func TestOnlyThoseSharedWithAndPhonesThatChoseLocationAreTold(t *testing.T) {
	unshared := newLocationNotifyFixture(t, "")
	visit := unshared.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})
	unshared.worker.Phones().NotifyPlaceVisit(context.Background(), unshared.sharer.ID, visit, true)
	if unshared.told() != 0 {
		t.Errorf("a person nothing is shared with was sent %d notifications", unshared.told())
	}

	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit = fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.UpdateAgentPhoneSettings(fixture.agent.ID, fixture.phone.ID, "A test phone", []string{models.NotificationApproval}); err != nil {
			t.Fatalf("UpdateAgentPhoneSettings: %s", err)
		}
	})
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 0 {
		t.Errorf("a phone that did not choose location was sent %d notifications", fixture.told())
	}
}

// The same arrival is told once in ten minutes, however often the phone
// crosses the edge of the place, and again after.
func TestTheSameArrivalIsToldOnceInTenMinutes(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})
	for range 3 {
		fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	}
	if fixture.told() != 1 {
		t.Errorf("three arrivals in a row sent %d notifications, not one", fixture.told())
	}
}

// claim is the rule itself, on a clock the test sets.
func TestAPlaceVisitNoticeIsClaimedOncePerOwnerPlaceReceiverAndDirection(t *testing.T) {
	var notices placeVisitNotices
	start := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	if !notices.claim("owner", "place", "receiver", true, start) {
		t.Fatal("the first is told")
	}
	if notices.claim("owner", "place", "receiver", true, start.Add(9*time.Minute)) {
		t.Error("the same again within ten minutes is not")
	}
	for name, claimed := range map[string]bool{
		"the other direction": notices.claim("owner", "place", "receiver", false, start.Add(time.Minute)),
		"another receiver":    notices.claim("owner", "place", "another", true, start.Add(time.Minute)),
		"another place":       notices.claim("owner", "elsewhere", "receiver", true, start.Add(time.Minute)),
		"another owner":       notices.claim("someone", "place", "receiver", true, start.Add(time.Minute)),
	} {
		if !claimed {
			t.Errorf("%s is told", name)
		}
	}
	if !notices.claim("owner", "place", "receiver", true, start.Add(10*time.Minute)) {
		t.Error("after ten minutes it is told again")
	}
	if len(notices.toldAt) > 5 {
		t.Errorf("the notices held grow without bound: %d", len(notices.toldAt))
	}
}

func TestPlaceVisitBodyNamesTheKindsOfPlace(t *testing.T) {
	for name, each := range map[string]struct {
		place     *models.Place
		isArrival bool
		expected  string
	}{
		"named":          {&models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true}, true, "arrived at Corner Cafe"},
		"named, labeled": {&models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true, PlaceLabel: models.PlaceLabelWork}, false, "left Corner Cafe"},
		"work":           {&models.Place{PlaceName: "Example Road", PlaceLabel: models.PlaceLabelWork}, true, "arrived at work"},
		"family":         {&models.Place{PlaceName: "Example Road", PlaceLabel: models.PlaceLabelFamily}, true, "arrived at a family member's home"},
		"unlabeled":      {&models.Place{PlaceName: "Example Road"}, true, ""},
		"none":           {nil, true, ""},
	} {
		if body := PlaceVisitBody(each.place, each.isArrival, push.LanguageEnglish); body != each.expected {
			t.Errorf("%s: %q, not %q", name, body, each.expected)
		}
	}
	for _, label := range models.PlaceLabels {
		words := placeVisitLabelWords[label]
		if words.English == "" || words.Chinese == "" || words.Japanese == "" {
			t.Errorf("the label %q has no words in some language: %+v", label, words)
		}
	}
	home := &models.Place{PlaceName: "Example Road", PlaceLabel: models.PlaceLabelHome}
	if body := PlaceVisitBody(home, true, push.LanguageChinese); body != "到家了" {
		t.Errorf("an arrival home in Chinese: %q", body)
	}
	if body := PlaceVisitBody(home, false, push.LanguageJapanese); body != "家を出ました" {
		t.Errorf("a departure from home in Japanese: %q", body)
	}
}

// An arrival or departure is told only when it happened in the last fifteen
// minutes: a phone that was offline sends its queue when it reconnects, and
// "arrived at home" about last night is wrong now.
func TestOnlyAnArrivalOfTheLastFifteenMinutesIsTold(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})
	// The location was shared long before.
	dbtest.Exec(t, fixture.database, `UPDATE "location_share" SET "created_at" = now() - interval '1 day'`)

	arrivedAt, departedAt := time.Now().Add(-20*time.Minute), time.Now().Add(-18*time.Minute)
	visit.ArrivedAt, visit.DepartedAt = &arrivedAt, &departedAt
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, false)
	if fixture.told() != 0 {
		t.Fatalf("an arrival twenty minutes ago and a departure eighteen minutes ago were sent %d notifications", fixture.told())
	}

	arrivedAt = time.Now().Add(-10 * time.Minute)
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 1 {
		t.Fatalf("an arrival ten minutes ago was sent %d notifications, not one", fixture.told())
	}
	noTime := &models.PlaceVisit{ID: "no-time", UserID: fixture.sharer.ID, PlaceID: visit.PlaceID}
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, noTime, true)
	if fixture.told() != 1 {
		t.Errorf("an arrival with no time was told")
	}
}

// What the owner did before a location was shared is not told to the person it
// was then shared with, and nobody is told who can no longer use Location.
func TestAnArrivalBeforeTheShareIsNotToldAndNeitherIsAPersonWithoutThePermission(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})

	beforeTheShare := time.Now().Add(-time.Minute)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		shares, err := tx.ListLocationShares(fixture.sharer.ID)
		if err != nil || len(shares) != 1 {
			t.Fatalf("ListLocationShares: %+v %v", shares, err)
		}
	})
	visit.ArrivedAt = &beforeTheShare
	// The share was made just now, a moment after this arrival.
	dbtest.Exec(t, fixture.database, `UPDATE "location_share" SET "created_at" = now() + interval '1 second'`)
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 0 {
		t.Fatalf("an arrival from before the share was sent %d notifications", fixture.told())
	}

	dbtest.Exec(t, fixture.database, `UPDATE "location_share" SET "created_at" = now() - interval '1 hour'`)
	dbtest.Exec(t, fixture.database, `DELETE FROM "user_group"`)
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 0 {
		t.Errorf("a person who can no longer use Location was sent %d notifications", fixture.told())
	}
}

// An arrival joined into an earlier visit is stored with the first arrival,
// hours old; the notice is judged by the time that was reported, so a person
// who left and came back within minutes is told to have arrived again.
func TestAnArrivalJoinedIntoAnEarlierVisitIsToldByTheTimeReported(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})
	earlier := time.Now().Add(-3 * time.Hour)
	visit.ArrivedAt, visit.DepartedAt = &earlier, nil

	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 0 {
		t.Fatalf("judged by the stored arrival it is old: %d notifications", fixture.told())
	}
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, time.Now())
	if fixture.told() != 1 {
		t.Errorf("the arrival just reported was sent %d notifications, not one", fixture.told())
	}
}

// Nobody is told of an arrival or a departure inside a pause, nor while the
// person is paused: a notice a few minutes late says where they are. Once
// they resume, the same arrival is told again.
func TestNoArrivalIsToldInsideAPauseOrWhileThePersonIsPaused(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})

	now := time.Now()
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, _, err := tx.PauseLocation(fixture.sharer.ID, nil, now.Add(-time.Minute)); err != nil {
			t.Fatalf("PauseLocation: %s", err)
		}
	})
	// The arrival was before the pause began, and told while it is in effect.
	earlier := now.Add(-5 * time.Minute)
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, earlier)
	// The arrival was inside it.
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, now)
	if fixture.told() != 0 {
		t.Fatalf("the phone was sent %d notifications while the person is paused", fixture.told())
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.ResumeLocation(fixture.sharer.ID, time.Now()); err != nil {
			t.Fatalf("ResumeLocation: %s", err)
		}
	})
	// An arrival inside the pause that has since ended is still not told.
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, now.Add(-30*time.Second))
	if fixture.told() != 0 {
		t.Fatalf("an arrival inside the pause was told after it: %d", fixture.told())
	}
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, time.Now())
	if fixture.told() != 1 {
		t.Errorf("an arrival after the pause is told: %d notifications", fixture.told())
	}
}

// A person who muted the sharer is not told of their arrivals or departures
// while the mute lasts, and is told again once they unmute.
func TestAMutedSharerIsNotToldUntilUnmuted(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true})

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		mutedUntil := time.Now().Add(time.Hour)
		if _, err := tx.MuteLocationNotifications(fixture.owner.ID, fixture.sharer.ID, &mutedUntil, time.Now()); err != nil {
			t.Fatalf("MuteLocationNotifications: %s", err)
		}
	})
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, false)
	if fixture.told() != 0 {
		t.Fatalf("a muted sharer's arrival and departure sent %d notifications", fixture.told())
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if isUnmuted, err := tx.UnmuteLocationNotifications(fixture.owner.ID, fixture.sharer.ID, time.Now()); err != nil || !isUnmuted {
			t.Fatalf("UnmuteLocationNotifications: %v %s", isUnmuted, err)
		}
	})
	fixture.worker.Phones().NotifyPlaceVisit(context.Background(), fixture.sharer.ID, visit, true)
	if fixture.told() != 1 {
		t.Errorf("after unmuting the arrival sent %d notifications, not one", fixture.told())
	}
}

// receiverVisit stores a visit of the receiver's own, at a place of theirs at
// the coordinates, arrived a minute ago and still in progress.
func (self *locationNotifyFixture) receiverVisit(t *testing.T, latitudeDegrees, longitudeDegrees float64) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		own, err := tx.CreatePlace(self.owner.ID, &models.Place{PlaceName: "Their place", IsNamedByPerson: true, LatitudeDegrees: latitudeDegrees, LongitudeDegrees: longitudeDegrees, RadiusMeters: 100})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		arrivedAt := time.Now().Add(-time.Minute)
		if _, err := tx.PutPlaceVisit(self.owner.ID, &models.PlaceVisit{
			PlaceID: own.ID, ClientVisitKey: "receiver-visit", VisitSource: models.VisitSourceRegion, ArrivedAt: &arrivedAt,
			LatitudeDegrees: latitudeDegrees, LongitudeDegrees: longitudeDegrees, AccuracyMeters: 20,
		}); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
	})
}

// receiverPosition stores a position of the receiver's own, measured a
// minute ago.
func (self *locationNotifyFixture) receiverPosition(t *testing.T, latitudeDegrees, longitudeDegrees float64) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.InsertLocationPoints(self.owner.ID, []*models.LocationPoint{{
			LatitudeDegrees: latitudeDegrees, LongitudeDegrees: longitudeDegrees, AccuracyMeters: 20,
			MeasuredAt: time.Now().Add(-time.Minute), LocationSource: models.LocationSourcePhone,
		}}); err != nil {
			t.Fatalf("InsertLocationPoints: %s", err)
		}
	})
}

// A receiver who was at the place too is not told that the sharer arrived or
// left: they came together, left together, or were already there. One whose
// visit or position was somewhere else is told.
func TestAReceiverWhoWasThereTooIsNotTold(t *testing.T) {
	for _, each := range []struct {
		name       string
		isVisit    bool
		isThere    bool
		isArrival  bool
		toldNumber int
	}{
		{name: "a visit there, an arrival", isVisit: true, isThere: true, isArrival: true, toldNumber: 0},
		{name: "a visit there, a departure", isVisit: true, isThere: true, isArrival: false, toldNumber: 0},
		{name: "a visit elsewhere", isVisit: true, isThere: false, isArrival: true, toldNumber: 1},
		{name: "a position there", isVisit: false, isThere: true, isArrival: false, toldNumber: 0},
		{name: "a position elsewhere", isVisit: false, isThere: false, isArrival: true, toldNumber: 1},
	} {
		t.Run(each.name, func(t *testing.T) {
			fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
			visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true, RadiusMeters: 100})
			// visitTo puts the sharer's place at 51.5, -0.1; elsewhere is about
			// eleven kilometers north, and there is thirty meters off.
			latitudeDegrees := 51.6
			if each.isThere {
				latitudeDegrees = 51.50027
			}
			if each.isVisit {
				fixture.receiverVisit(t, latitudeDegrees, -0.1)
			} else {
				fixture.receiverPosition(t, latitudeDegrees, -0.1)
			}
			fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, each.isArrival, time.Now())
			if fixture.told() != each.toldNumber {
				t.Errorf("the phone was sent %d notifications, want %d", fixture.told(), each.toldNumber)
			}
		})
	}
}

// A receiver who left the place before the sharer arrived, and whose phone
// has said so since, is told: they were not there together.
func TestAReceiverWhoLeftBeforeTheArrivalIsTold(t *testing.T) {
	fixture := newLocationNotifyFixture(t, models.LocationShareLevelCurrent)
	visit := fixture.visitTo(t, &models.Place{PlaceName: "Corner Cafe", IsNamedByPerson: true, RadiusMeters: 100})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		own, err := tx.CreatePlace(fixture.owner.ID, &models.Place{PlaceName: "Their place", IsNamedByPerson: true, LatitudeDegrees: 51.5, LongitudeDegrees: -0.1, RadiusMeters: 100})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		arrivedAt, departedAt := time.Now().Add(-time.Hour), time.Now().Add(-10*time.Minute)
		if _, err := tx.PutPlaceVisit(fixture.owner.ID, &models.PlaceVisit{
			PlaceID: own.ID, ClientVisitKey: "receiver-visit", VisitSource: models.VisitSourceRegion, ArrivedAt: &arrivedAt, DepartedAt: &departedAt,
			LatitudeDegrees: 51.5, LongitudeDegrees: -0.1, AccuracyMeters: 20,
		}); err != nil {
			t.Fatalf("PutPlaceVisit: %s", err)
		}
	})
	fixture.worker.Phones().NotifyPlaceVisitAt(context.Background(), fixture.sharer.ID, visit, true, time.Now())
	if fixture.told() != 1 {
		t.Errorf("the phone was sent %d notifications, not one", fixture.told())
	}
}
