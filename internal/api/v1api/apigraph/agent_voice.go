package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
)

// Voice: a person talking to their agent in the drawer. The drawer streams
// the microphone over a websocket; this server streams it on to the
// provider's realtime transcription, which detects speech and ends each
// utterance itself, and says back what it heard (internal/voice). It also
// reads the answers aloud, a piece at a time as the drawer asks. It starts
// no turn: the drawer sends each final transcript as an ordinary turn, the
// way it sends what was typed, so a spoken turn is a typed one in
// everything the agent does with it.

// voiceSessionLongest bounds one session, which the drawer opens again.
const voiceSessionLongest = 30 * time.Minute

// answerSegmentsAtOnce bounds the pieces of an answer being spoken at
// once: the one playing and the next few, made while it plays.
const answerSegmentsAtOnce = 4

// voiceSilenceLongest is how long the drawer may send nothing at all; it
// sends audio continuously while listening, silence included.
const voiceSilenceLongest = time.Minute

// AgentVoiceQuery says whether the caller may talk to their agent.
type AgentVoiceQuery interface {
	// Whether the drawer offers the microphone: the operator turned voice
	// on with a provider to transcribe it, and the caller has an agent that
	// is on. Needs agent:use.
	ReadAgentVoice(ctx context.Context) (*AgentVoiceView, error)
}

// AgentVoiceView is what the drawer needs to offer voice.
type AgentVoiceView struct {
	IsVoiceAvailable bool `json:"isVoiceAvailable"`

	// SampleRate is the audio the socket takes: mono 16-bit PCM at this
	// many samples a second.
	SampleRate int `json:"sampleRate"`
}

func (self *graph) ReadAgentVoice(ctx context.Context) (*AgentVoiceView, error) {
	if _, _, err := self.requireAgentPerson(ctx); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	return &AgentVoiceView{
		IsVoiceAvailable: configuration.Agent.Voice.Enabled && configuration.Agent.VoiceProvider() != nil && self.agentWorker() != nil,
		SampleRate:       voice.SampleRate,
	}, nil
}

// voiceSocket is the drawer's websocket as both directions write to it.
type voiceSocket struct {
	conn  *websocket.Conn
	mutex sync.Mutex
}

func (self *voiceSocket) say(event *voice.Event) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	_ = self.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return self.conn.WriteJSON(event)
}

