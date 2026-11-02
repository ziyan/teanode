package models

import "testing"

// A voice message is read as its length and its words, and as the plain fact
// that there are none when the words are not there.
func TestWordsForReadingOfAVoiceMessage(t *testing.T) {
	t.Parallel()

	recording := []AgentAttachment{{Name: "Voice message.m4a", AudioDurationMS: 11700}}
	for name, testCase := range map[string]struct {
		message  ChatMessage
		expected string
	}{
		"words": {
			ChatMessage{IsVoiceMessage: true, Attachments: recording, TranscriptState: VoiceTranscriptDone, TranscriptText: " Leaving the bakery now. "},
			"[voice message, 0:12] Leaving the bakery now.",
		},
		"waiting": {
			ChatMessage{IsVoiceMessage: true, Attachments: recording, TranscriptState: VoiceTranscriptWaiting},
			"[voice message, 0:12, no transcript]",
		},
		"off": {
			ChatMessage{IsVoiceMessage: true, Attachments: recording, TranscriptState: VoiceTranscriptOff},
			"[voice message, 0:12, no transcript]",
		},
		"done with nothing said": {
			ChatMessage{IsVoiceMessage: true, Attachments: recording, TranscriptState: VoiceTranscriptDone},
			"[voice message, 0:12, no transcript]",
		},
		"over a minute": {
			ChatMessage{IsVoiceMessage: true, Attachments: []AgentAttachment{{AudioDurationMS: 75000}}, TranscriptState: VoiceTranscriptOff},
			"[voice message, 1:15, no transcript]",
		},
		"plain message": {
			ChatMessage{Body: "See you at six"},
			"See you at six",
		},
	} {
		if got := testCase.message.WordsForReading(); got != testCase.expected {
			t.Errorf("%s: got %q, want %q", name, got, testCase.expected)
		}
		agentMessage := AgentMessage{
			Content: testCase.message.Body, Attachments: testCase.message.Attachments, IsVoiceMessage: testCase.message.IsVoiceMessage,
			TranscriptState: testCase.message.TranscriptState, TranscriptText: testCase.message.TranscriptText,
		}
		if got := agentMessage.WordsForReading(); got != testCase.expected {
			t.Errorf("%s as an agent message: got %q, want %q", name, got, testCase.expected)
		}
	}
	if got := VoiceMessageSeconds(600); got != 1 {
		t.Errorf("a short recording is at least one second: %d", got)
	}
}
