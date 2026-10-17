package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// newLocationShareWorld is the people and circles the share tests use, all
// with the permission to use Location, as the ordinary roles give it.
func newLocationShareWorld(t *testing.T, tx db.Transaction) *shareWorld {
	t.Helper()
	world := newShareWorld(t, tx)
	role, err := tx.CreateRole(&models.Role{Name: "Location users", Permissions: []models.Permission{models.PermissionLocationUse}})
	if err != nil {
		t.Fatalf("CreateRole: %s", err)
	}
	if _, err := tx.CreateGroup(&models.Group{
		Name: "Location users", RoleIDs: []string{role.ID},
		UserIDs: []string{world.ada.ID, world.ben.ID, world.cora.ID, world.dave.ID},
	}); err != nil {
		t.Fatalf("CreateGroup: %s", err)
	}
	return world
}

func locationLevelOf(t *testing.T, tx db.Transaction, viewerUserId, ownerUserId string) models.LocationShareLevel {
	t.Helper()
	level, err := tx.GetLocationShareLevel(viewerUserId, ownerUserId)
	if err != nil {
		t.Fatalf("GetLocationShareLevel: %s", err)
	}
	return level
}

// Sharing a location needs a circle in common with a person or membership of a
// circle, and says not found rather than why, as a calendar does. Sharing again
// changes the level.
func TestLocationShareNeedsACircleInCommonAndChangesLevelWhenRepeated(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)

		share, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelCurrent})
		if err != nil || share.ID == "" || share.UserID != world.ada.ID || share.ShareLevel != models.LocationShareLevelCurrent {
			t.Fatalf("a share with someone in a shared circle: %+v %v", share, err)
		}
		again, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelHistory})
		if err != nil || again.ID != share.ID || again.ShareLevel != models.LocationShareLevelHistory {
			t.Fatalf("sharing again changes the level of the same share: %+v %v", again, err)
		}
		for _, stranger := range []*models.User{world.cora, world.dave} {
			if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: stranger.ID, ShareLevel: models.LocationShareLevelCurrent}); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("a share with %s needs a circle in common: %v", stranger.Username, err)
			}
		}
		if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}); err != nil {
			t.Fatalf("a share with her own circle: %s", err)
		}
		if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeCircleID: world.club.ID, ShareLevel: models.LocationShareLevelCurrent}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a share with a circle she is not in: %v", err)
		}
		for name, bad := range map[string]*models.LocationShare{
			"owner":    {GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelOwner},
			"empty":    {GranteeUserID: world.ben.ID},
			"both":     {GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent},
			"neither":  {ShareLevel: models.LocationShareLevelCurrent},
			"herself":  {GranteeUserID: world.ada.ID, ShareLevel: models.LocationShareLevelCurrent},
			"stranger": {GranteeUserID: "nobody", ShareLevel: models.LocationShareLevelCurrent},
		} {
			if _, err := tx.ShareLocation(world.ada.ID, bad); err == nil {
				t.Errorf("a share with %s is refused", name)
			}
		}

		listed, err := tx.ListLocationShares(world.ada.ID)
		if err != nil || len(listed) != 2 {
			t.Fatalf("ListLocationShares: %+v %v", listed, err)
		}
		if listed[0].GranteeUsername != "ben" || listed[0].GranteeName == "" || listed[1].GranteeCircleName != "Home" {
			t.Errorf("the listing says who each is for: %+v %+v", listed[0], listed[1])
		}
		if others, _ := tx.ListLocationShares(world.ben.ID); len(others) != 0 {
			t.Errorf("ben shares nothing: %+v", others)
		}
	})
}

// Only the sharer takes a share back.
func TestOnlyTheSharerTakesALocationShareBack(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)
		share, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelCurrent})
		if err != nil {
			t.Fatalf("ShareLocation: %s", err)
		}
		if err := tx.UnshareLocation(world.ben.ID, share.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("the grantee cannot take it back: %v", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != models.LocationShareLevelCurrent {
			t.Fatalf("still shared: %q", level)
		}
		if err := tx.UnshareLocation(world.ada.ID, share.ID); err != nil {
			t.Fatalf("UnshareLocation: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != "" {
			t.Fatalf("no longer shared: %q", level)
		}
		if err := tx.UnshareLocation(world.ada.ID, share.ID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a second time: %v", err)
		}
	})
}

