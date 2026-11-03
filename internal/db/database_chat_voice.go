package db

import (
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func (self *transaction) ClaimVoiceMessageTranscript(messageId string, retryAfter time.Duration) (*models.ChatMessage, error) {
	now := time.Now()
	result := self.tx.Model(&chatMessageModel{}).
		Where(`"id" = ? AND "transcript_state" = ? AND "deleted_at" IS NULL AND ("transcript_tried_at" IS NULL OR "transcript_tried_at" < ?)`,
			messageId, models.VoiceTranscriptWaiting, now.Add(-retryAfter)).
		Update("transcript_tried_at", now)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatMessage(messageId)
}

func (self *transaction) FinishVoiceMessageTranscript(messageId, transcriptState, transcriptText string) (*models.ChatMessage, error) {
	switch transcriptState {
	case models.VoiceTranscriptDone, models.VoiceTranscriptFailed, models.VoiceTranscriptOff:
	default:
		return nil, fmt.Errorf("%w: %q is not a state a transcript ends in", ErrInvalidArguments, transcriptState)
	}
	result := self.tx.Model(&chatMessageModel{}).
		Where(`"id" = ? AND "transcript_state" = ? AND "deleted_at" IS NULL`, messageId, models.VoiceTranscriptWaiting).
		Updates(map[string]any{"transcript_state": transcriptState, "transcript_text": truncateRunes(transcriptText, 20000)})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatMessage(messageId)
}

func (self *transaction) WriteVoiceMessageWords(messageId, transcriptText string) (*models.ChatMessage, error) {
	result := self.tx.Model(&chatMessageModel{}).
		Where(`"id" = ? AND "transcript_state" IN ? AND "deleted_at" IS NULL`, messageId, []string{models.VoiceTranscriptOff, models.VoiceTranscriptFailed}).
		Updates(map[string]any{"transcript_state": models.VoiceTranscriptDone, "transcript_text": truncateRunes(transcriptText, 20000)})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatMessage(messageId)
}

func (self *transaction) ListWaitingVoiceMessageIDs(retryAfter time.Duration, limit int) ([]string, error) {
	var messageIds []string
	if err := self.tx.Model(&chatMessageModel{}).
		Where(`"transcript_state" = ? AND "deleted_at" IS NULL AND ("transcript_tried_at" IS NULL OR "transcript_tried_at" < ?)`,
			models.VoiceTranscriptWaiting, time.Now().Add(-retryAfter)).
		Order(`"created_at" ASC, "id" ASC`).Limit(limit).Pluck("id", &messageIds).Error; err != nil {
		return nil, err
	}
	return messageIds, nil
}

func (self *transaction) FailStaleVoiceMessages(before time.Time) ([]*models.ChatMessage, error) {
	var messageIds []string
	if err := self.tx.Raw(`UPDATE "chat_message" SET "transcript_state" = ?
		WHERE "transcript_state" = ? AND "created_at" < ?
		RETURNING "id"`, models.VoiceTranscriptFailed, models.VoiceTranscriptWaiting, before).Scan(&messageIds).Error; err != nil {
		return nil, err
	}
	messages := make([]*models.ChatMessage, 0, len(messageIds))
	for _, messageId := range messageIds {
		message, err := self.GetChatMessage(messageId)
		if err != nil {
			return nil, err
		}
		if message != nil {
			messages = append(messages, message)
		}
	}
	return messages, nil
}
