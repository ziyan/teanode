package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

type recordingDownloadTransport func(*http.Request) (*http.Response, error)

func (self recordingDownloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}
func TestRecordingDownloadNeverReplacesExistingDestination(test *testing.T) {
	original := []byte("fixture original")
	digest := sha256.Sum256(original)
	media := &models.RecordingMedia{ID: "fixture-media", OriginalSizeBytes: int64(len(original)), OriginalSHA256: hex.EncodeToString(digest[:]), RecordingUploadState: models.RecordingUploadComplete}
	for _, hasExistingDestination := range []bool{false, true} {
		for _, isCorrupt := range []bool{false, true} {
			directory := test.TempDir()
			destinationPath := filepath.Join(directory, "original.wav")
			existingBytes := []byte("fixture retained destination")
			if hasExistingDestination {
				if err := os.WriteFile(destinationPath, existingBytes, 0600); err != nil {
					test.Fatal(err)
				}
			}
			responseBytes := original
			if isCorrupt {
				responseBytes = []byte("fixture corrupt!")
			}
			connection, err := client.New(client.Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: recordingDownloadTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(responseBytes))}, nil
			})}})
			if err != nil {
				test.Fatal(err)
			}
			err = downloadRecordingToFile(test.Context(), connection, media, destinationPath)
			if (err == nil) != (!hasExistingDestination && !isCorrupt) {
				test.Fatal("unexpected publication outcome", err)
			}
			storedBytes, readError := os.ReadFile(destinationPath)
			if hasExistingDestination {
				if readError != nil || !bytes.Equal(storedBytes, existingBytes) {
					test.Fatal("existing destination changed", readError)
				}
			} else if isCorrupt {
				if !os.IsNotExist(readError) {
					test.Fatal("corrupt original was published")
				}
			} else if readError != nil || !bytes.Equal(storedBytes, original) {
				test.Fatal("verified original was not published", readError)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				test.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "original.wav" {
					test.Fatal("failed download retained temporary bytes")
				}
			}
		}
	}
}
