package apigraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// chatWorld is the circle world with the chat hub and the agent's worker behind
// the API, so that what happens in a chat is published to the feed the way a
// running server does. A server with no model has the hub and no worker; see
// newChatWorldWithoutWorker.
type chatWorld struct {
	*circleWorld
	worker *agent.Agent
	hub    *chat.Hub

	// frank may use the agent as well as circles, and has his on. grace may
	// too and has never made one. heidi may audit every agent.
	frank, grace, heidi *models.User
}

func newChatWorld(t *testing.T) *chatWorld {
	t.Helper()
	return newChatWorldWith(t, true)
}

// newChatWorldWithoutWorker is the server of a household that runs no model:
// the chat hub is there and the agent's worker is not.
func newChatWorldWithoutWorker(t *testing.T) *chatWorld {
	t.Helper()
	return newChatWorldWith(t, false)
}

func newChatWorldWith(t *testing.T, hasWorker bool) *chatWorld {
	t.Helper()
	world := &chatWorld{circleWorld: newCircleWorld(t)}
	configuration := config.Default()
	world.hub = chat.New(&chat.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration }, Instance: "chat-test",
	})
	world.graph.settings.Chat = world.hub
	if hasWorker {
		world.worker = agent.New(&agent.Settings{
			Database: world.database, Configuration: func() *config.Configuration { return configuration },
			Instance: "chat-test", Tick: time.Hour, Presence: world.hub.Presence(), Phones: world.hub.Phones(), ChatEvents: world.hub,
		})
		world.graph.settings.Agent = world.worker
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if world.frank, err = tx.CreateUser(&models.User{Username: "chat-frank", Name: "Chat-frank"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		role, err := tx.CreateRole(&models.Role{Name: "chat-frank role", Permissions: []models.Permission{models.PermissionCircleUse, models.PermissionAgentUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "chat-frank group", UserIDs: []string{world.frank.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: world.frank.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: username})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			return user
		}
		world.grace = create("chat-grace", models.PermissionCircleUse, models.PermissionAgentUse)
		world.heidi = create("chat-heidi", models.PermissionAgentAudit)
	})
	return world
}

// chatFields is a chat conversation as the tests read it.
const chatTestFields = `{ id kind title lastAt chat { chatId chatKind circleId title isCircleChat unreadCount isMuted mutedUntil isRemoved
	members { userId username name avatarColorIndex memberRole isDeletedAccount isRemoved readThroughMessageId isAgentReading } } }`

// shareCircle puts people in a circle together.
func (self *chatWorld) shareCircle(t *testing.T, admin *models.User, name string, others ...*models.User) string {
	t.Helper()
	circleId := self.makeCircle(t, admin, name, "friends")
	for _, other := range others {
		self.join(t, admin, circleId, other, "member")
	}
	return circleId
}

// openDirect opens the direct chat from one person to another and returns
// their conversation's id.
func (self *chatWorld) openDirect(t *testing.T, from, to *models.User) string {
	t.Helper()
	data := self.call(t, from, fmt.Sprintf(`mutation { OpenDirectChat(userId: %q) %s }`, to.ID, chatTestFields))
	return data["OpenDirectChat"].(map[string]any)["id"].(string)
}

// say sends a message in a chat as a person and returns nothing; the run id
// of a chat's message is empty.
func (self *chatWorld) say(t *testing.T, from *models.User, conversationId, message string) {
	t.Helper()
	data := self.call(t, from, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: %q) { runId conversationId } }`, conversationId, message))
	turn := data["AskAgent"].(map[string]any)
	if turn["runId"] != "" || turn["conversationId"] != conversationId {
		t.Fatalf("saying %q in a chat answered %v; want no run", message, turn)
	}
}

// read is a conversation's messages as a person reads them.
func (self *chatWorld) read(t *testing.T, as *models.User, conversationId string) []map[string]any {
	t.Helper()
	data := self.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) {
		total conversation %s
		messages { id role content authorKind authorUserId authorName authorOwnerName authorAgentId authorAvatarColorIndex isMine editedAt isDeleted
			references { agentMessageId quotedRole quotedText } attachments { id name }
			reactions { emoji reactorKind reactorUserId reactorName } } } }`, conversationId, chatTestFields))
	view := data["ReadAgentConversation"].(map[string]any)
	messages := []map[string]any{}
	for _, each := range view["messages"].([]any) {
		messages = append(messages, each.(map[string]any))
	}
	return messages
}

// chatOf is the chat part of a conversation as the person lists it.
func (self *chatWorld) chatOf(t *testing.T, as *models.User, conversationId string) map[string]any {
	t.Helper()
	data := self.call(t, as, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)
	for _, each := range data["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if conversation["id"] == conversationId {
			return conversation["chat"].(map[string]any)
		}
	}
	t.Fatalf("%s does not list the chat conversation %s", as.Username, conversationId)
	return nil
}

// events collects what a conversation's feed carries until the test reads it.
type chatEvents struct {
	channel <-chan agent.Event
	stop    func()
}

func (self *chatWorld) follow(conversationId string) *chatEvents {
	channel, stop := self.hub.Subscribe(conversationId)
	return &chatEvents{channel: channel, stop: stop}
}

// next is the next event of the kind within a second, or nil.
func (self *chatEvents) next(kind agent.EventKind) *agent.Event {
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-self.channel:
			if event.Kind == kind {
				return &event
			}
		case <-deadline:
			return nil
		}
	}
}

// none says nothing of the kind arrives in the time it would take.
func (self *chatEvents) none(kind agent.EventKind) bool {
	deadline := time.After(150 * time.Millisecond)
	for {
		select {
		case event := <-self.channel:
			if event.Kind == kind {
				return false
			}
		case <-deadline:
			return true
		}
	}
}

