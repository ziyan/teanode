package db_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// newHealthShareWorld is the people and circles the share tests use: ada and
// ben in "home", ben and cora in "club", dave in none. Ada, ben and dave have
// the permission to use Health, as the ordinary roles give it; cora does not.
func newHealthShareWorld(t *testing.T, tx db.Transaction) *shareWorld {
	t.Helper()
	world := newShareWorld(t, tx)
	for _, person := range []*models.User{world.ada, world.ben, world.dave} {
		dbtest.GrantPermission(t, tx, person.ID, models.PermissionHealthUse)
	}
	return world
}

func mustListHealthShares(t *testing.T, tx db.Transaction, sharerUserId string) []*models.HealthShare {
	t.Helper()
	shares, err := tx.ListHealthShares(sharerUserId)
	if err != nil {
		t.Fatalf("ListHealthShares: %s", err)
	}
	return shares
}

func sharedCategoriesOf(t *testing.T, tx db.Transaction, viewerUserId, ownerUserId string) []string {
	t.Helper()
	categories, err := tx.GetSharedHealthCategories(viewerUserId, ownerUserId)
	if err != nil {
		t.Fatalf("GetSharedHealthCategories: %s", err)
	}
	return categories
}

// Sharing Health needs a circle in common with a person or membership of a
// circle, and says not found rather than why. Sharing again replaces the
// categories, they are kept in the Health app's order, and an empty list takes
// the share back.
func TestHealthShareNeedsACircleInCommonAndReplacesItsCategories(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newHealthShareWorld(t, tx)

		share, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"sleep", "activity", "sleep"}})
		if err != nil || share == nil || share.ID == "" || share.UserID != world.ada.ID || share.CreatedByUserID != world.ada.ID ||
			!slices.Equal(share.HealthCategories, []string{"activity", "sleep"}) {
			t.Fatalf("a share with someone in a shared circle: %+v %v", share, err)
		}
		again, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"heart"}})
		if err != nil || again.ID != share.ID || !slices.Equal(again.HealthCategories, []string{"heart"}) {
			t.Fatalf("sharing again replaces the categories of the same share: %+v %v", again, err)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: stranger.ID, HealthCategories: []string{"sleep"}}); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeCircleID: world.home.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatalf("a share with her own circle: %s", err)
		}
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeCircleID: world.club.ID, HealthCategories: []string{"sleep"}}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		for name, bad := range map[string]*models.HealthShare{
			"notacategory": {GranteeUserID: world.ben.ID, HealthCategories: []string{"sleeping"}},
			"wrongcase":    {GranteeUserID: world.ben.ID, HealthCategories: []string{"Sleep"}},
			"both":         {GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, HealthCategories: []string{"sleep"}},
			"neither":      {HealthCategories: []string{"sleep"}},
			"herself":      {GranteeUserID: world.ada.ID, HealthCategories: []string{"sleep"}},
		} {
			if _, err := tx.SetHealthShare(world.ada.ID, bad); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("a share with %s is refused: %v", name, err)
			}
		}
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: "nobody", HealthCategories: []string{"sleep"}}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person who does not exist: %v", err)
		}

		listed, err := tx.ListHealthShares(world.ada.ID)
		if err != nil || len(listed) != 2 {
			t.Fatalf("ListHealthShares: %+v %v", listed, err)
		}
		if listed[0].GranteeUsername != "ben" || listed[0].GranteeName == "" || listed[0].OwnerUsername != "ada" || listed[1].GranteeCircleName != "Home" {
			t.Errorf("the listing says who each is for: %+v %+v", listed[0], listed[1])
		}
		if others, _ := tx.ListHealthShares(world.ben.ID); len(others) != 0 {
			t.Errorf("ben shares nothing: %+v", others)
		}

		// An empty list takes the share back, and taking back what is not
		// there is not an error.
		if removed, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID}); err != nil || removed != nil {
			t.Fatalf("an empty list removes the share: %+v %v", removed, err)
		}
		if removed, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID}); err != nil || removed != nil {
			t.Fatalf("and again: %+v %v", removed, err)
		}
		if left, _ := tx.ListHealthShares(world.ada.ID); len(left) != 1 || left[0].GranteeCircleName != "Home" {
			t.Errorf("only the share with the circle is left: %+v", left)
		}
		if removed, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeCircleID: world.club.ID}); err != nil || removed != nil {
			t.Errorf("a circle she is not in, with nothing to share, is nothing: %+v %v", removed, err)
		}
	})
}

