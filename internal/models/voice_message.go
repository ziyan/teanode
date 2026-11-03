package models

import (
	"fmt"
	"strings"
)

// A voice message is a chat message that is one recording. The server writes
// its words after it is posted, so a message passes through these states.
const (
	// VoiceTranscriptWaiting: the words are being written.
	VoiceTranscriptWaiting = "waiting"

	// VoiceTranscriptDone: TranscriptText holds the words, which may be
	// empty when nothing was said.
	VoiceTranscriptDone = "done"

	// VoiceTranscriptFailed: the words could not be written and will not be
	// tried again.
	VoiceTranscriptFailed = "failed"

	// VoiceTranscriptOff: there is nothing to pay for the words (voice is
	// off for the server, or no member of the chat has an agent that is on
	// and within its budget), so there will be none unless a member asks
	// later. The recording still plays.
	VoiceTranscriptOff = "off"
)

// VoiceMessageWaveformLevelCount is how many peak levels a voice message's
// waveform holds.
const VoiceMessageWaveformLevelCount = 48

// VoiceMessageSeconds is the length of a recording in whole seconds, rounded
// to the nearest and never below one for a recording that has any length.
func VoiceMessageSeconds(durationMS int64) int64 {
	if durationMS <= 0 {
		return 0
	}
	return max(1, (durationMS+500)/1000)
}

// VoiceMessageClock is the length of a recording as a clock, "0:12".
func VoiceMessageClock(durationMS int64) string {
	seconds := VoiceMessageSeconds(durationMS)
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// voiceMessageWords is what a model reads in place of a recording: its length
// and its words, or the plain fact that there are none.
func voiceMessageWords(durationMS int64, transcriptState, transcriptText string) string {
	transcriptText = strings.TrimSpace(transcriptText)
	if transcriptState != VoiceTranscriptDone || transcriptText == "" {
		return fmt.Sprintf("[voice message, %s, no transcript]", VoiceMessageClock(durationMS))
	}
	return fmt.Sprintf("[voice message, %s] %s", VoiceMessageClock(durationMS), transcriptText)
}

// voiceMessageDurationMS is the length of the recording a message carries.
func voiceMessageDurationMS(attachments []AgentAttachment) int64 {
	for _, attachment := range attachments {
		if attachment.AudioDurationMS > 0 {
			return attachment.AudioDurationMS
		}
	}
	return 0
}

// VoiceMessageDurationMS is the length of the recording the message carries,
// zero when it is not a voice message.
func (self *ChatMessage) VoiceMessageDurationMS() int64 {
	if self == nil || !self.IsVoiceMessage {
		return 0
	}
	return voiceMessageDurationMS(self.Attachments)
}

// WordsForReading is what a model or a quote shows for the message: its
// body, or for a voice message "[voice message, 0:12] words" or "[voice
// message, 0:12, no transcript]".
func (self *ChatMessage) WordsForReading() string {
	if self == nil {
		return ""
	}
	if self.CallID != "" {
		return ChatCallWords(self.Call)
	}
	if !self.IsVoiceMessage {
		return self.Body
	}
	return voiceMessageWords(voiceMessageDurationMS(self.Attachments), self.TranscriptState, self.TranscriptText)
}

// VoiceMessageDurationMS is the length of the recording the message carries,
// zero when it is not a voice message.
func (self *AgentMessage) VoiceMessageDurationMS() int64 {
	if self == nil || !self.IsVoiceMessage {
		return 0
	}
	return voiceMessageDurationMS(self.Attachments)
}

// WordsForReading is what a model reads for the message: its content, or for
// a voice message the same words ChatMessage.WordsForReading gives.
func (self *AgentMessage) WordsForReading() string {
	if self == nil {
		return ""
	}
	if self.CallID != "" {
		return ChatCallWords(self.Call)
	}
	if !self.IsVoiceMessage {
		return self.Content
	}
	return voiceMessageWords(voiceMessageDurationMS(self.Attachments), self.TranscriptState, self.TranscriptText)
}
