package photo

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

func TestAnUploadTheServerHasNoRoomForIsAnsweredWithAWait(test *testing.T) {
	recorder := httptest.NewRecorder()
	full := fmt.Errorf("storage: cannot upload fixture: %w", &fs.PathError{Op: "write", Path: "blobs/fixture", Err: syscall.ENOSPC})
	(&Store{}).writeProblem(recorder, full)
	if recorder.Code != http.StatusInsufficientStorage {
		test.Fatalf("status = %d, want %d", recorder.Code, http.StatusInsufficientStorage)
	}
	if retryAfter := recorder.Header().Get("Retry-After"); retryAfter != "900" {
		test.Errorf("Retry-After = %q, want 900", retryAfter)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "out of storage space") {
		test.Errorf("body = %q, want it to say the server is out of storage space", body)
	}
}
