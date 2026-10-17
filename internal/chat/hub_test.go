package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// newTestHub is a hub with no database, as a test that is not about the
// other instances has it.
func newTestHub() *Hub {
	configuration := config.Default()
	return New(&Settings{Configuration: func() *config.Configuration { return configuration }, Instance: "only"})
}

// expectEvent is the next event of the feed within a second.
func expectEvent(t *testing.T, events <-chan agent.Event, where string) agent.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatalf("%s: the feed closed", where)
		}
		return event
	case <-time.After(time.Second):
		t.Fatalf("%s: no event within a second", where)
		return agent.Event{}
	}
}

// expectNothing says nothing arrives in the time it would take.
func expectNothing(t *testing.T, events <-chan agent.Event, where string) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("%s: heard %+v", where, event)
	case <-time.After(150 * time.Millisecond):
	}
}

// An event reaches the subscribers of the conversation it is addressed to and
// nobody else's, however many there are; and a feed that was ended closes.
func TestAnEventReachesTheSubscribersOfItsConversationOnly(t *testing.T) {
	hub := newTestHub()
	first, stopFirst := hub.Subscribe("conversation-a")
	defer stopFirst()
	second, stopSecond := hub.Subscribe("conversation-a")
	defer stopSecond()
	other, stopOther := hub.Subscribe("conversation-b")
	defer stopOther()

	hub.PublishChatEvents([]agent.Event{
		{Kind: agent.EventSaid, ConversationID: "conversation-a", MessageID: "message-1", Message: &models.AgentMessage{ID: "message-1", Content: "dinner at 7?"}},
		// An event for no conversation is for nobody.
		{Kind: agent.EventTyping, UserID: "someone"},
	})
	for where, events := range map[string]<-chan agent.Event{"first": first, "second": second} {
		event := expectEvent(t, events, where)
		if event.Kind != agent.EventSaid || event.ConversationID != "conversation-a" || event.Message == nil || event.Message.Content != "dinner at 7?" || event.At.IsZero() || event.RunID != "" {
			t.Errorf("%s heard %+v", where, event)
		}
		expectNothing(t, events, where)
	}
	expectNothing(t, other, "the other conversation")

	// Nothing is replayed to a subscriber that comes later.
	late, stopLate := hub.Subscribe("conversation-a")
	defer stopLate()
	expectNothing(t, late, "a late subscriber")

	stopFirst()
	if _, isOpen := <-first; isOpen {
		t.Error("an ended feed stayed open")
	}
	hub.PublishChatEvents([]agent.Event{{Kind: agent.EventTyping, ConversationID: "conversation-a", UserID: "someone"}})
	if event := expectEvent(t, second, "second"); event.Kind != agent.EventTyping {
		t.Errorf("second heard %+v", event)
	}
}

