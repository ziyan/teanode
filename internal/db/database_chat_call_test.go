package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A call goes in a chat one at a time: whoever starts it is on it and the
// others are rung; it is answered when somebody else joins, and a late
// member joins without having been rung; when it ends whoever is still rung
// missed it and whoever is on it has left; its line in the chat reads the
// call as it now stands.
func TestACallIsRungAnsweredAndEnded(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mira := newCircleTestUser(t, tx, "call-mira")
		tobias := newCircleTestUser(t, tx, "call-tobias")
		priya := newCircleTestUser(t, tx, "call-priya")
		late := newCircleTestUser(t, tx, "call-late")
		chat, err := tx.OpenDirectChat(mira.ID, tobias.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}

		call, err := tx.CreateChatCall(chat.ID, mira.ID, "instance-1", []string{tobias.ID, priya.ID, mira.ID})
		if err != nil {
			t.Fatalf("CreateChatCall: %s", err)
		}
		if !call.IsGoing() || call.OnCount() != 1 || len(call.Members) != 3 || call.Instance != "instance-1" {
			t.Fatalf("the call starts as %+v", call)
		}
		if starter := call.Member(mira.ID); starter.RingState != models.ChatCallRingNone || !starter.IsOn() {
			t.Errorf("whoever started it is %+v", starter)
		}
		if rung := call.Member(tobias.ID); rung.RingState != models.ChatCallRinging || rung.IsOn() {
			t.Errorf("a member rung is %+v", rung)
		}
		if _, err := tx.CreateChatCall(chat.ID, tobias.ID, "instance-1", nil); !errors.Is(err, db.ErrAlreadyExists) {
			t.Errorf("a second call was started in the same chat: %v", err)
		}
		if going, _ := tx.GetGoingChatCall(chat.ID); going == nil || going.ID != call.ID {
			t.Errorf("the going call is %+v", going)
		}

		line, err := tx.AppendChatMessage(&models.ChatMessage{ChatID: chat.ID, AuthorUserID: mira.ID, AuthorName: mira.DisplayName(), CallID: call.ID})
		if err != nil {
			t.Fatalf("AppendChatMessage: %s", err)
		}

		if call, err = tx.JoinChatCall(call.ID, tobias.ID); err != nil || call.AnsweredAt == nil || call.OnCount() != 2 {
			t.Fatalf("after Tobias joined: %+v %v", call, err)
		}
		if member := call.Member(tobias.ID); member.RingState != models.ChatCallRingAnswered {
			t.Errorf("Tobias answered as %+v", member)
		}
		if call, err = tx.JoinChatCall(call.ID, late.ID); err != nil || call.Member(late.ID) == nil || call.Member(late.ID).RingState != models.ChatCallRingAnswered {
			t.Fatalf("a member who was not rung joins: %+v %v", call, err)
		}
		if call, err = tx.SetChatCallVideo(call.ID, tobias.ID, true); err != nil || !call.Member(tobias.ID).IsVideoOn {
			t.Errorf("video: %+v %v", call, err)
		}
		if call, err = tx.LeaveChatCall(call.ID, tobias.ID); err != nil || call.Member(tobias.ID).IsOn() || call.Member(tobias.ID).IsVideoOn {
			t.Errorf("after Tobias left: %+v %v", call, err)
		}
		if call, err = tx.JoinChatCall(call.ID, tobias.ID); err != nil || !call.Member(tobias.ID).IsOn() {
			t.Errorf("Tobias came back: %+v %v", call, err)
		}

		if call, err = tx.EndChatCall(call.ID, models.ChatCallEnded); err != nil || call.IsGoing() || call.EndReason != models.ChatCallEnded || call.OnCount() != 0 {
			t.Fatalf("after the end: %+v %v", call, err)
		}
		if missed := call.Member(priya.ID); missed.RingState != models.ChatCallRingMissed {
			t.Errorf("Priya, still rung at the end, is %+v", missed)
		}
		if call.TranscriptState != "" {
			t.Errorf("a call nobody asked to transcribe waits for a transcript: %q", call.TranscriptState)
		}
		if _, err := tx.JoinChatCall(call.ID, priya.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a call that ended was joined: %v", err)
		}
		read, err := tx.GetChatMessage(line.ID)
		if err != nil || read.CallID != call.ID || read.Call == nil || read.Call.EndReason != models.ChatCallEnded {
			t.Fatalf("the call's line reads %+v %v", read, err)
		}
		if words := read.WordsForReading(); words != "[call, 0:00]" && words != "[call, 0:01]" {
			t.Errorf("a model reads the line as %q", words)
		}

		if listed, err := tx.ListChatCalls(chat.ID, nil, 10); err != nil || len(listed) != 1 || listed[0].ID != call.ID {
			t.Errorf("ListChatCalls: %+v %v", listed, err)
		}
		if going, err := tx.ListGoingChatCallsOfUser(tobias.ID); err != nil || len(going) != 0 {
			t.Errorf("an ended call is still going for Tobias: %+v %v", going, err)
		}
	})
}

