package apigraph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/security"
)

// photoWorld is a graph over a real database and a temporary blob store, with
// its routes mounted the way the server mounts them. A header the real
// authentication middleware would set from a session is set by hand.
type photoWorld struct {
	test     *testing.T
	database db.Database
	graph    *graph
	files    storage.Storage
	server   *httptest.Server
	config   config.Store
}

// testUserHeader stands in for the session: the middleware turns a bearer
// token or a cookie into the authenticated username header.
const testUserHeader = "X-Test-Session-Username"

func newPhotoWorld(test *testing.T) *photoWorld {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = files.Close() })
	// Photos are on unless an operator turns them off; the world says so
	// outright, so that no test depends on the default.
	configuration := config.Example()
	configuration.Services.Photos.IsEnabled = new(true)
	// Signed addresses need the server's secret, which setup generates.
	configuration.Server.Secret = "photo-test-signing-secret"
	self := &photoWorld{test: test, database: database, files: files, config: config.NewMemoryStore(configuration)}
	self.graph = &graph{database: database, storage: files, config: self.config}
	router := mux.NewRouter()
	if err := self.graph.AddRoutes(router); err != nil {
		test.Fatal(err)
	}
	self.server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Header.Del(api.AuthenticatedUsernameHeader)
		if username := request.Header.Get(testUserHeader); username != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		router.ServeHTTP(response, request)
	}))
	test.Cleanup(self.server.Close)
	return self
}

// person is an account that may keep photos, or one that may not.
func (self *photoWorld) person(username string, permissions ...models.Permission) string {
	self.test.Helper()
	userId := dbtest.CreateUser(self.test, self.database, username)
	if len(permissions) > 0 {
		dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
			role, err := transaction.CreateRole(&models.Role{Name: "Role of " + username, Permissions: permissions})
			if err != nil {
				self.test.Fatal(err)
			}
			if _, err := transaction.CreateGroup(&models.Group{Name: "Group of " + username, UserIDs: []string{userId}, RoleIDs: []string{role.ID}}); err != nil {
				self.test.Fatal(err)
			}
		})
	}
	return userId
}

// devicePassword makes a password for a device with the access given.
func (self *photoWorld) devicePassword(userId string, hasPhotosAccess bool) string {
	self.test.Helper()
	selector := security.GenerateRandomString(12, appPasswordAlphabet)
	password := "sync-" + selector + "-" + security.GenerateRandomString(24, appPasswordAlphabet)
	hash, err := security.HashPassword(password)
	if err != nil {
		self.test.Fatal(err)
	}
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		if _, err := transaction.CreateSyncCredential(&models.SyncCredential{UserID: userId, CredentialName: "Test device", Selector: selector, PasswordHash: string(hash), HasPhotosAccess: hasPhotosAccess, HasCalendarAccess: !hasPhotosAccess}); err != nil {
			self.test.Fatal(err)
		}
	})
	return password
}

func (self *photoWorld) do(method, path string, configure func(*http.Request), body []byte) *http.Response {
	self.test.Helper()
	request, err := http.NewRequestWithContext(self.test.Context(), method, self.server.URL+path, bytes.NewReader(body))
	if err != nil {
		self.test.Fatal(err)
	}
	if configure != nil {
		configure(request)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		self.test.Fatal(err)
	}
	return response
}

func (self *photoWorld) status(method, path string, configure func(*http.Request), body []byte) int {
	self.test.Helper()
	response := self.do(method, path, configure, body)
	_ = response.Body.Close()
	return response.StatusCode
}

func asSession(username string) func(*http.Request) {
	return func(request *http.Request) { request.Header.Set(testUserHeader, username) }
}

func withBasic(username, password string) func(*http.Request) {
	return func(request *http.Request) { request.SetBasicAuth(username, password) }
}

func uploadHeaders(clientPhotoId string, configure func(*http.Request)) func(*http.Request) {
	return func(request *http.Request) {
		request.Header.Set("Content-Type", "image/jpeg")
		request.Header.Set("X-Photo-Client-ID", clientPhotoId)
		if configure != nil {
			configure(request)
		}
	}
}

