package db_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// say stores a message by a person in a chat and fails the test when it is
// refused.
func say(t *testing.T, tx db.Transaction, chatId string, author *models.User, body string) *models.ChatMessage {
	t.Helper()
	message, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chatId, AuthorUserID: author.ID, AuthorName: author.DisplayName(), Body: body})
	if err != nil {
		t.Fatalf("AppendChatMessage(%q): %s", body, err)
	}
	return message
}

// conversationOf is the person's own conversation for a chat.
func conversationOf(t *testing.T, tx db.Transaction, user *models.User, chatId string) *models.AgentConversation {
	t.Helper()
	agent, err := tx.GetAgentByUser(user.ID)
	if err != nil || agent == nil {
		t.Fatalf("%s has no agent: %v", user.Username, err)
	}
	conversations, err := tx.ListChatConversations(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, conversation := range conversations {
		if conversation.ChatID == chatId {
			return conversation
		}
	}
	t.Fatalf("%s has no conversation for chat %s", user.Username, chatId)
	return nil
}

// Everybody who chats has an agent row, switched off if they never turned theirs
// on, and a conversation of their own for the chat; opening a chat with the
// same person again is the same chat.
func TestADirectChatIsOneChatWithAConversationForEachPerson(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "direct-alice")
		bob := newCircleTestUser(t, tx, "direct-bob")
		first, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}
		again, err := tx.OpenDirectChat(bob.ID, alice.ID)
		if err != nil || again.ID != first.ID {
			t.Fatalf("opening it from the other side: %v %v", err, again)
		}
		if first.ChatKind != models.ChatKindDirect || len(first.ActiveMembers()) != 2 {
			t.Fatalf("a direct chat is %+v", first)
		}
		for _, user := range []*models.User{alice, bob} {
			agent, err := tx.GetAgentByUser(user.ID)
			if err != nil || agent == nil || agent.Enabled || agent.Active() {
				t.Errorf("%s has the agent %+v, %v; want one that is switched off", user.Username, agent, err)
			}
			if conversation := conversationOf(t, tx, user, first.ID); conversation.Kind != models.AgentConversationChat {
				t.Errorf("%s's conversation is a %s", user.Username, conversation.Kind)
			}
		}
		if _, err := tx.OpenDirectChat(alice.ID, alice.ID); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a chat with oneself: %v", err)
		}
		if _, err := tx.OpenDirectChat(alice.ID, "nobody"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a chat with nobody: %v", err)
		}
	})
}

