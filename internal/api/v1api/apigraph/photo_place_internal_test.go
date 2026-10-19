package apigraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

var photoAndLocationAllowed = []models.Permission{models.PermissionPhotosUse, models.PermissionLocationUse}

// A place provider that answers every point with the same street and counts
// how often it is asked.
func fakeAddressProvider(test *testing.T, world *photoWorld, requestCount *atomic.Int32) {
	test.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"osm_type":"way","osm_id":7,"lat":"10.5","lon":"20.5","category":"building","type":"yes","display_name":"1 Fixture Road, Harbor City","address":{"house_number":"1","road":"Fixture Road","city":"Harbor City"}}`))
	}))
	test.Cleanup(provider.Close)
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		test.Fatal(err)
	}
}

func switchPlaceProviderOff(test *testing.T, world *photoWorld) {
	test.Helper()
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = ""
		return nil
	}); err != nil {
		test.Fatal(err)
	}
}

func TestPhotoPlaceIsTheOwnPlaceBeforeTheStreet(test *testing.T) {
	world := newPhotoWorld(test)
	var requestCount atomic.Int32
	fakeAddressProvider(test, world, &requestCount)
	adaId := world.person("place-ada", photoAndLocationAllowed...)
	photo := world.storePhoto(adaId, "harbor", "IMG_0001.HEIC", []byte("original"), nil)
	world.givePlace(photo.ID, "Harbor City, Exampleland")
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.CreatePlace(adaId, &models.Place{PlaceName: "Fixture Home", PlaceLabel: models.PlaceLabelHome, LatitudeDegrees: 10.5, LongitudeDegrees: 20.5}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view == nil || view.LocationPlace == nil || view.LocationPlace.PlaceName != "Fixture Home" || view.AddressText != "" {
			test.Fatalf("%+v %v", view, err)
		}
	})
	if requestCount.Load() != 0 {
		test.Fatalf("the provider was asked %d times for a photo taken inside the person's own place", requestCount.Load())
	}
	// Without the Location permission the same position gets the street.
	world.ask(adaId, photosAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view == nil || view.LocationPlace != nil || view.AddressText == "" {
			test.Fatalf("%+v %v", view, err)
		}
	})
}

func TestPhotoPlaceAddressIsRememberedByPosition(test *testing.T) {
	world := newPhotoWorld(test)
	var requestCount atomic.Int32
	fakeAddressProvider(test, world, &requestCount)
	adaId := world.person("place-street-ada", photoAndLocationAllowed...)
	first := world.storePhoto(adaId, "first", "first.jpg", []byte("first"), nil)
	second := world.storePhoto(adaId, "second", "second.jpg", []byte("second"), nil)
	world.givePlace(first.ID, "Harbor City, Exampleland")
	world.givePlace(second.ID, "Harbor City, Exampleland")
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: first.ID})
		if err != nil || view == nil || view.LocationPlace != nil || !strings.Contains(view.AddressText, "Fixture Road") {
			test.Fatalf("%+v %v", view, err)
		}
		askedBefore := requestCount.Load()
		if askedBefore == 0 {
			test.Fatal("the provider was never asked")
		}
		// Another photo at the same position, and the same photo again.
		for _, photoId := range []string{first.ID, second.ID} {
			again, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photoId})
			if err != nil || again == nil || again.AddressText != view.AddressText {
				test.Fatalf("%+v %v", again, err)
			}
		}
		if requestCount.Load() != askedBefore {
			test.Fatalf("the provider was asked again: %d requests, then %d", askedBefore, requestCount.Load())
		}
	})
}

func TestPhotoPlaceHasNoStreetWithoutAProvider(test *testing.T) {
	world := newPhotoWorld(test)
	switchPlaceProviderOff(test, world)
	adaId := world.person("place-off-ada", photoAndLocationAllowed...)
	photo := world.storePhoto(adaId, "harbor", "IMG_0001.HEIC", []byte("original"), nil)
	world.givePlace(photo.ID, "Harbor City, Exampleland")
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view == nil || view.LocationPlace != nil || view.AddressText != "" {
			test.Fatalf("%+v %v", view, err)
		}
	})
}

func TestPhotoPlaceIsNullWithoutAPosition(test *testing.T) {
	world := newPhotoWorld(test)
	var requestCount atomic.Int32
	fakeAddressProvider(test, world, &requestCount)
	adaId := world.person("place-none-ada", photoAndLocationAllowed...)
	unplaced := world.storePhoto(adaId, "unplaced", "unplaced.jpg", []byte("unplaced"), nil)
	removed := world.storePhoto(adaId, "removed", "removed.jpg", []byte("removed"), nil)
	world.givePlace(removed.ID, "Harbor City, Exampleland")
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		if view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: unplaced.ID}); err != nil || view != nil {
			test.Fatalf("a photo with no position: %+v %v", view, err)
		}
		if view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: removed.ID}); err != nil || view == nil {
			test.Fatalf("before removing: %+v %v", view, err)
		}
		if _, err := world.graph.RemovePhotoLocation(ctx, PhotoIDsArguments{PhotoIDs: []string{removed.ID}}); err != nil {
			test.Fatal(err)
		}
		if view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: removed.ID}); err != nil || view != nil {
			test.Fatalf("a removed location: %+v %v", view, err)
		}
	})
}

// Another person's photo shows its street only when the share includes where
// it was taken, and a person with no share finds nothing.
func TestPhotoPlaceOfSharedPhotoFollowsTheShare(test *testing.T) {
	world := newPhotoWorld(test)
	var requestCount atomic.Int32
	fakeAddressProvider(test, world, &requestCount)
	adaId := world.person("place-share-ada", photoAndLocationAllowed...)
	benId := world.person("place-share-ben", photoAndLocationAllowed...)
	daveId := world.person("place-share-dave", photoAndLocationAllowed...)
	world.circleOf("Home", adaId, benId)
	photo := world.storePhoto(adaId, "harbor", "IMG_0001.HEIC", []byte("original"), nil)
	world.givePlace(photo.ID, "Harbor City, Exampleland")
	var shareId string
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		album, err := world.graph.CreatePhotoAlbum(ctx, CreatePhotoAlbumArguments{AlbumTitle: "Trip", PhotoIDs: []string{photo.ID}})
		if err != nil {
			test.Fatal(err)
		}
		share, err := world.graph.SharePhotoAlbum(ctx, SharePhotoAlbumArguments{PhotoAlbumID: album.ID, GranteeUserID: benId, ShareLevel: "view"})
		if err != nil {
			test.Fatal(err)
		}
		shareId = share.ID
	})
	world.ask(benId, photoAndLocationAllowed, func(ctx context.Context) {
		if view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID}); err != nil || view != nil {
			test.Fatalf("a hidden position: %+v %v", view, err)
		}
	})
	if requestCount.Load() != 0 {
		test.Fatalf("the provider was asked %d times for a position hidden from the reader", requestCount.Load())
	}
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		if _, err := world.graph.UpdatePhotoAlbumShare(ctx, UpdatePhotoAlbumShareArguments{PhotoAlbumShareID: shareId, ShareLevel: "view", IsLocationIncluded: true}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(benId, photoAndLocationAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view == nil || view.LocationPlace != nil || !strings.Contains(view.AddressText, "Fixture Road") {
			test.Fatalf("a shown position: %+v %v", view, err)
		}
	})
	world.ask(daveId, photoAndLocationAllowed, func(ctx context.Context) {
		if view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID}); !isNotFoundError(err) || view != nil {
			test.Fatalf("a stranger: %+v %v", view, err)
		}
	})
}

// A provider that fails gives no street and no error.
func TestPhotoPlaceSurvivesAFailingProvider(test *testing.T) {
	world := newPhotoWorld(test)
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Error(response, "down", http.StatusInternalServerError)
	}))
	test.Cleanup(provider.Close)
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	adaId := world.person("place-fail-ada", photoAndLocationAllowed...)
	photo := world.storePhoto(adaId, "harbor", "IMG_0001.HEIC", []byte("original"), nil)
	world.givePlace(photo.ID, "Harbor City, Exampleland")
	world.ask(adaId, photoAndLocationAllowed, func(ctx context.Context) {
		view, err := world.graph.ReadPhotoPlace(ctx, PhotoArguments{PhotoID: photo.ID})
		if err != nil || view == nil || view.AddressText != "" {
			test.Fatalf("%+v %v", view, err)
		}
	})
}

// The document the command line and the tool send is valid against the schema
// the server builds.
func TestPhotoPlaceClientDocumentMatchesTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	document, err := gqlparser.Parse(gqlparser.ParseParams{Source: client.DocumentReadPhotoPlace})
	if err != nil {
		test.Fatal(err)
	}
	if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
		test.Fatalf("ReadPhotoPlace does not match the built schema: %v", validated.Errors)
	}
}
