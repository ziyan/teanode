package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

const (
	fixtureNamedGroup   = `{"id":"group-1","userId":"u","contactId":"contact-1","contactAddressBookId":"book-1","contactName":"Example Aunt","faceCount":12,"photoCount":9,"facePictureUrl":"/x","isHidden":false,"coverPhotoFaceId":"face-1"}`
	fixtureOwnGroup     = `{"id":"group-4","userId":"u","contactId":"contact-own","contactAddressBookId":"book-1","contactName":"Ada Example","isOwnContact":true,"faceCount":30,"photoCount":25,"facePictureUrl":"/z","isHidden":false,"coverPhotoFaceId":"face-4"}`
	fixtureUnnamedGroup = `{"id":"group-2","userId":"u","contactId":"","contactAddressBookId":"","contactName":"","faceCount":4,"photoCount":4,"facePictureUrl":"/y","isHidden":true,"coverPhotoFaceId":"face-2"}`
)

// newPhotoFaceServer answers the people documents; everything else is nothing.
func newPhotoFaceServer(test *testing.T) *photoServer {
	test.Helper()
	self := &photoServer{files: map[string][]byte{}, photoViews: map[string]string{}}
	self.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		var asked struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if request.URL.Path != "/api/v1/graphql" || json.NewDecoder(request.Body).Decode(&asked) != nil {
			http.NotFound(response, request)
			return
		}
		self.documents = append(self.documents, asked.Query)
		self.variables = append(self.variables, asked.Variables)
		response.Header().Set("Content-Type", "application/json")
		answer := func(operation, data string) {
			_, _ = response.Write([]byte(`{"data":{"` + operation + `":` + data + `}}`))
		}
		switch {
		case strings.Contains(asked.Query, "ListPhotoFaceGroups("):
			answer("ListPhotoFaceGroups", `[`+fixtureNamedGroup+`,`+fixtureUnnamedGroup+`,`+fixtureOwnGroup+`]`)
		case strings.Contains(asked.Query, "ReadPhotoFaceGroup("):
			if asked.Variables["photoFaceGroupId"] == "group-4" {
				answer("ReadPhotoFaceGroup", fixtureOwnGroup)
				return
			}
			answer("ReadPhotoFaceGroup", fixtureNamedGroup)
		case strings.Contains(asked.Query, "UnnamePhotoFaceGroup("):
			answer("UnnamePhotoFaceGroup", fixtureUnnamedGroup)
		case strings.Contains(asked.Query, "NamePhotoFaceGroup("):
			switch asked.Variables["photoFaceGroupId"] {
			case "group-merged":
				answer("NamePhotoFaceGroup", `{"photoFaceGroup":`+fixtureNamedGroup+`,"mergedPhotoFaceGroupId":"group-merged","isContactPictureSet":true}`)
			default:
				answer("NamePhotoFaceGroup", `{"photoFaceGroup":`+fixtureNamedGroup+`,"mergedPhotoFaceGroupId":"","isContactPictureSet":false}`)
			}
		case strings.Contains(asked.Query, "MergePhotoFaceGroups("):
			answer("MergePhotoFaceGroups", fixtureNamedGroup)
		case strings.Contains(asked.Query, "RemovePhotoFaceFromGroup("):
			answer("RemovePhotoFaceFromGroup", fixtureUnnamedGroup)
		case strings.Contains(asked.Query, "HidePhotoFaceGroup("):
			answer("HidePhotoFaceGroup", fixtureUnnamedGroup)
		case strings.Contains(asked.Query, "SetPhotoFaceRecognition("):
			answer("SetPhotoFaceRecognition", boolText(asked.Variables["isEnabled"]))
		case strings.Contains(asked.Query, "ReadPhotoLibrary"):
			answer("ReadPhotoLibrary", `{"photoCount":5,"videoCount":2,"favoriteCount":1,"trashedCount":3,"usedBytes":1572864,"maximumFileBytes":8589934592,"maximumAccountBytes":0,"trashRetentionDays":30,"isFaceRecognitionAvailable":true,"faceRecognitionUnavailableMessage":"","isPhotoFaceRecognitionEnabled":false}`)
		case strings.Contains(asked.Query, "ReadPhoto("):
			answer("ReadPhoto", `{"photo":{"id":"photo-1","photoKind":"picture","photoProcessingState":"ready","createdAt":"2025-03-04T10:30:00Z"},"photoFiles":[],"photoPreviews":[],"albums":[],"faces":[`+
				`{"id":"face-1","photoId":"photo-1","photoFaceGroupId":"group-1","contactId":"contact-1","contactName":"Example Aunt"},`+
				`{"id":"face-2","photoId":"photo-1","photoFaceGroupId":"group-2"},{"id":"face-3","photoId":"photo-1"},`+
				`{"id":"face-4","photoId":"photo-1","photoFaceGroupId":"group-4","contactId":"contact-own","contactName":"Ada Example","isOwnContact":true}]}`)
		case strings.Contains(asked.Query, "ListPhotos("):
			answer("ListPhotos", `[{"id":"photo-1","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":60,"placeName":"Example City","widthPixels":40,"heightPixels":30,"originalFilename":"IMG_1.JPG","photoProcessingState":"ready"}]`)
		default:
			_, _ = response.Write([]byte(`{"data":{}}`))
		}
	}))
	test.Cleanup(self.Close)
	return self
}

