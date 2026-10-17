package chat

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
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// fakeRelay is the relay as a test sees it: what was sent, to which
// registration.
type fakeRelay struct {
	mutex sync.Mutex
	sent  map[string][]*push.Delivery
}

func (self *fakeRelay) Send(_ context.Context, pushRegistration string, delivery *push.Delivery) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.sent == nil {
		self.sent = map[string][]*push.Delivery{}
	}
	self.sent[pushRegistration] = append(self.sent[pushRegistration], delivery)
	return nil
}

func (self *fakeRelay) to(pushRegistration string) []*push.Delivery {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]*push.Delivery(nil), self.sent[pushRegistration]...)
}

// phoneWorld is a server with no model and so no agent: a hub, and a person
// whose own agent is switched off, with one phone that sends through a fake
// relay.
type phoneWorld struct {
	hub       *Hub
	relay     *fakeRelay
	recipient Recipient
	key       []byte
}

func newPhoneWorld(t *testing.T) *phoneWorld {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Server.Secret = "a secret for sealing in this test"
	configuration.Agent.Enabled = false
	world := &phoneWorld{relay: &fakeRelay{}, key: make([]byte, push.PhoneKeyBytes)}
	_, _ = rand.Read(world.key)
	getConfiguration := func() *config.Configuration { return configuration }
	phones := agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
		Database: database, Configuration: getConfiguration, Context: context.Background(), WaitGroup: &sync.WaitGroup{}, Sender: world.relay,
	})
	world.hub = New(&Settings{Database: database, Configuration: getConfiguration, Instance: "only", Phones: phones})
	sealedKey, err := agent.SealConnectionSecret(configuration, base64.StdEncoding.EncodeToString(world.key))
	if err != nil {
		t.Fatalf("SealConnectionSecret: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "robin"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		// The person never turned the agent on: its row is switched off.
		owned, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: false})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if _, err = tx.PutAgentPhone(&models.AgentPhone{
			AgentID: owned.ID, ClientID: "a-phone-client", PhoneName: "A test phone",
			PushRegistration: "sealed:a-registration", PayloadKey: sealedKey,
		}); err != nil {
			t.Fatalf("PutAgentPhone: %s", err)
		}
		other, err := tx.PutAgentPhone(&models.AgentPhone{
			AgentID: owned.ID, ClientID: "another-phone-client", PhoneName: "A phone that wants approvals",
			PushRegistration: "sealed:the-other-registration", PayloadKey: sealedKey,
		})
		if err != nil {
			t.Fatalf("PutAgentPhone: %s", err)
		}
		if err := tx.UpdateAgentPhoneSettings(owned.ID, other.ID, other.PhoneName, []string{models.NotificationApproval}); err != nil {
			t.Fatalf("UpdateAgentPhoneSettings: %s", err)
		}
		world.recipient = Recipient{UserID: person.ID, AgentID: owned.ID, ConversationID: "the-members-conversation"}
	})
	// The phones are signed in: each client holds a token.
	for index, clientId := range []string{"a-phone-client", "another-phone-client"} {
		if _, err := database.CreateAuthorizedToken(&models.Token{
			ID: fmt.Sprintf("a-token-%d", index), UserID: world.recipient.UserID, Name: "A test phone", ClientID: clientId, ExpiresAt: time.Now().Add(time.Hour),
		}, fmt.Sprintf("a-key-hash-%d", index), fmt.Sprintf("a-refresh-hash-%d", index)); err != nil {
			t.Fatalf("CreateAuthorizedToken: %s", err)
		}
	}
	return world
}

// expectSent waits for the notifications on their way to the phone, which are
// sent in the background, and fails unless exactly count arrived: it gives a
// notification that should not come a moment to come.
func (self *phoneWorld) expectSent(t *testing.T, count int, why string) []*push.Delivery {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(self.relay.to("sealed:a-registration")) < count && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	deliveries := self.relay.to("sealed:a-registration")
	if len(deliveries) != count {
		t.Fatalf("%s: %d notifications reached the phone, want %d", why, len(deliveries), count)
	}
	return deliveries
}

