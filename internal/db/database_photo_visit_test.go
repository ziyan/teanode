package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Each window answers its own photos: counted, earliest first, cut to the
// preview, with the trash, other people and the edges left out.
func TestListVisitPhotos(t *testing.T) {
	dbtest.RunTransaction(t, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "visit-photo-owner"})
		if err != nil {
			t.Fatal(err)
		}
		other, err := transaction.CreateUser(&models.User{Username: "visit-photo-other"})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Date(2030, 3, 5, 9, 0, 0, 0, time.UTC)
		take := func(userId, name string, takenAt time.Time) *models.Photo {
			photo, _, err := transaction.CreatePhoto(&models.Photo{UserID: userId, ClientPhotoID: name, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceIPhone, TakenAt: &takenAt})
			if err != nil {
				t.Fatal(err)
			}
			return photo
		}
		var inFirst []string
		for index := range 6 {
			inFirst = append(inFirst, take(owner.ID, fmt.Sprintf("first-%d", index), start.Add(time.Duration(index)*time.Minute)).ID)
		}
		take(owner.ID, "at-the-end", start.Add(time.Hour))
		take(other.ID, "of-another-person", start.Add(time.Minute))
		trashed := take(owner.ID, "in-the-trash", start.Add(7*time.Minute))
		if _, err := transaction.TrashPhotos(owner.ID, []string{trashed.ID}); err != nil {
			t.Fatal(err)
		}
		found, err := transaction.ListVisitPhotos(owner.ID, []db.VisitPhotoWindow{
			{VisitID: "first", StartedAt: start, EndedAt: start.Add(time.Hour)},
			{VisitID: "empty", StartedAt: start.Add(2 * time.Hour), EndedAt: start.Add(3 * time.Hour)},
			{VisitID: "counted-only", StartedAt: start, EndedAt: start.Add(time.Hour)},
		}, 4)
		if err != nil {
			t.Fatal(err)
		}
		if got := found["first"]; got.PhotoCount != 6 || len(got.Photos) != 4 {
			t.Fatalf("six counted, four kept: %d %d", got.PhotoCount, len(got.Photos))
		}
		for index, photo := range found["first"].Photos {
			if photo.ID != inFirst[index] {
				t.Errorf("earliest first, photo %d: %s", index, photo.ID)
			}
		}
		if got := found["empty"]; got == nil || got.PhotoCount != 0 || len(got.Photos) != 0 {
			t.Errorf("an empty window: %v", got)
		}
		if got := found["counted-only"]; got.PhotoCount != 6 || len(got.Photos) != 4 {
			t.Errorf("a second window over the same span: %v", got)
		}
		none, err := transaction.ListVisitPhotos(owner.ID, []db.VisitPhotoWindow{{VisitID: "first", StartedAt: start, EndedAt: start.Add(time.Hour)}}, 0)
		if err != nil || none["first"].PhotoCount != 6 || len(none["first"].Photos) != 0 {
			t.Errorf("no preview still counts: %v %v", none["first"], err)
		}
	})
}
