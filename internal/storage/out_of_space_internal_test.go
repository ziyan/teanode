package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type codedFixtureError struct{ code string }

func (self *codedFixtureError) Error() string     { return "api error " + self.code }
func (self *codedFixtureError) ErrorCode() string { return self.code }

type statusFixtureError struct{ statusCode int }

func (self *statusFixtureError) Error() string {
	return fmt.Sprintf("response error StatusCode: %d", self.statusCode)
}
func (self *statusFixtureError) HTTPStatusCode() int { return self.statusCode }

func TestAFullDiskOrObjectStoreIsToldApartFromOtherFailures(test *testing.T) {
	for _, testCase := range []struct {
		name         string
		err          error
		isOutOfSpace bool
	}{
		{"a full file system", &fs.PathError{Op: "write", Path: "spool/fixture", Err: syscall.ENOSPC}, true},
		{"an object store naming the error", fmt.Errorf("operation error S3: PutObject: %w", &codedFixtureError{code: "XMinioStorageFull"}), true},
		{"an object store answering 507", fmt.Errorf("operation error S3: PutObject: %w", &statusFixtureError{statusCode: http.StatusInsufficientStorage}), true},
		{"an object store that cannot be reached", errors.New("request send failed"), false},
		{"an object store answering 500", &statusFixtureError{statusCode: http.StatusInternalServerError}, false},
		{"another object store error", &codedFixtureError{code: "SlowDown"}, false},
		{"no error", nil, false},
	} {
		if isOutOfSpace := IsOutOfSpace(testCase.err); isOutOfSpace != testCase.isOutOfSpace {
			test.Errorf("%s: IsOutOfSpace = %v, want %v", testCase.name, isOutOfSpace, testCase.isOutOfSpace)
		}
	}
}

// fullObjectStore answers every request the way MinIO does when its drives
// are full.
type fullObjectStore struct{}

func (fullObjectStore) RoundTrip(request *http.Request) (*http.Response, error) {
	body := `<Error><Code>XMinioStorageFull</Code><Message>Storage backend has reached its minimum free drive threshold.</Message></Error>`
	return &http.Response{StatusCode: http.StatusInsufficientStorage, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func TestAnObjectStoreWithFullDrivesIsOutOfSpaceThroughTheSDK(test *testing.T) {
	client := s3.New(s3.Options{
		Region: "test-region", Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
		HTTPClient: &http.Client{Transport: fullObjectStore{}}, RetryMaxAttempts: 1,
	})
	objectStore := &s3Storage{settings: &S3Settings{Bucket: "fixture-bucket"}, client: client}
	err := objectStore.putObject(context.Background(), "media/fixture", []byte("fixture"))
	if err == nil || !IsOutOfSpace(fmt.Errorf("storage: cannot upload fixture: %w", err)) {
		test.Fatalf("IsOutOfSpace(%v) = false, want true", err)
	}
}
