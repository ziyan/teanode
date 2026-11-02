package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A voice message keeps its recording's length and waveform with the file it
// carries, for every member who reads it; one claimed is not claimed again
// until the claim has run out; its words are written once and only while it
// is waiting; a deleted one has no words and cannot be given any.
func TestAVoiceMessageIsClaimedAndFinishedOnce(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mira := newCircleTestUser(t, tx, "voice-mira")
		tobias := newCircleTestUser(t, tx, "voice-tobias")
		chat, err := tx.OpenDirectChat(mira.ID, tobias.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}
		miraAgent, err := tx.GetAgentByUser(mira.ID)
		if err != nil || miraAgent == nil {
			t.Fatalf("Mira has no agent: %v", err)
		}
		recording, err := tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: miraAgent.ID, Name: "Voice message.m4a", ContentType: "audio/mp4", Size: 100, AudioDurationMS: 12300, AudioWaveform: "AAEC",
		})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
		if got, _ := tx.GetAgentAttachment(recording.ID); got.AudioDurationMS != 12300 || got.AudioWaveform != "AAEC" {
			t.Errorf("the file keeps %+v", got)
		}
		posted, err := tx.AppendChatMessage(&models.ChatMessage{
			ChatID: chat.ID, AuthorUserID: mira.ID, AuthorName: mira.DisplayName(), IsVoiceMessage: true,
			TranscriptState: models.VoiceTranscriptWaiting, Attachments: []models.AgentAttachment{*recording},
		})
		if err != nil {
			t.Fatalf("AppendChatMessage: %s", err)
		}
		read, err := tx.GetChatMessage(posted.ID)
		if err != nil || read == nil {
			t.Fatalf("GetChatMessage: %v %v", read, err)
		}
		if !read.IsVoiceMessage || read.TranscriptState != models.VoiceTranscriptWaiting || read.Body != "" ||
			len(read.Attachments) != 1 || read.Attachments[0].AudioDurationMS != 12300 || read.Attachments[0].AudioWaveform != "AAEC" {
			t.Fatalf("the message reads %+v", read)
		}
		// Another member's conversation shows the same facts.
		shown := db.ChatMessageAsAgentMessage(read, "", nil)
		if !shown.IsVoiceMessage || shown.VoiceMessageDurationMS() != 12300 || shown.WordsForReading() != "[voice message, 0:12, no transcript]" {
			t.Errorf("the message is drawn as %+v", shown)
		}

		// Waiting, and nobody has it.
		waiting, err := tx.ListWaitingVoiceMessageIDs(5*time.Minute, 10)
		if err != nil || len(waiting) != 1 || waiting[0] != posted.ID {
			t.Fatalf("waiting: %v %v", waiting, err)
		}
		claimed, err := tx.ClaimVoiceMessageTranscript(posted.ID, 5*time.Minute)
		if err != nil || claimed == nil {
			t.Fatalf("the first claim: %v %v", claimed, err)
		}
		if again, err := tx.ClaimVoiceMessageTranscript(posted.ID, 5*time.Minute); err != nil || again != nil {
			t.Errorf("a claimed message was claimed again: %v %v", again, err)
		}
		if waiting, _ := tx.ListWaitingVoiceMessageIDs(5*time.Minute, 10); len(waiting) != 0 {
			t.Errorf("a claimed message is listed as waiting: %v", waiting)
		}
		// A claim that has run out is no claim.
		if again, err := tx.ClaimVoiceMessageTranscript(posted.ID, 0); err != nil || again == nil {
			t.Errorf("a claim that ran out was not taken over: %v %v", again, err)
		}

		if _, err := tx.FinishVoiceMessageTranscript(posted.ID, models.VoiceTranscriptWaiting, "x"); err == nil {
			t.Error("a message was finished as still waiting")
		}
		finished, err := tx.FinishVoiceMessageTranscript(posted.ID, models.VoiceTranscriptDone, "Leaving the bakery now")
		if err != nil || finished == nil || finished.TranscriptText != "Leaving the bakery now" || finished.TranscriptState != models.VoiceTranscriptDone || finished.EditedAt != nil {
			t.Fatalf("finishing: %+v %v", finished, err)
		}
		if again, err := tx.FinishVoiceMessageTranscript(posted.ID, models.VoiceTranscriptFailed, ""); err != nil || again != nil {
			t.Errorf("a finished message was finished again: %v %v", again, err)
		}
		if claimed, err := tx.ClaimVoiceMessageTranscript(posted.ID, 0); err != nil || claimed != nil {
			t.Errorf("a finished message was claimed: %v %v", claimed, err)
		}

		// Half an hour on, a message still waiting is given up on; a fresh one is not.
		old, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: mira.ID, AuthorName: mira.DisplayName(), IsVoiceMessage: true, TranscriptState: models.VoiceTranscriptWaiting})
		if err != nil {
			t.Fatal(err)
		}
		failed, err := tx.FailStaleVoiceMessages(time.Now().Add(time.Minute))
		if err != nil || len(failed) != 1 || failed[0].ID != old.ID || failed[0].TranscriptState != models.VoiceTranscriptFailed {
			t.Fatalf("giving up: %+v %v", failed, err)
		}
		if again, _ := tx.FailStaleVoiceMessages(time.Now().Add(time.Minute)); len(again) != 0 {
			t.Errorf("a message was given up on twice: %+v", again)
		}

		// Deleting takes the words, and nothing is left to write them for.
		third, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: mira.ID, AuthorName: mira.DisplayName(), IsVoiceMessage: true, TranscriptState: models.VoiceTranscriptWaiting})
		if err != nil {
			t.Fatal(err)
		}
		deleted, err := tx.DeleteChatMessage(posted.ID)
		if err != nil || deleted.IsVoiceMessage || deleted.TranscriptText != "" || deleted.TranscriptState != "" || len(deleted.Attachments) != 0 {
			t.Errorf("a deleted message keeps %+v (%v)", deleted, err)
		}
		if _, err := tx.DeleteChatMessage(third.ID); err != nil {
			t.Fatal(err)
		}
		if claimed, err := tx.ClaimVoiceMessageTranscript(third.ID, 0); err != nil || claimed != nil {
			t.Errorf("a deleted message was claimed: %v %v", claimed, err)
		}
		if finished, err := tx.FinishVoiceMessageTranscript(third.ID, models.VoiceTranscriptDone, "late words"); err != nil || finished != nil {
			t.Errorf("a deleted message was given words: %v %v", finished, err)
		}
	})
}