func TestPhotoUploadsAcceptASessionOrADevicePasswordThatGrantsPhotos(test *testing.T) {
	world := newPhotoWorld(test)
	userId := world.person("uploader", models.PermissionPhotosUse)
	world.person("session-only", models.PermissionPhotosUse)
	granted := world.devicePassword(userId, true)
	ungranted := world.devicePassword(userId, false)

	if status := world.status(http.MethodOptions, api.PathPhotoUploads, withBasic("uploader", granted), nil); status != http.StatusOK {
		test.Fatalf("a device password that grants photos was refused: %d", status)
	}
	response := world.do(http.MethodPost, api.PathPhotoUploads, uploadHeaders("by-device", withBasic("uploader", granted)), []byte("jpeg bytes"))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated || response.Header.Get("X-Photo-ID") == "" {
		test.Fatalf("upload with a device password: %d %v", response.StatusCode, response.Header)
	}
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, withBasic("uploader", ungranted), nil); status != http.StatusForbidden {
		test.Fatalf("a device password without photos access got %d", status)
	}
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, withBasic("uploader", granted+"x"), nil); status != http.StatusUnauthorized {
		test.Fatalf("a wrong password got %d", status)
	}
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, withBasic("session-only", granted), nil); status != http.StatusUnauthorized {
		test.Fatalf("a device password of another account got %d", status)
	}
	refused := world.do(http.MethodOptions, api.PathPhotoUploads, nil, nil)
	_ = refused.Body.Close()
	if refused.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(refused.Header.Get("WWW-Authenticate"), "Basic") {
		test.Fatalf("nobody got %d %v", refused.StatusCode, refused.Header)
	}
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, asSession("session-only"), nil); status != http.StatusOK {
		test.Fatalf("a session got %d", status)
	}
	// A device password reaches the upload and nothing else of the library.
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(response.Header.Get("X-Photo-ID"), "small"), withBasic("uploader", granted), nil); status != http.StatusUnauthorized {
		test.Fatalf("a device password read a preview: %d", status)
	}
	// And an upload in progress is as private as the library.
	created := world.do(http.MethodPost, api.PathPhotoUploads, uploadHeaders("in-progress", func(request *http.Request) {
		request.Header.Set("Upload-Complete", "?0")
		asSession("session-only")(request)
	}), []byte("part"))
	_ = created.Body.Close()
	location := strings.TrimPrefix(created.Header.Get("Location"), world.server.URL)
	if status := world.status(http.MethodHead, location, withBasic("uploader", granted), nil); status != http.StatusNotFound {
		test.Fatalf("somebody else's upload was found: %d", status)
	}
	if status := world.status(http.MethodHead, location, asSession("session-only"), nil); status != http.StatusNoContent {
		test.Fatalf("an upload was not found by its owner: %d", status)
	}
}

func TestPhotoUploadsNeedThePermissionAndTheService(test *testing.T) {
	world := newPhotoWorld(test)
	world.person("allowed", models.PermissionPhotosUse)
	world.person("not-allowed", models.PermissionCalendarUse)
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, asSession("not-allowed"), nil); status != http.StatusForbidden {
		test.Fatalf("an account without photos:use got %d", status)
	}
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Photos.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	if status := world.status(http.MethodOptions, api.PathPhotoUploads, asSession("allowed"), nil); status != http.StatusForbidden {
		test.Fatalf("photos switched off got %d", status)
	}
}

func TestADevicePasswordIsNotAcceptedInTheClear(test *testing.T) {
	world := newPhotoWorld(test)
	userId := world.person("clear-uploader", models.PermissionPhotosUse)
	password := world.devicePassword(userId, true)
	request := httptest.NewRequest(http.MethodOptions, "http://photos.example.com"+api.PathPhotoUploads, nil)
	request.SetBasicAuth("clear-uploader", password)
	request.RemoteAddr = "203.0.113.7:51000"
	recorder := httptest.NewRecorder()
	world.graph.photoUploadCollectionView(recorder, request)
	if recorder.Code != http.StatusForbidden {
		test.Fatalf("a password over plain HTTP from another machine got %d", recorder.Code)
	}
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder = httptest.NewRecorder()
	world.graph.photoUploadCollectionView(recorder, request)
	if recorder.Code != http.StatusOK {
		test.Fatalf("a password behind a proxy that ended TLS got %d", recorder.Code)
	}
}