func TestAChatIsListedLiveWithWhatIsUnread(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)

	// Bob has never turned on an agent, and sees the chat anyway.
	bobChat := ""
	data := world.call(t, world.bob, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)
	for _, each := range data["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if conversation["chat"].(map[string]any)["chatKind"] == "direct" {
			bobChat = conversation["id"].(string)
			chat := conversation["chat"].(map[string]any)
			if chat["title"] != "Circle-alice" || conversation["title"] != "Circle-alice" || chat["unreadCount"] != float64(0) || chat["isMuted"] != false || chat["isRemoved"] != false || chat["circleId"] != nil {
				t.Errorf("Bob's chat before anything was said is %v", conversation)
			}
		}
	}
	if bobChat == "" || bobChat == aliceChat {
		t.Fatalf("Bob's conversation is %q, Alice's %q; each has their own", bobChat, aliceChat)
	}

	events := world.follow(bobChat)
	defer events.stop()
	world.say(t, world.alice, aliceChat, "dinner at 7?")

	said := events.next(agent.EventSaid)
	if said == nil || said.Message == nil || said.Message.Content != "dinner at 7?" || said.Message.ConversationID != bobChat ||
		said.Message.AuthorName != "Circle-alice" || said.Message.IsMine || said.Message.Role != "user" || said.Message.AuthorKind != models.ChatAuthorPerson {
		t.Fatalf("Bob's feed carried %+v", said)
	}
	if chat := world.chatOf(t, world.bob, bobChat); chat["unreadCount"] != float64(1) {
		t.Errorf("Bob has %v unread, want 1", chat["unreadCount"])
	}
	if chat := world.chatOf(t, world.alice, aliceChat); chat["unreadCount"] != float64(0) {
		t.Errorf("Alice has %v unread of her own message", chat["unreadCount"])
	}

	// The chat is at the top of Bob's list, with the newest message, above the
	// circle's chat; and with his agent off the list holds chats and nothing else.
	list := world.call(t, world.bob, `query { ListAgentConversations `+chatTestFields+` }`)["ListAgentConversations"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != bobChat {
		t.Errorf("Bob, with no agent on, lists %v; want his direct chat first", list)
	}
	for _, each := range list {
		if each.(map[string]any)["kind"] != "chat" {
			t.Errorf("Bob, with no agent on, lists %v", each)
		}
	}

	messages := world.read(t, world.bob, bobChat)
	if len(messages) != 1 || messages[0]["isMine"] != false || messages[0]["authorName"] != "Circle-alice" || messages[0]["authorUserId"] != world.alice.ID {
		t.Fatalf("Bob reads %v", messages)
	}
	messages = world.read(t, world.alice, aliceChat)
	if len(messages) != 1 || messages[0]["isMine"] != true {
		t.Fatalf("Alice reads %v", messages)
	}

	// Bob reads it; Alice is told, and sees the mark on the chat.
	aliceEvents := world.follow(aliceChat)
	defer aliceEvents.stop()
	world.call(t, world.bob, fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: %q) }`, bobChat, messages[0]["id"]))
	read := aliceEvents.next(agent.EventRead)
	if read == nil || read.UserID != world.bob.ID || read.MessageID != messages[0]["id"] {
		t.Errorf("Alice's feed carried %+v", read)
	}
	for _, each := range world.chatOf(t, world.alice, aliceChat)["members"].([]any) {
		member := each.(map[string]any)
		if member["username"] == world.bob.Username && member["readThroughMessageId"] != messages[0]["id"] {
			t.Errorf("Bob's read mark is %v", member["readThroughMessageId"])
		}
	}
	if chat := world.chatOf(t, world.bob, bobChat); chat["unreadCount"] != float64(0) {
		t.Errorf("after reading, Bob has %v unread", chat["unreadCount"])
	}
}

func TestAPersonWithoutAnActiveAgentCanChatAndDoNothingElse(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	conversationId := world.openDirect(t, world.bob, world.alice)

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		bobAgent, err := tx.GetAgentByUser(world.bob.ID)
		if err != nil || bobAgent == nil || bobAgent.Enabled || bobAgent.Active() {
			t.Fatalf("Bob's agent is %+v, %v; want one that is switched off", bobAgent, err)
		}
	})
	world.say(t, world.bob, conversationId, "hello from a person with no agent")
	if messages := world.read(t, world.bob, conversationId); len(messages) != 1 {
		t.Fatalf("Bob reads %v", messages)
	}
	world.call(t, world.bob, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, world.read(t, world.bob, conversationId)[0]["id"]))

	// And nothing else of the agent's.
	for _, query := range []string{
		`query { ReadAgentConversation { total } }`,
		`mutation { AskAgent(message: "hello") { runId } }`,
		`mutation { StartAgentConversation(title: "x") { id } }`,
		`query { ListAgentTools { name } }`,
		`mutation { SetAgentMainConversation { id } }`,
	} {
		world.refused(t, world.bob, query)
	}
	// The list holds chats and no main conversation; and it made none.
	list := world.call(t, world.bob, `query { ListAgentConversations { id kind } }`)["ListAgentConversations"].([]any)
	if len(list) == 0 {
		t.Error("Bob lists nothing")
	}
	for _, each := range list {
		if each.(map[string]any)["kind"] != "chat" {
			t.Errorf("Bob lists %v", list)
		}
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		bobAgent, _ := tx.GetAgentByUser(world.bob.ID)
		mains, err := tx.ListAgentConversations(bobAgent.ID, []models.AgentConversationKind{models.AgentConversationMain}, nil)
		if err != nil || len(mains) != 0 {
			t.Errorf("a main conversation was made for a person whose agent is off: %v %v", mains, err)
		}
	})
	// For a person whose agent is on, the main conversation comes first and
	// the chat is among the rest.
	world.shareCircle(t, world.alice, "Work", world.frank)
	world.openDirect(t, world.frank, world.alice)
	list = world.call(t, world.frank, `query { ListAgentConversations { id kind } }`)["ListAgentConversations"].([]any)
	kinds := []string{}
	for _, each := range list {
		kinds = append(kinds, each.(map[string]any)["kind"].(string))
	}
	if len(kinds) < 2 || kinds[0] != "main" || kinds[1] != "chat" {
		t.Errorf("Frank, with his agent on, lists %v", kinds)
	}

	// A person who cannot use circles has no chats, whatever their agent.
	world.refused(t, world.dave, fmt.Sprintf(`mutation { OpenDirectChat(userId: %q) { id } }`, world.alice.ID))
	world.refused(t, world.dave, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hi") { runId } }`, conversationId))
}

// The agent row that chatting needs is no agent to anything that asks whether
// a person uses one: their settings page offers to turn it on as it does for a
// person with none, the operator's list does not show it, and a mailbox cannot
// be granted to it. Once the person changes anything about their agent it is
// theirs.
func TestAnAgentRowMadeForChattingIsNoAgentToAnythingElse(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.grace)

	// Joining a circle puts a person in its chat, which needs the row.
	var row *models.Agent
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if row, err = tx.GetAgentByUser(world.grace.ID); err != nil || row == nil || !row.IsPlaceholder() || row.Enabled {
			t.Fatalf("Grace has the agent %+v after she joined a circle: %v", row, err)
		}
	})
	if view := world.call(t, world.grace, `query { ReadAgent { agent { id enabled } } }`)["ReadAgent"].(map[string]any); view["agent"] != nil {
		t.Errorf("Grace's settings page is shown the agent %v rather than the offer to turn it on", view["agent"])
	}
	for _, agentSummary := range world.call(t, world.heidi, `query { ListAgents { agentId userId } }`)["ListAgents"].([]any) {
		if agentSummary.(map[string]any)["agentId"] == row.ID {
			t.Error("the operator's list shows an agent nobody uses")
		}
	}
	if message := world.refused(t, world.grace, `mutation { GrantAgentMailbox(mailboxId: "any") { agent { id } } }`); !strings.Contains(message, "not found") && !strings.Contains(message, "turn the agent on") {
		t.Errorf("granting a mailbox was answered %q", message)
	}

	// A person with no circle is made nothing by looking at their chats.
	loner := world.erin
	world.call(t, loner, `query { ListAgentConversations { id } }`)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if found, err := tx.GetAgentByUser(loner.ID); err != nil || found != nil {
			t.Errorf("a person in no circle was given the agent %+v: %v", found, err)
		}
	})
}

func TestAStrangerCannotOpenAChatAndIsToldNothingMore(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)

	// Erin shares no circle with Alice, and no such account reads the same.
	for _, userId := range []string{world.alice.ID, "no-such-account", world.dave.ID} {
		message := world.refused(t, world.erin, fmt.Sprintf(`mutation { OpenDirectChat(userId: %q) { id } }`, userId))
		if !strings.Contains(message, "not found") {
			t.Errorf("opening a chat with %s was answered %q", userId, message)
		}
	}
	world.refused(t, world.erin, fmt.Sprintf(`mutation { CreateGroupChat(title: "Us", userIds: [%q]) { id } }`, world.alice.ID))
	world.refused(t, world.alice, `mutation { OpenDirectChat(userId: "") { id } }`)
	if message := world.refused(t, world.alice, fmt.Sprintf(`mutation { OpenDirectChat(userId: %q) { id } }`, world.alice.ID)); !strings.Contains(message, "somebody else") {
		t.Errorf("a chat with oneself was answered %q", message)
	}

	// Nothing was made for the stranger to find.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		chat, err := tx.GetDirectChat(world.erin.ID, world.alice.ID)
		if err != nil || chat != nil {
			t.Errorf("a chat exists between strangers: %+v, %v", chat, err)
		}
	})
}

