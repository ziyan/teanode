package apigraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
)

// The cleaned words are valid text without control characters, bounded.
func TestTheProvidersWordsAreCleanedBeforeTheyAreKept(t *testing.T) {
	if got := recordingLiveCleanText("  Hello\x07 there \nfriend \xff ", false); got != "Hello there friend" {
		t.Fatalf("cleaned: %q", got)
	}
	if got := recordingLiveCleanText(" next ", true); got != " next " {
		t.Fatalf("a part of an utterance keeps its spaces: %q", got)
	}
	long := recordingLiveCleanText(strings.Repeat("é", recordingLiveTextBytesMaximum), false)
	if len(long) > recordingLiveTextBytesMaximum || strings.ContainsRune(long, '�') {
		t.Fatalf("bounded on a character: %d bytes", len(long))
	}
}

// Times the provider gives are placed on the recording's timeline; times it
// does not give, or gives nonsense, come from the audio it was sent.
func TestLiveTimesAreBoundedByTheAudioSent(t *testing.T) {
	for _, example := range []struct {
		startMS, endMS, previousEndMS, appendedMS, streamStartMS int64
		wantStartMS, wantEndMS                                   int64
	}{
		{500, 2500, 0, 4000, 60000, 60500, 62500},
		{0, 0, 1200, 4000, 60000, 61200, 64000},
		{3000, 2000, 0, 4000, 0, 3000, 4000},
		{1000, 9000000, 0, 4000, 0, 1000, 9000},
	} {
		startMS, endMS := recordingLiveTimes(example.startMS, example.endMS, example.previousEndMS, example.appendedMS, example.streamStartMS)
		if startMS != example.wantStartMS || endMS != example.wantEndMS {
			t.Errorf("%+v: %d to %d", example, startMS, endMS)
		}
	}
}

