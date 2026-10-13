package apigraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

func TestRecordingPlaybackHTTPAndQueryKeepOwnershipAndRanges(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	files, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = files.Close() })
	ownerId := dbtest.CreateUser(test, database, "playback-owner")
	otherId := dbtest.CreateUser(test, database, "playback-other")
	var media *models.RecordingMedia
	content := bytes.Repeat([]byte("a"), 64)
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "Recording playback", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Playback people", RoleIDs: []string{role.ID}, UserIDs: []string{ownerId, otherId}}); err != nil {
			test.Fatal(err)
		}
		source, err := transaction.CreateRecording(&models.Recording{UserID: ownerId, ClientRecordingID: "http-playback", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			test.Fatal(err)
		}
		media, err = transaction.CreateRecordingMedia(ownerId, &models.RecordingMedia{RecordingID: source.ID, ClientMediaID: "http-media", OriginalFilename: "original.caf", OriginalContentType: "audio/x-caf", OriginalSizeBytes: int64(len(content)), OriginalSHA256: digestText, TimelineStartMS: 9000}, 1<<20, 2<<20)
		if err != nil {
			test.Fatal(err)
		}
	})
	store := recording.New(database, files.(storage.StreamFiles))
	if _, err := store.PutPart(test.Context(), ownerId, media.ID, 0, digestText, content); err != nil {
		test.Fatal(err)
	}
	if _, _, err := store.Complete(test.Context(), ownerId, media.ID); err != nil {
		test.Fatal(err)
	}
	resolver := &graph{database: database, storage: files, config: config.NewMemoryStore(config.Default())}
	requestAs := func(username, method, rangeHeader, ifRange string) *http.Request {
		request := httptest.NewRequest(method, "/", nil)
		if username != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		request.Header.Set("Range", rangeHeader)
		request.Header.Set("If-Range", ifRange)
		return mux.SetURLVars(request, map[string]string{"mediaId": media.ID})
	}
	queryAs := func(userId string) (*models.RecordingPlayback, error) {
		var playback *models.RecordingPlayback
		err := database.Transaction(func(transaction db.Transaction) error {
			owner, err := transaction.GetUser(userId)
			if err != nil {
				return err
			}
			ctx := api.ContextWithTransaction(api.ContextWithPrincipal(test.Context(), &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}), transaction)
			playback, err = resolver.ReadRecordingPlayback(ctx, RecordingPlaybackArguments{RecordingMediaID: media.ID})
			return err
		})
		return playback, err
	}
	if playback, err := queryAs(ownerId); err != nil || playback != nil {
		test.Fatal("query implicitly prepared media", err)
	}
	if _, err := queryAs(otherId); err == nil {
		test.Fatal("foreign query exposed original")
	}
	response := httptest.NewRecorder()
	resolver.recordingPlaybackView(response, requestAs("playback-owner", http.MethodGet, "", ""))
	if response.Code != http.StatusNotFound {
		test.Fatal("GET implicitly prepared media", response.Code)
	}
	blobId := db.RecordingPreviewFileID(media.ID)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if _, err := transaction.ReserveRecordingPreview(ownerId, media.ID, &models.RecordingBlob{ID: blobId, BlobSizeBytes: int64(len(content)), BlobSHA256: digestText}, 2<<20); err != nil {
			test.Fatal(err)
		}
	})
	if err := files.PutFile(test.Context(), blobId, content); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.VerifyRecordingBlob(ownerId, blobId, digestText); err != nil {
			test.Fatal(err)
		}
	})
	playback, err := queryAs(ownerId)
	if err != nil || playback == nil || playback.TimelineStartMS != 9000 || playback.PlaybackPath != api.Prefix+"/recordings/media/"+media.ID+"/playback" {
		test.Fatal("owned query lost path or timeline", playback, err)
	}
	etag := "\"" + digestText + "\""
	for _, testCase := range []struct {
		method         string
		rangeHeader    string
		ifRange        string
		responseStatus int
		bodyBytes      []byte
	}{
		{http.MethodGet, "", "", http.StatusOK, content},
		{http.MethodHead, "bytes=3-8", "", http.StatusOK, nil},
		{http.MethodGet, "bytes=3-8", etag, http.StatusPartialContent, content[3:9]},
		{http.MethodGet, "bytes=3-8", "\"old\"", http.StatusOK, content},
		{http.MethodGet, "bytes=-4", "", http.StatusPartialContent, content[60:]},
	} {
		response := httptest.NewRecorder()
		resolver.recordingPlaybackView(response, requestAs("playback-owner", testCase.method, testCase.rangeHeader, testCase.ifRange))
		if response.Code != testCase.responseStatus || !bytes.Equal(response.Body.Bytes(), testCase.bodyBytes) || response.Header().Get("ETag") != etag || response.Header().Get("Content-Type") != "audio/wav" || response.Header().Get("Cache-Control") != "private, no-store" {
			test.Fatal("preview range or headers changed", response.Code, response.Header())
		}
		if testCase.method == http.MethodHead && response.Header().Get("Content-Length") != strconv.Itoa(len(content)) {
			test.Fatal("HEAD omitted full size")
		}
	}
	for _, testCase := range []struct {
		username       string
		rangeHeader    string
		responseStatus int
	}{
		{"", "", http.StatusUnauthorized}, {"playback-other", "", http.StatusNotFound}, {"playback-owner", "bytes=0-1,3-4", http.StatusRequestedRangeNotSatisfiable},
	} {
		response := httptest.NewRecorder()
		resolver.recordingPlaybackView(response, requestAs(testCase.username, http.MethodGet, testCase.rangeHeader, ""))
		if response.Code != testCase.responseStatus {
			test.Fatal("preview authorization or range failed", response.Code)
		}
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		if err := transaction.DeleteRecording(ownerId, media.RecordingID); err != nil {
			test.Fatal(err)
		}
	})
	response = httptest.NewRecorder()
	resolver.recordingPlaybackView(response, requestAs("playback-owner", http.MethodGet, "", ""))
	if response.Code != http.StatusGone {
		test.Fatal("deleted recording preview remained accessible", response.Code)
	}
}
