package db

import (
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ChatOperation is the chats: who is in them, what was said, and every
// member's own conversation for them. docs/subsystems/conversations.md says
// how a chat is read.
type ChatOperation interface {
	// EnsureAgent is the person's agent row, made switched off when they
	// have none. Everybody who can chat needs one, because their
	// conversations and phones hang from it; a switched-off agent does
	// nothing else (agent.Active is false), and the settings page offers to
	// turn it on as it does for a person who never had one.
	EnsureAgent(userId string) (*models.Agent, error)

	// EnsureChatConversation is the agent's own conversation for a chat,
	// made when there is none.
	EnsureChatConversation(agentId, chatId string) (*models.AgentConversation, error)

	// SyncChatsOf makes what the person's circles imply true: the circle
	// chat of each circle they are in (circles older than chats have none
	// until now), and a conversation for each chat they are in. Cheap when
	// there is nothing to do.
	SyncChatsOf(userId string) error

	// OpenDirectChat is the direct chat between two people, made when there
	// is none and the same one every time. That they know each other is the
	// caller's to check. ErrNotFound when either account is not there.
	OpenDirectChat(userId, otherUserId string) (*models.Chat, error)

	// CreateGroupChat makes a group chat with the maker as its first admin and
	// the others as members. That each shares a circle with the maker is the
	// caller's to check.
	CreateGroupChat(creatorUserId, title string, memberUserIds []string) (*models.Chat, error)

	// GetChat is a chat with everybody who is or was in it, or nil.
	GetChat(chatId string) (*models.Chat, error)

	// GetCircleChat is the chat a circle has, with everybody who is or was in
	// it, or nil when it has none yet.
	GetCircleChat(circleId string) (*models.Chat, error)

	// ListChatsOf are the chats the person is in now, with everybody who is or
	// was in each. It is how the caller learns whom to tell before a change
	// takes the person out of some of them.
	ListChatsOf(userId string) ([]*models.Chat, error)

	// GetDirectChat is the direct chat between two people, or nil when
	// they have none.
	GetDirectChat(userId, otherUserId string) (*models.Chat, error)

	// GetChatOfConversation is the chat a member's conversation is for, or
	// nil when the conversation is not a chat's.
	GetChatOfConversation(conversationId string) (*models.Chat, error)

	// RenameChat changes a group's title; an empty one leaves it unnamed.
	RenameChat(chatId, title string) (*models.Chat, error)

	// AddChatMembers puts people in a group chat who are not in it, and back
	// in those who left; they read it from then on. ErrNotFound for a
	// direct chat or a circle chat, whose members are not chosen.
	AddChatMembers(chatId string, userIds []string) (*models.Chat, error)

	// RemoveChatMember takes a person out of a group chat, who then reads
	// what was said until now. The last admin leaving makes the member who
	// joined first an admin, and the last member leaving deletes the chat,
	// answering nil. ErrNotFound when they are not in it, or it is a direct
	// chat or a circle chat.
	RemoveChatMember(chatId, userId string) (*models.Chat, error)

	// ChatViewsFor describes these conversations' chats as the person sees
	// them, setting each conversation's Chat; conversations that are not a
	// chat's are left as they are.
	ChatViewsFor(userId string, conversations []*models.AgentConversation) error

	// ListChatConversations are the conversations of the agent that are
	// chats', newest first.
	ListChatConversations(agentId string) ([]*models.AgentConversation, error)

	// AppendChatMessage stores what a member said and tells the chat there
	// is something new: its last message, the members' conversations and the
	// author's own read mark. ErrNotFound when the author is not in the
	// chat.
	AppendChatMessage(message *models.ChatMessage) (*models.ChatMessage, error)

	// GetChatMessage is one message with its reactions, or nil.
	GetChatMessage(messageId string) (*models.ChatMessage, error)

	// EditChatMessage changes what a message says and marks it edited.
	// ErrNotFound when it is deleted.
	EditChatMessage(messageId, body string) (*models.ChatMessage, error)

	// DeleteChatMessage deletes a message: its text and files go and the
	// message stays, drawn as deleted.
	DeleteChatMessage(messageId string) (*models.ChatMessage, error)

	// SharesChat says whether two different people are both current members
	// of one chat, whatever circles they are in: a chat shows each member to
	// the others by name, so their picture goes with it.
	SharesChat(userId, otherUserId string) (bool, error)

	// ListChatConversationMessages is the newest page of what a member
	// reads in their conversation for a chat -- the chat's messages from the
	// time they joined until the time they left, if they did, and their own
	// agent's rows in the conversation, by time -- with how many there are
	// in all. Offset counts back from the newest. A page is oldest first.
	ListChatConversationMessages(conversation *models.AgentConversation, limit, offset int) ([]*models.AgentMessage, int, error)

	// ListChatMemberTurnMessages is the same page as a turn somebody else
	// asked the member's agent for may read it: the chat's messages alone,
	// none of the agent's own rows in the conversation, and only those that
	// every current member of the chat could read, the ones created at or
	// after the latest time any current member joined. Whoever asked was
	// shown the chat from their own joining, which may be later than the
	// owner's, and what the owner's agent says in answer goes to all of them.
	ListChatMemberTurnMessages(conversation *models.AgentConversation, limit, offset int) ([]*models.AgentMessage, int, error)

	// SetChatMessageReaction puts a reactor's emoji on a message, replacing
	// the one they had; an empty emoji takes it away. It says whether
	// anything changed.
	SetChatMessageReaction(messageId string, reactorKind models.AgentReactorKind, reactorId, reactorName, reactionEmoji string) (bool, error)

	// MarkChatRead moves a member's read mark forward to a message of the
	// chat, and never back. It says whether the mark moved. ErrNotFound when
	// the message is not the chat's.
	MarkChatRead(chatId, userId, throughMessageId string) (bool, error)

	// SetChatMuted silences a chat's notifications for a member until the
	// time given; nil un-silences it.
	SetChatMuted(chatId, userId string, mutedUntil *time.Time) error

	// GetUsersByID reads accounts, without their groups, by id. One that is
	// gone is left out.
	GetUsersByID(userIds []string) (map[string]*models.User, error)
}

type chatModel struct {
	ID              string     `gorm:"column:id;primaryKey"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	ModifiedAt      time.Time  `gorm:"column:modified_at"`
	ChatKind        string     `gorm:"column:chat_kind"`
	Title           *string    `gorm:"column:title"`
	CircleID        *string    `gorm:"column:circle_id"`
	CreatedByUserID *string    `gorm:"column:created_by_user_id"`
	LastMessageAt   *time.Time `gorm:"column:last_message_at"`
	DirectKey       *string    `gorm:"column:direct_key"`
}

func (chatModel) TableName() string { return "chat" }

type chatMemberModel struct {
	ChatID                 string     `gorm:"column:chat_id"`
	UserID                 *string    `gorm:"column:user_id"`
	MemberName             string     `gorm:"column:member_name"`
	MemberRole             string     `gorm:"column:member_role"`
	JoinedAt               time.Time  `gorm:"column:joined_at"`
	RemovedAt              *time.Time `gorm:"column:removed_at"`
	ReadThroughMessageID   *string    `gorm:"column:read_through_message_id"`
	MutedUntil             *time.Time `gorm:"column:muted_until"`
	IsAgentReading         bool       `gorm:"column:is_agent_reading"`
	IsAgentAskableByOthers bool       `gorm:"column:is_agent_askable_by_others"`
}

func (chatMemberModel) TableName() string { return "chat_member" }

type chatMessageModel struct {
	ID               string     `gorm:"column:id;primaryKey"`
	ChatID           string     `gorm:"column:chat_id"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	AuthorUserID     *string    `gorm:"column:author_user_id"`
	AuthorMemberName string     `gorm:"column:author_member_name"`
	AuthorOwnerName  string     `gorm:"column:author_owner_name"`
	AuthorKind       string     `gorm:"column:author_kind"`
	AuthorAgentID    *string    `gorm:"column:author_agent_id"`
	AgentRunID       *string    `gorm:"column:agent_run_id"`
	Body             string     `gorm:"column:body"`
	Attachments      []byte     `gorm:"column:attachments;type:jsonb"`
	ReplyToMessageID *string    `gorm:"column:reply_to_message_id"`
	EditedAt         *time.Time `gorm:"column:edited_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

func (chatMessageModel) TableName() string { return "chat_message" }

type chatReactionModel struct {
	MessageID   string    `gorm:"column:message_id;primaryKey"`
	ReactorKey  string    `gorm:"column:reactor_key;primaryKey"`
	ReactorName string    `gorm:"column:reactor_name"`
	Emoji       string    `gorm:"column:emoji"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (chatReactionModel) TableName() string { return "chat_reaction" }

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func valueOfString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (self *transaction) GetUsersByID(userIds []string) (map[string]*models.User, error) {
	return loadUsersById(self.tx, userIds)
}

func (self *transaction) EnsureAgent(userId string) (*models.Agent, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	// The user's row is taken first, so that two requests making the same
	// person's agent do not both find none and one fail on the unique key.
	if err := lockRow(self.tx, &userModel{}, userId); err != nil {
		return nil, err
	}
	existing, err := self.GetAgentByUser(userId)
	if err != nil || existing != nil {
		return existing, err
	}
	return self.CreateAgent(&models.Agent{UserID: userId, Name: models.AgentDefaultName, Enabled: false})
}

func (self *transaction) EnsureChatConversation(agentId, chatId string) (*models.AgentConversation, error) {
	if agentId == "" || chatId == "" {
		return nil, ErrInvalidArguments
	}
	now := time.Now()
	model := &agentConversationModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, AgentID: agentId,
		Kind: string(models.AgentConversationChat), LastAt: now, ChatID: &chatId,
	}
	// One per agent per chat: the unique key settles two at once.
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model).Error; err != nil {
		return nil, err
	}
	var found agentConversationModel
	result := self.tx.Where(`"agent_id" = ? AND "chat_id" = ?`, agentId, chatId).Limit(1).Find(&found)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return conversationFromModel(&found), nil
}

