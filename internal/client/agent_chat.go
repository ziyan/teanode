package client

import (
	"context"
	"strings"
)

// Chats: conversations with other people in them. A chat is listed, read,
// written to and reacted to as any conversation is (agent_ask.go); these are
// what only a chat needs. Every operation names the caller's own conversation
// for the chat, or a message.

// AgentChat is a chat as one member sees it.
type AgentChat struct {
	ChatID       string `json:"chatId"`
	ChatKind     string `json:"chatKind"`
	CircleID     string `json:"circleId"`
	Title        string `json:"title"`
	IsCircleChat bool   `json:"isCircleChat"`

	// Members are the people in it, then those who left.
	Members []*AgentChatMember `json:"members"`

	// UnreadCount is how many messages by others you have not read.
	UnreadCount int `json:"unreadCount"`

	// NotificationLevel is what your phone is told of in the chat: all,
	// mentions or off.
	NotificationLevel string `json:"notificationLevel"`

	// IsRemoved says you are no longer in it; you read it up to then.
	IsRemoved bool `json:"isRemoved"`
}

// AgentChatMember is one person in a chat.
type AgentChatMember struct {
	UserID               string `json:"userId"`
	Username             string `json:"username"`
	Name                 string `json:"name"`
	MemberRole           string `json:"memberRole"`
	IsDeletedAccount     bool   `json:"isDeletedAccount"`
	IsRemoved            bool   `json:"isRemoved"`
	ReadThroughMessageID string `json:"readThroughMessageId"`
	IsAgentReading       bool   `json:"isAgentReading"`

	// IsAgentAskableByOthers says the others in the chat may ask their agent;
	// AgentName is what it is called and IsAgentActive whether it is on, told
	// for your own and for an agent its owner let the others ask.
	IsAgentAskableByOthers bool   `json:"isAgentAskableByOthers"`
	AgentID                string `json:"agentId"`
	AgentName              string `json:"agentName"`
	IsAgentActive          bool   `json:"isAgentActive"`
}

// chatFields is a chat as the commands read it. The pictures are the dashboard's
// and the app's to draw.
const chatFields = `{ chatId chatKind circleId title isCircleChat unreadCount notificationLevel isRemoved
		members { userId username name memberRole isDeletedAccount isRemoved readThroughMessageId isAgentReading isAgentAskableByOthers agentId agentName isAgentActive } }`

// The documents.
const (
	DocumentOpenDirectChat              = `mutation ($userId: String!) { OpenDirectChat(userId: $userId) ` + conversationFields + ` }`
	DocumentCreateGroupChat             = `mutation ($title: String, $userIds: [String!]!) { CreateGroupChat(title: $title, userIds: $userIds) ` + conversationFields + ` }`
	DocumentRenameChat                  = `mutation ($conversationId: String!, $title: String!) { RenameChat(conversationId: $conversationId, title: $title) ` + conversationFields + ` }`
	DocumentAddChatMembers              = `mutation ($conversationId: String!, $userIds: [String!]!) { AddChatMembers(conversationId: $conversationId, userIds: $userIds) ` + conversationFields + ` }`
	DocumentRemoveChatMember            = `mutation ($conversationId: String!, $userId: String!) { RemoveChatMember(conversationId: $conversationId, userId: $userId) ` + conversationFields + ` }`
	DocumentLeaveChat                   = `mutation ($conversationId: String!) { LeaveChat(conversationId: $conversationId) }`
	DocumentEditChatMessage             = `mutation ($messageId: String!, $message: String!) { EditChatMessage(messageId: $messageId, message: $message) { id createdAt content authorName editedAt } }`
	DocumentDeleteChatMessage           = `mutation ($messageId: String!) { DeleteChatMessage(messageId: $messageId) }`
	DocumentMarkChatRead                = `mutation ($conversationId: String!, $throughMessageId: String!) { MarkChatRead(conversationId: $conversationId, throughMessageId: $throughMessageId) }`
	DocumentSetChatNotificationLevel    = `mutation ($conversationId: String!, $notificationLevel: String!) { SetChatNotificationLevel(conversationId: $conversationId, notificationLevel: $notificationLevel) ` + conversationFields + ` }`
	DocumentSetChatTyping               = `mutation ($conversationId: String!) { SetChatTyping(conversationId: $conversationId) }`
	DocumentSetChatAgentReading         = `mutation ($conversationId: String!, $isAgentReading: Boolean!) { SetChatAgentReading(conversationId: $conversationId, isAgentReading: $isAgentReading) ` + conversationFields + ` }`
	DocumentSetChatAgentAskableByOthers = `mutation ($conversationId: String!, $isAgentAskableByOthers: Boolean!) { SetChatAgentAskableByOthers(conversationId: $conversationId, isAgentAskableByOthers: $isAgentAskableByOthers) ` + conversationFields + ` }`
	DocumentSayInChatAsAgent            = `mutation ($conversationId: String!, $message: String!) { SayInChatAsAgent(conversationId: $conversationId, message: $message) { id createdAt content authorName } }`
)

