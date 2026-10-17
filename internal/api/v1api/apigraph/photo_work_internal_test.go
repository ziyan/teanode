package apigraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/graphapi"
)

// withSchema builds the schema over the world's own graph, so that a document
// sent to its GraphQL route is executed by it.
func (self *photoWorld) withSchema() {
	self.test.Helper()
	graphApi := graphapi.New()
	var query Query = self.graph
	var mutation Mutation = self.graph
	var subscription Subscription = self.graph
	if err := graphApi.Register(&query, &mutation, &subscription); err != nil {
		self.test.Fatal(err)
	}
	schema, err := graphApi.Build()
	if err != nil {
		self.test.Fatal(err)
	}
	self.graph.schema = schema
	self.graph.wrapServiceOperations()
	self.graph.wrapQueryTransactions()
	self.graph.wrapTransactionFreeMutations()
}

// watchedFiles is a store that notes, whenever a file is read, how many
// database transactions are open.
type watchedFiles struct {
	storage.Storage
	files    storage.StreamFiles
	database *queryTransactionDatabase

	mutex sync.Mutex
	// openDuringReads has the count of open transactions at each read.
	openDuringReads []int64
}

func (self *watchedFiles) PutFileStream(ctx context.Context, id string, content io.ReadSeeker, sizeBytes int64) error {
	return self.files.PutFileStream(ctx, id, content, sizeBytes)
}

func (self *watchedFiles) OpenFileRange(ctx context.Context, id string, offsetBytes, sizeBytes int64) (*storage.FileStream, error) {
	// Reading one byte of a blob to see that it is stored is done inside the
	// transaction that records the file, on purpose: it is not reading the file.
	if sizeBytes != 1 {
		self.mutex.Lock()
		self.openDuringReads = append(self.openDuringReads, self.database.activeCount.Load())
		self.mutex.Unlock()
	}
	return self.files.OpenFileRange(ctx, id, offsetBytes, sizeBytes)
}

func (self *watchedFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	return self.files.DeleteFileEverywhere(ctx, id)
}

// savingWorld is a world whose attachment is waiting to be saved, with the
// database and the store watched.
func savingWorld(test *testing.T, username string) (*photoWorld, *watchedFiles, *models.AgentAttachment, string) {
	test.Helper()
	world := newPhotoWorld(test)
	watched := &watchedFiles{Storage: world.files, files: world.files.(storage.StreamFiles)}
	watched.database = &queryTransactionDatabase{Database: world.database}
	world.graph.database, world.graph.storage = watched.database, watched
	world.withSchema()
	ownerId := world.person(username, models.PermissionPhotosUse)
	picture := []byte("\xff\xd8\xff\xe0 a picture that only pretends to be one")
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		assistant, err := transaction.CreateAgent(&models.Agent{UserID: ownerId, Name: "Saving assistant", Enabled: false})
		if err != nil {
			test.Fatal(err)
		}
		attachment, err = transaction.CreateAgentAttachment(&models.AgentAttachment{AgentID: assistant.ID, Name: "chart.jpg", ContentType: "image/jpeg", Size: int64(len(picture))})
		if err != nil {
			test.Fatal(err)
		}
		if err := world.files.PutFile(test.Context(), attachment.ID, picture); err != nil {
			test.Fatal(err)
		}
	})
	return world, watched, attachment, ownerId
}

const savePhotoDocument = `mutation ($agentAttachmentId: String!) { SavePhotoFromAttachment(agentAttachmentId: $agentAttachmentId) { id photoSource originalFilename } }`

