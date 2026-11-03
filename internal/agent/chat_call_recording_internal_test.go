package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/call"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A call transcribed becomes a recording owned by whoever turned
// transcription on: one original for each person's stretch of microphone,
// at its offset from the answer, each naming its speaker; the call names the
// recording, and the files the media server kept are gone.
func TestATranscribedCallBecomesARecordingOfEachSpeaker(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	recordings := test.TempDir()
	recorder := call.NewRecorder(recordings)
	worker := New(&Settings{Database: database, Storage: store, CallRecorder: recorder, Configuration: func() *config.Configuration { return configuration }})

	miraId := dbtest.CreateUser(test, database, "call-recording-mira")
	tobiasId := dbtest.CreateUser(test, database, "call-recording-tobias")
	var chatCall *models.ChatCall
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		if _, err := tx.CreateAgent(&models.Agent{UserID: miraId, Name: "Mira's agent", Enabled: true}); err != nil {
			test.Fatal(err)
		}
		chat, err := tx.OpenDirectChat(miraId, tobiasId)
		if err != nil {
			test.Fatal(err)
		}
		if chatCall, err = tx.CreateChatCall(chat.ID, miraId, "", []string{tobiasId}); err != nil {
			test.Fatal(err)
		}
		if chatCall, err = tx.JoinChatCall(chatCall.ID, tobiasId); err != nil {
			test.Fatal(err)
		}
		if _, err = tx.AskChatCallTranscription(chatCall.ID, miraId); err != nil {
			test.Fatal(err)
		}
		if chatCall, err = tx.EndChatCall(chatCall.ID, models.ChatCallEnded); err != nil || chatCall.TranscriptState != models.ChatCallTranscriptWaiting {
			test.Fatalf("EndChatCall: %+v %v", chatCall, err)
		}
	})
	// What the media server kept: a stretch of each, Tobias two seconds in.
	directory := filepath.Join(recordings, chatCall.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		test.Fatal(err)
	}
	answeredAt := *chatCall.AnsweredAt
	for userId, startedAt := range map[string]time.Time{miraId: answeredAt, tobiasId: answeredAt.Add(2 * time.Second)} {
		path := filepath.Join(directory, fmt.Sprintf("%s-%d.ogg", userId, startedAt.UnixNano()))
		if err := os.WriteFile(path, []byte("OggS kept voice of "+userId), 0o600); err != nil {
			test.Fatal(err)
		}
	}

	if err := worker.TranscribeChatCall(test.Context(), chatCall.ID); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		recorded, err := tx.GetChatCall(chatCall.ID)
		if err != nil || recorded.RecordingID == "" || recorded.TranscriptState != models.ChatCallTranscriptWaiting {
			test.Fatalf("the call after its files were kept: %+v %v", recorded, err)
		}
		source, err := tx.GetRecording(miraId, recorded.RecordingID)
		if err != nil || source.ChatCallID != chatCall.ID {
			test.Fatalf("the recording: %+v %v", source, err)
		}
		media, err := tx.ListRecordingMedia(miraId, source.ID)
		if err != nil || len(media) != 2 {
			test.Fatalf("the originals: %+v %v", media, err)
		}
		offsetOf := map[string]int64{}
		for _, each := range media {
			if each.RecordingUploadState != models.RecordingUploadComplete || each.SpeakerName == "" {
				test.Errorf("an original is %+v", each)
			}
			offsetOf[each.SpeakerUserID] = each.TimelineStartMS
		}
		if offsetOf[miraId] != 0 || offsetOf[tobiasId] != 2000 {
			test.Errorf("the speakers' offsets are %v", offsetOf)
		}
	})
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		test.Errorf("the media server's files are still there: %v", err)
	}
	// A second look while the recording is transcribed changes nothing.
	if err := worker.TranscribeChatCall(test.Context(), chatCall.ID); err != nil {
		test.Fatal(err)
	}
}
