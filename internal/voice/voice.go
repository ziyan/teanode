// Package voice is a person talking to their agent: the audio they speak,
// streamed to a provider's realtime transcription as they speak it, and
// what the provider hears, said back in TeaNode's own terms.
//
// The provider detects speech: it says when somebody starts and stops
// talking, and commits each utterance itself. What comes back is said as
// events of this package (Event), which the drawer reads off its socket:
// speech started and stopped, the words of an utterance as they are
// recognized, and each utterance's final transcript, released in the order
// the utterances were spoken even when the provider finishes them out of
// order. Nothing here starts a turn: the drawer sends a final transcript
// the way it sends what the person typed.
package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("voice")

// SampleRate is the audio this package takes: mono, 16-bit little-endian
// PCM, 24,000 samples a second.
const SampleRate = 24000

// MaximumFrameBytes bounds one frame of audio a caller hands over: a
// little over a second of it.
const MaximumFrameBytes = 64 << 10

// The events said to the drawer, by their voiceEvent.
const (
	EventReady            = "ready"
	EventRefused          = "refused"
	EventSpeechStarted    = "speechStarted"
	EventSpeechStopped    = "speechStopped"
	EventTranscriptDelta  = "transcriptDelta"
	EventTranscriptFinal  = "transcriptFinal"
	EventTranscriptFailed = "transcriptFailed"
	EventError            = "error"
	// EventProblem is something to tell the person that does not end the
	// call.
	EventProblem = "problem"

	// A spoken answer's audio, a piece at a time; that it is all there;
	// that it could not be spoken.
	EventAnswerAudio       = "answerAudio"
	EventAnswerAudioDone   = "answerAudioDone"
	EventAnswerAudioFailed = "answerAudioFailed"
)

// Event is one thing the drawer is told.
type Event struct {
	VoiceEvent string `json:"voiceEvent"`

	// UtteranceID is the utterance it is about, the provider's own id for
	// it; UtteranceSequence counts final transcripts and failures from 1,
	// in the order the utterances were spoken.
	UtteranceID       string `json:"utteranceId,omitempty"`
	UtteranceSequence int    `json:"utteranceSequence,omitempty"`

	// TranscriptText is the words: the next few of an utterance being
	// heard, or the whole of a finished one, empty when nothing was said.
	TranscriptText string `json:"transcriptText,omitempty"`

	// UtteranceStartMS and UtteranceEndMS are where the provider heard the
	// utterance begin and end, in milliseconds from the start of the audio
	// the session was given. Zero when the provider did not say; the start
	// is on speechStarted and the end on speechStopped, and a final
	// transcript has whichever were said.
	UtteranceStartMS int64 `json:"utteranceStartMS,omitempty"`
	UtteranceEndMS   int64 `json:"utteranceEndMS,omitempty"`

	ErrorMessage string `json:"errorMessage,omitempty"`

	// SampleRate is the audio the session takes, on ready.
	SampleRate int `json:"sampleRate,omitempty"`

	// AnswerSegmentID is the piece of a spoken answer the drawer asked for,
	// in its own name; AnswerAudio some of its audio, mono 16-bit PCM at
	// SampleRate.
	AnswerSegmentID string `json:"answerSegmentId,omitempty"`
	AnswerAudio     []byte `json:"answerAudio,omitempty"`
}

// Settings are how a session reaches its provider and what it asks for.
type Settings struct {
	// BaseURL is the provider's API address, https://api.openai.com/v1
	// when empty; APIKey its key.
	BaseURL string
	APIKey  string

	// TranscriptionModel is the model; TranscriptionPrompt what the
	// recording is about, which helps with names; SilenceMS the pause that
	// ends an utterance, or zero for the provider to judge from what was
	// said whether the person has finished, so that a pause after a name
	// or in the middle of a thought does not end it.
	TranscriptionModel  string
	TranscriptionPrompt string
	SilenceMS           int

	// Dialer reaches the provider; the default one when nil.
	Dialer *websocket.Dialer
}

// Usage is what one finished transcription cost.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// configureWait is how long the provider has to accept the session.
const configureWait = 15 * time.Second

// Session is one provider transcription session.
type Session struct {
	conn       *websocket.Conn
	writeMutex sync.Mutex
	order      utteranceOrder

	// appendedBytes is the audio handed to the provider so far, which dates
	// an utterance the provider gave no times for.
	appendedBytes atomic.Int64
}

// realtimeURL is the provider's realtime address for a transcription
// session, from its API address.
func realtimeURL(baseURL string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("voice: the provider's address %q does not read: %w", baseURL, err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("voice: the provider's address %q is not http or https", baseURL)
	}
	parsed.Path += "/realtime"
	parsed.RawQuery = url.Values{"intent": {"transcription"}}.Encode()
	return parsed.String(), nil
}