// storePhoto puts a photo with an original file and the previews named into
// the database and the blob store.
func (self *photoWorld) storePhoto(userId, clientPhotoId, filename string, original []byte, previews map[models.PhotoPreviewSize][]byte) *models.Photo {
	self.test.Helper()
	files := self.files.(storage.StreamFiles)
	digest := sha256.Sum256(original)
	digestText := hex.EncodeToString(digest[:])
	if err := files.PutFileStream(self.test.Context(), models.PhotoBlobFileID(digestText), bytes.NewReader(original), int64(len(original))); err != nil {
		self.test.Fatal(err)
	}
	var stored *models.Photo
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		stored, _, err = transaction.CreatePhoto(&models.Photo{UserID: userId, ClientPhotoID: clientPhotoId, PhotoKind: models.PhotoKindPicture, PhotoSource: models.PhotoSourceDashboard})
		if err != nil {
			self.test.Fatal(err)
		}
		if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digestText, BlobSizeBytes: int64(len(original))}); err != nil {
			self.test.Fatal(err)
		}
		if _, err := transaction.SetPhotoFile(userId, stored.ID, &models.PhotoFile{PhotoFileRole: models.PhotoFileRoleOriginal, BlobSHA256: digestText, ContentType: "image/jpeg", FileSizeBytes: int64(len(original)), OriginalFilename: filename}, 0); err != nil {
			self.test.Fatal(err)
		}
		for size, content := range previews {
			contentType := "image/jpeg"
			if size == models.PhotoPreviewVideo {
				contentType = "video/mp4"
			}
			if err := files.PutFileStream(self.test.Context(), models.PhotoPreviewFileID(stored.ID, size), bytes.NewReader(content), int64(len(content))); err != nil {
				self.test.Fatal(err)
			}
			if _, err := transaction.UpsertPhotoPreview(userId, &models.PhotoPreview{PhotoID: stored.ID, PhotoPreviewSize: size, ContentType: contentType, PreviewSizeBytes: int64(len(content))}); err != nil {
				self.test.Fatal(err)
			}
		}
		// Recording the file moved the photo's update time on, into a later
		// second when the clock ticked over meanwhile. A preview's address
		// carries that time, so the photo handed back is read again, after
		// the writes, and says the time the database holds.
		stored, err = transaction.GetPhoto(userId, stored.ID)
		if err != nil {
			self.test.Fatal(err)
		}
	})
	return stored
}

func readBody(test *testing.T, response *http.Response) string {
	test.Helper()
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		test.Fatal(err)
	}
	return string(content)
}

