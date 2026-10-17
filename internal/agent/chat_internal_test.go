package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A chat event that is too long for a notification between instances is cut
// and says so, or left out; a client then reads the conversation again.
func TestAChatMessageTooLongToTravelSaysItWasCut(t *testing.T) {
	long := strings.Repeat("é", 6000)
	event := fitForRelay(Event{Kind: EventSaid, ConversationID: "c", Message: &models.AgentMessage{ID: "m", Content: long}})
	if !event.IsMessageCut || event.Message == nil || !strings.HasSuffix(event.Message.Content, "…") || len(event.Message.Content) >= len(long) {
		t.Fatalf("a long message was left as %+v", event)
	}
	if long != strings.Repeat("é", 6000) {
		t.Error("the cut changed the original message")
	}
	whole := fitForRelay(Event{Kind: EventSaid, ConversationID: "c", Message: &models.AgentMessage{ID: "m", Content: "short"}})
	if whole.IsMessageCut || whole.Message.Content != "short" {
		t.Errorf("a short message was changed: %+v", whole)
	}
	// What cuts cannot save is left out, and the flag says to read again.
	heavy := &models.AgentMessage{ID: "m", Content: "x"}
	for count := 0; count < 400; count++ {
		heavy.Attachments = append(heavy.Attachments, models.AgentAttachment{ID: strings.Repeat("a", 30), Name: strings.Repeat("n", 30)})
	}
	left := fitForRelay(Event{Kind: EventSaid, ConversationID: "c", Message: heavy})
	if left.Message != nil || !left.IsMessageCut {
		t.Errorf("a message that cannot be cut to fit was %+v", left)
	}
}
