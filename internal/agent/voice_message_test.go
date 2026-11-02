package agent_test

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// transcriptionServer is the provider's file transcription: it answers every
// request with the words it is given and keeps what it was sent.
type transcriptionServer struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []transcriptionRequest
	count    atomic.Int64

	// status, when not zero, is the HTTP status it answers with instead.
	status int
	delay  time.Duration
}

type transcriptionRequest struct {
	filename, contentType, prompt, model string
	byteCount                            int
}

func newTranscriptionServer(t *testing.T, words string) *transcriptionServer {
	t.Helper()
	fake := &transcriptionServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		fake.count.Add(1)
		if fake.delay > 0 {
			time.Sleep(fake.delay)
		}
		_, params, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
		reader := multipart.NewReader(request.Body, params["boundary"])
		seen := transcriptionRequest{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			content, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				seen.filename, seen.contentType, seen.byteCount = part.FileName(), part.Header.Get("Content-Type"), len(content)
			case "prompt":
				seen.prompt = string(content)
			case "model":
				seen.model = string(content)
			}
		}
		fake.mutex.Lock()
		fake.requests = append(fake.requests, seen)
		status := fake.status
		fake.mutex.Unlock()
		if status != 0 {
			response.WriteHeader(status)
			_, _ = response.Write([]byte(`{"error":{"message":"refused"}}`))
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"text":` + quoted(words) + `,"usage":{"type":"tokens","input_tokens":30,"output_tokens":12,"input_token_details":{"audio_tokens":28,"text_tokens":2}}}`))
	}))
	t.Cleanup(fake.Close)
	return fake
}

func quoted(text string) string { return `"` + strings.ReplaceAll(text, `"`, `\"`) + `"` }

// voiceFixture is a group chat of Mira, whose agent pays for her voice
// messages, Tobias, and Noor, who joins after the message is said.
type voiceFixture struct {
	database           db.Database
	store              storage.Storage
	worker             *agent.Agent
	recorder           *recordingChat
	provider           *transcriptionServer
	configuration      *config.Configuration
	mira, tobias, noor *models.User
	miraAgent          *models.Agent
	chat               *models.Chat
}

func newVoiceFixture(t *testing.T, isVoiceOn, isMiraAgentOn bool) *voiceFixture {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	fixture := &voiceFixture{database: database, recorder: &recordingChat{}, provider: newTranscriptionServer(t, "Leaving the bakery now, see you at six")}
	fixture.configuration = config.Default()
	fixture.configuration.Agent.Enabled = true
	fixture.configuration.Agent.Features.Dreaming = new(bool)
	fixture.configuration.Agent.Voice.Enabled = isVoiceOn
	fixture.configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: fixture.provider.URL, APIKey: "k"}}
	fixture.configuration.Agent.Models.Default = "fake:thinker"
	registry, err := llm.Open(&fixture.configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	fixture.store = store
	fixture.worker = agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return fixture.configuration },
		Instance: "test", Tick: time.Hour, ChatEvents: fixture.recorder,
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if fixture.mira, err = tx.CreateUser(&models.User{Username: "mira", Name: "Mira Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.tobias, err = tx.CreateUser(&models.User{Username: "tobias", Name: "Tobias Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.noor, err = tx.CreateUser(&models.User{Username: "noor", Name: "Noor Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		dbtest.GrantPermission(t, tx, fixture.mira.ID, models.PermissionAgentUse, models.PermissionCircleUse)
		if fixture.miraAgent, err = tx.CreateAgent(&models.Agent{UserID: fixture.mira.ID, Enabled: isMiraAgentOn, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		tobiasAgent, err := tx.CreateAgent(&models.Agent{UserID: fixture.tobias.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if fixture.chat, err = tx.CreateGroupChat(fixture.mira.ID, "Bakery", []string{fixture.tobias.ID}); err != nil {
			t.Fatalf("CreateGroupChat: %s", err)
		}
		for _, owned := range []*models.Agent{fixture.miraAgent, tobiasAgent} {
			if _, err := tx.EnsureChatConversation(owned.ID, fixture.chat.ID); err != nil {
				t.Fatalf("EnsureChatConversation: %s", err)
			}
		}
	})
	return fixture
}

// join adds Noor to the chat, who then reads from now on.
func (self *voiceFixture) join(t *testing.T) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		noorAgent, err := tx.CreateAgent(&models.Agent{UserID: self.noor.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if _, err := tx.AddChatMembers(self.chat.ID, []string{self.noor.ID}); err != nil {
			t.Fatalf("AddChatMembers: %s", err)
		}
		if _, err := tx.EnsureChatConversation(noorAgent.ID, self.chat.ID); err != nil {
			t.Fatalf("EnsureChatConversation: %s", err)
		}
	})
}

// post stores a voice message of Mira's, as the API stores one, with its
// recording in the store.
func (self *voiceFixture) post(t *testing.T, transcriptState string) *models.ChatMessage {
	t.Helper()
	var stored *models.ChatMessage
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		recording, err := tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: self.miraAgent.ID, Name: "Voice message.m4a", ContentType: "audio/mp4", Size: 12,
			AudioDurationMS: 6800, AudioWaveform: "AAAA",
		})
		if err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
		if err := self.store.PutFile(t.Context(), recording.ID, []byte("not really sound")); err != nil {
			t.Fatalf("PutFile: %s", err)
		}
		if stored, err = tx.AppendChatMessage(&models.ChatMessage{
			ChatID: self.chat.ID, AuthorUserID: self.mira.ID, AuthorName: self.mira.DisplayName(),
			IsVoiceMessage: true, TranscriptState: transcriptState, Attachments: []models.AgentAttachment{*recording},
		}); err != nil {
			t.Fatalf("AppendChatMessage: %s", err)
		}
	})
	return stored
}

func (self *voiceFixture) message(t *testing.T, messageId string) *models.ChatMessage {
	t.Helper()
	var message *models.ChatMessage
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if message, err = tx.GetChatMessage(messageId); err != nil || message == nil {
			t.Fatalf("GetChatMessage: %v %v", message, err)
		}
	})
	return message
}

// The words are written under the message, paid for by the sender's agent
// with the kind voice, and the members who can read the message are told it
// changed; a member who joined after it was said is not.
func TestAVoiceMessageGetsItsWordsAndTheMembersAreTold(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	message := fixture.post(t, models.VoiceTranscriptWaiting)
	fixture.join(t)

	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil {
		t.Fatalf("TranscribeVoiceMessage: %s", err)
	}
	finished := fixture.message(t, message.ID)
	if finished.TranscriptState != models.VoiceTranscriptDone || finished.TranscriptText != "Leaving the bakery now, see you at six" {
		t.Errorf("the message is %q %q", finished.TranscriptState, finished.TranscriptText)
	}
	if finished.EditedAt != nil {
		t.Errorf("its words are not an edit: %v", finished.EditedAt)
	}
	if len(fixture.provider.requests) != 1 {
		t.Fatalf("the provider was asked %d times", len(fixture.provider.requests))
	}
	asked := fixture.provider.requests[0]
	if asked.contentType != "audio/mp4" || asked.byteCount != len("not really sound") || !strings.Contains(asked.prompt, "Mira Example") || !strings.Contains(asked.prompt, "Tobias Example") {
		t.Errorf("the provider was sent %+v", asked)
	}
	if asked.model != "gpt-4o-transcribe" {
		t.Errorf("the model is %q", asked.model)
	}

	edited := fixture.recorder.eventsOf(agent.EventEdited)
	if len(edited) != 2 {
		t.Fatalf("%d members were told, want Mira and Tobias but not Noor, who joined later", len(edited))
	}
	for _, event := range edited {
		if event.MessageID != message.ID || event.Message == nil || !event.Message.IsVoiceMessage || event.Message.TranscriptText != finished.TranscriptText || event.Message.TranscriptState != models.VoiceTranscriptDone {
			t.Errorf("the event is %+v", event)
		}
		if len(event.Message.Attachments) != 1 || event.Message.Attachments[0].AudioDurationMS != 6800 {
			t.Errorf("the recording's length did not come with the message: %+v", event.Message.Attachments)
		}
		if event.Message.AuthorUserID != fixture.mira.ID || event.Message.ConversationID != event.ConversationID {
			t.Errorf("the message is not drawn for the member it is told to: %+v", event.Message)
		}
	}
	isMineCount := 0
	for _, event := range edited {
		if event.Message.IsMine {
			isMineCount++
		}
	}
	if isMineCount != 1 {
		t.Errorf("the message is Mira's own to one member, not %d", isMineCount)
	}
	// Paid for by the sender's agent, as voice.
	if got := dbtest.QueryString(t, fixture.database, `SELECT "kind" || ':' || "agent_id" FROM "agent_usage" WHERE "kind" = 'voice' LIMIT 1`); got != "voice:"+fixture.miraAgent.ID {
		t.Errorf("the usage went to %q", got)
	}

	// Done is done: it is not written again.
	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil || len(fixture.provider.requests) != 1 {
		t.Errorf("a finished message was transcribed again: %v, %d requests", err, len(fixture.provider.requests))
	}
}

// With voice off, or the sender's agent off, nobody pays: the message ends off,
// the provider is never asked, and the members are told so the bubble stops
// waiting.
func TestAVoiceMessageEndsOffWhenNobodyCanPayForItsWords(t *testing.T) {
	for name, isVoiceOn := range map[string]bool{"voice off": false, "agent off": true} {
		t.Run(name, func(t *testing.T) {
			fixture := newVoiceFixture(t, isVoiceOn, isVoiceOn == false)
			message := fixture.post(t, models.VoiceTranscriptWaiting)
			if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil {
				t.Fatalf("TranscribeVoiceMessage: %s", err)
			}
			if got := fixture.message(t, message.ID); got.TranscriptState != models.VoiceTranscriptOff || got.TranscriptText != "" {
				t.Errorf("the message is %q %q", got.TranscriptState, got.TranscriptText)
			}
			if len(fixture.provider.requests) != 0 {
				t.Errorf("the provider was asked for words nobody pays for")
			}
			if len(fixture.recorder.eventsOf(agent.EventEdited)) != 2 {
				t.Errorf("the members were not told")
			}
		})
	}
}

// The state a message starts in follows who can pay: waiting for a sender
// whose agent is on and has budget, off for any other.
func TestAVoiceMessageStartsOffWhenTheSendersAgentCannotPay(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	stateOf := func(sender *models.Agent, owner *models.User) string {
		var state string
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			state = fixture.worker.VoiceMessageTranscriptState(tx, sender, owner)
		})
		return state
	}
	if got := stateOf(fixture.miraAgent, fixture.mira); got != models.VoiceTranscriptWaiting {
		t.Errorf("a sender whose agent is on starts %q", got)
	}
	if got := stateOf(nil, fixture.mira); got != models.VoiceTranscriptOff {
		t.Errorf("a sender with no agent starts %q", got)
	}
	// Tobias's agent is on, but he may not use it.
	var tobiasAgent *models.Agent
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if tobiasAgent, err = tx.GetAgentByUser(fixture.tobias.ID); err != nil {
			t.Fatal(err)
		}
	})
	if got := stateOf(tobiasAgent, fixture.tobias); got != models.VoiceTranscriptOff {
		t.Errorf("a sender who may not use an agent starts %q", got)
	}
	// Voice switched off on the server.
	fixture.configuration.Agent.Voice.Enabled = false
	if got := stateOf(fixture.miraAgent, fixture.mira); got != models.VoiceTranscriptOff {
		t.Errorf("with voice off a message starts %q", got)
	}
}

// Of two servers asked at once, one works on the message and the provider is
// paid once.
func TestAClaimedVoiceMessageIsNotTranscribedTwice(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	fixture.provider.delay = 200 * time.Millisecond
	message := fixture.post(t, models.VoiceTranscriptWaiting)

	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil {
				t.Errorf("TranscribeVoiceMessage: %s", err)
			}
		}()
	}
	group.Wait()
	if count := fixture.provider.count.Load(); count != 1 {
		t.Errorf("the provider was asked %d times", count)
	}
	if got := fixture.message(t, message.ID); got.TranscriptState != models.VoiceTranscriptDone {
		t.Errorf("the message is %q", got.TranscriptState)
	}
}

// A provider that is down leaves the message waiting for another try once the
// claim has run out; one that refuses the recording itself ends it failed.
func TestAVoiceMessageWaitsForAProviderThatIsDownAndFailsForOneThatRefuses(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	message := fixture.post(t, models.VoiceTranscriptWaiting)

	fixture.provider.mutex.Lock()
	fixture.provider.status = http.StatusBadGateway
	fixture.provider.mutex.Unlock()
	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err == nil {
		t.Error("a provider that is down should be an error to try again")
	}
	if got := fixture.message(t, message.ID); got.TranscriptState != models.VoiceTranscriptWaiting {
		t.Errorf("the message is %q", got.TranscriptState)
	}
	if len(fixture.recorder.eventsOf(agent.EventEdited)) != 0 {
		t.Error("the members were told of a message that is still waiting")
	}

	// Claimed a moment ago, so nobody else works on it now.
	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil || fixture.provider.count.Load() != 1 {
		t.Errorf("a claimed message was tried again at once: %v, %d requests", err, fixture.provider.count.Load())
	}

	refused := fixture.post(t, models.VoiceTranscriptWaiting)
	fixture.provider.mutex.Lock()
	fixture.provider.status = http.StatusBadRequest
	fixture.provider.mutex.Unlock()
	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), refused.ID); err != nil {
		t.Errorf("TranscribeVoiceMessage: %s", err)
	}
	if got := fixture.message(t, refused.ID); got.TranscriptState != models.VoiceTranscriptFailed {
		t.Errorf("the message is %q", got.TranscriptState)
	}
}

// The tick starts the messages nobody is working on, and gives up on those
// that have waited half an hour, telling the members.
func TestTheSweepTranscribesWaitingVoiceMessagesAndGivesUpOnOldOnes(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	old := fixture.post(t, models.VoiceTranscriptWaiting)
	// The members were in the chat before the message was said.
	dbtest.Exec(t, fixture.database, `UPDATE "chat_member" SET "joined_at" = now() - interval '2 hours'`)
	dbtest.Exec(t, fixture.database, `UPDATE "chat_message" SET "created_at" = now() - interval '31 minutes' WHERE "id" = '`+old.ID+`'`)
	fresh := fixture.post(t, models.VoiceTranscriptWaiting)

	if err := fixture.worker.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	fixture.worker.Wait()
	if got := fixture.message(t, old.ID); got.TranscriptState != models.VoiceTranscriptFailed {
		t.Errorf("a message that waited over half an hour is %q", got.TranscriptState)
	}
	if got := fixture.message(t, fresh.ID); got.TranscriptState != models.VoiceTranscriptDone || got.TranscriptText == "" {
		t.Errorf("a message nobody was working on is %q %q", got.TranscriptState, got.TranscriptText)
	}
	if told := len(fixture.recorder.eventsOf(agent.EventEdited)); told != 4 {
		t.Errorf("%d events were published, want two members for each of the two messages", told)
	}
}

// A message deleted before its words come back is left alone, and no event
// brings it back.
func TestADeletedVoiceMessageGetsNoWords(t *testing.T) {
	fixture := newVoiceFixture(t, true, true)
	message := fixture.post(t, models.VoiceTranscriptWaiting)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.DeleteChatMessage(message.ID); err != nil {
			t.Fatal(err)
		}
	})
	if err := fixture.worker.TranscribeVoiceMessage(t.Context(), message.ID); err != nil {
		t.Fatal(err)
	}
	got := fixture.message(t, message.ID)
	if got.TranscriptText != "" || got.TranscriptState != "" || got.IsVoiceMessage || got.DeletedAt == nil {
		t.Errorf("the deleted message is %+v", got)
	}
	if fixture.provider.count.Load() != 0 || len(fixture.recorder.eventsOf(agent.EventEdited)) != 0 {
		t.Error("something was done for a deleted message")
	}
}