// OpenDirectChat is your direct chat with somebody you share a circle with,
// made when there is none, as your conversation.
func OpenDirectChat(ctx context.Context, connection *Client, userId string) (*AgentConversation, error) {
	var result struct {
		OpenDirectChat *AgentConversation `json:"OpenDirectChat"`
	}
	if err := connection.Execute(ctx, DocumentOpenDirectChat, map[string]any{"userId": userId}, &result); err != nil {
		return nil, err
	}
	return result.OpenDirectChat, nil
}

// CreateGroupChat makes a group chat with you as its admin and the people
// given, by account id, as its members.
func CreateGroupChat(ctx context.Context, connection *Client, title string, userIds []string) (*AgentConversation, error) {
	var result struct {
		CreateGroupChat *AgentConversation `json:"CreateGroupChat"`
	}
	variables := map[string]any{"userIds": userIds}
	if title = strings.TrimSpace(title); title != "" {
		variables["title"] = title
	}
	if err := connection.Execute(ctx, DocumentCreateGroupChat, variables, &result); err != nil {
		return nil, err
	}
	return result.CreateGroupChat, nil
}

// RenameChat changes a group chat's title.
func RenameChat(ctx context.Context, connection *Client, conversationId, title string) (*AgentConversation, error) {
	var result struct {
		RenameChat *AgentConversation `json:"RenameChat"`
	}
	if err := connection.Execute(ctx, DocumentRenameChat, map[string]any{"conversationId": conversationId, "title": title}, &result); err != nil {
		return nil, err
	}
	return result.RenameChat, nil
}

// AddChatMembers puts people in a group chat.
func AddChatMembers(ctx context.Context, connection *Client, conversationId string, userIds []string) (*AgentConversation, error) {
	var result struct {
		AddChatMembers *AgentConversation `json:"AddChatMembers"`
	}
	if err := connection.Execute(ctx, DocumentAddChatMembers, map[string]any{"conversationId": conversationId, "userIds": userIds}, &result); err != nil {
		return nil, err
	}
	return result.AddChatMembers, nil
}

// RemoveChatMember takes a person out of a group chat.
func RemoveChatMember(ctx context.Context, connection *Client, conversationId, userId string) (*AgentConversation, error) {
	var result struct {
		RemoveChatMember *AgentConversation `json:"RemoveChatMember"`
	}
	if err := connection.Execute(ctx, DocumentRemoveChatMember, map[string]any{"conversationId": conversationId, "userId": userId}, &result); err != nil {
		return nil, err
	}
	return result.RemoveChatMember, nil
}

// LeaveChat takes you out of a group chat.
func LeaveChat(ctx context.Context, connection *Client, conversationId string) error {
	var result struct {
		LeaveChat bool `json:"LeaveChat"`
	}
	return connection.Execute(ctx, DocumentLeaveChat, map[string]any{"conversationId": conversationId}, &result)
}