func boolText(value any) string {
	if isTrue, isBool := value.(bool); isBool && isTrue {
		return "true"
	}
	return "false"
}

func (self *photoServer) lastVariables() map[string]any { return self.variables[len(self.variables)-1] }

func TestPeopleListPrintsATableOrJSON(test *testing.T) {
	server := newPhotoFaceServer(test)
	_, table, err := server.run(test, "people", "list")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"group-1", "Example Aunt", "12", "9", "contact-1", "group-2", "hidden", "Ada Example (you)"} {
		if !strings.Contains(table, want) {
			test.Fatalf("the table lacks %q:\n%s", want, table)
		}
	}
	if strings.Contains(table, "Example Aunt (you)") {
		test.Fatalf("somebody else is not you:\n%s", table)
	}
	if server.lastVariables()["isHiddenIncluded"] != false {
		test.Fatalf("%v", server.lastVariables())
	}
	if _, _, err := server.run(test, "people", "list", "--hidden"); err != nil || server.lastVariables()["isHiddenIncluded"] != true {
		test.Fatalf("%v %v", err, server.lastVariables())
	}
	_, output, err := server.run(test, "people", "list", "--json")
	var groups []models.PhotoFaceGroup
	if err != nil || json.Unmarshal([]byte(output), &groups) != nil || len(groups) != 3 || groups[0].ContactName != "Example Aunt" || groups[0].FaceCount != 12 {
		test.Fatalf("%v: %s", err, output)
	}
	// The JSON carries who is the person themselves, for the one group only.
	if groups[0].IsOwnContact || groups[1].IsOwnContact || !groups[2].IsOwnContact || !strings.Contains(output, `"isOwnContact": true`) {
		test.Fatalf("%s", output)
	}
}

func TestPeopleShowSaysWhenThePersonIsYou(test *testing.T) {
	server := newPhotoFaceServer(test)
	_, table, err := server.run(test, "people", "show", "group-4")
	if err != nil || !strings.Contains(table, "Ada Example (you)") {
		test.Fatalf("%v:\n%s", err, table)
	}
	_, table, err = server.run(test, "people", "show", "group-1")
	if err != nil || strings.Contains(table, "(you)") {
		test.Fatalf("%v:\n%s", err, table)
	}
}