// A member's page is the chat's messages from when they joined, and their own
// conversation's rows and nobody else's, by time; the unread count is what
// others said since their read mark, and never goes back.
func TestAChatIsReadWithOnlyTheReadersOwnRowsAndCountsWhatIsUnread(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "read-alice")
		bob := newCircleTestUser(t, tx, "read-bob")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		aliceConversation := conversationOf(t, tx, alice, chat.ID)
		bobConversation := conversationOf(t, tx, bob, chat.ID)

		first := say(t, tx, chat.ID, alice, "dinner at 7?")
		// Bob's agent did something in his conversation; Alice must not read it.
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: bobConversation.ID, Role: "tool", Content: "a private tool result"}); err != nil {
			t.Fatal(err)
		}
		second := say(t, tx, chat.ID, bob, "yes")
		third := say(t, tx, chat.ID, alice, "great")

		aliceReads, total, err := tx.ListChatConversationMessages(aliceConversation, 100, 0)
		if err != nil || total != 3 || len(aliceReads) != 3 {
			t.Fatalf("Alice reads %d of %d: %v", len(aliceReads), total, err)
		}
		for _, message := range aliceReads {
			if strings.Contains(message.Content, "private") {
				t.Errorf("Alice read Bob's agent's row: %+v", message)
			}
		}
		if aliceReads[0].ID != first.ID || aliceReads[2].ID != third.ID || aliceReads[0].Role != "user" || aliceReads[0].AuthorKind != models.ChatAuthorPerson || aliceReads[1].AuthorName != "read-bob" {
			t.Errorf("Alice reads %+v", aliceReads)
		}
		bobReads, total, err := tx.ListChatConversationMessages(bobConversation, 100, 0)
		if err != nil || total != 4 || len(bobReads) != 4 {
			t.Fatalf("Bob reads %d of %d: %v", len(bobReads), total, err)
		}
		if bobReads[1].Role != "tool" || bobReads[1].Content != "a private tool result" {
			t.Errorf("Bob's own row is not among his messages by time: %+v", bobReads[1])
		}
		// A page counts back from the newest.
		page, total, err := tx.ListChatConversationMessages(bobConversation, 2, 1)
		if err != nil || total != 4 || len(page) != 2 || page[0].ID != bobReads[1].ID || page[1].ID != second.ID {
			t.Fatalf("a page of two, one back: %v %d %+v", err, total, page)
		}

		views := []*models.AgentConversation{bobConversation}
		if err := tx.ChatViewsFor(bob.ID, views); err != nil {
			t.Fatal(err)
		}
		// Bob wrote the second, which marks what came before it as read: only
		// the third is unread to him.
		if views[0].Chat == nil || views[0].Chat.UnreadCount != 1 || views[0].Chat.Title != "read-alice" {
			t.Fatalf("Bob's chat is %+v", views[0].Chat)
		}
		isMoved, err := tx.MarkChatRead(chat.ID, bob.ID, first.ID)
		if err != nil || isMoved {
			t.Fatalf("a mark behind what he wrote moved: %v %v", isMoved, err)
		}
		isMoved, err = tx.MarkChatRead(chat.ID, bob.ID, third.ID)
		if err != nil || !isMoved {
			t.Fatalf("MarkChatRead: %v %v", isMoved, err)
		}
		// Never backwards.
		isMoved, err = tx.MarkChatRead(chat.ID, bob.ID, second.ID)
		if err != nil || isMoved {
			t.Errorf("the mark moved back: %v %v", isMoved, err)
		}
		if err := tx.ChatViewsFor(bob.ID, views); err != nil || views[0].Chat.UnreadCount != 0 {
			t.Errorf("after reading, %d are unread: %v", views[0].Chat.UnreadCount, err)
		}
		reloaded, err := tx.GetChat(chat.ID)
		if err != nil || reloaded.Member(bob.ID).ReadThroughMessageID != third.ID {
			t.Errorf("Bob's read mark is %q, %v", reloaded.Member(bob.ID).ReadThroughMessageID, err)
		}
		if _, err := tx.MarkChatRead(chat.ID, bob.ID, "not-a-message"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a mark on a message that is not there: %v", err)
		}
		// What a person wrote they have read, so their own message is never
		// unread and nothing before it is; Bob's next one is.
		say(t, tx, chat.ID, bob, "see you there")
		if err := tx.ChatViewsFor(alice.ID, []*models.AgentConversation{aliceConversation}); err != nil || aliceConversation.Chat.UnreadCount != 1 {
			t.Errorf("Alice has %d unread, want Bob's one: %v", aliceConversation.Chat.UnreadCount, err)
		}
	})
}

// A member added later reads from then on; one who left reads up to then and
// can say nothing more; coming back starts their reading afresh.
func TestAGroupChatShowsAMemberOnlyWhatWasSaidWhileTheyWereIn(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "group-alice")
		bob := newCircleTestUser(t, tx, "group-bob")
		carol := newCircleTestUser(t, tx, "group-carol")
		chat, err := tx.CreateGroupChat(alice.ID, " Weekend ", []string{bob.ID})
		if err != nil || chat.Title != "Weekend" || chat.Member(alice.ID).MemberRole != models.MemberRoleAdmin || chat.Member(bob.ID).MemberRole != models.MemberRoleMember {
			t.Fatalf("CreateGroupChat: %v %+v", err, chat)
		}
		say(t, tx, chat.ID, alice, "before carol")
		time.Sleep(5 * time.Millisecond)
		if chat, err = tx.AddChatMembers(chat.ID, []string{carol.ID}); err != nil || len(chat.ActiveMembers()) != 3 {
			t.Fatalf("AddChatMembers: %v %+v", err, chat)
		}
		time.Sleep(5 * time.Millisecond)
		say(t, tx, chat.ID, alice, "with carol")
		carolConversation := conversationOf(t, tx, carol, chat.ID)
		messages, total, err := tx.ListChatConversationMessages(carolConversation, 100, 0)
		if err != nil || total != 1 || messages[0].Content != "with carol" {
			t.Fatalf("Carol reads %+v of %d: %v", messages, total, err)
		}

		// Carol leaves; what is said later is not hers, and she cannot say more.
		time.Sleep(5 * time.Millisecond)
		if chat, err = tx.RemoveChatMember(chat.ID, carol.ID); err != nil || len(chat.ActiveMembers()) != 2 || !chat.Member(carol.ID).IsRemoved {
			t.Fatalf("RemoveChatMember: %v %+v", err, chat)
		}
		time.Sleep(5 * time.Millisecond)
		say(t, tx, chat.ID, bob, "after carol")
		if _, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: carol.ID, AuthorName: "Carol", Body: "hello?"}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a member who left wrote: %v", err)
		}
		messages, total, err = tx.ListChatConversationMessages(carolConversation, 100, 0)
		if err != nil || total != 1 || len(messages) != 1 {
			t.Errorf("after leaving, Carol reads %d of %d: %v", len(messages), total, err)
		}

		// Back again, she reads from now.
		time.Sleep(5 * time.Millisecond)
		if chat, err = tx.AddChatMembers(chat.ID, []string{carol.ID}); err != nil || chat.Member(carol.ID).IsRemoved {
			t.Fatalf("AddChatMembers again: %v %+v", err, chat)
		}
		time.Sleep(5 * time.Millisecond)
		say(t, tx, chat.ID, bob, "welcome back")
		messages, total, err = tx.ListChatConversationMessages(carolConversation, 100, 0)
		if err != nil || total != 1 || messages[0].Content != "welcome back" {
			t.Errorf("after coming back, Carol reads %+v of %d: %v", messages, total, err)
		}

		// The last admin leaving makes the member who joined first one.
		if chat, err = tx.RemoveChatMember(chat.ID, alice.ID); err != nil || chat.Member(bob.ID).MemberRole != models.MemberRoleAdmin {
			t.Fatalf("the admin left: %v %+v", err, chat)
		}
		// The last member leaving takes the chat with them.
		if _, err = tx.RemoveChatMember(chat.ID, bob.ID); err != nil {
			t.Fatal(err)
		}
		if chat, err = tx.RemoveChatMember(chat.ID, carol.ID); err != nil || chat != nil {
			t.Fatalf("the last member left: %v %+v", err, chat)
		}
		if found, err := tx.GetChat(carolConversation.ChatID); err != nil || found != nil {
			t.Errorf("the chat is still there: %v %+v", err, found)
		}
		if found, err := tx.GetAgentConversation(carolConversation.ID); err != nil || found != nil {
			t.Errorf("the member's conversation is still there: %v %+v", err, found)
		}
	})
}

