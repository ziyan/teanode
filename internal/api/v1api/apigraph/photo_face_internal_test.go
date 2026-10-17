package apigraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/graphql-go/graphql"
	gqlparser "github.com/graphql-go/graphql/language/parser"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// facePictureJpeg is a picture of 400 by 300 pixels with a face-sized red
// square in it, made here so that no photo of anybody is in the tests.
func facePictureJpeg(test *testing.T) []byte {
	test.Helper()
	made := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for row := range 300 {
		for column := range 400 {
			shade := color.RGBA{R: 110, G: 110, B: 110, A: 255}
			if column >= 150 && column < 250 && row >= 100 && row < 200 {
				shade = color.RGBA{R: 220, G: 40, B: 40, A: 255}
			}
			made.SetRGBA(column, row, shade)
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, made, nil); err != nil {
		test.Fatal(err)
	}
	return encoded.Bytes()
}

func faceVectorTowards(angleDegrees float64) []float32 {
	vector := make([]float32, 128)
	radians := angleDegrees * math.Pi / 180
	vector[0], vector[1] = float32(math.Cos(radians)), float32(math.Sin(radians))
	return vector
}

// storeFaceOf gives a stored photo one face in the red square, grouped as
// asked: in a new group when photoFaceGroupId is empty. It returns the face
// and its group.
func (self *photoWorld) storeFaceOf(userId, photoId, photoFaceGroupId string, angleDegrees float64) (*models.PhotoFace, string) {
	self.test.Helper()
	var stored []*models.PhotoFace
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		stored, err = transaction.StorePhotoFaces(userId, photoId, []*models.PhotoFace{{
			BoxLeftFraction: 0.375, BoxTopFraction: 1.0 / 3, BoxWidthFraction: 0.25, BoxHeightFraction: 1.0 / 3,
			DetectionScore: 0.97, FaceVector: faceVectorTowards(angleDegrees),
		}})
		if err != nil {
			self.test.Fatal(err)
		}
		if photoFaceGroupId == "" {
			photoFaceGroupId, err = transaction.StartPhotoFaceGroup(userId, stored[0].ID)
		} else {
			err = transaction.JoinPhotoFaceGroup(userId, stored[0].ID, photoFaceGroupId)
		}
		if err != nil {
			self.test.Fatal(err)
		}
	})
	return stored[0], photoFaceGroupId
}

func (self *photoWorld) keepContact(userId, name string) string {
	self.test.Helper()
	var contactId string
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		book, err := transaction.CreateAddressBook(&models.AddressBook{UserID: userId})
		if err != nil {
			self.test.Fatal(err)
		}
		kept, err := transaction.PutContact(&models.Contact{AddressBookID: book.ID, UID: "urn:uuid:" + name, ETag: "e1", Name: name, Card: "BEGIN:VCARD\nVERSION:3.0\nFN:" + name + "\nEND:VCARD\n"})
		if err != nil {
			self.test.Fatal(err)
		}
		contactId = kept.ID
	})
	return contactId
}

