package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A file the night cannot open is declined with the reason that is true
// of it: an empty file is empty, and a text file had no words in it,
// rather than either being "not a picture".
func TestAnUnopenableFileSaysWhy(t *testing.T) {
	for _, each := range []struct {
		contentType string
		bytes       int64
		want        string
	}{
		{"text/markdown", 1, "it is empty"},
		{"text/plain", 4096, "no words could be read"},
		{"application/zip", 4096, "is not a picture"},
		{"application/pdf", 4096, "a PDF with no text the computer could read"},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", 4096, "a spreadsheet with no text"},
		{"image/png", 4096, ""},
	} {
		document := &models.AgentDocument{Bytes: each.bytes, Metadata: map[string]any{"contentType": each.contentType}}
		got := unopenable(document)
		if (each.want == "") != (got == "") || !strings.Contains(got, each.want) {
			t.Errorf("%s of %d bytes: %q, want %q", each.contentType, each.bytes, got, each.want)
		}
	}
}
