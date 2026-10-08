package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
)

// A page of this server, signed in, opens the voice socket: it is told the
// session is ready, its audio reaches the provider, what the provider hears
// comes back in the order spoken, and the transcription is paid for from
// the agent's day. A page of another site, or one not signed in, is
// refused, and with voice off there is no socket at all.
func TestTheVoiceSocketTranscribesForThePersonsAgent(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	var agentId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "speaker", Name: "Example Speaker"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		grantMCPPermissions(t, tx, person, models.PermissionAgentUse)
		created, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		agentId = created.ID
	})

	// The provider: accepts the session, and once audio arrives says it
	// heard one utterance.
	var configured map[string]any
	var spoken map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Its text-to-speech: the audio in two pieces, then what it cost.
		if strings.HasSuffix(request.URL.Path, "/audio/speech") {
			_ = json.NewDecoder(request.Body).Decode(&spoken)
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, line := range []string{
				`{"type":"speech.audio.delta","audio":"AQACAA=="}`,
				`{"type":"speech.audio.delta","audio":"AwAEAA=="}`,
				`{"type":"speech.audio.done","usage":{"input_tokens":12,"output_tokens":30}}`,
			} {
				_, _ = writer.Write([]byte("data: " + line + "\n\n"))
			}
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.ReadJSON(&configured)
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		var appended map[string]any
		if err := conn.ReadJSON(&appended); err != nil || appended["type"] != "input_audio_buffer.append" {
			return
		}
		for _, event := range []map[string]any{
			{"type": "input_audio_buffer.speech_started", "item_id": "u1"},
			{"type": "input_audio_buffer.committed", "item_id": "u1"},
			{"type": "conversation.item.input_audio_transcription.completed", "item_id": "u1", "transcript": "What is on tomorrow?", "usage": map[string]any{"input_tokens": 40, "output_tokens": 9}},
		} {
			_ = conn.WriteJSON(event)
		}
		var ignored map[string]any
		_ = conn.ReadJSON(&ignored)
	}))
	defer provider.Close()

	serve := func(isVoiceEnabled bool) *httptest.Server {
		configuration := config.Default()
		configuration.Agent.Enabled = true
		configuration.Agent.Providers = []config.AgentProvider{{Name: "spoken", Kind: config.AgentProviderKindOpenAI, BaseURL: provider.URL + "/v1", APIKey: "test-key"}}
		configuration.Agent.Voice = config.AgentVoice{Enabled: isVoiceEnabled}
		store := config.NewMemoryStore(configuration)
		worker := agentpackage.New(&agentpackage.Settings{
			Database: database, Configuration: func() *config.Configuration { return store.Current() }, Instance: "test", Tick: time.Hour,
		})
		resolver := &graph{database: database, config: store, settings: &api.Settings{Agent: worker}}
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			// What the authentication middleware does for a session cookie.
			if request.Header.Get("X-Test-Signed-In") != "" {
				request.Header.Set(api.AuthenticatedUsernameHeader, "speaker")
			}
			resolver.voiceView(writer, request)
		}))
		t.Cleanup(server.Close)
		return server
	}
	var server *httptest.Server
	dial := func(origin string, isSignedIn bool) (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		if origin != "" {
			header.Set("Origin", origin)
		}
		if isSignedIn {
			header.Set("X-Test-Signed-In", "yes")
		}
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	}

	// Off: no socket.
	server = serve(false)
	if _, response, err := dial(server.URL, true); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("voice off is refused before the socket opens: %v", err)
	}
	server = serve(true)

	refusal := func(origin string, isSignedIn bool) string {
		t.Helper()
		conn, _, err := dial(origin, isSignedIn)
		if err != nil {
			return "no socket: " + err.Error()
		}
		defer func() { _ = conn.Close() }()
		_ = conn.WriteJSON(map[string]any{"voiceEvent": "hello"})
		var said voice.Event
		if err := conn.ReadJSON(&said); err != nil {
			return "closed: " + err.Error()
		}
		return said.VoiceEvent + ": " + said.ErrorMessage
	}
	if said := refusal(server.URL, false); !strings.Contains(said, "refused: sign in first") {
		t.Fatalf("not signed in: %s", said)
	}
	if said := refusal("https://elsewhere.example", true); !strings.HasPrefix(said, "no socket") {
		t.Fatalf("a page of another site: %s", said)
	}

	conn, _, err := dial(server.URL, true)
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.WriteJSON(map[string]any{"voiceEvent": "hello"})
	var ready voice.Event
	if err := conn.ReadJSON(&ready); err != nil || ready.VoiceEvent != voice.EventReady || ready.SampleRate != voice.SampleRate {
		t.Fatalf("ready: %+v %v", ready, err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 4800)); err != nil {
		t.Fatalf("audio: %s", err)
	}
	var heard []string
	for len(heard) < 2 {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var said voice.Event
		if err := conn.ReadJSON(&said); err != nil {
			t.Fatalf("heard %v, then %s", heard, err)
		}
		heard = append(heard, said.VoiceEvent+":"+said.TranscriptText)
	}
	if strings.Join(heard, "|") != "speechStarted:|transcriptFinal:What is on tomorrow?" {
		t.Fatalf("heard %v", heard)
	}

	// An answer read aloud: its audio comes back in pieces, then done.
	_ = conn.WriteJSON(map[string]any{"voiceEvent": "speakAnswer", "answerSegmentId": "answer-1", "answerText": "You have a dentist at nine."})
	var audio []byte
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var said voice.Event
		if err := conn.ReadJSON(&said); err != nil {
			t.Fatalf("spoken %v, then %s", audio, err)
		}
		if said.AnswerSegmentID != "answer-1" {
			t.Fatalf("a piece of another answer: %+v", said)
		}
		if said.VoiceEvent == voice.EventAnswerAudioDone {
			break
		}
		if said.VoiceEvent != voice.EventAnswerAudio {
			t.Fatalf("speaking: %+v", said)
		}
		audio = append(audio, said.AnswerAudio...)
	}
	if string(audio) != "\x01\x00\x02\x00\x03\x00\x04\x00" {
		t.Fatalf("the answer's audio: %v", audio)
	}
	if spoken["model"] != config.VoiceSpeechModelDefault || spoken["voice"] != config.VoiceSpeechVoiceDefault || spoken["input"] != "You have a dentist at nine." || spoken["response_format"] != "pcm" {
		t.Fatalf("what the provider was asked to speak: %v", spoken)
	}
	_ = conn.WriteJSON(map[string]any{"voiceEvent": "stop"})

	transcription := configured["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)["transcription"].(map[string]any)
	if transcription["model"] != config.VoiceTranscriptionModelDefault || !strings.Contains(transcription["prompt"].(string), "Bertie") {
		t.Fatalf("the session names the agent to the transcription: %v", transcription)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var totals models.AgentUsageTotals
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			totals, err = tx.SumAgentUsage(agentId, time.Now().Add(-time.Hour))
		})
		if err == nil && totals.PromptTokens == 52 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the transcription and the speech are paid for: %+v %v", totals, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