func TestPhotoFaceOperationsNameMergeHideAndRemoveGroups(test *testing.T) {
	world := newPhotoWorld(test)
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	permissions := []models.Permission{models.PermissionPhotosUse, models.PermissionContactsUse}
	ownerId := world.person("face-operations-owner", permissions...)
	large := map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: facePictureJpeg(test)}
	var photoIds []string
	var faceIds []string
	var firstGroup, secondGroup string
	for index := range 6 {
		stored := world.storePhoto(ownerId, fmt.Sprintf("face-%d", index), fmt.Sprintf("face-%d.jpg", index), []byte(fmt.Sprintf("original %d", index)), large)
		photoIds = append(photoIds, stored.ID)
		var face *models.PhotoFace
		switch {
		case index < 3:
			face, firstGroup = world.storeFaceOf(ownerId, stored.ID, firstGroup, 0)
		case index < 5:
			face, secondGroup = world.storeFaceOf(ownerId, stored.ID, secondGroup, 80)
		default:
			// A lone face in a group of its own: not worth listing.
			face, _ = world.storeFaceOf(ownerId, stored.ID, "", 160)
		}
		faceIds = append(faceIds, face.ID)
	}
	contactId := world.keepContact(ownerId, "Example Aunt")

	world.ask(ownerId, permissions, func(ctx context.Context) {
		listed, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{})
		if err != nil || len(listed) != 1 || listed[0].ID != firstGroup || listed[0].FaceCount != 3 || listed[0].PhotoCount != 3 || listed[0].ContactID != "" {
			test.Fatalf("%+v %v", listed, err)
		}
		if listed[0].FacePictureURL != api.PhotoFacePicturePath(listed[0].CoverPhotoFaceID) || listed[0].CoverPhotoFaceID == "" {
			test.Fatalf("the group is shown by the picture of a face: %+v", listed[0])
		}

		// Naming is choosing a contact; the name is the contact's.
		named, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup, ContactID: contactId})
		if err != nil || named.ContactID != contactId || named.ContactName != "Example Aunt" {
			test.Fatalf("%+v %v", named, err)
		}
		listed, err = world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{})
		if err != nil || len(listed) != 1 || listed[0].ContactName != "Example Aunt" {
			test.Fatalf("%+v %v", listed, err)
		}

		// Her photos, by the group and by the contact.
		byGroup, err := world.graph.ListPhotos(ctx, ListPhotosArguments{FaceGroupID: firstGroup})
		if err != nil || len(byGroup) != 3 {
			test.Fatalf("%v %v", byGroup, err)
		}
		byContact, err := world.graph.ListPhotos(ctx, ListPhotosArguments{ContactID: contactId})
		if err != nil || len(byContact) != 3 {
			test.Fatalf("%v %v", byContact, err)
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{ContactID: contactId}); err != nil || count != 3 {
			test.Fatalf("%d %v", count, err)
		}
		// A photo says who is in it.
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photoIds[0]})
		if err != nil || len(view.Faces) != 1 || view.Faces[0].ContactName != "Example Aunt" || view.Faces[0].ContactID != contactId ||
			view.Faces[0].PhotoFaceGroupID != firstGroup || view.Faces[0].FacePictureURL != api.PhotoFacePicturePath(faceIds[0]) {
			test.Fatalf("%+v %v", view, err)
		}
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photoIds[5]}); err != nil || len(view.Faces) != 1 || view.Faces[0].ContactName != "" {
			test.Fatalf("%+v %v", view, err)
		}

		// Merging puts the second into the first, which keeps its name.
		merged, err := world.graph.MergePhotoFaceGroups(ctx, MergePhotoFaceGroupsArguments{PhotoFaceGroupIDs: []string{firstGroup, secondGroup}})
		if err != nil || merged.ID != firstGroup || merged.FaceCount != 5 || merged.ContactName != "Example Aunt" {
			test.Fatalf("%+v %v", merged, err)
		}
		if _, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: secondGroup}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.MergePhotoFaceGroups(ctx, MergePhotoFaceGroupsArguments{PhotoFaceGroupIDs: []string{firstGroup}}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("%v", err)
		}

		// "This is not her": the face is a group of its own, confirmed.
		alone, err := world.graph.RemovePhotoFaceFromGroup(ctx, PhotoFaceArguments{PhotoFaceID: faceIds[3]})
		if err != nil || alone.ID == firstGroup || alone.FaceCount != 1 || alone.ContactID != "" {
			test.Fatalf("%+v %v", alone, err)
		}
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photoIds[3]}); err != nil || view.Faces[0].PhotoFaceGroupID != alone.ID || !view.Faces[0].IsGroupConfirmed || view.Faces[0].ContactName != "" {
			test.Fatalf("%+v %v", view, err)
		}
		if read, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup}); err != nil || read.FaceCount != 4 {
			test.Fatalf("%+v %v", read, err)
		}

		// Hidden groups are listed only when asked for.
		hidden, err := world.graph.HidePhotoFaceGroup(ctx, HidePhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup, IsHidden: true})
		if err != nil || !hidden.IsHidden {
			test.Fatalf("%+v %v", hidden, err)
		}
		if listed, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{}); err != nil || len(listed) != 0 {
			test.Fatalf("%+v %v", listed, err)
		}
		including := true
		if listed, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{IsHiddenIncluded: &including}); err != nil || len(listed) != 1 || !listed[0].IsHidden {
			test.Fatalf("%+v %v", listed, err)
		}
		if _, err := world.graph.HidePhotoFaceGroup(ctx, HidePhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup, IsHidden: false}); err != nil {
			test.Fatal(err)
		}

		unnamed, err := world.graph.UnnamePhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: firstGroup})
		if err != nil || unnamed.ContactID != "" || unnamed.ContactName != "" {
			test.Fatalf("%+v %v", unnamed, err)
		}
	})

	// Library: faces are on by default, and the reason they cannot be found
	// here is told.
	world.ask(ownerId, permissions, func(ctx context.Context) {
		library, err := world.graph.ReadPhotoLibrary(ctx)
		if err != nil || !library.IsPhotoFaceRecognitionEnabled || library.IsFaceRecognitionAvailable != (library.FaceRecognitionUnavailableMessage == "") {
			test.Fatalf("%+v %v", library, err)
		}
		isEnabled, err := world.graph.SetPhotoFaceRecognition(ctx, SetPhotoFaceRecognitionArguments{IsEnabled: false})
		if err != nil || isEnabled {
			test.Fatalf("%v %v", isEnabled, err)
		}
		if library, err := world.graph.ReadPhotoLibrary(ctx); err != nil || library.IsPhotoFaceRecognitionEnabled {
			test.Fatalf("%+v %v", library, err)
		}
		including := true
		if listed, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{IsHiddenIncluded: &including}); err != nil || len(listed) != 0 {
			test.Fatalf("switching faces off left groups: %+v %v", listed, err)
		}
		if view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: photoIds[0]}); err != nil || view.Faces == nil || len(view.Faces) != 0 {
			test.Fatalf("switching faces off left faces: %+v %v", view, err)
		}
		if isEnabled, err := world.graph.SetPhotoFaceRecognition(ctx, SetPhotoFaceRecognitionArguments{IsEnabled: true}); err != nil || !isEnabled {
			test.Fatalf("%v %v", isEnabled, err)
		}
	})

	// The operator's switch is said as the reason.
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Photos.IsFaceRecognitionEnabled = false
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	world.ask(ownerId, permissions, func(ctx context.Context) {
		library, err := world.graph.ReadPhotoLibrary(ctx)
		if err != nil || library.IsFaceRecognitionAvailable || !strings.Contains(library.FaceRecognitionUnavailableMessage, "switched off") {
			test.Fatalf("%+v %v", library, err)
		}
	})
}