// The people of a direct chat, or of a circle's chat, are not chosen: only a
// group's are.
func TestOnlyAGroupChatsPeopleAreChosen(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "fixed-alice")
		bob := newCircleTestUser(t, tx, "fixed-bob")
		carol := newCircleTestUser(t, tx, "fixed-carol")
		direct, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddChatMembers(direct.ID, []string{carol.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a third person in a direct chat: %v", err)
		}
		if _, err := tx.RemoveChatMember(direct.ID, bob.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person taken out of a direct chat: %v", err)
		}
		if _, err := tx.RenameChat(direct.ID, "Us"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a direct chat renamed: %v", err)
		}
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, alice.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.SyncChatsOf(alice.ID); err != nil {
			t.Fatal(err)
		}
		var circleChat *models.AgentConversation
		agent, _ := tx.GetAgentByUser(alice.ID)
		conversations, _ := tx.ListChatConversations(agent.ID)
		for _, conversation := range conversations {
			if chat, _ := tx.GetChat(conversation.ChatID); chat != nil && chat.CircleID == circle.ID {
				circleChat = conversation
			}
		}
		if circleChat == nil {
			t.Fatal("a circle has no chat")
		}
		if _, err := tx.AddChatMembers(circleChat.ChatID, []string{carol.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a person put in a circle's chat by hand: %v", err)
		}
		if _, err := tx.RenameChat(circleChat.ChatID, "Other"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a circle's chat renamed by hand: %v", err)
		}
	})
}

