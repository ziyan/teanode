package db

import (
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

type chatCallModel struct {
	ID                         string     `gorm:"column:id;primaryKey"`
	ChatID                     string     `gorm:"column:chat_id"`
	StartedByUserID            *string    `gorm:"column:started_by_user_id"`
	StartedAt                  time.Time  `gorm:"column:started_at"`
	AnsweredAt                 *time.Time `gorm:"column:answered_at"`
	EndedAt                    *time.Time `gorm:"column:ended_at"`
	EndReason                  string     `gorm:"column:end_reason"`
	Instance                   string     `gorm:"column:instance"`
	TranscriptionAskedByUserID *string    `gorm:"column:transcription_asked_by_user_id"`
	TranscriptState            string     `gorm:"column:transcript_state"`
	TranscriptTriedAt          *time.Time `gorm:"column:transcript_tried_at"`
	SummaryMessageID           *string    `gorm:"column:summary_message_id"`
	RecordingID                *string    `gorm:"column:recording_id"`
}

func (chatCallModel) TableName() string { return "chat_call" }

type chatCallMemberModel struct {
	CallID    string     `gorm:"column:call_id;primaryKey"`
	UserID    string     `gorm:"column:user_id;primaryKey"`
	RingState string     `gorm:"column:ring_state"`
	JoinedAt  *time.Time `gorm:"column:joined_at"`
	LeftAt    *time.Time `gorm:"column:left_at"`
	IsVideoOn bool       `gorm:"column:is_video_on"`
	IsMuted   bool       `gorm:"column:is_muted"`
}

func (chatCallMemberModel) TableName() string { return "chat_call_member" }

func chatCallFromModel(model *chatCallModel, members []chatCallMemberModel) *models.ChatCall {
	call := &models.ChatCall{
		ID:                         model.ID,
		ChatID:                     model.ChatID,
		StartedByUserID:            valueOfString(model.StartedByUserID),
		StartedAt:                  model.StartedAt.In(time.Local),
		AnsweredAt:                 localTime(model.AnsweredAt),
		EndedAt:                    localTime(model.EndedAt),
		EndReason:                  models.ChatCallEndReason(model.EndReason),
		Instance:                   model.Instance,
		TranscriptionAskedByUserID: valueOfString(model.TranscriptionAskedByUserID),
		TranscriptState:            model.TranscriptState,
		TranscriptTriedAt:          localTime(model.TranscriptTriedAt),
		SummaryMessageID:           valueOfString(model.SummaryMessageID),
		RecordingID:                valueOfString(model.RecordingID),
		Members:                    []models.ChatCallMember{},
	}
	transcriptionConsent := ""
	if call.TranscriptionAskedByUserID != "" {
		transcriptionConsent = "yes"
	}
	for _, member := range members {
		call.Members = append(call.Members, models.ChatCallMember{
			UserID:               member.UserID,
			RingState:            models.ChatCallRingState(member.RingState),
			JoinedAt:             localTime(member.JoinedAt),
			LeftAt:               localTime(member.LeftAt),
			IsVideoOn:            member.IsVideoOn,
			IsMuted:              member.IsMuted,
			TranscriptionConsent: transcriptionConsent,
		})
	}
	return call
}

// chatCallsWithMembers reads the members of the calls given and makes them
// whole, in the order given.
func (self *transaction) chatCallsWithMembers(rows []chatCallModel) ([]*models.ChatCall, error) {
	calls := make([]*models.ChatCall, 0, len(rows))
	if len(rows) == 0 {
		return calls, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var memberRows []chatCallMemberModel
	if err := self.tx.Where(`"call_id" IN ?`, ids).Find(&memberRows).Error; err != nil {
		return nil, err
	}
	membersByCall := map[string][]chatCallMemberModel{}
	for _, member := range memberRows {
		membersByCall[member.CallID] = append(membersByCall[member.CallID], member)
	}
	for index := range rows {
		members := membersByCall[rows[index].ID]
		// The one who started it first, then in the order they joined, then
		// the rest by account, so a list of who was on reads the same each
		// time.
		sort.SliceStable(members, func(left, right int) bool {
			leftJoined, rightJoined := members[left].JoinedAt, members[right].JoinedAt
			switch {
			case members[left].RingState == string(models.ChatCallRingNone) != (members[right].RingState == string(models.ChatCallRingNone)):
				return members[left].RingState == string(models.ChatCallRingNone)
			case (leftJoined == nil) != (rightJoined == nil):
				return leftJoined != nil
			case leftJoined != nil && !leftJoined.Equal(*rightJoined):
				return leftJoined.Before(*rightJoined)
			}
			return members[left].UserID < members[right].UserID
		})
		calls = append(calls, chatCallFromModel(&rows[index], members))
	}
	return calls, nil
}

func (self *transaction) CreateChatCall(chatId, startedByUserId, instance string, rungUserIds []string) (*models.ChatCall, error) {
	if chatId == "" || startedByUserId == "" {
		return nil, ErrInvalidArguments
	}
	now := time.Now()
	model := &chatCallModel{
		ID: newID(), ChatID: chatId, StartedByUserID: stringPointer(startedByUserId), StartedAt: now, Instance: instance,
	}
	// One call goes in a chat at a time (the partial unique index
	// chat_call_going): a second is not made, without failing the
	// transaction the way a refused insert would.
	result := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(model)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrAlreadyExists
	}
	members := []chatCallMemberModel{{CallID: model.ID, UserID: startedByUserId, RingState: string(models.ChatCallRingNone), JoinedAt: &now}}
	for _, userId := range uniqueStrings(rungUserIds) {
		if userId == "" || userId == startedByUserId {
			continue
		}
		members = append(members, chatCallMemberModel{CallID: model.ID, UserID: userId, RingState: string(models.ChatCallRinging)})
	}
	if err := self.tx.Create(&members).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(model.ID)
}

func (self *transaction) GetChatCall(callId string) (*models.ChatCall, error) {
	var rows []chatCallModel
	if err := self.tx.Where(`"id" = ?`, callId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	calls, err := self.chatCallsWithMembers(rows)
	if err != nil {
		return nil, err
	}
	return calls[0], nil
}

func (self *transaction) GetGoingChatCall(chatId string) (*models.ChatCall, error) {
	var rows []chatCallModel
	if err := self.tx.Where(`"chat_id" = ? AND "ended_at" IS NULL`, chatId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	calls, err := self.chatCallsWithMembers(rows)
	if err != nil {
		return nil, err
	}
	return calls[0], nil
}

func (self *transaction) ListChatCalls(chatId string, before *time.Time, limit int) ([]*models.ChatCall, error) {
	if chatId == "" || limit <= 0 {
		return nil, ErrInvalidArguments
	}
	query := self.tx.Where(`"chat_id" = ?`, chatId)
	if before != nil {
		query = query.Where(`"started_at" < ?`, *before)
	}
	var rows []chatCallModel
	if err := query.Order(`"started_at" DESC, "id" DESC`).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.chatCallsWithMembers(rows)
}

func (self *transaction) ListGoingChatCallsOfUser(userId string) ([]*models.ChatCall, error) {
	var rows []chatCallModel
	if err := self.tx.Where(`"ended_at" IS NULL AND "id" IN (SELECT "call_id" FROM "chat_call_member" WHERE "user_id" = ?)`, userId).
		Order(`"started_at" ASC, "id" ASC`).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.chatCallsWithMembers(rows)
}

func (self *transaction) ListGoingChatCalls(instance string) ([]*models.ChatCall, error) {
	var rows []chatCallModel
	if err := self.tx.Where(`"ended_at" IS NULL AND "instance" = ?`, instance).Order(`"started_at" ASC, "id" ASC`).Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.chatCallsWithMembers(rows)
}

// goingChatCall locks a call that has not ended; ErrNotFound when it has.
func (self *transaction) goingChatCall(callId string) (*chatCallModel, error) {
	var rows []chatCallModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(`"id" = ? AND "ended_at" IS NULL`, callId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

func (self *transaction) JoinChatCall(callId, userId string) (*models.ChatCall, error) {
	call, err := self.goingChatCall(callId)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	result := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "user_id" = ?`, callId, userId).
		Updates(map[string]any{
			"joined_at": now, "left_at": nil,
			"ring_state": gorm.Expr(`CASE WHEN "ring_state" = 'none' THEN 'none' ELSE 'answered' END`),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		// Somebody who came into the chat after the call started joins it all
		// the same: they were not rung, and they answered.
		if err := self.tx.Create(&chatCallMemberModel{CallID: callId, UserID: userId, RingState: string(models.ChatCallRingAnswered), JoinedAt: &now}).Error; err != nil {
			return nil, err
		}
	}
	// Answered once somebody other than whoever started it is on it.
	if call.AnsweredAt == nil && valueOfString(call.StartedByUserID) != userId {
		if err := self.tx.Model(&chatCallModel{}).Where(`"id" = ? AND "answered_at" IS NULL`, callId).Update("answered_at", now).Error; err != nil {
			return nil, err
		}
	}
	return self.GetChatCall(callId)
}

func (self *transaction) DeclineChatCall(callId, userId string) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "user_id" = ? AND "ring_state" = ?`, callId, userId, models.ChatCallRinging).
		Update("ring_state", models.ChatCallRingDeclined).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) LeaveChatCall(callId, userId string) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "user_id" = ? AND "joined_at" IS NOT NULL AND "left_at" IS NULL`, callId, userId).
		Updates(map[string]any{"left_at": time.Now(), "is_video_on": false, "is_muted": false}).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) SetChatCallVideo(callId, userId string, isVideoOn bool) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "user_id" = ? AND "joined_at" IS NOT NULL AND "left_at" IS NULL`, callId, userId).
		Update("is_video_on", isVideoOn).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) SetChatCallMuted(callId, userId string, isMuted bool) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "user_id" = ? AND "joined_at" IS NOT NULL AND "left_at" IS NULL`, callId, userId).
		Update("is_muted", isMuted).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) EndChatCall(callId string, endReason models.ChatCallEndReason) (*models.ChatCall, error) {
	switch endReason {
	case models.ChatCallEnded, models.ChatCallMissed, models.ChatCallDeclined, models.ChatCallFailed:
	default:
		return nil, fmt.Errorf("%w: %q is not how a call ends", ErrInvalidArguments, endReason)
	}
	call, err := self.goingChatCall(callId)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	updates := map[string]any{"ended_at": now, "end_reason": endReason}
	// A transcript asked for is written now that the call is over.
	if call.TranscriptionAskedByUserID != nil && call.AnsweredAt != nil {
		updates["transcript_state"] = models.ChatCallTranscriptWaiting
	}
	if err := self.tx.Model(&chatCallModel{}).Where(`"id" = ?`, callId).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "ring_state" = ?`, callId, models.ChatCallRinging).
		Update("ring_state", models.ChatCallRingMissed).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "joined_at" IS NOT NULL AND "left_at" IS NULL`, callId).
		Updates(map[string]any{"left_at": now, "is_video_on": false, "is_muted": false}).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) StopRingingChatCall(callId string) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallMemberModel{}).Where(`"call_id" = ? AND "ring_state" = ?`, callId, models.ChatCallRinging).
		Update("ring_state", models.ChatCallRingMissed).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) AskChatCallTranscription(callId, userId string) (*models.ChatCall, error) {
	if _, err := self.goingChatCall(callId); err != nil {
		return nil, err
	}
	if err := self.tx.Model(&chatCallModel{}).Where(`"id" = ? AND "transcription_asked_by_user_id" IS NULL`, callId).
		Update("transcription_asked_by_user_id", userId).Error; err != nil {
		return nil, err
	}
	return self.GetChatCall(callId)
}

func (self *transaction) ClaimChatCallTranscript(callId string, retryAfter time.Duration) (*models.ChatCall, error) {
	now := time.Now()
	result := self.tx.Model(&chatCallModel{}).
		Where(`"id" = ? AND "transcript_state" = ? AND ("transcript_tried_at" IS NULL OR "transcript_tried_at" < ?)`,
			callId, models.ChatCallTranscriptWaiting, now.Add(-retryAfter)).
		Update("transcript_tried_at", now)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatCall(callId)
}

func (self *transaction) ListWaitingChatCallTranscriptIDs(retryAfter time.Duration, limit int) ([]string, error) {
	var callIds []string
	if err := self.tx.Model(&chatCallModel{}).
		Where(`"transcript_state" = ? AND ("transcript_tried_at" IS NULL OR "transcript_tried_at" < ?)`,
			models.ChatCallTranscriptWaiting, time.Now().Add(-retryAfter)).
		Order(`"ended_at" ASC, "id" ASC`).Limit(limit).Pluck("id", &callIds).Error; err != nil {
		return nil, err
	}
	return callIds, nil
}

func (self *transaction) SetChatCallRecording(callId, recordingId string) (*models.ChatCall, error) {
	if recordingId == "" {
		return nil, ErrInvalidArguments
	}
	result := self.tx.Model(&chatCallModel{}).Where(`"id" = ? AND "transcript_state" = ? AND "recording_id" IS NULL`, callId, models.ChatCallTranscriptWaiting).
		Update("recording_id", recordingId)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatCall(callId)
}

func (self *transaction) FinishChatCallTranscript(callId, transcriptState string, summaryMessageId string) (*models.ChatCall, error) {
	if transcriptState != models.ChatCallTranscriptDone && transcriptState != models.ChatCallTranscriptFailed {
		return nil, fmt.Errorf("%w: %q is not a state a transcript ends in", ErrInvalidArguments, transcriptState)
	}
	result := self.tx.Model(&chatCallModel{}).Where(`"id" = ? AND "transcript_state" = ?`, callId, models.ChatCallTranscriptWaiting).
		Updates(map[string]any{"transcript_state": transcriptState, "summary_message_id": stringPointer(summaryMessageId)})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return self.GetChatCall(callId)
}

// attachChatCalls reads the calls that messages are the lines of.
func (self *transaction) attachChatCalls(messages []*models.ChatMessage) error {
	var ids []string
	for _, message := range messages {
		if message.CallID != "" {
			ids = append(ids, message.CallID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []chatCallModel
	if err := self.tx.Where(`"id" IN ?`, uniqueStrings(ids)).Find(&rows).Error; err != nil {
		return err
	}
	calls, err := self.chatCallsWithMembers(rows)
	if err != nil {
		return err
	}
	byId := map[string]*models.ChatCall{}
	for _, call := range calls {
		byId[call.ID] = call
	}
	for _, message := range messages {
		if message.CallID != "" {
			message.Call = byId[message.CallID]
		}
	}
	return nil
}
