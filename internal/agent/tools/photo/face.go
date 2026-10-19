package photo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// People are the faces found in the library, grouped by person. A group is
// named by a contact from the person's address book. The operations call the
// ones the dashboard's People page and teanode photo people call.

// maximumMergedGroupCount is how many groups person-merge puts into one.
const maximumMergedGroupCount = client.MaximumMergedPhotoFaceGroups

// facePreviewOf describes a people operation for the card that asks first.
func facePreviewOf(asked arguments) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(asked.Operation)) {
	case "people":
		return "List the people found in the photos", true
	case "person-read":
		return "Read one person found in the photos", true
	case "person-photos":
		return "List the photos of a person", true
	case "person-name":
		return "Say who a group of faces is, by a contact", true
	case "person-merge":
		return "Put groups of faces together as one person", true
	case "person-unname":
		return "Take the name off a group of faces; the faces stay grouped", true
	case "person-hide":
		if asked.IsHidden != nil && !*asked.IsHidden {
			return "List a hidden person among the people again", true
		}
		return "Leave a person out of the people list", true
	case "person-remove-face":
		return "Say a face is not the person its group is", true
	case "faces":
		switch {
		case asked.IsEnabled == nil:
			return "Read whether faces are found in the photos", true
		case *asked.IsEnabled:
			return "Start finding faces in every photo, and keep what identifies each face", true
		}
		return "Stop finding faces in the photos and delete every face and group found so far", true
	}
	return "", false
}

// faceRiskOf is how risky a people operation is. Finding faces is a privacy
// choice either way, so both ways of changing it ask the person. Switching
// faces on finds and keeps a face vector for every face in every photo,
// the faces of other people among them, which is not something to start because
// a message said to: it is granting, the class that asks unless the person has
// allowed it. Switching faces off deletes what was found, and the only way back
// is reading every photo again, so it is destructive. Naming, merging, taking
// a name off, hiding and taking a face out of its group only change who the
// groups are called and which are listed, and the person can change them back.
func faceRiskOf(asked arguments) (tools.Risk, bool) {
	switch strings.ToLower(strings.TrimSpace(asked.Operation)) {
	case "people", "person-read", "person-photos":
		return tools.RiskRead, true
	case "person-name", "person-merge", "person-unname", "person-hide", "person-remove-face":
		return tools.RiskWrite, true
	case "faces":
		switch {
		case asked.IsEnabled == nil:
			return tools.RiskRead, true
		case *asked.IsEnabled:
			return tools.RiskGranting, true
		}
		return tools.RiskDestructive, true
	}
	return tools.RiskRead, false
}

// groupRow is a group of faces as the agent is told of it. The name is the
// person's own contact, written by anybody who could write to the address
// book, so an answer that carries it is untrusted.
func groupRow(group *models.PhotoFaceGroup) map[string]any {
	row := map[string]any{"face_group_id": group.ID, "face_count": group.FaceCount, "photo_count": group.PhotoCount}
	if group.ContactID != "" {
		row["contact_id"], row["contact_name"] = group.ContactID, group.ContactName
	} else {
		row["contact_name"] = nil
	}
	if group.IsOwnContact {
		row["is_you"] = true
	}
	if group.Account != nil {
		row["person"] = group.Account.Username
	}
	if group.IsHidden {
		row["is_hidden"] = true
	}
	return row
}

func people(ctx context.Context, current tools.Run, asked *arguments) (*tools.Result, error) {
	groups, err := client.ListPhotoFaceGroups(ctx, current.Operations(), asked.IsHiddenIncluded)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, groupRow(group))
	}
	answer := map[string]any{
		"note": "a group nobody has named is listed once it has three faces; a face group is named by a contact; list with face_group_id or contact_id gives a person's photos. A group with contact_name null is nobody yet: ask the person who it is, and name it only with a contact they choose",
	}
	page, err := pageOfRows(rows, asked, answer)
	if err != nil {
		return nil, err
	}
	answer["people"] = page
	result, err := tools.JSONResult(answer)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

