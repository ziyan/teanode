package photo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

const (
	namedGroupJson   = `{"id":"group-one","contactId":"contact-one","contactName":"Example Aunt","faceCount":12,"photoCount":9}`
	unnamedGroupJson = `{"id":"group-two","contactId":"","contactName":"","faceCount":4,"photoCount":4,"isHidden":false}`
	ownGroupJson     = `{"id":"group-three","contactId":"contact-own","contactName":"Example Self","isOwnContact":true,"faceCount":30,"photoCount":25}`
)

func TestAGroupNamedByThePersonsOwnContactIsSaidToBeThem(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotoFaceGroups": `[` + ownGroupJson + `,` + namedGroupJson + `,` + unnamedGroupJson + `]`, "NamePhotoFaceGroup": namingJson(ownGroupJson, "", false)})
	result, err := current.call(map[string]any{"operation": "people"})
	if err != nil {
		test.Fatal(err)
	}
	var answer struct {
		People []map[string]any `json:"people"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.People) != 3 || answer.People[0]["is_you"] != true {
		test.Fatalf("%s", result.Content)
	}
	for _, other := range answer.People[1:] {
		if _, isThere := other["is_you"]; isThere {
			test.Fatalf("only the person's own contact is them: %s", result.Content)
		}
	}
	// Naming answers with the same row.
	named, err := current.call(map[string]any{"operation": "person-name", "face_group_id": "group-three", "contact_id": "contact-own"})
	if err != nil || !strings.Contains(named.Content, `"is_you":true`) {
		test.Fatalf("%v %v", named, err)
	}
}

// namingJson is what naming a group answers.
func namingJson(groupJson, mergedPhotoFaceGroupId string, isContactPictureSet bool) string {
	return fmt.Sprintf(`{"photoFaceGroup":%s,"mergedPhotoFaceGroupId":%q,"isContactPictureSet":%t}`, groupJson, mergedPhotoFaceGroupId, isContactPictureSet)
}

// A contact names one person: naming a group with a contact that names another
// group answers with the group left, and says the group named is part of it.
// A contact without a picture is given a face, and the answer says so.
func TestNamingAGroupSaysWhenItWasMergedAndWhenTheContactWasGivenAPicture(test *testing.T) {
	current := newRun(test, map[string]string{"NamePhotoFaceGroup": namingJson(namedGroupJson, "group-two", true)})
	result, err := current.call(map[string]any{"operation": "person-name", "face_group_id": "group-two", "contact_id": "contact-one"})
	if err != nil {
		test.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if answer["face_group_id"] != "group-one" || answer["merged_into"] != "group-one" || answer["merged_face_group_id"] != "group-two" || answer["contact_picture_set"] != true {
		test.Fatalf("%s", result.Content)
	}
	for _, want := range []string{"put it together", "used a face as their picture"} {
		if !strings.Contains(result.Note, want) {
			test.Fatalf("%q lacks %q", result.Note, want)
		}
	}
	// Merged only, and picture only.
	current = newRun(test, map[string]string{"NamePhotoFaceGroup": namingJson(namedGroupJson, "group-two", false)})
	result, err = current.call(map[string]any{"operation": "person-name", "face_group_id": "group-two", "contact_id": "contact-one"})
	if err != nil || !strings.Contains(result.Content, `"merged_into":"group-one"`) || strings.Contains(result.Content, "contact_picture_set") {
		test.Fatalf("%v %v", result, err)
	}
	current = newRun(test, map[string]string{"NamePhotoFaceGroup": namingJson(namedGroupJson, "", true)})
	result, err = current.call(map[string]any{"operation": "person-name", "face_group_id": "group-one", "contact_id": "contact-one"})
	if err != nil || strings.Contains(result.Content, "merged_into") || !strings.Contains(result.Content, `"contact_picture_set":true`) {
		test.Fatalf("%v %v", result, err)
	}
}

func TestPeopleListsGroupsWithTheirNamesAndSaysAnUnnamedOneIsNobodyYet(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotoFaceGroups": `[` + namedGroupJson + `,` + unnamedGroupJson + `]`})
	result, err := current.call(map[string]any{"operation": "people"})
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted {
		test.Fatal("a contact's name is somebody's words")
	}
	var answer struct {
		People []struct {
			FaceGroupID string  `json:"face_group_id"`
			ContactID   string  `json:"contact_id"`
			ContactName *string `json:"contact_name"`
			FaceCount   int     `json:"face_count"`
			PhotoCount  int     `json:"photo_count"`
		} `json:"people"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.People) != 2 || answer.People[0].FaceGroupID != "group-one" || answer.People[0].ContactID != "contact-one" || answer.People[0].ContactName == nil ||
		*answer.People[0].ContactName != "Example Aunt" || answer.People[0].FaceCount != 12 || answer.People[0].PhotoCount != 9 {
		test.Fatalf("%s", result.Content)
	}
	if answer.People[1].ContactName != nil || answer.People[1].ContactID != "" || !strings.Contains(answer.Note, "ask the person who it is") {
		test.Fatalf("%s", result.Content)
	}
	if asked := current.operations.asked("ListPhotoFaceGroups"); len(asked) != 1 || asked[0]["isHiddenIncluded"] != false {
		test.Fatalf("%v", asked)
	}
}