func TestOpeningAChatTwiceIsTheSameChat(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	first := world.openDirect(t, world.alice, world.bob)
	if again := world.openDirect(t, world.alice, world.bob); again != first {
		t.Errorf("opening it again gave %s, not %s", again, first)
	}
	fromBob := world.openDirect(t, world.bob, world.alice)
	if fromBob == first {
		t.Error("Bob was given Alice's conversation")
	}
	if world.chatOf(t, world.alice, first)["chatId"] != world.chatOf(t, world.bob, fromBob)["chatId"] {
		t.Error("the two of them are not in one chat")
	}
	// Alice has one conversation for it, however often she opened it.
	list := world.call(t, world.alice, `query { ListAgentConversations(kind: "chat") { id chat { chatKind } } }`)["ListAgentConversations"].([]any)
	direct := 0
	for _, each := range list {
		if each.(map[string]any)["chat"].(map[string]any)["chatKind"] == "direct" {
			direct++
		}
	}
	if direct != 1 {
		t.Errorf("Alice lists %d direct chats", direct)
	}
}

func TestAMemberNeverReadsAnotherMembersAgentRows(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.carol)
	aliceChat := world.openDirect(t, world.alice, world.carol)
	carolChat := world.openDirect(t, world.carol, world.alice)
	world.say(t, world.alice, aliceChat, "can you look something up?")

	// What Carol's agent did to answer is in her conversation and nobody else's.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: carolChat, Role: "tool", Name: "mail_search", Content: "a summary of Carol's mail"}); err != nil {
			t.Fatal(err)
		}
	})
	world.say(t, world.carol, carolChat, "yes")
	for _, message := range world.read(t, world.alice, aliceChat) {
		if strings.Contains(fmt.Sprint(message["content"]), "Carol's mail") {
			t.Errorf("Alice read a row of Carol's agent: %v", message)
		}
	}
	if messages := world.read(t, world.carol, carolChat); len(messages) != 3 || messages[1]["role"] != "tool" {
		t.Errorf("Carol reads %v; want the chat and her own agent's row by time", messages)
	}
	// Nobody reaches a conversation that is another's: not to read, not to write.
	for _, query := range []string{
		fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, carolChat),
		fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "as carol") { runId } }`, carolChat),
		fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: "x") }`, carolChat),
		fmt.Sprintf(`mutation { MuteChat(conversationId: %q) { id } }`, carolChat),
		fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, carolChat),
	} {
		world.refused(t, world.alice, query)
	}
	// Not even an operator who may act as other people.
	world.refused(t, world.operator, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, carolChat))
	// Nor to react in it as them, to what her agent said there or to take
	// one of her reactions away.
	var answerId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		answer, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: carolChat, Role: "assistant", Content: "here is what I found"})
		if err != nil {
			t.Fatal(err)
		}
		answerId = answer.ID
	})
	var actor *models.User
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if actor, err = tx.CreateUser(&models.User{Username: "chat-actor", Name: "chat-actor"}); err != nil {
			t.Fatal(err)
		}
		role, err := tx.CreateRole(&models.Role{Name: "chat-actor role", Permissions: []models.Permission{models.PermissionCircleUse, models.PermissionAgentUse, models.PermissionAgentAct}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "chat-actor group", UserIDs: []string{actor.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: actor.ID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	})
	world.refused(t, actor, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, carolChat))
	for _, emoji := range []string{"👍", ""} {
		message := world.refused(t, actor, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: %q) }`, answerId, emoji))
		if !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("a person who may act as others, reacting in a chat as Carol, was told %q; want not found", message)
		}
	}
	world.call(t, world.carol, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, answerId))
}

func TestRemovingSomeoneFromACircleRemovesThemFromItsChatsInOneStep(t *testing.T) {
	world := newChatWorld(t)
	circleId := world.shareCircle(t, world.alice, "Home", world.bob, world.carol)

	// The circle's chat is there for each of them, with the circle's name.
	circleChat := ""
	data := world.call(t, world.bob, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)
	for _, each := range data["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if chat := conversation["chat"].(map[string]any); chat["isCircleChat"] == true {
			circleChat = conversation["id"].(string)
			if chat["title"] != "Home" || chat["circleId"] != circleId || len(chat["members"].([]any)) != 3 {
				t.Errorf("the circle chat is %v", chat)
			}
		}
	}
	if circleChat == "" {
		t.Fatal("Bob has no circle chat")
	}
	world.say(t, world.bob, circleChat, "hello family")

	// A group chat of the three, and Bob's direct chat with Alice.
	group := world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q, %q]) %s }`, world.bob.ID, world.carol.ID, chatTestFields))["CreateGroupChat"].(map[string]any)["id"].(string)
	bobGroup := ""
	for _, each := range world.call(t, world.bob, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)["ListAgentConversations"].([]any) {
		if chat := each.(map[string]any)["chat"].(map[string]any); chat["title"] == "Trip" {
			bobGroup = each.(map[string]any)["id"].(string)
		}
	}
	direct := world.openDirect(t, world.alice, world.bob)
	bobDirect := world.openDirect(t, world.bob, world.alice)
	world.say(t, world.alice, direct, "see you")

	world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, circleId, world.bob.ID))

	// Out of the circle chat and out of the group, where he now shares a circle
	// with nobody; the direct chat is still his to read.
	for name, conversationId := range map[string]string{"the circle chat": circleChat, "the group": bobGroup} {
		if chat := world.chatOf(t, world.bob, conversationId); chat["isRemoved"] != true {
			t.Errorf("Bob is still in %s: %v", name, chat)
		}
		message := world.refused(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "still here?") { runId } }`, conversationId))
		if !strings.Contains(message, "you are no longer in this chat") {
			t.Errorf("writing in %s was answered %q", name, message)
		}
	}
	if messages := world.read(t, world.bob, circleChat); len(messages) != 1 || messages[0]["content"] != "hello family" {
		t.Errorf("Bob reads %v of the circle chat after leaving", messages)
	}
	for _, each := range world.call(t, world.carol, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)["ListAgentConversations"].([]any) {
		if chat := each.(map[string]any)["chat"].(map[string]any); chat["title"] == "Trip" && len(chat["members"].([]any)) != 3 {
			// Bob stays on the list of those who were in it.
			t.Errorf("Carol sees the members %v", chat["members"])
		}
	}
	_ = group
	if chat := world.chatOf(t, world.bob, bobDirect); chat["isRemoved"] != false {
		t.Errorf("Bob left his direct chat: %v", chat)
	}
	if messages := world.read(t, world.bob, bobDirect); len(messages) != 1 {
		t.Errorf("Bob reads %v of his direct chat", messages)
	}
	// They no longer share a circle, so neither may write there.
	for _, as := range []*models.User{world.alice, world.bob} {
		conversationId := direct
		if as == world.bob {
			conversationId = bobDirect
		}
		message := world.refused(t, as, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello?") { runId } }`, conversationId))
		if !strings.Contains(message, "you no longer share a circle") {
			t.Errorf("%s writing in a direct chat without a circle was answered %q", as.Username, message)
		}
	}
	// Opening it from either side is still the same chat, and nothing new can be started.
	if again := world.openDirect(t, world.bob, world.alice); again != bobDirect {
		t.Errorf("opening the old chat gave %s, not %s", again, bobDirect)
	}
	world.refused(t, world.bob, fmt.Sprintf(`mutation { OpenDirectChat(userId: %q) { id } }`, world.erin.ID))
}

