package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestStreamedFilesPreserveBytesRangesAndPriorFilesOnFailure(test *testing.T) {
	spool, err := Open(&Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	streaming := spool.(StreamFiles)
	mediaBytes := bytes.Repeat([]byte("original-media"), 10000)
	if err := streaming.PutFileStream(test.Context(), "fixture-media", bytes.NewReader(mediaBytes), int64(len(mediaBytes))); err != nil {
		test.Fatal(err)
	}
	portion, err := streaming.OpenFileRange(test.Context(), "fixture-media", 11, 29)
	if err != nil {
		test.Fatal(err)
	}
	portionBytes, err := io.ReadAll(portion)
	_ = portion.Close()
	if err != nil || portion.SizeBytes != 29 || !bytes.Equal(portionBytes, mediaBytes[11:40]) {
		test.Fatal("range changed the media")
	}
	if err := streaming.PutFileStream(test.Context(), "fixture-media", strings.NewReader("short"), 100); err == nil {
		test.Fatal("a size mismatch was published")
	}
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	if err := streaming.PutFileStream(ctx, "fixture-media", strings.NewReader("replacement"), 11); !errors.Is(err, context.Canceled) {
		test.Fatal("a canceled write was published")
	}
	storedBytes, err := spool.GetFile(test.Context(), "fixture-media")
	if err != nil || !bytes.Equal(storedBytes, mediaBytes) {
		test.Fatal("failed writes replaced the durable original")
	}
	filesystemStore := spool.(*filesystem)
	filename, err := filesystemStore.filePath("fixture-media")
	if err != nil {
		test.Fatal(err)
	}
	metadata, err := os.Stat(filename)
	if err != nil || metadata.Mode().Perm() != 0600 {
		test.Fatal("streamed originals are not private")
	}
	if _, err := streaming.OpenFileRange(test.Context(), "../escape", 0, -1); err == nil {
		test.Fatal("an identifier escaped blob storage")
	}
	if _, err := streaming.OpenFileRange(test.Context(), "fixture-media", int64(len(mediaBytes)), 1); err == nil {
		test.Fatal("a range exceeded the original")
	}
}

type streamTransport func(*http.Request) (*http.Response, error)

func (self streamTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return self(request)
}

func TestObjectStoreStreamsTheOriginalAndRequestsOnlyThePlaybackRange(test *testing.T) {
	mediaBytes := []byte("unchanged-original-media")
	transport := streamTransport(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPut {
			content, err := io.ReadAll(request.Body)
			if err != nil || !bytes.Equal(content, mediaBytes) {
				test.Fatal("object-store upload changed original bytes")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if request.Method != http.MethodGet || request.Header.Get("Range") != "bytes=3-8" {
			test.Fatal("playback did not request the bounded range")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Length": []string{"6"}, "Content-Range": []string{"bytes 3-8/24"}}, ContentLength: 6, Body: io.NopCloser(bytes.NewReader(mediaBytes[3:9]))}, nil
	})
	client := s3.New(s3.Options{Region: "test-region", Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""), HTTPClient: &http.Client{Transport: transport}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired})
	spool := &s3Storage{settings: &S3Settings{Bucket: "fixture-bucket"}, client: client}
	if err := spool.PutFileStream(test.Context(), "fixture-media", bytes.NewReader(mediaBytes), int64(len(mediaBytes))); err != nil {
		test.Fatal(err)
	}
	portion, err := spool.OpenFileRange(test.Context(), "fixture-media", 3, 6)
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = portion.Close() }()
	content, err := io.ReadAll(portion)
	if err != nil || portion.SizeBytes != 6 || !bytes.Equal(content, mediaBytes[3:9]) {
		test.Fatal("object-store playback changed the original")
	}
}

type recordingMirror struct {
	objectStore
	deletionError error
	deletionCount int
}

func (self *recordingMirror) DeleteFile(context.Context, string) error {
	self.deletionCount++
	return self.deletionError
}
func (*recordingMirror) Close() error { return nil }

func TestRecordingDeletionRetriesTheMirrorAfterLocalBytesAreGone(test *testing.T) {
	spool, err := Open(&Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = spool.Close() }()
	if err := spool.PutFile(test.Context(), "fixture-original", []byte("raw-media")); err != nil {
		test.Fatal(err)
	}
	filesystemStore := spool.(*filesystem)
	mirror := &recordingMirror{deletionError: errors.New("fixture mirror unavailable")}
	filesystemStore.mirror = mirror
	if err := filesystemStore.DeleteFileEverywhere(test.Context(), "fixture-original"); !errors.Is(err, mirror.deletionError) {
		test.Fatal("mirror failure was hidden", err)
	}
	filename, err := filesystemStore.filePath("fixture-original")
	if err != nil {
		test.Fatal(err)
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		test.Fatal("local deletion failed", err)
	}
	mirror.deletionError = nil
	if err := filesystemStore.DeleteFileEverywhere(test.Context(), "fixture-original"); err != nil || mirror.deletionCount != 2 {
		test.Fatal("missing local bytes prevented a mirror retry", err)
	}
}

func TestObjectStoreRefusesAnIgnoredOrDifferentRange(test *testing.T) {
	for _, contentRange := range []string{"", "bytes 0-5/24"} {
		transport := streamTransport(func(*http.Request) (*http.Response, error) {
			headers := http.Header{"Content-Length": []string{"6"}}
			if contentRange != "" {
				headers.Set("Content-Range", contentRange)
			}
			return &http.Response{StatusCode: 206, Header: headers, ContentLength: 6, Body: io.NopCloser(strings.NewReader("abcdef"))}, nil
		})
		client := s3.New(s3.Options{Region: "test-region", Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""), HTTPClient: &http.Client{Transport: transport}})
		spool := &s3Storage{settings: &S3Settings{Bucket: "fixture-bucket"}, client: client}
		if stream, err := spool.OpenFileRange(test.Context(), "fixture-media", 3, 6); err == nil {
			_ = stream.Close()
			test.Fatal("object store returned bytes from a different range")
		}
	}
}
