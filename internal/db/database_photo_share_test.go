package db_test

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A world of five people. The circle "home" has erin as its admin and ada, ben
// and cora as members; the circle "club" has ben as its admin and dave as a
// member, so dave shares a circle with ben and with nobody else. Ada's album
// is the one shared, and the circle's album is the one every member of "home"
// adds to.
type photoShareWorld struct {
	ada, ben, cora, dave, erin *models.User
	home, club                 *models.Circle
}

func newPhotoShareWorld(test *testing.T, transaction db.Transaction) *photoShareWorld {
	test.Helper()
	world := &photoShareWorld{}
	for username, target := range map[string]**models.User{"ada": &world.ada, "ben": &world.ben, "cora": &world.cora, "dave": &world.dave, "erin": &world.erin} {
		made, err := transaction.CreateUser(&models.User{Username: username})
		if err != nil {
			test.Fatalf("CreateUser %s: %s", username, err)
		}
		*target = made
	}
	var err error
	if world.home, err = transaction.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, world.erin.ID); err != nil {
		test.Fatal(err)
	}
	for _, member := range []*models.User{world.ada, world.ben, world.cora} {
		if _, err := transaction.AddCircleMember(world.home.ID, member.ID, models.MemberRoleMember, world.erin.ID); err != nil {
			test.Fatal(err)
		}
	}
	if world.club, err = transaction.CreateCircle(&models.Circle{Name: "Club", CircleKind: models.CircleKindFriends}, world.ben.ID); err != nil {
		test.Fatal(err)
	}
	if _, err := transaction.AddCircleMember(world.club.ID, world.dave.ID, models.MemberRoleMember, world.ben.ID); err != nil {
		test.Fatal(err)
	}
	return world
}

// sharedTestPhoto makes a photo of a person's, with a place when one is given.
func sharedTestPhoto(test *testing.T, transaction db.Transaction, owner *models.User, clientPhotoId, placeName string) *models.Photo {
	test.Helper()
	photo := &models.Photo{UserID: owner.ID, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}
	if placeName != "" {
		latitude, longitude := 10.5, 20.5
		photo.Latitude, photo.Longitude, photo.PlaceName = &latitude, &longitude, placeName
	}
	made, _, err := transaction.CreatePhoto(photo)
	if err != nil {
		test.Fatal(err)
	}
	return made
}

func shareAlbum(test *testing.T, transaction db.Transaction, owner *models.User, photoAlbumId string, granteeUserId, granteeCircleId string, shareLevel models.PhotoAlbumShareLevel, isLocationIncluded bool) *models.PhotoAlbumShare {
	test.Helper()
	share, err := transaction.SharePhotoAlbum(owner.ID, &models.PhotoAlbumShare{
		PhotoAlbumID: photoAlbumId, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, ShareLevel: shareLevel, IsLocationIncluded: isLocationIncluded,
	})
	if err != nil {
		test.Fatalf("SharePhotoAlbum: %s", err)
	}
	return share
}

func isNotFound(err error) bool { return errors.Is(err, db.ErrNotFound) }

// Who reaches an album, at what level, and with what powers: its owner, the
// people it is shared with directly or through a circle, and nobody else. A
// person who shares a circle with the owner but was given nothing has nothing.
func TestWhoReachesAnAlbumAndAtWhatLevel(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		photo := sharedTestPhoto(test, transaction, world.ada, "trip-one", "Harbor")
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", []string{photo.ID})
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ada, album.ID, world.ben.ID, "", models.PhotoAlbumShareLevelView, false)
		shareAlbum(test, transaction, world.ada, album.ID, world.cora.ID, "", models.PhotoAlbumShareLevelContribute, true)

		type expectation struct {
			isReached          bool
			isOwner            bool
			shareLevel         models.PhotoAlbumShareLevel
			isManager          bool
			isLocationIncluded bool
		}
		check := func(name string, user *models.User, want expectation) {
			test.Helper()
			access, err := transaction.GetPhotoAlbumAccess(user.ID, album.ID)
			if !want.isReached {
				if !isNotFound(err) {
					test.Errorf("%s: an album nobody gave them is reached: %+v %v", name, access, err)
				}
				return
			}
			if err != nil {
				test.Errorf("%s: %s", name, err)
				return
			}
			if access.IsOwner != want.isOwner || access.ShareLevel != want.shareLevel || access.IsManager != want.isManager || access.IsLocationIncluded != want.isLocationIncluded ||
				access.OwnerUserID != world.ada.ID || access.CircleID != "" {
				test.Errorf("%s: %+v, want %+v", name, access, want)
			}
		}
		check("the owner", world.ada, expectation{isReached: true, isOwner: true, isManager: true})
		check("a viewer", world.ben, expectation{isReached: true, shareLevel: models.PhotoAlbumShareLevelView})
		check("a contributor", world.cora, expectation{isReached: true, shareLevel: models.PhotoAlbumShareLevelContribute, isLocationIncluded: true})
		check("a circle admin the album was not given to", world.erin, expectation{})
		check("a stranger", world.dave, expectation{})

		// The same for the photo in it.
		for name, want := range map[string]struct {
			user               *models.User
			isReached          bool
			isOwner            bool
			isLocationIncluded bool
		}{
			"the owner":       {world.ada, true, true, true},
			"a viewer":        {world.ben, true, false, false},
			"a contributor":   {world.cora, true, false, true},
			"a circle admin":  {world.erin, false, false, false},
			"a stranger":      {world.dave, false, false, false},
			"somebody absent": {&models.User{ID: "nobody"}, false, false, false},
		} {
			access, err := transaction.GetPhotoAccess(want.user.ID, photo.ID)
			if !want.isReached {
				if !isNotFound(err) {
					test.Errorf("%s: %+v %v", name, access, err)
				}
				continue
			}
			if err != nil || access.IsOwner != want.isOwner || access.IsLocationIncluded != want.isLocationIncluded || access.OwnerUserID != world.ada.ID {
				test.Errorf("%s: %+v %v", name, access, err)
			}
		}

		// Shared with the circle as well, everyone in it reaches it at the
		// level the circle was given; a person given both has the highest.
		shareAlbum(test, transaction, world.ada, album.ID, "", world.home.ID, models.PhotoAlbumShareLevelView, false)
		check("a circle admin once the circle has it", world.erin, expectation{isReached: true, shareLevel: models.PhotoAlbumShareLevelView})
		check("a viewer who is also in the circle", world.ben, expectation{isReached: true, shareLevel: models.PhotoAlbumShareLevelView})
		check("a contributor who is also in the circle", world.cora, expectation{isReached: true, shareLevel: models.PhotoAlbumShareLevelContribute, isLocationIncluded: true})

		// The trash is the owner's alone.
		if _, err := transaction.TrashPhotos(world.ada.ID, []string{photo.ID}); err != nil {
			test.Fatal(err)
		}
		if access, err := transaction.GetPhotoAccess(world.ada.ID, photo.ID); err != nil || !access.IsOwner {
			test.Fatalf("the owner reads a photo in the trash: %+v %v", access, err)
		}
		if _, err := transaction.GetPhotoAccess(world.ben.ID, photo.ID); !isNotFound(err) {
			test.Fatalf("a photo in the trash is gone from the album for everybody else: %v", err)
		}
	})
}

