package client

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestAPhotoFilterSendsOnlyWhatIsAsked(test *testing.T) {
	var none *PhotoFilter
	if variables := none.variables(); len(variables) != 0 {
		test.Fatalf("%v", variables)
	}
	isFavorite := false
	after := time.Date(2025, 7, 12, 3, 0, 0, 0, time.FixedZone("", 3600))
	latitude, longitude, distance := 10.0, 20.0, 5.0
	variables := (&PhotoFilter{
		BeforePhotoID: "photo-9", Offset: 0, PageSize: 25, IsFavorite: &isFavorite, IsTrashed: true, PhotoKind: models.PhotoKindVideo, TakenAfter: &after,
		SearchText: "harbor", NearLatitude: &latitude, NearLongitude: &longitude, NearDistanceKilometers: &distance,
	}).variables()
	want := map[string]any{
		"beforePhotoId": "photo-9", "pageSize": 25, "isFavorite": false, "isTrashed": true, "photoKind": "video",
		"takenAfter": "2025-07-12T02:00:00Z", "searchText": "harbor", "nearLatitude": 10.0, "nearLongitude": 20.0, "nearDistanceKilometers": 5.0,
	}
	if len(variables) != len(want) {
		test.Fatalf("%v", variables)
	}
	for name, value := range want {
		if variables[name] != value {
			test.Errorf("%s is %v, want %v", name, variables[name], value)
		}
	}
}

func TestDownloadingAPhotoFileChecksThatAllOfItArrived(test *testing.T) {
	content := bytes.Repeat([]byte("photo"), 100)
	var served []byte
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer fixture-token" || request.URL.Path != "/api/v1/photos/photo-1/files/original" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(served)
	}))
	defer server.Close()
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}
	file := &models.PhotoFile{PhotoID: "photo-1", PhotoFileRole: models.PhotoFileRoleOriginal, FileSizeBytes: int64(len(content))}

	served = content
	var destination bytes.Buffer
	if err := connection.DownloadPhotoFile(test.Context(), file, &destination); err != nil || !bytes.Equal(destination.Bytes(), content) {
		test.Fatalf("%v", err)
	}
	served = content[:len(content)-1]
	destination.Reset()
	if err := connection.DownloadPhotoFile(test.Context(), file, &destination); err == nil || !strings.Contains(err.Error(), "bytes of a file of") {
		test.Fatalf("a short file was taken for the photo: %v", err)
	}
	other := *file
	other.PhotoID = "photo-2"
	if err := connection.DownloadPhotoFile(test.Context(), &other, &destination); err == nil {
		test.Fatal("a photo that is not there was downloaded")
	}
}

func TestAReadOnlyClientDoesNotUpload(test *testing.T) {
	connection, err := New(Options{URL: "http://127.0.0.1:1", Token: "t", ReadOnly: true})
	if err != nil {
		test.Fatal(err)
	}
	upload := &PhotoUpload{Source: strings.NewReader("abc"), SizeBytes: 3, ClientPhotoID: "abc"}
	if _, err := connection.UploadPhoto(test.Context(), upload); err == nil {
		test.Fatal("a read-only connection uploaded")
	} else if _, isReadOnly := err.(*ReadOnlyError); !isReadOnly {
		test.Fatalf("%v", err)
	}
	if _, err := connection.UploadPhoto(test.Context(), &PhotoUpload{}); err == nil {
		test.Fatal("an empty upload was accepted")
	}
}

func TestPhotoSizesAndOffsetsAreWrittenAsTheDashboardWritesThem(test *testing.T) {
	for sizeBytes, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1 KB", 1536: "1.5 KB", 1572864: "1.5 MB", 8 << 30: "8 GB", 2000: "2 KB"} {
		if got := PhotoByteSizeText(sizeBytes); got != want {
			test.Errorf("%d bytes is %q, want %q", sizeBytes, got, want)
		}
	}
	for offsetMinutes, want := range map[int]string{540: "UTC+09:00", -210: "UTC-03:30", 0: "UTC+00:00"} {
		if got := PhotoOffsetText(offsetMinutes); got != want {
			test.Errorf("%d minutes is %q, want %q", offsetMinutes, got, want)
		}
	}
	limited := &PhotoLibrary{UsedBytes: 1536, MaximumAccountBytes: 5 << 30}
	if got := limited.SpaceUsedText(); got != "1.5 KB of 5 GB" {
		test.Errorf("%q", got)
	}
	if got := (&PhotoLibrary{UsedBytes: 1536}).SpaceUsedText(); got != "1.5 KB · No limit" {
		test.Errorf("%q", got)
	}
}
