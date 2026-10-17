package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeMultipartObjectStore answers the requests of S3's multipart upload and
// keeps what it was sent, so a test can see which calls were made and in what
// order, and that the pieces add up to the file.
type fakeMultipartObjectStore struct {
	mutex          sync.Mutex
	calls          []string
	pieces         map[int][]byte
	completedOrder []int
	failPartNumber int
	putObjectBody  []byte

	// onPart runs when the first part arrives, to cancel the caller.
	onPart func()
}

func (self *fakeMultipartObjectStore) RoundTrip(request *http.Request) (*http.Response, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	query := request.URL.Query()
	respond := func(status int, body string, headers ...string) (*http.Response, error) {
		response := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
		for index := 0; index+1 < len(headers); index += 2 {
			response.Header.Set(headers[index], headers[index+1])
		}
		return response, nil
	}
	switch {
	case request.Method == http.MethodPost && query.Has("uploads"):
		self.calls = append(self.calls, "create")
		return respond(200, `<InitiateMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>k</Key><UploadId>fixture-upload</UploadId></InitiateMultipartUploadResult>`)
	case request.Method == http.MethodPut && query.Has("partNumber"):
		var partNumber int
		_, _ = fmt.Sscan(query.Get("partNumber"), &partNumber)
		self.calls = append(self.calls, fmt.Sprintf("part %d", partNumber))
		if query.Get("uploadId") != "fixture-upload" {
			return respond(404, "<Error><Code>NoSuchUpload</Code></Error>")
		}
		if self.onPart != nil {
			self.onPart()
		}
		if partNumber == self.failPartNumber {
			return respond(500, "<Error><Code>InternalError</Code><Message>fixture failure</Message></Error>")
		}
		content, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		self.pieces[partNumber] = content
		return respond(200, "", "ETag", fmt.Sprintf(`"etag-%d"`, partNumber))
	case request.Method == http.MethodPost && query.Has("uploadId"):
		self.calls = append(self.calls, "complete")
		var completed struct {
			Parts []struct {
				PartNumber int
				ETag       string
			} `xml:"Part"`
		}
		content, _ := io.ReadAll(request.Body)
		if err := xml.Unmarshal(content, &completed); err != nil {
			return respond(400, "<Error><Code>MalformedXML</Code></Error>")
		}
		for _, part := range completed.Parts {
			if part.ETag != fmt.Sprintf(`"etag-%d"`, part.PartNumber) {
				return respond(400, "<Error><Code>InvalidPart</Code></Error>")
			}
			self.completedOrder = append(self.completedOrder, part.PartNumber)
		}
		return respond(200, `<CompleteMultipartUploadResult><Bucket>fixture-bucket</Bucket><Key>k</Key><ETag>"whole"</ETag></CompleteMultipartUploadResult>`)
	case request.Method == http.MethodDelete && query.Has("uploadId"):
		self.calls = append(self.calls, "abort")
		return respond(204, "")
	case request.Method == http.MethodPut:
		self.calls = append(self.calls, "put")
		self.putObjectBody, _ = io.ReadAll(request.Body)
		return respond(200, "")
	}
	return nil, fmt.Errorf("fixture object store did not expect %s %s", request.Method, request.URL)
}

func newFakeMultipartStorage(fake *fakeMultipartObjectStore) *s3Storage {
	fake.pieces = map[int][]byte{}
	client := s3.New(s3.Options{
		Region: "test-region", Credentials: credentials.NewStaticCredentialsProvider("test-key", "test-secret", ""),
		HTTPClient:                 &http.Client{Transport: fake},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		// A fake has no use for a retry that would hide how often it was asked.
		RetryMaxAttempts: 1,
	})
	return &s3Storage{settings: &S3Settings{Bucket: "fixture-bucket"}, client: client}
}

func withSmallMultipartSizes(test *testing.T, thresholdBytes, partBytes int64) {
	test.Helper()
	previousThreshold, previousPart := s3MultipartThresholdBytes, s3MultipartPartBytes
	s3MultipartThresholdBytes, s3MultipartPartBytes = thresholdBytes, partBytes
	test.Cleanup(func() { s3MultipartThresholdBytes, s3MultipartPartBytes = previousThreshold, previousPart })
}

func TestAFileOverTheThresholdGoesUpInPartsInOrder(test *testing.T) {
	withSmallMultipartSizes(test, 10, 4)
	fake := &fakeMultipartObjectStore{}
	storage := newFakeMultipartStorage(fake)
	content := []byte("0123456789abcdefghij")[:11]
	if err := storage.PutFileStream(test.Context(), "fixture-media", bytes.NewReader(content), int64(len(content))); err != nil {
		test.Fatal(err)
	}
	if got := strings.Join(fake.calls, ","); got != "create,part 1,part 2,part 3,complete" {
		test.Fatalf("calls: %s", got)
	}
	var whole []byte
	for partNumber := 1; partNumber <= 3; partNumber++ {
		whole = append(whole, fake.pieces[partNumber]...)
	}
	if !bytes.Equal(whole, content) || len(fake.pieces[1]) != 4 || len(fake.pieces[3]) != 3 {
		test.Fatalf("the parts %v do not add up to %q", fake.pieces, content)
	}
	if fmt.Sprint(fake.completedOrder) != "[1 2 3]" {
		test.Fatalf("the parts were completed as %v", fake.completedOrder)
	}
}

func TestAFileAtTheThresholdGoesUpInOneRequest(test *testing.T) {
	withSmallMultipartSizes(test, 10, 4)
	fake := &fakeMultipartObjectStore{}
	storage := newFakeMultipartStorage(fake)
	content := []byte("0123456789")
	if err := storage.PutFileStream(test.Context(), "fixture-media", bytes.NewReader(content), int64(len(content))); err != nil {
		test.Fatal(err)
	}
	if got := strings.Join(fake.calls, ","); got != "put" || !bytes.Equal(fake.putObjectBody, content) {
		test.Fatalf("calls: %s", got)
	}
}

func TestAFailedPartAbortsTheUpload(test *testing.T) {
	withSmallMultipartSizes(test, 10, 4)
	fake := &fakeMultipartObjectStore{failPartNumber: 2}
	storage := newFakeMultipartStorage(fake)
	content := []byte("0123456789abc")
	err := storage.PutFileStream(test.Context(), "fixture-media", bytes.NewReader(content), int64(len(content)))
	if err == nil {
		test.Fatal("a failed part was reported as stored")
	}
	if got := strings.Join(fake.calls, ","); got != "create,part 1,part 2,abort" {
		test.Fatalf("calls: %s", got)
	}
}

func TestAnAbandonedContextStillAbortsTheUpload(test *testing.T) {
	withSmallMultipartSizes(test, 10, 4)
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	fake := &fakeMultipartObjectStore{onPart: cancel}
	storage := newFakeMultipartStorage(fake)
	content := []byte("0123456789abc")
	if err := storage.PutFileStream(ctx, "fixture-media", bytes.NewReader(content), int64(len(content))); err == nil {
		test.Fatal("a canceled upload was reported as stored")
	}
	if got := fake.calls[len(fake.calls)-1]; got != "abort" {
		test.Fatalf("the upload was not aborted: %v", fake.calls)
	}
}
