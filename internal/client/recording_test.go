package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingUploadsRefuseReadOnlyAndMismatchedReceipts(test *testing.T) {
	originalBytes := []byte{0, 255, 13, 10, 42}
	digest := sha256.Sum256(originalBytes)
	originalSha256 := hex.EncodeToString(digest[:])
	verifiedAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	expected := &models.RecordingMedia{ID: "fixture-media", RecordingID: "fixture-recording", OriginalSizeBytes: int64(len(originalBytes)), OriginalSHA256: originalSha256}
	for _, isReadOnly := range []bool{false, true} {
		for _, hasMatchingReceipt := range []bool{false, true} {
			requestCount := 0
			transport := recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
				requestCount++
				content, err := io.ReadAll(request.Body)
				if err != nil {
					test.Fatal(err)
				}
				var receipt any
				switch request.Method {
				case http.MethodPut:
					if !bytes.Equal(content, originalBytes) || request.ContentLength != int64(len(originalBytes)) || request.Header.Get("X-Recording-Part-SHA256") != originalSha256 || request.Header.Get("Content-Type") != "application/octet-stream" {
						test.Fatal("original part changed on the client")
					}
					part := &models.RecordingMediaPart{RecordingMediaID: expected.ID, PartIndex: 0, PartSizeBytes: expected.OriginalSizeBytes, PartSHA256: originalSha256, StoredAt: verifiedAt}
					if !hasMatchingReceipt {
						part.PartSHA256 = "mismatched-digest"
					}
					receipt = part
				case http.MethodPost:
					if request.Header.Get("Content-Type") != "application/json" {
						test.Fatal("completion lost cookie protection")
					}
					completed := *expected
					completed.RecordingUploadState = models.RecordingUploadComplete
					completed.VerifiedAt = &verifiedAt
					if !hasMatchingReceipt {
						completed.OriginalSizeBytes++
					}
					receipt = &completed
				default:
					test.Fatal("unexpected request method")
				}
				responseJSON, err := json.Marshal(receipt)
				if err != nil {
					test.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(responseJSON)), Header: http.Header{}}, nil
			})
			connection, err := New(Options{URL: "https://example.com", ReadOnly: isReadOnly, HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				test.Fatal(err)
			}
			_, partError := connection.PutRecordingPart(test.Context(), expected.ID, 0, originalBytes)
			_, completionError := connection.CompleteRecordingUpload(test.Context(), expected)
			if isReadOnly {
				var readOnlyError *ReadOnlyError
				if requestCount != 0 || !errors.As(partError, &readOnlyError) || !errors.As(completionError, &readOnlyError) {
					test.Fatal("read-only uploads reached the transport")
				}
			} else if hasMatchingReceipt {
				if partError != nil || completionError != nil {
					test.Fatal("valid durable receipts were rejected", partError, completionError)
				}
			} else if partError == nil || completionError == nil {
				test.Fatal("client accepted mismatched durable receipts")
			}
		}
	}
}
