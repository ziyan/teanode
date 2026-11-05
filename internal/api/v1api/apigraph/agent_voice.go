package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
	"github.com/ziyan/teanode/internal/voicemessage"
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

// voiceBudgetRecheck is how often a call checks the agent's day again.
const voiceBudgetRecheck = 2 * time.Minute

// answerSegmentsAtOnce bounds the pieces of an answer being spoken at
// once: the one playing and the next few, made while it plays.
const answerSegmentsAtOnce = 4

// voiceSilenceLongest is how long the drawer may send nothing at all; it
// sends audio continuously while listening, silence included, and
// stillHere while the person has the microphone muted.
const voiceSilenceLongest = time.Minute

// AgentVoiceQuery says whether the caller may talk to their agent.
type AgentVoiceQuery interface {
	// Whether the drawer offers the microphone: the operator turned voice
	// on with a provider to transcribe it, and the caller has an agent that
	// is on. Needs agent:use.
	ReadAgentVoice(ctx context.Context) (*AgentVoiceView, error)

	// A message as it is read aloud: without what is only for the eye, cut
	// into pieces to say one after another. Each is said by posting it to
	// the voice's read address, or by the device when the server has no voice
	ReadSpeechPieces(ctx context.Context, arguments ReadSpeechPiecesArguments) ([]string, error)
}

// AgentVoiceView is what the drawer needs to offer voice.
type AgentVoiceView struct {
	IsVoiceAvailable bool `json:"isVoiceAvailable"`

	// SpeechVoice is the voice the caller's answers are read in: their own
	// choice, or the server's. SpeechVoices are the ones they may choose,
	// and ServerSpeechVoice is the server's, which an empty choice means.
	SpeechVoice       string   `json:"speechVoice"`
	SpeechVoices      []string `json:"speechVoices"`
	ServerSpeechVoice string   `json:"serverSpeechVoice"`

	// SampleRate is the audio the socket takes: mono 16-bit PCM at this
	// many samples a second.
	SampleRate int `json:"sampleRate"`

	// IsVoiceMessageAvailable says the server can prepare a voice message
	// (ffmpeg was found when it started), so that a chat offers to record one.
	// It does not depend on the agent: a person whose agent is off, or who may
	// only chat, sends voice messages that play, with no words under them.
	IsVoiceMessageAvailable bool `json:"isVoiceMessageAvailable"`
}

type ReadSpeechPiecesArguments struct {
	MessageText string `json:"messageText"`
}

func (self *graph) ReadSpeechPieces(ctx context.Context, arguments ReadSpeechPiecesArguments) ([]string, error) {
	if _, err := self.requireAccount(ctx); err != nil {
		return nil, err
	}
	pieces := voice.SpokenPieces(models.StripHiddenMarkers(arguments.MessageText))
	if pieces == nil {
		return []string{}, nil
	}
	return pieces, nil
}

func (self *graph) ReadAgentVoice(ctx context.Context) (*AgentVoiceView, error) {
	_, found, err := self.requireAgentPerson(ctx)
	isVoiceMessageAvailable := voicemessage.IsAvailable()
	if errors.Is(err, agent.ErrUnavailable) {
		// The drawer opens for chats with the agent off, and a microphone
		// to talk to an agent that is off is simply not offered.
		return &AgentVoiceView{SampleRate: voice.SampleRate, SpeechVoices: config.VoiceSpeechVoices, IsVoiceMessageAvailable: isVoiceMessageAvailable}, nil
	}
	// A person who may chat but not use an agent still sends voice messages.
	if principal := api.ContextPrincipal(ctx); errors.Is(err, api.ErrNotFound) && principal != nil && principal.User != nil && principal.Permissions.Has(models.PermissionCircleUse) {
		return &AgentVoiceView{SampleRate: voice.SampleRate, SpeechVoices: config.VoiceSpeechVoices, IsVoiceMessageAvailable: isVoiceMessageAvailable}, nil
	}
	if err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	return &AgentVoiceView{
		IsVoiceMessageAvailable: isVoiceMessageAvailable,
		IsVoiceAvailable:        configuration.Agent.Voice.Enabled && configuration.Agent.VoiceProvider() != nil && self.agentWorker() != nil,
		SampleRate:              voice.SampleRate,
		SpeechVoice:             speechVoiceOf(configuration, found),
		SpeechVoices:            config.VoiceSpeechVoices,
		ServerSpeechVoice:       configuration.Agent.Voice.EffectiveSpeechVoice(),
	}, nil
}