func TestAMemberWhoWasRemovedGetsNoMoreEvents(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	group := world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q, %q]) { id } }`, world.bob.ID, world.carol.ID))["CreateGroupChat"].(map[string]any)["id"].(string)
	bobGroup, carolGroup := "", ""
	for user, into := range map[*models.User]*string{world.bob: &bobGroup, world.carol: &carolGroup} {
		for _, each := range world.call(t, user, `query { ListAgentConversations(kind: "chat") { id chat { title } } }`)["ListAgentConversations"].([]any) {
			if each.(map[string]any)["chat"].(map[string]any)["title"] == "Trip" {
				*into = each.(map[string]any)["id"].(string)
			}
		}
	}
	bobEvents, carolEvents := world.follow(bobGroup), world.follow(carolGroup)
	defer bobEvents.stop()
	defer carolEvents.stop()

	world.say(t, world.alice, group, "first")
	if bobEvents.next(agent.EventSaid) == nil || carolEvents.next(agent.EventSaid) == nil {
		t.Fatal("a member did not hear what was said")
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, group, world.bob.ID))
	world.say(t, world.alice, group, "second")
	if carolEvents.next(agent.EventSaid) == nil {
		t.Error("Carol did not hear the second")
	}
	if !bobEvents.none(agent.EventSaid) {
		t.Error("Bob, who was taken out, still hears the chat")
	}
	// Nor does he read it: only up to when he left.
	if messages := world.read(t, world.bob, bobGroup); len(messages) != 1 || messages[0]["content"] != "first" {
		t.Errorf("Bob reads %v", messages)
	}
}

func TestADeletedAccountsWordsKeepTheNameItHad(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.carol)
	aliceChat := world.openDirect(t, world.alice, world.carol)
	carolChat := world.openDirect(t, world.carol, world.alice)
	world.say(t, world.carol, carolChat, "see you soon")

	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.DeleteUser(world.carol.ID); err != nil {
			t.Fatal(err)
		}
	})
	messages := world.read(t, world.alice, aliceChat)
	if len(messages) != 1 || messages[0]["authorName"] != "Circle-carol" || messages[0]["authorUserId"] != "" || messages[0]["isMine"] != false {
		t.Fatalf("Alice reads %v", messages)
	}
	isFound := false
	for _, each := range world.chatOf(t, world.alice, aliceChat)["members"].([]any) {
		member := each.(map[string]any)
		if member["name"] == "Circle-carol" {
			isFound = true
			if member["isDeletedAccount"] != true || member["isRemoved"] != true || member["userId"] != "" {
				t.Errorf("Carol's place is %v", member)
			}
		}
	}
	if !isFound {
		t.Error("the deleted account is gone from the members")
	}
	message := world.refused(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello?") { runId } }`, aliceChat))
	if !strings.Contains(message, "account is gone") {
		t.Errorf("writing to a deleted account was answered %q", message)
	}
}

func TestEachPersonHasOneReactionOnAChatMessage(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	world.say(t, world.alice, aliceChat, "lunch?")
	messageId := world.read(t, world.alice, aliceChat)[0]["id"].(string)

	events := world.follow(aliceChat)
	defer events.stop()
	react := func(as *models.User, emoji string) {
		t.Helper()
		world.call(t, as, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: %q) }`, messageId, emoji))
	}
	react(world.bob, "👍")
	reacted := events.next(agent.EventReacted)
	if reacted == nil || reacted.Emoji != "👍" || reacted.ReactorUserID != world.bob.ID || reacted.ReactorName != "Circle-bob" || reacted.MessageID != messageId {
		t.Fatalf("Alice's feed carried %+v", reacted)
	}
	// People may react to their own words in a chat, and each has one.
	react(world.alice, "🙏")
	react(world.bob, "❤️")
	reactions := world.read(t, world.bob, bobChat)[0]["reactions"].([]any)
	if len(reactions) != 2 {
		t.Fatalf("the message has the reactions %v; want one for each person", reactions)
	}
	byName := map[string]string{}
	for _, each := range reactions {
		reaction := each.(map[string]any)
		byName[reaction["reactorName"].(string)] = reaction["emoji"].(string)
		if reaction["reactorKind"] != "person" {
			t.Errorf("a reaction is of the kind %v", reaction["reactorKind"])
		}
	}
	if byName["Circle-alice"] != "🙏" || byName["Circle-bob"] != "❤️" {
		t.Errorf("the reactions are %v", byName)
	}
	react(world.alice, "")
	if reactions = world.read(t, world.alice, aliceChat)[0]["reactions"].([]any); len(reactions) != 1 {
		t.Errorf("after Alice took hers away: %v", reactions)
	}
	// Somebody outside the chat reaches nothing, and an emoji that is not one is refused.
	world.refused(t, world.carol, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, messageId))
	world.refused(t, world.bob, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "not an emoji") }`, messageId))
}

func TestOnlyTheAuthorEditsAndAnAdminOfAGroupCanDeleteAnyMessage(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	group := world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q, %q]) { id } }`, world.bob.ID, world.carol.ID))["CreateGroupChat"].(map[string]any)["id"].(string)
	groupOf := func(user *models.User) string {
		for _, each := range world.call(t, user, `query { ListAgentConversations(kind: "chat") { id chat { title } } }`)["ListAgentConversations"].([]any) {
			if each.(map[string]any)["chat"].(map[string]any)["title"] == "Trip" {
				return each.(map[string]any)["id"].(string)
			}
		}
		return ""
	}
	bobGroup, carolGroup := groupOf(world.bob), groupOf(world.carol)
	world.say(t, world.bob, bobGroup, "meet at nine")
	world.say(t, world.carol, carolGroup, "ok")
	messages := world.read(t, world.alice, group)
	bobMessage, carolMessage := messages[0]["id"].(string), messages[1]["id"].(string)

	events := world.follow(carolGroup)
	defer events.stop()
	// Bob changes his own, and the others are told with the new words.
	edited := world.call(t, world.bob, fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "meet at ten") { id content editedAt isMine } }`, bobMessage))["EditChatMessage"].(map[string]any)
	if edited["content"] != "meet at ten" || edited["editedAt"] == nil || edited["isMine"] != true {
		t.Errorf("EditChatMessage answered %v", edited)
	}
	event := events.next(agent.EventEdited)
	if event == nil || event.Message == nil || event.Message.Content != "meet at ten" || event.Message.IsMine || event.Message.EditedAt == nil {
		t.Errorf("Carol's feed carried %+v", event)
	}
	if reread := world.read(t, world.carol, carolGroup); reread[0]["content"] != "meet at ten" || reread[0]["editedAt"] == nil {
		t.Errorf("Carol reads %v", reread[0])
	}
	// Nobody else edits it, an admin included, and a message needs words.
	for _, as := range []*models.User{world.alice, world.carol} {
		if message := world.refused(t, as, fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "mine now") { id } }`, bobMessage)); !strings.Contains(message, "only the person who wrote") {
			t.Errorf("%s editing another's message was told %q", as.Username, message)
		}
	}
	world.refused(t, world.bob, fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "  ") { id } }`, bobMessage))

	// A member takes back only their own; the group's admin any.
	if message := world.refused(t, world.carol, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, bobMessage)); !strings.Contains(message, "only the person who wrote") {
		t.Errorf("a member deleting another's message was told %q", message)
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, bobMessage))
	unsaid := events.next(agent.EventUnsaid)
	if unsaid == nil || unsaid.MessageID != bobMessage {
		t.Errorf("Carol's feed carried %+v", unsaid)
	}
	taken := world.read(t, world.carol, carolGroup)[0]
	if taken["isDeleted"] != true || taken["content"] != "" {
		t.Errorf("a message taken back reads %v", taken)
	}
	world.call(t, world.carol, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, carolMessage))
	world.refused(t, world.bob, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, carolMessage))

	// In a direct chat the other person is not an admin of anything.
	direct := world.openDirect(t, world.alice, world.bob)
	bobDirect := world.openDirect(t, world.bob, world.alice)
	world.say(t, world.bob, bobDirect, "hi")
	directMessage := world.read(t, world.alice, direct)[0]["id"]
	world.refused(t, world.alice, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, directMessage))
}