// Reading a file and storing it takes as long as the file is big, so saving an
// attachment does not do it inside the request's transaction: no transaction is
// open while the file is read.
func TestSavingAnAttachmentReadsTheFileWithNoTransactionOpen(test *testing.T) {
	world, watched, attachment, _ := savingWorld(test, "save-over-http")
	body, _ := json.Marshal(map[string]any{"query": savePhotoDocument, "variables": map[string]any{"agentAttachmentId": attachment.ID}})
	response := world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("save-over-http")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	var answer struct {
		Data struct {
			SavePhotoFromAttachment *struct {
				ID, PhotoSource, OriginalFilename string
			}
		}
		Errors []struct{ Message string }
	}
	if err := json.Unmarshal([]byte(readBody(test, response)), &answer); err != nil || len(answer.Errors) > 0 || answer.Data.SavePhotoFromAttachment == nil {
		test.Fatalf("%v %+v", err, answer)
	}
	if saved := answer.Data.SavePhotoFromAttachment; saved.ID == "" || saved.PhotoSource != "conversation" || saved.OriginalFilename != "chart.jpg" {
		test.Fatalf("%+v", saved)
	}
	if len(watched.openDuringReads) == 0 {
		test.Fatal("the file was never read")
	}
	for _, open := range watched.openDuringReads {
		if open != 0 {
			test.Fatalf("%d transactions were open while the file was read", open)
		}
	}
	// Saved again, it is the photo that is there.
	response = world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("save-over-http")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	var again struct {
		Data struct{ SavePhotoFromAttachment struct{ ID string } }
	}
	if err := json.Unmarshal([]byte(readBody(test, response)), &again); err != nil || again.Data.SavePhotoFromAttachment.ID != answer.Data.SavePhotoFromAttachment.ID {
		test.Fatalf("%v %+v", err, again)
	}

	// Somebody else's attachment is not found, and nothing is saved for them.
	world.person("save-intruder", models.PermissionPhotosUse)
	response = world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("save-intruder")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	if text := readBody(test, response); !strings.Contains(text, "errors") || strings.Contains(text, answer.Data.SavePhotoFromAttachment.ID) {
		test.Fatalf("%s", text)
	}
	// A person who may not keep photos is refused.
	world.person("save-no-photos", models.PermissionCalendarUse)
	response = world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("save-no-photos")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	if text := readBody(test, response); !strings.Contains(text, "errors") {
		test.Fatalf("%s", text)
	}
}

// The agent's tools run the same document as the person, with the agent's
// limits, and the same holds: no transaction is open while the file is read.
func TestTheAgentSavesAnAttachmentWithNoTransactionOpenAndWithinItsLimits(test *testing.T) {
	world, watched, attachment, ownerId := savingWorld(test, "save-by-agent")
	var owner *models.User
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		var err error
		if owner, err = transaction.GetUser(ownerId); err != nil {
			test.Fatal(err)
		}
	})
	operations := &agentOperations{graph: world.graph, user: owner, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionPhotosUse}}}
	var saved struct {
		SavePhotoFromAttachment struct{ ID, PhotoSource string }
	}
	if err := operations.Execute(test.Context(), savePhotoDocument, map[string]any{"agentAttachmentId": attachment.ID}, &saved); err != nil {
		test.Fatal(err)
	}
	if saved.SavePhotoFromAttachment.ID == "" || saved.SavePhotoFromAttachment.PhotoSource != "conversation" {
		test.Fatalf("%+v", saved)
	}
	if len(watched.openDuringReads) == 0 {
		test.Fatal("the file was never read")
	}
	for _, open := range watched.openDuringReads {
		if open != 0 {
			test.Fatalf("%d transactions were open while the file was read", open)
		}
	}
	// Narrowed to what does not include photos, the same call is refused.
	narrowed := operations.NarrowedTo(&models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionCalendarUse}})
	if err := narrowed.Execute(test.Context(), savePhotoDocument, map[string]any{"agentAttachmentId": attachment.ID}, &saved); err == nil {
		test.Fatal("a turn limited to calendars saved a photo")
	}
}

func TestRetryingPhotoProcessingIsTheOwnersAndNeedsThePermission(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("retry-owner", models.PermissionPhotosUse)
	otherId := world.person("retry-other", models.PermissionPhotosUse)
	failed := world.storePhoto(ownerId, "failed", "failed.jpg", []byte("failed original"), nil)
	foreign := world.storePhoto(otherId, "foreign", "foreign.jpg", []byte("foreign original"), nil)
	dbtest.Exec(test, world.database, "UPDATE photo SET photo_processing_state = 'failed', processing_attempt_count = 5, processing_error_message = 'The picture could not be read.'")
	allowed := []models.Permission{models.PermissionPhotosUse}
	world.ask(ownerId, allowed, func(ctx context.Context) {
		retried, err := world.graph.RetryPhotoProcessing(ctx, PhotoIDsArguments{PhotoIDs: []string{failed.ID, foreign.ID}})
		if err != nil || retried != 1 {
			test.Fatalf("retried %d: %v", retried, err)
		}
		found, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: failed.ID})
		if err != nil || found.Photo.PhotoProcessingState != models.PhotoProcessingWaiting || found.Photo.ProcessingAttemptCount != 0 || found.Photo.ProcessingErrorMessage != "" {
			test.Fatalf("%+v %v", found, err)
		}
	})
	if state := dbtest.QueryString(test, world.database, "SELECT photo_processing_state FROM photo WHERE id = '"+foreign.ID+"'"); state != "failed" {
		test.Fatalf("somebody else's photo is %s", state)
	}
	world.ask(ownerId, []models.Permission{models.PermissionCalendarUse}, func(ctx context.Context) {
		if _, err := world.graph.RetryPhotoProcessing(ctx, PhotoIDsArguments{PhotoIDs: []string{failed.ID}}); err == nil {
			test.Fatal("an account without photos:use retried a photo")
		}
	})
}