// speechVoiceOf is the voice an agent's answers are read in: the person's
// choice, or the server's.
func speechVoiceOf(configuration *config.Configuration, found *models.Agent) string {
	if found != nil && found.SpeechVoice != "" {
		return found.SpeechVoice
	}
	return configuration.Agent.Voice.EffectiveSpeechVoice()
}

// voiceSampleView says a few words in the voice asked for, as a WAV file,
// for a person choosing theirs. It is paid for like any speech.
func (self *graph) voiceSampleView(response http.ResponseWriter, request *http.Request) {
	_, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	configuration := self.config.Current()
	provider := configuration.Agent.VoiceProvider()
	if !configuration.Agent.Voice.Enabled || provider == nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "voice is off on this server"})
		return
	}
	speechVoice := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("speechVoice")))
	if speechVoice == "" {
		speechVoice = speechVoiceOf(configuration, found)
	}
	if !slices.Contains(config.VoiceSpeechVoices, speechVoice) && speechVoice != configuration.Agent.Voice.EffectiveSpeechVoice() {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "not a voice"})
		return
	}
	self.speakAsWAV(response, request, found, speechVoice, voiceSampleText(found), "private, max-age=3600")
}

// readPieceBytesMost bounds what one request asks to have said, which is
// what the provider takes at once.
const readPieceBytesMost = voice.MaximumAnswerTextLength