// The viewer gets the highest of what reaches them, their own location is
// theirs, and a person nobody shares with gets nothing.
func TestAViewerGetsTheHighestLocationLevelReachingThem(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)
		if level := locationLevelOf(t, tx, world.ada.ID, world.ada.ID); level != models.LocationShareLevelOwner {
			t.Fatalf("their own: %q", level)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != "" {
			t.Fatalf("before any share: %q", level)
		}
		if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}); err != nil {
			t.Fatalf("ShareLocation: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != models.LocationShareLevelCurrent {
			t.Fatalf("through the circle: %q", level)
		}
		if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelHistory}); err != nil {
			t.Fatalf("ShareLocation: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != models.LocationShareLevelHistory {
			t.Fatalf("the better of direct and circle: %q", level)
		}
		// A share is one way.
		if level := locationLevelOf(t, tx, world.ada.ID, world.ben.ID); level != "" {
			t.Fatalf("the other way: %q", level)
		}
		// Cora is in "club", not "home", and dave in nothing.
		for _, other := range []*models.User{world.cora, world.dave} {
			if level := locationLevelOf(t, tx, other.ID, world.ada.ID); level != "" {
				t.Fatalf("%s: %q", other.Username, level)
			}
		}
		// A disabled owner's location reaches nobody.
		if _, err := tx.UpdateUser(world.ada.ID, func(owner *models.User) error {
			now := time.Now()
			owner.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != "" {
			t.Fatalf("a disabled owner: %q", level)
		}
	})
}

// The sharers a viewer sees and the grantees an owner's location reaches are
// the two sides of the same shares, each person once.
func TestLocationSharersAndGranteesAreTheTwoSidesOfTheShares(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)
		// Ben shares with ada directly at history and, through "club", with
		// cora; cora shares with the circle "club" at current.
		agents := map[string]*models.Agent{}
		for _, person := range []*models.User{world.ada, world.ben, world.cora} {
			found, err := tx.GetAgentByUser(person.ID)
			if err != nil {
				t.Fatalf("GetAgentByUser: %s", err)
			}
			if found == nil {
				if found, err = tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Tea"}); err != nil {
					t.Fatalf("CreateAgent: %s", err)
				}
			}
			agents[person.ID] = found
		}
		for _, each := range []struct {
			sharer string
			share  *models.LocationShare
		}{
			{world.ben.ID, &models.LocationShare{GranteeUserID: world.ada.ID, ShareLevel: models.LocationShareLevelHistory}},
			{world.ben.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}},
			{world.ben.ID, &models.LocationShare{GranteeCircleID: world.club.ID, ShareLevel: models.LocationShareLevelCurrent}},
			{world.cora.ID, &models.LocationShare{GranteeCircleID: world.club.ID, ShareLevel: models.LocationShareLevelCurrent}},
		} {
			if _, err := tx.ShareLocation(each.sharer, each.share); err != nil {
				t.Fatalf("ShareLocation: %s", err)
			}
		}

		sharers, err := tx.ListLocationSharers(world.ada.ID)
		if err != nil || len(sharers) != 1 || sharers[0].UserID != world.ben.ID || sharers[0].ShareLevel != models.LocationShareLevelHistory ||
			sharers[0].Username != "ben" || sharers[0].Name == "" || sharers[0].User == nil {
			t.Fatalf("ada sees ben once at the better level: %+v %v", sharers, err)
		}
		sharers, err = tx.ListLocationSharers(world.cora.ID)
		if err != nil || len(sharers) != 1 || sharers[0].UserID != world.ben.ID || sharers[0].ShareLevel != models.LocationShareLevelCurrent {
			t.Fatalf("cora sees ben, through the circle: %+v %v", sharers, err)
		}
		sharers, err = tx.ListLocationSharers(world.ben.ID)
		if err != nil || len(sharers) != 1 || sharers[0].UserID != world.cora.ID {
			t.Fatalf("ben sees cora, not himself: %+v %v", sharers, err)
		}
		if sharers, _ := tx.ListLocationSharers(world.dave.ID); len(sharers) != 0 {
			t.Fatalf("dave sees nobody: %+v", sharers)
		}

		grantees, err := tx.ListLocationGrantees(world.ben.ID)
		if err != nil || len(grantees) != 2 {
			t.Fatalf("ben's location reaches ada and cora once each: %+v %v", grantees, err)
		}
		seen := map[string]string{}
		for _, grantee := range grantees {
			seen[grantee.UserID] = grantee.AgentID
		}
		if seen[world.ada.ID] != agents[world.ada.ID].ID || seen[world.cora.ID] != agents[world.cora.ID].ID || seen[world.ben.ID] != "" {
			t.Fatalf("each with their agent and never the owner: %+v", seen)
		}

		// A grantee with no agent row is skipped.
		if _, err := tx.AddCircleMember(world.club.ID, world.dave.ID, models.MemberRoleMember, world.ben.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if daveAgent, err := tx.GetAgentByUser(world.dave.ID); err != nil {
			t.Fatalf("GetAgentByUser: %s", err)
		} else if daveAgent != nil {
			if err := tx.DeleteAgent(daveAgent.ID); err != nil {
				t.Fatalf("DeleteAgent: %s", err)
			}
		}
		grantees, err = tx.ListLocationGrantees(world.ben.ID)
		if err != nil || len(grantees) != 2 {
			t.Fatalf("dave has no agent: %d grantees, %v", len(grantees), err)
		}
	})
}