// A circle's album is read and added to by every member, managed by the
// circle's admins, and never shared further. Its photos stay the photos of
// whoever added them.
func TestACirclesAlbumBelongsToItsMembers(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		benPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-one", "Harbor")
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-one", "")
		if _, err := transaction.CreateCirclePhotoAlbum(world.dave.ID, world.home.ID, "Not mine", nil); !isNotFound(err) {
			test.Fatalf("somebody not in the circle made its album: %v", err)
		}
		album, err := transaction.CreateCirclePhotoAlbum(world.ben.ID, world.home.ID, "Family", []string{benPhoto.ID})
		if err != nil || album.CircleID == nil || *album.CircleID != world.home.ID || album.OwnerUserID != nil || album.CircleName != "Home" || album.PhotoCount != 1 {
			test.Fatalf("%+v %v", album, err)
		}
		if _, err := transaction.CreateCirclePhotoAlbum(world.ben.ID, world.home.ID, "Taken", []string{adaPhoto.ID}); !isNotFound(err) {
			test.Fatalf("somebody else's photo went into an album: %v", err)
		}

		for name, want := range map[string]struct {
			user       *models.User
			isReached  bool
			isManager  bool
			isInCircle bool
		}{
			"a member":     {world.ada, true, false, true},
			"the maker":    {world.ben, true, false, true},
			"the admin":    {world.erin, true, true, true},
			"a stranger":   {world.dave, false, false, false},
			"another cora": {world.cora, true, false, true},
		} {
			access, err := transaction.GetPhotoAlbumAccess(want.user.ID, album.ID)
			if !want.isReached {
				if !isNotFound(err) {
					test.Errorf("%s: %+v %v", name, access, err)
				}
				continue
			}
			if err != nil || access.IsOwner || access.ShareLevel != models.PhotoAlbumShareLevelContribute || access.IsManager != want.isManager ||
				access.CircleID != world.home.ID || access.OwnerUserID != "" || access.IsLocationIncluded {
				test.Errorf("%s: %+v %v", name, access, err)
			}
			if !access.CanContribute() {
				test.Errorf("%s may add to the circle's album", name)
			}
		}

		// Everybody adds their own photos, and only theirs.
		if added, err := transaction.AddPhotosToAlbum(world.ada.ID, album.ID, []string{adaPhoto.ID}); err != nil || added != 1 {
			test.Fatalf("a member adds their photo: %d %v", added, err)
		}
		if _, err := transaction.AddPhotosToAlbum(world.ada.ID, album.ID, []string{benPhoto.ID}); !isNotFound(err) {
			test.Fatalf("a member added somebody else's photo: %v", err)
		}
		if _, err := transaction.AddPhotosToAlbum(world.dave.ID, album.ID, nil); !isNotFound(err) {
			test.Fatalf("a stranger added to the album: %v", err)
		}
		// The photos are the photos of those who added them.
		if access, err := transaction.GetPhotoAccess(world.ada.ID, benPhoto.ID); err != nil || access.IsOwner || access.OwnerUserID != world.ben.ID || access.IsLocationIncluded {
			test.Fatalf("a member reads a member's photo, without its place: %+v %v", access, err)
		}

		// A member takes out the photos they added; the admin any photo.
		if removed, err := transaction.RemovePhotosFromAlbum(world.ada.ID, album.ID, []string{benPhoto.ID}); err != nil || removed != 0 {
			test.Fatalf("a member took out somebody else's photo: %d %v", removed, err)
		}
		if removed, err := transaction.RemovePhotosFromAlbum(world.erin.ID, album.ID, []string{benPhoto.ID}); err != nil || removed != 1 {
			test.Fatalf("the admin took out a member's photo: %d %v", removed, err)
		}
		if removed, err := transaction.RemovePhotosFromAlbum(world.ada.ID, album.ID, []string{adaPhoto.ID}); err != nil || removed != 1 {
			test.Fatalf("a member took out their own photo: %d %v", removed, err)
		}
		if _, err := transaction.RemovePhotosFromAlbum(world.dave.ID, album.ID, []string{adaPhoto.ID}); !isNotFound(err) {
			test.Fatalf("a stranger took out a photo: %v", err)
		}

		// Renaming, choosing the cover, sharing and deleting are the admin's.
		if _, err := transaction.AddPhotosToAlbum(world.ben.ID, album.ID, []string{benPhoto.ID}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.RenamePhotoAlbum(world.ada.ID, album.ID, "Mine now"); !isNotFound(err) {
			test.Fatalf("a member renamed the album: %v", err)
		}
		if _, err := transaction.SetPhotoAlbumCover(world.ben.ID, album.ID, benPhoto.ID); !isNotFound(err) {
			test.Fatalf("a member chose the cover: %v", err)
		}
		if err := transaction.DeletePhotoAlbum(world.ada.ID, album.ID); !isNotFound(err) {
			test.Fatalf("a member deleted the album: %v", err)
		}
		if _, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: world.dave.ID, ShareLevel: models.PhotoAlbumShareLevelView}); !isNotFound(err) {
			test.Fatalf("a member shared the album: %v", err)
		}
		if _, err := transaction.SharePhotoAlbum(world.erin.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: world.ada.ID, ShareLevel: models.PhotoAlbumShareLevelView}); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("an album that belongs to a circle is not shared further: %v", err)
		}
		if _, err := transaction.ListPhotoAlbumShares(world.ada.ID, album.ID); !isNotFound(err) {
			test.Fatalf("a member listed the shares: %v", err)
		}
		if renamed, err := transaction.RenamePhotoAlbum(world.erin.ID, album.ID, "Family album"); err != nil || renamed.AlbumTitle != "Family album" || !renamed.CanCallerManage {
			test.Fatalf("the admin renamed it: %+v %v", renamed, err)
		}
		if covered, err := transaction.SetPhotoAlbumCover(world.erin.ID, album.ID, benPhoto.ID); err != nil || covered.CoverPhotoID == nil {
			test.Fatalf("the admin chose the cover: %+v %v", covered, err)
		}

		// The circle's album is in every member's listing, marked as theirs to
		// add to and the circle's to manage.
		listed, err := transaction.ListPhotoAlbums(world.cora.ID)
		if err != nil || len(listed) != 1 || listed[0].CircleName != "Home" || listed[0].IsOwnedByCaller || listed[0].ShareLevel != models.PhotoAlbumShareLevelContribute ||
			!listed[0].CanCallerContribute || listed[0].CanCallerManage {
			test.Fatalf("%+v %v", listed, err)
		}
		if listed, err := transaction.ListPhotoAlbums(world.dave.ID); err != nil || len(listed) != 0 {
			test.Fatalf("a stranger lists %+v %v", listed, err)
		}
		if err := transaction.DeletePhotoAlbum(world.erin.ID, album.ID); err != nil {
			test.Fatalf("the admin deleted the album: %s", err)
		}
		// The photos are still their owners'.
		if _, err := transaction.GetPhoto(world.ben.ID, benPhoto.ID); err != nil {
			test.Fatalf("a photo went with the album: %s", err)
		}
	})
}

