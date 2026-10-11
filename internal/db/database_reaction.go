package db

import (
	"fmt"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// ReactionOperation is the emoji the person and the agent put on the
// messages of a conversation. The rows go with their message and their
// conversation, by foreign keys that cascade (migration 0162).
type ReactionOperation interface {
	// SetAgentMessageReaction puts a reactor's emoji on a message of the
	// conversation given, replacing the one it had; an empty emoji takes it
	// away. It says whether anything changed, and ErrNotFound when the
	// conversation has no such message or is not the agent's. The emoji is
	// the caller's to check (models.ValidateReactionEmoji).
	SetAgentMessageReaction(agentId, conversationId, messageId string, reactorKind models.AgentReactorKind, reactionEmoji string) (bool, error)

	// GetAgentMessage is a message by its id alone, with its reactions, or
	// nil; whose conversation it is in is the caller's to check.
	GetAgentMessage(messageId string) (*models.AgentMessage, error)

	// LastAgentPersonMessageID is the newest message the person wrote in a
	// conversation, leaving out the turns the agent took on its own, or
	// empty when they never wrote there.
	LastAgentPersonMessageID(conversationId string) (string, error)
}

type agentMessageReactionModel struct {
	MessageID      string    `gorm:"column:message_id;primaryKey"`
	ConversationID string    `gorm:"column:conversation_id"`
	AgentID        string    `gorm:"column:agent_id"`
	ReactorKind    string    `gorm:"column:reactor_kind;primaryKey"`
	Emoji          string    `gorm:"column:emoji"`
	ReactedAt      time.Time `gorm:"column:reacted_at"`
}

func (agentMessageReactionModel) TableName() string { return "agent_message_reaction" }

func (self *transaction) SetAgentMessageReaction(agentId, conversationId, messageId string, reactorKind models.AgentReactorKind, reactionEmoji string) (bool, error) {
	if agentId == "" || conversationId == "" || messageId == "" {
		return false, fmt.Errorf("db: a reaction needs the agent, the conversation and the message")
	}
	if reactorKind != models.ReactorPerson && reactorKind != models.ReactorAgent {
		return false, fmt.Errorf("db: %q reacts to nothing", reactorKind)
	}
	// The message must be in the conversation, and the conversation the
	// agent's: an id from somebody else's transcript changes nothing.
	var found int64
	if err := self.tx.Table(`"agent_message" AS "message"`).
		Joins(`JOIN "agent_conversation" AS "conversation" ON "conversation"."id" = "message"."conversation_id"`).
		Where(`"message"."id" = ? AND "message"."conversation_id" = ? AND "conversation"."agent_id" = ?`, messageId, conversationId, agentId).
		Count(&found).Error; err != nil {
		return false, err
	}
	if found == 0 {
		return false, ErrNotFound
	}
	if reactionEmoji == "" {
		removed := self.tx.Where(`"message_id" = ? AND "reactor_kind" = ?`, messageId, string(reactorKind)).Delete(&agentMessageReactionModel{})
		return removed.RowsAffected > 0, removed.Error
	}
	// Reacting with the emoji already there is no change, and says so, so
	// that nobody is told of one.
	var existing []agentMessageReactionModel
	if err := self.tx.Where(`"message_id" = ? AND "reactor_kind" = ?`, messageId, string(reactorKind)).Limit(1).Find(&existing).Error; err != nil {
		return false, err
	}
	if len(existing) > 0 && existing[0].Emoji == reactionEmoji {
		return false, nil
	}
	model := &agentMessageReactionModel{
		MessageID: messageId, ConversationID: conversationId, AgentID: agentId,
		ReactorKind: string(reactorKind), Emoji: reactionEmoji, ReactedAt: time.Now(),
	}
	if err := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "message_id"}, {Name: "reactor_kind"}},
		DoUpdates: clause.AssignmentColumns([]string{"emoji", "reacted_at"}),
	}).Create(model).Error; err != nil {
		return false, err
	}
	return true, nil
}

func (self *transaction) GetAgentMessage(messageId string) (*models.AgentMessage, error) {
	var found []agentMessageModel
	if err := self.tx.Where(`"id" = ?`, messageId).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	message, err := messageFromModel(&found[0])
	if err != nil {
		return nil, err
	}
	if err := self.attachReactions(message.ConversationID, []*models.AgentMessage{message}); err != nil {
		return nil, err
	}
	return message, nil
}

func (self *transaction) LastAgentPersonMessageID(conversationId string) (string, error) {
	var ids []string
	query := self.tx.Model(&agentMessageModel{}).Where(`"conversation_id" = ? AND "role" = ?`, conversationId, "user")
	if err := withoutOwnTurns(query, `"content"`).
		Order(`"created_at" DESC, "id" DESC`).Limit(1).Pluck("id", &ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// attachReactions puts each message's reactions on it: one query for the
// conversation, however many messages there are, the person's before the
// agent's.
func (self *transaction) attachReactions(conversationId string, messages []*models.AgentMessage) error {
	if conversationId == "" || len(messages) == 0 {
		return nil
	}
	var found []agentMessageReactionModel
	if err := self.tx.Where(`"conversation_id" = ?`, conversationId).
		Order(`"message_id" ASC, "reactor_kind" DESC`).Find(&found).Error; err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
	}
	byMessage := make(map[string][]models.AgentMessageReaction, len(found))
	for _, reaction := range found {
		byMessage[reaction.MessageID] = append(byMessage[reaction.MessageID], models.AgentMessageReaction{
			Emoji: reaction.Emoji, ReactorKind: models.AgentReactorKind(reaction.ReactorKind), ReactedAt: reaction.ReactedAt.In(time.Local),
		})
	}
	for _, message := range messages {
		message.Reactions = byMessage[message.ID]
	}
	return nil
}