// addChatMemberRow puts a person in a chat as a member, or back in it. A
// member returning reads from now, not from when they left or first came.
func (self *transaction) addChatMemberRow(chatId, userId string, memberRole models.MemberRole, now time.Time) (bool, error) {
	user, err := self.GetUser(userId)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, ErrNotFound
	}
	var rows []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).Limit(1).Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) > 0 {
		if rows[0].RemovedAt == nil {
			return false, nil
		}
		return true, self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).Updates(map[string]any{
			"member_name": user.DisplayName(), "member_role": string(memberRole), "joined_at": now, "removed_at": nil,
			"read_through_message_id": nil, "muted_until": nil, "is_agent_reading": false, "is_agent_askable_by_others": false,
		}).Error
	}
	if err := self.tx.Create(&chatMemberModel{
		ChatID: chatId, UserID: &userId, MemberName: user.DisplayName(), MemberRole: string(memberRole), JoinedAt: now,
	}).Error; err != nil {
		return false, err
	}
	return true, nil
}

// joinChat adds a person to a chat in full: their row, the agent their
// conversation hangs from, and the conversation.
func (self *transaction) joinChat(chatId, userId string, memberRole models.MemberRole, now time.Time) error {
	if _, err := self.addChatMemberRow(chatId, userId, memberRole, now); err != nil {
		return err
	}
	agent, err := self.EnsureAgent(userId)
	if err != nil {
		return err
	}
	_, err = self.EnsureChatConversation(agent.ID, chatId)
	return err
}