// Sharing needs a circle in common, says not found rather than why, and
// sharing again changes what the grantee has instead of adding a share.
func TestSharingAnAlbumNeedsACircleInCommonAndRepeatingChangesIt(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", nil)
		if err != nil {
			test.Fatal(err)
		}
		share := shareAlbum(test, transaction, world.ada, album.ID, world.ben.ID, "", models.PhotoAlbumShareLevelView, false)
		if share.ShareLevel != models.PhotoAlbumShareLevelView || share.IsLocationIncluded || share.GranteeUserID != world.ben.ID || share.CreatedByUserID != world.ada.ID {
			test.Fatalf("%+v", share)
		}

		// Nobody dave shares a circle with is ada; a circle she is not in.
		if _, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: world.dave.ID, ShareLevel: models.PhotoAlbumShareLevelView}); !isNotFound(err) {
			test.Fatalf("a share with somebody she shares no circle with: %v", err)
		}
		if _, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeCircleID: world.club.ID, ShareLevel: models.PhotoAlbumShareLevelView}); !isNotFound(err) {
			test.Fatalf("a share with a circle she is not in: %v", err)
		}
		if _, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: "nobody", ShareLevel: models.PhotoAlbumShareLevelView}); !isNotFound(err) {
			test.Fatalf("a share with an account that is not there: %v", err)
		}
		// Somebody else's album is not hers to share, and not found.
		if _, err := transaction.SharePhotoAlbum(world.ben.ID, &models.PhotoAlbumShare{PhotoAlbumID: album.ID, GranteeUserID: world.cora.ID, ShareLevel: models.PhotoAlbumShareLevelContribute}); !isNotFound(err) {
			test.Fatalf("a viewer shared the album: %v", err)
		}
		for name, bad := range map[string]*models.PhotoAlbumShare{
			"a level that is not one": {PhotoAlbumID: album.ID, GranteeUserID: world.ben.ID, ShareLevel: "owner"},
			"no level":                {PhotoAlbumID: album.ID, GranteeUserID: world.ben.ID},
			"both":                    {PhotoAlbumID: album.ID, GranteeUserID: world.ben.ID, GranteeCircleID: world.home.ID, ShareLevel: models.PhotoAlbumShareLevelView},
			"neither":                 {PhotoAlbumID: album.ID, ShareLevel: models.PhotoAlbumShareLevelView},
			"herself":                 {PhotoAlbumID: album.ID, GranteeUserID: world.ada.ID, ShareLevel: models.PhotoAlbumShareLevelView},
		} {
			if _, err := transaction.SharePhotoAlbum(world.ada.ID, bad); err == nil {
				test.Errorf("a share with %s is refused", name)
			}
		}

		// Again: one row, with the new level and the new choice about places.
		again := shareAlbum(test, transaction, world.ada, album.ID, world.ben.ID, "", models.PhotoAlbumShareLevelContribute, true)
		if again.ID != share.ID || again.ShareLevel != models.PhotoAlbumShareLevelContribute || !again.IsLocationIncluded {
			test.Fatalf("%+v became %+v", share, again)
		}
		shares, err := transaction.ListPhotoAlbumShares(world.ada.ID, album.ID)
		if err != nil || len(shares) != 1 || shares[0].GranteeUsername != "ben" || shares[0].GranteeName != "ben" || shares[0].GranteeUser == nil {
			test.Fatalf("%+v %v", shares, err)
		}
		shareAlbum(test, transaction, world.ada, album.ID, "", world.home.ID, models.PhotoAlbumShareLevelView, false)
		if shares, err := transaction.ListPhotoAlbumShares(world.ada.ID, album.ID); err != nil || len(shares) != 2 || shares[1].GranteeCircleName != "Home" {
			test.Fatalf("%+v %v", shares, err)
		}

		// Changing a share and taking it back are the owner's alone.
		if _, err := transaction.UpdatePhotoAlbumShare(world.ben.ID, share.ID, models.PhotoAlbumShareLevelView, false); !isNotFound(err) {
			test.Fatalf("the grantee changed their own share: %v", err)
		}
		if err := transaction.DeletePhotoAlbumShare(world.ben.ID, share.ID); !isNotFound(err) {
			test.Fatalf("the grantee removed their own share: %v", err)
		}
		if _, err := transaction.ListPhotoAlbumShares(world.ben.ID, album.ID); !isNotFound(err) {
			test.Fatalf("the grantee listed the shares: %v", err)
		}
		if _, err := transaction.UpdatePhotoAlbumShare(world.ada.ID, share.ID, "owner", false); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatalf("a share is not updated to a level that is not one: %v", err)
		}
		updated, err := transaction.UpdatePhotoAlbumShare(world.ada.ID, share.ID, models.PhotoAlbumShareLevelView, false)
		if err != nil || updated.ShareLevel != models.PhotoAlbumShareLevelView || updated.IsLocationIncluded {
			test.Fatalf("%+v %v", updated, err)
		}
		if err := transaction.DeletePhotoAlbumShare(world.ada.ID, share.ID); err != nil {
			test.Fatal(err)
		}
		if err := transaction.DeletePhotoAlbumShare(world.ada.ID, share.ID); !isNotFound(err) {
			test.Fatalf("a share removed twice: %v", err)
		}
		// Still reached through the circle's share.
		if access, err := transaction.GetPhotoAlbumAccess(world.ben.ID, album.ID); err != nil || access.ShareLevel != models.PhotoAlbumShareLevelView {
			test.Fatalf("%+v %v", access, err)
		}
	})
}

