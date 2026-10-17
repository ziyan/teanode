package photo_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// vectorAt is a unit vector of 128 numbers pointing at an angle in the plane
// of the first two, so that the similarity of two faces is the cosine of the
// angle between them.
func vectorAt(angleDegrees float64) []float32 {
	vector := make([]float32, faces.VectorLength)
	radians := angleDegrees * math.Pi / 180
	vector[0], vector[1] = float32(math.Cos(radians)), float32(math.Sin(radians))
	return vector
}

func scriptedFace(angleDegrees, boxLeft, detectionScore, boxSize float64) *faces.FoundFace {
	return &faces.FoundFace{BoxLeft: boxLeft, BoxTop: 0.2, BoxWidth: boxSize, BoxHeight: boxSize, DetectionScore: detectionScore, FaceVector: vectorAt(angleDegrees)}
}

// fakeFaceReader finds the faces a test scripted for a picture, chosen by how
// red it is, so that no real face is needed and a test says which photo has
// which people in it.
type fakeFaceReader struct {
	mutex   sync.Mutex
	scripts map[int][]*faces.FoundFace
	err     error
	calls   int
}

// redBucket is the script of a picture: its red divided into steps of 60.
func redBucket(picture image.Image) int {
	bounds := picture.Bounds()
	red, _, _, _ := picture.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	return int(red>>8) / 60
}

func (self *fakeFaceReader) ReadFaces(ctx context.Context, picture image.Image) ([]*faces.FoundFace, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.calls++
	if self.err != nil {
		return nil, self.err
	}
	found := make([]*faces.FoundFace, 0)
	for _, face := range self.scripts[redBucket(picture)] {
		copied := *face
		copied.FaceVector = append([]float32(nil), face.FaceVector...)
		found = append(found, &copied)
	}
	return found, nil
}

func (self *fakeFaceReader) callCount() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.calls
}

// reddish is the shade smallJpeg gives for a script: 20, 90, 160 and 230 are
// buckets 0 to 3.
var reddish = map[int]uint8{0: 20, 1: 90, 2: 160, 3: 230}

func (self *fixture) facesOf(photoId string) []*models.PhotoFace {
	self.test.Helper()
	var found []*models.PhotoFace
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		if found, err = transaction.ListPhotoFaces(self.userId, []string{photoId}); err != nil {
			self.test.Fatal(err)
		}
	})
	return found
}

func (self *fixture) facesFoundAt(clientPhotoId string) string {
	self.test.Helper()
	return dbtest.QueryString(self.test, self.database, "SELECT COALESCE(faces_found_at::text, 'never') FROM photo WHERE client_photo_id = '"+clientPhotoId+"'")
}

func (self *fixture) sendPicture(clientPhotoId string, bucket int) {
	self.test.Helper()
	self.expect(self.send(http.MethodPost, self.collectionUrl(), jpegHeaders(clientPhotoId), smallJpeg(self.test, reddish[bucket])), http.StatusCreated)
}

func (self *fixture) process() {
	self.test.Helper()
	if err := self.store.Process(self.test.Context(), 4); err != nil {
		self.test.Fatal(err)
	}
}

