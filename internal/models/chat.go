package models

import (
	"strings"
	"time"
	"unicode/utf8"
)

// ChatKind says who a chat is between: two people, or any number.
type ChatKind string

// The kinds of chat. A circle's circle chat is a group.
const (
	ChatKindDirect ChatKind = "direct"
	ChatKindGroup  ChatKind = "group"
)

// IsValid reports whether this is one of the kinds.
func (self ChatKind) IsValid() bool {
	return self == ChatKindDirect || self == ChatKindGroup
}

// The limits on what a person writes in a chat.
const (
	// ChatTitleMostCharacters is the most characters a group's title may have.
	ChatTitleMostCharacters = 100

	// ChatMessageMostCharacters is the most characters a message may have.
	ChatMessageMostCharacters = 16000

	// ChatMessagesPerMinute is how many messages one person may send a
	// minute on one instance, which stops a runaway script and nobody typing.
	ChatMessagesPerMinute = 60

	// ChatMembersMost is how many people a group chat holds at once.
	ChatMembersMost = 50

	// GroupChatsMadePerDayMost is how many group chats one person starts in
	// a day.
	GroupChatsMadePerDayMost = 20
)

// ChatAuthorKind says whether a chat message was written by a person or by
// somebody's agent.
type ChatAuthorKind string

// The two who write.
const (
	ChatAuthorPerson ChatAuthorKind = "person"
	ChatAuthorAgent  ChatAuthorKind = "agent"
)

// ValidateChatTitle says why a title cannot be a group's, or nil.
func ValidateChatTitle(title string) error {
	var errors ValidationErrors
	if utf8.RuneCountInString(strings.TrimSpace(title)) > ChatTitleMostCharacters {
		errors.add("title", "must be at most %d characters", ChatTitleMostCharacters)
	}
	return errors.ErrOrNil()
}

// ValidateChatMessageBody says why a message cannot be sent, or nil. A
// message with nothing to say needs a file with it.
func ValidateChatMessageBody(body string, hasAttachments bool) error {
	var errors ValidationErrors
	if strings.TrimSpace(body) == "" && !hasAttachments {
		errors.add("message", "say something, or send a file")
	} else if utf8.RuneCountInString(body) > ChatMessageMostCharacters {
		errors.add("message", "must be at most %d characters", ChatMessageMostCharacters)
	}
	return errors.ErrOrNil()
}

// Chat is a conversation with people in it. What a person sees of it is
// their own conversation of the kind chat, which points here with ChatID.
type Chat struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	ChatKind   ChatKind  `json:"chatKind"`

	// Title is a group's name; empty for a direct chat, and for a group
	// nobody named.
	Title string `json:"title"`

	// CircleID is the circle this is the circle chat of, empty for any other.
	CircleID string `json:"circleId,omitempty"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`

	// Members are everybody who is or was in it, those still in it first,
	// then in the order they joined.
	Members []*ChatMember `json:"members"`
}

// IsCircleChat says whether this is the chat every circle has.
func (self *Chat) IsCircleChat() bool {
	return self != nil && self.CircleID != ""
}

// Member is the row of one account in the chat, taken out or not, or nil.
func (self *Chat) Member(userId string) *ChatMember {
	if userId == "" {
		return nil
	}
	for _, member := range self.Members {
		if member.UserID == userId {
			return member
		}
	}
	return nil
}

// ActiveMembers are the people in it now.
func (self *Chat) ActiveMembers() []*ChatMember {
	active := make([]*ChatMember, 0, len(self.Members))
	for _, member := range self.Members {
		if !member.IsRemoved {
			active = append(active, member)
		}
	}
	return active
}

// ChatMember is one person in one chat, as a screen draws them.
type ChatMember struct {
	// UserID is empty once the account is deleted.
	UserID   string `json:"userId"`
	Username string `json:"username"`

	// Name is what they were called when last seen, which a deleted account
	// keeps.
	Name string `json:"name"`

	// AvatarURL is the address of their picture, null when they have none.
	AvatarURL *string `json:"avatarUrl"`

	// AvatarColorIndex is which of the twelve avatar colors their initial is
	// drawn on when they have no picture.
	AvatarColorIndex int `json:"avatarColorIndex"`

	// MemberRole is admin or member. In a circle's circle chat it is the
	// role they have in the circle.
	MemberRole MemberRole `json:"memberRole"`

	// IsDeletedAccount says the account is gone and Name is all that is left.
	IsDeletedAccount bool `json:"isDeletedAccount"`

	// IsRemoved says they are not in the chat any more; they read it up to
	// the time they left.
	IsRemoved bool `json:"isRemoved"`

	// ReadThroughMessageID is the newest message they have read, empty when
	// they have read none.
	ReadThroughMessageID string `json:"readThroughMessageId"`

	// IsAgentReading says their agent remembers this chat.
	IsAgentReading bool `json:"isAgentReading"`

	// IsAgentAskableByOthers says the others in this chat may address their
	// agent: each of their questions starts a turn of it held to what
	// everybody in the chat may see. Off, only the person can.
	IsAgentAskableByOthers bool `json:"isAgentAskableByOthers"`

	// AgentID and AgentName say which agent is theirs and what it is called,
	// for an @ to address it by; the agent is shown by its name. IsAgentActive
	// says it is switched on, so addressing it starts a turn; a switched-off
	// agent is in the list but starts nothing.
	AgentID       string `json:"agentId"`
	AgentName     string `json:"agentName"`
	IsAgentActive bool   `json:"isAgentActive"`

	// The rest is read from the row for the code that decides, and not
	// shown.
	JoinedAt   time.Time  `json:"-"`
	RemovedAt  *time.Time `json:"-"`
	MutedUntil *time.Time `json:"-"`
	User       *User      `json:"-"`

	// ConversationID is the member's own conversation for the chat, when the
	// query that returned the member looked it up.
	ConversationID string `json:"-"`

	// IsConversationArchived says the member put that conversation away, which
	// quiets the chat for them the way a mute does.
	IsConversationArchived bool `json:"-"`
}