// A circle's chat is made with the circle and follows its members, their
// roles and its name, in the same transaction that changes the circle.
func TestACircleChatFollowsTheCircle(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "follow-maker")
		friend := newCircleTestUser(t, tx, "follow-friend")
		third := newCircleTestUser(t, tx, "follow-third")
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		chatOf := func() *models.Chat {
			t.Helper()
			agent, err := tx.GetAgentByUser(maker.ID)
			if err != nil || agent == nil {
				t.Fatalf("the maker has no agent: %v", err)
			}
			conversations, err := tx.ListChatConversations(agent.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, conversation := range conversations {
				if chat, _ := tx.GetChat(conversation.ChatID); chat != nil && chat.CircleID == circle.ID {
					return chat
				}
			}
			t.Fatal("the circle has no chat")
			return nil
		}
		chat := chatOf()
		if chat.Title != "Home" || chat.ChatKind != models.ChatKindGroup || len(chat.ActiveMembers()) != 1 {
			t.Fatalf("a new circle's chat is %+v", chat)
		}
		if _, err := tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(circle.ID, third.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}
		chat = chatOf()
		if len(chat.ActiveMembers()) != 3 || chat.Member(friend.ID).ConversationID == "" {
			t.Fatalf("after two joined: %+v", chat)
		}
		if _, err := tx.SetCircleMemberRole(circle.ID, friend.ID, models.MemberRoleAdmin); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.RenameCircle(circle.ID, "Family"); err != nil {
			t.Fatal(err)
		}
		chat = chatOf()
		if chat.Title != "Family" || chat.Member(friend.ID).MemberRole != models.MemberRoleAdmin {
			t.Errorf("after a rename and a promotion: %+v", chat.Member(friend.ID))
		}

		// A group chat among the three, and a direct chat between the maker and the friend.
		group, err := tx.CreateGroupChat(maker.ID, "Trip", []string{friend.ID, third.ID})
		if err != nil {
			t.Fatal(err)
		}
		direct, err := tx.OpenDirectChat(maker.ID, friend.ID)
		if err != nil {
			t.Fatal(err)
		}
		say(t, tx, chat.ID, friend, "hello all")

		// The friend leaves the circle: out of the circle chat and out of the
		// group chat, where they now share a circle with nobody, in this
		// transaction; their direct chat stays.
		if _, err := tx.RemoveCircleMember(circle.ID, friend.ID); err != nil {
			t.Fatal(err)
		}
		chat = chatOf()
		if !chat.Member(friend.ID).IsRemoved || len(chat.ActiveMembers()) != 2 {
			t.Errorf("the friend is still in the circle chat: %+v", chat.Member(friend.ID))
		}
		if reloaded, _ := tx.GetChat(group.ID); !reloaded.Member(friend.ID).IsRemoved || reloaded.Member(maker.ID).IsRemoved {
			t.Errorf("the friend is still in the group: %+v", reloaded.Members)
		}
		if reloaded, _ := tx.GetChat(direct.ID); reloaded.Member(friend.ID).IsRemoved || reloaded.Member(maker.ID).IsRemoved {
			t.Errorf("the direct chat lost a person: %+v", reloaded.Members)
		}
		// What the friend said while in the circle chat is still theirs to read.
		friendConversation := conversationOf(t, tx, friend, chat.ID)
		if messages, _, err := tx.ListChatConversationMessages(friendConversation, 100, 0); err != nil || len(messages) != 1 {
			t.Errorf("the friend reads %d after leaving: %v", len(messages), err)
		}

		// The circle going takes its chat.
		if err := tx.DeleteCircle(circle.ID); err != nil {
			t.Fatal(err)
		}
		if found, err := tx.GetChat(chat.ID); err != nil || found != nil {
			t.Errorf("the circle chat outlived the circle: %v %+v", err, found)
		}
	})
}

// A circle made before chats existed gets its chat, with the members it has,
// the first time a member's chats are looked at.
func TestACircleThatHasNoChatGetsOne(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "old-maker")
		friend := newCircleTestUser(t, tx, "old-friend")
		circle, err := tx.CreateCircle(&models.Circle{Name: "Old", CircleKind: models.CircleKindFriends}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}
		// As the migration found it: no chat, and no conversations.
		dbtest.Exec(t, database, `DELETE FROM "chat"`)
		if err := tx.SyncChatsOf(friend.ID); err != nil {
			t.Fatalf("SyncChatsOf: %s", err)
		}
		agent, _ := tx.GetAgentByUser(friend.ID)
		conversations, err := tx.ListChatConversations(agent.ID)
		if err != nil || len(conversations) != 1 {
			t.Fatalf("the friend has %d chat conversations: %v", len(conversations), err)
		}
		chat, err := tx.GetChat(conversations[0].ChatID)
		if err != nil || chat.CircleID != circle.ID || len(chat.ActiveMembers()) != 2 {
			t.Errorf("the circle's chat is %+v, %v", chat, err)
		}
		// Asking again changes nothing.
		if err := tx.SyncChatsOf(friend.ID); err != nil {
			t.Fatal(err)
		}
		if conversations, _ = tx.ListChatConversations(agent.ID); len(conversations) != 1 {
			t.Errorf("asking twice made %d conversations", len(conversations))
		}
	})
}