// joinChatInOrder puts each of the people in a chat, in the order of their
// identifiers and whatever order they were named in. Joining takes the row of
// each person (EnsureAgent), and two transactions that name the same people in
// opposite orders, adding them to two chats at once, each held the row the
// other wanted next: PostgreSQL ended one of them with a deadlock.
func (self *transaction) joinChatInOrder(chatId string, userIds []string, memberRoleOf func(userId string) models.MemberRole, now time.Time) error {
	ordered := slices.Clone(uniqueStrings(userIds))
	slices.Sort(ordered)
	for _, userId := range ordered {
		if err := self.joinChat(chatId, userId, memberRoleOf(userId), now); err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) loadChats(chatIds []string) ([]*models.Chat, error) {
	chatIds = uniqueStrings(chatIds)
	if len(chatIds) == 0 {
		return []*models.Chat{}, nil
	}
	var rows []chatModel
	if err := self.tx.Where(`"id" IN ?`, chatIds).Find(&rows).Error; err != nil {
		return nil, err
	}
	var memberRows []chatMemberModel
	if err := self.tx.Where(`"chat_id" IN ?`, chatIds).
		Order(`("removed_at" IS NULL) DESC, ("member_role" = 'admin') DESC, "joined_at" ASC, "user_id" ASC`).Find(&memberRows).Error; err != nil {
		return nil, err
	}
	userIds := make([]string, 0, len(memberRows))
	for _, row := range memberRows {
		if row.UserID != nil {
			userIds = append(userIds, *row.UserID)
		}
	}
	users, err := loadUsersById(self.tx, userIds)
	if err != nil {
		return nil, err
	}
	// Each member's own conversation, so a message can be told to it.
	type conversationRow struct {
		ChatID         string `gorm:"column:chat_id"`
		UserID         string `gorm:"column:user_id"`
		AgentID        string `gorm:"column:agent_id"`
		AgentName      string `gorm:"column:agent_name"`
		IsAgentActive  bool   `gorm:"column:is_agent_active"`
		ConversationID string `gorm:"column:conversation_id"`
		IsArchived     bool   `gorm:"column:is_archived"`
	}
	var conversationRows []conversationRow
	if err := self.tx.Raw(`SELECT "agent_conversation"."chat_id" AS chat_id, "agent"."user_id" AS user_id, "agent"."id" AS agent_id,
			"agent"."name" AS agent_name, ("agent"."enabled" AND "agent"."operator_disabled_at" IS NULL) AS is_agent_active,
			"agent_conversation"."id" AS conversation_id, ("agent_conversation"."archived_at" IS NOT NULL) AS is_archived
		FROM "agent_conversation" JOIN "agent" ON "agent"."id" = "agent_conversation"."agent_id"
		WHERE "agent_conversation"."chat_id" IN ?`, chatIds).Scan(&conversationRows).Error; err != nil {
		return nil, err
	}
	conversations := map[string]conversationRow{}
	for _, row := range conversationRows {
		conversations[row.ChatID+"/"+row.UserID] = row
	}
	chats := make(map[string]*models.Chat, len(rows))
	ordered := make([]*models.Chat, 0, len(rows))
	for index := range rows {
		chat := chatFromModel(&rows[index])
		chats[chat.ID] = chat
		ordered = append(ordered, chat)
	}
	for index := range memberRows {
		chat := chats[memberRows[index].ChatID]
		if chat == nil {
			continue
		}
		member := chatMemberFromModel(&memberRows[index], users)
		if found, ok := conversations[chat.ID+"/"+member.UserID]; ok {
			member.ConversationID, member.AgentID = found.ConversationID, found.AgentID
			member.IsConversationArchived = found.IsArchived
			member.AgentName, member.IsAgentActive = found.AgentName, found.IsAgentActive
			if strings.TrimSpace(member.AgentName) == "" {
				member.AgentName = models.AgentDefaultName
			}
		}
		chat.Members = append(chat.Members, member)
	}
	return ordered, nil
}

func chatFromModel(model *chatModel) *models.Chat {
	chat := &models.Chat{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt.In(time.Local),
		ModifiedAt:      model.ModifiedAt.In(time.Local),
		ChatKind:        models.ChatKind(model.ChatKind),
		Title:           valueOfString(model.Title),
		CircleID:        valueOfString(model.CircleID),
		CreatedByUserID: valueOfString(model.CreatedByUserID),
		LastMessageAt:   localTime(model.LastMessageAt),
		Members:         []*models.ChatMember{},
	}
	return chat
}

func chatMemberFromModel(model *chatMemberModel, users map[string]*models.User) *models.ChatMember {
	member := &models.ChatMember{
		UserID:                 valueOfString(model.UserID),
		Name:                   model.MemberName,
		MemberRole:             models.MemberRole(model.MemberRole),
		IsDeletedAccount:       model.UserID == nil,
		IsRemoved:              model.RemovedAt != nil,
		ReadThroughMessageID:   valueOfString(model.ReadThroughMessageID),
		IsAgentReading:         model.IsAgentReading,
		JoinedAt:               model.JoinedAt.In(time.Local),
		RemovedAt:              localTime(model.RemovedAt),
		MutedUntil:             localTime(model.MutedUntil),
		IsAgentAskableByOthers: model.IsAgentAskableByOthers,
	}
	if user := users[member.UserID]; user != nil {
		member.User = user
		member.Username = user.Username
		member.Name = user.DisplayName()
	}
	return member
}

func (self *transaction) GetChat(chatId string) (*models.Chat, error) {
	if chatId == "" {
		return nil, nil
	}
	chats, err := self.loadChats([]string{chatId})
	if err != nil || len(chats) == 0 {
		return nil, err
	}
	return chats[0], nil
}

func (self *transaction) GetCircleChat(circleId string) (*models.Chat, error) {
	if circleId == "" {
		return nil, nil
	}
	var chatIds []string
	if err := self.tx.Model(&chatModel{}).Where(`"circle_id" = ?`, circleId).Pluck("id", &chatIds).Error; err != nil {
		return nil, err
	}
	if len(chatIds) == 0 {
		return nil, nil
	}
	return self.GetChat(chatIds[0])
}

func (self *transaction) ListChatsOf(userId string) ([]*models.Chat, error) {
	if userId == "" {
		return []*models.Chat{}, nil
	}
	var chatIds []string
	if err := self.tx.Model(&chatMemberModel{}).Where(`"user_id" = ? AND "removed_at" IS NULL`, userId).
		Order(`"chat_id" ASC`).Pluck("chat_id", &chatIds).Error; err != nil {
		return nil, err
	}
	return self.loadChats(chatIds)
}

func (self *transaction) GetDirectChat(userId, otherUserId string) (*models.Chat, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return nil, nil
	}
	var chatIds []string
	if err := self.tx.Model(&chatModel{}).Where(`"direct_key" = ?`, models.ChatDirectKey(userId, otherUserId)).Pluck("id", &chatIds).Error; err != nil {
		return nil, err
	}
	if len(chatIds) == 0 {
		return nil, nil
	}
	return self.GetChat(chatIds[0])
}

func (self *transaction) GetChatOfConversation(conversationId string) (*models.Chat, error) {
	var chatIds []*string
	if err := self.tx.Model(&agentConversationModel{}).Where(`"id" = ?`, conversationId).Pluck("chat_id", &chatIds).Error; err != nil {
		return nil, err
	}
	if len(chatIds) == 0 || chatIds[0] == nil {
		return nil, nil
	}
	return self.GetChat(*chatIds[0])
}

func (self *transaction) touchChat(chatId string) error {
	return self.tx.Model(&chatModel{}).Where(`"id" = ?`, chatId).Update("modified_at", time.Now()).Error
}

func (self *transaction) OpenDirectChat(userId, otherUserId string) (*models.Chat, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return nil, ErrInvalidArguments
	}
	now := time.Now()
	key := models.ChatDirectKey(userId, otherUserId)
	model := &chatModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, ChatKind: string(models.ChatKindDirect),
		CreatedByUserID: &userId, DirectKey: &key,
	}
	for _, id := range []string{userId, otherUserId} {
		var count int64
		if err := self.tx.Model(&userModel{}).Where(`"id" = ?`, id).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNotFound
		}
	}
	// The unique key settles two opening the same chat at once.
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model).Error; err != nil {
		return nil, err
	}
	var chatIds []string
	if err := self.tx.Model(&chatModel{}).Where(`"direct_key" = ?`, key).Pluck("id", &chatIds).Error; err != nil {
		return nil, err
	}
	if len(chatIds) == 0 {
		return nil, ErrNotFound
	}
	if err := self.joinChatInOrder(chatIds[0], []string{userId, otherUserId}, func(string) models.MemberRole { return models.MemberRoleMember }, now); err != nil {
		return nil, err
	}
	return self.GetChat(chatIds[0])
}

func (self *transaction) CreateGroupChat(creatorUserId, title string, memberUserIds []string) (*models.Chat, error) {
	if creatorUserId == "" {
		return nil, ErrInvalidArguments
	}
	title = strings.TrimSpace(title)
	if err := models.ValidateChatTitle(title); err != nil {
		return nil, err
	}
	now := time.Now()
	peopleCount := 1
	for _, userId := range uniqueStrings(memberUserIds) {
		if userId != creatorUserId {
			peopleCount++
		}
	}
	if peopleCount > models.ChatMembersMost {
		return nil, limitReached("a group chat holds at most %d people", models.ChatMembersMost)
	}
	var madeCount int64
	if err := self.tx.Model(&chatModel{}).
		Where(`"created_by_user_id" = ? AND "chat_kind" = ? AND "circle_id" IS NULL AND "created_at" > ?`, creatorUserId, string(models.ChatKindGroup), now.Add(-24*time.Hour)).
		Count(&madeCount).Error; err != nil {
		return nil, err
	}
	if madeCount >= models.GroupChatsMadePerDayMost {
		return nil, limitReached("you have started %d group chats in the last day, the most one person starts; try again tomorrow", models.GroupChatsMadePerDayMost)
	}
	model := &chatModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, ChatKind: string(models.ChatKindGroup),
		Title: stringPointer(title), CreatedByUserID: &creatorUserId,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	if err := self.joinChatInOrder(model.ID, append([]string{creatorUserId}, memberUserIds...), func(userId string) models.MemberRole {
		if userId == creatorUserId {
			return models.MemberRoleAdmin
		}
		return models.MemberRoleMember
	}, now); err != nil {
		return nil, err
	}
	return self.GetChat(model.ID)
}