// Open dials the provider and configures the session, answering once the
// provider has accepted the configuration.
func Open(ctx context.Context, settings *Settings) (*Session, error) {
	address, err := realtimeURL(settings.BaseURL)
	if err != nil {
		return nil, err
	}
	dialer := settings.Dialer
	if dialer == nil {
		dialer = &websocket.Dialer{HandshakeTimeout: configureWait, Proxy: http.ProxyFromEnvironment}
	}
	header := http.Header{"Authorization": {"Bearer " + settings.APIKey}}
	conn, response, err := dialer.DialContext(ctx, address, header)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("voice: the provider refused the session (%s)", response.Status)
		}
		return nil, fmt.Errorf("voice: the provider could not be reached: %w", err)
	}
	session := &Session{conn: conn}
	isJudgingFromWords := settings.SilenceMS <= 0
	// Why the provider refused the first settings, said if the fallback
	// fails too: the first refusal is the one that explains.
	firstRefusal := ""
	if err := session.send(configuration(settings, turnDetection(settings.SilenceMS))); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(configureWait))
	for {
		var event providerEvent
		if err := conn.ReadJSON(&event); err != nil {
			_ = conn.Close()
			if firstRefusal != "" {
				return nil, fmt.Errorf("voice: the provider refused the session: %s", firstRefusal)
			}
			return nil, fmt.Errorf("voice: the provider did not accept the session: %w", err)
		}
		switch event.Type {
		case "session.updated":
			_ = conn.SetReadDeadline(time.Time{})
			return session, nil
		case "error":
			// A provider that cannot judge from the words still ends a
			// turn on a pause: the call goes on with that.
			if isJudgingFromWords {
				isJudgingFromWords = false
				firstRefusal = event.errorMessage()
				// Said once a call, so an operator learns that calls end
				// a turn on a pause rather than on the words, and why.
				log.Noticef("the provider cannot judge from the words when somebody has finished, so this call ends a turn after a %d ms pause: %s", fallbackSilenceMS, firstRefusal)
				if err := session.send(configuration(settings, turnDetection(fallbackSilenceMS))); err != nil {
					_ = conn.Close()
					return nil, err
				}
				continue
			}
			_ = conn.Close()
			return nil, fmt.Errorf("voice: the provider refused the session: %s", event.errorMessage())
		}
	}
}

// fallbackSilenceMS is the pause that ends a turn when the provider cannot
// judge from the words.
const fallbackSilenceMS = 500

// turnDetection is how the provider tells that somebody has finished: from
// what they said when silenceMS is zero, otherwise after a pause that long.
// Judging from the words, "medium" waits at most four seconds on somebody
// who seems to be in the middle of a sentence, less than the six seconds of
// silence a muted drawer sends, so muting still ends what was being said.
func turnDetection(silenceMS int) map[string]any {
	if silenceMS <= 0 {
		return map[string]any{"type": "semantic_vad", "eagerness": "medium"}
	}
	return map[string]any{"type": "server_vad", "threshold": 0.5, "prefix_padding_ms": 300, "silence_duration_ms": silenceMS}
}

// configuration is the session's settings as the provider takes them.
func configuration(settings *Settings, detection map[string]any) map[string]any {
	transcription := map[string]any{"model": settings.TranscriptionModel}
	if prompt := strings.TrimSpace(settings.TranscriptionPrompt); prompt != "" {
		transcription["prompt"] = prompt
	}
	return map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format":        map[string]any{"type": "audio/pcm", "rate": SampleRate},
					"transcription": transcription,
					// The provider says when somebody starts and stops
					// talking, and commits each utterance itself.
					"turn_detection":  detection,
					"noise_reduction": map[string]any{"type": "near_field"},
				},
			},
		},
	}
}

// Append sends one frame of audio.
func (self *Session) Append(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm) > MaximumFrameBytes || len(pcm)%2 != 0 {
		return fmt.Errorf("voice: a frame of %d bytes is not 16-bit audio of at most %d bytes", len(pcm), MaximumFrameBytes)
	}
	if err := self.send(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)}); err != nil {
		return err
	}
	self.appendedBytes.Add(int64(len(pcm)))
	return nil
}

// AppendedMS is how much audio the session has been given, in milliseconds
// of audio at SampleRate.
func (self *Session) AppendedMS() int64 {
	return self.appendedBytes.Load() * 1000 / (SampleRate * 2)
}

// Close ends the session.
func (self *Session) Close() error {
	self.writeMutex.Lock()
	_ = self.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	self.writeMutex.Unlock()
	return self.conn.Close()
}

func (self *Session) send(message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if err := self.conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
		return fmt.Errorf("voice: the provider could not be written to: %w", err)
	}
	return nil
}

// Read reads the provider's events until the session ends, telling emit
// each event for the drawer and used what each transcription cost. It
// answers nil when the session was closed, and the error otherwise.
func (self *Session) Read(emit func(*Event), used func(*Usage)) error {
	for {
		_, data, err := self.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, websocket.ErrCloseSent) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("voice: the provider's session ended: %w", err)
		}
		var event providerEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		for _, said := range self.order.translate(&event) {
			emit(said)
		}
		// Each utterance is paid for once, whatever the provider repeats.
		if event.Type == "conversation.item.input_audio_transcription.completed" && event.Usage != nil && used != nil && !self.order.charged[event.ItemID] {
			if self.order.charged == nil {
				self.order.charged = map[string]bool{}
			}
			self.order.charged[event.ItemID] = true
			used(&Usage{InputTokens: event.Usage.InputTokens, OutputTokens: event.Usage.OutputTokens})
		}
	}
}

