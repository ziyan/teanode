package mailparse_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

// The names of a message's attachments come back as the sender gave them,
// however the wire encoded them; a part shown inline is not one of them.
func TestAttachmentNamesAreTheSendersNames(test *testing.T) {
	test.Parallel()

	var body bytes.Buffer
	headers, err := mailparse.Compose(&body, []byte("hello\r\n"), nil, []*mailparse.Attachment{
		{Filename: "invoice-march.pdf", ContentType: "application/pdf", Content: []byte("%PDF")},
		{Filename: "notes — 2026.txt", ContentType: "text/plain", Content: []byte("notes")},
	})
	if err != nil {
		test.Fatalf("Compose: %s", err)
	}
	names := mailparse.AttachmentNames(headers, body.Bytes())
	if strings.Join(names, "|") != "invoice-march.pdf|notes — 2026.txt" {
		test.Errorf("AttachmentNames = %q, want the two files as named", names)
	}
}

func TestAttachmentNamesSkipWhatIsShownInline(test *testing.T) {
	test.Parallel()

	headers := []string{"MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="b"`}
	body := strings.ReplaceAll(`--b
Content-Type: text/html

<p>hi</p>
--b
Content-Type: image/png; name="logo.png"
Content-Disposition: inline; filename="logo.png"

png
--b
Content-Type: application/octet-stream; name="named-only.bin"

bin
--b
Content-Type: text/plain
Content-Disposition: attachment

no name at all
--b--
`, "\n", "\r\n")
	names := mailparse.AttachmentNames(headers, []byte(body))
	if strings.Join(names, "|") != "named-only.bin" {
		test.Errorf("AttachmentNames = %q, want the named part alone: not the inline image, not the nameless part", names)
	}
	if malformed := mailparse.AttachmentNames(headers, []byte("--b\r\nContent-Type: garbage\r\n\r\n--b")); len(malformed) != 0 {
		test.Errorf("a malformed body names %q", malformed)
	}
}