// A person who may contribute adds their own photos and takes out the ones they
// added; a viewer does neither; the owner takes out any.
func TestContributorsAddAndRemoveOnlyTheirOwnPhotos(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-one", "")
		coraPhoto := sharedTestPhoto(test, transaction, world.cora, "cora-one", "")
		benPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-one", "")
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", []string{adaPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ada, album.ID, world.ben.ID, "", models.PhotoAlbumShareLevelView, false)
		shareAlbum(test, transaction, world.ada, album.ID, world.cora.ID, "", models.PhotoAlbumShareLevelContribute, false)

		if _, err := transaction.AddPhotosToAlbum(world.ben.ID, album.ID, []string{benPhoto.ID}); !isNotFound(err) {
			test.Fatalf("a viewer added a photo: %v", err)
		}
		if _, err := transaction.AddPhotosToAlbum(world.dave.ID, album.ID, nil); !isNotFound(err) {
			test.Fatalf("a stranger added a photo: %v", err)
		}
		if _, err := transaction.AddPhotosToAlbum(world.cora.ID, album.ID, []string{adaPhoto.ID, coraPhoto.ID}); !isNotFound(err) {
			test.Fatalf("a contributor added the owner's photo: %v", err)
		}
		if added, err := transaction.AddPhotosToAlbum(world.cora.ID, album.ID, []string{coraPhoto.ID}); err != nil || added != 1 {
			test.Fatalf("a contributor adds their photo: %d %v", added, err)
		}
		if _, err := transaction.RemovePhotosFromAlbum(world.ben.ID, album.ID, []string{coraPhoto.ID}); !isNotFound(err) {
			test.Fatalf("a viewer took out a photo: %v", err)
		}
		if removed, err := transaction.RemovePhotosFromAlbum(world.cora.ID, album.ID, []string{adaPhoto.ID}); err != nil || removed != 0 {
			test.Fatalf("a contributor took out the owner's photo: %d %v", removed, err)
		}
		// The album counts and lists every photo in it, whoever added it.
		listed, err := transaction.ListPhotos(world.ben.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100})
		if err != nil || len(listed) != 2 {
			test.Fatalf("a viewer lists the album: %v %v", listed, err)
		}
		if count, err := transaction.CountPhotos(world.ben.ID, &db.PhotoFilter{AlbumID: album.ID}); err != nil || count != 2 {
			test.Fatalf("a viewer counts the album: %d %v", count, err)
		}
		if _, err := transaction.ListPhotos(world.dave.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100}); !isNotFound(err) {
			test.Fatalf("a stranger lists the album: %v", err)
		}
		// The owner's own library is still only the owner's.
		if own, err := transaction.ListPhotos(world.ada.ID, &db.PhotoFilter{PageSize: 100}); err != nil || len(own) != 1 {
			test.Fatalf("the owner's library holds a contributor's photo: %v %v", own, err)
		}
		if removed, err := transaction.RemovePhotosFromAlbum(world.ada.ID, album.ID, []string{coraPhoto.ID}); err != nil || removed != 1 {
			test.Fatalf("the owner took out a contributor's photo: %d %v", removed, err)
		}
		if _, err := transaction.GetPhoto(world.cora.ID, coraPhoto.ID); err != nil {
			test.Fatalf("the photo left the library with the album: %s", err)
		}
	})
}

