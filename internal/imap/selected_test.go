package imap

import (
	"reflect"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
)

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

// A sequence range is clamped to the messages the folder has, so a command
// naming 1:4294967295 costs the size of the folder, not four billion turns of
// a loop that no database call interrupts.
func TestResolveSequenceRangeIsBoundedByTheFolder(t *testing.T) {
	t.Parallel()

	selected := &view{uids: []uint64{101, 102, 103}}
	tests := []struct {
		name string
		set  goimap.SeqSet
		want []uint64
	}{
		{"a range far past the last message", goimap.SeqSet{{Start: 1, Stop: 4294967295}}, []uint64{101, 102, 103}},
		{"a range ending in the last message", goimap.SeqSet{{Start: 2, Stop: 0}}, []uint64{102, 103}},
		{"a range reversed", goimap.SeqSet{{Start: 3, Stop: 2}}, []uint64{102, 103}},
		{"a range entirely past the last message", goimap.SeqSet{{Start: 4, Stop: 4294967295}}, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			started := time.Now()
			got := selected.resolve(test.set)
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Errorf("resolve took %s", elapsed)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("resolve = %v, want %v", got, test.want)
			}
		})
	}

	empty := &view{}
	if got := empty.resolve(goimap.SeqSet{{Start: 1, Stop: 4294967295}}); len(got) != 0 {
		t.Errorf("an empty folder resolved %v", got)
	}
}