func TestFacesAreFoundWhilePhotosAreProcessedAndGroupedByPerson(test *testing.T) {
	fixture := newFixture(test)
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{
		// Ada (0 degrees) and Bo (90) in the first, Ada alone in the second.
		0: {scriptedFace(0, 0.1, 0.95, 0.2), scriptedFace(90, 0.6, 0.95, 0.2)},
		1: {scriptedFace(15, 0.3, 0.95, 0.2)},
		// Bo again; a blur the detector was unsure of; and a face too small
		// to tell from any other.
		2: {scriptedFace(100, 0.1, 0.96, 0.2), scriptedFace(45, 0.4, 0.5, 0.2), scriptedFace(200, 0.7, 0.99, 0.01)},
		// Somebody new, and Ada once more.
		3: {scriptedFace(180, 0.1, 0.97, 0.2), scriptedFace(5, 0.6, 0.97, 0.2)},
	}}
	fixture.store.SetFaceReader(reader, nil)
	for index, bucket := range []int{0, 1, 2, 3} {
		fixture.sendPicture([]string{"first", "second", "third", "fourth"}[index], bucket)
	}
	fixture.process()
	if reader.callCount() != 4 {
		test.Fatalf("%d photos were read", reader.callCount())
	}

	groupOf := map[string]map[int]string{}
	for index, clientPhotoId := range []string{"first", "second", "third", "fourth"} {
		found := fixture.photoByClientId(clientPhotoId)
		if found.PhotoProcessingState != models.PhotoProcessingReady || fixture.facesFoundAt(clientPhotoId) == "never" {
			test.Fatalf("%s: %+v", clientPhotoId, found)
		}
		groupOf[clientPhotoId] = map[int]string{}
		for position, face := range fixture.facesOf(found.ID) {
			groupOf[clientPhotoId][position] = face.PhotoFaceGroupID
			if len(face.FaceVector) != 0 || face.IsGroupConfirmed {
				test.Fatalf("%d: %+v", index, face)
			}
		}
	}
	ada, bo, somebodyNew := groupOf["first"][0], groupOf["first"][1], groupOf["fourth"][0]
	if ada == "" || bo == "" || somebodyNew == "" || ada == bo || ada == somebodyNew || bo == somebodyNew {
		test.Fatalf("%v", groupOf)
	}
	if groupOf["second"][0] != ada || groupOf["fourth"][1] != ada {
		test.Fatalf("Ada is in three photos and one group: %v", groupOf)
	}
	if groupOf["third"][0] != bo {
		test.Fatalf("Bo is in two photos and one group: %v", groupOf)
	}
	// The unsure face and the small one are kept and in no group.
	if groupOf["third"][1] != "" || groupOf["third"][2] != "" {
		test.Fatalf("%v", groupOf)
	}
	if count := fixture.count("photo_face"); count != "8" {
		test.Fatalf("%s faces", count)
	}
	if count := fixture.count("photo_face_group"); count != "3" {
		test.Fatalf("%s groups", count)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		listed, err := transaction.ListPhotoFaceGroups(fixture.userId, false)
		if err != nil || len(listed) != 1 || listed[0].ID != ada || listed[0].FaceCount != 3 || listed[0].PhotoCount != 3 {
			test.Fatalf("only the group of three is worth listing: %+v %v", listed, err)
		}
	})

	// A photo is read once: another pass reads nothing.
	fixture.process()
	fixture.process()
	if reader.callCount() != 4 {
		test.Fatalf("%d reads", reader.callCount())
	}
}

func TestTwoFacesOfOnePhotoAreNeverPutInOneGroup(test *testing.T) {
	fixture := newFixture(test)
	// Two faces that look alike: the second would join the first's group
	// were they not in one photo.
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{
		0: {scriptedFace(0, 0.1, 0.95, 0.2), scriptedFace(4, 0.6, 0.95, 0.2)},
	}}
	fixture.store.SetFaceReader(reader, nil)
	fixture.sendPicture("twins", 0)
	fixture.process()
	found := fixture.facesOf(fixture.photoByClientId("twins").ID)
	if len(found) != 2 || found[0].PhotoFaceGroupID == "" || found[1].PhotoFaceGroupID == "" || found[0].PhotoFaceGroupID == found[1].PhotoFaceGroupID {
		test.Fatalf("%+v", found)
	}
}

func TestAFaceReaderThatFailsLeavesThePhotoReadyAndIsTriedAgainLater(test *testing.T) {
	fixture := newFixture(test)
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{0: {scriptedFace(0, 0.1, 0.95, 0.2)}}, err: errors.New("faces: the program was killed")}
	fixture.store.SetFaceReader(reader, nil)
	fixture.sendPicture("broken", 0)
	fixture.process()
	found := fixture.photoByClientId("broken")
	if found.PhotoProcessingState != models.PhotoProcessingReady || found.ProcessingAttemptCount != 0 || found.ProcessingErrorMessage != "" {
		test.Fatalf("a face reader that failed failed the photo: %+v", found)
	}
	if fixture.facesFoundAt("broken") != "never" || len(fixture.facesOf(found.ID)) != 0 {
		test.Fatal("faces were recorded as found")
	}
	if reader.callCount() != 1 {
		test.Fatalf("%d reads", reader.callCount())
	}

	// The next passes leave the photo alone for a while.
	reader.mutex.Lock()
	reader.err = nil
	reader.mutex.Unlock()
	fixture.process()
	fixture.process()
	if reader.callCount() != 1 || fixture.facesFoundAt("broken") != "never" {
		test.Fatalf("a photo that just failed was read again at once: %d reads", reader.callCount())
	}
	fixture.store.SetNow(func() time.Time { return time.Now().Add(2 * time.Hour) })
	fixture.process()
	if reader.callCount() != 2 || fixture.facesFoundAt("broken") == "never" || len(fixture.facesOf(found.ID)) != 1 {
		test.Fatalf("%d reads", reader.callCount())
	}
}

