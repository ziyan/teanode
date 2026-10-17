package photo

import (
	"context"
	"encoding/json"
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
	photos    map[string]string
	documents []string
	variables []map[string]any
}

func (self *fakeOperations) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionPhotosUse}}
}

func (self *fakeOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	self.documents = append(self.documents, document)
	self.variables = append(self.variables, variables)
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
	if answer.MatchingPhotoCount != 40 || !strings.Contains(answer.More, "before_photo_id photo-three") {
		test.Fatalf("paging: %+v", answer)
	}
	asked := current.operations.asked("ListPhotos")
	if len(asked) != 1 || asked[0]["searchText"] != "Example" || asked[0]["pageSize"] != 3 || asked[0]["isFavorite"] != false || asked[0]["takenAfter"] != "2025-01-01T00:00:00Z" {
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
	if len(lines) != 4 || !strings.Contains(lines[0], "photo-one, picture, taken 2025-03-04 11:30 (UTC+01:00), at Example City, Exampleland") ||
		!strings.Contains(lines[1], "a video of 12 seconds; the first frame is shown") || !strings.Contains(lines[2], "not made yet") || !strings.HasPrefix(lines[3], "photo-else: ") {
		test.Fatalf("lines:\n%s", result.Content)
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
	if result, err := current.call(map[string]any{"operation": "restore", "photo_ids": []string{"a"}}); err != nil || !strings.Contains(result.Content, "1 of 1 photos taken out of the Trash") {
		test.Fatalf("%v %v", result, err)
	}
	if _, err := current.call(map[string]any{"operation": "trash"}); err == nil {
		test.Fatal("trashing nothing was accepted")
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
		test.Fatal("the agent can delete photos forever")
	}
}

func TestEachOperationHasItsRisk(test *testing.T) {
	photoTool := tools.Build().Get("photo")
	if photoTool == nil {
		test.Fatal("the photo tool is not registered")
	}
	for operation, want := range map[string]tools.Risk{
		"library": tools.RiskRead, "list": tools.RiskRead, "read": tools.RiskRead, "look": tools.RiskRead, "show": tools.RiskRead,
		"favorite": tools.RiskWrite, "restore": tools.RiskWrite, "save": tools.RiskWrite, "trash": tools.RiskDestructive,
	} {
		arguments, _ := json.Marshal(map[string]any{"operation": operation})
		if got := photoTool.RiskFor(arguments); got != want {
			test.Errorf("%s is %s, want %s", operation, got, want)
		}
		if line := photoTool.PreviewLine(test.Context(), arguments); line == "" || strings.Contains(line, "{") {
			test.Errorf("%s has no sentence for its card: %q", operation, line)
		}
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
	if enum, _ := operation["enum"].([]string); len(enum) != 17 {
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
	for _, want := range []string{`"photo_count":5`, `"video_count":2`, `"trash_kept_days":30`, "does not know what is in a picture", `"space_used":"1000 B · No limit"`, `"largest_file":"2 KB"`} {
		if !strings.Contains(result.Content, want) {
			test.Errorf("the library answer lacks %s: %s", want, result.Content)
		}
	}
	var _ client.Executor = current.operations
}

func TestAPhotosDateIsTheClockWhereItWasTakenWithItsOffset(test *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		test.Skip("no time zone database")
	}
	// 08:00 on April 4 where the offset is +09:00 is the evening of April 3
	// for a person in New York, and the photo must still be April 4.
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
		{"offset unknown", &models.Photo{TakenAt: &taken}, "2025-04-03 19:00"},
		{"no date", &models.Photo{}, ""},
	} {
		if got := takenAtText(check.photo, newYork); got != check.want {
			test.Errorf("%s: %q, want %q", check.name, got, check.want)
		}
	}
	if got := describePhoto(&models.Photo{ID: "photo-x", PhotoKind: models.PhotoKindPicture, TakenAt: &taken, TakenAtOffsetMinutes: &offset}, newYork); !strings.Contains(got, "taken 2025-04-04 08:00 (UTC+09:00)") {
		test.Errorf("the line that goes with a shown photo says %q", got)
	}
}
