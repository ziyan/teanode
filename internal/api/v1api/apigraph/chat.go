package apigraph

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ratelimit"
)

// Chats: conversations with people in them. A chat is shown and used as a
// conversation, through the operations a conversation with the agent has
// (ListAgentConversations, ReadAgentConversation, AgentConversationEvents,
// AskAgent and ReactToAgentMessage in agent_ask.go), and these are the
// operations only a chat needs. What the people and their agents say is stored
// once, in the chat, and each member has their own conversation of the kind
// chat that points at it; every operation here names that conversation, or a
// message, and the caller's place in the chat is found from it.
//
// Every operation needs circle:use and none needs the agent to be on. A chat
// the caller is not in, or is not theirs, is not found. A person who left
// reads the chat up to the time they left and writes nothing more.
// docs/decisions/20261016-a-chat-is-a-conversation-with-people-in-it.md says
// why, and docs/subsystems/conversations.md how a chat is stored and read.

// ChatMutation changes chats, and what is said in them.
type ChatMutation interface {
	// The caller's direct chat with somebody they share a circle with, made
	// when there is none and the same one every time, as the caller's
	// conversation. Needs circle:use.
	OpenDirectChat(ctx context.Context, arguments OpenDirectChatArguments) (*models.AgentConversation, error)

	// Make a group chat with the caller as its first admin, and people who
	// each share a circle with the caller as members. Needs circle:use.
	CreateGroupChat(ctx context.Context, arguments CreateGroupChatArguments) (*models.AgentConversation, error)

	// Change a group chat's title. Needs the caller to be an admin of a group
	// chat that is not a circle's.
	RenameChat(ctx context.Context, arguments RenameChatArguments) (*models.AgentConversation, error)

	// Put people in a group chat, each sharing a circle with the caller. They
	// read it from now on. Needs the caller to be an admin of a group chat
	// that is not a circle's.
	AddChatMembers(ctx context.Context, arguments AddChatMembersArguments) (*models.AgentConversation, error)

	// Take a person out of a group chat. Needs the caller to be an admin of a
	// group chat that is not a circle's.
	RemoveChatMember(ctx context.Context, arguments RemoveChatMemberArguments) (*models.AgentConversation, error)

	// Leave a group chat that is not a circle's. The chat stays readable up to
	// the time of leaving.
	LeaveChat(ctx context.Context, arguments LeaveChatArguments) (bool, error)

	// Change what a message of the caller's says. The others are told it was
	// edited.
	EditChatMessage(ctx context.Context, arguments EditChatMessageArguments) (*models.AgentMessage, error)

	// Take a message back: the caller's own, or any in a group chat the caller
	// is an admin of. The others are told, and see that it was taken back.
	DeleteChatMessage(ctx context.Context, arguments DeleteChatMessageArguments) (bool, error)

	// Say the caller has read a chat through a message. The mark moves
	// forward and never back; it answers whether it moved. The others are
	// told.
	MarkChatRead(ctx context.Context, arguments MarkChatReadArguments) (bool, error)

	// Silence a chat's phone notifications until a time, or, with none, stop
	// silencing them.
	MuteChat(ctx context.Context, arguments MuteChatArguments) (*models.AgentConversation, error)

	// Tell the others the caller is writing. Nothing is stored.
	SetChatTyping(ctx context.Context, arguments SetChatTypingArguments) (bool, error)

	// Say something in a chat as the caller's agent, under its name, for the
	// agent's own tool. Needs the agent to be on.
	SayInChatAsAgent(ctx context.Context, arguments SayInChatAsAgentArguments) (*models.AgentMessage, error)

	// Make the caller's agent remember a chat from now on, as a knowledge
	// source, or stop. Needs circle:use.
	SetChatAgentReading(ctx context.Context, arguments SetChatAgentReadingArguments) (*models.AgentConversation, error)

	// Let the others in a chat address the caller's agent there, or stop
	// them. Needs circle:use.
	SetChatAgentAskableByOthers(ctx context.Context, arguments SetChatAgentAskableByOthersArguments) (*models.AgentConversation, error)
}

// OpenDirectChatArguments name the person.
type OpenDirectChatArguments struct {
	UserID string `json:"userId"`
}

// CreateGroupChatArguments name the group and its people.
type CreateGroupChatArguments struct {
	// What to call it, at most 100 characters; may be empty
	Title   string   `json:"title" graphapi:"nullable"`
	UserIDs []string `json:"userIds"`
}