func TestPeopleShowListsThePersonAndTheirPhotos(test *testing.T) {
	server := newPhotoFaceServer(test)
	_, table, err := server.run(test, "people", "show", "group-1", "--limit", "7", "--continue-after", "photo-9")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(table, "Example Aunt") || !strings.Contains(table, "contact-1") || !strings.Contains(table, "photo-1") || !strings.Contains(table, "IMG_1.JPG") {
		test.Fatalf("the person and their photos:\n%s", table)
	}
	// The photos are the listing of the group.
	variables := server.lastVariables()
	if variables["faceGroupId"] != "group-1" || variables["pageSize"] != float64(7) || variables["beforePhotoId"] != "photo-9" {
		test.Fatalf("%v", variables)
	}
	_, output, err := server.run(test, "people", "show", "group-1", "--json")
	var shown struct {
		Person models.PhotoFaceGroup `json:"person"`
		Photos []models.Photo        `json:"photos"`
	}
	if err != nil || json.Unmarshal([]byte(output), &shown) != nil || shown.Person.ID != "group-1" || len(shown.Photos) != 1 {
		test.Fatalf("%v: %s", err, output)
	}
	for _, arguments := range [][]string{{"people", "show"}, {"people", "show", "group-1", "--limit", "501"}} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	// The same photos are reached from photo list, by group and by contact.
	if _, _, err := server.run(test, "list", "--face-group", "group-1", "--contact", "contact-1"); err != nil {
		test.Fatal(err)
	}
	if variables := server.lastVariables(); variables["faceGroupId"] != "group-1" || variables["contactId"] != "contact-1" {
		test.Fatalf("%v", variables)
	}
}

