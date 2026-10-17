package db

import (
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// ChatAgentOperation is what agents in chats need beyond the chat itself: the
// two switches a member has for their own agent, the history a turn of it
// reads, and the messages its reading of the chat files.
type ChatAgentOperation interface {
	// SetChatAgentReading makes a member's agent remember the chat, or stop.
	// Turning it on makes the chat a knowledge source of the member's agent
	// (kind chat), read from now: what was said before is not read, nor what
	// is said while it is off. Turning it off switches the source off at once;
	// what the agent learned stays, as with any source. It answers the source,
	// nil when there is none. ErrNotFound when the member is not in the chat.
	SetChatAgentReading(chatId, userId string, isReading bool) (*models.AgentKnowledgeSource, error)

	// SetChatAgentAskableByOthers lets the others in a chat address a
	// member's agent, or stops them. ErrNotFound when the member is not in
	// the chat.
	SetChatAgentAskableByOthers(chatId, userId string, isAskable bool) error

	// ListChatTurnRows is what a turn of a member's agent reads of their chat
	// conversation: the chat's messages from the time the member joined until
	// they left, interleaved with the member's own agent's rows in the
	// conversation, oldest first. Without a compaction it is the newest limit
	// of them; with one, everything from the message it stands in for, which
	// is kept, and the compaction's note. A deleted message is left out.
	// With isChatOnly it is the chat's messages alone, the newest limit of
	// them, with none of the member's own agent's rows and no compaction, and
	// only those every current member of the chat could read (created at or
	// after the latest time any of them joined): what a turn somebody else
	// asked for may read, since those rows hold what the member's own agent
	// found, and the answer goes to everybody in the room.
	ListChatTurnRows(conversation *models.AgentConversation, limit int, isChatOnly bool) ([]*models.AgentMessage, error)

	// ListChatMessagesAfter is the messages of a chat newer than the one
	// given, oldest first, for the reading of a chat as a source. A message
	// deleted is left out. An empty id starts at the beginning.
	ListChatMessagesAfter(chatId, afterMessageId string, limit int) ([]*models.ChatMessage, error)

	// NewestChatMessageID is the id of the newest message of a chat, empty
	// when there is none.
	NewestChatMessageID(chatId string) (string, error)
}

// chatSourceName is what a chat's source is called on the Knowledge page.
func chatSourceName(chatTitle string) string {
	name := "Chat"
	if title := strings.TrimSpace(chatTitle); title != "" {
		name += ": " + title
	}
	return truncateRunes(name, 200)
}

// chatReadThroughKey is where a chat source's cursor remembers the newest
// message it has read; the agent reads it as the same key. The ingest pass
// keeps keys it does not know.
const chatReadThroughKey = "chatReadThrough"

func (self *transaction) SetChatAgentReading(chatId, userId string, isReading bool) (*models.AgentKnowledgeSource, error) {
	var members []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ? AND "removed_at" IS NULL`, chatId, userId).Limit(1).Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrNotFound
	}
	if err := self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).
		Update("is_agent_reading", isReading).Error; err != nil {
		return nil, err
	}
	agent, err := self.EnsureAgent(userId)
	if err != nil {
		return nil, err
	}
	return self.putChatSource(chatId, agent.ID, isReading)
}

// putChatSource makes the chat a source of the agent, or switches it off.
func (self *transaction) putChatSource(chatId, agentId string, isEnabled bool) (*models.AgentKnowledgeSource, error) {
	var found []agentSourceModel
	if err := self.tx.Where(`"agent_id" = ? AND "kind" = ? AND "specification"->>'chatId' = ?`, agentId, string(models.SourceChat), chatId).
		Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	var source *models.AgentKnowledgeSource
	if len(found) > 0 {
		var err error
		if source, err = self.LockAgentSource(agentId, found[0].ID); err != nil {
			return nil, err
		}
	}
	if source == nil && !isEnabled {
		return nil, nil
	}
	if source != nil && source.Enabled == isEnabled {
		return source, nil
	}
	if source == nil {
		chat, err := self.GetChat(chatId)
		if err != nil || chat == nil {
			return nil, firstError(err, ErrNotFound)
		}
		source = &models.AgentKnowledgeSource{
			AgentID: agentId, Kind: models.SourceChat, Name: chatSourceName(chat.Title),
			Specification: models.AgentKnowledgeSpecification{ChatID: chatId},
			// The dream reads at its own pace, as every source does.
			Cron: "17 3 * * *",
		}
	}
	source.Enabled = isEnabled
	if isEnabled {
		// From now: the pass reads what is said after the newest message
		// there is, so neither what came before nor what was said while it
		// was off is read.
		newest, err := self.NewestChatMessageID(chatId)
		if err != nil {
			return nil, err
		}
		source.Cursor = map[string]any{chatReadThroughKey: newest}
		now := time.Now()
		source.NextRunAt = &now
		source.LastError = ""
	}
	return self.PutAgentSource(source)
}

func firstError(first, second error) error {
	if first != nil {
		return first
	}
	return second
}

// switchOffChatAgent is what a member leaving a chat does to their agent's
// reading of it and to the others asking it: both stop with the membership.
// The chat's row is the caller's to have locked.
func switchOffChatAgent(tx *gorm.DB, chatId, userId string) error {
	if err := tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ?`, chatId, userId).
		Updates(map[string]any{"is_agent_reading": false, "is_agent_askable_by_others": false}).Error; err != nil {
		return err
	}
	// The generation moves, so a pass in flight stops filing what it read.
	return tx.Exec(`UPDATE "agent_source" SET "enabled" = false, "generation" = "generation" + 1, "modified_at" = ?
		WHERE "kind" = ? AND "specification"->>'chatId' = ? AND "enabled"
		  AND "agent_id" IN (SELECT "id" FROM "agent" WHERE "user_id" = ?)`, time.Now(), string(models.SourceChat), chatId, userId).Error
}

