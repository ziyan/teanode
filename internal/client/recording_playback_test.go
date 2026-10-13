package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func playbackFixture(content []byte) *RecordingPlayback {
	digest := sha256.Sum256(content)
	return &RecordingPlayback{RecordingMediaID: "fixture-media", PlaybackPath: RecordingPlaybackPath("fixture-media"), PlaybackContentType: "audio/wav", PlaybackSizeBytes: int64(len(content)), PlaybackSHA256: hex.EncodeToString(digest[:]), PlaybackDurationMS: 1000, TimelineStartMS: 4000}
}

func TestPlaybackReceiptsCannotRedirectOwnedDownloads(test *testing.T) {
	for _, mismatch := range []string{"media", "path", "content-type", "size", "digest", "duration", "offset", "null"} {
		test.Run(mismatch, func(test *testing.T) {
			playback := playbackFixture([]byte("fixture compatible audio"))
			switch mismatch {
			case "media":
				playback.RecordingMediaID = "other-media"
			case "path":
				playback.PlaybackPath = "https://example.net/foreign-preview"
			case "content-type":
				playback.PlaybackContentType = "text/html"
			case "size":
				playback.PlaybackSizeBytes = -1
			case "digest":
				playback.PlaybackSHA256 = "not-a-digest"
			case "duration":
				playback.PlaybackDurationMS = 0
			case "offset":
				playback.TimelineStartMS = -1
			case "null":
				playback = nil
			}
			downloadRequestCount := 0
			connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodGet {
					downloadRequestCount++
					test.Fatal("invalid preview reached the byte route")
				}
				responseJSON, err := json.Marshal(map[string]any{"data": map[string]any{"PrepareRecordingPlayback": playback}})
				if err != nil {
					test.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(responseJSON))}, nil
			})}})
			if err != nil {
				test.Fatal(err)
			}
			if _, err := PrepareRecordingPlayback(test.Context(), connection, "fixture-media"); err == nil || downloadRequestCount != 0 {
				test.Fatal("mismatched preview was accepted")
			}
		})
	}
}

func TestReadOnlyAllowsReadyPlaybackButNeverPreparesIt(test *testing.T) {
	content := []byte("fixture compatible audio")
	playback := playbackFixture(content)
	requestCount := 0
	connection, err := New(Options{URL: "https://example.com", Token: "fixture-token", ReadOnly: true, HTTPClient: &http.Client{Transport: recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
		requestCount++
		if request.Header.Get("Authorization") != "Bearer fixture-token" {
			test.Fatal("preview lost owned authentication")
		}
		responseBytes := content
		if request.Method == http.MethodPost {
			requestBytes, _ := io.ReadAll(request.Body)
			if strings.Contains(string(requestBytes), "PrepareRecordingPlayback") {
				test.Fatal("read-only prepare reached the transport")
			}
			responseBytes, _ = json.Marshal(map[string]any{"data": map[string]any{"ReadRecordingPlayback": playback}})
		} else if request.URL.Path != RecordingPlaybackPath("fixture-media") {
			test.Fatal("preview downloaded from a different path")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(responseBytes))}, nil
	})}})
	if err != nil {
		test.Fatal(err)
	}
	ready, err := ReadRecordingPlayback(test.Context(), connection, "fixture-media")
	if err != nil {
		test.Fatal(err)
	}
	var downloaded bytes.Buffer
	if err := connection.DownloadRecordingPlayback(test.Context(), ready, &downloaded); err != nil || !bytes.Equal(content, downloaded.Bytes()) {
		test.Fatal("ready audio could not be read", err)
	}
	_, err = PrepareRecordingPlayback(test.Context(), connection, "fixture-media")
	var readOnlyError *ReadOnlyError
	if !errors.As(err, &readOnlyError) || requestCount != 2 {
		test.Fatal("prepare was not refused before transport", err)
	}
}

func TestPlaybackDownloadChecksSizeAndDigest(test *testing.T) {
	content := []byte("fixture compatible audio")
	for _, responseBytes := range [][]byte{content, []byte("short"), append(append([]byte{}, content...), 42), bytes.Repeat([]byte{42}, len(content))} {
		connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(responseBytes))}, nil
		})}})
		if err != nil {
			test.Fatal(err)
		}
		var downloaded bytes.Buffer
		err = connection.DownloadRecordingPlayback(test.Context(), playbackFixture(content), &downloaded)
		if (err == nil) != bytes.Equal(content, responseBytes) {
			test.Fatal("preview integrity was not checked", err)
		}
	}
}
