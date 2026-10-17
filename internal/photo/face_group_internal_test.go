package photo

import (
	"context"
	"fmt"
	"image"
	"math"
	"math/rand"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
)

// unitVectorAt is a unit vector of 128 numbers at an angle, in the plane of
// the first two, from the vector at 0 degrees.
func unitVectorAt(angleDegrees float64) []float32 {
	vector := make([]float32, faces.VectorLength)
	radians := angleDegrees * math.Pi / 180
	vector[0], vector[1] = float32(math.Cos(radians)), float32(math.Sin(radians))
	return vector
}

func TestAFaceIsGroupedOnlyWhenSureAndLargeEnough(test *testing.T) {
	test.Parallel()
	// A picture of 4000 by 3000: 3 percent of its shorter side is 90 pixels,
	// which is 0.0225 of the width and 0.03 of the height.
	scenarios := []struct {
		name           string
		score          float64
		widthFraction  float64
		heightFraction float64
		isGroupable    bool
		widthPixels    int
		heightPixels   int
	}{
		{"sure and large", 0.95, 0.1, 0.13, true, 4000, 3000},
		{"just sure enough", 0.85, 0.1, 0.13, true, 4000, 3000},
		{"not sure enough", 0.849, 0.1, 0.13, false, 4000, 3000},
		{"just wide enough", 0.95, 0.0226, 0.13, true, 4000, 3000},
		{"too narrow", 0.95, 0.0224, 0.13, false, 4000, 3000},
		{"too short", 0.95, 0.1, 0.0299, false, 4000, 3000},
		{"measured against the shorter side of a tall picture", 0.95, 0.07, 0.04, true, 3000, 4000},
		{"no picture to measure against", 0.95, 0.1, 0.1, false, 0, 0},
	}
	for _, scenario := range scenarios {
		face := &models.PhotoFace{DetectionScore: scenario.score, BoxWidthFraction: scenario.widthFraction, BoxHeightFraction: scenario.heightFraction}
		if got := isFaceGroupable(face, scenario.widthPixels, scenario.heightPixels); got != scenario.isGroupable {
			test.Errorf("%s: %v", scenario.name, got)
		}
	}
}

func TestAFaceJoinsTheNearestGroupOnlyFromAFixedSimilarity(test *testing.T) {
	test.Parallel()
	groups := []*db.PhotoFaceGroupVector{
		{ID: "at zero", VectorSum: unitVectorAt(0)},
		{ID: "at ninety", VectorSum: unitVectorAt(90)},
	}
	for _, scenario := range []struct {
		name        string
		angle       float64
		excluded    map[string]bool
		wantGroupId string
	}{
		{"on a group", 3, nil, "at zero"},
		{"nearer the second", 80, nil, "at ninety"},
		{"the nearest is excluded and the other is too far", 3, map[string]bool{"at zero": true}, ""},
		{"the nearest is excluded and the other is near enough", 50, map[string]bool{"at ninety": true}, "at zero"},
	} {
		if groupId, _ := nearestFaceGroup(unitVectorAt(scenario.angle), groups, scenario.excluded); groupId != scenario.wantGroupId {
			test.Errorf("%s: %q", scenario.name, groupId)
		}
	}

	// One group: the cosine of 66 degrees is 0.407 and of 67 is 0.391, either
	// side of the 0.40 a face needs.
	alone := []*db.PhotoFaceGroupVector{{ID: "at zero", VectorSum: unitVectorAt(0)}}
	if groupId, similarity := nearestFaceGroup(unitVectorAt(66), alone, nil); groupId != "at zero" || similarity < 0.40 {
		test.Errorf("%q at %v", groupId, similarity)
	}
	if groupId, _ := nearestFaceGroup(unitVectorAt(67), alone, nil); groupId != "" {
		test.Errorf("%q", groupId)
	}
	// A group's vector is a sum, and only its direction counts.
	large := []*db.PhotoFaceGroupVector{{ID: "sum", VectorSum: []float32{300, 0}}}
	if groupId, _ := nearestFaceGroup([]float32{1, 0}, large, nil); groupId != "sum" {
		test.Errorf("%q", groupId)
	}
	// A face of another length, or no group at all, joins nothing.
	if groupId, _ := nearestFaceGroup(unitVectorAt(0), large, nil); groupId != "" {
		test.Errorf("%q", groupId)
	}
	if groupId, _ := nearestFaceGroup(unitVectorAt(0), nil, nil); groupId != "" {
		test.Errorf("%q", groupId)
	}
}

// noisyVector is a unit vector near center: the center with a little noise in
// every one of its 128 numbers.
func noisyVector(random *rand.Rand, center []float32, noise float64) []float32 {
	vector := make([]float32, len(center))
	for index := range center {
		vector[index] = center[index] + float32((random.Float64()-0.5)*noise)
	}
	return faces.Normalize(vector)
}