// A call nobody answers is missed by everyone rung; one declined by the only
// person rung ends as declined.
func TestACallNobodyAnswersIsMissed(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mira := newCircleTestUser(t, tx, "missed-mira")
		tobias := newCircleTestUser(t, tx, "missed-tobias")
		chat, err := tx.OpenDirectChat(mira.ID, tobias.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}
		call, err := tx.CreateChatCall(chat.ID, mira.ID, "", []string{tobias.ID})
		if err != nil {
			t.Fatalf("CreateChatCall: %s", err)
		}
		if call, err = tx.StopRingingChatCall(call.ID); err != nil || call.Member(tobias.ID).RingState != models.ChatCallRingMissed {
			t.Fatalf("after the ringing stopped: %+v %v", call, err)
		}
		if call, err = tx.EndChatCall(call.ID, models.ChatCallMissed); err != nil || call.AnsweredAt != nil {
			t.Fatalf("EndChatCall: %+v %v", call, err)
		}
		if words := models.ChatCallWords(call); words != "[missed call]" {
			t.Errorf("a missed call reads %q", words)
		}

		call, err = tx.CreateChatCall(chat.ID, tobias.ID, "", []string{mira.ID})
		if err != nil {
			t.Fatalf("a call after one ended: %s", err)
		}
		if call, err = tx.DeclineChatCall(call.ID, mira.ID); err != nil || call.Member(mira.ID).RingState != models.ChatCallRingDeclined {
			t.Fatalf("DeclineChatCall: %+v %v", call, err)
		}
		if _, err := tx.EndChatCall(call.ID, models.ChatCallEndReason("hung up")); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a call ended for no known reason: %v", err)
		}
	})
}

// A transcript is asked for during a call, by the first to ask; it waits
// once an answered call ends, is claimed by one server, becomes a recording
// once, and ends once.
func TestACallsTranscriptIsAskedAndKept(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mira := newCircleTestUser(t, tx, "transcript-mira")
		tobias := newCircleTestUser(t, tx, "transcript-tobias")
		chat, err := tx.OpenDirectChat(mira.ID, tobias.ID)
		if err != nil {
			t.Fatalf("OpenDirectChat: %s", err)
		}
		call, err := tx.CreateChatCall(chat.ID, mira.ID, "", []string{tobias.ID})
		if err != nil {
			t.Fatalf("CreateChatCall: %s", err)
		}
		if call, err = tx.JoinChatCall(call.ID, tobias.ID); err != nil {
			t.Fatalf("JoinChatCall: %s", err)
		}
		if call, err = tx.AskChatCallTranscription(call.ID, mira.ID); err != nil || call.TranscriptionAskedByUserID != mira.ID {
			t.Fatalf("AskChatCallTranscription: %+v %v", call, err)
		}
		if call, err = tx.AskChatCallTranscription(call.ID, tobias.ID); err != nil || call.TranscriptionAskedByUserID != mira.ID {
			t.Errorf("a second ask took it over: %+v %v", call, err)
		}
		if call, err = tx.EndChatCall(call.ID, models.ChatCallEnded); err != nil || call.TranscriptState != models.ChatCallTranscriptWaiting {
			t.Fatalf("an answered call asked to be transcribed ends waiting: %+v %v", call, err)
		}
		if waiting, err := tx.ListWaitingChatCallTranscriptIDs(time.Minute, 10); err != nil || len(waiting) != 1 || waiting[0] != call.ID {
			t.Errorf("waiting: %v %v", waiting, err)
		}
		if claimed, err := tx.ClaimChatCallTranscript(call.ID, time.Minute); err != nil || claimed == nil {
			t.Fatalf("ClaimChatCallTranscript: %v %v", claimed, err)
		}
		if again, err := tx.ClaimChatCallTranscript(call.ID, time.Minute); err != nil || again != nil {
			t.Errorf("claimed twice: %v %v", again, err)
		}
		// The call becomes a recording owned by whoever asked, once.
		source, err := tx.CreateRecording(&models.Recording{UserID: mira.ID, ClientRecordingID: "call-" + call.ID, RecordingOrigin: models.RecordingOriginAudioImport, ChatCallID: call.ID})
		if err != nil || source.ChatCallID != call.ID {
			t.Fatalf("CreateRecording: %+v %v", source, err)
		}
		if recorded, err := tx.SetChatCallRecording(call.ID, source.ID); err != nil || recorded == nil || recorded.RecordingID != source.ID {
			t.Fatalf("SetChatCallRecording: %+v %v", recorded, err)
		}
		if again, err := tx.SetChatCallRecording(call.ID, source.ID); err != nil || again != nil {
			t.Errorf("a second recording was set: %+v %v", again, err)
		}
		if finished, err := tx.FinishChatCallTranscript(call.ID, models.ChatCallTranscriptDone, ""); err != nil || finished.TranscriptState != models.ChatCallTranscriptDone || finished.RecordingID != source.ID {
			t.Fatalf("FinishChatCallTranscript: %+v %v", finished, err)
		}
		if again, err := tx.FinishChatCallTranscript(call.ID, models.ChatCallTranscriptDone, ""); err != nil || again != nil {
			t.Errorf("finished twice: %+v %v", again, err)
		}
		if words := models.ChatCallWords(call); words == "" {
			t.Error("no words for a transcribed call")
		}
	})
}