func TestNamingAFaceGroupNeedsContactsAndTheCallersOwnContact(test *testing.T) {
	world := newPhotoWorld(test)
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	both := []models.Permission{models.PermissionPhotosUse, models.PermissionContactsUse}
	ownerId := world.person("naming-refused-owner", both...)
	otherId := world.person("naming-refused-other", both...)
	stored := world.storePhoto(ownerId, "named", "named.jpg", []byte("original"), nil)
	_, groupId := world.storeFaceOf(ownerId, stored.ID, "", 0)
	ownContactId := world.keepContact(ownerId, "Example Neighbor")
	foreignContactId := world.keepContact(otherId, "Foreign Neighbor")

	world.ask(ownerId, both, func(ctx context.Context) {
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: foreignContactId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("another person's contact was accepted: %v", err)
		}
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: "nobody"}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: " "}); !errors.Is(err, api.ErrInvalidArguments) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: "nowhere", ContactID: ownContactId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
	})
	// Another person's group is not found.
	world.ask(otherId, both, func(ctx context.Context) {
		if _, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: foreignContactId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: groupId}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
		if _, err := world.graph.HidePhotoFaceGroup(ctx, HidePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, IsHidden: true}); !errors.Is(err, api.ErrNotFound) {
			test.Fatalf("%v", err)
		}
	})

	// An account that may not use Contacts is told what is missing.
	world.ask(ownerId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		_, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: ownContactId})
		if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "Contacts") {
			test.Fatalf("%v", err)
		}
	})
	// And so is one on a server with Contacts switched off.
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Contacts.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	world.ask(ownerId, both, func(ctx context.Context) {
		_, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, ContactID: ownContactId})
		if !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), "turn on Contacts") {
			test.Fatalf("%v", err)
		}
		// Everything that does not name a person still works.
		if _, err := world.graph.HidePhotoFaceGroup(ctx, HidePhotoFaceGroupArguments{PhotoFaceGroupID: groupId, IsHidden: true}); err != nil {
			test.Fatal(err)
		}
	})
}