func TestPhotoMediaIsServedToItsOwnerOnly(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("media-owner", models.PermissionPhotosUse)
	world.person("media-other", models.PermissionPhotosUse)
	video := []byte("0123456789abcdefghij")
	stored := world.storePhoto(ownerId, "media", `Beach "day".JPG`, []byte("the original picture"), map[models.PhotoPreviewSize][]byte{
		models.PhotoPreviewSmall: []byte("small picture"), models.PhotoPreviewVideo: video,
	})

	response := world.do(http.MethodGet, api.PhotoPreviewPath(stored.ID, "small"), asSession("media-owner"), nil)
	body := readBody(test, response)
	if response.StatusCode != http.StatusOK || body != "small picture" || response.Header.Get("Content-Type") != "image/jpeg" ||
		response.Header.Get("Cache-Control") != "private, max-age=31536000, immutable" || response.Header.Get("ETag") == "" {
		test.Fatalf("preview: %d %q %v", response.StatusCode, body, response.Header)
	}
	etag := response.Header.Get("ETag")
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(stored.ID, "small"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("If-None-Match", etag)
	}, nil); status != http.StatusNotModified {
		test.Fatalf("a preview the browser has got %d", status)
	}
	head := world.do(http.MethodHead, api.PhotoPreviewPath(stored.ID, "small"), asSession("media-owner"), nil)
	if body := readBody(test, head); head.StatusCode != http.StatusOK || body != "" || head.Header.Get("Content-Length") != strconv.Itoa(len("small picture")) {
		test.Fatalf("HEAD: %d %q %v", head.StatusCode, body, head.Header)
	}
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(stored.ID, "large"), asSession("media-owner"), nil); status != http.StatusNotFound {
		test.Fatalf("a preview that was not made got %d", status)
	}
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(stored.ID, "huge"), asSession("media-owner"), nil); status != http.StatusNotFound {
		test.Fatalf("a preview of no size got %d", status)
	}
	for _, path := range []string{api.PhotoPreviewPath(stored.ID, "small"), api.PhotoFilePath(stored.ID, "original")} {
		if status := world.status(http.MethodGet, path, asSession("media-other"), nil); status != http.StatusNotFound {
			test.Fatalf("%s for somebody else got %d", path, status)
		}
		if status := world.status(http.MethodGet, path, nil, nil); status != http.StatusUnauthorized {
			test.Fatalf("%s for nobody got %d", path, status)
		}
	}

	ranged := world.do(http.MethodGet, api.PhotoPreviewPath(stored.ID, "video"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("Range", "bytes=5-9")
	}, nil)
	if body := readBody(test, ranged); ranged.StatusCode != http.StatusPartialContent || body != "56789" || ranged.Header.Get("Content-Range") != "bytes 5-9/20" {
		test.Fatalf("range: %d %q %v", ranged.StatusCode, body, ranged.Header)
	}
	stale := world.do(http.MethodGet, api.PhotoPreviewPath(stored.ID, "video"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("Range", "bytes=5-9")
		request.Header.Set("If-Range", `"another version"`)
	}, nil)
	if body := readBody(test, stale); stale.StatusCode != http.StatusOK || body != string(video) {
		test.Fatalf("a range of another version: %d %q", stale.StatusCode, body)
	}
	if status := world.status(http.MethodGet, api.PhotoPreviewPath(stored.ID, "video"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("Range", "bytes=50-60")
	}, nil); status != http.StatusRequestedRangeNotSatisfiable {
		test.Fatalf("a range past the end got %d", status)
	}
	// A picture preview is not read in ranges.
	whole := world.do(http.MethodGet, api.PhotoPreviewPath(stored.ID, "small"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("Range", "bytes=0-3")
	}, nil)
	if body := readBody(test, whole); whole.StatusCode != http.StatusOK || body != "small picture" {
		test.Fatalf("%d %q", whole.StatusCode, body)
	}

	file := world.do(http.MethodGet, api.PhotoFilePath(stored.ID, "original"), asSession("media-owner"), nil)
	if body := readBody(test, file); file.StatusCode != http.StatusOK || body != "the original picture" {
		test.Fatalf("file: %d %q", file.StatusCode, body)
	}
	disposition := file.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, `filename="Beach \"day\".JPG"`) {
		test.Fatalf("the file name is not safely quoted: %q", disposition)
	}
	fileRange := world.do(http.MethodGet, api.PhotoFilePath(stored.ID, "original"), func(request *http.Request) {
		asSession("media-owner")(request)
		request.Header.Set("Range", "bytes=-7")
	}, nil)
	if body := readBody(test, fileRange); fileRange.StatusCode != http.StatusPartialContent || body != "picture" {
		test.Fatalf("file range: %d %q", fileRange.StatusCode, body)
	}
	if status := world.status(http.MethodGet, api.PhotoFilePath(stored.ID, "edited"), asSession("media-owner"), nil); status != http.StatusNotFound {
		test.Fatalf("a file the photo does not have got %d", status)
	}
}

// ask runs an operation as a person, in a transaction of its own.
func (self *photoWorld) ask(userId string, permissions []models.Permission, run func(ctx context.Context)) {
	self.test.Helper()
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		user, err := transaction.GetUser(userId)
		if err != nil {
			self.test.Fatal(err)
		}
		grants := make([]models.Grant, 0, len(permissions))
		for _, permission := range permissions {
			grants = append(grants, models.Grant{Permission: permission})
		}
		run(api.ContextWithTransaction(api.ContextWithPrincipal(self.test.Context(), &api.Principal{User: user, Permissions: models.NewEffectivePermissions(grants)}), transaction))
	})
}

