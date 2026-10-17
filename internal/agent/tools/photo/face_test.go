package photo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

const (
	namedGroupJson   = `{"id":"group-one","contactId":"contact-one","contactName":"Example Aunt","faceCount":12,"photoCount":9}`
	unnamedGroupJson = `{"id":"group-two","contactId":"","contactName":"","faceCount":4,"photoCount":4,"isHidden":false}`
)

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
	current := newRun(test, map[string]string{"NamePhotoFaceGroup": namedGroupJson})
	result, err := current.call(map[string]any{"operation": "person-name", "face_group_id": "group-one", "contact_id": "contact-one"})
	if err != nil || !strings.Contains(result.Content, `"contact_name":"Example Aunt"`) || !strings.Contains(result.Note, "Example Aunt") {
		test.Fatalf("%v %v", result, err)
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

func TestSwitchingFacesOffAsksFirstAndTheRestOfThePeopleOperationsDoNot(test *testing.T) {
	for _, scenario := range []struct {
		arguments map[string]any
		risk      tools.Risk
		line      string
	}{
		{map[string]any{"operation": "people"}, tools.RiskRead, "List the people found in the photos"},
		{map[string]any{"operation": "person-photos", "face_group_id": "x"}, tools.RiskRead, "List the photos of a person"},
		{map[string]any{"operation": "person-name"}, tools.RiskWrite, "Say who a group of faces is, by a contact"},
		{map[string]any{"operation": "person-merge"}, tools.RiskWrite, "Put groups of faces together as one person"},
		{map[string]any{"operation": "faces"}, tools.RiskRead, "Read whether faces are found in the photos"},
		{map[string]any{"operation": "faces", "is_enabled": true}, tools.RiskWrite, "Find faces in the photos"},
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
			`{"id":"face-three","photoFaceGroupId":"","contactId":"","contactName":""}]}`,
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
	if len(details.People) != 3 || details.People[0]["contact_name"] != "Example Aunt" || details.People[0]["face_group_id"] != "group-one" || details.People[0]["contact_id"] != "contact-one" ||
		details.People[1]["face_group_id"] != "group-two" || details.People[1]["contact_name"] != nil || details.People[2]["face_group_id"] != nil {
		test.Fatalf("%s", result.Content)
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
	for _, name := range []string{"face_group_id", "face_group_ids", "contact_id", "is_enabled", "album_id", "album_title"} {
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
	for _, name := range []string{"people", "person-photos", "person-name", "person-merge", "faces"} {
		found := false
		for _, candidate := range enum {
			found = found || candidate == name
		}
		if !found {
			test.Errorf("the tool has no %s", name)
		}
	}
	for _, words := range []string{"never guess who somebody is from their face", "ask the person who it is", "always asks the person first"} {
		if !strings.Contains(photoTool.Description, words) {
			test.Errorf("the description does not say %q", words)
		}
	}
}
