package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
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
	return &models.PlaceVisit{ID: "a-visit", UserID: self.sharer.ID, PlaceID: made.ID, ArrivedAt: &arrivedAt}
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
		if body := PlaceVisitBody(each.place, each.isArrival); body != each.expected {
			t.Errorf("%s: %q, not %q", name, body, each.expected)
		}
	}
	for _, label := range models.PlaceLabels {
		if placeVisitLabelWords[label] == "" {
			t.Errorf("the label %q has no words", label)
		}
	}
}
