package photo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeOperations answers the photo documents from a table, and remembers what
// it was asked, the way the account API would as the person.
type fakeOperations struct {
	// answers is the data of each operation, by the name the document opens.
	answers map[string]string
	// photos is what ReadPhoto answers for each photo, by its id; a photo
	// not here is not found.
	photos map[string]string

	// photoPlaces is what ReadPhotoPlace answers for a photo; a photo not in
	// it is answered null.
	photoPlaces map[string]string
	// refusals is the error each operation answers with, by name, as a
	// server does that refuses it.
	refusals  map[string]string
	documents []string
	variables []map[string]any
}

func (self *fakeOperations) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionPhotosUse}}
}

func (self *fakeOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
	for operationName, message := range self.refusals {
		if strings.Contains(document, operationName+"(") {
			return errors.New(message)
		}
	}
	if strings.Contains(document, "ReadPhotoPlace(") {
		answer, isKnown := self.photoPlaces[variables["photoId"].(string)]
		if !isKnown {
			answer = "null"
		}
		return json.Unmarshal([]byte(`{"ReadPhotoPlace":`+answer+`}`), answered)
	}
	if strings.Contains(document, "ReadPhoto(") {
		answer, isKnown := self.photos[variables["photoId"].(string)]
		if !isKnown {
			return client.ErrNotFound
		}
		return json.Unmarshal([]byte(`{"ReadPhoto":`+answer+`}`), answered)
	}
	for operationName, answer := range self.answers {
		if strings.Contains(document, operationName+"(") || strings.Contains(document, operationName+" ") || strings.HasSuffix(document, operationName+" }") {
			if answered == nil {
				return nil
			}
			return json.Unmarshal([]byte(`{"`+operationName+`":`+answer+`}`), answered)
		}
	}
	return nil
}

func (self *fakeOperations) asked(operationName string) []map[string]any {
	var found []map[string]any
	for index, document := range self.documents {
		if strings.Contains(document, operationName+"(") || strings.HasSuffix(document, operationName+" }") {
			found = append(found, self.variables[index])
		}
	}
	return found
}

// fakeRun is a turn with a person, an agent, a conversation, a store and a
// database, whose account API is the fake.
type fakeRun struct {
	tools.Run
	operations   *fakeOperations
	owner        *models.User
	agent        *models.Agent
	conversation *models.AgentConversation
	database     db.Database
	store        storage.Storage
}

func (self *fakeRun) Operations() tools.Operations            { return self.operations }
func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Storage() storage.Storage                { return self.store }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }

func (self *fakeRun) call(arguments map[string]any) (*tools.Result, error) {
	encoded, _ := json.Marshal(arguments)
	return run(tools.WithRun(context.Background(), self), &tools.Call{ID: "fixture-call", Arguments: encoded})
}

// newRun is a run over a real database and store, for the operations that
// keep attachments, with the fake API in front of the library.
func newRun(test *testing.T, answers map[string]string) *fakeRun {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = store.Close() })
	current := &fakeRun{operations: &fakeOperations{answers: answers}, database: database, store: store}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		if current.owner, err = transaction.CreateUser(&models.User{Username: "photo-tool-owner", Name: "Alice Example", Timezone: "UTC"}); err != nil {
			test.Fatal(err)
		}
		if current.agent, err = transaction.CreateAgent(&models.Agent{UserID: current.owner.ID, Enabled: true, Name: "Fixture assistant"}); err != nil {
			test.Fatal(err)
		}
		if current.conversation, err = transaction.CreateAgentConversation(&models.AgentConversation{AgentID: current.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			test.Fatal(err)
		}
	})
	return current
}

const (
	pictureRow = `{"id":"photo-one","photoKind":"picture","takenAt":"2025-03-04T10:30:00Z","takenAtOffsetMinutes":60,"placeName":"Example City, Exampleland","originalFilename":"IMG_0001.JPG","photoProcessingState":"ready","largePreviewUrl":"/x","smallPreviewUrl":"/y","isFavorite":true}`
	videoRow   = `{"id":"photo-two","photoKind":"video","durationMS":12400,"photoProcessingState":"ready","largePreviewUrl":"/x"}`
	waitingRow = `{"id":"photo-three","photoKind":"picture","photoProcessingState":"waiting"}`
)

