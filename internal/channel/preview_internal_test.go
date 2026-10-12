package channel

import (
	"context"
	"strings"
	"testing"
	"time"
)

// recordingChat keeps what was sent to it.
type recordingChat struct {
	sent []string
}

func (self *recordingChat) Send(_ context.Context, text, _ string) (string, error) {
	self.sent = append(self.sent, text)
	return "1", nil
}
func (self *recordingChat) Edit(_ context.Context, _, text string) error {
	self.sent = append(self.sent, text)
	return nil
}
func (self *recordingChat) Delete(context.Context, string) error { return nil }
func (self *recordingChat) Typing(context.Context) error         { return nil }
func (self *recordingChat) SendFile(context.Context, string, string, []byte, string) error {
	return nil
}
func (self *recordingChat) Limit() int { return 4096 }

// The agent's links to the dashboard's own pages reach a chat app as the
// dashboard's addresses, which open there, and as their words when the
// server has no name to make them from.
func TestAnswerLinksTheDashboard(t *testing.T) {
	t.Parallel()

	const answer = "He is on [Some Person](memory:people/some-person#4), from [Mooring invoice](mail:item42)."
	chat := &recordingChat{}
	(&preview{chat: chat, dashboard: "https://mail.example.com"}).finish(context.Background(), answer)
	if len(chat.sent) != 1 || chat.sent[0] != "He is on [Some Person](https://mail.example.com/knowledge/people/some-person), from [Mooring invoice](https://mail.example.com/mailbox/starred/item42)." {
		t.Fatalf("sent %q", chat.sent)
	}

	unnamed := &recordingChat{}
	(&preview{chat: unnamed}).finish(context.Background(), answer)
	if len(unnamed.sent) != 1 || unnamed.sent[0] != "He is on Some Person, from Mooring invoice." {
		t.Fatalf("with no name, sent %q", unnamed.sent)
	}
}

// A chat app shows the words of an answer and nothing the dashboard hides:
// not the suggested replies, which arrive a few characters at a time while
// the answer streams, and not any other comment.
func TestAnswerLeavesOutWhatIsHidden(t *testing.T) {
	t.Parallel()

	chat := &recordingChat{}
	live := &preview{chat: chat}
	for _, piece := range []string{"Booked", " for Friday.", "\n<!--sugg", "estions:[\"Thanks\",", "\"Not now\"]-->"} {
		live.text += piece
		live.lastEdit = time.Time{}
		live.flush(context.Background())
	}
	live.finish(context.Background(), "Booked for Friday. <!-- kept: 2 -->\n<!--suggestions:[\"Thanks\",\"Not now\"]-->")
	for _, sent := range chat.sent {
		if strings.Contains(sent, "<!--") || strings.Contains(sent, "sugg") {
			t.Errorf("a chat app was sent %q", sent)
		}
	}
	if last := chat.sent[len(chat.sent)-1]; last != "Booked for Friday." {
		t.Errorf("the answer ended as %q", last)
	}

	// Only the pieces were heard, and the turn ended without a whole answer.
	unfinished := &recordingChat{}
	live = &preview{chat: unfinished, text: "Done.\n<!--suggestions:[\"A\",\"B\"]-->"}
	live.finish(context.Background(), "")
	if len(unfinished.sent) != 1 || unfinished.sent[0] != "Done." {
		t.Errorf("a chat app was sent %q", unfinished.sent)
	}
}