func TestFacesWaitUntilTheyAreWantedAndTheProgramIsThere(test *testing.T) {
	fixture := newFixture(test)
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{0: {scriptedFace(0, 0.1, 0.95, 0.2)}, 1: {scriptedFace(90, 0.1, 0.95, 0.2)}, 2: {scriptedFace(180, 0.1, 0.95, 0.2)}}}

	// The program is not installed: the reason is the operator's to read.
	fixture.store.SetFaceReader(nil, errors.New("faces are unavailable: teanode-faces was not found on the path"))
	fixture.sendPicture("before the program", 0)
	fixture.process()
	if found := fixture.photoByClientId("before the program"); found.PhotoProcessingState != models.PhotoProcessingReady || fixture.facesFoundAt("before the program") != "never" {
		test.Fatalf("%+v", found)
	}
	if message := photo.FaceRecognitionUnavailableMessage(fixture.configuration); message == "" {
		test.Fatal("faces are said to be available with no program")
	}

	// The operator has them off, the program is there.
	fixture.store.SetFaceReader(reader, nil)
	fixture.configuration.Photos.IsFaceRecognitionEnabled = false
	if message := photo.FaceRecognitionUnavailableMessage(fixture.configuration); message != "Finding faces in photos is switched off on this server." {
		test.Fatalf("%q", message)
	}
	fixture.sendPicture("operator off", 1)
	fixture.process()
	fixture.process()

	// The person has them off.
	fixture.configuration.Photos.IsFaceRecognitionEnabled = true
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoFaceRecognition(fixture.userId, false); err != nil {
			test.Fatal(err)
		}
	})
	fixture.sendPicture("person off", 2)
	fixture.process()
	fixture.process()
	if reader.callCount() != 0 {
		test.Fatalf("%d photos were read though faces were not wanted", reader.callCount())
	}

	// Turned on, the photos that waited are read, a few each pass.
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoFaceRecognition(fixture.userId, true); err != nil {
			test.Fatal(err)
		}
	})
	fixture.process()
	for _, clientPhotoId := range []string{"before the program", "operator off", "person off"} {
		found := fixture.photoByClientId(clientPhotoId)
		if fixture.facesFoundAt(clientPhotoId) == "never" || len(fixture.facesOf(found.ID)) != 1 || found.PhotoProcessingState != models.PhotoProcessingReady {
			test.Fatalf("%s was not caught up: %+v", clientPhotoId, found)
		}
	}
	if reader.callCount() != 3 {
		test.Fatalf("%d reads", reader.callCount())
	}
}