// A listing of an album gives away a place by no other means than showing it:
// not a search for its words, not a distance from it, and not by the album's
// owner reading the photos others added, unless the share includes places.
func TestAnAlbumListingHoldsPlacesBack(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-harbor", "Harbor Quay")
		benPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-market", "Market Square")
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", []string{adaPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		share := shareAlbum(test, transaction, world.ada, album.ID, world.ben.ID, "", models.PhotoAlbumShareLevelContribute, false)
		if _, err := transaction.AddPhotosToAlbum(world.ben.ID, album.ID, []string{benPhoto.ID}); err != nil {
			test.Fatal(err)
		}
		near := func(userId string) []string {
			latitude, longitude, distance := 10.5, 20.5, 5.0
			listed, err := transaction.ListPhotos(userId, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, NearLatitude: &latitude, NearLongitude: &longitude, NearDistanceKilometers: &distance})
			if err != nil {
				test.Fatal(err)
			}
			return photoIdsOf(listed)
		}
		search := func(userId, words string) []string {
			listed, err := transaction.ListPhotos(userId, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, SearchText: words})
			if err != nil {
				test.Fatal(err)
			}
			count, err := transaction.CountPhotos(userId, &db.PhotoFilter{AlbumID: album.ID, SearchText: words})
			if err != nil || count != len(listed) {
				test.Fatalf("counted %d for %d listed: %v", count, len(listed), err)
			}
			return photoIdsOf(listed)
		}
		equal := func(got []string, want ...string) bool {
			return strings.Join(got, ",") == strings.Join(want, ",")
		}

		// Each sees the place of their own photo and of no other.
		if got := near(world.ben.ID); !equal(got, benPhoto.ID) {
			test.Errorf("ben's distance filter found %v, want only his own photo", got)
		}
		if got := near(world.ada.ID); !equal(got, adaPhoto.ID) {
			test.Errorf("the owner's distance filter found %v, want only her own photo", got)
		}
		if got := search(world.ben.ID, "market"); !equal(got, benPhoto.ID) {
			test.Errorf("ben searched his own place: %v", got)
		}
		if got := search(world.ben.ID, "harbor"); len(got) != 0 {
			test.Errorf("ben found the owner's photo by its place: %v", got)
		}
		if got := search(world.ada.ID, "harbor"); !equal(got, adaPhoto.ID) {
			test.Errorf("the owner searched her own place: %v", got)
		}
		if got := search(world.ada.ID, "market"); len(got) != 0 {
			test.Errorf("the owner found a contributor's photo by its place: %v", got)
		}
		// Part of a word is held back the same way.
		if got := search(world.ben.ID, "harb"); len(got) != 0 {
			test.Errorf("ben found the owner's photo by part of its place: %v", got)
		}
		// What is not a place is found, in the owner's and in the other's.
		if got := search(world.ben.ID, "trip"); len(got) != 2 {
			test.Errorf("the title of the album finds both photos: %v", got)
		}

		// Included, the places are the reader's to see and to search.
		if _, err := transaction.UpdatePhotoAlbumShare(world.ada.ID, share.ID, models.PhotoAlbumShareLevelContribute, true); err != nil {
			test.Fatal(err)
		}
		if got := search(world.ben.ID, "harbor"); !equal(got, adaPhoto.ID) {
			test.Errorf("ben searched a place the share includes: %v", got)
		}
		if got := near(world.ben.ID); len(got) != 2 {
			test.Errorf("ben's distance filter with places included: %v", got)
		}

		// The trash and the favorites are a person's own, in an album as in the
		// library.
		if _, err := transaction.TrashPhotos(world.ada.ID, []string{adaPhoto.ID}); err != nil {
			test.Fatal(err)
		}
		if listed, err := transaction.ListPhotos(world.ben.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100}); err != nil || len(listed) != 1 {
			test.Fatalf("a photo in the owner's trash is in the album: %v %v", listed, err)
		}
		if listed, err := transaction.ListPhotos(world.ben.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, IsTrashed: true}); err != nil || len(listed) != 0 {
			test.Fatalf("ben lists somebody else's trash: %v %v", listed, err)
		}
		if listed, err := transaction.ListPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, IsTrashed: true}); err != nil || len(listed) != 1 {
			test.Fatalf("the owner lists their trash: %v %v", listed, err)
		}
		isFavorite := true
		if _, err := transaction.UpdatePhoto(world.ben.ID, benPhoto.ID, &db.PhotoUpdate{IsFavorite: &isFavorite}); err != nil {
			test.Fatal(err)
		}
		if listed, err := transaction.ListPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, IsFavorite: &isFavorite}); err != nil || len(listed) != 0 {
			test.Fatalf("somebody else's favorite is one of mine: %v %v", listed, err)
		}
		if listed, err := transaction.ListPhotos(world.ben.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100, IsFavorite: &isFavorite}); err != nil || len(listed) != 1 {
			test.Fatalf("a favorite is the owner's: %v %v", listed, err)
		}
	})
}

// The list of a person's albums has their own, their circles' and those shared
// with them, each marked with whose it is and what the person may do.
func TestAlbumsAreListedWithWhoseTheyAreAndWhatThePersonMayDo(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		own, err := transaction.CreatePhotoAlbum(world.ben.ID, "Mine", nil)
		if err != nil {
			test.Fatal(err)
		}
		shared, err := transaction.CreatePhotoAlbum(world.ada.ID, "Hers", nil)
		if err != nil {
			test.Fatal(err)
		}
		hidden, err := transaction.CreatePhotoAlbum(world.ada.ID, "Hidden", nil)
		if err != nil {
			test.Fatal(err)
		}
		circle, err := transaction.CreateCirclePhotoAlbum(world.cora.ID, world.home.ID, "Ours", nil)
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ada, shared.ID, world.ben.ID, "", models.PhotoAlbumShareLevelView, true)

		albums, err := transaction.ListPhotoAlbums(world.ben.ID)
		if err != nil || len(albums) != 3 {
			test.Fatalf("%+v %v", albums, err)
		}
		byId := map[string]*models.PhotoAlbum{}
		for _, album := range albums {
			byId[album.ID] = album
		}
		if mine := byId[own.ID]; mine == nil || !mine.IsOwnedByCaller || mine.ShareLevel != "" || !mine.CanCallerManage || !mine.CanCallerContribute || mine.OwnerName != "ben" {
			test.Errorf("his own: %+v", mine)
		}
		if hers := byId[shared.ID]; hers == nil || hers.IsOwnedByCaller || hers.ShareLevel != models.PhotoAlbumShareLevelView || hers.CanCallerManage || hers.CanCallerContribute ||
			hers.OwnerName != "ada" || hers.OwnerUsername != "ada" || hers.OwnerUser == nil || !hers.IsLocationIncluded || hers.CircleID != nil {
			test.Errorf("shared with him: %+v", hers)
		}
		if ours := byId[circle.ID]; ours == nil || ours.IsOwnedByCaller || ours.ShareLevel != models.PhotoAlbumShareLevelContribute || ours.CircleName != "Home" || ours.OwnerUserID != nil || ours.CanCallerManage {
			test.Errorf("the circle's: %+v", ours)
		}
		if byId[hidden.ID] != nil {
			test.Errorf("an album nobody gave him is listed")
		}
		if _, err := transaction.GetPhotoAlbum(world.ben.ID, hidden.ID); !isNotFound(err) {
			test.Errorf("an album nobody gave him is read: %v", err)
		}
		if read, err := transaction.GetPhotoAlbum(world.ben.ID, shared.ID); err != nil || read.OwnerName != "ada" {
			test.Errorf("%+v %v", read, err)
		}
	})
}