// A deleted account's words stay, under the name it had, and the chat is
// still readable by the others.
func TestADeletedAccountKeepsItsNameInTheChat(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "gone-alice")
		bob, err := tx.CreateUser(&models.User{Username: "gone-bob", Name: "Bob Example"})
		if err != nil {
			t.Fatal(err)
		}
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		said := say(t, tx, chat.ID, bob, "see you soon")
		if _, err := tx.SetChatMessageReaction(said.ID, models.ReactorPerson, bob.ID, "Bob Example", "👍"); err != nil {
			t.Fatal(err)
		}
		other := say(t, tx, chat.ID, alice, "ok")
		if _, err := tx.SetChatMessageReaction(other.ID, models.ReactorPerson, bob.ID, "Bob Example", "🙏"); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteUser(bob.ID); err != nil {
			t.Fatalf("DeleteUser: %s", err)
		}
		reloaded, err := tx.GetChat(chat.ID)
		if err != nil || reloaded == nil {
			t.Fatalf("the chat is gone: %v", err)
		}
		var found *models.ChatMember
		for _, member := range reloaded.Members {
			if member.Name == "Bob Example" {
				found = member
			}
		}
		if found == nil || !found.IsDeletedAccount || !found.IsRemoved || found.UserID != "" {
			t.Fatalf("Bob's place is %+v", found)
		}
		conversation := conversationOf(t, tx, alice, chat.ID)
		messages, _, err := tx.ListChatConversationMessages(conversation, 100, 0)
		if err != nil || len(messages) != 2 {
			t.Fatalf("Alice reads %d: %v", len(messages), err)
		}
		if messages[0].AuthorName != "Bob Example" || messages[0].AuthorUserID != "" || messages[0].AuthorKind != models.ChatAuthorPerson {
			t.Errorf("Bob's message is %+v", messages[0])
		}
		if len(messages[1].Reactions) != 1 || messages[1].Reactions[0].ReactorName != "Bob Example" || messages[1].Reactions[0].ReactorUserID == "" {
			t.Errorf("Bob's reaction on Alice's message is %+v", messages[1].Reactions)
		}
	})
}

// What an agent said stays the agent's once the agent is deleted: the foreign
// key clears author_agent_id, and without author_kind the message would read
// as its owner's own words.
func TestADeletedAgentsMessageStaysAnAgents(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "agent-alice")
		bob := newCircleTestUser(t, tx, "agent-bob")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		// Opening the chat gave Alice a switched-off agent to hang it from.
		helper, err := tx.GetAgentByUser(alice.ID)
		if err != nil || helper == nil {
			t.Fatalf("Alice has no agent: %v", err)
		}
		if _, err := tx.AppendChatMessage(&models.ChatMessage{
			ChatID: chat.ID, AuthorUserID: alice.ID, AuthorName: "Helper", AuthorAgentID: helper.ID, Body: "the dentist is at nine",
		}); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteAgent(helper.ID); err != nil {
			t.Fatalf("DeleteAgent: %s", err)
		}
		messages, _, err := tx.ListChatConversationMessages(conversationOf(t, tx, bob, chat.ID), 100, 0)
		if err != nil || len(messages) != 1 {
			t.Fatalf("Bob reads %d: %v", len(messages), err)
		}
		if messages[0].Role != "assistant" || messages[0].AuthorKind != models.ChatAuthorAgent || messages[0].AuthorName != "Helper" {
			t.Errorf("the deleted agent's message reads as %+v", messages[0])
		}
	})
}

// Two people's agents that both keep the name "Agent" are told apart by the
// owner's name, which the chat message keeps as it knew them, so it still
// reads once the owner's account is gone.
func TestAnAgentsMessageKeepsWhoseAgentItIs(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice, err := tx.CreateUser(&models.User{Username: "whose-alice", Name: "Alice Example"})
		if err != nil {
			t.Fatal(err)
		}
		bob, err := tx.CreateUser(&models.User{Username: "whose-bob"})
		if err != nil {
			t.Fatal(err)
		}
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, owner := range []*models.User{alice, bob} {
			ownerAgent, err := tx.GetAgentByUser(owner.ID)
			if err != nil || ownerAgent == nil {
				t.Fatalf("%s has no agent: %v", owner.Username, err)
			}
			// The owner's name is not given by the caller: the chat knows it.
			if _, err := tx.AppendChatMessage(&models.ChatMessage{
				ChatID: chat.ID, AuthorUserID: owner.ID, AuthorName: models.AgentDefaultName, AuthorAgentID: ownerAgent.ID, Body: "on it",
			}); err != nil {
				t.Fatal(err)
			}
		}
		say(t, tx, chat.ID, alice, "thanks")
		check := func(reader *models.User, wantOwnerNames ...string) {
			t.Helper()
			messages, _, err := tx.ListChatConversationMessages(conversationOf(t, tx, reader, chat.ID), 100, 0)
			if err != nil || len(messages) != 3 {
				t.Fatalf("%s reads %d: %v", reader.Username, len(messages), err)
			}
			for index, want := range wantOwnerNames {
				if messages[index].AuthorOwnerName != want {
					t.Errorf("message %d is from the agent of %q, want %q", index, messages[index].AuthorOwnerName, want)
				}
			}
		}
		// A person's own message names no owner.
		check(bob, "Alice Example", "whose-bob", "")
		if err := tx.DeleteUser(alice.ID); err != nil {
			t.Fatalf("DeleteUser: %s", err)
		}
		check(bob, "Alice Example", "whose-bob", "")
	})
}