// lockedGroupChat takes a group chat's row for update, so that changes to who
// is in it do not interleave. ErrNotFound for any other kind, or a circle's.
func (self *transaction) lockedGroupChat(chatId string) (*chatModel, error) {
	var locked chatModel
	if err := lockRow(self.tx, &locked, chatId); err != nil {
		return nil, err
	}
	if locked.ChatKind != string(models.ChatKindGroup) || locked.CircleID != nil {
		return nil, ErrNotFound
	}
	return &locked, nil
}

func (self *transaction) RenameChat(chatId, title string) (*models.Chat, error) {
	if _, err := self.lockedGroupChat(chatId); err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	if err := models.ValidateChatTitle(title); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatModel{}).Where(`"id" = ?`, chatId).
		Updates(map[string]any{"title": stringPointer(title), "modified_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	return self.GetChat(chatId)
}

func (self *transaction) AddChatMembers(chatId string, userIds []string) (*models.Chat, error) {
	if _, err := self.lockedGroupChat(chatId); err != nil {
		return nil, err
	}
	now := time.Now()
	var currentUserIds []string
	if err := self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "removed_at" IS NULL AND "user_id" IS NOT NULL`, chatId).Pluck("user_id", &currentUserIds).Error; err != nil {
		return nil, err
	}
	peopleCount := len(currentUserIds)
	for _, userId := range uniqueStrings(userIds) {
		if !slices.Contains(currentUserIds, userId) {
			peopleCount++
		}
	}
	if peopleCount > models.ChatMembersMost {
		return nil, limitReached("a group chat holds at most %d people", models.ChatMembersMost)
	}
	if err := self.joinChatInOrder(chatId, userIds, func(string) models.MemberRole { return models.MemberRoleMember }, now); err != nil {
		return nil, err
	}
	if err := self.touchChat(chatId); err != nil {
		return nil, err
	}
	return self.GetChat(chatId)
}

func (self *transaction) RemoveChatMember(chatId, userId string) (*models.Chat, error) {
	if _, err := self.lockedGroupChat(chatId); err != nil {
		return nil, err
	}
	var rows []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ? AND "removed_at" IS NULL`, chatId, userId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	isDeleted, err := leaveChatRows(self.tx, chatId, userId, time.Now())
	if err != nil || isDeleted {
		return nil, err
	}
	return self.GetChat(chatId)
}

// leaveChatRows marks a member as out of a chat and keeps the chat in order:
// when the last admin of a group goes, the member who joined first becomes
// one, and when the last member goes the chat is deleted, which says so. The
// chat's row is the caller's to have locked. A circle's circle chat keeps
// the roles its circle gives, so no member is promoted there.
func leaveChatRows(tx *gorm.DB, chatId, userId string, now time.Time) (bool, error) {
	if err := tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ? AND "removed_at" IS NULL`, chatId, userId).
		Update("removed_at", now).Error; err != nil {
		return false, err
	}
	// Their agent stops remembering the chat and being asked in it.
	if err := switchOffChatAgent(tx, chatId, userId); err != nil {
		return false, err
	}
	var chat chatModel
	if err := tx.Where(`"id" = ?`, chatId).Limit(1).Find(&chat).Error; err != nil {
		return false, err
	}
	var remaining []chatMemberModel
	if err := tx.Where(`"chat_id" = ? AND "removed_at" IS NULL`, chatId).Order(`"joined_at" ASC, "user_id" ASC`).Find(&remaining).Error; err != nil {
		return false, err
	}
	if len(remaining) == 0 {
		return true, tx.Where(`"id" = ?`, chatId).Delete(&chatModel{}).Error
	}
	if chat.CircleID == nil && chat.ChatKind == string(models.ChatKindGroup) {
		hasAdmin := false
		for _, row := range remaining {
			if row.MemberRole == string(models.MemberRoleAdmin) {
				hasAdmin = true
			}
		}
		if !hasAdmin {
			if err := tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, valueOfString(remaining[0].UserID)).
				Update("member_role", string(models.MemberRoleAdmin)).Error; err != nil {
				return false, err
			}
		}
	}
	return false, tx.Model(&chatModel{}).Where(`"id" = ?`, chatId).Update("modified_at", now).Error
}

// ---- circles ----------------------------------------------------------

// ensureCircleChat is the circle chat of a circle, made with the circle's
// members when there is none: a circle made before chats existed gets its
// chat the first time anything asks. The circle's row is the caller's to
// have locked.
func (self *transaction) ensureCircleChat(circleId string) (string, error) {
	var chatIds []string
	if err := self.tx.Model(&chatModel{}).Where(`"circle_id" = ?`, circleId).Pluck("id", &chatIds).Error; err != nil {
		return "", err
	}
	if len(chatIds) > 0 {
		return chatIds[0], nil
	}
	var circle circleModel
	result := self.tx.Where(`"id" = ?`, circleId).Limit(1).Find(&circle)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", ErrNotFound
	}
	now := time.Now()
	model := &chatModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, ChatKind: string(models.ChatKindGroup),
		Title: stringPointer(circle.Name), CircleID: &circleId, CreatedByUserID: circle.CreatedByUserID,
	}
	if err := self.tx.Create(model).Error; err != nil {
		return "", err
	}
	var members []circleMemberModel
	if err := self.tx.Where(`"circle_id" = ?`, circleId).Find(&members).Error; err != nil {
		return "", err
	}
	roles := make(map[string]models.MemberRole, len(members))
	userIds := make([]string, 0, len(members))
	for _, member := range members {
		roles[member.UserID] = models.MemberRole(member.MemberRole)
		userIds = append(userIds, member.UserID)
	}
	if err := self.joinChatInOrder(model.ID, userIds, func(userId string) models.MemberRole { return roles[userId] }, now); err != nil {
		return "", err
	}
	return model.ID, nil
}

// joinCircleChat puts a new member of a circle in its circle chat.
func (self *transaction) joinCircleChat(circleId, userId string, memberRole models.MemberRole) error {
	chatId, err := self.ensureCircleChat(circleId)
	if err != nil {
		return err
	}
	return self.joinChat(chatId, userId, memberRole, time.Now())
}

// syncCircleChatRoles gives the members of a circle's chat the roles they
// have in the circle.
func syncCircleChatRoles(tx *gorm.DB, circleId string) error {
	return tx.Exec(`UPDATE "chat_member" SET "member_role" = "circle_member"."member_role"
		FROM "chat", "circle_member"
		WHERE "chat"."circle_id" = ? AND "chat_member"."chat_id" = "chat"."id" AND "circle_member"."circle_id" = "chat"."circle_id"
		  AND "circle_member"."user_id" = "chat_member"."user_id" AND "chat_member"."removed_at" IS NULL
		  AND "chat_member"."member_role" <> "circle_member"."member_role"`, circleId).Error
}

// leaveCircleChats takes a person out of the chats that follow from a circle
// they have left: the circle's chat, and every group chat in which they now
// share a circle with none of the others. A direct chat is left as it is,
// readable by both and refusing messages until they share a circle again.
func leaveCircleChats(tx *gorm.DB, circleId, userId string, now time.Time) error {
	var chatIds []string
	if err := tx.Model(&chatModel{}).Where(`"circle_id" = ?`, circleId).Pluck("id", &chatIds).Error; err != nil {
		return err
	}
	for _, chatId := range chatIds {
		if err := tx.Exec(`SELECT 1 FROM "chat" WHERE "id" = ? FOR UPDATE`, chatId).Error; err != nil {
			return err
		}
		if _, err := leaveChatRows(tx, chatId, userId, now); err != nil {
			return err
		}
	}
	// A group chat that is not a circle's, whose other members are now all
	// strangers to this person.
	var groupIds []string
	if err := tx.Raw(`SELECT "chat"."id" FROM "chat" JOIN "chat_member" ON "chat_member"."chat_id" = "chat"."id"
		WHERE "chat"."chat_kind" = 'group' AND "chat"."circle_id" IS NULL
		  AND "chat_member"."user_id" = ? AND "chat_member"."removed_at" IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM "chat_member" other
			JOIN "circle_member" theirs ON theirs."user_id" = other."user_id"
			JOIN "circle_member" mine ON mine."circle_id" = theirs."circle_id" AND mine."user_id" = ?
			WHERE other."chat_id" = "chat"."id" AND other."removed_at" IS NULL AND other."user_id" <> ?)
		ORDER BY "chat"."id"`, userId, userId, userId).Scan(&groupIds).Error; err != nil {
		return err
	}
	for _, chatId := range groupIds {
		if err := tx.Exec(`SELECT 1 FROM "chat" WHERE "id" = ? FOR UPDATE`, chatId).Error; err != nil {
			return err
		}
		if _, err := leaveChatRows(tx, chatId, userId, now); err != nil {
			return err
		}
	}
	return nil
}

// leaveEveryChat is what deleting an account does to its chats: the account
// is out of each, under the name it had, before the foreign keys set its
// user id to null. A chat left with nobody in it goes.
func leaveEveryChat(tx *gorm.DB, userId, name string, now time.Time) error {
	var chatIds []string
	if err := tx.Model(&chatMemberModel{}).Where(`"user_id" = ?`, userId).Order(`"chat_id" ASC`).Pluck("chat_id", &chatIds).Error; err != nil {
		return err
	}
	for _, chatId := range chatIds {
		if err := tx.Exec(`SELECT 1 FROM "chat" WHERE "id" = ? FOR UPDATE`, chatId).Error; err != nil {
			return err
		}
		if err := tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).Update("member_name", name).Error; err != nil {
			return err
		}
		if _, err := leaveChatRows(tx, chatId, userId, now); err != nil {
			return err
		}
	}
	// The files it sent go with its agent, whose rows they are: the messages
	// stay, saying their files were removed with the account instead of
	// offering a link that answers not found.
	if err := tx.Exec(`UPDATE "chat_message" SET "attachments" = (
			SELECT COALESCE(jsonb_agg(attached || '{"isRemoved": true}'::jsonb), '[]'::jsonb)
			FROM jsonb_array_elements("attachments") AS attached)
		WHERE "author_user_id" = ? AND jsonb_array_length("attachments") > 0`, userId).Error; err != nil {
		return err
	}
	// What it said carries the name it had; what it reacted with keeps it too.
	return tx.Model(&chatReactionModel{}).Where(`"reactor_key" = ?`, models.ChatReactorKey(models.ReactorPerson, userId)).
		Update("reactor_name", name).Error
}

func (self *transaction) SyncChatsOf(userId string) error {
	circlesWithoutChat := func() (circleIds []string, err error) {
		err = self.tx.Raw(`SELECT "circle_member"."circle_id" FROM "circle_member"
			WHERE "circle_member"."user_id" = ?
			  AND NOT EXISTS (SELECT 1 FROM "chat" WHERE "chat"."circle_id" = "circle_member"."circle_id")
			ORDER BY "circle_member"."circle_id"`, userId).Scan(&circleIds).Error
		return circleIds, err
	}
	circleIds, err := circlesWithoutChat()
	if err != nil {
		return err
	}
	if len(circleIds) > 0 {
		// The circle membership lock comes before the circles' rows (see
		// circleMembershipLockKey), taken only when there is a chat to make,
		// since this runs on every sign-in. The circles are read again once it
		// is held: one may have gone, or got its chat, while this waited.
		if err := lockCircleMembership(self.tx); err != nil {
			return err
		}
		if circleIds, err = circlesWithoutChat(); err != nil {
			return err
		}
	}
	for _, circleId := range circleIds {
		var locked circleModel
		if err := lockRow(self.tx, &locked, circleId); err != nil {
			return err
		}
		if _, err := self.ensureCircleChat(circleId); err != nil {
			return err
		}
	}
	// A conversation for every chat the person is in, which a forgotten
	// agent took with it.
	var chatIds []string
	if err := self.tx.Raw(`SELECT "chat_member"."chat_id" FROM "chat_member"
		JOIN "agent" ON "agent"."user_id" = "chat_member"."user_id"
		WHERE "chat_member"."user_id" = ?
		  AND NOT EXISTS (SELECT 1 FROM "agent_conversation" WHERE "agent_conversation"."agent_id" = "agent"."id" AND "agent_conversation"."chat_id" = "chat_member"."chat_id")`,
		userId).Scan(&chatIds).Error; err != nil {
		return err
	}
	if len(chatIds) == 0 {
		return nil
	}
	agent, err := self.GetAgentByUser(userId)
	if err != nil || agent == nil {
		return err
	}
	for _, chatId := range chatIds {
		if _, err := self.EnsureChatConversation(agent.ID, chatId); err != nil {
			return err
		}
	}
	return nil
}

// ---- listing ----------------------------------------------------------

func (self *transaction) ListChatConversations(agentId string) ([]*models.AgentConversation, error) {
	return self.ListAgentConversations(agentId, []models.AgentConversationKind{models.AgentConversationChat}, nil)
}

// chatUnreadCounts is how many messages by others each chat the person is in
// has that they have not read: those inside the time they were in the chat,
// newer than their read mark (by when stored, then by identifier, as
// moveChatReadMark has it), not deleted and not their own.
func (self *transaction) chatUnreadCounts(userId string) (map[string]int, error) {
	var rows []struct {
		ChatID string `gorm:"column:chat_id"`
		Count  int    `gorm:"column:unread_count"`
	}
	if err := self.tx.Raw(`SELECT "chat_member"."chat_id" AS chat_id, COUNT("chat_message"."id") AS unread_count
		FROM "chat_member" JOIN "chat_message" ON "chat_message"."chat_id" = "chat_member"."chat_id"
		LEFT JOIN "chat_message" AS "read_mark" ON "read_mark"."id" = "chat_member"."read_through_message_id"
		WHERE "chat_member"."user_id" = ?
		  AND "chat_message"."created_at" >= "chat_member"."joined_at"
		  AND ("chat_member"."removed_at" IS NULL OR "chat_message"."created_at" <= "chat_member"."removed_at")
		  AND ("read_mark"."id" IS NULL OR ("chat_message"."created_at", "chat_message"."id") > ("read_mark"."created_at", "read_mark"."id"))
		  AND "chat_message"."deleted_at" IS NULL
		  AND ("chat_message"."author_user_id" IS DISTINCT FROM "chat_member"."user_id" OR "chat_message"."author_agent_id" IS NOT NULL)
		GROUP BY "chat_member"."chat_id"`, userId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.ChatID] = row.Count
	}
	return counts, nil
}

func (self *transaction) ChatViewsFor(userId string, conversations []*models.AgentConversation) error {
	chatIds := []string{}
	for _, conversation := range conversations {
		if conversation.IsChat() && conversation.ChatID != "" {
			chatIds = append(chatIds, conversation.ChatID)
		}
	}
	if len(chatIds) == 0 {
		return nil
	}
	chats, err := self.loadChats(chatIds)
	if err != nil {
		return err
	}
	byId := make(map[string]*models.Chat, len(chats))
	for _, chat := range chats {
		byId[chat.ID] = chat
	}
	counts, err := self.chatUnreadCounts(userId)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, conversation := range conversations {
		chat := byId[conversation.ChatID]
		if !conversation.IsChat() || chat == nil {
			continue
		}
		view := ChatViewOf(chat, userId)
		view.UnreadCount = counts[chat.ID]
		if member := chat.Member(userId); member != nil {
			view.IsMuted = member.IsMutedAt(now)
			if view.IsMuted {
				mutedUntil := *member.MutedUntil
				view.MutedUntil = &mutedUntil
			}
		}
		conversation.Chat = view
		conversation.Title = view.Title
	}
	return nil
}

// ChatViewOf is a chat as one member sees it, without what is theirs alone
// (the unread count and the mute): its title for them, and who is in it.
func ChatViewOf(chat *models.Chat, userId string) *models.ChatView {
	view := &models.ChatView{
		ChatID:       chat.ID,
		ChatKind:     chat.ChatKind,
		IsCircleChat: chat.IsCircleChat(),
		Members:      chat.Members,
		Title:        chat.Title,
	}
	if chat.CircleID != "" {
		circleId := chat.CircleID
		view.CircleID = &circleId
	}
	if member := chat.Member(userId); member != nil {
		view.IsRemoved = member.IsRemoved
	}
	if view.Title == "" {
		names := []string{}
		for _, member := range chat.Members {
			if member.UserID == userId || (member.IsRemoved && chat.ChatKind == models.ChatKindGroup) {
				continue
			}
			names = append(names, member.Name)
		}
		if chat.ChatKind == models.ChatKindGroup && len(names) > 3 {
			names = append(names[:3], "…")
		}
		view.Title = strings.Join(names, ", ")
	}
	return view
}

// ---- messages ---------------------------------------------------------

func encodeChatAttachments(attachments []models.AgentAttachment) ([]byte, error) {
	if len(attachments) == 0 {
		return []byte("[]"), nil
	}
	// What the other members need to show and fetch the file; who uploaded
	// it and from which conversation is nobody else's business.
	kept := make([]models.AgentAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		kept = append(kept, models.AgentAttachment{
			ID: attachment.ID, CreatedAt: attachment.CreatedAt, Name: attachment.Name,
			ContentType: attachment.ContentType, Size: attachment.Size, IsRemoved: attachment.IsRemoved,
		})
	}
	return encodeJSON(kept)
}

func chatMessageFromModel(model *chatMessageModel) (*models.ChatMessage, error) {
	message := &models.ChatMessage{
		ID:               model.ID,
		ChatID:           model.ChatID,
		CreatedAt:        model.CreatedAt.In(time.Local),
		AuthorUserID:     valueOfString(model.AuthorUserID),
		AuthorName:       model.AuthorMemberName,
		AuthorOwnerName:  model.AuthorOwnerName,
		AuthorKind:       models.ChatAuthorKind(model.AuthorKind),
		AuthorAgentID:    valueOfString(model.AuthorAgentID),
		AgentRunID:       valueOfString(model.AgentRunID),
		Body:             model.Body,
		ReplyToMessageID: valueOfString(model.ReplyToMessageID),
		EditedAt:         localTime(model.EditedAt),
		DeletedAt:        localTime(model.DeletedAt),
	}
	if err := decodeJSON(model.Attachments, &message.Attachments); err != nil {
		return nil, fmt.Errorf("db: cannot read the attachments of chat message %q: %w", model.ID, err)
	}
	return message, nil
}

func (self *transaction) AppendChatMessage(message *models.ChatMessage) (*models.ChatMessage, error) {
	if message.ChatID == "" || message.AuthorUserID == "" {
		return nil, ErrInvalidArguments
	}
	var members []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ? AND "removed_at" IS NULL`, message.ChatID, message.AuthorUserID).Limit(1).Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrNotFound
	}
	encoded, err := encodeChatAttachments(message.Attachments)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	authorKind := models.ChatAuthorPerson
	if message.AuthorAgentID != "" {
		authorKind = models.ChatAuthorAgent
	}
	// An agent speaks for its owner, who is the member named; their name as
	// the chat knows them is kept beside the agent's, so that two agents with
	// the same name can be told apart.
	authorOwnerName := ""
	if authorKind == models.ChatAuthorAgent {
		// What the owner is called now, from the account; the member row's
		// name is a copy that UpdateUser keeps, and the fallback for an
		// account that cannot be read.
		ownerName := members[0].MemberName
		if owner, err := self.GetUser(message.AuthorUserID); err != nil {
			return nil, err
		} else if owner != nil && strings.TrimSpace(owner.DisplayName()) != "" {
			ownerName = owner.DisplayName()
		}
		authorOwnerName = truncateRunes(ownerName, 100)
	}
	model := &chatMessageModel{
		ID: newID(), ChatID: message.ChatID, CreatedAt: now,
		AuthorUserID: stringPointer(message.AuthorUserID), AuthorMemberName: truncateRunes(message.AuthorName, 100),
		AuthorOwnerName: authorOwnerName,
		AuthorKind:      string(authorKind),
		AuthorAgentID:   stringPointer(message.AuthorAgentID), AgentRunID: stringPointer(message.AgentRunID),
		Body: strings.ToValidUTF8(message.Body, "�"), Attachments: encoded,
		ReplyToMessageID: stringPointer(message.ReplyToMessageID),
	}
	if model.ReplyToMessageID != nil {
		var count int64
		if err := self.tx.Model(&chatMessageModel{}).Where(`"id" = ? AND "chat_id" = ?`, *model.ReplyToMessageID, message.ChatID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNotFound
		}
	}
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatModel{}).Where(`"id" = ?`, message.ChatID).
		Updates(map[string]any{"last_message_at": now, "modified_at": now}).Error; err != nil {
		return nil, err
	}
	// The members' conversations are sorted by their last message, as every
	// other conversation is.
	if err := self.tx.Exec(`UPDATE "agent_conversation" SET "last_at" = ?, "modified_at" = ?
		WHERE "chat_id" = ? AND "agent_id" IN (
			SELECT "agent"."id" FROM "agent" JOIN "chat_member" ON "chat_member"."user_id" = "agent"."user_id"
			WHERE "chat_member"."chat_id" = ? AND "chat_member"."removed_at" IS NULL)`, now, now, message.ChatID, message.ChatID).Error; err != nil {
		return nil, err
	}
	// What a person has written they have read up to. Not what their agent
	// has: the answer to a question is for them to read.
	if message.AuthorAgentID == "" {
		if _, err := moveChatReadMark(self.tx, message.ChatID, message.AuthorUserID, model.ID); err != nil {
			return nil, err
		}
	}
	return chatMessageFromModel(model)
}