// Everybody who reads an album, for telling them when photos are added.
func TestWhoReadsAnAlbum(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		album, err := transaction.CreatePhotoAlbum(world.ben.ID, "Trip", nil)
		if err != nil {
			test.Fatal(err)
		}
		readers := func(photoAlbumId string) string {
			userIds, err := transaction.ListPhotoAlbumReaderUserIDs(photoAlbumId)
			if err != nil {
				test.Fatal(err)
			}
			names := []string{}
			for _, userId := range userIds {
				user, err := transaction.GetUser(userId)
				if err != nil {
					test.Fatal(err)
				}
				names = append(names, user.Username)
			}
			// The reading is by identifier, which is not alphabetical.
			sort.Strings(names)
			return strings.Join(names, ",")
		}
		if got := readers(album.ID); got != "ben" {
			test.Fatalf("an album shared with nobody is read by %q", got)
		}
		shareAlbum(test, transaction, world.ben, album.ID, world.dave.ID, "", models.PhotoAlbumShareLevelView, false)
		shareAlbum(test, transaction, world.ben, album.ID, "", world.home.ID, models.PhotoAlbumShareLevelView, false)
		if got := readers(album.ID); got != "ada,ben,cora,dave,erin" {
			test.Fatalf("an album shared with a person and a circle is read by %q", got)
		}
		circleAlbum, err := transaction.CreateCirclePhotoAlbum(world.ada.ID, world.home.ID, "Ours", nil)
		if err != nil {
			test.Fatal(err)
		}
		if got := readers(circleAlbum.ID); got != "ada,ben,cora,erin" {
			test.Fatalf("a circle's album is read by its four members, not by %q", got)
		}
	})
}

// The face route finds whose photo a face is in before anything else.
func TestAFacesPhotoBelongsToItsOwner(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		photo := sharedTestPhoto(test, transaction, world.ada, "with-a-face", "")
		stored, err := transaction.StorePhotoFaces(world.ada.ID, photo.ID, []*models.PhotoFace{foundFaceAt(10, 0.1)})
		if err != nil || len(stored) != 1 {
			test.Fatalf("%v %v", stored, err)
		}
		if ownerUserId, err := transaction.GetPhotoFaceOwnerUserID(stored[0].ID); err != nil || ownerUserId != world.ada.ID {
			test.Fatalf("%q %v", ownerUserId, err)
		}
		if _, err := transaction.GetPhotoFaceOwnerUserID("no-such-face"); !isNotFound(err) {
			test.Fatalf("%v", err)
		}
	})
}