// voiceView is the drawer's voice websocket.
func (self *graph) voiceView(response http.ResponseWriter, request *http.Request) {
	configuration := self.config.Current()
	provider := configuration.Agent.VoiceProvider()
	if !configuration.Agent.Voice.Enabled || provider == nil || self.agentWorker() == nil {
		http.Error(response, "voice is off on this server", http.StatusForbidden)
		return
	}
	// The session cookie is taken only from a page of this server, which
	// the browser says in the Origin; a token in the hello is taken from
	// anywhere, as the GraphQL socket takes one.
	isFromThisServer := request.Header.Get("Origin") != "" && fromThisServer(request)
	sessionUsername := api.UsernameFromRequest(request)
	upgrader := websocket.Upgrader{CheckOrigin: fromThisServer}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(voice.MaximumFrameBytes + 1024)
	socket := &voiceSocket{conn: conn}
	refuse := func(reason string) {
		_ = socket.say(&voice.Event{VoiceEvent: voice.EventRefused, ErrorMessage: reason})
	}

	// Who this is comes with the first message.
	_ = conn.SetReadDeadline(time.Now().Add(helloWait))
	var hello struct {
		VoiceEvent    string `json:"voiceEvent"`
		Authorization string `json:"authorization"`

		// CaptureSettings is what the browser says it granted for the
		// microphone, in the browser's own names: whether echo
		// cancellation is on is what decides whether a spoken answer will
		// be heard back as the person talking.
		CaptureSettings struct {
			EchoCancellation any `json:"echoCancellation"`
			NoiseSuppression any `json:"noiseSuppression"`
			AutoGainControl  any `json:"autoGainControl"`
			SampleRate       any `json:"sampleRate"`
		} `json:"captureSettings"`
	}
	if messageType, data, err := conn.ReadMessage(); err != nil || messageType != websocket.TextMessage || json.Unmarshal(data, &hello) != nil || hello.VoiceEvent != "hello" {
		refuse("the first message says hello")
		return
	}
	username := ""
	switch {
	case strings.TrimSpace(hello.Authorization) != "":
		username = self.usernameOfToken(request, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(hello.Authorization), "Bearer ")))
	case isFromThisServer:
		username = sessionUsername
	}
	if username == "" {
		refuse("sign in first")
		return
	}
	found, err := self.agentOfPerson(username)
	if err != nil {
		refuse(err.Error())
		return
	}
	var owner *models.User
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		if owner, err = tx.GetUserByUsername(username); err != nil || owner == nil {
			return fmt.Errorf("sign in first")
		}
		return agent.RequireBudget(tx, configuration, found, owner, time.Now())
	}); err != nil {
		refuse(err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), voiceSessionLongest)
	defer cancel()
	model := configuration.Agent.Voice.EffectiveTranscriptionModel()
	session, err := voice.Open(ctx, &voice.Settings{
		BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		TranscriptionModel:  model,
		TranscriptionPrompt: voicePrompt(found, owner),
		SilenceMS:           configuration.Agent.Voice.EffectiveSilenceMS(),
	})
	if err != nil {
		log.Warningf("%s's voice session could not start: %s", username, err)
		refuse("the transcription could not start: " + err.Error())
		return
	}
	defer func() { _ = session.Close() }()
	if err := socket.say(&voice.Event{VoiceEvent: voice.EventReady, SampleRate: voice.SampleRate}); err != nil {
		return
	}
	started := time.Now()
	log.Noticef("%s started talking to their agent (echo cancellation %v, noise suppression %v, gain control %v, capture at %v Hz)", username,
		hello.CaptureSettings.EchoCancellation, hello.CaptureSettings.NoiseSuppression, hello.CaptureSettings.AutoGainControl, hello.CaptureSettings.SampleRate)
	defer func() {
		log.Noticef("%s stopped talking to their agent after %s", username, time.Since(started).Round(time.Second))
	}()

	// What the provider hears goes back to the drawer as it comes; each
	// transcription is paid for from the agent's day like any model call.
	usageModel := provider.Name + ":" + model
	read := make(chan error, 1)
	go func() {
		read <- session.Read(func(event *voice.Event) { _ = socket.say(event) }, func(usage *voice.Usage) {
			agent.RecordUsage(self.database, found.ID, "", usageModel, "voice", llm.Usage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens})
		})
	}()
	go func() {
		select {
		case err := <-read:
			if err != nil {
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: "the transcription stopped: " + err.Error()})
			}
		case <-ctx.Done():
			_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: "the session reached its end; start talking again"})
		}
		// Either way the drawer's socket ends with it, which ends the loop
		// below.
		_ = conn.Close()
	}()

	// The answers are spoken here too, a piece at a time as the drawer asks,
	// with the same key; each piece is paid for like a transcription.
	speechSettings := &voice.SpeechSettings{
		BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		SpeechModel: configuration.Agent.Voice.EffectiveSpeechModel(),
		SpeechVoice: configuration.Agent.Voice.EffectiveSpeechVoice(),
	}
	speechUsageModel := provider.Name + ":" + speechSettings.SpeechModel
	var speakingMutex sync.Mutex
	speaking := map[string]context.CancelFunc{}
	speak := func(answerSegmentID, answerText string) {
		speakingMutex.Lock()
		if _, isSpeaking := speaking[answerSegmentID]; isSpeaking || answerSegmentID == "" || len(speaking) >= answerSegmentsAtOnce {
			speakingMutex.Unlock()
			_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioFailed, AnswerSegmentID: answerSegmentID, ErrorMessage: "too many pieces of an answer at once"})
			return
		}
		speechContext, cancelSpeech := context.WithCancel(ctx)
		speaking[answerSegmentID] = cancelSpeech
		speakingMutex.Unlock()
		go func() {
			defer func() {
				speakingMutex.Lock()
				delete(speaking, answerSegmentID)
				speakingMutex.Unlock()
				cancelSpeech()
			}()
			usage, err := voice.Speak(speechContext, speechSettings, answerText, func(pcm []byte) error {
				return socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudio, AnswerSegmentID: answerSegmentID, AnswerAudio: pcm})
			})
			if usage != nil {
				agent.RecordUsage(self.database, found.ID, "", speechUsageModel, "voice", llm.Usage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens})
			}
			switch {
			case speechContext.Err() != nil:
				// Cancelled: the drawer has moved on and wants nothing more.
			case err != nil:
				log.Warningf("%s's answer could not be spoken: %s", username, err)
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioFailed, AnswerSegmentID: answerSegmentID, ErrorMessage: err.Error()})
			default:
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioDone, AnswerSegmentID: answerSegmentID})
			}
		}()
	}
	cancelSpeaking := func(answerSegmentIDs []string) {
		speakingMutex.Lock()
		defer speakingMutex.Unlock()
		for _, answerSegmentID := range answerSegmentIDs {
			if cancelSpeech, isSpeaking := speaking[answerSegmentID]; isSpeaking {
				cancelSpeech()
			}
		}
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(voiceSilenceLongest))
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if err := session.Append(data); err != nil {
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: err.Error()})
				return
			}
			continue
		}
		var said struct {
			VoiceEvent       string   `json:"voiceEvent"`
			AnswerSegmentID  string   `json:"answerSegmentId"`
			AnswerText       string   `json:"answerText"`
			AnswerSegmentIDs []string `json:"answerSegmentIds"`
		}
		if json.Unmarshal(data, &said) != nil {
			continue
		}
		switch said.VoiceEvent {
		case "stop":
			return
		case "speakAnswer":
			speak(said.AnswerSegmentID, said.AnswerText)
		case "cancelAnswer":
			cancelSpeaking(said.AnswerSegmentIDs)
		}
	}
}

// voicePrompt tells the transcription who is being talked to, which is
// what gets the agent's name spelled right.
func voicePrompt(found *models.Agent, owner *models.User) string {
	prompt := "A person talking to their personal agent, " + found.DisplayName() + ", in TeaNode, their mail and agent server."
	if name := strings.TrimSpace(owner.Name); name != "" {
		prompt += " The person is " + name + "."
	}
	return prompt
}