// Leaving a circle stops a location reaching at once, in both directions, for
// a direct share between two people who share nothing else and for a share to
// the circle left, and the shares do not come back when they meet again.
func TestLeavingACircleEndsLocationSharesAtOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)
		for _, each := range []struct {
			sharer string
			share  *models.LocationShare
		}{
			{world.ada.ID, &models.LocationShare{GranteeUserID: world.ben.ID, ShareLevel: models.LocationShareLevelCurrent}},
			{world.ben.ID, &models.LocationShare{GranteeUserID: world.ada.ID, ShareLevel: models.LocationShareLevelHistory}},
			{world.ada.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}},
			{world.ben.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}},
			// Ben and cora keep sharing through "club".
			{world.ben.ID, &models.LocationShare{GranteeUserID: world.cora.ID, ShareLevel: models.LocationShareLevelCurrent}},
		} {
			if _, err := tx.ShareLocation(each.sharer, each.share); err != nil {
				t.Fatalf("ShareLocation: %s", err)
			}
		}
		if locationLevelOf(t, tx, world.ben.ID, world.ada.ID) == "" || locationLevelOf(t, tx, world.ada.ID, world.ben.ID) == "" {
			t.Fatal("shared both ways before")
		}

		if _, err := tx.SetCircleMemberRole(world.home.ID, world.ben.ID, models.MemberRoleAdmin); err != nil {
			t.Fatalf("SetCircleMemberRole: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != "" {
			t.Errorf("ada to ben after she left: %q", level)
		}
		if level := locationLevelOf(t, tx, world.ada.ID, world.ben.ID); level != "" {
			t.Errorf("ben to ada after she left: %q", level)
		}
		if shares, _ := tx.ListLocationShares(world.ada.ID); len(shares) != 0 {
			t.Errorf("ada's shares, direct and to the circle she left, are gone: %+v", shares)
		}
		shares, _ := tx.ListLocationShares(world.ben.ID)
		if len(shares) != 2 {
			t.Errorf("ben keeps his share with cora and to the circle he is still in: %+v", shares)
		}
		if level := locationLevelOf(t, tx, world.cora.ID, world.ben.ID); level != models.LocationShareLevelCurrent {
			t.Errorf("cora still sees ben through club: %q", level)
		}

		if _, err := tx.AddCircleMember(world.club.ID, world.ada.ID, models.MemberRoleMember, world.ben.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ada.ID, world.ben.ID); level != "" {
			t.Errorf("a share does not come back when the two meet in another circle: %q", level)
		}
	})
}

