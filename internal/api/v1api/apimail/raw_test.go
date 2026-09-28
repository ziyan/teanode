package apimail

import (
	"bytes"
	"io"
	netmail "net/mail"
	"testing"
)

// A message appended over IMAP is stored with the line ending still on each
// header, and one received over SMTP without; both have to download as a
// message that parses, with every header a header.
func TestRawMessageParsesWhateverTheHeadersEndWith(test *testing.T) {
	for name, headers := range map[string][]string{
		"appended": {"From: someone@example.com\r\n", "Subject: A note\r\n", "X-Universally-Unique-Identifier: 1A2B3C4D-0000-4000-8000-000000000001\r\n"},
		"received": {"From: someone@example.com", "Subject: A note", "X-Universally-Unique-Identifier: 1A2B3C4D-0000-4000-8000-000000000001"},
	} {
		message, err := netmail.ReadMessage(bytes.NewReader(rawMessage(headers, []byte("<div>body</div>"))))
		if err != nil {
			test.Fatalf("%s: %s", name, err)
		}
		if message.Header.Get("Subject") != "A note" || message.Header.Get("X-Universally-Unique-Identifier") == "" {
			test.Fatalf("%s: headers=%v", name, message.Header)
		}
		body, _ := io.ReadAll(message.Body)
		if string(body) != "<div>body</div>" {
			test.Fatalf("%s: body=%q", name, body)
		}
	}
}
