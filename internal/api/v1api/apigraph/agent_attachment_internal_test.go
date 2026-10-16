package apigraph

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A phone's video player asks for a range of bytes, then for more, and
// gives up on a server that answers each with the whole file.
func TestAnAttachmentIsServedByTheByteRangeAskedFor(test *testing.T) {
	attachment := &models.AgentAttachment{ID: "attachment-1", Name: "walkthrough.mp4", ContentType: "video/mp4", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	content := []byte("0123456789")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/attachments/attachment-1", nil)
	request.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	writeAgentAttachment(recorder, request, attachment, content)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "2345" {
		test.Fatalf("a range: %d %q", recorder.Code, recorder.Body.String())
	}
	if contentRange := recorder.Header().Get("Content-Range"); contentRange != "bytes 2-5/10" {
		test.Fatalf("Content-Range: %q", contentRange)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "video/mp4" {
		test.Fatalf("Content-Type: %q", contentType)
	}

	request = httptest.NewRequest(http.MethodHead, "/api/v1/agent/attachments/attachment-1", nil)
	recorder = httptest.NewRecorder()
	writeAgentAttachment(recorder, request, attachment, content)
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 || recorder.Header().Get("Accept-Ranges") != "bytes" || recorder.Header().Get("Content-Length") != "10" {
		test.Fatalf("HEAD: %d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

// A file that is not a picture, a sound, a video or an artifact is handed
// over as a download, whatever type it was uploaded as.
func TestAnUnknownAttachmentIsADownload(test *testing.T) {
	attachment := &models.AgentAttachment{ID: "attachment-2", Name: "page.html", ContentType: "text/html"}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/attachments/attachment-2", nil)
	recorder := httptest.NewRecorder()
	writeAgentAttachment(recorder, request, attachment, []byte("<p>hello</p>"))
	if recorder.Header().Get("Content-Type") != "application/octet-stream" || recorder.Header().Get("Content-Disposition") != `attachment; filename="page.html"` || recorder.Body.String() != "<p>hello</p>" {
		test.Fatalf("download: %v %q", recorder.Header(), recorder.Body.String())
	}
}