func TestAPersonsPhotosAreAskedForByGroupOrByContact(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": `[` + pictureRow + `]`})
	if _, err := current.call(map[string]any{"operation": "person-photos", "face_group_id": " group-one "}); err != nil {
		test.Fatal(err)
	}
	if _, err := current.call(map[string]any{"operation": "person-photos", "contact_id": "contact-one", "page_size": 5}); err != nil {
		test.Fatal(err)
	}
	if _, err := current.call(map[string]any{"operation": "list", "contact_id": "contact-one", "taken_after": "2025-01-01"}); err != nil {
		test.Fatal(err)
	}
	asked := current.operations.asked("ListPhotos")
	if len(asked) != 3 || asked[0]["faceGroupId"] != "group-one" || asked[0]["contactId"] != nil || asked[1]["contactId"] != "contact-one" || asked[1]["pageSize"] != 5 ||
		asked[2]["contactId"] != "contact-one" || asked[2]["takenAfter"] != "2025-01-01T00:00:00Z" {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "person-photos"}); err == nil || !strings.Contains(err.Error(), "face_group_id or contact_id") {
		test.Fatalf("%v", err)
	}
}

func TestNamingAGroupNeedsTheGroupAndAContactAndSaysNotToGuess(test *testing.T) {
	current := newRun(test, map[string]string{"NamePhotoFaceGroup": namingJson(namedGroupJson, "", false)})
	result, err := current.call(map[string]any{"operation": "person-name", "face_group_id": "group-one", "contact_id": "contact-one"})
	if err != nil || !strings.Contains(result.Content, `"contact_name":"Example Aunt"`) || !strings.Contains(result.Note, "Example Aunt") {
		test.Fatalf("%v %v", result, err)
	}
	// Nothing was merged and no picture was made, so the answer says neither.
	if strings.Contains(result.Content, "merged_into") || strings.Contains(result.Content, "contact_picture_set") {
		test.Fatalf("%s", result.Content)
	}
	if asked := current.operations.asked("NamePhotoFaceGroup"); len(asked) != 1 || asked[0]["photoFaceGroupId"] != "group-one" || asked[0]["contactId"] != "contact-one" {
		test.Fatalf("%v", asked)
	}
	for _, arguments := range []map[string]any{
		{"operation": "person-name", "face_group_id": "group-one"},
		{"operation": "person-name", "contact_id": "contact-one"},
		{"operation": "person-name", "face_group_id": "group-one", "contact_id": "  "},
	} {
		_, err := current.call(arguments)
		if err == nil || !strings.Contains(err.Error(), "Do not guess a name from a face") {
			test.Fatalf("%v: %v", arguments, err)
		}
	}
	if len(current.operations.asked("NamePhotoFaceGroup")) != 1 {
		test.Fatal("a refused call was sent")
	}
	// What the server refuses is what the agent is told.
	current.operations.refusals = map[string]string{"NamePhotoFaceGroup": "naming a person means choosing a contact; turn on Contacts to name people"}
	if _, err := current.call(map[string]any{"operation": "person-name", "face_group_id": "group-one", "contact_id": "contact-one"}); err == nil || !strings.Contains(err.Error(), "turn on Contacts") {
		test.Fatalf("%v", err)
	}
}