func TestPhotoFaceOperationsRefuseWithoutThePermissionOrTheService(test *testing.T) {
	world := newPhotoWorld(test)
	userId := world.person("face-refused", models.PermissionCalendarUse)
	calls := map[string]func(ctx context.Context) error{
		"list": func(ctx context.Context) error {
			_, err := world.graph.ListPhotoFaceGroups(ctx, ListPhotoFaceGroupsArguments{})
			return err
		},
		"read": func(ctx context.Context) error {
			_, err := world.graph.ReadPhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: "x"})
			return err
		},
		"name": func(ctx context.Context) error {
			_, err := world.graph.NamePhotoFaceGroup(ctx, NamePhotoFaceGroupArguments{PhotoFaceGroupID: "x", ContactID: "y"})
			return err
		},
		"unname": func(ctx context.Context) error {
			_, err := world.graph.UnnamePhotoFaceGroup(ctx, PhotoFaceGroupArguments{PhotoFaceGroupID: "x"})
			return err
		},
		"merge": func(ctx context.Context) error {
			_, err := world.graph.MergePhotoFaceGroups(ctx, MergePhotoFaceGroupsArguments{PhotoFaceGroupIDs: []string{"x", "y"}})
			return err
		},
		"remove": func(ctx context.Context) error {
			_, err := world.graph.RemovePhotoFaceFromGroup(ctx, PhotoFaceArguments{PhotoFaceID: "x"})
			return err
		},
		"hide": func(ctx context.Context) error {
			_, err := world.graph.HidePhotoFaceGroup(ctx, HidePhotoFaceGroupArguments{PhotoFaceGroupID: "x", IsHidden: true})
			return err
		},
		"switch": func(ctx context.Context) error {
			_, err := world.graph.SetPhotoFaceRecognition(ctx, SetPhotoFaceRecognitionArguments{IsEnabled: false})
			return err
		},
	}
	world.ask(userId, []models.Permission{models.PermissionCalendarUse}, func(ctx context.Context) {
		for name, call := range calls {
			if err := call(ctx); err == nil {
				test.Errorf("%s without photos:use: %v", name, err)
			}
		}
	})
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Photos.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	world.ask(userId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		for name, call := range calls {
			var unavailable *models.ServiceUnavailableError
			if err := call(ctx); !errorsAs(err, &unavailable) {
				test.Errorf("%s with photos switched off: %v", name, err)
			}
		}
	})
}