// A model that fills in every field it is shown sends false, zero and the
// first choice of each list for the ones it has no view on: none of them
// narrows the listing.
func TestAListingWithEveryFieldFilledInNarrowsOnlyByWhatNamesSomething(test *testing.T) {
	current := newRun(test, map[string]string{"ListPhotos": `[` + pictureRow + `]`})
	if _, err := current.call(map[string]any{
		"operation": "person-photos", "face_group_id": "group-one", "page_size": 6, "offset": 0,
		"is_favorite": false, "is_trashed": false, "photo_kind": "any", "search_text": "", "taken_after": "", "taken_before": "",
		"near_latitude": 0, "near_longitude": 0, "near_distance_kilometers": 0, "share_level": "view", "source": "conversation",
	}); err != nil {
		test.Fatal(err)
	}
	asked := current.operations.asked("ListPhotos")
	if len(asked) != 1 || asked[0]["faceGroupId"] != "group-one" || asked[0]["isFavorite"] != nil || asked[0]["photoKind"] != nil ||
		asked[0]["nearLatitude"] != nil || asked[0]["nearDistanceKilometers"] != nil {
		test.Fatalf("the listing asked %v", asked)
	}
	// A distance with a point is a place to narrow by, and true a flag.
	if _, err := current.call(map[string]any{"operation": "list", "is_favorite": true, "photo_kind": "video", "near_latitude": 10, "near_longitude": 20, "near_distance_kilometers": 5}); err != nil {
		test.Fatal(err)
	}
	asked = current.operations.asked("ListPhotos")
	if len(asked) != 2 || asked[1]["isFavorite"] != true || asked[1]["photoKind"] != "video" || asked[1]["nearDistanceKilometers"] != 5.0 {
		test.Fatalf("the listing asked %v", asked[1])
	}
}

func TestListAnswersCompactRowsInThePersonsZoneAndSaysHowToPage(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListPhotos":  `[` + pictureRow + `,` + videoRow + `,` + waitingRow + `]`,
		"CountPhotos": `40`,
	})
	result, err := current.call(map[string]any{"operation": "list", "page_size": 3, "search_text": "Example", "taken_after": "2025-01-01", "is_favorite": false})
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted {
		test.Fatal("photo metadata is somebody else's words")
	}
	var answer struct {
		Photos []struct {
			PhotoID         string `json:"photo_id"`
			PhotoKind       string `json:"photo_kind"`
			TakenAt         string `json:"taken_at"`
			PlaceName       string `json:"place_name"`
			DurationSeconds int    `json:"duration_seconds"`
			HasDescription  *bool  `json:"has_description"`
			PreviewState    string `json:"preview_state"`
		} `json:"photos"`
		MatchingPhotoCount int    `json:"matching_photo_count"`
		NextOffset         int    `json:"next_offset"`
		More               string `json:"more"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.Photos) != 3 || answer.Photos[0].TakenAt != "2025-03-04 11:30 (UTC+01:00)" || answer.Photos[0].PlaceName != "Example City, Exampleland" || answer.Photos[0].HasDescription == nil || *answer.Photos[0].HasDescription {
		test.Fatalf("%+v", answer.Photos)
	}
	if answer.Photos[1].DurationSeconds != 12 || answer.Photos[1].PhotoKind != "video" || answer.Photos[2].PreviewState != "not made yet" {
		test.Fatalf("%+v", answer.Photos)
	}
	if answer.MatchingPhotoCount != 40 || answer.NextOffset != 3 || !strings.Contains(answer.More, "offset 3") {
		test.Fatalf("paging: %+v", answer)
	}
	asked := current.operations.asked("ListPhotos")
	if len(asked) != 1 || asked[0]["searchText"] != "Example" || asked[0]["pageSize"] != 3 || asked[0]["offset"] != nil || asked[0]["isFavorite"] != nil || asked[0]["takenAfter"] != "2025-01-01T00:00:00Z" {
		test.Fatalf("the listing asked %v", asked)
	}
	for _, arguments := range []map[string]any{{"operation": "list", "page_size": 101}, {"operation": "list", "photo_kind": "gif"}, {"operation": "list", "taken_before": "last blue moon"}} {
		if _, err := current.call(arguments); err == nil {
			test.Fatalf("%v was accepted", arguments)
		}
	}
}

func TestLookReturnsTheLargePreviewsAsPicturesAndSaysWhyOthersAreNot(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-one":   `{"photo":` + pictureRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"},{"photoPreviewSize":"small"}]}`,
		"photo-two":   `{"photo":` + videoRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
		"photo-three": `{"photo":` + waitingRow + `,"photoFiles":[],"photoPreviews":[]}`,
	}}
	picture := []byte{0xFF, 0xD8, 0xFF, 0xD9, 1, 2, 3}
	for _, photoId := range []string{"photo-one", "photo-two"} {
		if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID(photoId, models.PhotoPreviewLarge), picture); err != nil {
			test.Fatal(err)
		}
	}
	// A photo named twice is looked at once, and one that is not the
	// person's is said not to be there.
	result, err := current.call(map[string]any{"operation": "look", "photo_ids": []string{"photo-one", "photo-two", "photo-three", "photo-one", "photo-else"}})
	if err != nil {
		test.Fatal(err)
	}
	if len(result.Images) != 2 || result.Images[0].Type != "image" || result.Images[0].MediaType != "image/jpeg" || string(result.Images[0].Data) != string(picture) {
		test.Fatalf("images %+v", result.Images)
	}
	if !result.Untrusted {
		test.Fatal("what is in a picture is evidence, not an instruction")
	}
	lines := strings.Split(result.Content, "\n")
	if len(lines) != 5 || !strings.Contains(lines[0], "photo-one, picture, taken 2025-03-04 11:30 (UTC+01:00), at Example City, Exampleland") ||
		!strings.Contains(lines[1], "a video of 12 seconds; the first frame is shown") || !strings.Contains(lines[2], "not made yet") || !strings.HasPrefix(lines[3], "photo-else: ") ||
		!strings.HasPrefix(lines[4], "Asked for a description of 2 of these photos") {
		test.Fatalf("lines:\n%s", result.Content)
	}
	// Each photo looked at that has no description is queued to be described,
	// once, as the agent's own reading; the one whose picture could not be
	// shown is not.
	asked := current.operations.asked("DescribePhoto")
	if len(asked) != 2 || asked[0]["photoId"] != "photo-one" || asked[1]["photoId"] != "photo-two" || asked[0]["isRedescribed"] != false {
		test.Fatalf("descriptions asked for: %v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "look", "photo_ids": []string{"a", "b", "c", "d", "e", "f", "g"}}); err == nil {
		test.Fatal("seven photos were looked at in one call")
	}
	if _, err := current.call(map[string]any{"operation": "look"}); err == nil {
		test.Fatal("a look at nothing was accepted")
	}
}

