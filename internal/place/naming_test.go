package place

import (
	"context"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// scriptedProvider answers every question the same way and counts them.
type scriptedProvider struct {
	found      *Found
	err        error
	askedCount int
}

func (self *scriptedProvider) Name(context.Context, float64, float64) (*Found, error) {
	self.askedCount++
	return self.found, self.err
}

func (self *scriptedProvider) Search(context.Context, string, float64, float64, float64, int) ([]*Found, error) {
	return nil, nil
}

func newWaitingPlace(t *testing.T, database db.Database, username string) (userId, placeId string) {
	t.Helper()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: username})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		made, err := tx.CreatePlace(user.ID, &models.Place{PlaceName: "Exampleton, Exampleland", LatitudeDegrees: 10.5, LongitudeDegrees: 20.5})
		if err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
		userId, placeId = user.ID, made.ID
	})
	return userId, placeId
}

func readPlace(t *testing.T, database db.Database, userId, placeId string) *models.Place {
	t.Helper()
	var read *models.Place
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if read, err = tx.GetPlace(userId, placeId); err != nil {
			t.Fatalf("GetPlace: %s", err)
		}
	})
	return read
}

// A place just made is named at once, from what the provider answers there, and
// the naming job does not ask about it again.
func TestNameNowNamesANewPlaceFromWhatTheProviderKnowsThere(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	userId, placeId := newWaitingPlace(t, database, "name-now-person")
	provider := &scriptedProvider{found: &Found{
		PlaceName: "Birch Cafe", OSMType: models.OSMTypeNode, OSMID: 11, OSMCategory: "amenity=cafe", AddressText: "1 Elm Street, Exampleton",
		LatitudeDegrees: 10.5002, LongitudeDegrees: 20.5,
	}}

	if err := NameNow(context.Background(), database, provider, placeId); err != nil {
		t.Fatalf("NameNow: %s", err)
	}
	named := readPlace(t, database, userId, placeId)
	if named.PlaceName != "Birch Cafe" || named.OSMID != 11 || named.OSMCategory != "amenity=cafe" || named.NamedAt == nil || named.NamingErrorMessage != "" {
		t.Errorf("named at once: %+v", named)
	}
	if err := NameNow(context.Background(), database, provider, placeId); err != nil || provider.askedCount != 1 {
		t.Errorf("a place that is named is not asked about again: %v, asked %d times", err, provider.askedCount)
	}
}

// A provider that answers with a place far from the point does not name it, and
// a failure is recorded as a sentence that says nothing of the provider's
// address or the coordinates, with the place left for the job to try again.
func TestNameNowDropsADistantAnswerAndRecordsAFailureAsASentence(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)

	userId, placeId := newWaitingPlace(t, database, "name-now-distant")
	distant := &scriptedProvider{found: &Found{
		PlaceName: "Faraway Shop", OSMType: models.OSMTypeNode, OSMID: 12, LatitudeDegrees: 10.55, LongitudeDegrees: 20.5,
	}}
	if err := NameNow(context.Background(), database, distant, placeId); err != nil {
		t.Fatalf("NameNow: %s", err)
	}
	if named := readPlace(t, database, userId, placeId); named.NamedAt == nil || named.OSMID != 0 || named.PlaceName != "Exampleton, Exampleland" {
		t.Errorf("an answer 5 km away is nothing known: %+v", named)
	}

	userId, placeId = newWaitingPlace(t, database, "name-now-failing")
	failing := &scriptedProvider{err: fmt.Errorf("%w: Get \"http://10.0.0.5:2322/reverse?lat=10.500000&lon=20.500000\": connection refused", ErrProviderUnreachable)}
	if err := NameNow(context.Background(), database, failing, placeId); err != nil {
		t.Fatalf("NameNow: %s", err)
	}
	waiting := readPlace(t, database, userId, placeId)
	if waiting.NamedAt != nil || waiting.NamingErrorMessage != "could not reach the place provider" {
		t.Errorf("a failure waits and says so in a sentence: %+v", waiting)
	}
	// The claim holds the place back from a second try for a while.
	if err := NameNow(context.Background(), database, failing, placeId); err != nil || failing.askedCount != 1 {
		t.Errorf("a claimed place is left alone: %v, asked %d times", err, failing.askedCount)
	}

	// No provider, no place, and no id are not failures.
	if err := NameNow(context.Background(), database, nil, placeId); err != nil {
		t.Errorf("no provider: %s", err)
	}
	if err := NameNow(context.Background(), database, failing, "no-such-place"); err != nil {
		t.Errorf("a place that is gone: %s", err)
	}
}