func TestTwoClustersOfFacesMakeTwoGroupsWhateverTheNoise(test *testing.T) {
	test.Parallel()
	random := rand.New(rand.NewSource(7))
	centers := [][]float32{faces.Normalize(randomVector(random)), faces.Normalize(randomVector(random))}
	// Random directions in 128 numbers are nearly at right angles.
	if similarity := faces.CosineSimilarity(centers[0], centers[1]); math.Abs(similarity) > 0.3 {
		test.Fatalf("the centers are alike: %v", similarity)
	}
	var groups []*db.PhotoFaceGroupVector
	groupOf := func(faceVector []float32) string {
		groupId, _ := nearestFaceGroup(faceVector, groups, nil)
		if groupId == "" {
			groupId = fmt.Sprintf("group %d", len(groups))
			groups = append(groups, &db.PhotoFaceGroupVector{ID: groupId, VectorSum: append([]float32(nil), faceVector...)})
			return groupId
		}
		for _, group := range groups {
			if group.ID == groupId {
				for index, number := range faceVector {
					group.VectorSum[index] += number
				}
			}
		}
		return groupId
	}
	assigned := [2]map[string]int{{}, {}}
	for round := 0; round < 60; round++ {
		for cluster := range centers {
			assigned[cluster][groupOf(noisyVector(random, centers[cluster], 0.3))]++
		}
	}
	if len(groups) != 2 || len(assigned[0]) != 1 || len(assigned[1]) != 1 {
		test.Fatalf("%d groups: %v", len(groups), assigned)
	}
}

func randomVector(random *rand.Rand) []float32 {
	vector := make([]float32, faces.VectorLength)
	for index := range vector {
		vector[index] = float32(random.NormFloat64())
	}
	return vector
}

// fixedFaceReader finds the one face a test gave it, in any picture.
type fixedFaceReader struct{ found []*faces.FoundFace }

func (self *fixedFaceReader) ReadFaces(ctx context.Context, picture image.Image) ([]*faces.FoundFace, error) {
	copied := make([]*faces.FoundFace, 0, len(self.found))
	for _, face := range self.found {
		next := *face
		next.FaceVector = append([]float32(nil), face.FaceVector...)
		copied = append(copied, &next)
	}
	return copied, nil
}

// photoOf makes a photo of the person's that is there to have faces found in.
func photoOf(test *testing.T, database db.Database, userId, clientPhotoId string) *models.Photo {
	test.Helper()
	var made *models.Photo
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		made, _, err = transaction.CreatePhoto(&models.Photo{UserID: userId, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine})
		if err != nil {
			test.Fatal(err)
		}
	})
	return made
}

// TestPhotosGroupedAtTheSameTimeMakeOneGroupForOnePerson is what the person's
// lock is for: two instances, or two photos of one pass, reading the same
// new person at the same time must not each start a group for them.
func TestPhotosGroupedAtTheSameTimeMakeOneGroupForOnePerson(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	userId := dbtest.CreateUser(test, database, "grouping-person")
	store := New(database, nil, config.Default)

	const peopleCount = 12
	picture := image.NewRGBA(image.Rect(0, 0, 40, 30))
	var photos []*models.Photo
	var readers []*fixedFaceReader
	for person := 0; person < peopleCount; person++ {
		// Each person is a direction of their own, and each is seen in two
		// photos, which are grouped at the same time.
		center := make([]float32, faces.VectorLength)
		center[person] = 1
		for appearance := 0; appearance < 2; appearance++ {
			photos = append(photos, photoOf(test, database, userId, fmt.Sprintf("person %d appearance %d", person, appearance)))
			readers = append(readers, &fixedFaceReader{found: []*faces.FoundFace{{BoxLeft: 0.2, BoxTop: 0.2, BoxWidth: 0.4, BoxHeight: 0.4, DetectionScore: 0.99, FaceVector: center}}})
		}
	}
	var waitGroup sync.WaitGroup
	start := make(chan struct{})
	for index := range photos {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			if err := store.readAndKeepFaces(test.Context(), readers[index], photos[index], picture); err != nil {
				test.Error(err)
			}
		}()
	}
	close(start)
	waitGroup.Wait()

	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		groups, err := transaction.ListPhotoFaceGroups(userId, true)
		if err != nil || len(groups) != 0 {
			// Two faces are not enough to be listed.
			test.Fatalf("%d %v", len(groups), err)
		}
	})
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group"); count != fmt.Sprint(peopleCount) {
		test.Fatalf("%s groups for %d people", count, peopleCount)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face_group WHERE face_group_vector_count = 2"); count != fmt.Sprint(peopleCount) {
		test.Fatalf("%s groups of two faces", count)
	}
}

func TestNothingIsKeptOnceTheyHaveSwitchedFacesOff(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	userId := dbtest.CreateUser(test, database, "switched-off")
	store := New(database, nil, config.Default)
	made := photoOf(test, database, userId, "read while switching")
	// The reader takes a while; the person switches faces off meanwhile.
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.SetPhotoFaceRecognition(userId, false); err != nil {
			test.Fatal(err)
		}
	})
	reader := &fixedFaceReader{found: []*faces.FoundFace{{BoxLeft: 0.2, BoxTop: 0.2, BoxWidth: 0.4, BoxHeight: 0.4, DetectionScore: 0.99, FaceVector: unitVectorAt(0)}}}
	if err := store.readAndKeepFaces(test.Context(), reader, made, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		test.Fatal(err)
	}
	if count := dbtest.QueryString(test, database, "SELECT count(*) FROM photo_face"); count != "0" {
		test.Fatalf("%s faces", count)
	}
	if foundAt := dbtest.QueryString(test, database, "SELECT COALESCE(faces_found_at::text, 'never') FROM photo"); foundAt != "never" {
		test.Fatalf("%s", foundAt)
	}
}