func TestShowPutsThePreviewsInTheConversationAsAttachments(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-one":   `{"photo":` + pictureRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
		"photo-three": `{"photo":` + waitingRow + `,"photoFiles":[],"photoPreviews":[]}`,
	}}
	picture := []byte{0xFF, 0xD8, 0xFF, 0xD9, 9}
	if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID("photo-one", models.PhotoPreviewLarge), picture); err != nil {
		test.Fatal(err)
	}
	result, err := current.call(map[string]any{"operation": "show", "photo_ids": []string{"photo-one", "photo-three"}})
	if err != nil {
		test.Fatal(err)
	}
	var answer struct {
		Files []struct {
			PhotoID      string `json:"photo_id"`
			AttachmentID string `json:"attachment_id"`
			Name         string `json:"name"`
			ContentType  string `json:"content_type"`
			Size         int    `json:"size"`
			URL          string `json:"url"`
		} `json:"files"`
		NotShown []string `json:"not_shown"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.Files) != 1 || len(answer.NotShown) != 1 || !strings.Contains(answer.NotShown[0], "not made yet") {
		test.Fatalf("%s", result.Content)
	}
	file := answer.Files[0]
	// The shape the dashboard and the phone draw a handed-over file from.
	if file.PhotoID != "photo-one" || file.Name != "IMG_0001.jpg" || file.ContentType != "image/jpeg" || file.Size != len(picture) || file.URL != "/api/v1/agent/attachments/"+file.AttachmentID {
		test.Fatalf("%+v", file)
	}
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, current.database, func(transaction db.Transaction) {
		var err error
		if attachment, err = transaction.GetAgentAttachment(file.AttachmentID); err != nil {
			test.Fatal(err)
		}
	})
	if attachment == nil || attachment.ConversationID != current.conversation.ID || attachment.AgentID != current.agent.ID || attachment.MessageID != "shared" {
		test.Fatalf("%+v", attachment)
	}
	stored, err := current.store.GetFile(test.Context(), attachment.ID)
	if err != nil || string(stored) != string(picture) {
		test.Fatalf("the attachment's bytes: %v %v", stored, err)
	}
}

func TestFavoriteTrashRestoreAndSaveUseTheSharedOperations(test *testing.T) {
	current := newRun(test, nil)
	operations := &fakeOperations{answers: map[string]string{
		"UpdatePhoto":             `{"id":"photo-one","isFavorite":false}`,
		"TrashPhotos":             `2`,
		"RestorePhotos":           `1`,
		"SavePhotoFromAttachment": `{"id":"photo-new","photoKind":"picture","originalFilename":"chart.png"}`,
	}}
	current.operations = operations
	if result, err := current.call(map[string]any{"operation": "favorite", "photo_id": "photo-one", "is_favorite": false}); err != nil || !strings.Contains(result.Content, "not a favorite") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("UpdatePhoto"); len(asked) != 1 || asked[0]["isFavorite"] != false || asked[0]["photoId"] != "photo-one" {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "favorite", "photo_id": "photo-one"}); err != nil {
		test.Fatal(err)
	}
	if asked := operations.asked("UpdatePhoto"); asked[1]["isFavorite"] != true {
		test.Fatalf("a favorite with no word on it is not one: %v", asked[1])
	}
	if result, err := current.call(map[string]any{"operation": "trash", "photo_ids": []string{"a", "b"}}); err != nil || !strings.Contains(result.Content, "2 of 2 photos moved to the Trash") {
		test.Fatalf("%v %v", result, err)
	}
	if result, err := current.call(map[string]any{"operation": "restore", "photo_ids": []string{"a"}}); err != nil || !strings.Contains(result.Content, "1 of 1 photos restored from the Trash") {
		test.Fatalf("%v %v", result, err)
	}
	if _, err := current.call(map[string]any{"operation": "trash"}); err == nil {
		test.Fatal("trashing nothing was accepted")
	}
	// Retrying makes the previews of photos again; it is a write, and it names
	// the photos the way trash does.
	operations.answers["RetryPhotoProcessing"] = "1"
	if result, err := current.call(map[string]any{"operation": "retry", "photo_ids": []string{"a", "b"}}); err != nil || !strings.Contains(result.Content, "1 of 2 photos queued to have their thumbnails made again") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("RetryPhotoProcessing"); len(asked) != 1 || len(asked[0]["photoIds"].([]string)) != 2 {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "retry"}); err == nil {
		test.Fatal("retrying nothing was accepted")
	}

	// A file of the conversation is kept as a photo through the API's own
	// operation; one of another conversation is not found.
	var attachment, elsewhere *models.AgentAttachment
	dbtest.RunTransactionOn(test, current.database, func(transaction db.Transaction) {
		other, err := transaction.CreateAgentConversation(&models.AgentConversation{AgentID: current.agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
		if attachment, err = transaction.CreateAgentAttachment(&models.AgentAttachment{AgentID: current.agent.ID, ConversationID: current.conversation.ID, Name: "chart.png", ContentType: "image/png", Size: 4}); err != nil {
			test.Fatal(err)
		}
		if elsewhere, err = transaction.CreateAgentAttachment(&models.AgentAttachment{AgentID: current.agent.ID, ConversationID: other.ID, Name: "other.png", ContentType: "image/png", Size: 4}); err != nil {
			test.Fatal(err)
		}
	})
	if result, err := current.call(map[string]any{"operation": "save", "source": "conversation", "attachment_id": attachment.ID}); err != nil || !strings.Contains(result.Content, "photo-new") {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("SavePhotoFromAttachment"); len(asked) != 1 || asked[0]["agentAttachmentId"] != attachment.ID {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "save", "source": "conversation", "attachment_id": elsewhere.ID}); err == nil || len(operations.asked("SavePhotoFromAttachment")) != 1 {
		test.Fatal("a file of another conversation was saved", err)
	}
	if _, err := current.call(map[string]any{"operation": "delete_forever"}); err == nil {
		test.Fatal("an operation nobody named was accepted")
	}
}

// Deleting forever is what teanode photo delete and empty-trash do, with the
// same words, and the person is asked first because neither can be undone.
func TestDeleteAndEmptyTrashDeleteForeverAndAskFirst(test *testing.T) {
	current := newRun(test, nil)
	operations := &fakeOperations{answers: map[string]string{"DeletePhotos": `1`, "EmptyPhotoTrash": `3`}}
	current.operations = operations
	if result, err := current.call(map[string]any{"operation": "delete", "photo_ids": []string{"a", "b"}}); err != nil || !strings.Contains(result.Content, "1 of 2 photos deleted forever; the others were not in the Trash and were left alone") {
		test.Fatalf("%v %v", result, err)
	}
	// When every photo went there is nothing to explain.
	operations.answers["DeletePhotos"] = `2`
	if result, err := current.call(map[string]any{"operation": "delete", "photo_ids": []string{"a", "b"}}); err != nil || result.Content != "2 of 2 photos deleted forever" {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("DeletePhotos"); len(asked) != 2 || len(asked[0]["photoIds"].([]string)) != 2 {
		test.Fatalf("%v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "delete"}); err == nil {
		test.Fatal("deleting nothing was accepted")
	}
	if result, err := current.call(map[string]any{"operation": "empty-trash"}); err != nil || !strings.Contains(result.Content, "deleted 3 photos forever") {
		test.Fatalf("%v %v", result, err)
	}
	operations.answers["EmptyPhotoTrash"] = `1`
	if result, err := current.call(map[string]any{"operation": "empty-trash"}); err != nil || result.Content != "deleted 1 photo forever" {
		test.Fatalf("%v %v", result, err)
	}
	if asked := operations.asked("EmptyPhotoTrash"); len(asked) != 2 {
		test.Fatalf("%v", asked)
	}
	for operation, want := range map[string]string{
		"delete":      "Delete 2 photos forever; only photos in the Trash are deleted",
		"empty-trash": "Empty the Trash: delete everything in it forever",
	} {
		encoded, _ := json.Marshal(map[string]any{"operation": operation, "photo_ids": []string{"a", "b"}})
		if risk := riskOf(encoded); risk != tools.RiskDestructive {
			test.Errorf("%s is %s", operation, risk)
		}
		var asked arguments
		if err := json.Unmarshal(encoded, &asked); err != nil {
			test.Fatal(err)
		}
		if line := previewOf(asked); line != want {
			test.Errorf("%s: the card says %q", operation, line)
		}
	}
}

func TestEachOperationHasItsRisk(test *testing.T) {
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the photo tool is not registered")
	}
	for operation, want := range map[string]tools.Risk{
		"library": tools.RiskRead, "list": tools.RiskRead, "read": tools.RiskRead, "look": tools.RiskRead, "show": tools.RiskRead,
		"favorite": tools.RiskWrite, "set-date": tools.RiskWrite, "set-location": tools.RiskWrite, "remove-location": tools.RiskDestructive, "search-places": tools.RiskRead, "restore": tools.RiskWrite, "retry": tools.RiskWrite, "save": tools.RiskWrite, "trash": tools.RiskDestructive, "delete": tools.RiskDestructive, "empty-trash": tools.RiskDestructive, "person-read": tools.RiskRead,
		"album-cover": tools.RiskWrite, "save-shared": tools.RiskWrite, "album-share": tools.RiskGranting, "album-unshare": tools.RiskGranting, "album-shares": tools.RiskRead, "person-unname": tools.RiskWrite, "person-hide": tools.RiskWrite, "person-remove-face": tools.RiskWrite,
	} {
		arguments, _ := json.Marshal(map[string]any{"operation": operation})
		if got := photoTool.RiskFor(arguments); got != want {
			test.Errorf("%s is %s, want %s", operation, got, want)
		}
		if line := photoTool.PreviewLine(test.Context(), arguments); line == "" || strings.Contains(line, "{") {
			test.Errorf("%s has no sentence for its card: %q", operation, line)
		}
	}
	removing, _ := json.Marshal(map[string]any{"operation": "remove-location", "photo_ids": []string{"a"}})
	setting, _ := json.Marshal(map[string]any{"operation": "set-location", "photo_ids": []string{"a"}, "place": "Lisbon"})
	if !tools.NeedsConfirmation(photoTool, removing, nil, nil) || tools.NeedsConfirmation(photoTool, setting, nil, nil) {
		test.Error("removing a location, which cannot be undone, must ask the person, and setting one need not")
	}
	if photoTool.RiskFor(json.RawMessage(`not json`)) != tools.RiskDestructive {
		test.Error("a call that cannot be read is not held to a card")
	}
	if len(photoTool.Permissions) != 1 || photoTool.Permissions[0] != models.PermissionPhotosUse {
		test.Errorf("the tool is offered to %v", photoTool.Permissions)
	}
	if services := tools.RequiredServices(photoTool); len(services) != 1 || services[0] != models.ServicePhotos {
		test.Errorf("the tool needs the services %v", services)
	}
	properties, _ := photoTool.Parameters["properties"].(map[string]any)
	operation, _ := properties["operation"].(map[string]any)
	if enum, _ := operation["enum"].([]string); len(enum) != 40 {
		test.Errorf("operations %v", enum)
	}
}

func TestLibraryAnswersCountsAndLimits(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{answers: map[string]string{"ReadPhotoLibrary": `{"photoCount":5,"videoCount":2,"favoriteCount":1,"trashedCount":3,"usedBytes":1000,"maximumFileBytes":2000,"maximumAccountBytes":0,"trashRetentionDays":30}`}}
	result, err := current.call(map[string]any{"operation": "library"})
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{`"photo_count":7`, `"video_count":2`, `"photos":"7 (2 videos)"`, `"trash_kept_days":30`, "found only by the rest", `"space_used":"1000 B · No limit"`, `"largest_file":"2 KB"`} {
		if !strings.Contains(result.Content, want) {
			test.Errorf("the library answer lacks %s: %s", want, result.Content)
		}
	}
	if strings.Contains(result.Content, "total_count") {
		test.Errorf("the library answer counts photos under the name the paged lists use for other things: %s", result.Content)
	}
	var _ client.Executor = current.operations
}

func TestAPhotosDateIsTheClockWhereItWasTakenWithItsOffset(test *testing.T) {
	// 08:00 on April 4 where the offset is +09:00 is the evening of April 3
	// for a person west of it, and the photo must still be April 4.
	offset := 9 * 60
	taken := time.Date(2025, 4, 3, 23, 0, 0, 0, time.UTC)
	for _, check := range []struct {
		name  string
		photo *models.Photo
		want  string
	}{
		{"offset known", &models.Photo{TakenAt: &taken, TakenAtOffsetMinutes: &offset}, "2025-04-04 08:00 (UTC+09:00)"},
		{"offset west of UTC", &models.Photo{TakenAt: &taken, TakenAtOffsetMinutes: new(-210)}, "2025-04-03 19:30 (UTC-03:30)"},
		{"offset zero", &models.Photo{TakenAt: &taken, TakenAtOffsetMinutes: new(0)}, "2025-04-03 23:00 (UTC+00:00)"},
		// A photo not processed yet has no offset: it is on UTC, and says so,
		// whatever zone the person is in.
		{"offset unknown", &models.Photo{TakenAt: &taken}, "2025-04-03 23:00 (UTC)"},
		{"no date", &models.Photo{}, ""},
	} {
		if got := takenAtText(check.photo); got != check.want {
			test.Errorf("%s: %q, want %q", check.name, got, check.want)
		}
	}
	if got := describePhoto(&models.Photo{ID: "photo-x", PhotoKind: models.PhotoKindPicture, TakenAt: &taken, TakenAtOffsetMinutes: &offset}); !strings.Contains(got, "taken 2025-04-04 08:00 (UTC+09:00)") {
		test.Errorf("the line that goes with a shown photo says %q", got)
	}
}

const describedRow = `{"id":"photo-described","photoKind":"picture","photoProcessingState":"ready","largePreviewUrl":"/x","photoDescription":"A red lighthouse on a rocky point. Waves break at its base.","descriptionVisibleText":"HARBOR ENTRANCE","describedAt":"2025-03-05T08:00:00Z"}`

func TestLookDoesNotAskAgainForAPhotoThatIsDescribedOrAlreadyBeingDescribed(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-described": `{"photo":` + describedRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
		"photo-pending":   `{"photo":{"id":"photo-pending","photoKind":"picture","photoProcessingState":"ready","isDescriptionRequested":true},"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`,
	}}
	for _, photoId := range []string{"photo-described", "photo-pending"} {
		if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID(photoId, models.PhotoPreviewLarge), []byte{0xFF, 0xD8, 0xFF, 0xD9}); err != nil {
			test.Fatal(err)
		}
	}
	result, err := current.call(map[string]any{"operation": "look", "photo_ids": []string{"photo-described", "photo-pending"}})
	if err != nil {
		test.Fatal(err)
	}
	if asked := current.operations.asked("DescribePhoto"); len(asked) != 0 {
		test.Fatalf("a description was asked for again: %v", asked)
	}
	if !strings.Contains(result.Content, "it was described as: A red lighthouse on a rocky point.") || strings.Contains(result.Content, "Asked for a description") {
		test.Fatalf("lines:\n%s", result.Content)
	}
}