// The viewer gets the categories that reach them together, their own Health is
// theirs without a share, and a person nobody shares with gets nothing.
func TestAViewerGetsTheHealthCategoriesReachingThem(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newHealthShareWorld(t, tx)
		if got := sharedCategoriesOf(t, tx, world.ben.ID, world.ada.ID); len(got) != 0 {
			t.Fatalf("before any share: %v", got)
		}
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeCircleID: world.home.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatal(err)
		}
		if got := sharedCategoriesOf(t, tx, world.ben.ID, world.ada.ID); !slices.Equal(got, []string{"sleep"}) {
			t.Fatalf("through the circle: %v", got)
		}
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"vitals", "activity"}}); err != nil {
			t.Fatal(err)
		}
		if got := sharedCategoriesOf(t, tx, world.ben.ID, world.ada.ID); !slices.Equal(got, []string{"activity", "sleep", "vitals"}) {
			t.Fatalf("the direct share and the circle's together, in the Health app's order: %v", got)
		}
		// A share is one way, and says nothing about the person's own.
		if got := sharedCategoriesOf(t, tx, world.ada.ID, world.ben.ID); len(got) != 0 {
			t.Fatalf("the other way: %v", got)
		}
		if got := sharedCategoriesOf(t, tx, world.ada.ID, world.ada.ID); len(got) != 0 {
			t.Fatalf("a person reads their own Health without a share: %v", got)
		}
		// Cora is in "club", not "home", and dave in nothing.
		for _, other := range []*models.User{world.cora, world.dave} {
			if got := sharedCategoriesOf(t, tx, other.ID, world.ada.ID); len(got) != 0 {
				t.Fatalf("%s: %v", other.Username, got)
			}
		}

		reaching, err := tx.ListHealthSharesReaching(world.ben.ID)
		if err != nil || len(reaching) != 2 {
			t.Fatalf("the shares that reach ben: %+v %v", reaching, err)
		}
		for _, each := range reaching {
			if each.UserID != world.ada.ID || each.OwnerUsername != "ada" || each.OwnerName == "" {
				t.Errorf("a share says whose Health it is: %+v", each)
			}
		}
		if none, _ := tx.ListHealthSharesReaching(world.ada.ID); len(none) != 0 {
			t.Errorf("nothing reaches ada: %+v", none)
		}

		// A disabled owner's Health reaches nobody.
		if _, err := tx.UpdateUser(world.ada.ID, func(owner *models.User) error {
			now := time.Now()
			owner.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
		if got := sharedCategoriesOf(t, tx, world.ben.ID, world.ada.ID); len(got) != 0 {
			t.Fatalf("a disabled owner: %v", got)
		}
	})
}

// Health reaches only people who may use it, and only from an owner who may:
// taking the permission from a role switches Health off for those people.
func TestAHealthShareReachesOnlyPeopleWhoMayUseHealth(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newHealthShareWorld(t, tx)
		// Ben shares with the circle "club", which cora is in and may not use
		// Health.
		if _, err := tx.SetHealthShare(world.ben.ID, &models.HealthShare{GranteeCircleID: world.club.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatal(err)
		}
		if got := sharedCategoriesOf(t, tx, world.cora.ID, world.ben.ID); len(got) != 0 {
			t.Fatalf("a viewer without health:use: %v", got)
		}
		dbtest.GrantPermission(t, tx, world.cora.ID, models.PermissionHealthUse)
		if got := sharedCategoriesOf(t, tx, world.cora.ID, world.ben.ID); !slices.Equal(got, []string{"sleep"}) {
			t.Fatalf("with health:use: %v", got)
		}
		// Cora shares with ben; dave, who may use Health, is told nothing of
		// ben's.
		if got := sharedCategoriesOf(t, tx, world.dave.ID, world.ben.ID); len(got) != 0 {
			t.Fatalf("dave is in no circle with ben: %v", got)
		}
	})
}

// A share to a person ends when the two no longer share a circle, and a share
// to a circle ends when the sharer leaves it.
func TestLeavingACircleEndsTheHealthSharesItMadePossible(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newHealthShareWorld(t, tx)
		if _, err := tx.SetHealthShare(world.ada.ID, &models.HealthShare{GranteeUserID: world.ben.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetHealthShare(world.ben.ID, &models.HealthShare{GranteeCircleID: world.club.ID, HealthCategories: []string{"heart"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetHealthShare(world.ben.ID, &models.HealthShare{GranteeUserID: world.ada.ID, HealthCategories: []string{"activity"}}); err != nil {
			t.Fatal(err)
		}

		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := sharedCategoriesOf(t, tx, world.ben.ID, world.ada.ID); len(got) != 0 {
			t.Errorf("ada's share to ben after they no longer meet: %v", got)
		}
		if left, _ := tx.ListHealthShares(world.ada.ID); len(left) != 0 {
			t.Errorf("ada's share is gone, not just silent: %+v", left)
		}
		if left, _ := tx.ListHealthShares(world.ben.ID); len(left) != 1 || left[0].GranteeCircleName != "Club" {
			t.Errorf("ben's share to a circle he is still in stays: %+v", left)
		}
		if got := sharedCategoriesOf(t, tx, world.cora.ID, world.ben.ID); len(got) != 0 {
			// cora has no health:use here; the point is that nothing leaks.
			t.Errorf("cora: %v", got)
		}

		// A circle somebody else runs, which ben joins and leaves again.
		gym, err := tx.CreateCircle(&models.Circle{Name: "Gym", CircleKind: models.CircleKindFriends}, world.dave.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err := tx.AddCircleMember(gym.ID, world.ben.ID, models.MemberRoleMember, world.dave.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if _, err := tx.SetHealthShare(world.ben.ID, &models.HealthShare{GranteeCircleID: gym.ID, HealthCategories: []string{"sleep"}}); err != nil {
			t.Fatal(err)
		}
		if got := sharedCategoriesOf(t, tx, world.dave.ID, world.ben.ID); !slices.Equal(got, []string{"sleep"}) {
			t.Fatalf("dave reads what ben shares with the gym: %v", got)
		}
		if _, err := tx.RemoveCircleMember(gym.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if got := sharedCategoriesOf(t, tx, world.dave.ID, world.ben.ID); len(got) != 0 {
			t.Errorf("after ben left: %v", got)
		}
		for _, each := range mustListHealthShares(t, tx, world.ben.ID) {
			if each.GranteeCircleID == gym.ID {
				t.Errorf("ben's share to a circle he left is gone: %+v", each)
			}
		}
	})
}