func (self *phoneWorld) read(t *testing.T, delivery *push.Delivery) *push.Content {
	t.Helper()
	content, err := push.DecryptContent(self.key, delivery.Sealed)
	if err != nil {
		t.Fatalf("the phone cannot read its notification: %s", err)
	}
	return content
}

// A member who archived the chat is left alone by it, as by a muted one, and
// is told again once they put it back.
func TestAChatMessageIsNotNotifiedToAMemberWhoArchivedTheChat(t *testing.T) {
	world := newPhoneWorld(t)
	message := &models.AgentMessage{
		ID: "a-message", AuthorKind: models.ChatAuthorPerson, AuthorUserID: "another-person", AuthorName: "Maria",
		Content: "are you there?",
	}
	archived := world.recipient
	archived.IsArchived = true
	world.hub.NotifyChatMessage(message, "Weekend trip", true, []Recipient{archived})
	world.expectSent(t, 0, "an archived chat")

	archived.IsArchived = false
	world.hub.NotifyChatMessage(message, "Weekend trip", true, []Recipient{archived})
	world.expectSent(t, 1, "a chat put back")
}

// On a server with no agent, a message in a chat reaches the phone of a member
// who is not looking, with the author's name and the first line, grouped by
// the member's conversation; not the author's own, not a muted chat's, not a
// member who is looking.
func TestAChatMessageIsNotifiedWithNoAgentUnlessMutedOrWatchedOrOwn(t *testing.T) {
	world := newPhoneWorld(t)
	message := &models.AgentMessage{
		ID: "a-message", AuthorKind: models.ChatAuthorPerson, AuthorUserID: "another-person", AuthorName: "Maria",
		Content: "dinner at 7?\nbring the salad",
	}

	// Silenced: nothing.
	mutedUntil := time.Now().Add(time.Hour)
	muted := world.recipient
	muted.MutedUntil = &mutedUntil
	world.hub.NotifyChatMessage(message, "", false, []Recipient{muted})
	world.expectSent(t, 0, "a muted chat")
	// A mute that has ended is no mute.
	ended := time.Now().Add(-time.Minute)
	muted.MutedUntil = &ended
	world.hub.NotifyChatMessage(message, "", false, []Recipient{muted})
	sent := world.expectSent(t, 1, "a chat whose mute ended")
	content := world.read(t, sent[0])
	if content.NotificationKind != models.NotificationChat || content.Title != "Maria" || content.Body != "dinner at 7?" ||
		content.ThreadID != "the-members-conversation" || content.ConversationID != "the-members-conversation" {
		t.Errorf("the notification says %+v", content)
	}

	// The author does not hear themselves.
	own := *message
	own.AuthorUserID = world.recipient.UserID
	world.hub.NotifyChatMessage(&own, "", false, []Recipient{world.recipient})
	world.expectSent(t, 1, "the author's own message")

	// Looking at this chat in a dashboard or the app now, as reported to the
	// hub with no agent to take the report: nothing.
	world.hub.ReportPresence(world.recipient.UserID, "a-tab", true, 0, world.recipient.ConversationID, time.Now())
	world.hub.NotifyChatMessage(message, "", false, []Recipient{world.recipient})
	world.expectSent(t, 1, "a member who was looking at this chat")

	// Looking at the app on another screen, or at another conversation, is
	// not looking at this one: the message is news.
	world.hub.ReportPresence(world.recipient.UserID, "a-tab", true, 0, "", time.Now())
	world.hub.NotifyChatMessage(message, "", false, []Recipient{world.recipient})
	world.expectSent(t, 2, "a member who was on another screen")
	world.hub.ReportPresence(world.recipient.UserID, "a-tab", true, 0, "another-conversation", time.Now())
	world.hub.NotifyChatMessage(message, "", false, []Recipient{world.recipient})
	world.expectSent(t, 3, "a member who was looking at another conversation")

	// One of two tabs on this chat is enough, and a tab on this chat that is
	// hidden is not looking.
	world.hub.ReportPresence(world.recipient.UserID, "another-tab", true, 0, world.recipient.ConversationID, time.Now())
	world.hub.NotifyChatMessage(message, "", false, []Recipient{world.recipient})
	world.expectSent(t, 3, "a member with one tab on another conversation and one on this chat")
	world.hub.ReportPresence(world.recipient.UserID, "another-tab", false, 0, world.recipient.ConversationID, time.Now())
	world.hub.NotifyChatMessage(message, "", false, []Recipient{world.recipient})
	world.expectSent(t, 4, "a member whose tab on this chat was hidden")

	// In a group the title says which; a message of a file only says so.
	fileOnly := &models.AgentMessage{AuthorKind: models.ChatAuthorPerson, AuthorUserID: "another-person", AuthorName: "Maria", Attachments: []models.AgentAttachment{{ID: "a-file"}}}
	world.hub.NotifyChatMessage(fileOnly, "Weekend trip", true, []Recipient{world.recipient})
	sent = world.expectSent(t, 5, "a group message")
	content = world.read(t, sent[4])
	if content.Title != "Maria in Weekend trip" || !strings.Contains(content.Body, "file") {
		t.Errorf("the group notification says %+v", content)
	}

	// A member with no agent row to hang a phone from is not notified, and a
	// phone that chose other kinds is not sent a chat.
	world.hub.NotifyChatMessage(message, "", false, []Recipient{{UserID: world.recipient.UserID, ConversationID: "the-members-conversation"}})
	world.expectSent(t, 5, "a recipient with no agent row")
	if other := world.relay.to("sealed:the-other-registration"); len(other) != 0 {
		t.Errorf("a phone that wants only approvals was sent %d chat notifications", len(other))
	}
}

