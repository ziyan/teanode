package apigraph

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

// A multipart body of files, the way the compose page sends one.
func multipartOf(test *testing.T, files map[string][]byte) (*bytes.Buffer, string) {
	test.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for name, content := range files {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			test.Fatalf("CreateFormFile: %s", err)
		}
		if _, err := part.Write(content); err != nil {
			test.Fatalf("Write: %s", err)
		}
	}
	if err := writer.Close(); err != nil {
		test.Fatalf("Close: %s", err)
	}
	return body, writer.FormDataContentType()
}

// The limit is on the files together, not on each: two files that each fit
// but together do not are refused, and one that fits exactly is kept.
func TestReadUploadsLimitsTheFilesTogether(test *testing.T) {
	test.Parallel()

	body, contentType := multipartOf(test, map[string][]byte{"a.txt": bytes.Repeat([]byte("a"), 600), "b.txt": bytes.Repeat([]byte("b"), 600)})
	request := httptest.NewRequest("PUT", "/api/v1/mailbox/drafts/x/attachments", body)
	request.Header.Set("Content-Type", contentType)
	if _, err := readUploads(request, 1000); !errors.Is(err, errTooLarge) {
		test.Errorf("1200 bytes of files under a limit of 1000: err = %v, want errTooLarge", err)
	}

	body, contentType = multipartOf(test, map[string][]byte{"exact.txt": bytes.Repeat([]byte("x"), 1000)})
	request = httptest.NewRequest("PUT", "/api/v1/mailbox/drafts/x/attachments", body)
	request.Header.Set("Content-Type", contentType)
	uploads, err := readUploads(request, 1000)
	if err != nil || len(uploads) != 1 || len(uploads[0].Content) != 1000 || uploads[0].Filename != "exact.txt" {
		test.Errorf("a file of exactly the limit: %d uploads, err = %v, want one of 1000 bytes", len(uploads), err)
	}
}

// Parts that are not files, and a body that is not multipart at all, do
// not become attachments.
func TestReadUploadsSkipsWhatIsNotAFile(test *testing.T) {
	test.Parallel()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("note", "not a file"); err != nil {
		test.Fatalf("WriteField: %s", err)
	}
	part, err := writer.CreateFormFile("file", "keep.txt")
	if err != nil {
		test.Fatalf("CreateFormFile: %s", err)
	}
	_, _ = part.Write([]byte("kept"))
	_ = writer.Close()
	request := httptest.NewRequest("POST", "/api/v1/mailbox/m/drafts/attachments", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	uploads, err := readUploads(request, 0)
	if err != nil || len(uploads) != 1 || uploads[0].Filename != "keep.txt" {
		test.Errorf("a field beside a file: %d uploads, err = %v, want the file alone", len(uploads), err)
	}

	request = httptest.NewRequest("POST", "/api/v1/mailbox/m/drafts/attachments", bytes.NewBufferString("{}"))
	request.Header.Set("Content-Type", "application/json")
	if _, err := readUploads(request, 0); err == nil {
		test.Error("a JSON body was read as uploads")
	}
}