// personRead gives one group of faces: who it is named by, how many faces and
// photos it has and the face it is shown by. Its photos are person-photos.
func personRead(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	faceGroupId := strings.TrimSpace(asked.FaceGroupID)
	if faceGroupId == "" {
		return nil, errors.New("person-read needs face_group_id; people gives them")
	}
	group, err := client.ReadPhotoFaceGroup(ctx, executor, faceGroupId)
	if err != nil {
		return nil, err
	}
	row := groupRow(group)
	if group.CoverPhotoFaceID != "" {
		row["cover_photo_face_id"] = group.CoverPhotoFaceID
	}
	row["note"] = "person-photos with this face_group_id lists their photos"
	result, err := tools.JSONResult(row)
	if err != nil {
		return nil, err
	}
	result.Untrusted = true
	return result, nil
}

// personPhotos lists the photos of one group of faces or of one contact, in
// the pages list gives.
func personPhotos(ctx context.Context, current tools.Run, asked *arguments) (*tools.Result, error) {
	faceGroupId, contactId := strings.TrimSpace(asked.FaceGroupID), strings.TrimSpace(asked.ContactID)
	if faceGroupId == "" && contactId == "" && strings.TrimSpace(asked.Person) == "" {
		return nil, errors.New("person-photos needs face_group_id or contact_id (or person, the username of somebody in the circles); people gives them")
	}
	return list(ctx, current, asked)
}

func personName(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	faceGroupId, contactId := strings.TrimSpace(asked.FaceGroupID), strings.TrimSpace(asked.ContactID)
	if faceGroupId == "" || contactId == "" {
		return nil, errors.New("person-name needs face_group_id and contact_id: the contact the person chose or confirmed. Do not guess a name from a face; ask the person who it is")
	}
	named, err := client.NamePhotoFaceGroup(ctx, executor, faceGroupId, contactId)
	if err != nil {
		return nil, err
	}
	row := groupRow(named.PhotoFaceGroup)
	result, err := tools.JSONResult(namedGroupRow(row, named))
	if err != nil {
		return nil, err
	}
	result.Note = "named a group of faces " + tools.Named(named.PhotoFaceGroup.ContactName, "after a contact")
	switch {
	case named.MergedPhotoFaceGroupID != "" && named.IsContactPictureSet:
		result.Note += ", put it together with the group that contact already named, and used a face as their picture"
	case named.MergedPhotoFaceGroupID != "":
		result.Note += " and put it together with the group that contact already named"
	case named.IsContactPictureSet:
		result.Note += " and used a face as their picture"
	}
	return result, nil
}

// namedGroupRow is the group naming left, with what else the naming did. A
// contact names one person: face_group_id is the group left, merged_into says
// the group that was named is part of it now (merged_face_group_id is the id
// that is gone), and contact_picture_set says the contact had no picture and
// was given one cut from a face.
func namedGroupRow(row map[string]any, named *client.NamedPhotoFaceGroup) map[string]any {
	if named.MergedPhotoFaceGroupID != "" {
		row["merged_into"], row["merged_face_group_id"] = named.PhotoFaceGroup.ID, named.MergedPhotoFaceGroupID
	}
	if named.IsContactPictureSet {
		row["contact_picture_set"] = true
	}
	return row
}

func personMerge(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	groupIds := make([]string, 0, len(asked.FaceGroupIDs))
	seen := map[string]bool{}
	for _, groupId := range asked.FaceGroupIDs {
		if groupId = strings.TrimSpace(groupId); groupId != "" && !seen[groupId] {
			seen[groupId] = true
			groupIds = append(groupIds, groupId)
		}
	}
	if len(groupIds) < 2 {
		return nil, errors.New("person-merge needs face_group_ids, at least two groups; the first is the one that stays")
	}
	if len(groupIds) > maximumMergedGroupCount {
		return nil, fmt.Errorf("person-merge takes at most %d groups at once, not %d", maximumMergedGroupCount, len(groupIds))
	}
	group, err := client.MergePhotoFaceGroups(ctx, executor, groupIds)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(groupRow(group))
	if err != nil {
		return nil, err
	}
	result.Note = fmt.Sprintf("put %d groups of faces together", len(groupIds))
	return result, nil
}