func TestAFacePictureIsServedToTheOwnerOnlyAndCached(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("face-picture-owner", models.PermissionPhotosUse)
	world.person("face-picture-other", models.PermissionPhotosUse)
	stored := world.storePhoto(ownerId, "face picture", "face.jpg", []byte("original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: facePictureJpeg(test)})
	face, _ := world.storeFaceOf(ownerId, stored.ID, "", 0)
	path := api.PhotoFacePicturePath(face.ID)

	response := world.do(http.MethodGet, path, asSession("face-picture-owner"), nil)
	body := readBody(test, response)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/jpeg" || response.Header.Get("ETag") == "" ||
		!strings.Contains(response.Header.Get("Cache-Control"), "private") || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		test.Fatalf("%d %v", response.StatusCode, response.Header)
	}
	decoded, err := jpeg.Decode(strings.NewReader(body))
	if err != nil {
		test.Fatal(err)
	}
	// The face is 100 pixels square, so the picture is 160 pixels square
	// with the face in the middle.
	if bounds := decoded.Bounds(); bounds.Dx() != 160 || bounds.Dy() != 160 {
		test.Fatalf("%v", bounds)
	}
	if red, green, _, _ := decoded.At(80, 80).RGBA(); red>>8 < 180 || green>>8 > 90 {
		test.Fatalf("the middle is not the face: %d %d", red>>8, green>>8)
	}
	if red, green, _, _ := decoded.At(5, 5).RGBA(); red>>8 > 140 || green>>8 < 80 {
		test.Fatalf("the corner is not margin: %d %d", red>>8, green>>8)
	}
	if status := world.status(http.MethodGet, path, func(request *http.Request) {
		asSession("face-picture-owner")(request)
		request.Header.Set("If-None-Match", response.Header.Get("ETag"))
	}, nil); status != http.StatusNotModified {
		test.Fatalf("a picture the browser has got %d", status)
	}
	if status := world.status(http.MethodHead, path, asSession("face-picture-owner"), nil); status != http.StatusOK {
		test.Fatalf("HEAD %d", status)
	}
	if status := world.status(http.MethodGet, path, asSession("face-picture-other"), nil); status != http.StatusNotFound {
		test.Fatalf("somebody else's face got %d", status)
	}
	if status := world.status(http.MethodGet, path, nil, nil); status != http.StatusUnauthorized {
		test.Fatalf("nobody got %d", status)
	}
	if status := world.status(http.MethodGet, api.PhotoFacePicturePath("nowhere"), asSession("face-picture-owner"), nil); status != http.StatusNotFound {
		test.Fatalf("a face that is not there got %d", status)
	}
}

// Every document the command line and the tool send for people is valid
// against the schema the server builds, and so are the photo documents that
// gained the face filters and the people of a photo.
func TestPhotoFaceClientDocumentsMatchTheBuiltSchema(test *testing.T) {
	schema := buildSchemaForValidation(test)
	for name, source := range map[string]string{
		"ListPhotoFaceGroups":      client.DocumentListPhotoFaceGroups,
		"ReadPhotoFaceGroup":       client.DocumentReadPhotoFaceGroup,
		"NamePhotoFaceGroup":       client.DocumentNamePhotoFaceGroup,
		"UnnamePhotoFaceGroup":     client.DocumentUnnamePhotoFaceGroup,
		"MergePhotoFaceGroups":     client.DocumentMergePhotoFaceGroups,
		"RemovePhotoFaceFromGroup": client.DocumentRemovePhotoFaceFromGroup,
		"HidePhotoFaceGroup":       client.DocumentHidePhotoFaceGroup,
		"SetPhotoFaceRecognition":  client.DocumentSetPhotoFaceRecognition,
		"ListPhotos":               client.DocumentListPhotos,
		"CountPhotos":              client.DocumentCountPhotos,
		"ReadPhoto":                client.DocumentReadPhoto,
		"ReadPhotoLibrary":         client.DocumentReadPhotoLibrary,
	} {
		document, err := gqlparser.Parse(gqlparser.ParseParams{Source: source})
		if err != nil {
			test.Fatalf("%s does not parse: %s", name, err)
		}
		if validated := graphql.ValidateDocument(&schema, document, nil); !validated.IsValid {
			test.Errorf("%s does not match the built schema: %v", name, validated.Errors)
		}
	}
}

// Cutting a face picture decodes a whole preview, so only a few are cut at
// once: while the turns are taken a request waits, and one whose browser goes
// away is given up on without decoding anything.
func TestFacePicturesAreCutAFewAtATime(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("face-slots-owner", models.PermissionPhotosUse)
	stored := world.storePhoto(ownerId, "face slots", "face.jpg", []byte("original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewLarge: facePictureJpeg(test)})
	face, _ := world.storeFaceOf(ownerId, stored.ID, "", 0)
	path := api.PhotoFacePicturePath(face.ID)

	if err := facePictureSlots.Acquire(test.Context(), facePictureConcurrency); err != nil {
		test.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			facePictureSlots.Release(facePictureConcurrency)
		}
	}
	defer release()

	// A browser that gives up while the turns are taken is told so.
	ctx, cancel := context.WithTimeout(test.Context(), 300*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, world.server.URL+path, nil)
	if err != nil {
		test.Fatal(err)
	}
	asSession("face-slots-owner")(request)
	if response, err := http.DefaultClient.Do(request); err == nil {
		_ = response.Body.Close()
		test.Fatalf("a face was cut with every turn taken: %d", response.StatusCode)
	}

	// A request that waits is served once a turn is free.
	answered := make(chan int, 1)
	go func() { answered <- world.status(http.MethodGet, path, asSession("face-slots-owner"), nil) }()
	select {
	case status := <-answered:
		test.Fatalf("a face was cut with every turn taken: %d", status)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	select {
	case status := <-answered:
		if status != http.StatusOK {
			test.Fatalf("the waiting request got %d", status)
		}
	case <-time.After(10 * time.Second):
		test.Fatal("the waiting request was never served")
	}
}