// An agent's answer is told to its owner too, who may be waiting for it away
// from the screen, under the agent's name; a person's own message is not.
func TestAnAgentsAnswerIsNotifiedToItsOwner(t *testing.T) {
	world := newPhoneWorld(t)
	answer := &models.AgentMessage{
		ID: "an-answer", AuthorKind: models.ChatAuthorAgent, AuthorUserID: world.recipient.UserID, AuthorName: "Bertie",
		AuthorOwnerName: "Alice", Content: "Thursday at 3.",
	}
	world.hub.NotifyChatMessage(answer, "Weekend trip", true, []Recipient{world.recipient})
	sent := world.expectSent(t, 1, "the owner's agent's answer")
	if content := world.read(t, sent[0]); content.Title != "Bertie · Alice's agent in Weekend trip" || content.Body != "Thursday at 3." {
		t.Errorf("the notification says %+v", content)
	}
}

// Most agents keep the name "Agent", so a notification of an agent's message
// says whose it is: with the agent's name when it has been given one, and
// without repeating the word when it has not. A person is named as they are.
func TestAnAgentsNotificationTitleSaysWhoseAgentItIs(t *testing.T) {
	t.Parallel()

	agentMessage := func(agentName, ownerName string) *models.AgentMessage {
		return &models.AgentMessage{AuthorKind: models.ChatAuthorAgent, AuthorName: agentName, AuthorOwnerName: ownerName}
	}
	for name, test := range map[string]struct {
		message *models.AgentMessage
		chat    string
		isGroup bool
		want    string
	}{
		"a named agent in a group":        {agentMessage("Bertie", "Alice"), "Weekend trip", true, "Bertie · Alice's agent in Weekend trip"},
		"a default name in a group":       {agentMessage("Agent", "Alice"), "Weekend trip", true, "Alice's agent in Weekend trip"},
		"another default name in a group": {agentMessage("Agent", "Maria"), "Weekend trip", true, "Maria's agent in Weekend trip"},
		"a default name in a direct chat": {agentMessage("Agent", "Alice"), "", false, "Alice's agent"},
		"an agent with no owner recorded": {agentMessage("Bertie", ""), "", false, "Bertie"},
		"a person in a group":             {&models.AgentMessage{AuthorKind: models.ChatAuthorPerson, AuthorName: "Maria"}, "Weekend trip", true, "Maria in Weekend trip"},
	} {
		if got := notificationTitle(test.message, test.chat, test.isGroup); got != test.want {
			t.Errorf("%s: the title is %q, want %q", name, got, test.want)
		}
	}
}