func TestLookSaysSoWhenDescriptionsCannotBeAskedFor(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{
		photos:   map[string]string{"photo-one": `{"photo":` + pictureRow + `,"photoFiles":[],"photoPreviews":[{"photoPreviewSize":"large"}]}`},
		refusals: map[string]string{"DescribePhoto": "Describing photos needs the assistant to be on."},
	}
	if err := current.store.PutFile(test.Context(), models.PhotoPreviewFileID("photo-one", models.PhotoPreviewLarge), []byte{0xFF, 0xD8, 0xFF, 0xD9}); err != nil {
		test.Fatal(err)
	}
	result, err := current.call(map[string]any{"operation": "look", "photo_ids": []string{"photo-one"}})
	if err != nil || len(result.Images) != 1 {
		test.Fatalf("a refused description stopped the look: %v %+v", err, result)
	}
	if !strings.Contains(result.Content, "could not be queued to be described: Describing photos needs the assistant to be on.") {
		test.Fatalf("lines:\n%s", result.Content)
	}
}

func TestDescribeAsksForDescriptionsWithoutWaitingAndLeavesDescribedPhotosAlone(test *testing.T) {
	current := newRun(test, map[string]string{"DescribePhoto": `{"id":"photo-one","photoKind":"picture","isDescriptionRequested":true}`})
	result, err := current.call(map[string]any{"operation": "describe", "photo_id": "photo-one", "is_redescribed": true})
	if err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(result.Content, "photo-one: being described") || !result.Untrusted {
		test.Fatalf("%s", result.Content)
	}
	asked := current.operations.asked("DescribePhoto")
	if len(asked) != 1 || asked[0]["photoId"] != "photo-one" || asked[0]["isRedescribed"] != true {
		test.Fatalf("asked %v", asked)
	}
	current = newRun(test, map[string]string{"DescribePhoto": describedRow})
	result, err = current.call(map[string]any{"operation": "describe", "photo_ids": []string{"photo-described"}})
	if err != nil || !strings.Contains(result.Content, "photo-described: described as: A red lighthouse") {
		test.Fatalf("%v %v", err, result)
	}
	if asked := current.operations.asked("DescribePhoto"); asked[0]["isRedescribed"] != false {
		test.Fatalf("a new description was asked for without being asked: %v", asked)
	}
	if _, err := current.call(map[string]any{"operation": "describe"}); err == nil {
		test.Fatal("describing nothing was accepted")
	}
	if _, err := current.call(map[string]any{"operation": "describe", "photo_ids": []string{"a", "b", "c", "d", "e", "f", "g"}}); err == nil {
		test.Fatal("seven photos were described in one call")
	}
}