// A date a person corrects comes with the offset it was written in, which
// replaces the offset of the date it corrects.
func TestCorrectingADateStoresTheOffsetItWasWrittenIn(test *testing.T) {
	world := newPhotoWorld(test)
	world.withSchema()
	ownerId := world.person("date-owner", models.PermissionPhotosUse)
	stored := world.storePhoto(ownerId, "dated", "dated.jpg", []byte("dated original"), nil)
	dbtest.Exec(test, world.database, "UPDATE photo SET taken_at = '2024-05-06 05:08:09+00', taken_at_offset_minutes = 120")
	body, _ := json.Marshal(map[string]any{
		"query":     `mutation ($photoId: String!, $takenAt: DateTime) { UpdatePhoto(photoId: $photoId, takenAt: $takenAt) { takenAt takenAtOffsetMinutes } }`,
		"variables": map[string]any{"photoId": stored.ID, "takenAt": "2023-01-02T03:04:05-08:00"},
	})
	response := world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("date-owner")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	var answer struct {
		Data struct {
			UpdatePhoto struct {
				TakenAt              string
				TakenAtOffsetMinutes *int
			}
		}
		Errors []struct{ Message string }
	}
	if err := json.Unmarshal([]byte(readBody(test, response)), &answer); err != nil || len(answer.Errors) > 0 {
		test.Fatalf("%v %+v", err, answer)
	}
	taken, err := time.Parse(time.RFC3339, answer.Data.UpdatePhoto.TakenAt)
	if err != nil || !taken.Equal(time.Date(2023, 1, 2, 11, 4, 5, 0, time.UTC)) || answer.Data.UpdatePhoto.TakenAtOffsetMinutes == nil || *answer.Data.UpdatePhoto.TakenAtOffsetMinutes != -480 {
		test.Fatalf("%+v %v", answer.Data.UpdatePhoto, err)
	}
	// A date that ends in Z is on UTC.
	body, _ = json.Marshal(map[string]any{
		"query":     `mutation ($photoId: String!, $takenAt: DateTime) { UpdatePhoto(photoId: $photoId, takenAt: $takenAt) { takenAtOffsetMinutes } }`,
		"variables": map[string]any{"photoId": stored.ID, "takenAt": "2023-01-02T03:04:05Z"},
	})
	response = world.do(http.MethodPost, api.PathGraphQL, func(request *http.Request) {
		asSession("date-owner")(request)
		request.Header.Set("Content-Type", "application/json")
	}, body)
	if text := readBody(test, response); !bytes.Contains([]byte(text), []byte(`"takenAtOffsetMinutes":0`)) {
		test.Fatal(text)
	}
}

