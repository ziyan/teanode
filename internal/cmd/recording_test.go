package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

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

func TestCompatiblePlaybackDownloadPreservesDestinationOnIntegrityFailure(test *testing.T) {
	content := []byte("fixture compatible audio")
	digest := sha256.Sum256(content)
	playback := &client.RecordingPlayback{RecordingMediaID: "fixture-media", PlaybackPath: client.RecordingPlaybackPath("fixture-media"), PlaybackContentType: "audio/wav", PlaybackSizeBytes: int64(len(content)), PlaybackSHA256: hex.EncodeToString(digest[:]), PlaybackDurationMS: 1000}
	for _, hasExistingDestination := range []bool{false, true} {
		for _, isCorrupt := range []bool{false, true} {
			directory := test.TempDir()
			destinationPath := filepath.Join(directory, "compatible.wav")
			existingBytes := []byte("fixture retained destination")
			if hasExistingDestination {
				if err := os.WriteFile(destinationPath, existingBytes, 0600); err != nil {
					test.Fatal(err)
				}
			}
			responseBytes := content
			if isCorrupt {
				responseBytes = []byte("fixture corrupt audio")
			}
			connection, err := client.New(client.Options{URL: "https://example.com", ReadOnly: true, HTTPClient: &http.Client{Transport: recordingDownloadTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != client.RecordingPlaybackPath("fixture-media") {
					test.Fatal("preview downloaded from original route")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(responseBytes))}, nil
			})}})
			if err != nil {
				test.Fatal(err)
			}
			err = downloadRecordingPlaybackToFile(test.Context(), connection, playback, destinationPath)
			if (err == nil) != (!hasExistingDestination && !isCorrupt) {
				test.Fatal("unexpected preview publication outcome", err)
			}
			storedBytes, readError := os.ReadFile(destinationPath)
			if hasExistingDestination {
				if readError != nil || !bytes.Equal(storedBytes, existingBytes) {
					test.Fatal("existing destination changed")
				}
			} else if isCorrupt {
				if !os.IsNotExist(readError) {
					test.Fatal("corrupt compatible audio was published")
				}
			} else if readError != nil || !bytes.Equal(storedBytes, content) {
				test.Fatal("verified compatible audio was not published")
			}
		}
	}
}

func TestRecordingSpeakerConfirmOffersMeAndImportNeedsNoTitle(test *testing.T) {
	var speakerConfirm *cli.Command
	for _, command := range recordingActionCommands() {
		if command.Name == "speaker-confirm" {
			speakerConfirm = command
		}
	}
	if speakerConfirm == nil {
		test.Fatal("there is no speaker-confirm command")
	}
	var temporaryName *cli.Command
	for _, command := range recordingActionCommands() {
		if command.Name == "temporary-speaker-name" {
			temporaryName = command
		}
	}
	if temporaryName == nil {
		test.Fatal("there is no temporary-speaker-name command")
	}
	flagNames := map[string]bool{}
	for _, flag := range temporaryName.Flags {
		flagNames[flag.Names()[0]] = true
	}
	for _, expected := range []string{"recording-id", "speaker-label", "confirmed-name", "me"} {
		if !flagNames[expected] {
			test.Fatal("temporary-speaker-name has no --" + expected + " flag")
		}
	}
	var separate *cli.Command
	for _, command := range recordingActionCommands() {
		if command.Name == "speaker-separate" {
			separate = command
		}
	}
	if separate == nil {
		test.Fatal("there is no speaker-separate command")
	}
	separateFlagNames := map[string]bool{}
	for _, flag := range separate.Flags {
		separateFlagNames[flag.Names()[0]] = true
	}
	for _, expected := range []string{"recording-id", "speaker-label"} {
		if !separateFlagNames[expected] {
			test.Fatal("speaker-separate has no --" + expected + " flag")
		}
	}
	hasMe := false
	for _, flag := range speakerConfirm.Flags {
		if flag.Names()[0] == "me" {
			_, isBoolean := flag.(*cli.BoolFlag)
			hasMe = isBoolean
		}
	}
	if !hasMe {
		test.Fatal("speaker-confirm has no --me flag")
	}
	for _, command := range NewRecordingCommand().Commands {
		if command.Name != "import" {
			continue
		}
		for _, flag := range command.Flags {
			if flag.Names()[0] == "title" && !strings.Contains(flag.(*cli.StringFlag).Usage, "optional") {
				test.Fatal("the import title is not optional")
			}
		}
	}
}

func TestRecordingSpeakerContactCommandTiesOrUntiesAndNamingCommandsTakeAContact(test *testing.T) {
	flagsOf := func(commandName string) map[string]bool {
		for _, command := range recordingActionCommands() {
			if command.Name != commandName {
				continue
			}
			flagNames := map[string]bool{}
			for _, flag := range command.Flags {
				flagNames[flag.Names()[0]] = true
			}
			return flagNames
		}
		test.Fatalf("there is no %s command", commandName)
		return nil
	}
	for commandName, expectedFlags := range map[string][]string{
		"speaker-contact":        {"recording-id", "expected-source-generation", "speaker-identity-id", "contact", "none"},
		"speaker-confirm":        {"contact"},
		"temporary-speaker-name": {"contact"},
	} {
		flagNames := flagsOf(commandName)
		for _, expected := range expectedFlags {
			if !flagNames[expected] {
				test.Errorf("%s has no --%s flag", commandName, expected)
			}
		}
	}
}