func TestAPhotoWhoseFileChangesHasItsFacesFoundAgain(test *testing.T) {
	fixture := newFixture(test)
	reader := &fakeFaceReader{scripts: map[int][]*faces.FoundFace{0: {scriptedFace(0, 0.1, 0.95, 0.2)}, 1: {scriptedFace(0, 0.5, 0.95, 0.2), scriptedFace(90, 0.1, 0.95, 0.2)}}}
	fixture.store.SetFaceReader(reader, nil)
	fixture.sendPicture("edited", 0)
	fixture.process()
	found := fixture.photoByClientId("edited")
	if len(fixture.facesOf(found.ID)) != 1 {
		test.Fatal("the original has one face")
	}
	// The person confirms the face where it is, and then an edited version
	// arrives with the same person in about the same place and somebody else.
	faceId := fixture.facesOf(found.ID)[0].ID
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		if _, err := transaction.RemovePhotoFaceFromGroup(fixture.userId, faceId); err != nil {
			test.Fatal(err)
		}
	})
	confirmedGroupId := fixture.facesOf(found.ID)[0].PhotoFaceGroupID
	editedFile := smallJpeg(test, reddish[1])
	fixture.expect(fixture.send(http.MethodPost, fixture.collectionUrl(), jpegHeaders("edited", "X-Photo-File-Role", "edited"), editedFile), http.StatusCreated)
	if fixture.facesFoundAt("edited") != "never" {
		test.Fatal("an edited file did not clear the record that faces were found")
	}
	reader.scripts[1] = []*faces.FoundFace{scriptedFace(2, 0.11, 0.95, 0.2), scriptedFace(90, 0.6, 0.95, 0.2)}
	fixture.process()
	again := fixture.facesOf(found.ID)
	if len(again) != 2 {
		test.Fatalf("%+v", again)
	}
	sort.Slice(again, func(first, second int) bool { return again[first].BoxLeftFraction < again[second].BoxLeftFraction })
	if again[0].PhotoFaceGroupID != confirmedGroupId || !again[0].IsGroupConfirmed || again[0].ID == faceId {
		test.Fatalf("the face the person confirmed was not kept: %+v", again[0])
	}
	if again[1].PhotoFaceGroupID == "" || again[1].PhotoFaceGroupID == confirmedGroupId {
		test.Fatalf("%+v", again[1])
	}
}

func TestAFacePictureIsASquareAroundTheFaceWithMargin(test *testing.T) {
	test.Parallel()
	// A grey picture with a red face and a blue patch beside it that is
	// outside the margin.
	preview := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	for row := range 800 {
		for column := range 1000 {
			shade := color.RGBA{R: 120, G: 120, B: 120, A: 255}
			switch {
			case column >= 400 && column < 600 && row >= 300 && row < 550:
				shade = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			case column >= 700:
				shade = color.RGBA{R: 30, G: 30, B: 220, A: 255}
			}
			preview.SetRGBA(column, row, shade)
		}
	}
	face := &models.PhotoFace{BoxLeftFraction: 0.4, BoxTopFraction: 0.375, BoxWidthFraction: 0.2, BoxHeightFraction: 0.3125}
	encoded, err := photo.FacePicture(preview, face)
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		test.Fatal(err)
	}
	// The box is 250 pixels tall, so the square is 250 * 1.6 = 400 pixels,
	// shrunk to 256.
	if bounds := decoded.Bounds(); bounds.Dx() != photo.FacePictureSidePixels || bounds.Dy() != photo.FacePictureSidePixels {
		test.Fatalf("%v", bounds)
	}
	redAt := func(column, row int) uint32 { red, _, _, _ := decoded.At(column, row).RGBA(); return red >> 8 }
	if redAt(128, 128) < 190 {
		test.Fatalf("the center is not the face: %d", redAt(128, 128))
	}
	if redAt(5, 5) < 100 || redAt(5, 5) > 140 {
		test.Fatalf("the corner is margin and grey: %d", redAt(5, 5))
	}

	// A face at the edge gets a square moved inwards: the picture's corner,
	// not a cut-short strip.
	edge := &models.PhotoFace{BoxLeftFraction: 0, BoxTopFraction: 0, BoxWidthFraction: 0.1, BoxHeightFraction: 0.125}
	encoded, err = photo.FacePicture(preview, edge)
	if err != nil {
		test.Fatal(err)
	}
	if decoded, err = jpeg.Decode(bytes.NewReader(encoded)); err != nil || decoded.Bounds().Dx() != decoded.Bounds().Dy() {
		test.Fatalf("%v %v", decoded.Bounds(), err)
	}

	// A small preview is not enlarged, and a face larger than the picture is
	// the picture's shorter side.
	small := image.NewRGBA(image.Rect(0, 0, 100, 60))
	encoded, err = photo.FacePicture(small, &models.PhotoFace{BoxLeftFraction: 0, BoxTopFraction: 0, BoxWidthFraction: 1, BoxHeightFraction: 1})
	if err != nil {
		test.Fatal(err)
	}
	if decoded, err = jpeg.Decode(bytes.NewReader(encoded)); err != nil || decoded.Bounds().Dx() != 60 || decoded.Bounds().Dy() != 60 {
		test.Fatalf("%v %v", decoded.Bounds(), err)
	}
}