func TestPhotoOperationsListReadTrashRestoreAndDelete(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("operations-owner", models.PermissionPhotosUse)
	otherId := world.person("operations-other", models.PermissionPhotosUse)
	first := world.storePhoto(ownerId, "first", "first.jpg", []byte("first original"), map[models.PhotoPreviewSize][]byte{models.PhotoPreviewSmall: []byte("small one")})
	second := world.storePhoto(ownerId, "second", "second.jpg", []byte("second original"), nil)
	world.storePhoto(otherId, "foreign", "foreign.jpg", []byte("foreign original"), nil)
	allowed := []models.Permission{models.PermissionPhotosUse}

	world.ask(ownerId, allowed, func(ctx context.Context) {
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{})
		if err != nil || len(listed) != 2 {
			test.Fatalf("listed %v %v", listed, err)
		}
		byId := map[string]*models.Photo{listed[0].ID: listed[0], listed[1].ID: listed[1]}
		wantAddress := api.PhotoPreviewPath(first.ID, "small") + "?v=" + strconv.FormatInt(byId[first.ID].UpdatedAt.Unix(), 10)
		if byId[first.ID].SmallPreviewURL != wantAddress || byId[first.ID].LargePreviewURL != "" || byId[first.ID].VideoPreviewURL != "" || byId[second.ID].SmallPreviewURL != "" {
			test.Fatalf("preview addresses: %q %q %q", byId[first.ID].SmallPreviewURL, byId[first.ID].LargePreviewURL, byId[second.ID].SmallPreviewURL)
		}
		// A page reached by number, and the size of the whole listing.
		if page, err := world.graph.ListPhotos(ctx, ListPhotosArguments{Offset: 1, PageSize: 1}); err != nil || len(page) != 1 {
			test.Fatalf("page two of one: %v %v", page, err)
		}
		if _, err := world.graph.ListPhotos(ctx, ListPhotosArguments{Offset: 1, BeforePhotoID: first.ID}); err == nil {
			test.Fatal("an offset with a cursor was accepted")
		}
		if _, err := world.graph.ListPhotos(ctx, ListPhotosArguments{Offset: 100001}); err == nil {
			test.Fatal("an offset past the limit was accepted")
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{Offset: 1, PageSize: 1, BeforePhotoID: first.ID}); err != nil || count != 2 {
			test.Fatalf("counted %d: %v", count, err)
		}
		if count, err := world.graph.CountPhotos(ctx, ListPhotosArguments{IsFavorite: new(true)}); err != nil || count != 0 {
			test.Fatalf("counted %d: %v", count, err)
		}
		view, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: first.ID})
		if err != nil || len(view.PhotoFiles) != 1 || view.PhotoFiles[0].FileURL != api.PhotoFilePath(first.ID, "original") || len(view.PhotoPreviews) != 1 || view.PhotoPreviews[0].PreviewURL != wantAddress {
			test.Fatalf("read %+v %v", view, err)
		}
		if _, err := world.graph.ReadPhoto(ctx, PhotoArguments{PhotoID: "no-such-photo"}); err == nil {
			test.Fatal("a photo that is not there was read")
		}
		found, err := world.graph.FindPhotos(ctx, FindPhotosArguments{ClientPhotoIDs: []string{"second", "foreign", "absent"}})
		if err != nil || len(found) != 1 || found[0].ID != second.ID {
			test.Fatalf("found %v %v", found, err)
		}
		updated, err := world.graph.UpdatePhoto(ctx, UpdatePhotoArguments{PhotoID: first.ID, IsFavorite: new(true)})
		if err != nil || !updated.IsFavorite {
			test.Fatalf("updated %v %v", updated, err)
		}
		if _, err := world.graph.UpdatePhoto(ctx, UpdatePhotoArguments{PhotoID: first.ID}); err == nil {
			test.Fatal("an update that changes nothing was accepted")
		}
		if _, err := world.graph.UpdatePhoto(ctx, UpdatePhotoArguments{PhotoID: "foreign", IsFavorite: new(true)}); err == nil {
			test.Fatal("somebody else's photo was updated")
		}
		library, err := world.graph.ReadPhotoLibrary(ctx)
		if err != nil || library.PhotoCount != 2 || library.FavoriteCount != 1 || library.UsedBytes != int64(len("first original")+len("second original")) ||
			library.MaximumFileBytes != 8<<30 || library.MaximumAccountBytes != 0 || library.TrashRetentionDays != 30 || library.UploadPath != api.PathPhotoUploads ||
			library.IsFaceRecognitionAvailable || library.FaceRecognitionUnavailableMessage == "" || library.IsDescriptionAvailable || library.DescriptionUnavailableMessage == "" {
			test.Fatalf("library %+v %v", library, err)
		}

		if deleted, err := world.graph.DeletePhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{second.ID}}); err != nil || deleted != 0 {
			test.Fatalf("a photo outside the trash was deleted: %d %v", deleted, err)
		}
		if trashed, err := world.graph.TrashPhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{second.ID}}); err != nil || trashed != 1 {
			test.Fatalf("trashed %d %v", trashed, err)
		}
		inTrash, err := world.graph.ListPhotos(ctx, ListPhotosArguments{IsTrashed: new(true)})
		if err != nil || len(inTrash) != 1 || inTrash[0].ID != second.ID {
			test.Fatalf("trash %v %v", inTrash, err)
		}
		if restored, err := world.graph.RestorePhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{second.ID}}); err != nil || restored != 1 {
			test.Fatalf("restored %d %v", restored, err)
		}
		if _, err := world.graph.TrashPhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{first.ID, second.ID}}); err != nil {
			test.Fatal(err)
		}
		if deleted, err := world.graph.DeletePhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{first.ID}}); err != nil || deleted != 1 {
			test.Fatalf("deleted %d %v", deleted, err)
		}
		if emptied, err := world.graph.EmptyPhotoTrash(ctx); err != nil || emptied != 1 {
			test.Fatalf("emptied %d %v", emptied, err)
		}
	})
	world.ask(otherId, allowed, func(ctx context.Context) {
		listed, err := world.graph.ListPhotos(ctx, ListPhotosArguments{})
		if err != nil || len(listed) != 1 || listed[0].ClientPhotoID != "foreign" {
			test.Fatalf("the other account's library is %v %v", listed, err)
		}
	})
}