// Leaving a circle ends what the circle made possible in albums: the direct
// shares between people who no longer share any circle, in both directions,
// and the photos people put in albums they no longer reach. Somebody who still
// shares another circle keeps everything.
func TestLeavingACircleEndsWhatItAllowedInAlbums(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-one", "")
		benPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-one", "")
		coraPhoto := sharedTestPhoto(test, transaction, world.cora, "cora-one", "")
		bensOwnPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-two", "")

		// Ada's album, shared with ben directly and with cora through the
		// circle, both to contribute; ben's album, shared back with ada.
		adaAlbum, err := transaction.CreatePhotoAlbum(world.ada.ID, "Ada's", []string{adaPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		benAlbum, err := transaction.CreatePhotoAlbum(world.ben.ID, "Ben's", []string{bensOwnPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ada, adaAlbum.ID, world.ben.ID, "", models.PhotoAlbumShareLevelContribute, false)
		shareAlbum(test, transaction, world.ada, adaAlbum.ID, "", world.home.ID, models.PhotoAlbumShareLevelContribute, false)
		shareAlbum(test, transaction, world.ben, benAlbum.ID, world.ada.ID, "", models.PhotoAlbumShareLevelView, false)
		for _, add := range []struct {
			user  *models.User
			photo *models.Photo
		}{{world.ben, benPhoto}, {world.cora, coraPhoto}} {
			if _, err := transaction.AddPhotosToAlbum(add.user.ID, adaAlbum.ID, []string{add.photo.ID}); err != nil {
				test.Fatal(err)
			}
		}
		photoCount := func(photoAlbumId string) int {
			count, err := transaction.CountPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: photoAlbumId})
			if err != nil {
				test.Fatal(err)
			}
			return count
		}
		shareCount := func(user *models.User, photoAlbumId string) int {
			shares, err := transaction.ListPhotoAlbumShares(user.ID, photoAlbumId)
			if err != nil {
				test.Fatal(err)
			}
			return len(shares)
		}
		if photoCount(adaAlbum.ID) != 3 || shareCount(world.ada, adaAlbum.ID) != 2 || shareCount(world.ben, benAlbum.ID) != 1 {
			test.Fatalf("the setting: %d photos, %d + %d shares", photoCount(adaAlbum.ID), shareCount(world.ada, adaAlbum.ID), shareCount(world.ben, benAlbum.ID))
		}

		// Ben and ada also share the club, which ada joins. Ben leaves "home":
		// they still share a circle, so nothing ends for them; cora, who shares
		// only "home" with ada, is not affected by ben leaving.
		if _, err := transaction.AddCircleMember(world.club.ID, world.ada.ID, models.MemberRoleMember, world.ben.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.RemoveCircleMember(world.home.ID, world.ben.ID); err != nil {
			test.Fatal(err)
		}
		if photoCount(adaAlbum.ID) != 3 || shareCount(world.ada, adaAlbum.ID) != 2 || shareCount(world.ben, benAlbum.ID) != 1 {
			test.Fatalf("somebody who still shares another circle lost something: %d photos, %d + %d shares", photoCount(adaAlbum.ID), shareCount(world.ada, adaAlbum.ID), shareCount(world.ben, benAlbum.ID))
		}
		// The share to the circle no longer reaches ben, who is out of it,
		// but the direct one does.
		if access, err := transaction.GetPhotoAlbumAccess(world.ben.ID, adaAlbum.ID); err != nil || access.ShareLevel != models.PhotoAlbumShareLevelContribute {
			test.Fatalf("%+v %v", access, err)
		}

		// Ada leaves the club too: she and ben share nothing now. The direct
		// shares in both directions end, and ben's photo leaves her album.
		if _, err := transaction.RemoveCircleMember(world.club.ID, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		if photoCount(adaAlbum.ID) != 2 || shareCount(world.ada, adaAlbum.ID) != 1 || shareCount(world.ben, benAlbum.ID) != 0 {
			test.Fatalf("after they shared no circle: %d photos, %d + %d shares", photoCount(adaAlbum.ID), shareCount(world.ada, adaAlbum.ID), shareCount(world.ben, benAlbum.ID))
		}
		if _, err := transaction.GetPhotoAlbumAccess(world.ben.ID, adaAlbum.ID); !isNotFound(err) {
			test.Fatalf("ben still reaches her album: %v", err)
		}
		if _, err := transaction.GetPhoto(world.ben.ID, benPhoto.ID); err != nil {
			test.Fatalf("his photo left his library with the album: %s", err)
		}
		if _, err := transaction.GetPhotoAccess(world.ada.ID, benPhoto.ID); !isNotFound(err) {
			test.Fatalf("ada still reads the photo he took back: %v", err)
		}

		// The album ada shared with "home" stays reached by cora, with her
		// photo in it; ada leaves "home": the share to the circle is hers and
		// ends with her membership, and cora's photo leaves her album.
		if _, err := transaction.RemoveCircleMember(world.home.ID, world.ada.ID); err != nil {
			test.Fatal(err)
		}
		if shareCount(world.ada, adaAlbum.ID) != 0 || photoCount(adaAlbum.ID) != 1 {
			test.Fatalf("after the owner left: %d shares, %d photos", shareCount(world.ada, adaAlbum.ID), photoCount(adaAlbum.ID))
		}
		if _, err := transaction.GetPhotoAlbumAccess(world.cora.ID, adaAlbum.ID); !isNotFound(err) {
			test.Fatalf("cora still reaches an album of somebody who left: %v", err)
		}
	})
}

// Deleting a circle, and a circle whose last member leaves, take its albums
// with it and what it allowed, each member's cleanup as if they had left.
func TestDeletingACircleEndsItsAlbumsAndWhatItAllowed(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-one", "")
		coraPhoto := sharedTestPhoto(test, transaction, world.cora, "cora-one", "")
		circleAlbum, err := transaction.CreateCirclePhotoAlbum(world.ada.ID, world.home.ID, "Ours", []string{adaPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		// Cora adds to ada's own album through the circle, and ben's album
		// is shared with ada directly.
		adaAlbum, err := transaction.CreatePhotoAlbum(world.ada.ID, "Hers", nil)
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ada, adaAlbum.ID, "", world.home.ID, models.PhotoAlbumShareLevelContribute, false)
		if _, err := transaction.AddPhotosToAlbum(world.cora.ID, adaAlbum.ID, []string{coraPhoto.ID}); err != nil {
			test.Fatal(err)
		}
		benAlbum, err := transaction.CreatePhotoAlbum(world.ben.ID, "His", nil)
		if err != nil {
			test.Fatal(err)
		}
		shareAlbum(test, transaction, world.ben, benAlbum.ID, world.ada.ID, "", models.PhotoAlbumShareLevelView, false)

		if err := transaction.DeleteCircle(world.home.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoAlbum(world.ada.ID, circleAlbum.ID); !isNotFound(err) {
			test.Fatalf("the circle's album outlived the circle: %v", err)
		}
		for _, photo := range []*models.Photo{adaPhoto, coraPhoto} {
			if _, err := transaction.GetPhoto(photo.UserID, photo.ID); err != nil {
				test.Fatalf("a photo went with the circle's album: %s", err)
			}
		}
		// Nobody shares a circle with ada now (the club has ben and dave).
		if count, err := transaction.CountPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: adaAlbum.ID}); err != nil || count != 0 {
			test.Fatalf("cora's photo is still in ada's album: %d %v", count, err)
		}
		if _, err := transaction.GetPhotoAlbumAccess(world.ada.ID, benAlbum.ID); !isNotFound(err) {
			test.Fatalf("ada still reaches ben's album: %v", err)
		}
		var shares []*models.PhotoAlbumShare
		if shares, err = transaction.ListPhotoAlbumShares(world.ben.ID, benAlbum.ID); err != nil || len(shares) != 0 {
			test.Fatalf("a direct share outlived the circle: %+v %v", shares, err)
		}
	})
}

// A circle whose last member leaves is deleted, with its albums.
func TestTheLastMemberLeavingTakesTheCirclesAlbumAway(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		photo := sharedTestPhoto(test, transaction, world.dave, "dave-one", "")
		album, err := transaction.CreateCirclePhotoAlbum(world.dave.ID, world.club.ID, "Club album", []string{photo.ID})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.SetCircleMemberRole(world.club.ID, world.dave.ID, models.MemberRoleAdmin); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.RemoveCircleMember(world.club.ID, world.ben.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoAlbumAccess(world.dave.ID, album.ID); err != nil {
			test.Fatalf("the album went before the last member: %s", err)
		}
		if left, err := transaction.RemoveCircleMember(world.club.ID, world.dave.ID); err != nil || left != nil {
			test.Fatalf("%+v %v", left, err)
		}
		if _, err := transaction.GetPhotoAlbumAccess(world.dave.ID, album.ID); !isNotFound(err) {
			test.Fatalf("the album outlived its circle: %v", err)
		}
		if _, err := transaction.GetPhoto(world.dave.ID, photo.ID); err != nil {
			test.Fatalf("the photo went with the album: %s", err)
		}
	})
}

