package db

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// OversightOperation is what an operator who may observe sees across every
// person's agent: their conversations and runs, which are in flight, and the
// turns that failed. Reading only.
type OversightOperation interface {
	// ListOversightConversations is every person's conversations and runs,
	// newest activity first, narrowed by the filter and paged by the options.
	// CountOversightConversations is how many the filter leaves.
	ListOversightConversations(filter *OversightFilter, options *Options) ([]*OversightConversation, error)
	CountOversightConversations(filter *OversightFilter) (int64, error)

	// ListFailedAgentTurns is the turns that ended in a failed note, newest
	// first, and CountFailedAgentTurns how many there are.
	ListFailedAgentTurns(filter *FailedTurnFilter, options *Options) ([]*FailedAgentTurn, error)
	CountFailedAgentTurns(filter *FailedTurnFilter) (int64, error)

	// RecordConversationObserved writes the audit row of the transaction's
	// actor reading another person's conversation, folded to one per reader
	// and conversation per interval. It says whether a row was written.
	RecordConversationObserved(conversation *models.AgentConversation, readerUserId, ownerUserId string, interval time.Duration, now time.Time) (bool, error)
}

// OversightFilter narrows the listing. Every field is optional.
type OversightFilter struct {
	// AgentID is one person's agent; empty is everybody's.
	AgentID string

	// Kinds are conversation kinds (main, named, goal) and the kinds of run
	// (a run's job kind). Empty is every conversation but a chat between
	// people, which is never listed.
	Kinds []string

	// Since and Until bound the conversation's last activity.
	Since *time.Time
	Until *time.Time

	// IsRunningOnly keeps the conversations with a turn in flight, one that
	// was not cut and whose instance was heard of since LiveAfter.
	IsRunningOnly bool
	LiveAfter     time.Time

	// JobID narrows to the runs one job made, Query to titles carrying the
	// words.
	JobID string
	Query string
}

// OversightConversation is a conversation with whose it is and whether a turn
// is running in it.
type OversightConversation struct {
	Conversation  *models.AgentConversation
	OwnerUsername string
	OwnerName     string
	IsRunning     bool
}

// FailedTurnFilter narrows the failed turns.
type FailedTurnFilter struct {
	AgentID string
	Since   *time.Time
}

// FailedAgentTurn is a turn that ended in a failed note.
type FailedAgentTurn struct {
	ConversationID    string
	ConversationKind  models.AgentConversationKind
	ConversationTitle string
	AgentID           string
	OwnerUsername     string
	OwnerName         string
	FailedAt          time.Time
	ErrorText         string
}

// OversightConversationModel is the conversation's row under an exported
// name, which is what lets it be embedded in a row that scans it.
type OversightConversationModel = agentConversationModel

type oversightRowModel struct {
	OversightConversationModel `gorm:"embedded"`
	OwnerUsername              string `gorm:"column:owner_username"`
	OwnerName                  string `gorm:"column:owner_name"`
	IsRunning                  bool   `gorm:"column:is_running"`
}

type failedTurnRowModel struct {
	ConversationID    string    `gorm:"column:conversation_id"`
	ConversationKind  string    `gorm:"column:conversation_kind"`
	ConversationTitle string    `gorm:"column:conversation_title"`
	AgentID           string    `gorm:"column:agent_id"`
	OwnerUsername     string    `gorm:"column:owner_username"`
	OwnerName         string    `gorm:"column:owner_name"`
	FailedAt          time.Time `gorm:"column:failed_at"`
	ErrorText         string    `gorm:"column:error_text"`
}

const runningTurnCondition = `EXISTS (SELECT 1 FROM "agent_turn" AS t WHERE t."conversation_id" = c."id" AND t."cut_at" IS NULL AND t."heartbeat_at" >= ?)`

func (self *transaction) oversightQuery(filter *OversightFilter) *gorm.DB {
	query := self.tx.Table(`"agent_conversation" AS c`).
		Joins(`JOIN "agent" AS a ON a."id" = c."agent_id"`).
		Joins(`JOIN "user" AS u ON u."id" = a."user_id"`).
		Where(`c."kind" <> ?`, string(models.AgentConversationChat))
	if filter == nil {
		return query
	}
	if filter.AgentID != "" {
		query = query.Where(`c."agent_id" = ?`, filter.AgentID)
	}
	if len(filter.Kinds) > 0 {
		var conversationKinds, runKinds []string
		for _, kind := range filter.Kinds {
			switch models.AgentConversationKind(kind) {
			case models.AgentConversationMain, models.AgentConversationNamed, models.AgentConversationGoal:
				conversationKinds = append(conversationKinds, kind)
			case models.AgentConversationChat:
				// Never listed.
			default:
				runKinds = append(runKinds, kind)
			}
		}
		switch {
		case len(conversationKinds) > 0 && len(runKinds) > 0:
			query = query.Where(`(c."kind" IN ? OR (c."kind" = ? AND c."job_kind" IN ?))`, conversationKinds, string(models.AgentConversationRun), runKinds)
		case len(conversationKinds) > 0:
			query = query.Where(`c."kind" IN ?`, conversationKinds)
		case len(runKinds) > 0:
			query = query.Where(`c."kind" = ? AND c."job_kind" IN ?`, string(models.AgentConversationRun), runKinds)
		default:
			query = query.Where(`1 = 0`)
		}
	}
	if filter.Since != nil {
		query = query.Where(`c."last_at" >= ?`, *filter.Since)
	}
	if filter.Until != nil {
		query = query.Where(`c."last_at" < ?`, *filter.Until)
	}
	if filter.IsRunningOnly {
		query = query.Where(runningTurnCondition, filter.LiveAfter)
	}
	if filter.JobID != "" {
		query = query.Where(`c."job_id" = ?`, filter.JobID)
	}
	if words := strings.TrimSpace(filter.Query); words != "" {
		query = query.Where(`c."title" ILIKE ?`, "%"+escapeLike(words)+"%")
	}
	return query
}