// attachChatReactions puts each message's reactions on it, in the order they
// were made.
func (self *transaction) attachChatReactions(messages []*models.ChatMessage) error {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var found []chatReactionModel
	if err := self.tx.Where(`"message_id" IN ?`, ids).Order(`"created_at" ASC, "reactor_key" ASC`).Find(&found).Error; err != nil {
		return err
	}
	byMessage := map[string][]models.AgentMessageReaction{}
	for _, reaction := range found {
		kind, id := models.ReactorPerson, strings.TrimPrefix(reaction.ReactorKey, "user:")
		if strings.HasPrefix(reaction.ReactorKey, "agent:") {
			kind, id = models.ReactorAgent, strings.TrimPrefix(reaction.ReactorKey, "agent:")
		}
		byMessage[reaction.MessageID] = append(byMessage[reaction.MessageID], models.AgentMessageReaction{
			Emoji: reaction.Emoji, ReactorKind: kind, ReactedAt: reaction.CreatedAt.In(time.Local),
			ReactorUserID: id, ReactorName: reaction.ReactorName,
		})
	}
	for _, message := range messages {
		message.Reactions = byMessage[message.ID]
	}
	return nil
}

func (self *transaction) GetChatMessage(messageId string) (*models.ChatMessage, error) {
	var found []chatMessageModel
	if err := self.tx.Where(`"id" = ?`, messageId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	message, err := chatMessageFromModel(&found[0])
	if err != nil {
		return nil, err
	}
	if err := self.attachChatReactions([]*models.ChatMessage{message}); err != nil {
		return nil, err
	}
	return message, nil
}

func (self *transaction) EditChatMessage(messageId, body string) (*models.ChatMessage, error) {
	now := time.Now()
	result := self.tx.Model(&chatMessageModel{}).Where(`"id" = ? AND "deleted_at" IS NULL`, messageId).
		Updates(map[string]any{"body": strings.ToValidUTF8(body, "�"), "edited_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return self.GetChatMessage(messageId)
}

func (self *transaction) DeleteChatMessage(messageId string) (*models.ChatMessage, error) {
	now := time.Now()
	result := self.tx.Model(&chatMessageModel{}).Where(`"id" = ? AND "deleted_at" IS NULL`, messageId).
		Updates(map[string]any{"body": "", "attachments": []byte("[]"), "deleted_at": now})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	// A reaction on what was deleted has nothing to be about.
	if err := self.tx.Where(`"message_id" = ?`, messageId).Delete(&chatReactionModel{}).Error; err != nil {
		return nil, err
	}
	return self.GetChatMessage(messageId)
}

func (self *transaction) SetChatMessageReaction(messageId string, reactorKind models.AgentReactorKind, reactorId, reactorName, reactionEmoji string) (bool, error) {
	if messageId == "" || reactorId == "" {
		return false, ErrInvalidArguments
	}
	if reactorKind != models.ReactorPerson && reactorKind != models.ReactorAgent {
		return false, fmt.Errorf("db: %q reacts to nothing", reactorKind)
	}
	var count int64
	if err := self.tx.Model(&chatMessageModel{}).Where(`"id" = ? AND "deleted_at" IS NULL`, messageId).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, ErrNotFound
	}
	reactorKey := models.ChatReactorKey(reactorKind, reactorId)
	if reactionEmoji == "" {
		removed := self.tx.Where(`"message_id" = ? AND "reactor_key" = ?`, messageId, reactorKey).Delete(&chatReactionModel{})
		return removed.RowsAffected > 0, removed.Error
	}
	var existing []chatReactionModel
	if err := self.tx.Where(`"message_id" = ? AND "reactor_key" = ?`, messageId, reactorKey).Limit(1).Find(&existing).Error; err != nil {
		return false, err
	}
	if len(existing) > 0 && existing[0].Emoji == reactionEmoji {
		return false, nil
	}
	model := &chatReactionModel{
		MessageID: messageId, ReactorKey: reactorKey, ReactorName: truncateRunes(reactorName, 100),
		Emoji: reactionEmoji, CreatedAt: time.Now(),
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "reactor_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"emoji", "reactor_name", "created_at"}),
	}).Create(model).Error; err != nil {
		return false, err
	}
	return true, nil
}

func (self *transaction) MarkChatRead(chatId, userId, throughMessageId string) (bool, error) {
	var count int64
	if err := self.tx.Model(&chatMessageModel{}).Where(`"id" = ? AND "chat_id" = ?`, throughMessageId, chatId).Count(&count).Error; err != nil {
		return false, err
	}
	if count == 0 {
		return false, ErrNotFound
	}
	return moveChatReadMark(self.tx, chatId, userId, throughMessageId)
}

// moveChatReadMark moves the person's read mark in a chat forward to a message
// and says whether it moved. Forward is by when the messages were stored and
// then their identifiers, the order everything else reads a chat in
// ("created_at", "id"): identifiers made by different server instances are
// not in the order of the clocks, so comparing them alone could keep a mark
// behind a message it had passed, or move it back past one it had not. A mark
// on a message that is gone is moved.
func moveChatReadMark(tx *gorm.DB, chatId, userId, throughMessageId string) (bool, error) {
	result := tx.Exec(`UPDATE "chat_member" SET "read_through_message_id" = @through
		FROM "chat_message" AS "new_read"
		WHERE "chat_member"."chat_id" = @chat AND "chat_member"."user_id" = @user
		  AND "new_read"."id" = @through AND "new_read"."chat_id" = @chat
		  AND ("chat_member"."read_through_message_id" IS NULL OR NOT EXISTS (
		        SELECT 1 FROM "chat_message" AS "old_read"
		        WHERE "old_read"."id" = "chat_member"."read_through_message_id"
		          AND ("old_read"."created_at", "old_read"."id") >= ("new_read"."created_at", "new_read"."id")))`,
		sql.Named("through", throughMessageId), sql.Named("chat", chatId), sql.Named("user", userId))
	return result.RowsAffected > 0, result.Error
}

func (self *transaction) SetChatMuted(chatId, userId string, mutedUntil *time.Time) error {
	result := self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).Update("muted_until", mutedUntil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// chatQuotedLength is how much of a message a reply quotes.
const chatQuotedLength = 200

func (self *transaction) ListChatConversationMessages(conversation *models.AgentConversation, limit, offset int) ([]*models.AgentMessage, int, error) {
	return self.listChatConversationMessages(conversation, limit, offset, false)
}

func (self *transaction) ListChatMemberTurnMessages(conversation *models.AgentConversation, limit, offset int) ([]*models.AgentMessage, int, error) {
	return self.listChatConversationMessages(conversation, limit, offset, true)
}

// chatEveryMemberReadFrom is the earliest time from which every current
// member of the chat could read it: the latest time any of them joined. A
// member who left is not counted, since they read nothing that is said now.
func (self *transaction) chatEveryMemberReadFrom(chatId string) (time.Time, error) {
	var latestJoin sql.NullTime
	if err := self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "removed_at" IS NULL`, chatId).
		Select(`MAX("joined_at")`).Scan(&latestJoin).Error; err != nil {
		return time.Time{}, err
	}
	return latestJoin.Time, nil
}

// listChatConversationMessages reads the page for ListChatConversationMessages
// and, with isChatOnly, for ListChatMemberTurnMessages.
func (self *transaction) listChatConversationMessages(conversation *models.AgentConversation, limit, offset int, isChatOnly bool) ([]*models.AgentMessage, int, error) {
	if conversation == nil || !conversation.IsChat() || conversation.ChatID == "" {
		return nil, 0, ErrInvalidArguments
	}
	agent, err := self.GetAgent(conversation.AgentID)
	if err != nil || agent == nil {
		return nil, 0, err
	}
	var memberRows []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ?`, conversation.ChatID, agent.UserID).Limit(1).Find(&memberRows).Error; err != nil {
		return nil, 0, err
	}
	if len(memberRows) == 0 {
		return []*models.AgentMessage{}, 0, nil
	}
	member := memberRows[0]
	readFrom := member.JoinedAt
	if isChatOnly {
		latestJoin, err := self.chatEveryMemberReadFrom(conversation.ChatID)
		if err != nil {
			return nil, 0, err
		}
		if latestJoin.After(readFrom) {
			readFrom = latestJoin
		}
	}
	window := self.tx.Model(&chatMessageModel{}).Where(`"chat_id" = ? AND "created_at" >= ?`, conversation.ChatID, readFrom)
	if member.RemovedAt != nil {
		window = window.Where(`"created_at" <= ?`, *member.RemovedAt)
	}
	var chatTotal, ownTotal int64
	if err := window.Session(&gorm.Session{}).Count(&chatTotal).Error; err != nil {
		return nil, 0, err
	}
	if !isChatOnly {
		if err := self.tx.Model(&agentMessageModel{}).Where(`"conversation_id" = ?`, conversation.ID).Count(&ownTotal).Error; err != nil {
			return nil, 0, err
		}
	}
	total := int(chatTotal + ownTotal)
	// The newest offset+limit of each source hold the newest offset+limit of
	// the two together.
	want := limit + offset
	var chatRows []chatMessageModel
	if err := window.Session(&gorm.Session{}).Order(`"created_at" DESC, "id" DESC`).Limit(want).Find(&chatRows).Error; err != nil {
		return nil, 0, err
	}
	var ownRows []agentMessageModel
	if !isChatOnly {
		if err := self.tx.Where(`"conversation_id" = ?`, conversation.ID).Order(`"created_at" DESC, "id" DESC`).Limit(want).Find(&ownRows).Error; err != nil {
			return nil, 0, err
		}
	}
	type entry struct {
		createdAt time.Time
		id        string
		chat      *chatMessageModel
		own       *agentMessageModel
	}
	entries := make([]entry, 0, len(chatRows)+len(ownRows))
	for index := range chatRows {
		entries = append(entries, entry{createdAt: chatRows[index].CreatedAt, id: chatRows[index].ID, chat: &chatRows[index]})
	}
	for index := range ownRows {
		entries = append(entries, entry{createdAt: ownRows[index].CreatedAt, id: ownRows[index].ID, own: &ownRows[index]})
	}
	// Newest first, so the page is the head of the list; reversed below.
	sort.Slice(entries, func(left, right int) bool {
		if !entries[left].createdAt.Equal(entries[right].createdAt) {
			return entries[left].createdAt.After(entries[right].createdAt)
		}
		return entries[left].id > entries[right].id
	})
	if offset > len(entries) {
		offset = len(entries)
	}
	end := min(offset+limit, len(entries))
	page := entries[offset:end]

	chatMessages := []*models.ChatMessage{}
	for _, each := range page {
		if each.chat == nil {
			continue
		}
		message, err := chatMessageFromModel(each.chat)
		if err != nil {
			return nil, 0, err
		}
		chatMessages = append(chatMessages, message)
	}
	if err := self.attachChatReactions(chatMessages); err != nil {
		return nil, 0, err
	}
	quoted, err := self.quotedChatMessages(conversation.ChatID, chatMessages, readFrom)
	if err != nil {
		return nil, 0, err
	}
	converted := map[string]*models.AgentMessage{}
	for _, message := range chatMessages {
		converted[message.ID] = ChatMessageAsAgentMessage(message, conversation.ID, quoted)
	}
	messages := make([]*models.AgentMessage, 0, len(page))
	for index := len(page) - 1; index >= 0; index-- {
		each := page[index]
		if each.chat != nil {
			messages = append(messages, converted[each.id])
			continue
		}
		message, err := messageFromModel(each.own)
		if err != nil {
			return nil, 0, err
		}
		messages = append(messages, message)
	}
	return messages, total, nil
}

func (self *transaction) SharesChat(userId, otherUserId string) (bool, error) {
	if userId == "" || otherUserId == "" || userId == otherUserId {
		return false, nil
	}
	var count int64
	if err := self.tx.Table("chat_member AS mine").
		Joins("JOIN chat_member AS theirs ON theirs.chat_id = mine.chat_id").
		Where("mine.user_id = ? AND theirs.user_id = ? AND mine.removed_at IS NULL AND theirs.removed_at IS NULL", userId, otherUserId).
		Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// quotedChatMessages are the messages the given ones reply to, by id. A reply
// shows the words of the message it is to, and a member is not shown what was
// said before they could read the chat, so the words of a message made before
// readFrom are left out; the reply still says which message it is to.
func (self *transaction) quotedChatMessages(chatId string, messages []*models.ChatMessage, readFrom time.Time) (map[string]*models.ChatMessage, error) {
	ids := []string{}
	for _, message := range messages {
		if message.ReplyToMessageID != "" {
			ids = append(ids, message.ReplyToMessageID)
		}
	}
	quoted := map[string]*models.ChatMessage{}
	if len(ids) == 0 {
		return quoted, nil
	}
	var rows []chatMessageModel
	if err := self.tx.Where(`"chat_id" = ? AND "id" IN ?`, chatId, uniqueStrings(ids)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		message, err := chatMessageFromModel(&rows[index])
		if err != nil {
			return nil, err
		}
		if message.CreatedAt.Before(readFrom) {
			message.Body = ""
		}
		quoted[message.ID] = message
	}
	return quoted, nil
}

// ChatMessageAsAgentMessage is a chat message as the conversation of one of
// its members shows it: a person's is a user message and an agent's an
// assistant message, so that the screens that draw a conversation draw a
// chat. quoted holds the messages that replies are to, by id.
func ChatMessageAsAgentMessage(message *models.ChatMessage, conversationId string, quoted map[string]*models.ChatMessage) *models.AgentMessage {
	converted := &models.AgentMessage{
		ID:             message.ID,
		CreatedAt:      message.CreatedAt,
		ConversationID: conversationId,
		Role:           "user",
		Content:        message.Body,
		Attachments:    message.Attachments,
		Reactions:      message.Reactions,
		AuthorKind:     models.ChatAuthorPerson,
		AuthorUserID:   message.AuthorUserID,
		AuthorName:     message.AuthorName,
		EditedAt:       message.EditedAt,
		IsDeleted:      message.DeletedAt != nil,
	}
	if message.IsByAgent() {
		converted.Role = "assistant"
		converted.AuthorKind = models.ChatAuthorAgent
		converted.AuthorAgentID = message.AuthorAgentID
		converted.AuthorOwnerName = message.AuthorOwnerName
		converted.AgentRunID = message.AgentRunID
	}
	if replied := quoted[message.ReplyToMessageID]; replied != nil && message.ReplyToMessageID != "" {
		reference := models.AgentReference{AgentMessageID: replied.ID, QuotedRole: "user"}
		if replied.IsByAgent() {
			reference.QuotedRole = "assistant"
		}
		if replied.DeletedAt == nil {
			reference.QuotedText = truncateRunes(replied.Body, chatQuotedLength)
		}
		converted.References = []models.AgentReference{reference}
	}
	return converted
}