// EditChatMessage changes what a message of yours says.
func EditChatMessage(ctx context.Context, connection *Client, messageId, message string) (*AgentMessage, error) {
	var result struct {
		EditChatMessage *AgentMessage `json:"EditChatMessage"`
	}
	if err := connection.Execute(ctx, DocumentEditChatMessage, map[string]any{"messageId": messageId, "message": message}, &result); err != nil {
		return nil, err
	}
	return result.EditChatMessage, nil
}

// DeleteChatMessage takes a message back.
func DeleteChatMessage(ctx context.Context, connection *Client, messageId string) error {
	var result struct {
		DeleteChatMessage bool `json:"DeleteChatMessage"`
	}
	return connection.Execute(ctx, DocumentDeleteChatMessage, map[string]any{"messageId": messageId}, &result)
}

// MarkChatRead says you have read a chat through a message, and answers
// whether the mark moved: it never goes back.
func MarkChatRead(ctx context.Context, connection *Client, conversationId, throughMessageId string) (bool, error) {
	var result struct {
		MarkChatRead bool `json:"MarkChatRead"`
	}
	if err := connection.Execute(ctx, DocumentMarkChatRead, map[string]any{"conversationId": conversationId, "throughMessageId": throughMessageId}, &result); err != nil {
		return false, err
	}
	return result.MarkChatRead, nil
}

// SetChatNotificationLevel chooses what your phone is told of in a chat:
// every message (all), only one that mentions you or everybody (mentions),
// or nothing (off).
func SetChatNotificationLevel(ctx context.Context, connection *Client, conversationId, notificationLevel string) (*AgentConversation, error) {
	var result struct {
		SetChatNotificationLevel *AgentConversation `json:"SetChatNotificationLevel"`
	}
	variables := map[string]any{"conversationId": conversationId, "notificationLevel": notificationLevel}
	if err := connection.Execute(ctx, DocumentSetChatNotificationLevel, variables, &result); err != nil {
		return nil, err
	}
	return result.SetChatNotificationLevel, nil
}

// SetChatTyping tells the others in a chat you are writing.
func SetChatTyping(ctx context.Context, connection *Client, conversationId string) error {
	var result struct {
		SetChatTyping bool `json:"SetChatTyping"`
	}
	return connection.Execute(ctx, DocumentSetChatTyping, map[string]any{"conversationId": conversationId}, &result)
}

// SayInChatAsAgent says something in a chat as your agent, under its name.
func SayInChatAsAgent(ctx context.Context, connection *Client, conversationId, message string) (*AgentMessage, error) {
	var result struct {
		SayInChatAsAgent *AgentMessage `json:"SayInChatAsAgent"`
	}
	if err := connection.Execute(ctx, DocumentSayInChatAsAgent, map[string]any{"conversationId": conversationId, "message": message}, &result); err != nil {
		return nil, err
	}
	return result.SayInChatAsAgent, nil
}

// SetChatAgentReading makes your agent remember a chat from now on, or stop.
func SetChatAgentReading(ctx context.Context, connection *Client, conversationId string, isAgentReading bool) (*AgentConversation, error) {
	var result struct {
		SetChatAgentReading *AgentConversation `json:"SetChatAgentReading"`
	}
	if err := connection.Execute(ctx, DocumentSetChatAgentReading, map[string]any{"conversationId": conversationId, "isAgentReading": isAgentReading}, &result); err != nil {
		return nil, err
	}
	return result.SetChatAgentReading, nil
}

// SetChatAgentAskableByOthers lets the others in a chat address your agent
// there, or stops them.
func SetChatAgentAskableByOthers(ctx context.Context, connection *Client, conversationId string, isAgentAskableByOthers bool) (*AgentConversation, error) {
	var result struct {
		SetChatAgentAskableByOthers *AgentConversation `json:"SetChatAgentAskableByOthers"`
	}
	if err := connection.Execute(ctx, DocumentSetChatAgentAskableByOthers, map[string]any{"conversationId": conversationId, "isAgentAskableByOthers": isAgentAskableByOthers}, &result); err != nil {
		return nil, err
	}
	return result.SetChatAgentAskableByOthers, nil
}