func TestAChatReadMarkNeverMovesBack(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	world.say(t, world.alice, aliceChat, "one")
	world.say(t, world.alice, aliceChat, "two")
	world.say(t, world.alice, aliceChat, "three")
	messages := world.read(t, world.bob, bobChat)
	mark := func(messageId any) bool {
		t.Helper()
		return world.call(t, world.bob, fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: %q) }`, bobChat, messageId))["MarkChatRead"].(bool)
	}
	if !mark(messages[1]["id"]) || mark(messages[0]["id"]) || mark(messages[1]["id"]) {
		t.Error("the mark did not move forward once and stay")
	}
	if chat := world.chatOf(t, world.bob, bobChat); chat["unreadCount"] != float64(1) {
		t.Errorf("Bob has %v unread, want the third", chat["unreadCount"])
	}
	if !mark(messages[2]["id"]) {
		t.Error("the mark did not move to the newest")
	}
	var markedThrough any
	for _, each := range world.chatOf(t, world.alice, aliceChat)["members"].([]any) {
		if member := each.(map[string]any); member["username"] == world.bob.Username {
			markedThrough = member["readThroughMessageId"]
		}
	}
	if markedThrough != messages[2]["id"] {
		t.Errorf("Bob's mark is %v", markedThrough)
	}
	// A message that is not in the chat moves nothing.
	world.refused(t, world.bob, fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: "no-such-message") }`, bobChat))
}

func TestAMessageIsBoundedAndNeedsWordsOrAFile(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	conversationId := world.openDirect(t, world.alice, world.bob)
	ask := func(message string) string {
		t.Helper()
		_, errors := world.ask(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: %q) { runId } }`, conversationId, message))
		return strings.Join(errors, "; ")
	}
	if message := ask("   "); !strings.Contains(message, "say something") {
		t.Errorf("an empty message was answered %q", message)
	}
	if message := ask(strings.Repeat("é", models.ChatMessageLength+1)); !strings.Contains(message, "at most 16000 characters") {
		t.Errorf("a message of 16001 characters was answered %q", message)
	}
	if message := ask(strings.Repeat("é", models.ChatMessageLength)); message != "" {
		t.Errorf("a message of exactly 16000 characters was refused: %q", message)
	}
	// A read-only profile does not write to other people.
	if message := world.refused(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hi", readOnly: true) { runId } }`, conversationId)); !strings.Contains(message, "read-only") {
		t.Errorf("a read-only client writing in a chat was answered %q", message)
	}
}

func TestAPersonIsLimitedToSixtyMessagesAMinute(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	conversationId := world.openDirect(t, world.alice, world.bob)
	refusals := 0
	for count := 0; count < models.ChatMessagesPerMinute+5; count++ {
		_, errors := world.ask(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "message %d") { runId } }`, conversationId, count))
		if len(errors) > 0 {
			if !strings.Contains(strings.Join(errors, ";"), "too many") {
				t.Fatalf("message %d was refused with %v", count, errors)
			}
			refusals++
		}
	}
	if refusals < 4 || refusals > 5 {
		t.Errorf("%d of %d messages in a burst were refused; want the ones past sixty", refusals, models.ChatMessagesPerMinute+5)
	}
	// Bob has his own allowance.
	world.say(t, world.bob, world.openDirect(t, world.bob, world.alice), "I can still talk")
}

func TestAReplyIsShownWithWhatItRepliesTo(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	world.say(t, world.alice, aliceChat, "which day works?")
	question := world.read(t, world.bob, bobChat)[0]["id"]
	world.call(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "Friday", references: [{agentMessageId: %q}]) { runId } }`, bobChat, question))
	reply := world.read(t, world.alice, aliceChat)[1]
	references := reply["references"].([]any)
	if len(references) != 1 || references[0].(map[string]any)["agentMessageId"] != question || references[0].(map[string]any)["quotedText"] != "which day works?" {
		t.Errorf("the reply is shown as %v", reply)
	}
	// A message of some other chat cannot be replied to.
	world.refused(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "x", references: [{agentMessageId: "no-such-message"}]) { runId } }`, bobChat))
}

func TestOnlyAGroupsAdminsChangeItsPeopleAndTitle(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	world.shareCircle(t, world.carol, "Work", world.erin)
	group := world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: " Trip ", userIds: [%q]) %s }`, world.bob.ID, chatTestFields))["CreateGroupChat"].(map[string]any)
	if group["title"] != "Trip" || group["chat"].(map[string]any)["chatKind"] != "group" {
		t.Fatalf("CreateGroupChat answered %v", group)
	}
	groupId := group["id"].(string)
	bobGroup := ""
	for _, each := range world.call(t, world.bob, `query { ListAgentConversations(kind: "chat") { id chat { title } } }`)["ListAgentConversations"].([]any) {
		if each.(map[string]any)["chat"].(map[string]any)["title"] == "Trip" {
			bobGroup = each.(map[string]any)["id"].(string)
		}
	}
	// The maker is the admin, and the members are not.
	for _, query := range []string{
		fmt.Sprintf(`mutation { RenameChat(conversationId: %q, title: "Mine") { id } }`, bobGroup),
		fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, bobGroup, world.carol.ID),
		fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, bobGroup, world.alice.ID),
	} {
		if message := world.refused(t, world.bob, query); !strings.Contains(message, "only an admin of this chat") {
			t.Errorf("a member was told %q for %s", message, query)
		}
	}
	renamed := world.call(t, world.alice, fmt.Sprintf(`mutation { RenameChat(conversationId: %q, title: "Weekend") { title chat { title } } }`, groupId))["RenameChat"].(map[string]any)
	if renamed["title"] != "Weekend" {
		t.Errorf("RenameChat answered %v", renamed)
	}
	// Added people must each share a circle with the adder.
	world.refused(t, world.alice, fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, groupId, world.erin.ID))
	world.refused(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "x", userIds: [%q]) { id } }`, world.erin.ID))
	world.refused(t, world.alice, `mutation { CreateGroupChat(title: "x", userIds: []) { id } }`)
	added := world.call(t, world.alice, fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) %s }`, groupId, world.carol.ID, chatTestFields))["AddChatMembers"].(map[string]any)
	if len(added["chat"].(map[string]any)["members"].([]any)) != 3 {
		t.Errorf("after adding Carol: %v", added["chat"])
	}
	// A member leaves, and an admin removes another.
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, bobGroup))
	if chat := world.chatOf(t, world.bob, bobGroup); chat["isRemoved"] != true {
		t.Errorf("Bob left, and the chat says %v", chat)
	}
	world.refused(t, world.bob, fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, bobGroup))
	removed := world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) %s }`, groupId, world.carol.ID, chatTestFields))["RemoveChatMember"].(map[string]any)
	activeCount := 0
	for _, each := range removed["chat"].(map[string]any)["members"].([]any) {
		if each.(map[string]any)["isRemoved"] == false {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("after removing Carol, %d are in it", activeCount)
	}
	world.refused(t, world.alice, fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, groupId, world.carol.ID))
}

func TestACircleChatCanBeChangedOnlyThroughItsCircle(t *testing.T) {
	world := newChatWorld(t)
	circleId := world.shareCircle(t, world.alice, "Home", world.bob)
	circleChat := ""
	for _, each := range world.call(t, world.alice, `query { ListAgentConversations(kind: "chat") { id chat { isCircleChat } } }`)["ListAgentConversations"].([]any) {
		if each.(map[string]any)["chat"].(map[string]any)["isCircleChat"] == true {
			circleChat = each.(map[string]any)["id"].(string)
		}
	}
	for _, query := range []string{
		fmt.Sprintf(`mutation { RenameChat(conversationId: %q, title: "Other") { id } }`, circleChat),
		fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, circleChat, world.carol.ID),
		fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, circleChat, world.bob.ID),
		fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, circleChat),
	} {
		if message := world.refused(t, world.alice, query); !strings.Contains(message, "circle") && !strings.Contains(message, "chat") {
			t.Errorf("%s was answered %q", query, message)
		}
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { RenameCircle(circleId: %q, name: "Family") { id } }`, circleId))
	if chat := world.chatOf(t, world.bob, world.listedCircleChat(t, world.bob)); chat["title"] != "Family" {
		t.Errorf("the circle chat is called %v after its circle was renamed", chat["title"])
	}
	// The members of the circle are those of its chat; the last one leaving takes both.
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	if chat := world.chatOf(t, world.bob, world.listedCircleChat(t, world.bob)); chat["isRemoved"] != true {
		t.Errorf("Bob left the circle and its chat says %v", chat)
	}
}