func TestListShowsFirstSentencesAndHowMuchOfARangeIsUndescribed(test *testing.T) {
	current := newRun(test, map[string]string{
		"ListPhotos":             `[` + describedRow + `,` + pictureRow + `]`,
		"CountUndescribedPhotos": `37`,
	})
	result, err := current.call(map[string]any{"operation": "list", "search_text": "lighthouse", "taken_after": "2025-03-01", "taken_before": "2025-04-01"})
	if err != nil {
		test.Fatal(err)
	}
	var answer struct {
		Photos []struct {
			PhotoID                  string `json:"photo_id"`
			HasDescription           bool   `json:"has_description"`
			DescriptionFirstSentence string `json:"description_first_sentence"`
		} `json:"photos"`
		UndescribedPhotoCount int    `json:"undescribed_photo_count"`
		SearchNote            string `json:"search_note"`
	}
	if err := json.Unmarshal([]byte(result.Content), &answer); err != nil {
		test.Fatal(err)
	}
	if len(answer.Photos) != 2 || !answer.Photos[0].HasDescription || answer.Photos[0].DescriptionFirstSentence != "A red lighthouse on a rocky point." || answer.Photos[1].HasDescription || answer.Photos[1].DescriptionFirstSentence != "" {
		test.Fatalf("%+v", answer.Photos)
	}
	if answer.UndescribedPhotoCount != 37 || !strings.Contains(answer.SearchNote, "found only by the rest") {
		test.Fatalf("%+v", answer)
	}
	// The count is of the range, so the words of the search are not asked about it.
	asked := current.operations.asked("CountUndescribedPhotos")
	if len(asked) != 1 || asked[0]["takenAfter"] != "2025-03-01T00:00:00Z" {
		test.Fatalf("asked %v", asked)
	}

	// With no words there is no search to be blind to undescribed photos, and
	// with no range the count would be of the whole library.
	for _, arguments := range []map[string]any{{"operation": "list", "taken_after": "2025-03-01"}, {"operation": "list", "search_text": "lighthouse"}} {
		current = newRun(test, map[string]string{"ListPhotos": `[` + pictureRow + `]`, "CountUndescribedPhotos": `5`})
		result, err := current.call(arguments)
		if err != nil || strings.Contains(result.Content, "undescribed_photo_count") {
			test.Fatalf("%v: %v %v", arguments, err, result)
		}
	}
}

