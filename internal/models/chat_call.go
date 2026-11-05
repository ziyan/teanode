package models

import (
	"fmt"
	"time"
)

// A call in a chat: everybody in the chat is rung, and talks with whoever
// answers, through the server's media server. See
// docs/subsystems/chat-calls.md.

// ChatCallEndReason says how a call ended; empty while it goes.
type ChatCallEndReason string

const (
	// ChatCallEnded: the last person on it left.
	ChatCallEnded ChatCallEndReason = "ended"
	// ChatCallMissed: nobody answered before the ringing stopped.
	ChatCallMissed ChatCallEndReason = "missed"
	// ChatCallDeclined: everybody rung said no.
	ChatCallDeclined ChatCallEndReason = "declined"
	// ChatCallFailed: the server stopped under it.
	ChatCallFailed ChatCallEndReason = "failed"
)

// ChatCallRingState is what came of ringing one member.
type ChatCallRingState string

const (
	// ChatCallRingNone: the one who started the call.
	ChatCallRingNone ChatCallRingState = "none"
	// ChatCallRinging: rung, and not answered yet.
	ChatCallRinging ChatCallRingState = "ringing"
	// ChatCallRingAnswered: they joined the call.
	ChatCallRingAnswered ChatCallRingState = "answered"
	// ChatCallRingDeclined: they said no.
	ChatCallRingDeclined ChatCallRingState = "declined"
	// ChatCallRingMissed: the ringing stopped before they answered.
	ChatCallRingMissed ChatCallRingState = "missed"
)

// ChatCallTranscriptState says how a call's transcript stands: empty when it
// was not asked for.
const (
	ChatCallTranscriptWaiting = "waiting"
	ChatCallTranscriptDone    = "done"
	ChatCallTranscriptFailed  = "failed"
)

// ChatCall is a call in a chat, with each member's part in it.
type ChatCall struct {
	ID              string            `json:"id"`
	ChatID          string            `json:"chatId"`
	StartedByUserID string            `json:"startedByUserId,omitempty"`
	StartedAt       time.Time         `json:"startedAt"`
	AnsweredAt      *time.Time        `json:"answeredAt,omitempty"`
	EndedAt         *time.Time        `json:"endedAt,omitempty"`
	EndReason       ChatCallEndReason `json:"endReason,omitempty"`

	// Instance is the server instance whose media server holds the call.
	Instance string `json:"-"`

	// TranscriptionAskedByUserID is who asked for the call to be
	// transcribed; empty when nobody did. Their agent pays for it.
	TranscriptionAskedByUserID string     `json:"transcriptionAskedByUserId,omitempty"`
	TranscriptState            string     `json:"transcriptState,omitempty"`
	TranscriptTriedAt          *time.Time `json:"-"`
	SummaryMessageID           string     `json:"summaryMessageId,omitempty"`
	// RecordingID is the recording the call became once it was
	// transcribed: owned by whoever turned transcription on.
	RecordingID string `json:"recordingId,omitempty"`

	Members []ChatCallMember `json:"members"`
}

// ChatCallMember is one person's part in a call.
type ChatCallMember struct {
	UserID    string            `json:"userId"`
	RingState ChatCallRingState `json:"ringState"`
	JoinedAt  *time.Time        `json:"joinedAt,omitempty"`
	LeftAt    *time.Time        `json:"leftAt,omitempty"`
	IsVideoOn bool              `json:"isVideoOn,omitempty"`
	IsMuted   bool              `json:"isMuted,omitempty"`
}

// IsGoing says the call has not ended.
func (self *ChatCall) IsGoing() bool {
	return self != nil && self.EndedAt == nil
}

// Member is the person's part in the call; nil when they have none.
func (self *ChatCall) Member(userId string) *ChatCallMember {
	if self == nil {
		return nil
	}
	for index := range self.Members {
		if self.Members[index].UserID == userId {
			return &self.Members[index]
		}
	}
	return nil
}

// IsOn says the member is on the call now: joined, and not left since.
func (self *ChatCallMember) IsOn() bool {
	return self != nil && self.JoinedAt != nil && self.LeftAt == nil
}

// OnCount is how many people are on the call now.
func (self *ChatCall) OnCount() int {
	count := 0
	for index := range self.Members {
		if self.Members[index].IsOn() {
			count++
		}
	}
	return count
}

// DurationMS is how long the call went from its first answer to its end, or
// to now while it goes; zero for a call nobody answered.
func (self *ChatCall) DurationMS(now time.Time) int64 {
	if self == nil || self.AnsweredAt == nil {
		return 0
	}
	end := now
	if self.EndedAt != nil {
		end = *self.EndedAt
	}
	return max(0, end.Sub(*self.AnsweredAt).Milliseconds())
}

// ChatCallWords is what a model reads in place of a call's line in a chat:
// "[call, 4:12]", "[call, 4:12, transcribed]", "[call going]", "[missed
// call]" or "[declined call]". The transcript is read with the chat tool,
// not here.
func ChatCallWords(call *ChatCall) string {
	if call == nil {
		return "[call]"
	}
	if call.IsGoing() {
		return "[call going]"
	}
	switch call.EndReason {
	case ChatCallMissed, ChatCallFailed:
		if call.AnsweredAt == nil {
			return "[missed call]"
		}
	case ChatCallDeclined:
		return "[declined call]"
	}
	words := fmt.Sprintf("[call, %s", VoiceMessageClock(call.DurationMS(time.Now())))
	if call.TranscriptState == ChatCallTranscriptDone {
		words += ", transcribed"
	}
	return words + "]"
}