// listedCircleChat is the conversation of the circle chat in a person's list.
func (self *chatWorld) listedCircleChat(t *testing.T, as *models.User) string {
	t.Helper()
	for _, each := range self.call(t, as, `query { ListAgentConversations(kind: "chat") { id chat { isCircleChat } } }`)["ListAgentConversations"].([]any) {
		if each.(map[string]any)["chat"].(map[string]any)["isCircleChat"] == true {
			return each.(map[string]any)["id"].(string)
		}
	}
	t.Fatalf("%s has no circle chat", as.Username)
	return ""
}

func TestAnAgentSpeaksInAChatUnderItsNameForItsOwner(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.frank)
	aliceChat := world.openDirect(t, world.alice, world.frank)
	frankChat := world.openDirect(t, world.frank, world.alice)

	said := world.call(t, world.frank, fmt.Sprintf(`mutation { SayInChatAsAgent(conversationId: %q, message: "Frank will be late.") { id content authorName authorOwnerName } }`, frankChat))["SayInChatAsAgent"].(map[string]any)
	if said["content"] != "Frank will be late." || said["authorName"] != models.AgentDefaultName || said["authorOwnerName"] != world.frank.DisplayName() {
		t.Errorf("SayInChatAsAgent answered %v", said)
	}
	message := world.read(t, world.alice, aliceChat)[0]
	if message["role"] != "assistant" || message["authorKind"] != "agent" || message["authorName"] != models.AgentDefaultName || message["authorUserId"] != world.frank.ID ||
		message["authorAgentId"] == "" || message["isMine"] != false || message["content"] != "Frank will be late." {
		t.Errorf("Alice reads %v", message)
	}
	// Both people's agents are called "Agent": whose it is travels with the
	// message, from the chat, not from anything the caller sent.
	if message["authorOwnerName"] != world.frank.DisplayName() {
		t.Errorf("Alice is told the agent is %v's; want %s's", message["authorOwnerName"], world.frank.DisplayName())
	}
	if mine := world.read(t, world.frank, frankChat)[0]; mine["isMine"] != true || mine["role"] != "assistant" {
		t.Errorf("Frank reads his agent's message as %v", mine)
	}
	// Only an agent that is on, of a person who may use it, speaks, and only in the owner's chats.
	world.refused(t, world.alice, fmt.Sprintf(`mutation { SayInChatAsAgent(conversationId: %q, message: "hello") { id } }`, aliceChat))
	world.refused(t, world.alice, fmt.Sprintf(`mutation { SayInChatAsAgent(conversationId: %q, message: "hello") { id } }`, frankChat))
}

