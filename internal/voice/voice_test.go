package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeProvider is a realtime transcription endpoint that accepts the
// session, keeps the audio it is sent, and says what the test scripts.
type fakeProvider struct {
	mutex         sync.Mutex
	authorization string
	query         string
	configured    map[string]any
	audioBytes    int
	script        func(conn *websocket.Conn)
}

func (self *fakeProvider) serve(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		self.mutex.Lock()
		self.authorization = request.Header.Get("Authorization")
		self.query = request.URL.Path + "?" + request.URL.RawQuery
		self.mutex.Unlock()
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var configure map[string]any
		if err := conn.ReadJSON(&configure); err != nil {
			return
		}
		self.mutex.Lock()
		self.configured = configure
		self.mutex.Unlock()
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		go func() {
			for {
				var message map[string]any
				if err := conn.ReadJSON(&message); err != nil {
					return
				}
				if message["type"] == "input_audio_buffer.append" {
					decoded, _ := base64.StdEncoding.DecodeString(message["audio"].(string))
					self.mutex.Lock()
					self.audioBytes += len(decoded)
					self.mutex.Unlock()
				}
			}
		}()
		self.script(conn)
		time.Sleep(100 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	t.Cleanup(server.Close)
	return server
}

// Two utterances, the second finished first: the drawer hears the speech
// boundaries and the words as they come, and the two final transcripts in
// the order they were spoken, each once, with what each cost.
func TestASessionTellsUtterancesInTheOrderSpoken(t *testing.T) {
	provider := &fakeProvider{script: func(conn *websocket.Conn) {
		for _, event := range []map[string]any{
			{"type": "input_audio_buffer.speech_started", "item_id": "first"},
			{"type": "input_audio_buffer.speech_stopped", "item_id": "first"},
			{"type": "input_audio_buffer.committed", "item_id": "first", "previous_item_id": nil},
			{"type": "input_audio_buffer.speech_started", "item_id": "second"},
			{"type": "conversation.item.input_audio_transcription.delta", "item_id": "first", "delta": "What is"},
			{"type": "input_audio_buffer.committed", "item_id": "second", "previous_item_id": "first"},
			{"type": "conversation.item.input_audio_transcription.completed", "item_id": "second", "transcript": " Also the plumber. ", "usage": map[string]any{"input_tokens": 20, "output_tokens": 5}},
			{"type": "conversation.item.input_audio_transcription.completed", "item_id": "first", "transcript": "What is on tomorrow?", "usage": map[string]any{"input_tokens": 30, "output_tokens": 8}},
			// Said again: told once, and paid for once.
			{"type": "conversation.item.input_audio_transcription.completed", "item_id": "first", "transcript": "What is on tomorrow?", "usage": map[string]any{"input_tokens": 30, "output_tokens": 8}},
		} {
			_ = conn.WriteJSON(event)
		}
	}}
	server := provider.serve(t)
	session, err := Open(context.Background(), &Settings{BaseURL: "http" + strings.TrimPrefix(server.URL, "http") + "/v1", APIKey: "test-key", TranscriptionModel: "gpt-4o-transcribe", TranscriptionPrompt: "A person talking to their agent, Bertie.", SilenceMS: 500})
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	if err := session.Append(make([]byte, 4800)); err != nil {
		t.Fatalf("Append: %s", err)
	}
	if err := session.Append(make([]byte, 3)); err == nil {
		t.Fatal("an odd number of bytes is not 16-bit audio")
	}
	var told []*Event
	var costs []*Usage
	if err := session.Read(func(event *Event) { told = append(told, event) }, func(usage *Usage) { costs = append(costs, usage) }); err != nil {
		t.Fatalf("Read: %s", err)
	}
	var said []string
	for _, event := range told {
		said = append(said, event.VoiceEvent+":"+event.UtteranceID+":"+event.TranscriptText)
	}
	want := []string{
		"speechStarted:first:", "speechStopped:first:", "speechStarted:second:", "transcriptDelta:first:What is",
		"transcriptFinal:first:What is on tomorrow?", "transcriptFinal:second:Also the plumber.",
	}
	if strings.Join(said, "|") != strings.Join(want, "|") {
		t.Fatalf("told\n%s\nwant\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}
	if told[4].UtteranceSequence != 1 || told[5].UtteranceSequence != 2 {
		t.Fatalf("numbered in the order spoken: %+v %+v", told[4], told[5])
	}
	if len(costs) != 2 || costs[0].InputTokens != 20 || costs[1].InputTokens != 30 {
		t.Fatalf("each transcription's cost: %+v", costs)
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	if provider.authorization != "Bearer test-key" || provider.query != "/v1/realtime?intent=transcription" {
		t.Fatalf("reached as %q at %q", provider.authorization, provider.query)
	}
	encoded, _ := json.Marshal(provider.configured)
	for _, fragment := range []string{`"type":"transcription"`, `"rate":24000`, `"model":"gpt-4o-transcribe"`, `"prompt":"A person talking to their agent, Bertie."`, `"type":"server_vad"`, `"silence_duration_ms":500`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Errorf("the session is configured with %s: %s", fragment, encoded)
		}
	}
	if provider.audioBytes != 4800 {
		t.Errorf("the audio reached the provider: %d bytes", provider.audioBytes)
	}
}

// A provider that refuses the configuration is said as the error of Open.
func TestARefusedSessionSaysWhy(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var configure map[string]any
		_ = conn.ReadJSON(&configure)
		_ = conn.WriteJSON(map[string]any{"type": "error", "error": map[string]any{"message": "model not supported in transcription sessions"}})
	}))
	defer server.Close()
	_, err := Open(context.Background(), &Settings{BaseURL: server.URL, APIKey: "k", TranscriptionModel: "something"})
	if err == nil || !strings.Contains(err.Error(), "model not supported") {
		t.Fatalf("refused: %v", err)
	}
}