// providerEvent is the part of a provider's event this package reads.
type providerEvent struct {
	Type           string `json:"type"`
	ItemID         string `json:"item_id"`
	AudioStartMS   int64  `json:"audio_start_ms"`
	AudioEndMS     int64  `json:"audio_end_ms"`
	PreviousItemID string `json:"previous_item_id"`
	Delta          string `json:"delta"`
	Transcript     string `json:"transcript"`
	Error          *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (self *providerEvent) errorMessage() string {
	if self.Error == nil || strings.TrimSpace(self.Error.Message) == "" {
		return "no reason given"
	}
	return self.Error.Message
}

// utteranceOrder turns the provider's events into the drawer's, holding
// each finished transcript until every utterance spoken before it is
// finished too: the provider commits utterances in the order they were
// spoken but may finish transcribing them in any order.
type utteranceOrder struct {
	// committed is the utterances not yet released, in the order spoken.
	committed []string

	// finished is what each finished utterance came to, by its id.
	finished map[string]*Event

	// released are the utterances already told, which a repeated event
	// does not tell again; sequence counts them.
	released map[string]bool
	sequence int

	// charged are the utterances whose transcription cost has been told.
	charged map[string]bool

	// startedMS and stoppedMS are where the provider heard each utterance
	// begin and end, by its id, until it is released.
	startedMS map[string]int64
	stoppedMS map[string]int64
}

func (self *utteranceOrder) translate(event *providerEvent) []*Event {
	if self.finished == nil {
		self.finished = map[string]*Event{}
		self.released = map[string]bool{}
		self.startedMS = map[string]int64{}
		self.stoppedMS = map[string]int64{}
	}
	switch event.Type {
	case "input_audio_buffer.speech_started":
		self.startedMS[event.ItemID] = event.AudioStartMS
		return []*Event{{VoiceEvent: EventSpeechStarted, UtteranceID: event.ItemID, UtteranceStartMS: event.AudioStartMS}}
	case "input_audio_buffer.speech_stopped":
		self.stoppedMS[event.ItemID] = event.AudioEndMS
		return []*Event{{VoiceEvent: EventSpeechStopped, UtteranceID: event.ItemID, UtteranceEndMS: event.AudioEndMS}}
	case "input_audio_buffer.committed":
		self.commit(event.ItemID, event.PreviousItemID)
		return self.release()
	case "conversation.item.input_audio_transcription.delta":
		if event.Delta == "" || self.released[event.ItemID] {
			return nil
		}
		return []*Event{{VoiceEvent: EventTranscriptDelta, UtteranceID: event.ItemID, TranscriptText: event.Delta}}
	case "conversation.item.input_audio_transcription.completed":
		if self.released[event.ItemID] {
			return nil
		}
		self.finished[event.ItemID] = &Event{VoiceEvent: EventTranscriptFinal, UtteranceID: event.ItemID, TranscriptText: strings.TrimSpace(event.Transcript)}
		return self.release()
	case "conversation.item.input_audio_transcription.failed":
		if self.released[event.ItemID] {
			return nil
		}
		self.finished[event.ItemID] = &Event{VoiceEvent: EventTranscriptFailed, UtteranceID: event.ItemID, ErrorMessage: event.errorMessage()}
		return self.release()
	case "error":
		// Something the provider did not like, which it goes on after;
		// what ends the session ends the connection, and Read says so.
		return []*Event{{VoiceEvent: EventProblem, ErrorMessage: event.errorMessage()}}
	}
	return nil
}

// commit places an utterance after the one the provider says came before
// it, or at the end.
func (self *utteranceOrder) commit(itemId, previousItemId string) {
	if itemId == "" || self.released[itemId] {
		return
	}
	for _, already := range self.committed {
		if already == itemId {
			return
		}
	}
	position := len(self.committed)
	for index, already := range self.committed {
		if already == previousItemId {
			position = index + 1
		}
	}
	self.committed = append(self.committed, "")
	copy(self.committed[position+1:], self.committed[position:])
	self.committed[position] = itemId
}

// release tells every finished utterance at the front of the order.
func (self *utteranceOrder) release() []*Event {
	var told []*Event
	for len(self.committed) > 0 {
		next := self.finished[self.committed[0]]
		if next == nil {
			break
		}
		self.sequence++
		next.UtteranceSequence = self.sequence
		next.UtteranceStartMS = self.startedMS[self.committed[0]]
		next.UtteranceEndMS = self.stoppedMS[self.committed[0]]
		delete(self.startedMS, self.committed[0])
		delete(self.stoppedMS, self.committed[0])
		told = append(told, next)
		self.released[self.committed[0]] = true
		delete(self.finished, self.committed[0])
		self.committed = self.committed[1:]
	}
	return told
}
