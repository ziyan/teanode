package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingFileImportResumesAndPreservesSource(test *testing.T) {
	original := bytes.Repeat([]byte{31, 72, 19}, int(models.RecordingUploadPartBytes/3)+2)
	digest := sha256.Sum256(original)
	media := &models.RecordingMedia{ID: "fixture-media", RecordingID: "fixture-recording", OriginalSizeBytes: int64(len(original)), OriginalSHA256: hex.EncodeToString(digest[:]), PartCount: 2, OriginalFilename: "fixture.wav", OriginalContentType: "audio/wav"}
	recording := &models.Recording{ID: media.RecordingID, UserID: "fixture-owner", RecordingTitle: "fixture meeting"}
	receipts := []*models.RecordingMediaPart{}
	partRequestCounts := make(map[int]int)
	hasLostResponse := false
	createMutationCount := 0
	transport := recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
		var answered any
		switch {
		case request.URL.Path == "/api/v1/graphql":
			var envelope struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
				test.Fatal(err)
			}
			operationName := ""
			for _, candidate := range []string{"ReadRecordingLimits", "CreateRecordingMedia", "CreateRecording", "ReadRecording"} {
				if strings.Contains(envelope.Query, candidate+"(") || strings.Contains(envelope.Query, candidate+" {") {
					operationName = candidate
					break
				}
			}
			switch operationName {
			case "ReadRecordingLimits":
				answered = map[string]any{operationName: RecordingLimits{MaximumMediaBytes: 32 << 20, UploadPartBytes: models.RecordingUploadPartBytes}}
			case "CreateRecording":
				createMutationCount++
				if envelope.Variables["clientRecordingId"] != "file-"+media.OriginalSHA256 {
					test.Fatal("import identity changed")
				}
				answered = map[string]any{operationName: recording}
			case "CreateRecordingMedia":
				createMutationCount++
				answered = map[string]any{operationName: media}
			case "ReadRecording":
				answered = map[string]any{operationName: RecordingView{Recording: recording, MediaFiles: []*models.RecordingMedia{media}, UploadParts: receipts}}
			default:
				test.Fatalf("unexpected query %s", envelope.Query)
			}
			answered = map[string]any{"data": answered}
		case strings.Contains(request.URL.Path, "/parts/"):
			partIndex := 0
			if strings.HasSuffix(request.URL.Path, "/1") {
				partIndex = 1
			}
			partRequestCounts[partIndex]++
			content, err := io.ReadAll(request.Body)
			if err != nil {
				test.Fatal(err)
			}
			byteOffset := int64(partIndex) * models.RecordingUploadPartBytes
			if !bytes.Equal(content, original[byteOffset:min(byteOffset+models.RecordingUploadPartBytes, int64(len(original)))]) {
				test.Fatal("original bytes changed")
			}
			part := &models.RecordingMediaPart{RecordingMediaID: media.ID, PartIndex: partIndex, PartSizeBytes: int64(len(content)), PartSHA256: request.Header.Get("X-Recording-Part-SHA256"), StoredAt: time.Now()}
			receipts = append(receipts, part)
			if partIndex == 1 && !hasLostResponse {
				hasLostResponse = true
				return nil, fmt.Errorf("fixture response lost after durable upload")
			}
			answered = part
		case strings.HasSuffix(request.URL.Path, "/complete"):
			verifiedAt := time.Now()
			media.VerifiedAt = &verifiedAt
			media.RecordingUploadState = models.RecordingUploadComplete
			answered = media
		default:
			test.Fatal("unexpected request")
		}
		content, err := json.Marshal(answered)
		if err != nil {
			test.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(content))}, nil
	})
	connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		test.Fatal(err)
	}
	source := bytes.NewReader(original)
	if _, err := connection.ImportRecordingFile(test.Context(), source, "fixture.wav", "audio/wav", recording.RecordingTitle, ""); err == nil {
		test.Fatal("lost upload response looked successful")
	}
	view, err := connection.ImportRecordingFile(test.Context(), source, "fixture.wav", "audio/wav", recording.RecordingTitle, "")
	if err != nil {
		test.Fatal(err)
	}
	if view.MediaFiles[0].VerifiedAt == nil || partRequestCounts[0] != 1 || partRequestCounts[1] != 1 || source.Size() != int64(len(original)) {
		test.Fatal("resume resent verified parts or lost the original")
	}
	beforeResumeMutationCount := createMutationCount
	if _, err := connection.ResumeRecordingFile(test.Context(), source, media); err != nil {
		test.Fatal(err)
	}
	wrongManifest := *media
	wrongManifest.OriginalSHA256 = "mismatched-digest"
	if _, err := connection.ResumeRecordingFile(test.Context(), source, &wrongManifest); err == nil {
		test.Fatal("resumed a different original")
	}
	if createMutationCount != beforeResumeMutationCount {
		test.Fatal("resume created another recording or original")
	}

}

func TestRecordingDownloadRejectsCorruptionAndOversize(test *testing.T) {
	original := []byte("fixture original")
	digest := sha256.Sum256(original)
	media := &models.RecordingMedia{ID: "fixture-media", OriginalSizeBytes: int64(len(original)), OriginalSHA256: hex.EncodeToString(digest[:]), RecordingUploadState: models.RecordingUploadComplete}
	for _, responseBytes := range [][]byte{original, []byte("fixture corrupt!"), append(append([]byte{}, original...), 42)} {
		connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(responseBytes)), Header: http.Header{}}, nil
		})}})
		if err != nil {
			test.Fatal(err)
		}
		var downloaded bytes.Buffer
		err = connection.DownloadRecordingMedia(test.Context(), media, &downloaded)
		if (err == nil) != bytes.Equal(original, responseBytes) {
			test.Fatal("download integrity was not enforced", err)
		}
	}
}
