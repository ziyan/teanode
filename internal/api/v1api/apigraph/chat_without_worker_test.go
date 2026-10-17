package apigraph

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// A household may run this server for mail, calendars and chats with no
// language model, and so with no agent worker. Chats work the same there: the
// feed, typing, read marks and the phones come from the chat hub.

// subscribe follows a conversation's feed through the resolver the
// subscription calls, as the person, and returns what it carries and the
// function that ends it.
func (self *chatWorld) subscribe(user *models.User, conversationId string) (<-chan *agent.Event, context.CancelFunc, error) {
	principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionCircleUse}, {Permission: models.PermissionAgentUse}})}
	ctx, cancel := context.WithCancel(api.ContextWithPrincipal(context.Background(), principal))
	events, err := self.graph.AgentConversationEvents(ctx, ReadAgentConversationEventsArguments{ConversationID: conversationId})
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return events, cancel, nil
}

// nextFeedEvent is the next event of the kind on a subscription within a
// second, or nil.
func nextFeedEvent(events <-chan *agent.Event, kind agent.EventKind) *agent.Event {
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if event.Kind == kind {
				return event
			}
		case <-deadline:
			return nil
		}
	}
}

func TestAChatIsFollowedLiveOnAServerWithNoAgentWorker(t *testing.T) {
	world := newChatWorldWithoutWorker(t)
	if world.graph.agentWorker() != nil {
		t.Fatal("the world has a worker")
	}
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := ""
	for _, each := range world.call(t, world.bob, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if conversation["chat"].(map[string]any)["chatKind"] == "direct" {
			bobChat = conversation["id"].(string)
		}
	}
	if bobChat == "" {
		t.Fatal("Bob has no chat")
	}

	bobFeed, stopBob, err := world.subscribe(world.bob, bobChat)
	if err != nil {
		t.Fatalf("Bob cannot follow his chat with no worker: %s", err)
	}
	defer stopBob()
	aliceFeed, stopAlice, err := world.subscribe(world.alice, aliceChat)
	if err != nil {
		t.Fatalf("Alice cannot follow her chat with no worker: %s", err)
	}
	defer stopAlice()

	// What Alice says is heard by Bob as his own conversation's message.
	world.say(t, world.alice, aliceChat, "dinner at 7?")
	said := nextFeedEvent(bobFeed, agent.EventSaid)
	if said == nil || said.Message == nil || said.Message.Content != "dinner at 7?" || said.ConversationID != bobChat || said.Message.ConversationID != bobChat || said.Message.IsMine {
		t.Fatalf("Bob's feed carried %+v", said)
	}

	// Typing reaches the other person and not the one typing.
	world.call(t, world.alice, fmt.Sprintf(`mutation { SetChatTyping(conversationId: %q) }`, aliceChat))
	if typing := nextFeedEvent(bobFeed, agent.EventTyping); typing == nil || typing.UserID != world.alice.ID || typing.ConversationID != bobChat {
		t.Errorf("Bob's feed carried %+v for Alice typing", typing)
	}

	// Bob reads it, and Alice's feed carries the mark.
	messageId := world.read(t, world.bob, bobChat)[0]["id"]
	world.call(t, world.bob, fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: %q) }`, bobChat, messageId))
	read := nextFeedEvent(aliceFeed, agent.EventRead)
	for read != nil && read.UserID != world.bob.ID {
		read = nextFeedEvent(aliceFeed, agent.EventRead)
	}
	if read == nil || read.MessageID != messageId || read.ConversationID != aliceChat {
		t.Errorf("Alice's feed carried %+v for Bob reading", read)
	}

	// A reaction, an edit and taking it back are heard the same way.
	world.call(t, world.bob, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, messageId))
	if reacted := nextFeedEvent(aliceFeed, agent.EventReacted); reacted == nil || reacted.Emoji != "👍" || reacted.ReactorUserID != world.bob.ID {
		t.Errorf("Alice's feed carried %+v for Bob's reaction", reacted)
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "dinner at 8?") { id } }`, messageId))
	if edited := nextFeedEvent(bobFeed, agent.EventEdited); edited == nil || edited.Message == nil || edited.Message.Content != "dinner at 8?" {
		t.Errorf("Bob's feed carried %+v for the edit", edited)
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, messageId))
	if unsaid := nextFeedEvent(bobFeed, agent.EventUnsaid); unsaid == nil || unsaid.MessageID != messageId {
		t.Errorf("Bob's feed carried %+v for taking it back", unsaid)
	}

	// Nobody follows a chat but its people, with a worker or without.
	if _, _, err := world.subscribe(world.carol, bobChat); err == nil {
		t.Error("Carol followed Bob's chat")
	}
	// What is not a chat is the agent's, and refused as unavailable without
	// a worker, to a person whose agent is on.
	if _, _, err := world.subscribe(world.frank, ""); err == nil || !strings.Contains(err.Error(), "agent: unavailable") {
		t.Errorf("Frank followed his main conversation with no worker: %v", err)
	}

	// The feed ends when the subscription does.
	stopBob()
	deadline := time.After(time.Second)
	for isOpen := true; isOpen; {
		select {
		case _, isOpen = <-bobFeed:
		case <-deadline:
			t.Fatal("Bob's feed stayed open after the subscription ended")
		}
	}
}