// One reaction for each reactor on a message, replaced by reacting again and
// taken away by an empty one; a message taken back has none and takes no more.
func TestAChatMessageHasOneReactionForEachPerson(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "react-alice")
		bob := newCircleTestUser(t, tx, "react-bob")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		message := say(t, tx, chat.ID, alice, "lunch?")
		for _, step := range []struct {
			by      *models.User
			emoji   string
			changed bool
		}{
			{bob, "👍", true}, {bob, "👍", false}, {alice, "🙏", true}, {bob, "❤️", true}, {alice, "", true}, {alice, "", false},
		} {
			changed, err := tx.SetChatMessageReaction(message.ID, models.ReactorPerson, step.by.ID, step.by.Username, step.emoji)
			if err != nil || changed != step.changed {
				t.Fatalf("%s reacts %q: changed %v, %v; want %v", step.by.Username, step.emoji, changed, err, step.changed)
			}
		}
		read, err := tx.GetChatMessage(message.ID)
		if err != nil || len(read.Reactions) != 1 || read.Reactions[0].Emoji != "❤️" || read.Reactions[0].ReactorUserID != bob.ID || read.Reactions[0].ReactorName != "react-bob" {
			t.Fatalf("the message has the reactions %+v, %v", read.Reactions, err)
		}
		if _, err := tx.DeleteChatMessage(message.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetChatMessageReaction(message.ID, models.ReactorPerson, alice.ID, "x", "👍"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a reaction on a message taken back: %v", err)
		}
		read, err = tx.GetChatMessage(message.ID)
		if err != nil || read.DeletedAt == nil || read.Body != "" || len(read.Reactions) != 0 {
			t.Errorf("a message taken back is %+v, %v", read, err)
		}
		if _, err := tx.EditChatMessage(message.ID, "back"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a message taken back was edited: %v", err)
		}
	})
}

// A reply is shown with what it replies to, and only to a message of its own chat.
func TestAReplyQuotesAMessageOfItsOwnChat(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "reply-alice")
		bob := newCircleTestUser(t, tx, "reply-bob")
		carol := newCircleTestUser(t, tx, "reply-carol")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		other, err := tx.OpenDirectChat(alice.ID, carol.ID)
		if err != nil {
			t.Fatal(err)
		}
		question := say(t, tx, chat.ID, alice, "which day?")
		foreign := say(t, tx, other.ID, alice, "a secret")
		if _, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: bob.ID, AuthorName: "Bob", Body: "no", ReplyToMessageID: foreign.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a reply to a message of another chat: %v", err)
		}
		if _, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: bob.ID, AuthorName: "Bob", Body: "Friday", ReplyToMessageID: question.ID}); err != nil {
			t.Fatal(err)
		}
		messages, _, err := tx.ListChatConversationMessages(conversationOf(t, tx, alice, chat.ID), 100, 0)
		if err != nil || len(messages) != 2 || len(messages[1].References) != 1 || messages[1].References[0].AgentMessageID != question.ID || messages[1].References[0].QuotedText != "which day?" {
			t.Fatalf("the reply is shown as %+v, %v", messages, err)
		}
	})
}

// A mute is until a time, and clearing it is nil.
func TestAChatCanBeMutedForAWhile(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "mute-alice")
		bob := newCircleTestUser(t, tx, "mute-bob")
		chat, err := tx.OpenDirectChat(alice.ID, bob.ID)
		if err != nil {
			t.Fatal(err)
		}
		conversation := conversationOf(t, tx, bob, chat.ID)
		until := time.Now().Add(time.Hour)
		if err := tx.SetChatMuted(chat.ID, bob.ID, &until); err != nil {
			t.Fatal(err)
		}
		if err := tx.ChatViewsFor(bob.ID, []*models.AgentConversation{conversation}); err != nil || !conversation.Chat.IsMuted || conversation.Chat.MutedUntil == nil {
			t.Fatalf("a muted chat is %+v, %v", conversation.Chat, err)
		}
		if err := tx.SetChatMuted(chat.ID, bob.ID, nil); err != nil {
			t.Fatal(err)
		}
		if err := tx.ChatViewsFor(bob.ID, []*models.AgentConversation{conversation}); err != nil || conversation.Chat.IsMuted || conversation.Chat.MutedUntil != nil {
			t.Errorf("an unmuted chat is %+v, %v", conversation.Chat, err)
		}
		if err := tx.SetChatMuted(chat.ID, "nobody", &until); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a mute by somebody who is not in the chat: %v", err)
		}
	})
}