func TestMergingGroupsNeedsTwoAndNoMoreThanTheServerTakes(test *testing.T) {
	current := newRun(test, map[string]string{"MergePhotoFaceGroups": namedGroupJson})
	if result, err := current.call(map[string]any{"operation": "person-merge", "face_group_ids": []string{"group-one", " group-two ", "group-one"}}); err != nil || !strings.Contains(result.Note, "2 groups") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("MergePhotoFaceGroups"); len(asked) != 1 || strings.Join(asked[0]["photoFaceGroupIds"].([]string), ",") != "group-one,group-two" {
		test.Fatalf("%v", asked)
	}
	tooMany := make([]string, 51)
	for index := range tooMany {
		tooMany[index] = "group-" + string(rune('a'+index%26)) + string(rune('a'+index/26))
	}
	for _, arguments := range []map[string]any{
		{"operation": "person-merge"}, {"operation": "person-merge", "face_group_ids": []string{"group-one"}},
		{"operation": "person-merge", "face_group_ids": []string{"group-one", "group-one"}}, {"operation": "person-merge", "face_group_ids": tooMany},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(current.operations.asked("MergePhotoFaceGroups")) != 1 {
		test.Fatal("a refused call was sent")
	}
}

func TestAPersonIsUnnamedHiddenShownAndCorrectedWithTheSharedOperations(test *testing.T) {
	current := newRun(test, map[string]string{
		"UnnamePhotoFaceGroup":     unnamedGroupJson,
		"HidePhotoFaceGroup":       `{"id":"group-two","contactId":"","contactName":"","faceCount":4,"photoCount":4,"isHidden":true}`,
		"RemovePhotoFaceFromGroup": `{"id":"group-three","contactId":"","contactName":"","faceCount":1,"photoCount":1}`,
		"ListPhotoFaceGroups":      `[` + unnamedGroupJson + `]`,
	})
	operations := current.operations
	if result, err := current.call(map[string]any{"operation": "person-unname", "face_group_id": " group-one "}); err != nil || !strings.Contains(result.Note, "faces stay grouped") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("UnnamePhotoFaceGroup"); len(asked) != 1 || asked[0]["photoFaceGroupId"] != "group-one" {
		test.Fatalf("%v", asked)
	}
	// Hiding is the default; is_hidden false lists the person again.
	if result, err := current.call(map[string]any{"operation": "person-hide", "face_group_id": "group-two"}); err != nil || !strings.Contains(result.Note, "left a group of faces out") {
		test.Fatalf("%v %v", result, err)
	}
	if result, err := current.call(map[string]any{"operation": "person-hide", "face_group_id": "group-two", "is_hidden": false}); err != nil || !strings.Contains(result.Note, "among the people again") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("HidePhotoFaceGroup"); len(asked) != 2 || asked[0]["isHidden"] != true || asked[1]["isHidden"] != false || asked[1]["photoFaceGroupId"] != "group-two" {
		test.Fatalf("%v", asked)
	}
	if result, err := current.call(map[string]any{"operation": "person-remove-face", "photo_face_id": "face-one"}); err != nil || !strings.Contains(result.Note, "will not be grouped with them again") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("RemovePhotoFaceFromGroup"); len(asked) != 1 || asked[0]["photoFaceId"] != "face-one" {
		test.Fatalf("%v", asked)
	}
	// The hidden groups are found through people, to be listed again.
	if _, err := current.call(map[string]any{"operation": "people", "is_hidden_included": true}); err != nil {
		test.Fatal(err)
	}
	if asked := operations.asked("ListPhotoFaceGroups"); len(asked) != 1 || asked[0]["isHiddenIncluded"] != true {
		test.Fatalf("%v", asked)
	}
	for _, arguments := range []map[string]any{
		{"operation": "person-unname"}, {"operation": "person-hide"}, {"operation": "person-remove-face"},
		{"operation": "person-remove-face", "face_group_id": "group-one"},
	} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(operations.asked("UnnamePhotoFaceGroup")) != 1 || len(operations.asked("HidePhotoFaceGroup")) != 2 || len(operations.asked("RemovePhotoFaceFromGroup")) != 1 {
		test.Fatal("something was sent for a call that was refused")
	}
}

func TestFacesReadsWhetherTheyAreFoundAndSwitchesThemOnlyWhenToldTo(test *testing.T) {
	current := newRun(test, map[string]string{
		"ReadPhotoLibrary":        `{"isFaceRecognitionAvailable":true,"faceRecognitionUnavailableMessage":"","isPhotoFaceRecognitionEnabled":true}`,
		"SetPhotoFaceRecognition": `false`,
	})
	result, err := current.call(map[string]any{"operation": "faces"})
	if err != nil || !strings.Contains(result.Content, `"is_available":true`) || !strings.Contains(result.Content, `"is_enabled":true`) {
		test.Fatalf("%v %v", result, err)
	}
	if len(current.operations.asked("SetPhotoFaceRecognition")) != 0 {
		test.Fatal("reading whether faces are on switched them")
	}
	result, err = current.call(map[string]any{"operation": "faces", "is_enabled": false})
	if err != nil || !strings.Contains(result.Content, "deleted") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := current.operations.asked("SetPhotoFaceRecognition"); len(asked) != 1 || asked[0]["isEnabled"] != false {
		test.Fatalf("%v", asked)
	}
	current.operations.answers["ReadPhotoLibrary"] = `{"isFaceRecognitionAvailable":false,"faceRecognitionUnavailableMessage":"faces are unavailable: the program was not found","isPhotoFaceRecognitionEnabled":true}`
	if result, err := current.call(map[string]any{"operation": "faces"}); err != nil || !strings.Contains(result.Content, "the program was not found") {
		test.Fatalf("%v %v", result, err)
	}
}

func TestSwitchingFacesOnOrOffAsksFirstAndTheRestOfThePeopleOperationsDoNot(test *testing.T) {
	for _, scenario := range []struct {
		arguments map[string]any
		risk      tools.Risk
		line      string
	}{
		{map[string]any{"operation": "people"}, tools.RiskRead, "List the people found in the photos"},
		{map[string]any{"operation": "person-photos", "face_group_id": "x"}, tools.RiskRead, "List the photos of a person"},
		{map[string]any{"operation": "person-name"}, tools.RiskWrite, "Say who a group of faces is, by a contact"},
		{map[string]any{"operation": "person-merge"}, tools.RiskWrite, "Put groups of faces together as one person"},
		{map[string]any{"operation": "person-unname"}, tools.RiskWrite, "Take the name off a group of faces; the faces stay grouped"},
		{map[string]any{"operation": "person-hide"}, tools.RiskWrite, "Leave a person out of the people list"},
		{map[string]any{"operation": "person-hide", "is_hidden": false}, tools.RiskWrite, "List a hidden person among the people again"},
		{map[string]any{"operation": "person-remove-face"}, tools.RiskWrite, "Say a face is not the person its group is"},
		{map[string]any{"operation": "album-cover"}, tools.RiskWrite, "Choose the photo an album shows as its cover"},
		{map[string]any{"operation": "faces"}, tools.RiskRead, "Read whether faces are found in the photos"},
		{map[string]any{"operation": "faces", "is_enabled": true}, tools.RiskGranting, "Start finding faces in every photo, and keep what identifies each face"},
		{map[string]any{"operation": "faces", "is_enabled": false}, tools.RiskDestructive, "Stop finding faces in the photos and delete every face and group found so far"},
	} {
		encoded, _ := json.Marshal(scenario.arguments)
		if risk := riskOf(encoded); risk != scenario.risk {
			test.Errorf("%v is %v, want %v", scenario.arguments, risk, scenario.risk)
		}
		var asked arguments
		if err := json.Unmarshal(encoded, &asked); err != nil {
			test.Fatal(err)
		}
		if line := previewOf(asked); line != scenario.line {
			test.Errorf("%v: the card says %q", scenario.arguments, line)
		}
	}
}

func TestReadSaysWhoIsInAPhoto(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-one": `{"photo":` + pictureRow + `,"photoFiles":[],"photoPreviews":[],"albums":[],"faces":[` +
			`{"id":"face-one","photoFaceGroupId":"group-one","contactId":"contact-one","contactName":"Example Aunt"},` +
			`{"id":"face-two","photoFaceGroupId":"group-two","contactId":"","contactName":""},` +
			`{"id":"face-three","photoFaceGroupId":"","contactId":"","contactName":""},` +
			`{"id":"face-four","photoFaceGroupId":"group-three","contactId":"contact-own","contactName":"Example Self","isOwnContact":true}]}`,
		"photo-two": `{"photo":` + videoRow + `,"photoFiles":[],"photoPreviews":[]}`,
	}}
	result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-one"})
	if err != nil {
		test.Fatal(err)
	}
	var details struct {
		People []map[string]any `json:"people"`
	}
	if err := json.Unmarshal([]byte(result.Content), &details); err != nil {
		test.Fatal(err)
	}
	if len(details.People) != 4 || details.People[0]["contact_name"] != "Example Aunt" || details.People[0]["face_group_id"] != "group-one" || details.People[0]["contact_id"] != "contact-one" ||
		details.People[1]["face_group_id"] != "group-two" || details.People[1]["contact_name"] != nil || details.People[2]["face_group_id"] != nil {
		test.Fatalf("%s", result.Content)
	}
	// Only the face of the person's own contact says it is them.
	if details.People[3]["is_you"] != true || details.People[3]["contact_name"] != "Example Self" {
		test.Fatalf("the person's own face should say is_you: %s", result.Content)
	}
	for _, other := range details.People[:3] {
		if _, isThere := other["is_you"]; isThere {
			test.Fatalf("only the person's own face is them: %s", result.Content)
		}
	}
	if result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-two"}); err != nil || !strings.Contains(result.Content, `"people":[]`) {
		test.Fatalf("a photo with no faces should say so: %v %v", result, err)
	}
}

