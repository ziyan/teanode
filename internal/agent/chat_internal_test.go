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

// How hard the owner's agent thinks is the owner's to choose: a turn somebody
// else asked for runs on the default whatever effort the asker named, and the
// owner's own turn keeps the effort they named.
func TestATurnSomebodyElseAskedForRunsOnTheDefaultEffort(t *testing.T) {
	turn := func(isMemberTurn bool) *AskSettings {
		asker := "ann"
		if isMemberTurn {
			asker = "ben"
		}
		return &AskSettings{
			Agent: &models.Agent{ID: "agent-ann", UserID: "ann"}, Owner: &models.User{ID: "ann"},
			Conversation: &models.AgentConversation{ID: "thread", AgentID: "agent-ann", ChatID: "chat", Kind: models.AgentConversationChat},
			Effort:       "high",
			Chat:         &ChatTurn{ChatID: "chat", MessageID: "message", AskerUserID: asker, IsMemberTurn: isMemberTurn},
		}
	}
	member := turn(true)
	if err := applyChatSettings(member); err != nil || member.Effort != "" {
		t.Errorf("a member turn thinks at %q (%v); want the default", member.Effort, err)
	}
	owner := turn(false)
	if err := applyChatSettings(owner); err != nil || owner.Effort != "high" {
		t.Errorf("the owner's turn thinks at %q (%v); want what they chose", owner.Effort, err)
	}
}