// A chat is published to the members who are in it: the event is built for
// each one's own conversation, and a member who left, or who has no
// conversation, is not told, so a person taken out stops hearing at once.
func TestOnlyTheMembersWhoAreInTheChatAreToldAndARemovedOneStopsHearing(t *testing.T) {
	hub := newTestHub()
	members := []*models.ChatMember{
		{UserID: "user-a", ConversationID: "conversation-a"},
		{UserID: "user-b", ConversationID: "conversation-b"},
		{UserID: "user-c", ConversationID: "conversation-c"},
		{UserID: "user-d"},
	}
	feeds := map[string]<-chan agent.Event{}
	for _, member := range members {
		if member.ConversationID == "" {
			continue
		}
		events, stop := hub.Subscribe(member.ConversationID)
		defer stop()
		feeds[member.UserID] = events
	}
	say := func(message string) {
		hub.PublishChatEvents(EventsForMembers(members, func(member *models.ChatMember) agent.Event {
			return agent.Event{Kind: agent.EventSaid, MessageID: message, Message: &models.AgentMessage{ID: message, ConversationID: member.ConversationID, IsMine: member.UserID == "user-a"}}
		}))
	}

	say("message-1")
	for userId, events := range feeds {
		event := expectEvent(t, events, userId)
		if event.MessageID != "message-1" || event.Message.ConversationID != event.ConversationID || event.Message.IsMine != (userId == "user-a") {
			t.Errorf("%s heard %+v of %+v", userId, event, event.Message)
		}
	}

	// user-c is taken out; the next message does not reach them, nor does
	// anything built after, and the others still hear.
	members[2].IsRemoved = true
	say("message-2")
	for _, userId := range []string{"user-a", "user-b"} {
		if event := expectEvent(t, feeds[userId], userId); event.MessageID != "message-2" {
			t.Errorf("%s heard %+v", userId, event)
		}
	}
	expectNothing(t, feeds["user-c"], "the removed member")

	// A build function that answers nothing for a member leaves them out:
	// the one who is typing is not told of it.
	hub.PublishChatEvents(EventsForMembers(members, func(member *models.ChatMember) agent.Event {
		if member.UserID == "user-a" {
			return agent.Event{}
		}
		return agent.Event{Kind: agent.EventTyping, UserID: "user-a"}
	}))
	if event := expectEvent(t, feeds["user-b"], "user-b"); event.Kind != agent.EventTyping || event.UserID != "user-a" {
		t.Errorf("user-b heard %+v", event)
	}
	expectNothing(t, feeds["user-a"], "the one typing")
	expectNothing(t, feeds["user-c"], "the removed member")
}

// What one instance publishes the others hear, with the event shortened to
// fit a notification when it is too long, and the instance does not hear
// itself twice.
func TestAnEventReachesTheOtherInstancesAndNotItselfTwice(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	configuration := config.Default()
	newHub := func(instance string) *Hub {
		hub := New(&Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: instance})
		hub.Start()
		return hub
	}
	here := newHub("here")
	defer here.Stop()
	there := newHub("there")
	defer there.Stop()

	local, stopLocal := here.Subscribe("conversation-1")
	defer stopLocal()
	elsewhere, stopElsewhere := there.Subscribe("conversation-1")
	defer stopElsewhere()
	unrelated, stopUnrelated := there.Subscribe("conversation-2")
	defer stopUnrelated()
	// The LISTEN takes a moment to be in place.
	time.Sleep(300 * time.Millisecond)

	long := strings.Repeat("é", 6000)
	here.PublishChatEvents([]agent.Event{
		{Kind: agent.EventSaid, ConversationID: "conversation-1", MessageID: "message-1", Message: &models.AgentMessage{ID: "message-1", Content: "dinner at 7?"}},
		{Kind: agent.EventSaid, ConversationID: "conversation-1", MessageID: "message-2", Message: &models.AgentMessage{ID: "message-2", Content: long}},
		{Kind: agent.EventTyping, ConversationID: "conversation-1", UserID: "user-a", Name: "Maria"},
	})
	for where, events := range map[string]<-chan agent.Event{"this instance": local, "the other instance": elsewhere} {
		said := expectEvent(t, events, where)
		if said.Kind != agent.EventSaid || said.MessageID != "message-1" || said.Message == nil || said.Message.Content != "dinner at 7?" || said.IsMessageCut {
			t.Errorf("%s heard %+v", where, said)
		}
		big := expectEvent(t, events, where)
		if big.MessageID != "message-2" || big.Message == nil {
			t.Fatalf("%s heard %+v", where, big)
		}
		// This instance has the whole of it; the other a cut one that says so.
		if (where == "this instance") == big.IsMessageCut || (where == "this instance" && big.Message.Content != long) {
			t.Errorf("%s: the long message is cut=%v, %d bytes", where, big.IsMessageCut, len(big.Message.Content))
		}
		typing := expectEvent(t, events, where)
		if typing.Kind != agent.EventTyping || typing.UserID != "user-a" || typing.Name != "Maria" {
			t.Errorf("%s heard %+v", where, typing)
		}
		expectNothing(t, events, where)
	}
	expectNothing(t, unrelated, "another conversation")
}