func TestPhotoOperationsRefuseWithoutThePermissionOrTheService(test *testing.T) {
	world := newPhotoWorld(test)
	userId := world.person("refused", models.PermissionCalendarUse)
	world.ask(userId, []models.Permission{models.PermissionCalendarUse}, func(ctx context.Context) {
		if _, err := world.graph.ListPhotos(ctx, ListPhotosArguments{}); err == nil {
			test.Fatal("photos were listed without photos:use")
		}
		if _, err := world.graph.EmptyPhotoTrash(ctx); err == nil {
			test.Fatal("the trash was emptied without photos:use")
		}
	})
	if err := world.config.Update(func(configuration *config.Configuration) error {
		configuration.Services.Photos.IsEnabled = new(false)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	world.ask(userId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		_, err := world.graph.ListPhotos(ctx, ListPhotosArguments{})
		var unavailable *models.ServiceUnavailableError
		if !errorsAs(err, &unavailable) {
			test.Fatalf("photos switched off: %v", err)
		}
	})
}

func TestAConversationAttachmentIsSavedAsAPhotoOnlyByItsOwner(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("attachment-owner", models.PermissionPhotosUse)
	otherId := world.person("attachment-other", models.PermissionPhotosUse)
	picture := []byte("\xff\xd8\xff\xe0 a picture that only pretends to be one")
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		assistant, err := transaction.CreateAgent(&models.Agent{UserID: ownerId, Name: "Attachment assistant", Enabled: false})
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
	allowed := []models.Permission{models.PermissionPhotosUse}
	world.ask(otherId, allowed, func(ctx context.Context) {
		if _, err := world.graph.SavePhotoFromAttachment(ctx, SavePhotoFromAttachmentArguments{AgentAttachmentID: attachment.ID}); err == nil {
			test.Fatal("somebody else's attachment was saved")
		}
	})
	var savedId string
	world.ask(ownerId, allowed, func(ctx context.Context) {
		saved, err := world.graph.SavePhotoFromAttachment(ctx, SavePhotoFromAttachmentArguments{AgentAttachmentID: attachment.ID})
		if err != nil || saved.PhotoSource != models.PhotoSourceConversation || saved.UserID != ownerId {
			test.Fatalf("saved %+v %v", saved, err)
		}
		savedId = saved.ID
		again, err := world.graph.SavePhotoFromAttachment(ctx, SavePhotoFromAttachmentArguments{AgentAttachmentID: attachment.ID})
		if err != nil || again.ID != savedId {
			test.Fatalf("saved twice %+v %v", again, err)
		}
	})
}

func errorsAs[T error](err error, target *T) bool {
	for err != nil {
		if matched, ok := err.(T); ok {
			*target = matched
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}

func TestFindPhotosSaysWhichFilesTheServerHolds(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("roles-owner", models.PermissionPhotosUse)
	live := world.storePhoto(ownerId, "live", "live.heic", []byte("live original"), nil)
	world.storePhoto(ownerId, "plain", "plain.jpg", []byte("plain original"), nil)
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		for _, role := range []models.PhotoFileRole{models.PhotoFileRoleLiveVideo, models.PhotoFileRoleEdited} {
			sum := sha256.Sum256([]byte(role))
			digest := hex.EncodeToString(sum[:])
			if _, err := transaction.InsertPhotoBlobIfAbsent(&models.PhotoBlob{BlobSHA256: digest, BlobSizeBytes: 10}); err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.SetPhotoFile(ownerId, live.ID, &models.PhotoFile{PhotoFileRole: role, BlobSHA256: digest, ContentType: "video/quicktime", FileSizeBytes: 10}, 0); err != nil {
				test.Fatal(err)
			}
		}
	})
	world.ask(ownerId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		found, err := world.graph.FindPhotos(ctx, FindPhotosArguments{ClientPhotoIDs: []string{"live", "plain"}})
		if err != nil || len(found) != 2 {
			test.Fatalf("found %v %v", found, err)
		}
		rolesOf := map[string][]models.PhotoFileRole{}
		for _, photo := range found {
			rolesOf[photo.ClientPhotoID] = photo.PhotoFileRoles
		}
		if got := rolesOf["live"]; len(got) != 3 || got[0] != models.PhotoFileRoleEdited || got[1] != models.PhotoFileRoleLiveVideo || got[2] != models.PhotoFileRoleOriginal {
			test.Fatalf("the Live Photo holds %v, want edited, liveVideo and original", got)
		}
		if got := rolesOf["plain"]; len(got) != 1 || got[0] != models.PhotoFileRoleOriginal {
			test.Fatalf("the plain photo holds %v, want only the original", got)
		}
		for _, photo := range found {
			if photo.TrashedAt != nil {
				test.Fatalf("photo %q in the library says it is in the Trash since %v", photo.ClientPhotoID, photo.TrashedAt)
			}
		}
	})

	// A photo in the Trash is found too, and says since when, so an uploader
	// can tell "already in your library" from "in the Trash".
	dbtest.RunTransactionOn(test, world.database, func(transaction db.Transaction) {
		if _, err := transaction.TrashPhotos(ownerId, []string{live.ID}); err != nil {
			test.Fatal(err)
		}
	})
	world.ask(ownerId, []models.Permission{models.PermissionPhotosUse}, func(ctx context.Context) {
		found, err := world.graph.FindPhotos(ctx, FindPhotosArguments{ClientPhotoIDs: []string{"live", "plain"}})
		if err != nil || len(found) != 2 {
			test.Fatalf("found %v %v", found, err)
		}
		for _, photo := range found {
			if isTrashed := photo.TrashedAt != nil; isTrashed != (photo.ClientPhotoID == "live") {
				test.Fatalf("photo %q has trashedAt %v", photo.ClientPhotoID, photo.TrashedAt)
			}
		}
	})
}