// RenameChatArguments name the chat by the caller's conversation.
type RenameChatArguments struct {
	ConversationID string `json:"conversationId"`
	Title          string `json:"title"`
}

// AddChatMembersArguments name the people to put in.
type AddChatMembersArguments struct {
	ConversationID string   `json:"conversationId"`
	UserIDs        []string `json:"userIds"`
}

// RemoveChatMemberArguments name the person to take out.
type RemoveChatMemberArguments struct {
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId"`
}

// LeaveChatArguments name the chat by the caller's conversation.
type LeaveChatArguments struct {
	ConversationID string `json:"conversationId"`
}

// EditChatMessageArguments name the message and what it should say.
type EditChatMessageArguments struct {
	MessageID string `json:"messageId"`
	Message   string `json:"message"`
}

// DeleteChatMessageArguments name the message.
type DeleteChatMessageArguments struct {
	MessageID string `json:"messageId"`
}

// MarkChatReadArguments name the newest message the caller has read.
type MarkChatReadArguments struct {
	ConversationID   string `json:"conversationId"`
	ThroughMessageID string `json:"throughMessageId"`
}

// MuteChatArguments say until when; none un-silences the chat.
type MuteChatArguments struct {
	ConversationID string     `json:"conversationId"`
	MutedUntil     *time.Time `json:"mutedUntil" graphapi:"nullable"`
}

// SetChatTypingArguments name the chat by the caller's conversation.
type SetChatTypingArguments struct {
	ConversationID string `json:"conversationId"`
}

// SayInChatAsAgentArguments name the chat and the words.
type SayInChatAsAgentArguments struct {
	ConversationID string `json:"conversationId"`
	Message        string `json:"message"`
}

// chatRateLimits are the limits on what one person does in chats on this
// instance, kept in memory: a runaway script is stopped, nobody typing is.
type chatRateLimits struct {
	messages *ratelimit.Registry
	typing   *ratelimit.Registry
}

func newChatRateLimits() *chatRateLimits {
	return &chatRateLimits{
		// A message a second, in bursts of up to a minute's worth.
		messages: ratelimit.NewRegistry(float64(models.ChatMessagesPerMinute)/60, models.ChatMessagesPerMinute, 10000, 5*time.Minute),
		// A notice that someone is typing at most every two seconds.
		typing: ratelimit.NewRegistry(0.5, 1, 10000, time.Minute),
	}
}

// describeChatMembers fills in what draws a member: the address of the
// picture and the color of the initial. Whose agent is whose is told only for
// the viewer's own and for agents their owners let the others ask: an @
// addresses those, and the rest of the room need not learn that a member has
// an agent at all.
func describeChatMembers(members []*models.ChatMember, viewerUserId string) {
	for _, member := range members {
		if member.UserID != viewerUserId && !member.IsAgentAskableByOthers {
			member.AgentID, member.AgentName, member.IsAgentActive = "", "", false
		}
		if member.User != nil {
			member.AvatarURL = avatarURL(member.User)
		}
		key := member.UserID
		if key == "" {
			key = member.Name
		}
		member.AvatarColorIndex = avatar.InitialColorIndex(key)
	}
}

// describeChatConversations sets the chat each of these conversations is
// for, as the person sees it. Conversations that are not a chat's are left
// as they are.
func describeChatConversations(tx db.Transaction, userId string, conversations []*models.AgentConversation) error {
	if err := tx.ChatViewsFor(userId, conversations); err != nil {
		return err
	}
	for _, conversation := range conversations {
		if conversation.Chat != nil {
			describeChatMembers(conversation.Chat.Members, userId)
		}
	}
	return nil
}

// chatConversationOf is the caller's conversation for the chat as the
// operations return it.
func (self *graph) chatConversationOf(ctx context.Context, userId string, conversation *models.AgentConversation) (*models.AgentConversation, error) {
	tx := self.transaction(ctx)
	refreshed, err := tx.GetAgentConversation(conversation.ID)
	if err != nil || refreshed == nil {
		return nil, err
	}
	if err := describeChatConversations(tx, userId, []*models.AgentConversation{refreshed}); err != nil {
		return nil, err
	}
	return refreshed, nil
}

