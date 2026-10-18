package photo

import (
	"math"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// minimumGroupedDetectionScore is how sure the detector must be that a
	// box holds a face for the face to be grouped. Below it the face is kept,
	// so that the viewer can still show where it was, but it joins no group:
	// a blurred edge of a crowd is a poor basis for deciding who somebody is.
	minimumGroupedDetectionScore = 0.85

	// minimumGroupedBoxFraction is how wide the narrower side of a face's box
	// must be, as a fraction of the picture's shorter side. A face smaller
	// than that has too few pixels for its vector to tell people apart.
	minimumGroupedBoxFraction = 0.03

	// minimumGroupSimilarity is the cosine similarity a face needs with a
	// group to join it. The recognizer's published threshold for the same
	// person is 0.363; this is a little above it, to prefer two groups of
	// one person, which a merge fixes in one step, to one group of two people,
	// which has to be taken apart face by face.
	minimumGroupSimilarity = 0.40
)

// isFaceGroupable says whether a face is sure enough and large enough to be
// grouped, given the size of the picture its box is a fraction of.
func isFaceGroupable(face *models.PhotoFace, widthPixels, heightPixels int) bool {
	if face.DetectionScore < minimumGroupedDetectionScore || widthPixels < 1 || heightPixels < 1 {
		return false
	}
	boxWidthPixels := face.BoxWidthFraction * float64(widthPixels)
	boxHeightPixels := face.BoxHeightFraction * float64(heightPixels)
	return math.Min(boxWidthPixels, boxHeightPixels) >= minimumGroupedBoxFraction*float64(min(widthPixels, heightPixels))
}

// nearestFaceGroup is the group whose vector is most like the face's, among
// the groups not excluded, and how alike. It names none when no group is alike
// enough to take the face.
func nearestFaceGroup(vector []float32, groups []*db.PhotoFaceGroupVector, excludedGroupIds map[string]bool) (string, float64) {
	nearestId, nearestSimilarity := "", minimumGroupSimilarity
	for _, group := range groups {
		if excludedGroupIds[group.ID] {
			continue
		}
		// The group's vector is a sum; a similarity looks at direction alone.
		if similarity := faces.CosineSimilarity(vector, group.VectorSum); similarity >= nearestSimilarity {
			nearestId, nearestSimilarity = group.ID, similarity
		}
	}
	return nearestId, nearestSimilarity
}

// groupFaces puts each stored face that can be grouped in the group it is
// most like, or in a group of its own, and says how many faces it grouped.
// The caller holds the person's grouping lock and has stored the faces in the
// same transaction, so what is read of the groups is not changing under it.
//
// Two faces of one photo are never put in one group: a photo does not hold
// the same person twice, and two people who look alike would otherwise be
// pulled together by a group that already took the first. So the faces are
// placed best match first, not in the order the detector found them: a face
// that only somewhat resembles a person cannot take that person's group from
// the face in the same photo that is them.
func groupFaces(transaction db.Transaction, userId string, stored []*models.PhotoFace, widthPixels, heightPixels int) (int, error) {
	groups, err := transaction.ListPhotoFaceGroupVectors(userId)
	if err != nil {
		return 0, err
	}
	groupIdsOfPhoto := map[string]bool{}
	var waiting []*models.PhotoFace
	for _, face := range stored {
		if face.PhotoFaceGroupID != "" {
			// The person confirmed this face where it is.
			groupIdsOfPhoto[face.PhotoFaceGroupID] = true
			continue
		}
		if isFaceGroupable(face, widthPixels, heightPixels) {
			waiting = append(waiting, face)
		}
	}
	groupedCount := 0
	for len(waiting) > 0 {
		// The face whose nearest group is nearest of all goes first.
		bestIndex, bestGroupId, bestSimilarity := -1, "", 0.0
		for index, face := range waiting {
			if groupId, similarity := nearestFaceGroup(face.FaceVector, groups, groupIdsOfPhoto); groupId != "" && (bestIndex < 0 || similarity > bestSimilarity) {
				bestIndex, bestGroupId, bestSimilarity = index, groupId, similarity
			}
		}
		if bestIndex < 0 {
			break
		}
		face := waiting[bestIndex]
		waiting = append(waiting[:bestIndex], waiting[bestIndex+1:]...)
		if err := transaction.JoinPhotoFaceGroup(userId, face.ID, bestGroupId); err != nil {
			return groupedCount, err
		}
		for _, group := range groups {
			if group.ID == bestGroupId {
				for index, number := range face.FaceVector {
					group.VectorSum[index] += number
				}
			}
		}
		face.PhotoFaceGroupID = bestGroupId
		groupIdsOfPhoto[bestGroupId] = true
		groupedCount++
	}
	// What is like no group, or like none left to this photo, is somebody new.
	for _, face := range waiting {
		groupId, err := transaction.StartPhotoFaceGroup(userId, face.ID)
		if err != nil {
			return groupedCount, err
		}
		groups = append(groups, &db.PhotoFaceGroupVector{ID: groupId, VectorSum: append([]float32(nil), face.FaceVector...)})
		face.PhotoFaceGroupID = groupId
		groupIdsOfPhoto[groupId] = true
		groupedCount++
	}
	return groupedCount, nil
}
