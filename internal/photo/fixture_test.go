package photo_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/storage"
)

// memoryFiles keeps files in memory, with the semantics of a blob store: a
// write is whole or not at all, a range is bounded, and a file that is not
// there is storage.ErrNotFound.
type memoryFiles struct {
	mutex sync.Mutex
	files map[string][]byte
}

func newMemoryFiles() *memoryFiles {
	return &memoryFiles{files: map[string][]byte{}}
}

func (self *memoryFiles) PutFileStream(ctx context.Context, id string, content io.ReadSeeker, sizeBytes int64) error {
	endBytes, err := content.Seek(0, io.SeekEnd)
	if err != nil || endBytes != sizeBytes {
		return fmt.Errorf("memory files: %s is %d bytes and was declared %d: %v", id, endBytes, sizeBytes, err)
	}
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return err
	}
	stored, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.files[id] = stored
	return nil
}

func (self *memoryFiles) OpenFileRange(ctx context.Context, id string, offsetBytes, sizeBytes int64) (*storage.FileStream, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	stored, isStored := self.files[id]
	if !isStored {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, id)
	}
	if sizeBytes < 0 {
		sizeBytes = int64(len(stored)) - offsetBytes
	}
	if offsetBytes < 0 || sizeBytes <= 0 || offsetBytes+sizeBytes > int64(len(stored)) {
		return nil, fmt.Errorf("memory files: invalid range of %s", id)
	}
	return &storage.FileStream{ReadCloser: io.NopCloser(bytes.NewReader(stored[offsetBytes : offsetBytes+sizeBytes])), SizeBytes: sizeBytes}, nil
}

func (self *memoryFiles) DeleteFileEverywhere(ctx context.Context, id string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.files, id)
	return nil
}

func (self *memoryFiles) has(id string) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_, isStored := self.files[id]
	return isStored
}

func (self *memoryFiles) get(id string) []byte {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.files[id]
}

func (self *memoryFiles) names() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	names := make([]string, 0, len(self.files))
	for name := range self.files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// fixture is a store over a real database and in-memory files, served over
// HTTP the way the server mounts it, for one signed-in person.
type fixture struct {
	test          *testing.T
	database      db.Database
	files         *memoryFiles
	store         *photo.Store
	server        *httptest.Server
	userId        string
	configuration *config.Configuration
}

func newFixture(test *testing.T) *fixture {
	test.Helper()
	database, release := dbtest.AcquireDatabase(test)
	test.Cleanup(release)
	self := &fixture{test: test, database: database, files: newMemoryFiles(), configuration: config.Default()}
	self.userId = dbtest.CreateUser(test, database, "photo-person")
	self.store = photo.New(database, self.files, func() *config.Configuration { return self.configuration })
	router := mux.NewRouter()
	router.Path(api.PathPhotoUploads).HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.store.ServeCollection(response, request, self.userId)
	})
	router.Path(api.PathPhotoUpload).HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		self.store.ServeUpload(response, request, self.userId, mux.Vars(request)["photoUploadId"])
	})
	self.server = httptest.NewServer(router)
	test.Cleanup(self.server.Close)
	return self
}

func (self *fixture) collectionUrl() string {
	return self.server.URL + api.PathPhotoUploads
}

// photoByClientId reads the person's photo by the uploader's name for it.
func (self *fixture) photoByClientId(clientPhotoId string) *models.Photo {
	self.test.Helper()
	var found *models.Photo
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		photos, err := transaction.FindPhotosByClientIDs(self.userId, []string{clientPhotoId})
		if err != nil {
			self.test.Fatal(err)
		}
		if len(photos) == 1 {
			found = photos[0]
		}
	})
	return found
}

func (self *fixture) photoFiles(photoId string) []*models.PhotoFile {
	self.test.Helper()
	var files []*models.PhotoFile
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		files, err = transaction.ListPhotoFiles(self.userId, []string{photoId})
		if err != nil {
			self.test.Fatal(err)
		}
	})
	return files
}

func (self *fixture) count(table string) string {
	self.test.Helper()
	return dbtest.QueryString(self.test, self.database, "SELECT count(*) FROM "+table)
}

func digestOf(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// smallJpeg is a real picture of one color, made here so that no photo of
// anybody is in the tests.
func smallJpeg(test *testing.T, shade uint8) []byte {
	test.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := range 12 {
		for x := range 16 {
			picture.Set(x, y, color.RGBA{R: shade, G: 100, B: 200, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, nil); err != nil {
		test.Fatal(err)
	}
	return encoded.Bytes()
}

// randomBytes is a file of the size asked for that is the same on every run.
func randomBytes(sizeBytes int, seed int64) []byte {
	content := make([]byte, sizeBytes)
	_, _ = rand.New(rand.NewSource(seed)).Read(content)
	return content
}

// readSeeker is a file to store, as the store wants it.
func readSeeker(content []byte) io.ReadSeeker { return bytes.NewReader(content) }