// chatContact is a person the caller wants in a chat with them: an account
// that can use circles and that the caller shares a circle with. Anyone else
// is not found, as a person the caller has no way to know is.
func (self *graph) chatContact(tx db.Transaction, caller *models.User, userId string) (*models.User, error) {
	userId = strings.TrimSpace(userId)
	if userId == "" || userId == caller.ID {
		return nil, api.ErrNotFound
	}
	other, err := tx.GetUser(userId)
	if err != nil {
		return nil, err
	}
	if other == nil || other.Disabled() {
		return nil, api.ErrNotFound
	}
	permissions, err := tx.EffectivePermissions(other.ID)
	if err != nil {
		return nil, err
	}
	if !permissions.Has(models.PermissionCircleUse) {
		return nil, api.ErrNotFound
	}
	if shares, err := tx.SharesCircle(caller.ID, other.ID); err != nil {
		return nil, err
	} else if !shares {
		return nil, api.ErrNotFound
	}
	return other, nil
}

func (self *graph) OpenDirectChat(ctx context.Context, arguments OpenDirectChatArguments) (*models.AgentConversation, error) {
	user, found, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	otherId := strings.TrimSpace(arguments.UserID)
	if otherId == user.ID {
		return nil, fmt.Errorf("%w: a chat is with somebody else", api.ErrInvalidArguments)
	}
	// A chat that exists can be opened by either of its people even once they
	// share no circle: it is theirs to read.
	existing, err := tx.GetDirectChat(user.ID, otherId)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if _, err := self.chatContact(tx, user, otherId); err != nil {
			return nil, err
		}
	}
	chat, err := tx.OpenDirectChat(user.ID, otherId)
	if err != nil {
		return nil, translateError(err)
	}
	conversation, err := tx.EnsureChatConversation(found.ID, chat.ID)
	if err != nil {
		return nil, translateError(err)
	}
	return self.chatConversationOf(ctx, user.ID, conversation)
}

func (self *graph) CreateGroupChat(ctx context.Context, arguments CreateGroupChatArguments) (*models.AgentConversation, error) {
	user, found, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	userIds := []string{}
	for _, userId := range arguments.UserIDs {
		other, err := self.chatContact(tx, user, userId)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(userIds, other.ID) {
			userIds = append(userIds, other.ID)
		}
	}
	if len(userIds) == 0 {
		return nil, fmt.Errorf("%w: a group chat needs somebody besides you", api.ErrInvalidArguments)
	}
	chat, err := tx.CreateGroupChat(user.ID, arguments.Title, userIds)
	if err != nil {
		return nil, translateError(err)
	}
	conversation, err := tx.EnsureChatConversation(found.ID, chat.ID)
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s made a group chat with %d others", user.Username, len(userIds))
	return self.chatConversationOf(ctx, user.ID, conversation)
}

// requireGroupAdmin is a session in which the caller may change who is in a
// group chat and what it is called: an admin of a group chat that is not a
// circle's, still in it.
func requireGroupAdmin(session *chatSession) error {
	switch {
	case session.Chat.IsCircleChat():
		return fmt.Errorf("%w: a circle's chat follows the circle; change the circle instead", api.ErrInvalidArguments)
	case session.Chat.ChatKind != models.ChatKindGroup:
		return fmt.Errorf("%w: a direct chat is between two people and has nothing to change", api.ErrInvalidArguments)
	case session.Member.IsRemoved:
		return fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	case session.Member.MemberRole != models.MemberRoleAdmin:
		return fmt.Errorf("%w: only an admin of this chat can do that", api.ErrPermissionDenied)
	}
	return nil
}

