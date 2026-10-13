package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
)

// Live words of a recording: a phone or browser capturing a recording
// streams the microphone over a websocket, this server streams it on to the
// provider's realtime transcription as voice mode does (internal/voice), and
// tells the socket what is heard with times on the recording's timeline.
// Each finished utterance is kept (models.RecordingLiveUtterance) until the
// saved audio has been transcribed with speakers; it is shown in the
// timeline meanwhile. The audio streamed here is not kept, and what the
// provider says is never given to the agent, to memory or to knowledge.

// recordingLiveStreamStartMaximumMS bounds where on the timeline a stream
// may say it begins: the longest a recording can be.
const recordingLiveStreamStartMaximumMS int64 = 7 * 24 * 60 * 60 * 1000

// recordingLiveTextBytesMaximum bounds the words of one utterance kept and
// told; the provider's words are somebody else's, and a limit is cheaper
// than trusting them to be short.
const recordingLiveTextBytesMaximum = 4096

// recordingLiveBeyondAudioMS is how far past the audio it was given the
// provider may place an utterance before its times are not believed.
const recordingLiveBeyondAudioMS int64 = 5000

// recordingLiveCleanText makes the provider's words safe to keep and show:
// valid UTF-8, no control characters, at most recordingLiveTextBytesMaximum
// bytes, and trimmed unless it is part of an utterance still being heard.
func recordingLiveCleanText(text string, isPartial bool) string {
	text = strings.ToValidUTF8(text, "")
	text = strings.Map(func(character rune) rune {
		switch {
		case character == '\n' || character == '\t':
			return ' '
		case unicode.IsControl(character) || character == ' ' || character == ' ':
			return -1
		}
		return character
	}, text)
	if len(text) > recordingLiveTextBytesMaximum {
		cut := recordingLiveTextBytesMaximum
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	if isPartial {
		return text
	}
	return strings.TrimSpace(text)
}

// recordingLiveTimes places an utterance on the recording's timeline from
// the session-relative times the provider gave, or from the audio handed to
// it when it gave none. previousEndMS is where the last utterance ended.
func recordingLiveTimes(startMS, endMS, previousEndMS, appendedMS, streamStartMS int64) (int64, int64) {
	if endMS <= 0 || endMS < startMS {
		startMS, endMS = max(startMS, previousEndMS), appendedMS
	}
	endMS = min(endMS, appendedMS+recordingLiveBeyondAudioMS)
	startMS = max(0, min(startMS, endMS))
	return streamStartMS + startMS, streamStartMS + endMS
}

// recordingLiveView is the websocket a capture streams its microphone to.
func (self *graph) recordingLiveView(response http.ResponseWriter, request *http.Request) {
	configuration := self.config.Current()
	provider := configuration.Agent.VoiceProvider()
	if !configuration.Agent.Recording.IsLiveTranscriptionOn() || provider == nil || self.agentWorker() == nil {
		http.Error(response, "live transcription is off on this server", http.StatusForbidden)
		return
	}
	recordingId := mux.Vars(request)["recordingId"]
	// The session cookie is taken only from a page of this server, as voice
	// takes it; a token in the hello is taken from anywhere.
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

	// Who this is, and where on the timeline the stream begins, come with
	// the first message.
	_ = conn.SetReadDeadline(time.Now().Add(helloWait))
	var hello struct {
		VoiceEvent    string `json:"voiceEvent"`
		Authorization string `json:"authorization"`
		StreamStartMS int64  `json:"streamStartMS"`
	}
	if messageType, helloBytes, err := conn.ReadMessage(); err != nil || messageType != websocket.TextMessage || json.Unmarshal(helloBytes, &hello) != nil || hello.VoiceEvent != "hello" {
		refuse("the first message says hello")
		return
	}
	if hello.StreamStartMS < 0 || hello.StreamStartMS > recordingLiveStreamStartMaximumMS {
		refuse("the stream starts somewhere on the recording's timeline")
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
	// requireCapture is what has to stay true for the stream to go on: the
	// agent's day has room, the recording is the caller's and its capture
	// has not stopped. A recording that is not the caller's is not found,
	// as it is nowhere else.
	requireCapture := func() error {
		return self.database.Transaction(func(tx db.Transaction) (err error) {
			if owner == nil {
				if owner, err = tx.GetUserByUsername(username); err != nil || owner == nil {
					return fmt.Errorf("sign in first")
				}
			}
			if !self.config.Current().Agent.Recording.IsLiveTranscriptionOn() {
				return fmt.Errorf("live transcription was turned off")
			}
			recording, err := tx.GetRecording(owner.ID, recordingId)
			if err != nil {
				return fmt.Errorf("that recording was not found")
			}
			if !db.IsRecordingCaptureLive(recording.RecordingCaptureState) {
				return fmt.Errorf("that recording is not being captured")
			}
			return agent.RequireBudget(tx, self.config.Current(), found, owner, time.Now())
		})
	}
	if err := requireCapture(); err != nil {
		refuse(err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), voiceSessionLongest)
	defer cancel()
	model := configuration.Agent.Voice.EffectiveTranscriptionModel()
	session, err := voice.Open(ctx, &voice.Settings{
		BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		TranscriptionModel:  model,
		TranscriptionPrompt: recordingLivePrompt(owner),
		SilenceMS:           configuration.Agent.Voice.SilenceMS,
	})
	if err != nil {
		log.Warningf("%s's live transcription of a recording could not start: %s", username, err)
		refuse("the transcription could not start: " + err.Error())
		return
	}
	defer func() { _ = session.Close() }()
	if err := socket.say(&voice.Event{VoiceEvent: voice.EventReady, SampleRate: voice.SampleRate}); err != nil {
		return
	}
	started := time.Now()
	log.Noticef("%s started live transcription of a recording at %d ms", username, hello.StreamStartMS)
	defer func() {
		log.Noticef("%s stopped live transcription of a recording after %s", username, time.Since(started).Round(time.Second))
	}()

	// What the provider hears goes back as it comes, on the recording's
	// timeline, and each transcription is paid for from the agent's day like
	// any model call.
	usageModel := provider.Name + ":" + model
	var previousEndMS int64
	read := make(chan error, 1)
	go func() {
		read <- session.Read(func(event *voice.Event) {
			switch event.VoiceEvent {
			case voice.EventTranscriptDelta:
				event.TranscriptText = recordingLiveCleanText(event.TranscriptText, true)
			case voice.EventSpeechStarted:
				event.UtteranceStartMS += hello.StreamStartMS
			case voice.EventSpeechStopped:
				event.UtteranceEndMS += hello.StreamStartMS
			case voice.EventTranscriptFinal:
				event.TranscriptText = recordingLiveCleanText(event.TranscriptText, false)
				startMS, endMS := recordingLiveTimes(event.UtteranceStartMS, event.UtteranceEndMS, previousEndMS, session.AppendedMS(), hello.StreamStartMS)
				previousEndMS = endMS - hello.StreamStartMS
				event.UtteranceStartMS, event.UtteranceEndMS = startMS, endMS
				if event.TranscriptText != "" {
					self.saveRecordingLiveUtterance(username, owner.ID, recordingId, event)
				}
			}
			_ = socket.say(event)
		}, func(usage *voice.Usage) {
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
			_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: "the session reached its end; stream again"})
		}
		// Either way the socket ends with it, which ends the loop below.
		_ = conn.Close()
	}()

	checkedAt := time.Now()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(voiceSilenceLongest))
		messageType, messageBytes, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if time.Since(checkedAt) > voiceBudgetRecheck {
				checkedAt = time.Now()
				if err := requireCapture(); err != nil {
					_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: err.Error()})
					return
				}
			}
			if err := session.Append(messageBytes); err != nil {
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventError, ErrorMessage: err.Error()})
				return
			}
			continue
		}
		var said struct {
			VoiceEvent string `json:"voiceEvent"`
		}
		if json.Unmarshal(messageBytes, &said) != nil {
			continue
		}
		if said.VoiceEvent == "stop" {
			return
		}
	}
}

// saveRecordingLiveUtterance keeps one utterance for the timeline. A
// capture that stopped while it was being transcribed refuses it, which is
// right: the saved audio has it.
func (self *graph) saveRecordingLiveUtterance(username, userId, recordingId string, event *voice.Event) {
	utteranceId := event.UtteranceID
	if len(utteranceId) > 128 {
		utteranceId = utteranceId[:128]
	}
	if err := self.database.Transaction(func(tx db.Transaction) error {
		return tx.SaveRecordingLiveUtterance(userId, recordingId, &models.RecordingLiveUtterance{
			UtteranceID: utteranceId, UtteranceStartMS: event.UtteranceStartMS, UtteranceEndMS: event.UtteranceEndMS, TranscriptText: event.TranscriptText,
		})
	}); err != nil {
		log.Warningf("%s's live words of a recording could not be kept: %s", username, err)
	}
}

// recordingLivePrompt tells the transcription what is being heard, which
// helps it spell names.
func recordingLivePrompt(owner *models.User) string {
	prompt := "People talking in a meeting that is being recorded."
	if name := strings.TrimSpace(owner.Name); name != "" {
		prompt += " One of them is " + name + "."
	}
	return prompt
}