// Taking a share back takes with it the photos the people it reached put in the
// album, and leaves the photos of people who still reach it some other way.
func TestUnsharingTakesTheirPhotosOutWithTheirAccess(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world := newPhotoShareWorld(test, transaction)
		adaPhoto := sharedTestPhoto(test, transaction, world.ada, "ada-one", "")
		benPhoto := sharedTestPhoto(test, transaction, world.ben, "ben-one", "")
		coraPhoto := sharedTestPhoto(test, transaction, world.cora, "cora-one", "")
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", []string{adaPhoto.ID})
		if err != nil {
			test.Fatal(err)
		}
		circleShare := shareAlbum(test, transaction, world.ada, album.ID, "", world.home.ID, models.PhotoAlbumShareLevelContribute, false)
		directShare := shareAlbum(test, transaction, world.ada, album.ID, world.cora.ID, "", models.PhotoAlbumShareLevelView, false)
		for _, add := range []struct {
			user  *models.User
			photo *models.Photo
		}{{world.ben, benPhoto}, {world.cora, coraPhoto}} {
			if _, err := transaction.AddPhotosToAlbum(add.user.ID, album.ID, []string{add.photo.ID}); err != nil {
				test.Fatal(err)
			}
		}
		// Cora was given the album twice. Taking back the circle's share leaves
		// her direct one, and her photo; ben has nothing left and his goes.
		if err := transaction.DeletePhotoAlbumShare(world.ada.ID, circleShare.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.GetPhotoAccess(world.ada.ID, benPhoto.ID); !isNotFound(err) {
			test.Fatalf("ben's photo is still in the album after his access went: %v", err)
		}
		if _, err := transaction.GetPhotoAccess(world.ada.ID, coraPhoto.ID); err != nil {
			test.Fatalf("cora's photo went though she still reaches the album: %v", err)
		}
		if access, err := transaction.GetPhotoAlbumAccess(world.cora.ID, album.ID); err != nil || access.ShareLevel != models.PhotoAlbumShareLevelView {
			test.Fatalf("%+v %v", access, err)
		}
		// Taking her direct share back takes hers.
		if err := transaction.DeletePhotoAlbumShare(world.ada.ID, directShare.ID); err != nil {
			test.Fatal(err)
		}
		if listed, err := transaction.ListPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: album.ID, PageSize: 100}); err != nil || len(listed) != 1 || listed[0].ID != adaPhoto.ID {
			test.Fatalf("%v %v", listed, err)
		}
		// The photos are in their libraries still.
		for _, photo := range []*models.Photo{benPhoto, coraPhoto} {
			if _, err := transaction.GetPhoto(photo.UserID, photo.ID); err != nil {
				test.Fatal(err)
			}
		}
	})
}

// A share made, and a photo added, at the moment the person it depends on is
// leaving are not left behind. Changes to who is in a circle take turns with
// them (the circle membership lock), so each is made before the leaving and
// cleaned up by it, or after it and refused.
func TestAShareOrAPhotoMadeAsSomebodyLeavesIsNotLeftBehind(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	var world *photoShareWorld
	var albumId, coraPhotoId string
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		world = newPhotoShareWorld(test, transaction)
		album, err := transaction.CreatePhotoAlbum(world.ada.ID, "Trip", nil)
		if err != nil {
			test.Fatal(err)
		}
		albumId = album.ID
		shareAlbum(test, transaction, world.ada, album.ID, "", world.home.ID, models.PhotoAlbumShareLevelContribute, false)
		coraPhotoId = sharedTestPhoto(test, transaction, world.cora, "cora-one", "").ID
	})

	// leavingFirst runs the leaving without committing it, runs the other
	// change meanwhile, and commits the leaving.
	leavingFirst := func(leaverId string, other func(db.Transaction) error) error {
		firstHasLeft, mayCommit := make(chan struct{}), make(chan struct{})
		firstDone := make(chan error, 1)
		go func() {
			firstDone <- database.Transaction(func(transaction db.Transaction) error {
				if _, err := transaction.RemoveCircleMember(world.home.ID, leaverId); err != nil {
					return err
				}
				close(firstHasLeft)
				<-mayCommit
				return nil
			})
		}()
		<-firstHasLeft
		otherDone := make(chan error, 1)
		go func() { otherDone <- database.Transaction(other) }()
		var otherError error
		select {
		case otherError = <-otherDone:
			test.Log("the other change did not wait for the leaving")
			close(mayCommit)
		case <-time.After(500 * time.Millisecond):
			close(mayCommit)
			otherError = <-otherDone
		}
		if err := <-firstDone; err != nil {
			test.Fatal(err)
		}
		return otherError
	}

	// Cora adds her photo to ada's album, which she reaches through the
	// circle, as ada leaves the circle: the add waits and is refused, so
	// that no photo is left in an album of somebody she no longer shares a
	// circle with.
	err := leavingFirst(world.ada.ID, func(transaction db.Transaction) error {
		_, err := transaction.AddPhotosToAlbum(world.cora.ID, albumId, []string{coraPhotoId})
		return err
	})
	if !isNotFound(err) {
		test.Fatalf("a photo added as the owner left was kept or failed another way: %v", err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if count, err := transaction.CountPhotos(world.ada.ID, &db.PhotoFilter{AlbumID: albumId}); err != nil || count != 0 {
			test.Fatalf("a photo is in the album of somebody who left: %d %v", count, err)
		}
	})

	// A share to ben, made as ben leaves the only circle he shared with ada,
	// is refused or ended: either way ben does not reach the album.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.AddCircleMember(world.home.ID, world.ada.ID, models.MemberRoleMember, world.erin.ID); err != nil {
			test.Fatal(err)
		}
	})
	_ = leavingFirst(world.ben.ID, func(transaction db.Transaction) error {
		_, err := transaction.SharePhotoAlbum(world.ada.ID, &models.PhotoAlbumShare{PhotoAlbumID: albumId, GranteeUserID: world.ben.ID, ShareLevel: models.PhotoAlbumShareLevelView})
		return err
	})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.GetPhotoAlbumAccess(world.ben.ID, albumId); !isNotFound(err) {
			test.Fatalf("ben reaches an album shared with him as he left: %v", err)
		}
		shares, err := transaction.ListPhotoAlbumShares(world.ada.ID, albumId)
		if err != nil {
			test.Fatal(err)
		}
		for _, share := range shares {
			if share.GranteeUserID == world.ben.ID {
				test.Fatalf("a share to somebody who shares no circle is left: %+v", share)
			}
		}
	})
}