func (self *transaction) ListOversightConversations(filter *OversightFilter, options *Options) ([]*OversightConversation, error) {
	liveAfter := time.Now().Add(-time.Minute)
	if filter != nil && !filter.LiveAfter.IsZero() {
		liveAfter = filter.LiveAfter
	}
	query := self.oversightQuery(filter).
		Select(`c.*, u."username" AS owner_username, u."name" AS owner_name, `+runningTurnCondition+` AS is_running`, liveAfter).
		Order(`c."last_at" DESC, c."id" DESC`)
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	var rows []oversightRowModel
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	found := make([]*OversightConversation, 0, len(rows))
	for index := range rows {
		found = append(found, &OversightConversation{
			Conversation:  conversationFromModel(&rows[index].OversightConversationModel),
			OwnerUsername: rows[index].OwnerUsername,
			OwnerName:     rows[index].OwnerName,
			IsRunning:     rows[index].IsRunning,
		})
	}
	return found, nil
}

func (self *transaction) CountOversightConversations(filter *OversightFilter) (int64, error) {
	var count int64
	if err := self.oversightQuery(filter).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (self *transaction) failedTurnQuery(filter *FailedTurnFilter) *gorm.DB {
	query := self.tx.Table(`"agent_message" AS m`).
		Joins(`JOIN "agent_conversation" AS c ON c."id" = m."conversation_id"`).
		Joins(`JOIN "agent" AS a ON a."id" = c."agent_id"`).
		Joins(`JOIN "user" AS u ON u."id" = a."user_id"`).
		Where(`m."role" = ? AND m."name" = ? AND c."kind" <> ?`, models.AgentMessageNote, string(models.NoteFailed), string(models.AgentConversationChat))
	if filter == nil {
		return query
	}
	if filter.AgentID != "" {
		query = query.Where(`c."agent_id" = ?`, filter.AgentID)
	}
	if filter.Since != nil {
		query = query.Where(`m."created_at" >= ?`, *filter.Since)
	}
	return query
}

func (self *transaction) ListFailedAgentTurns(filter *FailedTurnFilter, options *Options) ([]*FailedAgentTurn, error) {
	query := self.failedTurnQuery(filter).
		Select(`c."id" AS conversation_id, c."kind" AS conversation_kind, c."title" AS conversation_title, c."agent_id" AS agent_id,
			u."username" AS owner_username, u."name" AS owner_name, m."created_at" AS failed_at, m."content" AS error_text`).
		Order(`m."created_at" DESC, m."id" DESC`)
	if options != nil && options.Limit > 0 {
		query = query.Limit(int(options.Limit)).Offset(int(options.Offset))
	}
	var rows []failedTurnRowModel
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	turns := make([]*FailedAgentTurn, 0, len(rows))
	for _, row := range rows {
		turns = append(turns, &FailedAgentTurn{
			ConversationID: row.ConversationID, ConversationKind: models.AgentConversationKind(row.ConversationKind),
			ConversationTitle: row.ConversationTitle, AgentID: row.AgentID,
			OwnerUsername: row.OwnerUsername, OwnerName: row.OwnerName,
			FailedAt: row.FailedAt.In(time.Local), ErrorText: row.ErrorText,
		})
	}
	return turns, nil
}

func (self *transaction) CountFailedAgentTurns(filter *FailedTurnFilter) (int64, error) {
	var count int64
	if err := self.failedTurnQuery(filter).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// RecordConversationObserved writes the audit row of an operator reading
// another person's conversation: who read it is the transaction's actor, and
// whose it is and what kind it is go in the row. At most one row per reader and
// conversation per interval, so paging a transcript does not flood the log;
// it says whether this read was the one that was recorded. Two reads at the
// same moment wait for each other rather than both skipping the row.
func (self *transaction) RecordConversationObserved(conversation *models.AgentConversation, readerUserId, ownerUserId string, interval time.Duration, now time.Time) (bool, error) {
	isTaken, err := self.TakeThrottleWait("observe:"+readerUserId+":"+conversation.ID, interval, now)
	if err != nil || !isTaken {
		return false, err
	}
	after := map[string]string{"ownerUserId": ownerUserId, "conversationKind": string(conversation.Kind)}
	if err := self.applyMutation(models.AuditResourceAgentConversation, conversation.ID, models.AuditActionObserveConversation, nil, after, func(*gorm.DB) error { return nil }); err != nil {
		return false, err
	}
	return true, nil
}