func TestAFileSentInAChatIsReadableByTheOthersInIt(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)

	// Alice's upload, as the upload endpoint stores it.
	var attachmentId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		aliceAgent, err := tx.EnsureAgent(world.alice.ID)
		if err != nil {
			t.Fatal(err)
		}
		attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: aliceAgent.ID, Name: "menu.txt", ContentType: "text/plain", Size: 4})
		if err != nil {
			t.Fatal(err)
		}
		attachmentId = attachment.ID
	})
	if err := world.files.PutFile(t.Context(), attachmentId, []byte("menu")); err != nil {
		t.Fatal(err)
	}
	// Somebody else's upload is not Bob's to send.
	world.refused(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "look", attachmentIds: [%q]) { runId } }`, bobChat, attachmentId))
	world.call(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", attachmentIds: [%q]) { runId } }`, aliceChat, attachmentId))
	// The same file is not sent twice.
	world.refused(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "again", attachmentIds: [%q]) { runId } }`, aliceChat, attachmentId))

	message := world.read(t, world.bob, bobChat)[0]
	attachments := message["attachments"].([]any)
	if len(attachments) != 1 || attachments[0].(map[string]any)["id"] != attachmentId || attachments[0].(map[string]any)["name"] != "menu.txt" {
		t.Fatalf("Bob reads %v", message)
	}
	fetch := func(as *models.User) int {
		request := httptest.NewRequest(http.MethodGet, strings.Replace(api.PathAgentAttachment, "{attachmentId}", attachmentId, 1), nil)
		request.Header.Set(api.AuthenticatedUsernameHeader, as.Username)
		response := httptest.NewRecorder()
		world.router.ServeHTTP(response, request)
		return response.Code
	}
	world.router.Path(api.PathAgentAttachment).Methods(http.MethodGet).HandlerFunc(world.graph.agentAttachmentView)
	if code := fetch(world.alice); code != http.StatusOK {
		t.Errorf("the sender fetching the file got %d", code)
	}
	if code := fetch(world.bob); code != http.StatusOK {
		t.Errorf("a member of the chat fetching the file got %d", code)
	}
	if code := fetch(world.carol); code != http.StatusNotFound {
		t.Errorf("someone outside the chat fetching the file got %d", code)
	}
	// Taking the message back takes the file with it.
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, message["id"]))
	if code := fetch(world.bob); code != http.StatusNotFound {
		t.Errorf("after the message was taken back, a member fetching the file got %d", code)
	}
}

func TestAChatIsNotRenamedOrDeletedLikeAConversationWithTheAgent(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.frank)
	frankChat := world.openDirect(t, world.frank, world.alice)
	for query, expected := range map[string]string{
		fmt.Sprintf(`mutation { UpdateAgentConversation(conversationId: %q, title: "x") { id } }`, frankChat):     "RenameChat",
		fmt.Sprintf(`mutation { UpdateAgentConversation(conversationId: %q, archived: true) { id } }`, frankChat): "RenameChat",
		fmt.Sprintf(`mutation { DeleteAgentConversation(conversationId: %q) }`, frankChat):                        "left, not deleted",
		fmt.Sprintf(`mutation { SetAgentMainConversation(conversationId: %q) { id } }`, frankChat):                "named conversation",
	} {
		if message := world.refused(t, world.frank, query); !strings.Contains(message, expected) {
			t.Errorf("%s was answered %q", query, message)
		}
	}
	if chat := world.chatOf(t, world.frank, frankChat); chat["isRemoved"] != false {
		t.Errorf("the refused calls changed the chat: %v", chat)
	}
}

func TestAChatMuteIsPerPersonAndEndsWhenAsked(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	until := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	muted := world.call(t, world.alice, fmt.Sprintf(`mutation { MuteChat(conversationId: %q, mutedUntil: %q) %s }`, aliceChat, until, chatTestFields))["MuteChat"].(map[string]any)
	chat := muted["chat"].(map[string]any)
	if chat["isMuted"] != true || chat["mutedUntil"] == nil {
		t.Errorf("a muted chat is %v", chat)
	}
	bobChat := world.openDirect(t, world.bob, world.alice)
	if chat := world.chatOf(t, world.bob, bobChat); chat["isMuted"] != false {
		t.Errorf("Alice's mute reached Bob: %v", chat)
	}
	unmuted := world.call(t, world.alice, fmt.Sprintf(`mutation { MuteChat(conversationId: %q) %s }`, aliceChat, chatTestFields))["MuteChat"].(map[string]any)
	if chat := unmuted["chat"].(map[string]any); chat["isMuted"] != false || chat["mutedUntil"] != nil {
		t.Errorf("an unmuted chat is %v", chat)
	}
	// A time that has passed is no mute.
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if chat := world.call(t, world.alice, fmt.Sprintf(`mutation { MuteChat(conversationId: %q, mutedUntil: %q) %s }`, aliceChat, past, chatTestFields))["MuteChat"].(map[string]any)["chat"].(map[string]any); chat["isMuted"] != false {
		t.Errorf("a mute in the past left the chat muted: %v", chat)
	}
}

func TestTypingIsToldToTheOthersAndStoresNothing(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	bobEvents, aliceEvents := world.follow(bobChat), world.follow(aliceChat)
	defer bobEvents.stop()
	defer aliceEvents.stop()
	world.call(t, world.alice, fmt.Sprintf(`mutation { SetChatTyping(conversationId: %q) }`, aliceChat))
	typing := bobEvents.next(agent.EventTyping)
	if typing == nil || typing.UserID != world.alice.ID || typing.Name != "Circle-alice" {
		t.Errorf("Bob's feed carried %+v", typing)
	}
	if !aliceEvents.none(agent.EventTyping) {
		t.Error("Alice was told she was typing")
	}
	if messages := world.read(t, world.bob, bobChat); len(messages) != 0 {
		t.Errorf("typing stored %v", messages)
	}
}

func TestEveryChatOperationAnswersNotFoundToAnOutsider(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	world.say(t, world.alice, aliceChat, "private")
	messageId := world.read(t, world.alice, aliceChat)[0]["id"]

	for _, query := range []string{
		fmt.Sprintf(`mutation { RenameChat(conversationId: %q, title: "x") { id } }`, aliceChat),
		fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, aliceChat, world.erin.ID),
		fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, aliceChat, world.alice.ID),
		fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, aliceChat),
		fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "x") { id } }`, messageId),
		fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, messageId),
		fmt.Sprintf(`mutation { MarkChatRead(conversationId: %q, throughMessageId: %q) }`, aliceChat, messageId),
		fmt.Sprintf(`mutation { MuteChat(conversationId: %q) { id } }`, aliceChat),
		fmt.Sprintf(`mutation { SetChatTyping(conversationId: %q) }`, aliceChat),
		fmt.Sprintf(`mutation { SayInChatAsAgent(conversationId: %q, message: "x") { id } }`, aliceChat),
		fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, messageId),
		fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "x") { runId } }`, aliceChat),
		fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, aliceChat),
	} {
		for _, outsider := range []*models.User{world.erin, world.dave} {
			if message := world.refused(t, outsider, query); !strings.Contains(message, "not found") && !strings.Contains(message, "unavailable") {
				t.Errorf("%s was told %q for %s", outsider.Username, message, query)
			}
		}
		// And for a chat or message that is not there.
		missing := strings.ReplaceAll(strings.ReplaceAll(query, aliceChat, "no-such-chat"), fmt.Sprint(messageId), "no-such-message")
		if message := world.refused(t, world.alice, missing); !strings.Contains(message, "not found") {
			t.Errorf("something that is not there answered %q for %s", message, missing)
		}
	}
}

// A reply shows the words of the message it is to, but not to a member who
// joined after that was said: not when they read, not in the event they are
// told, and they cannot reply to it themselves.
func TestAReplyDoesNotQuoteWhatWasSaidBeforeAMemberJoined(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	group := world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q]) %s }`, world.bob.ID, chatTestFields))["CreateGroupChat"].(map[string]any)
	aliceGroup := group["id"].(string)
	conversationOf := func(user *models.User) string {
		for _, each := range world.call(t, user, `query { ListAgentConversations(kind: "chat") { id chat { title } } }`)["ListAgentConversations"].([]any) {
			if each.(map[string]any)["chat"].(map[string]any)["title"] == "Trip" {
				return each.(map[string]any)["id"].(string)
			}
		}
		t.Fatalf("%s has no Trip chat", user.Username)
		return ""
	}
	bobGroup := conversationOf(world.bob)

	world.say(t, world.alice, aliceGroup, "the surprise is a boat")
	surprise := world.read(t, world.alice, aliceGroup)[0]["id"]
	time.Sleep(5 * time.Millisecond)
	world.call(t, world.alice, fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, aliceGroup, world.carol.ID))
	time.Sleep(5 * time.Millisecond)
	carolGroup := conversationOf(world.carol)

	aliceFeed, carolFeed := world.follow(aliceGroup), world.follow(carolGroup)
	defer aliceFeed.stop()
	defer carolFeed.stop()
	world.call(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "a boat!", references: [{agentMessageId: %q}]) { runId } }`, bobGroup, surprise))

	quotedTextOf := func(message *models.AgentMessage) string {
		t.Helper()
		if message == nil || len(message.References) != 1 || message.References[0].AgentMessageID != surprise {
			t.Fatalf("the reply is shown as %+v", message)
		}
		return message.References[0].QuotedText
	}
	if event := aliceFeed.next(agent.EventSaid); event == nil || quotedTextOf(event.Message) != "the surprise is a boat" {
		t.Errorf("Alice, who was there, is told %+v", event)
	}
	if event := carolFeed.next(agent.EventSaid); event == nil || quotedTextOf(event.Message) != "" {
		t.Errorf("Carol, who joined after, is told what was said before: %+v", event)
	}
	for name, test := range map[string]struct {
		reader *models.User
		group  string
		want   string
	}{"Alice": {world.alice, aliceGroup, "the surprise is a boat"}, "Carol": {world.carol, carolGroup, ""}} {
		messages := world.read(t, test.reader, test.group)
		reply := messages[len(messages)-1]
		references := reply["references"].([]any)
		if len(references) != 1 || references[0].(map[string]any)["agentMessageId"] != surprise || references[0].(map[string]any)["quotedText"] != test.want {
			t.Errorf("%s reads the reply as %v; want the quote %q", name, reply, test.want)
		}
	}
	world.refused(t, world.carol, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "what surprise?", references: [{agentMessageId: %q}]) { runId } }`, carolGroup, surprise))
}

