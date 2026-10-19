package apigraph

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// allowPhotos lets a person of the location world use Photos.
func (self *locationWorld) allowPhotos(t *testing.T, user *models.User) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: user.Username + " photos role", Permissions: []models.Permission{models.PermissionPhotosUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: user.Username + " photos group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
	})
}

// takePhoto stores a photo of the person taken at a moment, and returns its ID.
func (self *locationWorld) takePhoto(t *testing.T, user *models.User, clientPhotoId string, takenAt time.Time) string {
	t.Helper()
	var photoId string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		photo, _, err := tx.CreatePhoto(&models.Photo{UserID: user.ID, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &takenAt})
		if err != nil {
			t.Fatalf("CreatePhoto: %s", err)
		}
		photoId = photo.ID
	})
	return photoId
}

const visitPhotosQuery = `query { ListLocationVisits%s { placeVisits { id photoCount photosTakenBefore photos { id takenAt } } } }`

func firstVisitOf(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	visits := data["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 1 {
		t.Fatalf("one visit wanted: %v", visits)
	}
	return visits[0].(map[string]any)
}

// A visit lists the photos taken during it, earliest first: not one taken
// before the arrival or after the departure, in the trash, or of somebody
// else. A family member reading the same visits, and a person who may not use
// Photos, get nulls.
func TestVisitsListThePhotosTakenDuringThem(t *testing.T) {
	world := newLocationWorld(t)
	world.allowPhotos(t, world.ada)
	arrival := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
	departure := arrival.Add(time.Hour)
	format := func(moment time.Time) string { return moment.Format(time.RFC3339) }

	world.report(t, world.ada, pointInput(40, -75, format(arrival)), visitInput("visit-with-photos", 40, -75, format(arrival), format(departure)))
	world.report(t, world.ben, pointInput(40, -75, format(arrival)), visitInput("visit-without-photos", 40, -75, format(arrival), format(departure)))
	world.takePhoto(t, world.ada, "before-arrival", arrival.Add(-10*time.Minute))
	second := world.takePhoto(t, world.ada, "second-during", arrival.Add(20*time.Minute))
	first := world.takePhoto(t, world.ada, "first-during", arrival.Add(10*time.Minute))
	world.takePhoto(t, world.ada, "after-departure", departure.Add(10*time.Minute))
	world.takePhoto(t, world.ada, "exactly-at-departure", departure)
	trashed := world.takePhoto(t, world.ada, "in-the-trash", arrival.Add(30*time.Minute))
	world.takePhoto(t, world.ben, "somebody-elses", arrival.Add(15*time.Minute))
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.TrashPhotos(world.ada.ID, []string{trashed}); err != nil {
			t.Fatalf("TrashPhotos: %s", err)
		}
	})

	visit := firstVisitOf(t, world.call(t, world.ada, fmt.Sprintf(visitPhotosQuery, "")))
	if visit["photoCount"] != float64(2) {
		t.Fatalf("two photos during the visit: %v", visit)
	}
	photos := visit["photos"].([]any)
	if len(photos) != 2 || photos[0].(map[string]any)["id"] != first || photos[1].(map[string]any)["id"] != second {
		t.Errorf("earliest first: %v", photos)
	}

	asAda := fmt.Sprintf(`(userId: %q)`, world.ada.ID)
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	seen := firstVisitOf(t, world.call(t, world.ben, fmt.Sprintf(visitPhotosQuery, asAda)))
	if seen["photoCount"] != nil || seen["photos"] != nil {
		t.Errorf("a family member sees no photos: %v", seen)
	}

	// ben may not use Photos, and has a photo during his own visit.
	unlisted := firstVisitOf(t, world.call(t, world.ben, fmt.Sprintf(visitPhotosQuery, "")))
	if unlisted["photoCount"] != nil || unlisted["photos"] != nil {
		t.Errorf("no photos:use, no photos: %v", unlisted)
	}
}

// More photos than the preview holds are counted, and only the first four are
// listed. A visit that has no arrival, and one nobody ended, are handled.
func TestVisitPhotoPreviewHoldsTheFirstFour(t *testing.T) {
	world := newLocationWorld(t)
	world.allowPhotos(t, world.ada)
	arrival := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	world.report(t, world.ada, pointInput(40, -75, arrival.Format(time.RFC3339)), visitInput("still-there", 40, -75, arrival.Format(time.RFC3339), ""))
	for index := range 6 {
		world.takePhoto(t, world.ada, fmt.Sprintf("during-%d", index), arrival.Add(time.Duration(index+1)*time.Minute))
	}
	visit := firstVisitOf(t, world.call(t, world.ada, fmt.Sprintf(visitPhotosQuery, "")))
	if visit["photoCount"] != float64(6) || len(visit["photos"].([]any)) != 4 {
		t.Errorf("six counted, four listed: %v", visit)
	}
}

// A visit nobody ended whose person has since been somewhere else ends at
// their latest position, not at now: a photo taken after it is not in it.
func TestOpenVisitThePersonLeftEndsAtTheirLatestPosition(t *testing.T) {
	world := newLocationWorld(t)
	world.allowPhotos(t, world.ada)
	arrival := time.Now().Add(-5 * 24 * time.Hour).UTC().Truncate(time.Second)
	latest := arrival.Add(2 * time.Hour)
	world.report(t, world.ada, pointInput(40, -75, arrival.Format(time.RFC3339)), visitInput("never-ended", 40, -75, arrival.Format(time.RFC3339), ""))
	world.report(t, world.ada, pointInput(45, -70, latest.Format(time.RFC3339)), "")
	world.takePhoto(t, world.ada, "while-there", arrival.Add(time.Hour))
	world.takePhoto(t, world.ada, "days-later", latest.Add(48*time.Hour))
	visit := firstVisitOf(t, world.call(t, world.ada, fmt.Sprintf(visitPhotosQuery, "")))
	if visit["photoCount"] != float64(1) {
		t.Errorf("only the photo taken before the latest position counts: %v", visit)
	}
	// Photos opened over the visit end where the count did.
	if ended, _ := time.Parse(time.RFC3339, fmt.Sprint(visit["photosTakenBefore"])); !ended.Equal(latest) {
		t.Errorf("the photos end at the latest position, not now: %v", visit["photosTakenBefore"])
	}
}