// A turn somebody else asked an owner's agent for answers to the whole room,
// so it reads only what every current member could read: nothing from before
// the latest member joined. The owner's own turn keeps the owner's window.
// Somebody who left no longer limits it.
func TestAMemberTurnReadsOnlyWhatEveryCurrentMemberCouldRead(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		alice := newCircleTestUser(t, tx, "window-alice")
		bob := newCircleTestUser(t, tx, "window-bob")
		carol := newCircleTestUser(t, tx, "window-carol")
		chat, err := tx.CreateGroupChat(alice.ID, "Weekend", []string{bob.ID})
		if err != nil {
			t.Fatal(err)
		}
		aliceConversation := conversationOf(t, tx, alice, chat.ID)
		time.Sleep(5 * time.Millisecond)
		say(t, tx, chat.ID, alice, "before carol, about the surprise")
		time.Sleep(5 * time.Millisecond)
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: aliceConversation.ID, Role: "tool", Content: "a private tool result"}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
		if chat, err = tx.AddChatMembers(chat.ID, []string{carol.ID}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
		say(t, tx, chat.ID, bob, "with carol")
		carolConversation := conversationOf(t, tx, carol, chat.ID)

		bodies := func(messages []*models.AgentMessage) string {
			var found []string
			for _, message := range messages {
				found = append(found, message.Content)
			}
			return strings.Join(found, "|")
		}
		ownerRows, err := tx.ListChatTurnRows(aliceConversation, 100, false)
		if err != nil || !strings.Contains(bodies(ownerRows), "before carol, about the surprise") || !strings.Contains(bodies(ownerRows), "private tool result") {
			t.Errorf("the owner's turn keeps the owner's window: %v %q", err, bodies(ownerRows))
		}
		memberRows, err := tx.ListChatTurnRows(aliceConversation, 100, true)
		if err != nil || bodies(memberRows) != "with carol" {
			t.Errorf("a turn somebody else asked reads from the latest join: %v %q", err, bodies(memberRows))
		}
		toolRows, total, err := tx.ListChatMemberTurnMessages(aliceConversation, 100, 0)
		if err != nil || total != 1 || bodies(toolRows) != "with carol" {
			t.Errorf("the conversation tool in that turn reads the same: %v %d %q", err, total, bodies(toolRows))
		}
		ownerRead, total, err := tx.ListChatConversationMessages(aliceConversation, 100, 0)
		if err != nil || total != 3 || len(ownerRead) != 3 {
			t.Errorf("the owner reads all of it: %v %d %q", err, total, bodies(ownerRead))
		}
		if rows, _ := tx.ListChatTurnRows(carolConversation, 100, true); bodies(rows) != "with carol" {
			t.Errorf("the latest member's own window: %q", bodies(rows))
		}

		// Carol leaves; the room is Alice and Bob again.
		time.Sleep(5 * time.Millisecond)
		if _, err := tx.RemoveChatMember(chat.ID, carol.ID); err != nil {
			t.Fatal(err)
		}
		memberRows, err = tx.ListChatTurnRows(aliceConversation, 100, true)
		if err != nil || !strings.Contains(bodies(memberRows), "before carol, about the surprise") {
			t.Errorf("a member who left no longer limits the window: %v %q", err, bodies(memberRows))
		}
	})
}

