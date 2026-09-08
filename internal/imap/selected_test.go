package imap

import "testing"

// The size recorded for an uploaded message is the whole message, not the
// body alone: a copy of a sent message used to show a tenth of the size of
// the message it copied.
func TestMessageSizeCountsTheHeaders(t *testing.T) {
	t.Parallel()
	headers := []string{"From: a@example.com", "Subject: hi"}
	body := []byte("hello\r\n")
	want := uint64(len("From: a@example.com\r\n") + len("Subject: hi\r\n") + len("\r\n") + len(body))
	if got := messageSize(headers, body); got != want {
		t.Errorf("messageSize = %d, want %d", got, want)
	}
}