func TestReadGivesTheDescriptionAndTheWordsReadInThePicture(test *testing.T) {
	current := newRun(test, nil)
	current.operations = &fakeOperations{photos: map[string]string{
		"photo-described": `{"photo":` + describedRow + `,"photoFiles":[],"photoPreviews":[],"albums":[]}`,
		"photo-pending":   `{"photo":{"id":"photo-pending","photoKind":"picture","isDescriptionRequested":true},"photoFiles":[],"photoPreviews":[],"albums":[]}`,
	}}
	result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-described"})
	if err != nil {
		test.Fatal(err)
	}
	for _, want := range []string{`"has_description":true`, `"description":"A red lighthouse on a rocky point. Waves break at its base."`, `"text_in_the_picture":"HARBOR ENTRANCE"`} {
		if !strings.Contains(result.Content, want) {
			test.Errorf("read lacks %s: %s", want, result.Content)
		}
	}
	result, err = current.call(map[string]any{"operation": "read", "photo_id": "photo-pending"})
	if err != nil || !strings.Contains(result.Content, `"description_state":"on its way"`) || !strings.Contains(result.Content, `"has_description":false`) {
		test.Fatalf("%v %v", err, result)
	}
}

func TestTheFirstSentenceStopsAtTheEndOfASentenceAndNotInsideANumber(test *testing.T) {
	for text, want := range map[string]string{
		"": "", "One. Two.": "One.", "About 3.5 meters tall. More": "About 3.5 meters tall.", "No ending": "No ending",
		"赤い灯台。波が打ち寄せる。": "赤い灯台。", "Really? Yes": "Really?",
	} {
		if got := firstSentenceOf(text); got != want {
			test.Errorf("%q gives %q, want %q", text, got, want)
		}
	}
	if long := firstSentenceOf(strings.Repeat("word ", 100)); len([]rune(long)) != maximumFirstSentenceCharacters+1 || !strings.HasSuffix(long, "…") {
		test.Errorf("a sentence without an end is cut: %d", len([]rune(long)))
	}
}