func TestASignedPlaybackAddressServesRangesWithoutASession(test *testing.T) {
	world := newPhotoWorld(test)
	ownerId := world.person("playback-owner", models.PermissionPhotosUse)
	otherId := world.person("playback-other", models.PermissionPhotosUse)
	video := []byte("0123456789abcdefghij")
	original := []byte("the original video")
	stored := world.storePhoto(ownerId, "playback", "clip.mov", original, map[models.PhotoPreviewSize][]byte{models.PhotoPreviewVideo: video, models.PhotoPreviewSmall: []byte("small")})
	foreign := world.storePhoto(otherId, "foreign-playback", "other.mov", []byte("somebody else's"), nil)
	allowed := []models.Permission{models.PermissionPhotosUse}

	var previewAddress, fileAddress string
	world.ask(ownerId, allowed, func(ctx context.Context) {
		var err error
		size, role := models.PhotoPreviewVideo, models.PhotoFileRoleOriginal
		if previewAddress, err = world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: stored.ID, PhotoPreviewSize: &size}); err != nil {
			test.Fatal(err)
		}
		if fileAddress, err = world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: stored.ID, PhotoFileRole: &role}); err != nil {
			test.Fatal(err)
		}
		// Exactly one of the two, a photo that is the caller's, and a thing that exists.
		if _, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: stored.ID}); err == nil {
			test.Fatal("an address for neither a preview nor a file was made")
		}
		if _, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: stored.ID, PhotoPreviewSize: &size, PhotoFileRole: &role}); err == nil {
			test.Fatal("an address for both a preview and a file was made")
		}
		if _, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: foreign.ID, PhotoFileRole: &role}); err == nil {
			test.Fatal("an address for somebody else's photo was made")
		}
		large := models.PhotoPreviewLarge
		if _, err := world.graph.PhotoPlaybackAddress(ctx, PhotoPlaybackAddressArguments{PhotoID: stored.ID, PhotoPreviewSize: &large}); err == nil {
			test.Fatal("an address for a preview that was not made was made")
		}
	})
	if !strings.HasPrefix(previewAddress, api.PhotoPlaybackPreviewPath(stored.ID, "video")+"?") || !strings.HasPrefix(fileAddress, api.PhotoPlaybackFilePath(stored.ID, "original")+"?") {
		test.Fatalf("the addresses are %q and %q", previewAddress, fileAddress)
	}

	// No session at all: a player cannot send one. A range is honored.
	ranged := world.do(http.MethodGet, previewAddress, func(request *http.Request) { request.Header.Set("Range", "bytes=5-9") }, nil)
	if body := readBody(test, ranged); ranged.StatusCode != http.StatusPartialContent || body != "56789" || ranged.Header.Get("Content-Range") != "bytes 5-9/20" ||
		ranged.Header.Get("Accept-Ranges") != "bytes" || ranged.Header.Get("Content-Type") != "video/mp4" {
		test.Fatalf("a range of the video: %d %q %v", ranged.StatusCode, body, ranged.Header)
	}
	whole := world.do(http.MethodGet, previewAddress, nil, nil)
	if body := readBody(test, whole); whole.StatusCode != http.StatusOK || body != string(video) {
		test.Fatalf("the whole video: %d %q", whole.StatusCode, body)
	}
	file := world.do(http.MethodGet, fileAddress, func(request *http.Request) { request.Header.Set("Range", "bytes=4-") }, nil)
	if body := readBody(test, file); file.StatusCode != http.StatusPartialContent || body != string(original[4:]) || !strings.HasPrefix(file.Header.Get("Content-Disposition"), "inline") {
		test.Fatalf("the rest of the file: %d %q %v", file.StatusCode, body, file.Header)
	}

	// Nothing else opens: not another file with the same signature, not
	// another photo, not another owner, not a changed or missing signature.
	query, err := url.ParseQuery(previewAddress[strings.Index(previewAddress, "?")+1:])
	if err != nil {
		test.Fatal(err)
	}
	owner, signature := query.Get("owner"), query.Get("signature")
	changedDigit := "0"
	if strings.HasSuffix(previewAddress, "0") {
		changedDigit = "1"
	}
	secret := []byte("photo-test-signing-secret")
	expired := photo.SignPlayback(secret, ownerId, stored.ID, photo.PlaybackKindPreview, "video", time.Now().Add(-time.Minute))
	otherOwner := photo.SignPlayback(secret, otherId, stored.ID, photo.PlaybackKindPreview, "video", time.Now().Add(time.Hour))
	wrongSecret := photo.SignPlayback([]byte("another secret"), ownerId, stored.ID, photo.PlaybackKindPreview, "video", time.Now().Add(time.Hour))
	for name, path := range map[string]string{
		"the preview's signature on the small preview":   api.PhotoPlaybackPreviewPath(stored.ID, "small") + "?owner=" + owner + "&signature=" + signature,
		"the preview's signature on a file of that kind": api.PhotoPlaybackFilePath(stored.ID, "original") + "?owner=" + owner + "&signature=" + signature,
		"another photo":                   api.PhotoPlaybackPreviewPath(foreign.ID, "video") + "?owner=" + owner + "&signature=" + signature,
		"a changed signature":             previewAddress[:len(previewAddress)-1] + changedDigit,
		"no signature":                    api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?owner=" + owner,
		"no owner":                        api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?signature=" + signature,
		"an expired signature":            api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?owner=" + owner + "&signature=" + expired,
		"another owner's":                 api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?owner=" + otherId + "&signature=" + otherOwner,
		"the signature of another server": api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?owner=" + owner + "&signature=" + wrongSecret,
		"a malformed signature":           api.PhotoPlaybackPreviewPath(stored.ID, "video") + "?owner=" + owner + "&signature=not-a-signature",
	} {
		if status := world.status(http.MethodGet, path, nil, nil); status != http.StatusNotFound {
			test.Errorf("%s was served with %d", name, status)
		}
	}
	// The address is for reading.
	if status := world.status(http.MethodDelete, previewAddress, nil, nil); status == http.StatusOK || status == http.StatusNoContent {
		test.Errorf("a DELETE of a shared address answered %d", status)
	}
	// A photo that is deleted ends it, though its address has not expired.
	world.ask(ownerId, allowed, func(ctx context.Context) {
		if _, err := world.graph.TrashPhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{stored.ID}}); err != nil {
			test.Fatal(err)
		}
		if _, err := world.graph.DeletePhotos(ctx, PhotoIDsArguments{PhotoIDs: []string{stored.ID}}); err != nil {
			test.Fatal(err)
		}
	})
	if status := world.status(http.MethodGet, previewAddress, nil, nil); status != http.StatusNotFound {
		test.Errorf("the address of a deleted photo answered %d", status)
	}
}