func TestTheToolDeclaresEveryParameterItsOperationsTakeAndTellsTheAgentNotToGuessNames(test *testing.T) {
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the photo tool is not registered")
	}
	properties, _ := photoTool.Parameters["properties"].(map[string]any)
	for _, name := range []string{"face_group_id", "face_group_ids", "contact_id", "is_enabled", "album_id", "album_title", "photo_face_id", "is_hidden", "is_hidden_included"} {
		if _, isDeclared := properties[name]; !isDeclared {
			test.Errorf("the tool does not declare %s, so a model cannot pass it", name)
		}
	}
	operation, _ := properties["operation"].(map[string]any)
	var enum []string
	switch typed := operation["enum"].(type) {
	case []string:
		enum = typed
	case []any:
		for _, name := range typed {
			enum = append(enum, name.(string))
		}
	}
	for _, name := range []string{"people", "person-photos", "person-name", "person-unname", "person-hide", "person-remove-face", "person-merge", "faces", "album-cover"} {
		found := false
		for _, candidate := range enum {
			found = found || candidate == name
		}
		if !found {
			test.Errorf("the tool has no %s", name)
		}
	}
	for _, words := range []string{"never guess who somebody is from their face", "is_you true is the person themselves", "ask the person who it is", "always asks the person first"} {
		if !strings.Contains(photoTool.Description, words) {
			test.Errorf("the description does not say %q", words)
		}
	}
}