// A file sent in a chat is kept for a person whose agent is off, but the text
// read out of it is meant for a model and only a person whose agent is on has
// one to spend it on.
func TestAFileSentInAChatIsReadForTheModelOnlyWhenTheUploadersAgentIsOn(t *testing.T) {
	world := newChatWorld(t)
	world.router.Path(api.PathAgentAttachments).Methods(http.MethodPost).HandlerFunc(world.graph.agentAttachmentsView)
	upload := func(as *models.User) []*models.AgentAttachment {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "menu.txt")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("soup and bread"))
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, api.PathAgentAttachments, &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set(api.AuthenticatedUsernameHeader, as.Username)
		response := httptest.NewRecorder()
		world.router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s uploading got %d: %s", as.Username, response.Code, response.Body.String())
		}
		var result AgentAttachmentUploadResult
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Attachments) != 1 {
			t.Fatalf("the upload answered %s", response.Body.String())
		}
		var stored []*models.AgentAttachment
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if stored, err = tx.GetAgentAttachments([]string{result.Attachments[0].ID}); err != nil || len(stored) != 1 {
				t.Fatalf("GetAgentAttachments: %v %v", stored, err)
			}
		})
		return stored
	}
	// Bob may chat, and his agent is off.
	if stored := upload(world.bob); stored[0].Text != "" {
		t.Errorf("text was read for a person whose agent is off: %q", stored[0].Text)
	}
	// Frank's is on.
	if stored := upload(world.frank); stored[0].Text != "soup and bread" {
		t.Errorf("text for a person whose agent is on: %q", stored[0].Text)
	}
}

// conversationWhere is the id of the person's chat conversation the test
// picks out of their list.
func (self *chatWorld) conversationWhere(t *testing.T, user *models.User, picks func(chat map[string]any) bool) string {
	t.Helper()
	for _, each := range self.call(t, user, `query { ListAgentConversations(kind: "chat") `+chatTestFields+` }`)["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if picks(conversation["chat"].(map[string]any)) {
			return conversation["id"].(string)
		}
	}
	t.Fatalf("%s has no such chat", user.Username)
	return ""
}

// Whoever a change to the people of a chat concerns is told, those it takes
// out included, so that an open chat reads its members again instead of
// staying as it was until a reload.
func TestAChangeOfPeopleIsToldToEveryoneItConcerns(t *testing.T) {
	world := newChatWorld(t)
	circleId := world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	world.call(t, world.alice, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q]) { id } }`, world.bob.ID))
	isTrip := func(chat map[string]any) bool { return chat["title"] == "Trip" }
	isCircle := func(chat map[string]any) bool { return chat["isCircleChat"] == true }
	aliceTrip := world.conversationWhere(t, world.alice, isTrip)
	bobTrip := world.conversationWhere(t, world.bob, isTrip)
	bobCircle := world.conversationWhere(t, world.bob, isCircle)
	aliceCircle := world.conversationWhere(t, world.alice, isCircle)
	carolCircle := world.conversationWhere(t, world.carol, isCircle)
	bobTripFeed, aliceCircleFeed, carolCircleFeed, bobCircleFeed := world.follow(bobTrip), world.follow(aliceCircle), world.follow(carolCircle), world.follow(bobCircle)
	defer bobTripFeed.stop()
	defer aliceCircleFeed.stop()
	defer carolCircleFeed.stop()
	defer bobCircleFeed.stop()
	// Events are published as the change commits, before the call returns, so
	// emptying a feed before a step leaves only what the step told.
	drain := func(feeds ...*chatEvents) {
		for _, feed := range feeds {
			for len(feed.channel) > 0 {
				<-feed.channel
			}
		}
	}

	// Adding Carol to the group tells Bob, who has it open.
	drain(bobTripFeed)
	world.call(t, world.alice, fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, aliceTrip, world.carol.ID))
	if bobTripFeed.next(agent.EventMembers) == nil {
		t.Error("Bob was not told that Carol joined the group")
	}
	// Carol is in it now, with a conversation of her own to follow.
	carolTrip := world.conversationWhere(t, world.carol, isTrip)
	carolTripFeed := world.follow(carolTrip)
	defer carolTripFeed.stop()

	// Taking her out tells her and the others.
	drain(bobTripFeed, carolTripFeed)
	world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveChatMember(conversationId: %q, userId: %q) { id } }`, aliceTrip, world.carol.ID))
	if carolTripFeed.next(agent.EventMembers) == nil {
		t.Error("Carol was not told that she was taken out of the group")
	}
	if bobTripFeed.next(agent.EventMembers) == nil {
		t.Error("Bob was not told that Carol was taken out")
	}
	// Carol is put back in, and leaves: the one who leaves hears it.
	drain(carolTripFeed, bobTripFeed)
	world.call(t, world.alice, fmt.Sprintf(`mutation { AddChatMembers(conversationId: %q, userIds: [%q]) { id } }`, aliceTrip, world.carol.ID))
	if carolTripFeed.next(agent.EventMembers) == nil {
		t.Error("Carol was not told that she was added again")
	}
	drain(carolTripFeed)
	world.call(t, world.carol, fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, carolTrip))
	if carolTripFeed.next(agent.EventMembers) == nil {
		t.Error("Carol was not told that she left")
	}

	// Taking Bob out of the circle takes him out of its chat and out of the
	// group, where he shares no circle with Alice any more; he, and the others,
	// are told of both.
	drain(bobCircleFeed, aliceCircleFeed, carolCircleFeed, bobTripFeed)
	world.call(t, world.alice, fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, circleId, world.bob.ID))
	if bobCircleFeed.next(agent.EventMembers) == nil {
		t.Error("Bob was not told that he left the circle's chat")
	}
	if aliceCircleFeed.next(agent.EventMembers) == nil || carolCircleFeed.next(agent.EventMembers) == nil {
		t.Error("the rest of the circle were not told that Bob left its chat")
	}
	if bobTripFeed.next(agent.EventMembers) == nil {
		t.Error("Bob was not told that he left the group")
	}
	// Deleting the circle ends its chat for the two who are left in it.
	drain(carolCircleFeed)
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, circleId))
	if carolCircleFeed.next(agent.EventMembers) == nil {
		t.Error("Carol was not told that the circle's chat ended")
	}
}