// IsMutedAt says whether the member asked not to be told of this chat at the
// moment given.
func (self *ChatMember) IsMutedAt(now time.Time) bool {
	return self != nil && self.MutedUntil != nil && self.MutedUntil.After(now)
}

// ChatView is a chat as one member sees it: what a screen needs beside the
// conversation, and what is theirs alone, their unread count and mute.
type ChatView struct {
	ChatID   string   `json:"chatId"`
	ChatKind ChatKind `json:"chatKind"`

	// CircleID is the circle this is the circle chat of, null for any other.
	CircleID *string `json:"circleId"`

	// Title is the group's name, or for a direct chat the other person's.
	Title string `json:"title"`

	IsCircleChat bool `json:"isCircleChat"`

	// Members are the people in it, then those who left.
	Members []*ChatMember `json:"members"`

	// UnreadCount is how many messages by others are newer than this
	// member's read mark.
	UnreadCount int `json:"unreadCount"`

	IsMuted    bool       `json:"isMuted"`
	MutedUntil *time.Time `json:"mutedUntil"`

	// IsRemoved says this member is no longer in the chat; it reads up to
	// the time they left and takes no more.
	IsRemoved bool `json:"isRemoved"`
}

// ChatMessage is a row of what was said: by a person, or by an agent
// speaking for its owner.
type ChatMessage struct {
	ID        string    `json:"id"`
	ChatID    string    `json:"chatId"`
	CreatedAt time.Time `json:"createdAt"`

	// AuthorUserID is empty once the account is gone. For an agent's
	// message it is the agent's owner.
	AuthorUserID string `json:"authorUserId,omitempty"`

	// AuthorName is what the author was called when they wrote it.
	AuthorName string `json:"authorName"`

	// AuthorOwnerName is, on an agent's message, the name of the agent's
	// owner as the chat knew them, kept like AuthorName after the account
	// is gone. Most agents keep the name "Agent", so the owner's name is what
	// tells two of them apart. Empty on a person's message.
	AuthorOwnerName string `json:"authorOwnerName,omitempty"`

	// AuthorKind says whether a person or an agent wrote it. It outlives
	// AuthorAgentID, which is cleared when the agent is deleted.
	AuthorKind ChatAuthorKind `json:"authorKind,omitempty"`

	// AuthorAgentID is set when the message is an agent's.
	AuthorAgentID string `json:"authorAgentId,omitempty"`

	AgentRunID string `json:"agentRunId,omitempty"`

	Body        string            `json:"body"`
	Attachments []AgentAttachment `json:"attachments,omitempty"`

	ReplyToMessageID string     `json:"replyToMessageId,omitempty"`
	EditedAt         *time.Time `json:"editedAt,omitempty"`
	DeletedAt        *time.Time `json:"deletedAt,omitempty"`

	// Reactions are one per reactor, in the order they were made.
	Reactions []AgentMessageReaction `json:"reactions,omitempty"`
}

// IsByAgent says an agent wrote it.
func (self *ChatMessage) IsByAgent() bool {
	return self != nil && (self.AuthorKind == ChatAuthorAgent || self.AuthorAgentID != "")
}

// ChatReactorKey is how a reactor is told apart from every other in
// chat_reaction: the account for a person, the agent for an agent.
func ChatReactorKey(reactorKind AgentReactorKind, id string) string {
	if reactorKind == ReactorAgent {
		return "agent:" + id
	}
	return "user:" + id
}

// ChatDirectKey is the same for the two people of a direct chat whichever way
// round they are given.
func ChatDirectKey(userId, otherUserId string) string {
	if userId > otherUserId {
		userId, otherUserId = otherUserId, userId
	}
	return userId + ":" + otherUserId
}

// AgentSpeakerLabel is how an agent's chat message says whose it is. Most
// agents keep the name "Agent", so a chat with two people's agents in it needs
// the owner to be told apart: "<agent name> · <owner name>'s agent", and just
// "<owner name>'s agent" while the agent still has the default name, which
// would otherwise repeat the word. The clients word it in the person's
// language; this is the English of the command line and of a phone's
// notification to a person who reads English (internal/chat words it in the
// others).
func AgentSpeakerLabel(agentName, ownerName string) string {
	agentName, ownerName = strings.TrimSpace(agentName), strings.TrimSpace(ownerName)
	if ownerName == "" {
		return agentName
	}
	owned := ownerName + "'s agent"
	if agentName == "" || agentName == AgentDefaultName {
		return owned
	}
	return agentName + " · " + owned
}