// voiceReadView says a piece of a message, from ReadSpeechPieces, in the
// voice the person chose, as a WAV file. The person sends the words, since
// they read them on their screen already; it is paid for like any speech.
func (self *graph) voiceReadView(response http.ResponseWriter, request *http.Request) {
	_, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	configuration := self.config.Current()
	if !configuration.Agent.Voice.Enabled || configuration.Agent.VoiceProvider() == nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "voice is off on this server"})
		return
	}
	var asked struct {
		AnswerText string `json:"answerText"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4*readPieceBytesMost)).Decode(&asked); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "not a piece of a message"})
		return
	}
	answerText := strings.TrimSpace(asked.AnswerText)
	if answerText == "" || len(answerText) > readPieceBytesMost {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "a piece of a message is a few sentences"})
		return
	}
	self.speakAsWAV(response, request, found, speechVoiceOf(configuration, found), answerText, "no-store")
}

// speakAsWAV answers with words said in a voice, held to the person's budget
// and counted against it.
func (self *graph) speakAsWAV(response http.ResponseWriter, request *http.Request, found *models.Agent, speechVoice, answerText, cacheControl string) {
	configuration := self.config.Current()
	provider := configuration.Agent.VoiceProvider()
	var owner *models.User
	if err := self.database.Transaction(func(tx db.Transaction) (err error) {
		if owner, err = tx.GetUser(found.UserID); err != nil || owner == nil {
			return fmt.Errorf("sign in first")
		}
		return agent.RequireBudget(tx, configuration, found, owner, time.Now())
	}); err != nil {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	settings := &voice.SpeechSettings{
		BaseURL: provider.BaseURL, APIKey: provider.APIKey,
		SpeechModel: configuration.Agent.Voice.EffectiveSpeechModel(),
		SpeechVoice: speechVoice,
	}
	var pcm []byte
	usage, err := voice.Speak(request.Context(), settings, answerText, func(piece []byte) error {
		pcm = append(pcm, piece...)
		return nil
	})
	if usage != nil {
		agent.RecordUsage(self.database, found.ID, "", provider.Name+":"+settings.SpeechModel, "voice", llm.Usage{PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens})
	}
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	response.Header().Set("Content-Type", "audio/wav")
	response.Header().Set("Cache-Control", cacheControl)
	_, _ = response.Write(voice.WAV(pcm))
}

// voiceSampleText is what a sample says, in the agent's language where
// there are words for it.
func voiceSampleText(found *models.Agent) string {
	name := found.DisplayName()
	switch strings.ToLower(found.Language) {
	case "ja":
		return "こんにちは、" + name + "です。回答を読み上げるときは、この声で話します。"
	case "zh":
		return "你好，我是" + name + "。朗读回答时，我会用这个声音。"
	}
	return "Hi, it's " + name + ". This is how I will sound when I read your answers aloud."
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
	isFromThisServer := request.Header.Get("Origin") != "" && api.FromThisServer(request)
	sessionUsername := api.UsernameFromRequest(request)
	upgrader := websocket.Upgrader{CheckOrigin: api.FromThisServer}
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

		// Purpose is "dictation" for words that go into a box and are never
		// sent as a turn, "call" or nothing for talking to the agent.
		// DictationPlace says which box: the agent's conversation, a chat or
		// the journal's own words.
		Purpose        string `json:"purpose"`
		DictationPlace string `json:"dictationPlace"`

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
	if messageType, helloBytes, err := conn.ReadMessage(); err != nil || messageType != websocket.TextMessage || json.Unmarshal(helloBytes, &hello) != nil || hello.VoiceEvent != "hello" {
		refuse("the first message says hello")
		return
	}
	isDictation := hello.Purpose == voicePurposeDictation
	switch {
	case hello.Purpose != "" && hello.Purpose != voicePurposeDictation && hello.Purpose != voicePurposeCall:
		refuse("the purpose is dictation or call")
		return
	case isDictation && hello.DictationPlace != "" && !slices.Contains(voiceDictationPlaces, hello.DictationPlace):
		refuse("dictation is for the agent, a chat or the journal")
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
		TranscriptionPrompt: voicePromptFor(found, owner, isDictation, hello.DictationPlace),
		SilenceMS:           configuration.Agent.Voice.SilenceMS,
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
	// What the session is for, in the log's words.
	doing, stopped := "talking to their agent", "stopped talking to their agent"
	if isDictation {
		doing, stopped = "dictating", "stopped dictating"
	}
	log.Noticef("%s started %s (echo cancellation %v, noise suppression %v, gain control %v, capture at %v Hz)", username, doing,
		hello.CaptureSettings.EchoCancellation, hello.CaptureSettings.NoiseSuppression, hello.CaptureSettings.AutoGainControl, hello.CaptureSettings.SampleRate)
	defer func() {
		log.Noticef("%s %s after %s", username, stopped, time.Since(started).Round(time.Second))
	}()

	// What the provider hears goes back to the drawer as it comes; each
	// transcription is paid for from the agent's day like any model call.
	usageModel := provider.Name + ":" + model
	read := make(chan error, 1)
	go func() {
		read <- session.Read(func(event *voice.Event) { _ = socket.say(event) }, func(usage *voice.Usage) {
			agent.RecordUsage(self.database, found.ID, "", usageModel, "voice", llm.Usage{PromptTokens: usage.InputTokens, AudioInputTokens: usage.AudioInputTokens, CompletionTokens: usage.OutputTokens})
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
	// requireBudget checks the agent's day again: a call outlasts the check
	// made when it began, and each piece spoken and each minute heard is
	// paid for from the same day.
	requireBudget := func() error {
		return self.database.Transaction(func(tx db.Transaction) error {
			return agent.RequireBudget(tx, self.config.Current(), found, owner, time.Now())
		})
	}
	type speakingPiece struct {
		cancel context.CancelFunc
	}
	var speakingMutex sync.Mutex
	speaking := map[string]*speakingPiece{}
	speak := func(answerSegmentID, answerText string) {
		// Dictation puts words in a box; nothing answers it.
		if isDictation {
			_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioFailed, AnswerSegmentID: answerSegmentID, ErrorMessage: "dictation has no answers to read"})
			return
		}
		speakingMutex.Lock()
		if _, isSpeaking := speaking[answerSegmentID]; isSpeaking || answerSegmentID == "" || len(speaking) >= answerSegmentsAtOnce {
			speakingMutex.Unlock()
			_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioFailed, AnswerSegmentID: answerSegmentID, ErrorMessage: "too many pieces of an answer at once"})
			return
		}
		speechContext, cancelSpeech := context.WithCancel(ctx)
		piece := &speakingPiece{cancel: cancelSpeech}
		speaking[answerSegmentID] = piece
		speakingMutex.Unlock()
		go func() {
			defer func() {
				speakingMutex.Lock()
				// Unless a cancellation already let it go, and the id was
				// asked for again since.
				if speaking[answerSegmentID] == piece {
					delete(speaking, answerSegmentID)
				}
				speakingMutex.Unlock()
				cancelSpeech()
			}()
			if err := requireBudget(); err != nil {
				_ = socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudioFailed, AnswerSegmentID: answerSegmentID, ErrorMessage: err.Error()})
				return
			}
			// The person's voice as it is now: they may change it mid-call.
			pieceSettings := *speechSettings
			var current *models.Agent
			_ = self.database.Transaction(func(tx db.Transaction) (err error) {
				current, err = tx.GetAgent(found.ID)
				return err
			})
			pieceSettings.SpeechVoice = speechVoiceOf(self.config.Current(), current)
			usage, err := voice.Speak(speechContext, &pieceSettings, answerText, func(pcm []byte) error {
				return socket.say(&voice.Event{VoiceEvent: voice.EventAnswerAudio, AnswerSegmentID: answerSegmentID, AnswerAudio: pcm})
			})
			if usage == nil && speechContext.Err() != nil {
				// Cut short, the provider says nothing of what it cost, and
				// charges for it all the same: counted from its words.
				usage = voice.EstimateSpeechUsage(answerText)
			}
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
			if piece, isSpeaking := speaking[answerSegmentID]; isSpeaking {
				piece.cancel()
				// Its room is free now, not when its request gives up: the
				// pieces of the next answer come at once.
				delete(speaking, answerSegmentID)
			}
		}
	}

	budgetCheckedAt := time.Now()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(voiceSilenceLongest))
		messageType, messageBytes, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if time.Since(budgetCheckedAt) > voiceBudgetRecheck {
				budgetCheckedAt = time.Now()
				if err := requireBudget(); err != nil {
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
			VoiceEvent       string   `json:"voiceEvent"`
			AnswerSegmentID  string   `json:"answerSegmentId"`
			AnswerText       string   `json:"answerText"`
			AnswerSegmentIDs []string `json:"answerSegmentIds"`
		}
		if json.Unmarshal(messageBytes, &said) != nil {
			continue
		}
		switch said.VoiceEvent {
		case "stop":
			return
		case "speakAnswer":
			speak(said.AnswerSegmentID, said.AnswerText)
		case "cancelAnswer":
			cancelSpeaking(said.AnswerSegmentIDs)
		case "stillHere":
			// A muted microphone: reading it was all it was for.
		}
	}
}

// The purposes a voice socket's hello may name. Nothing is a call.
const (
	voicePurposeCall      = "call"
	voicePurposeDictation = "dictation"
)

// voiceDictationPlaces are the boxes dictation puts words into.
var voiceDictationPlaces = []string{"agent", "chat", "journal"}

// voicePromptFor tells the transcription what is being said and to whom,
// which is what gets the names in it spelled right: a call is to the person's
// agent, and dictation is words for a box.
func voicePromptFor(found *models.Agent, owner *models.User, isDictation bool, dictationPlace string) string {
	prompt := "A person talking to their personal agent, " + found.DisplayName() + ", in TeaNode, their mail and agent server."
	if isDictation {
		switch dictationPlace {
		case "chat":
			prompt = "A person dictating a message to people in a chat, in TeaNode, their mail and agent server."
		case "journal":
			prompt = "A person dictating their own words for a page of their journal, in TeaNode, their mail and agent server."
		default:
			prompt = "A person dictating a message to their personal agent, " + found.DisplayName() + ", in TeaNode, their mail and agent server."
		}
	}
	if name := strings.TrimSpace(owner.Name); name != "" {
		prompt += " The person is " + name + "."
	}
	return prompt
}
