package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// fakeProvider answers by what it was told: a name for a point, nothing for
// another, a failure for a third.
type fakeProvider struct {
	asked []float64
}

func (self *fakeProvider) Name(_ context.Context, latitudeDegrees, longitudeDegrees float64) (*place.Found, error) {
	self.asked = append(self.asked, latitudeDegrees)
	switch latitudeDegrees {
	case 10:
		return &place.Found{
			PlaceName: "Maple Park", OSMType: "way", OSMID: 7, OSMCategory: "leisure=park", AddressText: "Maple Park, Elm",
			LatitudeDegrees: 10.0003, LongitudeDegrees: longitudeDegrees,
		}, nil
	case 20:
		return nil, nil
	case 40:
		// The nearest thing the provider has is 5 km away, which is not the place.
		return &place.Found{PlaceName: "Faraway Shop", OSMType: "node", OSMID: 9, LatitudeDegrees: 40.045, LongitudeDegrees: longitudeDegrees}, nil
	}
	return nil, fmt.Errorf("%w: dial tcp 10.0.0.5:2322 for lat=%v", place.ErrProviderUnreachable, latitudeDegrees)
}

func (self *fakeProvider) Search(context.Context, string, float64, float64, float64, int) ([]*place.Found, error) {
	return nil, nil
}

// Places that wait are named from what the provider knows; one it knows
// nothing about is named all the same, so that nothing asks again; one it
// fails on keeps waiting, with the reason on it.
func TestPlacesThatWaitAreNamed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	var user *models.User
	var placeIds [4]string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if user, err = tx.CreateUser(&models.User{Username: "place-job-person"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		for index, latitude := range []float64{10, 20, 30, 40} {
			made, err := tx.CreatePlace(user.ID, &models.Place{PlaceName: "Somewhere, Country", LatitudeDegrees: latitude, LongitudeDegrees: 5})
			if err != nil {
				t.Fatalf("CreatePlace: %s", err)
			}
			placeIds[index] = made.ID
		}
	})

	agent := &Agent{settings: &Settings{Database: database}}
	provider := &fakeProvider{}
	if err := agent.namePlaces(context.Background(), provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(provider.asked) != 4 {
		t.Errorf("each place was asked about once: %v", provider.asked)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		named, err := tx.GetPlace(user.ID, placeIds[0])
		if err != nil || named.PlaceName != "Maple Park" || named.NamedAt == nil || named.OSMID != 7 || named.OSMCategory != "leisure=park" {
			t.Errorf("named from the map: %+v %v", named, err)
		}
		unknown, err := tx.GetPlace(user.ID, placeIds[1])
		if err != nil || unknown.NamedAt == nil || unknown.PlaceName != "Somewhere, Country" {
			t.Errorf("known to the map as nothing: it keeps its city and is not asked again: %+v %v", unknown, err)
		}
		failed, err := tx.GetPlace(user.ID, placeIds[2])
		if err != nil || failed.NamedAt != nil || failed.NamingErrorMessage != "could not reach the place provider" {
			t.Errorf("a failure is recorded on the place as a sentence that names no address, and it waits: %+v %v", failed, err)
		}
		faraway, err := tx.GetPlace(user.ID, placeIds[3])
		if err != nil || faraway.NamedAt == nil || faraway.PlaceName != "Somewhere, Country" || faraway.OSMID != 0 || faraway.OSMName != "" {
			t.Errorf("an answer farther than the place's radius is nothing known, and the place is not asked about again: %+v %v", faraway, err)
		}
	})

	// A second look, at once, leaves the failed place alone: it is claimed.
	provider.asked = nil
	if err := agent.namePlaces(context.Background(), provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(provider.asked) != 0 {
		t.Errorf("the failed place is left until its claim is old: %v", provider.asked)
	}
	// And once the claim is old it is asked about again.
	if err := agent.namePlaces(context.Background(), provider, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(provider.asked) != 1 || provider.asked[0] != 30 {
		t.Errorf("asked again: %v", provider.asked)
	}
}