func (self *transaction) SetChatAgentAskableByOthers(chatId, userId string, isAskable bool) error {
	result := self.tx.Model(&chatMemberModel{}).Where(`"chat_id" = ? AND "user_id" = ? AND "removed_at" IS NULL`, chatId, userId).
		Update("is_agent_askable_by_others", isAskable)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) NewestChatMessageID(chatId string) (string, error) {
	var ids []string
	if err := self.tx.Model(&chatMessageModel{}).Where(`"chat_id" = ?`, chatId).Order(`"created_at" DESC, "id" DESC`).Limit(1).Pluck("id", &ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", nil
	}
	return ids[0], nil
}

// ListChatMessagesAfter reads in the order a chat is read in, when stored and
// then by identifier. Identifiers made by different server instances are not in
// the order of their clocks, so a cursor compared by identifier alone could
// pass over a message stored after it, or read one twice.
func (self *transaction) ListChatMessagesAfter(chatId, afterMessageId string, limit int) ([]*models.ChatMessage, error) {
	query := self.tx.Where(`"chat_id" = ? AND "deleted_at" IS NULL`, chatId)
	if afterMessageId != "" {
		var cursors []chatMessageModel
		if err := self.tx.Select("id", "created_at").Where(`"id" = ? AND "chat_id" = ?`, afterMessageId, chatId).Limit(1).Find(&cursors).Error; err != nil {
			return nil, err
		}
		if len(cursors) == 1 {
			query = query.Where(`("created_at", "id") > (?, ?)`, cursors[0].CreatedAt, cursors[0].ID)
		} else {
			// A cursor on a message that is gone has no time to be compared
			// by: its identifier is all that is left of it.
			query = query.Where(`"id" > ?`, afterMessageId)
		}
	}
	var rows []chatMessageModel
	if err := query.Order(`"created_at" ASC, "id" ASC`).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	messages := make([]*models.ChatMessage, 0, len(rows))
	for index := range rows {
		message, err := chatMessageFromModel(&rows[index])
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (self *transaction) ListChatTurnRows(conversation *models.AgentConversation, limit int, isChatOnly bool) ([]*models.AgentMessage, error) {
	if conversation == nil || !conversation.IsChat() || conversation.ChatID == "" {
		return nil, ErrInvalidArguments
	}
	agent, err := self.GetAgent(conversation.AgentID)
	if err != nil || agent == nil {
		return nil, err
	}
	var memberRows []chatMemberModel
	if err := self.tx.Where(`"chat_id" = ? AND "user_id" = ?`, conversation.ChatID, agent.UserID).Limit(1).Find(&memberRows).Error; err != nil {
		return nil, err
	}
	if len(memberRows) == 0 {
		return []*models.AgentMessage{}, nil
	}
	member := memberRows[0]
	readFrom := member.JoinedAt
	if isChatOnly {
		latestJoin, err := self.chatEveryMemberReadFrom(conversation.ChatID)
		if err != nil {
			return nil, err
		}
		if latestJoin.After(readFrom) {
			readFrom = latestJoin
		}
	}
	chatQuery := self.tx.Where(`"chat_id" = ? AND "created_at" >= ? AND "deleted_at" IS NULL`, conversation.ChatID, readFrom)
	if member.RemovedAt != nil {
		chatQuery = chatQuery.Where(`"created_at" <= ?`, *member.RemovedAt)
	}
	ownQuery := self.tx.Where(`"conversation_id" = ?`, conversation.ID)
	// A compaction stands in for everything up to a message: that message
	// and what came after it are read, and the compaction's note, which was
	// written after.
	through := conversation.CompactedThrough
	if isChatOnly {
		through = ""
	}
	if through != "" {
		chatQuery = chatQuery.Where(`"id" >= ?`, through)
		ownQuery = ownQuery.Where(`("id" >= ? OR "role" = ?)`, through, "compaction")
		limit = 0
	}
	var chatRows []chatMessageModel
	chatFind := chatQuery.Order(`"created_at" DESC, "id" DESC`)
	if limit > 0 {
		chatFind = chatFind.Limit(limit)
	}
	if err := chatFind.Find(&chatRows).Error; err != nil {
		return nil, err
	}
	var ownRows []agentMessageModel
	if !isChatOnly {
		ownFind := ownQuery.Order(`"created_at" DESC, "id" DESC`)
		if limit > 0 {
			ownFind = ownFind.Limit(limit)
		}
		if err := ownFind.Find(&ownRows).Error; err != nil {
			return nil, err
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
	sort.Slice(entries, func(left, right int) bool {
		if !entries[left].createdAt.Equal(entries[right].createdAt) {
			return entries[left].createdAt.Before(entries[right].createdAt)
		}
		return entries[left].id < entries[right].id
	})
	// The newest limit of the two together.
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	rows := make([]*models.AgentMessage, 0, len(entries))
	for _, each := range entries {
		if each.chat != nil {
			message, err := chatMessageFromModel(each.chat)
			if err != nil {
				return nil, err
			}
			rows = append(rows, ChatMessageAsAgentMessage(message, conversation.ID, nil))
			continue
		}
		message, err := messageFromModel(each.own)
		if err != nil {
			return nil, err
		}
		rows = append(rows, message)
	}
	return rows, nil
}