// chatRelay stands in for the push relay: what was sent, to which phone.
type chatRelay struct {
	mutex sync.Mutex
	sent  []*push.Delivery
}

func (self *chatRelay) Send(_ context.Context, _ string, delivery *push.Delivery) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.sent = append(self.sent, delivery)
	return nil
}

// settled is what was sent once nothing more comes: notifications are sent in
// the background, so a notification that should not come is given a moment to.
func (self *chatRelay) settled(minimum int) []*push.Delivery {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		self.mutex.Lock()
		count := len(self.sent)
		self.mutex.Unlock()
		if count >= minimum {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]*push.Delivery(nil), self.sent...)
}

func TestAChatMessageReachesAPhoneOnAServerWithNoAgentWorker(t *testing.T) {
	world := newChatWorldWithoutWorker(t)
	configuration := config.Default()
	configuration.Server.Secret = "a secret for sealing in this test"
	relay := &chatRelay{}
	world.hub = chat.New(&chat.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration }, Instance: "chat-test",
		Phones: agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
			Database: world.database, Configuration: func() *config.Configuration { return configuration },
			Context: context.Background(), WaitGroup: &sync.WaitGroup{}, Sender: relay,
		}),
	})
	world.graph.settings.Chat = world.hub

	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)

	// Bob's agent is switched off, and his phone hangs from it all the same.
	key := make([]byte, push.PhoneKeyBytes)
	_, _ = rand.Read(key)
	sealedKey, err := agent.SealConnectionSecret(configuration, base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatalf("SealConnectionSecret: %s", err)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		bobAgent, err := tx.GetAgentByUser(world.bob.ID)
		if err != nil || bobAgent == nil || bobAgent.Active() {
			t.Fatalf("Bob's agent is %+v, %v; want one that is switched off", bobAgent, err)
		}
		if _, err := tx.PutAgentPhone(&models.AgentPhone{
			AgentID: bobAgent.ID, ClientID: "a-phone-client", PhoneName: "A test phone", PushRegistration: "sealed:a-registration", PayloadKey: sealedKey,
		}); err != nil {
			t.Fatalf("PutAgentPhone: %s", err)
		}
	})
	if _, err := world.database.CreateAuthorizedToken(&models.Token{
		ID: "a-token", UserID: world.bob.ID, Name: "A test phone", ClientID: "a-phone-client", ExpiresAt: time.Now().Add(time.Hour),
	}, "a-key-hash", "a-refresh-hash"); err != nil {
		t.Fatalf("CreateAuthorizedToken: %s", err)
	}

	// A person whose agent is off lists and manages their phones.
	listed := world.call(t, world.bob, `query { ListAgentPhones { phones { id canBeNotified } } }`)["ListAgentPhones"].(map[string]any)
	if phones := listed["phones"].([]any); len(phones) != 1 || phones[0].(map[string]any)["canBeNotified"] != true {
		t.Fatalf("Bob lists the phones %v", listed)
	}

	world.say(t, world.alice, aliceChat, "dinner at 7?")
	sent := relay.settled(1)
	if len(sent) != 1 {
		t.Fatalf("%d notifications reached Bob's phone, want 1", len(sent))
	}
	content, err := push.DecryptContent(key, sent[0].Sealed)
	if err != nil {
		t.Fatalf("the phone cannot read its notification: %s", err)
	}
	if content.NotificationKind != models.NotificationChat || content.Title != world.alice.DisplayName() || content.Body != "dinner at 7?" || content.ConversationID == "" || content.ThreadID != content.ConversationID {
		t.Errorf("the notification says %+v", content)
	}

	// Bob has the app open on that chat: the report reaches the hub, with no
	// worker to take it, and the next message is not sent to the phone.
	reportOn := func(isVisible bool, conversationId string) any {
		return world.call(t, world.bob, fmt.Sprintf(`mutation { ReportAgentPresence(isVisible: %t, idleSeconds: 0, reporterId: "the-app", conversationId: %q) }`, isVisible, conversationId))["ReportAgentPresence"]
	}
	if reported := reportOn(true, content.ConversationID); reported != true {
		t.Fatalf("ReportAgentPresence answered %v with no worker", reported)
	}
	world.say(t, world.alice, aliceChat, "bring the salad")
	if sent := relay.settled(2); len(sent) != 1 {
		t.Errorf("a person with the app open on the chat was notified: %d", len(sent))
	}
	// The app is open on another screen: the next one is.
	reportOn(true, "")
	world.say(t, world.alice, aliceChat, "and the wine")
	if sent := relay.settled(2); len(sent) != 2 {
		t.Errorf("%d notifications in all after the app moved to another screen, want 2", len(sent))
	}
	// Back on the chat, and then in the background: the chat is quiet only
	// while it is on screen.
	reportOn(true, content.ConversationID)
	world.say(t, world.alice, aliceChat, "and the bread")
	if sent := relay.settled(3); len(sent) != 2 {
		t.Errorf("%d notifications in all with the app back on the chat, want 2", len(sent))
	}
	reportOn(false, content.ConversationID)
	world.say(t, world.alice, aliceChat, "and the cheese")
	if sent := relay.settled(3); len(sent) != 3 {
		t.Errorf("%d notifications in all after the app went to the background, want 3", len(sent))
	}
}