func personUnname(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	faceGroupId := strings.TrimSpace(asked.FaceGroupID)
	if faceGroupId == "" {
		return nil, errors.New("person-unname needs face_group_id; people gives them")
	}
	group, err := client.UnnamePhotoFaceGroup(ctx, executor, faceGroupId)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(groupRow(group))
	if err != nil {
		return nil, err
	}
	result.Note = "took the name off a group of faces; its faces stay grouped"
	return result, nil
}

// personHide leaves a group out of the people list, or with is_hidden false
// lists it again. Hiding is the default, as favorite's marking is.
func personHide(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	faceGroupId := strings.TrimSpace(asked.FaceGroupID)
	if faceGroupId == "" {
		return nil, errors.New("person-hide needs face_group_id; people gives them, and people with is_hidden_included gives the hidden ones")
	}
	isHidden := asked.IsHidden == nil || *asked.IsHidden
	group, err := client.HidePhotoFaceGroup(ctx, executor, faceGroupId, isHidden)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(groupRow(group))
	if err != nil {
		return nil, err
	}
	if isHidden {
		result.Note = "left a group of faces out of the people list"
	} else {
		result.Note = "listed a group of faces among the people again"
	}
	return result, nil
}

// personRemoveFace says one face is not the person its group is: the face
// goes into a group of its own that stays confirmed, so it is not grouped
// with them again.
func personRemoveFace(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	photoFaceId := strings.TrimSpace(asked.PhotoFaceID)
	if photoFaceId == "" {
		return nil, errors.New("person-remove-face needs photo_face_id, the face's id as read gives it for a photo")
	}
	group, err := client.RemovePhotoFaceFromGroup(ctx, executor, photoFaceId)
	if err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(groupRow(group))
	if err != nil {
		return nil, err
	}
	result.Note = "took a face out of its group; it is in a group of its own and will not be grouped with them again"
	return result, nil
}

// faces reads whether faces are found, and with is_enabled switches them.
func faces(ctx context.Context, executor client.Executor, asked *arguments) (*tools.Result, error) {
	if asked.IsEnabled != nil {
		if _, err := client.SetPhotoFaceRecognition(ctx, executor, *asked.IsEnabled); err != nil {
			return nil, err
		}
	}
	library, err := client.ReadPhotoLibrary(ctx, executor)
	if err != nil {
		return nil, err
	}
	answer := map[string]any{"is_available": library.IsFaceRecognitionAvailable, "is_enabled": library.IsPhotoFaceRecognitionEnabled}
	if !library.IsFaceRecognitionAvailable {
		answer["unavailable_message"] = library.FaceRecognitionUnavailableMessage
	}
	if asked.IsEnabled != nil && !*asked.IsEnabled {
		answer["note"] = "every face and group of people found so far is deleted; switching faces on again finds them again"
	}
	return tools.JSONResult(answer)
}

// peopleOf are the people in a photo, for read: each face with the group it
// is in and the contact that names the group.
func peopleOf(view *client.PhotoView) []map[string]any {
	rows := make([]map[string]any, 0, len(view.Faces))
	for _, face := range view.Faces {
		row := map[string]any{"photo_face_id": face.ID}
		if face.PhotoFaceGroupID != "" {
			row["face_group_id"] = face.PhotoFaceGroupID
		}
		if face.ContactID != "" {
			row["contact_id"], row["contact_name"] = face.ContactID, face.ContactName
		}
		if face.IsOwnContact {
			row["is_you"] = true
		}
		rows = append(rows, row)
	}
	return rows
}