// A capture's owner streams audio and reads back words on the recording's
// timeline; the words are kept for the timeline, and paid for from the
// agent's day. Anyone else, a stopped capture, a server with the setting
// off and an agent out of budget are refused.
func TestTheRecordingSocketTranscribesALiveCaptureForItsOwner(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	var agentId, ownerId, captureId, stoppedId, strangersId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "recorder", Name: "Example Recorder"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		grantMCPPermissions(t, tx, person, models.PermissionAgentUse)
		created, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId, ownerId = created.ID, person.ID
		stranger, err := tx.CreateUser(&models.User{Username: "stranger", Name: "Example Stranger"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		for index, userId := range []string{person.ID, person.ID, stranger.ID} {
			recording, err := tx.CreateRecording(&models.Recording{UserID: userId, ClientRecordingID: "capture-" + string(rune('a'+index)), RecordingOrigin: models.RecordingOriginCapture})
			if err != nil {
				t.Fatalf("CreateRecording: %s", err)
			}
			switch index {
			case 0:
				captureId = recording.ID
			case 1:
				stoppedId = recording.ID
			default:
				strangersId = recording.ID
			}
		}
	})
	dbtest.Exec(t, database, "UPDATE recording SET recording_capture_state = 'stopped' WHERE id = '"+stoppedId+"'")

	// The provider: once audio arrives it says it heard one utterance at 500
	// ms to 2,500 ms of the audio, with a control character in the words.
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var configured map[string]any
		_ = conn.ReadJSON(&configured)
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		var appended map[string]any
		if err := conn.ReadJSON(&appended); err != nil || appended["type"] != "input_audio_buffer.append" {
			return
		}
		for _, event := range []map[string]any{
			{"type": "input_audio_buffer.speech_started", "item_id": "u1", "audio_start_ms": 500},
			{"type": "input_audio_buffer.speech_stopped", "item_id": "u1", "audio_end_ms": 2500},
			{"type": "input_audio_buffer.committed", "item_id": "u1"},
			{"type": "conversation.item.input_audio_transcription.completed", "item_id": "u1", "transcript": " Shall we begin?\x07 ", "usage": map[string]any{"input_tokens": 40, "output_tokens": 9}},
		} {
			_ = conn.WriteJSON(event)
		}
		var ignored map[string]any
		_ = conn.ReadJSON(&ignored)
	}))
	defer provider.Close()

	serve := func(isLiveEnabled bool, dailyTokens int64) *httptest.Server {
		configuration := config.Default()
		configuration.Agent.Enabled = true
		configuration.Agent.Providers = []config.AgentProvider{{Name: "spoken", Kind: config.AgentProviderKindOpenAI, BaseURL: provider.URL + "/v1", APIKey: "test-key"}}
		configuration.Agent.Recording.IsLiveTranscriptionEnabled = &isLiveEnabled
		configuration.Agent.Limits.DailyTokensPerAgent = dailyTokens
		store := config.NewMemoryStore(configuration)
		worker := agentpackage.New(&agentpackage.Settings{
			Database: database, Configuration: func() *config.Configuration { return store.Current() }, Instance: "test", Tick: time.Hour,
		})
		resolver := &graph{database: database, config: store, settings: &api.Settings{Agent: worker}}
		router := mux.NewRouter()
		router.Path(api.PathRecordingLive).Methods(http.MethodGet).HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Header.Get("X-Test-Signed-In") != "" {
				request.Header.Set(api.AuthenticatedUsernameHeader, "recorder")
			}
			resolver.recordingLiveView(writer, request)
		})
		server := httptest.NewServer(router)
		t.Cleanup(server.Close)
		return server
	}
	dial := func(server *httptest.Server, recordingId string, isSignedIn bool) (*websocket.Conn, *http.Response, error) {
		header := http.Header{"Origin": {server.URL}}
		if isSignedIn {
			header.Set("X-Test-Signed-In", "yes")
		}
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/recordings/"+recordingId+"/live", header)
	}
	refusal := func(server *httptest.Server, recordingId string, isSignedIn bool) string {
		t.Helper()
		conn, _, err := dial(server, recordingId, isSignedIn)
		if err != nil {
			return "no socket: " + err.Error()
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteJSON(map[string]any{"voiceEvent": "hello", "streamStartMS": 0})
		var said voice.Event
		if err := conn.ReadJSON(&said); err != nil {
			return "closed: " + err.Error()
		}
		return said.VoiceEvent + ": " + said.ErrorMessage
	}

	off := serve(false, 0)
	if _, response, err := dial(off, captureId, true); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("the setting off is refused before the socket opens: %v", err)
	}
	server := serve(true, 0)
	if said := refusal(server, captureId, false); !strings.Contains(said, "refused: sign in first") {
		t.Fatalf("not signed in: %s", said)
	}
	if said := refusal(server, strangersId, true); !strings.Contains(said, "refused: that recording was not found") {
		t.Fatalf("somebody else's recording: %s", said)
	}
	if said := refusal(server, stoppedId, true); !strings.Contains(said, "refused: that recording is not being captured") {
		t.Fatalf("a stopped capture: %s", said)
	}

	conn, _, err := dial(server, captureId, true)
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.WriteJSON(map[string]any{"voiceEvent": "hello", "streamStartMS": 60000})
	var ready voice.Event
	if err := conn.ReadJSON(&ready); err != nil || ready.VoiceEvent != voice.EventReady {
		t.Fatalf("ready: %+v %v", ready, err)
	}
	// A frame past the limit ends the stream; ordinary ones, a second each, do not.
	for second := 0; second < 3; second++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 2*voice.SampleRate)); err != nil {
			t.Fatalf("audio: %s", err)
		}
	}
	var final voice.Event
	for index := 0; index < 3; index++ {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var said voice.Event
		if err := conn.ReadJSON(&said); err != nil {
			t.Fatalf("heard %d events, then %s", index, err)
		}
		final = said
	}
	if final.VoiceEvent != voice.EventTranscriptFinal || final.TranscriptText != "Shall we begin?" || final.UtteranceStartMS != 60500 || final.UtteranceEndMS != 62500 {
		t.Fatalf("on the recording's timeline: %+v", final)
	}
	_ = conn.WriteJSON(map[string]any{"voiceEvent": "stop"})

	deadline := time.Now().Add(5 * time.Second)
	for {
		var utterances []*models.RecordingLiveUtterance
		var totals models.AgentUsageTotals
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			utterances, err = tx.ListRecordingLiveUtterances(ownerId, captureId)
			totals, _ = tx.SumAgentUsage(agentId, time.Now().Add(-time.Hour))
		})
		if err == nil && len(utterances) == 1 && totals.PromptTokens == 40 {
			if utterances[0].TranscriptText != "Shall we begin?" || utterances[0].UtteranceStartMS != 60500 || utterances[0].UtteranceEndMS != 62500 {
				t.Fatalf("kept: %+v", utterances[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the words are kept and paid for: %+v %+v %v", utterances, totals, err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The agent's day is spent: no new stream.
	agentpackage.RecordUsage(database, agentId, "", "spoken:test", "voice", llm.Usage{PromptTokens: 1000})
	exhausted := serve(true, 100)
	if said := refusal(exhausted, captureId, true); !strings.HasPrefix(said, "refused: ") || strings.Contains(said, "ready") {
		t.Fatalf("an agent out of budget: %s", said)
	}
}
