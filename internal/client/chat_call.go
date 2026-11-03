package client

import (
	"context"
	"time"
)

// ChatCall is a call in a chat as the person asking sees it.
type ChatCall struct {
	ID              string            `json:"id"`
	ConversationID  string            `json:"conversationId"`
	ChatTitle       string            `json:"chatTitle"`
	StartedByName   string            `json:"startedByName"`
	StartedAt       time.Time         `json:"startedAt"`
	AnsweredAt      *time.Time        `json:"answeredAt"`
	EndedAt         *time.Time        `json:"endedAt"`
	EndReason       string            `json:"endReason"`
	IsGoing         bool              `json:"isGoing"`
	DurationMS      int64             `json:"durationMs"`
	TranscriptState string            `json:"transcriptState"`
	Members         []*ChatCallMember `json:"members"`
}

// ChatCallMember is one person's part in a call.
type ChatCallMember struct {
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	RingState string `json:"ringState"`
	IsOn      bool   `json:"isOn"`
}

// ChatCallTranscriptLine is one line of a transcribed call.
type ChatCallTranscriptLine struct {
	SpeakerName string `json:"speakerName"`
	StartMS     int64  `json:"startMs"`
	Text        string `json:"text"`
}

// ChatCallDetail is a call with its transcript, and the recording the call
// became when the caller may read it, theirs or shared with them.
type ChatCallDetail struct {
	Call             *ChatCall                 `json:"call"`
	Transcript       []*ChatCallTranscriptLine `json:"transcript"`
	RecordingID      string                    `json:"recordingId"`
	IsRecordingOwned bool                      `json:"isRecordingOwned"`
}

const chatCallFields = `{ id conversationId chatTitle startedByName startedAt answeredAt endedAt endReason isGoing durationMs transcriptState
	members { userId name ringState isOn } }`

const (
	DocumentListChatCalls = `query ($conversationId: String!, $first: Int) { ListChatCalls(conversationId: $conversationId, first: $first) ` + chatCallFields + ` }`
	DocumentReadChatCall  = `query ($callId: String!) { ReadChatCall(callId: $callId) { call ` + chatCallFields + ` transcript { speakerName startMs text } recordingId isRecordingOwned } }`
)

// ListChatCalls is a chat's calls, newest first.
func ListChatCalls(ctx context.Context, connection *Client, conversationId string, first int) ([]*ChatCall, error) {
	var result struct {
		ListChatCalls []*ChatCall `json:"ListChatCalls"`
	}
	if err := connection.Execute(ctx, DocumentListChatCalls, map[string]any{"conversationId": conversationId, "first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListChatCalls, nil
}

// ReadChatCall is one call with its transcript.
func ReadChatCall(ctx context.Context, connection *Client, callId string) (*ChatCallDetail, error) {
	var result struct {
		ReadChatCall *ChatCallDetail `json:"ReadChatCall"`
	}
	if err := connection.Execute(ctx, DocumentReadChatCall, map[string]any{"callId": callId}, &result); err != nil {
		return nil, err
	}
	return result.ReadChatCall, nil
}