// The objects the previews and the upload parts of an account are go with the
// account; the files' blobs stay for the cleanup of blobs.
func TestDeletingAnAccountDeletesItsPreviewsAndUploadParts(test *testing.T) {
	world := newPhotoWorld(test)
	adminId := world.person("account-admin", models.PermissionUserManage)
	ownerId := world.person("account-owner", models.PermissionPhotosUse)
	otherId := world.person("account-other", models.PermissionPhotosUse)
	mine := world.storePhoto(ownerId, "mine", "mine.jpg", []byte("mine original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewSmall: []byte("small mine"), models.PhotoPreviewVideo: []byte("video mine")})
	theirs := world.storePhoto(otherId, "theirs", "theirs.jpg", []byte("theirs original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewSmall: []byte("small theirs")})
	var upload *models.PhotoUpload
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		var err error
		if upload, err = transaction.CreatePhotoUpload(&models.PhotoUpload{UserID: ownerId, PhotoUploadHeaders: models.PhotoUploadHeaders{ClientPhotoID: "unfinished", PhotoFileRole: models.PhotoFileRoleOriginal, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceCommandLine}}); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.AppendPhotoUploadPart(ownerId, &models.PhotoUploadPart{PhotoUploadID: upload.ID, PartIndex: 0, PartSizeBytes: 4, PartSHA256: strings.Repeat("a", 64)}, 0); err != nil {
			test.Fatal(err)
		}
	})
	files := world.files.(storage.StreamFiles)
	partName := models.PhotoUploadPartFileID(upload.ID, 0)
	if err := files.PutFileStream(test.Context(), partName, strings.NewReader("part"), 4); err != nil {
		test.Fatal(err)
	}
	exists := func(name string) bool {
		content, err := world.files.GetFile(test.Context(), name)
		return err == nil && len(content) > 0
	}
	mineSmall, mineVideo := models.PhotoPreviewFileID(mine.ID, models.PhotoPreviewSmall), models.PhotoPreviewFileID(mine.ID, models.PhotoPreviewVideo)
	theirSmall := models.PhotoPreviewFileID(theirs.ID, models.PhotoPreviewSmall)
	for _, name := range []string{mineSmall, mineVideo, theirSmall, partName} {
		if !exists(name) {
			test.Fatalf("%s was not stored", name)
		}
	}

	// The objects are deleted after the account is, and not before: an account
	// whose deletion fails or is rolled back keeps its previews.
	rolledBack := errors.New("the deletion was rolled back")
	if err := world.database.TransactionContext(test.Context(), func(transaction db.Transaction) error {
		admin, err := transaction.GetUser(adminId)
		if err != nil {
			return err
		}
		grants := []models.Grant{{Permission: models.PermissionUserManage}}
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: admin, Permissions: models.NewEffectivePermissions(grants)}), transaction)
		if err := world.graph.DeleteUser(ctx, DeleteUserArguments{UserID: ownerId}); err != nil {
			return err
		}
		return rolledBack
	}); !errors.Is(err, rolledBack) {
		test.Fatal(err)
	}
	for _, name := range []string{mineSmall, mineVideo, partName} {
		if !exists(name) {
			test.Fatalf("%s was deleted with an account that was not", name)
		}
	}
	if rows := dbtest.QueryString(test, world.database, "SELECT count(*) FROM photo WHERE user_id = '"+ownerId+"'"); rows != "1" {
		test.Fatalf("the rolled back deletion left %s photos", rows)
	}

	world.ask(adminId, []models.Permission{models.PermissionUserManage}, func(ctx context.Context) {
		if err := world.graph.DeleteUser(ctx, DeleteUserArguments{UserID: ownerId}); err != nil {
			test.Fatal(err)
		}
		for _, name := range []string{mineSmall, mineVideo, partName} {
			if !exists(name) {
				test.Errorf("%s was deleted before the account was", name)
			}
		}
	})
	for _, name := range []string{mineSmall, mineVideo, partName} {
		if exists(name) {
			test.Errorf("%s is still in the store after its account went", name)
		}
	}
	if !exists(theirSmall) {
		test.Error("another account's preview was deleted")
	}
	if rows := dbtest.QueryString(test, world.database, "SELECT count(*) FROM photo WHERE user_id = '"+ownerId+"'"); rows != "0" {
		test.Errorf("%s photos of the account are left", rows)
	}
	// The blobs of the files are still there, for the cleanup of blobs.
	if rows := dbtest.QueryString(test, world.database, "SELECT count(*) FROM photo_blob"); rows != "2" {
		test.Errorf("%s blobs, want both", rows)
	}
}

// The words of a search are read as a vector once for the listing and the
// count a page asks for together, and read again for other words, for another
// account, and after a few seconds.
func TestTheReadingOfASearchIsKeptForAFewSeconds(test *testing.T) {
	var cache photoSearchMeaningCache
	calls := 0
	embed := func() *db.PhotoSearchMeaning {
		calls++
		return &db.PhotoSearchMeaning{ModelName: "test-model", Vector: []float32{1}}
	}
	first := cache.read("user-one", "harbor", embed)
	if again := cache.read("user-one", "harbor", embed); again != first || calls != 1 {
		test.Fatalf("the listing and the count read the words %d times", calls)
	}
	cache.read("user-one", "market", embed)
	cache.read("user-two", "harbor", embed)
	if calls != 3 {
		test.Fatalf("other words and another account share a reading: %d", calls)
	}
	// An answer of nothing is kept too, so a model that does not answer is not
	// asked again for each request of the page.
	nothing := func() *db.PhotoSearchMeaning { calls++; return nil }
	cache.read("user-one", "silence", nothing)
	cache.read("user-one", "silence", nothing)
	if calls != 4 {
		test.Fatalf("a missing answer was asked for again: %d", calls)
	}
	// After the lifetime it is read again.
	cache.mutex.Lock()
	for key, entry := range cache.entries {
		entry.expiresAt = time.Now().Add(-time.Second)
		cache.entries[key] = entry
	}
	cache.mutex.Unlock()
	cache.read("user-one", "harbor", embed)
	if calls != 5 {
		test.Fatalf("an old reading was kept: %d", calls)
	}
	// And it never holds more than its limit.
	for index := range 2 * photoSearchMeaningLimit {
		cache.read("user-one", strings.Repeat("w", index+1), embed)
	}
	cache.mutex.Lock()
	kept := len(cache.entries)
	cache.mutex.Unlock()
	if kept > photoSearchMeaningLimit {
		test.Fatalf("%d readings are kept", kept)
	}
}