// A circle share reaches only the members while the owner is in the circle,
// even before any cleanup ran.
func TestACircleLocationShareStopsWhenOneSideLeaves(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newLocationShareWorld(t, tx)
		if _, err := tx.ShareLocation(world.ada.ID, &models.LocationShare{GranteeCircleID: world.home.ID, ShareLevel: models.LocationShareLevelCurrent}); err != nil {
			t.Fatalf("ShareLocation: %s", err)
		}
		if _, err := tx.SetCircleMemberRole(world.home.ID, world.ben.ID, models.MemberRoleAdmin); err != nil {
			t.Fatalf("SetCircleMemberRole: %s", err)
		}
		if _, err := tx.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			t.Fatalf("RemoveCircleMember: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ben.ID, world.ada.ID); level != "" {
			t.Fatalf("a member who left: %q", level)
		}
	})
}

// A report is announced to every instance with the person's id and nothing
// else.
func TestALocationEventCarriesOnlyThePersonsID(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	payloads, err := database.ListenLocationEvents(ctx)
	if err != nil {
		t.Fatalf("ListenLocationEvents: %s", err)
	}
	if err := database.NotifyLocationEvent("a-person"); err != nil {
		t.Fatalf("NotifyLocationEvent: %s", err)
	}
	select {
	case payload := <-payloads:
		var fields map[string]any
		if err := json.Unmarshal([]byte(payload), &fields); err != nil || len(fields) != 1 || fields["userId"] != "a-person" {
			t.Fatalf("the payload is %q", payload)
		}
	case <-ctx.Done():
		t.Fatal("nothing was heard")
	}
}

// A person who is disabled, or whose roles no longer give location:use, cannot
// read a location, so a location does not reach them: not in what they may
// see and not in who is told when the owner arrives somewhere.
func TestALocationDoesNotReachSomeoneDisabledOrWithoutThePermission(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		world := newShareWorld(t, tx)
		role, err := tx.CreateRole(&models.Role{Name: "Location users", Permissions: []models.Permission{models.PermissionLocationUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		// Cora is not in the group, so she has no permission.
		if _, err := tx.CreateGroup(&models.Group{
			Name: "Location users", RoleIDs: []string{role.ID},
			UserIDs: []string{world.ada.ID, world.ben.ID},
		}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		for _, person := range []*models.User{world.ada, world.cora} {
			if found, err := tx.GetAgentByUser(person.ID); err != nil {
				t.Fatalf("GetAgentByUser: %s", err)
			} else if found == nil {
				if _, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Tea"}); err != nil {
					t.Fatalf("CreateAgent: %s", err)
				}
			}
		}
		// Ben shares with ada directly and with cora through "club".
		for _, share := range []*models.LocationShare{
			{GranteeUserID: world.ada.ID, ShareLevel: models.LocationShareLevelCurrent},
			{GranteeCircleID: world.club.ID, ShareLevel: models.LocationShareLevelHistory},
		} {
			if _, err := tx.ShareLocation(world.ben.ID, share); err != nil {
				t.Fatalf("ShareLocation: %s", err)
			}
		}

		if level := locationLevelOf(t, tx, world.cora.ID, world.ben.ID); level != "" {
			t.Errorf("cora has no location:use and sees ben at %q", level)
		}
		if sharers, err := tx.ListLocationSharers(world.cora.ID); err != nil || len(sharers) != 0 {
			t.Errorf("cora has no location:use and is offered sharers: %+v %v", sharers, err)
		}
		grantees, err := tx.ListLocationGrantees(world.ben.ID)
		if err != nil || len(grantees) != 1 || grantees[0].UserID != world.ada.ID {
			t.Fatalf("only ada, who can use Location, is told: %+v %v", grantees, err)
		}
		if grantees[0].SharedAt.IsZero() || time.Since(grantees[0].SharedAt) > time.Minute {
			t.Errorf("the grantee carries when the share was made: %v", grantees[0].SharedAt)
		}

		// Disabling ada ends it for her too.
		if _, err := tx.UpdateUser(world.ada.ID, func(user *models.User) error {
			disabledAt := time.Now()
			user.DisabledAt = &disabledAt
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
		if level := locationLevelOf(t, tx, world.ada.ID, world.ben.ID); level != "" {
			t.Errorf("a disabled ada sees ben at %q", level)
		}
		if grantees, err := tx.ListLocationGrantees(world.ben.ID); err != nil || len(grantees) != 0 {
			t.Errorf("a disabled ada is told: %+v %v", grantees, err)
		}
	})
}
