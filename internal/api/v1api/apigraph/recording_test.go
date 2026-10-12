package apigraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/web"
)

type unreadRecordingBody struct{}

func (unreadRecordingBody) Read([]byte) (int, error) { panic("unauthorized recording body was read") }
func (unreadRecordingBody) Close() error             { return nil }

func TestRecordingUploadAuthorizesBeforeReadingMedia(test *testing.T) {
	resolver := &graph{}
	request := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/", nil), map[string]string{"mediaId": "invented", "partIndex": "0"})
	request.Body = unreadRecordingBody{}
	response := httptest.NewRecorder()
	resolver.recordingPartView(response, request)
	if response.Code != http.StatusUnauthorized {
		test.Fatal("anonymous upload was not refused")
	}
}

func TestRecordingHTTPUploadReceiptsPlaybackAndCookieProtection(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	spool, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	ownerId := dbtest.CreateUser(test, database, "recording-owner")
	otherId := dbtest.CreateUser(test, database, "recording-other")
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "Recording access", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Recording people", RoleIDs: []string{role.ID}, UserIDs: []string{ownerId, otherId}}); err != nil {
			test.Fatal(err)
		}
	})
	resolver := &graph{database: database, storage: spool, config: config.NewMemoryStore(config.Default())}
	owner, err := database.GetUserByUsername("recording-owner")
	if err != nil {
		test.Fatal(err)
	}
	var source *models.Recording
	var media *models.RecordingMedia
	content := []byte("original-recording-bytes")
	digest := sha256.Sum256(content)
	sha256Text := hex.EncodeToString(digest[:])
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}), transaction)
		source, err = resolver.CreateRecording(ctx, CreateRecordingArguments{ClientRecordingID: "local-source", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		media, err = resolver.CreateRecordingMedia(ctx, CreateRecordingMediaArguments{RecordingID: source.ID, ClientMediaID: "local-file", OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(content)), OriginalSHA256: sha256Text})
		if err != nil {
			test.Fatal(err)
		}
		limits, err := resolver.ReadRecordingLimits(ctx)
		if err != nil || limits.MaximumMediaBytes != 2<<30 || limits.MaximumAccountBytes != 64<<30 {
			test.Fatal("recording limits borrowed the chat-file limit", err)
		}
	})
	requestAs := func(username, method string, body io.Reader) *http.Request {
		request := httptest.NewRequest(method, "/", body)
		request.Header.Set(api.AuthenticatedUsernameHeader, username)
		request = mux.SetURLVars(request, map[string]string{"mediaId": media.ID, "partIndex": "0"})
		return request
	}
	for _, testCase := range []struct {
		uploadBytes    []byte
		expectedStatus int
	}{
		{append(append([]byte{}, content...), 'x'), http.StatusRequestEntityTooLarge},
		{content[:len(content)-1], http.StatusBadRequest},
		{bytes.Repeat([]byte("x"), len(content)), http.StatusBadRequest},
	} {
		request := requestAs("recording-owner", http.MethodPut, bytes.NewReader(testCase.uploadBytes))
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Recording-Part-SHA256", sha256Text)
		response := httptest.NewRecorder()
		resolver.recordingPartView(response, request)
		if response.Code != testCase.expectedStatus {
			test.Fatal("invalid upload part was accepted", response.Code)
		}
	}
	upload := requestAs("recording-owner", http.MethodPut, bytes.NewReader(content))
	upload.Header.Set("Content-Type", "application/octet-stream")
	upload.Header.Set("X-Recording-Part-SHA256", sha256Text)
	response := httptest.NewRecorder()
	resolver.recordingPartView(response, upload)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "storedAt") {
		test.Fatal("durable upload failed", response.Code, response.Body.String())
	}
	otherUpload := requestAs("recording-other", http.MethodPut, nil)
	otherUpload.Body = unreadRecordingBody{}
	response = httptest.NewRecorder()
	resolver.recordingPartView(response, otherUpload)
	if response.Code != http.StatusNotFound {
		test.Fatal("cross-account upload read the body or exposed media", response.Code)
	}
	cookieRequest := requestAs("recording-owner", http.MethodPost, nil)
	cookieRequest.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: "fixture-cookie"})
	cookieRequest.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	resolver.recordingCompleteView(response, cookieRequest)
	if response.Code != http.StatusUnsupportedMediaType {
		test.Fatal("a simple cross-origin cookie request could publish media", response.Code)
	}
	complete := requestAs("recording-owner", http.MethodPost, nil)
	complete.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	resolver.recordingCompleteView(response, complete)
	if response.Code != http.StatusOK {
		test.Fatal("media completion failed", response.Code, response.Body.String())
	}
	completionJSON := response.Body.String()
	retryResponse := httptest.NewRecorder()
	resolver.recordingCompleteView(retryResponse, complete)
	if retryResponse.Code != http.StatusOK || retryResponse.Body.String() != completionJSON {
		test.Fatal("completion retry changed the durable receipt", retryResponse.Body.String())
	}
	playback := requestAs("recording-owner", http.MethodGet, nil)
	playback.Header.Set("Range", "bytes=3-8")
	response = httptest.NewRecorder()
	resolver.recordingMediaView(response, playback)
	if response.Code != http.StatusPartialContent || response.Body.String() != string(content[3:9]) || response.Header().Get("Content-Range") != fmt.Sprintf("bytes 3-8/%d", len(content)) || response.Header().Get("Cache-Control") != "private, no-store" {
		test.Fatal("private range playback failed", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	resolver.recordingMediaView(response, requestAs("recording-other", http.MethodGet, nil))
	if response.Code != http.StatusNotFound {
		test.Fatal("cross-account playback was accepted", response.Code)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(ownerId, source.ID); err != nil {
			test.Fatal(err)
		}
	})
	response = httptest.NewRecorder()
	resolver.recordingMediaView(response, playback)
	if response.Code != http.StatusGone {
		test.Fatal("deleted source stayed playable", response.Code)
	}
	if err := recording.New(database, spool.(storage.StreamFiles)).Cleanup(test.Context(), 10); err != nil {
		test.Fatal(err)
	}
}

func TestRecordingByteRangesAreBounded(test *testing.T) {
	for _, testCase := range []struct {
		rangeHeader            string
		offsetBytes, sizeBytes int64
	}{
		{"bytes=0-9", 0, 10}, {"bytes=9-", 9, 1}, {"bytes=-4", 6, 4}, {"bytes=-40", 0, 10}, {"bytes=2-100", 2, 8},
	} {
		offsetBytes, sizeBytes, err := recordingByteRange(testCase.rangeHeader, 10)
		if err != nil || offsetBytes != testCase.offsetBytes || sizeBytes != testCase.sizeBytes {
			test.Fatal("incorrect media range", testCase.rangeHeader, err)
		}
	}
	for _, rangeHeader := range []string{"bytes=10-", "bytes=4-2", "bytes=0-1,4-5", "bytes=-0", "bytes=--1", "items=0-2", "bytes=0-9223372036854775808"} {
		if _, _, err := recordingByteRange(rangeHeader, 10); !errors.Is(err, db.ErrInvalidArguments) {
			test.Fatal("invalid range was accepted", rangeHeader, err)
		}
	}
}