func TestPeopleNameUnnameMergeRemoveAndHideSendWhatTheyAreGiven(test *testing.T) {
	server := newPhotoFaceServer(test)
	output, _, err := server.run(test, "people", "name", "group-1", "contact-1")
	if err != nil || !strings.Contains(output, "named Example Aunt (group-1), 12 faces in 9 photos") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.lastVariables(); variables["photoFaceGroupId"] != "group-1" || variables["contactId"] != "contact-1" {
		test.Fatalf("%v", variables)
	}
	if strings.Contains(output, "merged with") || strings.Contains(output, "picture") {
		test.Fatalf("nothing was merged and no picture made: %s", output)
	}
	// A contact that named another group merges the two; a contact with no
	// picture is given a face.
	output, _, err = server.run(test, "people", "name", "group-merged", "contact-1")
	if err != nil || !strings.Contains(output, "named Example Aunt (group-1)") || !strings.Contains(output, "merged with Example Aunt") || !strings.Contains(output, "used a face as their picture") {
		test.Fatalf("%v: %s", err, output)
	}
	_, output, err = server.run(test, "people", "name", "group-merged", "contact-1", "--json")
	var mergedAnswer client.NamedPhotoFaceGroup
	if err != nil || json.Unmarshal([]byte(output), &mergedAnswer) != nil || mergedAnswer.MergedPhotoFaceGroupID != "group-merged" || !mergedAnswer.IsContactPictureSet || mergedAnswer.PhotoFaceGroup.ID != "group-1" {
		test.Fatalf("%v: %s", err, output)
	}
	if output, _, err := server.run(test, "people", "unname", "group-2"); err != nil || !strings.Contains(output, "unnamed a group of faces nobody has named (group-2)") {
		test.Fatalf("%v: %s", err, output)
	}
	if output, _, err := server.run(test, "people", "merge", "group-1", "group-2", "group-3"); err != nil || !strings.Contains(output, "merged into Example Aunt (group-1)") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.lastVariables(); !reflect.DeepEqual(variables["photoFaceGroupIds"], []any{"group-1", "group-2", "group-3"}) {
		test.Fatalf("%v", variables)
	}
	if output, _, err := server.run(test, "people", "remove", "face-9"); err != nil || !strings.Contains(output, "the face is now alone in") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.lastVariables(); variables["photoFaceId"] != "face-9" {
		test.Fatalf("%v", variables)
	}
	if output, _, err := server.run(test, "people", "hide", "group-2"); err != nil || !strings.Contains(output, "hid ") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.lastVariables(); variables["photoFaceGroupId"] != "group-2" || variables["isHidden"] != true {
		test.Fatalf("%v", variables)
	}
	if output, _, err := server.run(test, "people", "unhide", "group-2"); err != nil || !strings.Contains(output, "listing again") {
		test.Fatalf("%v: %s", err, output)
	}
	if variables := server.lastVariables(); variables["isHidden"] != false {
		test.Fatalf("%v", variables)
	}
	_, jsonOutput, err := server.run(test, "people", "name", "group-1", "contact-1", "--json")
	var namedAnswer client.NamedPhotoFaceGroup
	if err != nil || json.Unmarshal([]byte(jsonOutput), &namedAnswer) != nil || namedAnswer.PhotoFaceGroup.ContactName != "Example Aunt" {
		test.Fatalf("%v: %s", err, jsonOutput)
	}

	before := len(server.documents)
	tooMany := []string{"people", "merge"}
	for index := 0; index <= client.MaximumMergedPhotoFaceGroups; index++ {
		tooMany = append(tooMany, "group")
	}
	for _, arguments := range [][]string{
		{"people", "name"}, {"people", "name", "group-1"}, {"people", "name", "a", "b", "c"}, {"people", "unname"}, {"people", "merge", "group-1"},
		{"people", "remove"}, {"people", "hide"}, {"people", "unhide", "a", "b"}, tooMany,
	} {
		if _, _, err := server.run(test, arguments...); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
	if len(server.documents) != before {
		test.Fatal("something was sent for a call that was refused")
	}
}

func TestFacesOffDeletesWhatWasFoundSoItAsksFirst(test *testing.T) {
	server := newPhotoFaceServer(test)
	// Standard input is not a terminal here, so there is nobody to ask.
	before := len(server.documents)
	if _, _, err := server.run(test, "faces", "off"); err == nil || !strings.Contains(err.Error(), "--force") {
		test.Fatalf("%v", err)
	}
	if len(server.documents) != before {
		test.Fatal("faces were switched off without asking")
	}
	output, _, err := server.run(test, "faces", "off", "--force")
	if err != nil || !strings.Contains(output, "no longer finding faces") || server.lastVariables()["isEnabled"] != false {
		test.Fatalf("%v: %s %v", err, output, server.lastVariables())
	}
	output, _, err = server.run(test, "faces", "on")
	if err != nil || !strings.Contains(output, "finding faces in your photos") || server.lastVariables()["isEnabled"] != true {
		test.Fatalf("%v: %s %v", err, output, server.lastVariables())
	}
	_, jsonOutput, err := server.run(test, "faces", "on", "--json")
	if err != nil || !strings.Contains(jsonOutput, `"isPhotoFaceRecognitionEnabled": true`) {
		test.Fatalf("%v: %s", err, jsonOutput)
	}
	if _, _, err := server.run(test, "faces", "on", "extra"); err == nil {
		test.Fatal("an argument was accepted")
	}
}

func TestShowNamesThePeopleInAPhotoAndLibrarySaysWhetherFacesAreFound(test *testing.T) {
	server := newPhotoFaceServer(test)
	_, shown, err := server.run(test, "show", "photo-1")
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{"person:", "Example Aunt (group group-1, face face-1)", "not named (group group-2, face face-2)", "too small or too unsure to group (face face-3)", "Ada Example (you) (group group-4, face face-4)"} {
		if !strings.Contains(shown, want) {
			test.Fatalf("%q is not in:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "Example Aunt (you)") {
		test.Fatalf("somebody else is not you:\n%s", shown)
	}
	_, library, err := server.run(test, "library")
	if err != nil || !strings.Contains(library, "finding faces:") || !strings.Contains(library, "off (teanode photo faces on)") {
		test.Fatalf("%v:\n%s", err, library)
	}
}

// isPhotoFaceSurface says a subcommand of teanode photo or an operation of the
// photo tool is about the people in photos, which TestPhotoFaceParity checks
// where TestPhotoParity does not look.
func isPhotoFaceSurface(name string) bool {
	return strings.HasPrefix(name, "people") || strings.HasPrefix(name, "faces") || strings.HasPrefix(name, "person-")
}

// Everything a person does with the people in their photos is on every
// surface: the API, the command line and the photo tool. One table names each,
// and nothing exists on a surface that the table does not list.
func TestPhotoFaceParity(test *testing.T) {
	test.Parallel()
	table := map[string]struct {
		document       string
		subcommands    []string
		toolOperations []string
	}{
		"ListPhotoFaceGroups":      {client.DocumentListPhotoFaceGroups, []string{"people list"}, []string{"people"}},
		"ReadPhotoFaceGroup":       {client.DocumentReadPhotoFaceGroup, []string{"people show"}, []string{"person-photos"}},
		"NamePhotoFaceGroup":       {client.DocumentNamePhotoFaceGroup, []string{"people name"}, []string{"person-name"}},
		"UnnamePhotoFaceGroup":     {client.DocumentUnnamePhotoFaceGroup, []string{"people unname"}, []string{"person-unname"}},
		"MergePhotoFaceGroups":     {client.DocumentMergePhotoFaceGroups, []string{"people merge"}, []string{"person-merge"}},
		"RemovePhotoFaceFromGroup": {client.DocumentRemovePhotoFaceFromGroup, []string{"people remove"}, []string{"person-remove-face"}},
		"HidePhotoFaceGroup":       {client.DocumentHidePhotoFaceGroup, []string{"people hide", "people unhide"}, []string{"person-hide"}},
		"SetPhotoFaceRecognition":  {client.DocumentSetPhotoFaceRecognition, []string{"faces on", "faces off"}, []string{"faces"}},
	}
	// What the people operations add to the photo operations: the filters of
	// a listing, and the people of a photo.
	for name, document := range map[string]string{
		"faceGroupId": client.DocumentListPhotos, "contactId": client.DocumentCountPhotos, "faces": client.DocumentReadPhoto,
		"isPhotoFaceRecognitionEnabled": client.DocumentReadPhotoLibrary,
	} {
		if !strings.Contains(document, name) {
			test.Errorf("the document does not ask for %s:\n%s", name, document)
		}
	}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.PhotoFaceQuery](), reflect.TypeFor[apigraph.PhotoFaceMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}
	subcommands := map[string]bool{}
	for _, subcommand := range NewPhotoCommand().Commands {
		for _, nested := range subcommand.Commands {
			subcommands[subcommand.Name+" "+nested.Name] = true
		}
	}
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the agent has no photo tool")
	}
	properties, _ := photoTool.Parameters["properties"].(map[string]any)
	operation, _ := properties["operation"].(map[string]any)
	toolOperations := map[string]bool{}
	switch enum := operation["enum"].(type) {
	case []string:
		for _, name := range enum {
			toolOperations[name] = true
		}
	case []any:
		for _, name := range enum {
			toolOperations[name.(string)] = true
		}
	}

	listedSubcommands, listedToolOperations := map[string]bool{}, map[string]bool{}
	for name, entry := range table {
		if !operations[name] {
			test.Errorf("the API has no operation %s", name)
		}
		if !strings.Contains(entry.document, " "+name+"(") && !strings.Contains(entry.document, " "+name+" ") {
			test.Errorf("the document for %s does not call it:\n%s", name, entry.document)
		}
		for _, subcommand := range entry.subcommands {
			listedSubcommands[subcommand] = true
			if !subcommands[subcommand] {
				test.Errorf("teanode photo has no %s, which does %s", subcommand, name)
			}
		}
		for _, toolOperation := range entry.toolOperations {
			listedToolOperations[toolOperation] = true
			if !toolOperations[toolOperation] {
				test.Errorf("the photo tool has no %s, which does %s", toolOperation, name)
			}
		}
	}
	for name := range operations {
		if _, isListed := table[name]; !isListed {
			test.Errorf("the API's %s is on no other surface", name)
		}
	}
	for subcommand := range subcommands {
		if isPhotoFaceSurface(subcommand) && !listedSubcommands[subcommand] {
			test.Errorf("teanode photo %s does nothing the table lists", subcommand)
		}
	}
	for toolOperation := range toolOperations {
		if isPhotoFaceSurface(toolOperation) && !listedToolOperations[toolOperation] {
			test.Errorf("the photo tool's %s does nothing the table lists", toolOperation)
		}
	}
}