// A failure holds its place in the order, and a later utterance waits for
// it; an utterance said to be committed twice is placed once.
func TestTheOrderHoldsAFailureInPlace(t *testing.T) {
	var order utteranceOrder
	var told []*Event
	for _, event := range []providerEvent{
		{Type: "input_audio_buffer.committed", ItemID: "a"},
		{Type: "input_audio_buffer.committed", ItemID: "b", PreviousItemID: "a"},
		{Type: "input_audio_buffer.committed", ItemID: "b", PreviousItemID: "a"},
		{Type: "conversation.item.input_audio_transcription.completed", ItemID: "b", Transcript: "second"},
		{Type: "conversation.item.input_audio_transcription.failed", ItemID: "a"},
		{Type: "conversation.item.input_audio_transcription.completed", ItemID: "c", Transcript: "third, finished before it was committed"},
		{Type: "input_audio_buffer.committed", ItemID: "c", PreviousItemID: "b"},
	} {
		told = append(told, order.translate(&event)...)
	}
	if len(told) != 3 || told[0].VoiceEvent != EventTranscriptFailed || told[0].UtteranceID != "a" ||
		told[1].UtteranceID != "b" || told[1].UtteranceSequence != 2 || told[2].UtteranceID != "c" || told[2].UtteranceSequence != 3 {
		t.Fatalf("told %+v", told)
	}
}

func TestTheRealtimeAddressFollowsTheProvidersAddress(t *testing.T) {
	for base, want := range map[string]string{
		"":                           "wss://api.openai.com/v1/realtime?intent=transcription",
		"https://api.openai.com/v1/": "wss://api.openai.com/v1/realtime?intent=transcription",
		"http://localhost:8080/v1":   "ws://localhost:8080/v1/realtime?intent=transcription",
	} {
		if got, err := realtimeURL(base); err != nil || got != want {
			t.Errorf("%q: %q %v", base, got, err)
		}
	}
	if _, err := realtimeURL("ftp://example.com"); err == nil {
		t.Error("an address that is not http is refused")
	}
}