// Deleting a circle, or its last member leaving, does for each member what
// leaving does: out of the group chats in which they now share a circle with
// none of the others, and their agents stop reading the circle's chat.
func TestADeletedCircleTakesItsMembersOutOfTheChatsItHeld(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "gone-maker")
		friend := newCircleTestUser(t, tx, "gone-friend")
		circle, err := tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(circle.ID, friend.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}
		group, err := tx.CreateGroupChat(maker.ID, "Trip", []string{friend.ID})
		if err != nil {
			t.Fatal(err)
		}
		// The friend's agent remembers the circle's chat.
		friendAgent, err := tx.GetAgentByUser(friend.ID)
		if err != nil || friendAgent == nil {
			t.Fatalf("the friend has no agent: %v", err)
		}
		circleChatId := ""
		conversations, err := tx.ListChatConversations(friendAgent.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, conversation := range conversations {
			if chat, _ := tx.GetChat(conversation.ChatID); chat != nil && chat.CircleID == circle.ID {
				circleChatId = chat.ID
			}
		}
		if circleChatId == "" {
			t.Fatal("the circle has no chat")
		}
		source, err := tx.SetChatAgentReading(circleChatId, friend.ID, true)
		if err != nil || source == nil || !source.Enabled {
			t.Fatalf("SetChatAgentReading: %v %+v", err, source)
		}

		if err := tx.DeleteCircle(circle.ID); err != nil {
			t.Fatal(err)
		}
		if found, err := tx.GetChat(group.ID); err != nil || found != nil {
			t.Errorf("the group chat of two who now share no circle is still there: %v %+v", err, found)
		}
		switched, err := tx.GetAgentSource(friendAgent.ID, source.ID)
		if err != nil || switched == nil || switched.Enabled {
			t.Errorf("the friend's agent still reads the circle's chat: %v %+v", err, switched)
		}

		// A circle whose last member leaves: the member leaves the chats the
		// circle's departure strands them in.
		other := newCircleTestUser(t, tx, "gone-other")
		second, err := tx.CreateCircle(&models.Circle{Name: "Club", CircleKind: models.CircleKindFriends}, maker.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddCircleMember(second.ID, other.ID, models.MemberRoleMember, maker.ID); err != nil {
			t.Fatal(err)
		}
		pair, err := tx.CreateGroupChat(maker.ID, "Pair", []string{other.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.RemoveCircleMember(second.ID, other.ID); err != nil {
			t.Fatal(err)
		}
		if reloaded, _ := tx.GetChat(pair.ID); reloaded == nil || len(reloaded.ActiveMembers()) != 1 {
			t.Fatalf("the other left: %+v", reloaded)
		}
		if _, err := tx.RemoveCircleMember(second.ID, maker.ID); err != nil {
			t.Fatal(err)
		}
		if found, err := tx.GetChat(pair.ID); err != nil || found != nil {
			t.Errorf("the last member is left in a group chat with nobody: %v %+v", err, found)
		}
	})
}

// A group chat holds only so many people, and one person starts only so many
// in a day.
func TestAGroupChatAndWhoStartsOneAreBounded(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		maker := newCircleTestUser(t, tx, "full-maker")
		var people []string
		for count := 0; count < models.ChatMembersMost; count++ {
			people = append(people, newCircleTestUser(t, tx, fmt.Sprintf("full-person-%02d", count)).ID)
		}
		// The maker and forty-nine of them is fifty; all fifty and the maker is not.
		if _, err := tx.CreateGroupChat(maker.ID, "Too many", people); !errors.Is(err, db.ErrLimitReached) || !strings.Contains(err.Error(), "at most 50 people") {
			t.Fatalf("fifty-one people: %v", err)
		}
		full, err := tx.CreateGroupChat(maker.ID, "Everyone", people[:models.ChatMembersMost-1])
		if err != nil || len(full.ActiveMembers()) != models.ChatMembersMost {
			t.Fatalf("fifty people: %v", err)
		}
		if _, err := tx.AddChatMembers(full.ID, people[models.ChatMembersMost-1:]); !errors.Is(err, db.ErrLimitReached) {
			t.Errorf("a fifty-first person: %v", err)
		}
		// Somebody who is in already is not counted again, and a place frees up.
		if _, err := tx.AddChatMembers(full.ID, people[:2]); err != nil {
			t.Errorf("people already in: %v", err)
		}
		if _, err := tx.RemoveChatMember(full.ID, people[0]); err != nil {
			t.Fatal(err)
		}
		if chat, err := tx.AddChatMembers(full.ID, people[models.ChatMembersMost-1:]); err != nil || len(chat.ActiveMembers()) != models.ChatMembersMost {
			t.Errorf("after one left: %v", err)
		}

		// Twenty a day, the one above counted.
		for count := 1; count < models.GroupChatsMadePerDayMost; count++ {
			if _, err := tx.CreateGroupChat(maker.ID, fmt.Sprintf("Trip %d", count), people[:1]); err != nil {
				t.Fatalf("group chat %d: %s", count+1, err)
			}
		}
		if _, err := tx.CreateGroupChat(maker.ID, "One more", people[:1]); !errors.Is(err, db.ErrLimitReached) || !strings.Contains(err.Error(), "20 group chats") {
			t.Errorf("the twenty-first group chat: %v", err)
		}
		// Somebody else starts their own, and a direct chat is no group.
		if _, err := tx.CreateGroupChat(people[1], "Theirs", people[2:3]); err != nil {
			t.Errorf("another person's first group chat: %v", err)
		}
		if _, err := tx.OpenDirectChat(maker.ID, people[5]); err != nil {
			t.Errorf("a direct chat: %v", err)
		}
	})
}