// read says where a photo was taken more finely than the city: the person's own
// place, else the street, and nothing more when the server knows neither.
func TestReadGivesTheOwnPlaceOrTheStreetBesideTheCity(test *testing.T) {
	current := newRun(test, nil)
	const photoRow = `{"photo":{"id":"%s","photoKind":"picture","latitude":10.5,"longitude":20.25,"placeName":"Harbor City, Exampleland"},"photoFiles":[],"photoPreviews":[],"albums":[]}`
	current.operations = &fakeOperations{
		photos: map[string]string{
			"photo-own":    fmt.Sprintf(photoRow, "photo-own"),
			"photo-street": fmt.Sprintf(photoRow, "photo-street"),
			"photo-city":   fmt.Sprintf(photoRow, "photo-city"),
		},
		photoPlaces: map[string]string{
			"photo-own":    `{"locationPlace":{"id":"place-1","placeName":"Fixture Home","placeLabel":"home"},"addressText":""}`,
			"photo-street": `{"locationPlace":null,"addressText":"1 Fixture Road, Harbor City"}`,
		},
	}
	result, err := current.call(map[string]any{"operation": "read", "photo_id": "photo-own"})
	if err != nil || !strings.Contains(result.Content, `"location_place":{"id":"place-1","place_label":"home","place_name":"Fixture Home"}`) || strings.Contains(result.Content, "address_text") {
		test.Fatalf("%v %v", err, result)
	}
	result, err = current.call(map[string]any{"operation": "read", "photo_id": "photo-street"})
	if err != nil || !strings.Contains(result.Content, `"address_text":"1 Fixture Road, Harbor City"`) || strings.Contains(result.Content, "location_place") {
		test.Fatalf("%v %v", err, result)
	}
	result, err = current.call(map[string]any{"operation": "read", "photo_id": "photo-city"})
	if err != nil || strings.Contains(result.Content, "address_text") || strings.Contains(result.Content, "location_place") || !strings.Contains(result.Content, `"place_name":"Harbor City, Exampleland"`) {
		test.Fatalf("%v %v", err, result)
	}
}