func (self *graph) RenameChat(ctx context.Context, arguments RenameChatArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGroupAdmin(session); err != nil {
		return nil, err
	}
	if _, err := self.transaction(ctx).RenameChat(session.Chat.ID, arguments.Title); err != nil {
		return nil, translateError(err)
	}
	self.publishChatEvents(ctx, session.Chat.ID, func(member *models.ChatMember) agent.Event {
		return agent.Event{Kind: agent.EventTitled, Text: strings.TrimSpace(arguments.Title)}
	})
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

func (self *graph) AddChatMembers(ctx context.Context, arguments AddChatMembersArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGroupAdmin(session); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	userIds := []string{}
	for _, userId := range arguments.UserIDs {
		other, err := self.chatContact(tx, session.User, userId)
		if err != nil {
			return nil, err
		}
		userIds = append(userIds, other.ID)
	}
	if len(userIds) == 0 {
		return nil, fmt.Errorf("%w: say whom to add", api.ErrInvalidArguments)
	}
	if _, err := tx.AddChatMembers(session.Chat.ID, userIds); err != nil {
		return nil, translateError(err)
	}
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

func (self *graph) RemoveChatMember(ctx context.Context, arguments RemoveChatMemberArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGroupAdmin(session); err != nil {
		return nil, err
	}
	userId := strings.TrimSpace(arguments.UserID)
	if member := session.Chat.Member(userId); member == nil || member.IsRemoved {
		return nil, api.ErrNotFound
	}
	remaining, err := self.transaction(ctx).RemoveChatMember(session.Chat.ID, userId)
	if err != nil {
		return nil, translateError(err)
	}
	if remaining == nil {
		return nil, api.ErrNotFound
	}
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

func (self *graph) LeaveChat(ctx context.Context, arguments LeaveChatArguments) (bool, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return false, err
	}
	switch {
	case session.Chat.IsCircleChat():
		return false, fmt.Errorf("%w: a circle's chat follows the circle; leave the circle instead", api.ErrInvalidArguments)
	case session.Chat.ChatKind != models.ChatKindGroup:
		return false, fmt.Errorf("%w: you cannot leave a direct chat", api.ErrInvalidArguments)
	case session.Member.IsRemoved:
		return false, fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	if _, err := self.transaction(ctx).RemoveChatMember(session.Chat.ID, session.User.ID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

// requireChatWritable says why the caller cannot say anything in the chat
// now, or nil: they left it, or it is a direct chat and the two of them no
// longer share a circle.
func (self *graph) requireChatWritable(tx db.Transaction, session *chatSession) error {
	if session.Member.IsRemoved {
		return fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	if session.Chat.ChatKind != models.ChatKindDirect {
		return nil
	}
	for _, member := range session.Chat.Members {
		if member.UserID == session.User.ID {
			continue
		}
		if member.UserID == "" || member.IsRemoved {
			return fmt.Errorf("%w: the other person's account is gone", api.ErrInvalidArguments)
		}
		shares, err := tx.SharesCircle(session.User.ID, member.UserID)
		if err != nil {
			return err
		}
		if !shares {
			return fmt.Errorf("%w: you no longer share a circle", api.ErrInvalidArguments)
		}
	}
	return nil
}

// chatMessageFor is a chat message as the caller reads it: the author drawn,
// and whether it is the caller's. conversationId is the caller's.
func (self *graph) chatMessageFor(tx db.Transaction, message *models.ChatMessage, quoted map[string]*models.ChatMessage, conversationId, callerUserId string) (*models.AgentMessage, error) {
	converted := db.ChatMessageAsAgentMessage(message, conversationId, quoted)
	users, err := tx.GetUsersByID([]string{message.AuthorUserID})
	if err != nil {
		return nil, err
	}
	describeChatAuthors([]*models.AgentMessage{converted}, users, callerUserId)
	return converted, nil
}

// describeChatAuthors draws the authors of messages that came from a chat:
// the picture of a person, the color of the initial of both, and whether the
// message is the reader's. An agent has no picture, only its owner's color.
func describeChatAuthors(messages []*models.AgentMessage, users map[string]*models.User, callerUserId string) {
	for _, message := range messages {
		if message.AuthorKind == "" {
			continue
		}
		key := message.AuthorUserID
		if key == "" {
			key = message.AuthorName
		}
		message.AuthorAvatarColorIndex = avatar.InitialColorIndex(key)
		if message.AuthorKind == models.ChatAuthorPerson {
			message.AuthorAvatarURL = avatarURL(users[message.AuthorUserID])
		}
		message.IsMine = message.AuthorUserID != "" && message.AuthorUserID == callerUserId
	}
}

// copyForMember is the message as another member's conversation shows it.
// withoutQuoteBeforeJoining is the member's copy of a reply without the words
// of the message it is to, when that was said before they joined: the reply
// still says which message it is to. The copy is shallow, so the references
// are copied before they are changed.
func withoutQuoteBeforeJoining(copied *models.AgentMessage, member *models.ChatMember, repliedAt time.Time) *models.AgentMessage {
	if repliedAt.IsZero() || !repliedAt.Before(member.JoinedAt) || len(copied.References) == 0 {
		return copied
	}
	references := slices.Clone(copied.References)
	for index := range references {
		references[index].QuotedText = ""
	}
	copied.References = references
	return copied
}

func copyForMember(message *models.AgentMessage, member *models.ChatMember) *models.AgentMessage {
	copied := *message
	copied.ConversationID = member.ConversationID
	copied.IsMine = message.AuthorUserID != "" && message.AuthorUserID == member.UserID
	return &copied
}

// publishChatEvents tells everybody in the chat now, once the change is
// committed, with an event of each one's own: the event is addressed to the
// member's conversation, and the function given builds its contents. A person
// who is not in the chat is not told.
func (self *graph) publishChatEvents(ctx context.Context, chatId string, build func(member *models.ChatMember) agent.Event) {
	hub := self.chatHub()
	if hub == nil {
		return
	}
	tx := self.transaction(ctx)
	chat, err := tx.GetChat(chatId)
	if err != nil || chat == nil {
		return
	}
	events := eventsForMembers(chat.Members, build)
	tx.AfterCommit(func() { hub.PublishChatEvents(events) })
}

// postChatMessage stores what the caller said in a chat, tells the members,
// and notifies their phones. As the agent, the message is the agent's under
// its name and the caller is its owner.
func (self *graph) postChatMessage(ctx context.Context, session *chatSession, body string, attachmentIds []string, replyToMessageId string, isByAgent bool) (*models.AgentMessage, error) {
	tx := self.transaction(ctx)
	if err := self.requireChatWritable(tx, session); err != nil {
		return nil, err
	}
	if !self.chatLimits().messages.Allow(session.User.ID) {
		return nil, api.ErrTooManyRequests
	}
	attachmentIds = slices.DeleteFunc(slices.Clone(attachmentIds), func(id string) bool { return strings.TrimSpace(id) == "" })
	if err := models.ValidateChatMessageBody(body, len(attachmentIds) > 0); err != nil {
		return nil, translateError(err)
	}
	// The files are the caller's own uploads, not sent with any message yet.
	attachments, err := tx.GetAgentAttachments(attachmentIds)
	if err != nil {
		return nil, err
	}
	if len(attachments) != len(attachmentIds) {
		return nil, fmt.Errorf("%w: an attachment is missing", api.ErrInvalidArguments)
	}
	for _, attachment := range attachments {
		if attachment.AgentID != session.Agent.ID || attachment.MessageID != "" {
			return nil, api.ErrNotFound
		}
	}
	// A reply is to a message the caller could read: what was said before they
	// joined is not theirs to quote.
	if replyToMessageId = strings.TrimSpace(replyToMessageId); replyToMessageId != "" {
		if replied, err := tx.GetChatMessage(replyToMessageId); err != nil {
			return nil, err
		} else if replied != nil && replied.ChatID == session.Chat.ID && !isReadableByMember(replied, session.Member) {
			return nil, api.ErrNotFound
		}
	}
	message := &models.ChatMessage{
		ChatID:           session.Chat.ID,
		AuthorUserID:     session.User.ID,
		AuthorName:       session.User.DisplayName(),
		Body:             strings.TrimRight(body, " \t\r\n"),
		ReplyToMessageID: strings.TrimSpace(replyToMessageId),
	}
	for _, attachment := range attachments {
		message.Attachments = append(message.Attachments, *attachment)
	}
	if isByAgent {
		message.AuthorAgentID = session.Agent.ID
		message.AuthorName = session.Agent.DisplayName()
	}
	stored, err := tx.AppendChatMessage(message)
	if err != nil {
		return nil, translateError(err)
	}
	if err := tx.ClaimAgentAttachments(attachmentIds, session.Conversation.ID, stored.ID); err != nil {
		return nil, err
	}
	// Re-read with the claim in place so what is shown carries the message.
	stored.Attachments = message.Attachments
	for index := range stored.Attachments {
		stored.Attachments[index].MessageID = stored.ID
		stored.Attachments[index].AgentID = ""
	}
	quoted := map[string]*models.ChatMessage{}
	var repliedAt time.Time
	if stored.ReplyToMessageID != "" {
		if replied, err := tx.GetChatMessage(stored.ReplyToMessageID); err == nil && replied != nil {
			quoted[replied.ID] = replied
			repliedAt = replied.CreatedAt
		}
	}
	shown, err := self.chatMessageFor(tx, stored, quoted, session.Conversation.ID, session.User.ID)
	if err != nil {
		return nil, err
	}
	self.publishSaid(ctx, session, shown, repliedAt)
	return shown, nil
}

// publishSaid tells the members what was said, and their phones, once it is
// committed. repliedAt is when the message it replies to was said, zero for
// none: a member who joined after that is not shown what it said.
func (self *graph) publishSaid(ctx context.Context, session *chatSession, shown *models.AgentMessage, repliedAt time.Time) {
	hub := self.chatHub()
	if hub == nil {
		return
	}
	tx := self.transaction(ctx)
	events := []agent.Event{}
	for _, member := range session.Chat.ActiveMembers() {
		if member.ConversationID == "" {
			continue
		}
		events = append(events, agent.Event{Kind: agent.EventSaid, ConversationID: member.ConversationID, MessageID: shown.ID, Message: withoutQuoteBeforeJoining(copyForMember(shown, member), member, repliedAt)})
		// What a person writes they have read; what their agent writes, they
		// have yet to.
		if shown.AuthorKind == models.ChatAuthorPerson {
			events = append(events, agent.Event{Kind: agent.EventRead, ConversationID: member.ConversationID, MessageID: shown.ID, UserID: session.User.ID})
		}
	}
	recipients := recipientsOf(session.Chat.Members)
	title, isGroup := db.ChatViewOf(session.Chat, session.User.ID).Title, session.Chat.ChatKind == models.ChatKindGroup
	notified := *shown
	tx.AfterCommit(func() {
		hub.PublishChatEvents(events)
		notifyChatMessage(hub, &notified, title, isGroup, recipients)
	})
}

// sayInChat is AskAgent in a chat: the words are stored as the caller's
// message and published. They start a turn only of an agent they address, as
// internal/chat decides; the run of the caller's own agent is answered, for
// the caller to follow.
func (self *graph) sayInChat(ctx context.Context, session *chatSession, arguments AskAgentArguments) (*AgentTurnView, error) {
	// A read-only client asks the agent with its changing tools left out; it
	// has nothing to leave out here, and what it writes goes to other people.
	if arguments.ReadOnly {
		return nil, fmt.Errorf("%w: a read-only client does not write to other people", api.ErrInvalidArguments)
	}
	replyTo := ""
	for _, reference := range arguments.References {
		if id := strings.TrimSpace(reference.AgentMessageID); id != "" {
			replyTo = id
			break
		}
	}
	addressed := addressedAgentsOf(session, arguments)
	worker := self.agentWorker()
	if len(addressed) > 0 && worker == nil {
		return nil, agent.ErrUnavailable
	}
	shown, err := self.postChatMessage(ctx, session, arguments.Message, arguments.AttachmentIDs, replyTo, false)
	if err != nil {
		return nil, err
	}
	view := &AgentTurnView{RunID: "", ConversationID: session.Conversation.ID}
	if len(addressed) == 0 {
		return view, nil
	}
	if view.RunID, err = self.startChatTurns(ctx, worker, session, shown, arguments, addressed); err != nil {
		return nil, err
	}
	return view, nil
}

func (self *graph) SayInChatAsAgent(ctx context.Context, arguments SayInChatAsAgentArguments) (*models.AgentMessage, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	// An agent speaks for its owner only while the owner may use it and it is on.
	if principal := api.ContextPrincipal(ctx); principal == nil || !principal.Permissions.Has(models.PermissionAgentUse) {
		return nil, api.ErrNotFound
	}
	if !session.Agent.Active() {
		return nil, agent.ErrUnavailable
	}
	return self.postChatMessage(ctx, session, arguments.Message, nil, "", true)
}

func (self *graph) EditChatMessage(ctx context.Context, arguments EditChatMessageArguments) (*models.AgentMessage, error) {
	session, message, err := self.requireChatMessage(ctx, arguments.MessageID)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := self.requireChatWritable(tx, session); err != nil {
		return nil, err
	}
	if message.AuthorUserID != session.User.ID || message.IsByAgent() {
		return nil, fmt.Errorf("%w: only the person who wrote a message can change it", api.ErrPermissionDenied)
	}
	if message.DeletedAt != nil {
		return nil, api.ErrNotFound
	}
	if err := models.ValidateChatMessageBody(arguments.Message, len(message.Attachments) > 0); err != nil {
		return nil, translateError(err)
	}
	edited, err := tx.EditChatMessage(message.ID, strings.TrimRight(arguments.Message, " \t\r\n"))
	if err != nil {
		return nil, translateError(err)
	}
	shown, err := self.chatMessageFor(tx, edited, nil, session.Conversation.ID, session.User.ID)
	if err != nil {
		return nil, err
	}
	self.publishChatEvents(ctx, session.Chat.ID, func(member *models.ChatMember) agent.Event {
		return agent.Event{Kind: agent.EventEdited, MessageID: shown.ID, Message: copyForMember(shown, member)}
	})
	return shown, nil
}

func (self *graph) DeleteChatMessage(ctx context.Context, arguments DeleteChatMessageArguments) (bool, error) {
	session, message, err := self.requireChatMessage(ctx, arguments.MessageID)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	if session.Member.IsRemoved {
		return false, fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	isAuthor := message.AuthorUserID == session.User.ID
	isAdmin := session.Chat.ChatKind == models.ChatKindGroup && session.Member.MemberRole == models.MemberRoleAdmin
	if !isAuthor && !isAdmin {
		return false, fmt.Errorf("%w: only the person who wrote a message, or an admin of a group chat, can take it back", api.ErrPermissionDenied)
	}
	if message.DeletedAt != nil {
		return false, api.ErrNotFound
	}
	// The files go with the message: they were handed over for it.
	for _, attachment := range message.Attachments {
		if err := tx.DeleteAgentAttachment(attachment.ID); err != nil {
			return false, err
		}
		if err := self.storage.DeleteFile(ctx, attachment.ID); err != nil {
			log.Warningf("cannot remove the bytes of attachment %q: %s", attachment.ID, err)
		}
	}
	if _, err := tx.DeleteChatMessage(message.ID); err != nil {
		return false, translateError(err)
	}
	self.publishChatEvents(ctx, session.Chat.ID, func(*models.ChatMember) agent.Event {
		return agent.Event{Kind: agent.EventUnsaid, MessageID: message.ID}
	})
	return true, nil
}

func (self *graph) MarkChatRead(ctx context.Context, arguments MarkChatReadArguments) (bool, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	through, err := tx.GetChatMessage(strings.TrimSpace(arguments.ThroughMessageID))
	if err != nil {
		return false, err
	}
	if through == nil || through.ChatID != session.Chat.ID || !isReadableByMember(through, session.Member) {
		return false, api.ErrNotFound
	}
	isMoved, err := tx.MarkChatRead(session.Chat.ID, session.User.ID, through.ID)
	if err != nil {
		return false, translateError(err)
	}
	if isMoved {
		self.publishChatEvents(ctx, session.Chat.ID, func(*models.ChatMember) agent.Event {
			return agent.Event{Kind: agent.EventRead, MessageID: through.ID, UserID: session.User.ID}
		})
	}
	return isMoved, nil
}

func (self *graph) MuteChat(ctx context.Context, arguments MuteChatArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	mutedUntil := arguments.MutedUntil
	if mutedUntil != nil && !mutedUntil.After(time.Now()) {
		mutedUntil = nil
	}
	if err := self.transaction(ctx).SetChatMuted(session.Chat.ID, session.User.ID, mutedUntil); err != nil {
		return nil, translateError(err)
	}
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

func (self *graph) SetChatTyping(ctx context.Context, arguments SetChatTypingArguments) (bool, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return false, err
	}
	if session.Member.IsRemoved || !self.chatLimits().typing.Allow(session.User.ID+"/"+session.Chat.ID) {
		return true, nil
	}
	hub := self.chatHub()
	if hub == nil {
		return true, nil
	}
	events := eventsForMembers(session.Chat.Members, func(member *models.ChatMember) agent.Event {
		if member.UserID == session.User.ID {
			return agent.Event{}
		}
		return agent.Event{Kind: agent.EventTyping, UserID: session.User.ID, Name: session.User.DisplayName()}
	})
	// Nothing was stored, so there is nothing to wait for a commit of.
	hub.PublishChatEvents(events)
	return true, nil
}

// chatLimits are the per-person limits, made the first time they are wanted.
func (self *graph) chatLimits() *chatRateLimits {
	self.chatLimitsOnce.Do(func() { self.chatLimitsValue = newChatRateLimits() })
	return self.chatLimitsValue
}
