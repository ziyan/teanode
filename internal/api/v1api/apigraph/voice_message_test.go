package apigraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voicemessage"
)

// recording stores a prepared recording of the person's, as the upload endpoint
// leaves one, and returns its id.
func (self *chatWorld) recording(t *testing.T, owner *models.User, durationMS int64) string {
	t.Helper()
	var attachmentId string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		ownerAgent, err := tx.EnsureAgent(owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		attachment, err := tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID: ownerAgent.ID, Name: voicemessage.FileName, ContentType: voicemessage.ContentType, Size: 16,
			AudioDurationMS: durationMS, AudioWaveform: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gISIjJCUmJygpKissLS4v",
		})
		if err != nil {
			t.Fatal(err)
		}
		attachmentId = attachment.ID
	})
	if err := self.files.PutFile(t.Context(), attachmentId, []byte("not really sound")); err != nil {
		t.Fatal(err)
	}
	return attachmentId
}

func (self *chatWorld) sendVoiceMessage(t *testing.T, from *models.User, conversationId, attachmentId string) {
	t.Helper()
	data := self.call(t, from, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", voiceMessageAttachmentId: %q) { runId conversationId } }`, conversationId, attachmentId))
	if turn := data["AskAgent"].(map[string]any); turn["runId"] != "" || turn["conversationId"] != conversationId {
		t.Fatalf("a voice message started a run: %v", turn)
	}
}

// A voice message is one recording and nothing else: it posts with an empty
// body, starts no turn, carries the recording's length and waveform to the
// other member, who fetches the file by range; with no one to pay for the words
// it says so; and it cannot be edited, only deleted.
func TestAVoiceMessageIsPostedAsOneRecordingWithNoWords(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob, world.carol)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	events := world.follow(bobChat)
	defer events.stop()

	recordingId := world.recording(t, world.alice, 6800)
	world.sendVoiceMessage(t, world.alice, aliceChat, recordingId)

	message := world.read(t, world.bob, bobChat)[0]
	if message["isVoiceMessage"] != true || message["content"] != "" || message["isDeleted"] != false {
		t.Fatalf("Bob reads %v", message)
	}
	// Alice has no agent that can pay, and voice is off here: the bubble
	// plays and no words are coming.
	if message["transcriptState"] != models.VoiceTranscriptOff || message["transcriptText"] != "" {
		t.Errorf("the transcript is %v %v", message["transcriptState"], message["transcriptText"])
	}
	attachments := message["attachments"].([]any)
	recording := attachments[0].(map[string]any)
	if len(attachments) != 1 || recording["id"] != recordingId || recording["audioDurationMS"] != float64(6800) ||
		recording["contentType"] != "audio/mp4" || !strings.HasPrefix(recording["audioWaveform"].(string), "AAEC") {
		t.Errorf("the recording is %v", attachments)
	}
	if said := events.next(agent.EventSaid); said == nil || !said.Message.IsVoiceMessage || said.Message.TranscriptState != models.VoiceTranscriptOff || said.Message.Attachments[0].AudioDurationMS != 6800 {
		t.Errorf("Bob was told %+v", said)
	}
	// Alice reads her own voice message the same way.
	if own := world.read(t, world.alice, aliceChat)[0]; own["isVoiceMessage"] != true || own["isMine"] != true {
		t.Errorf("Alice reads %v", own)
	}

	// Other members fetch the file, and a player may ask for a range of it.
	world.router.Path(api.PathAgentAttachment).Methods(http.MethodGet).HandlerFunc(world.graph.agentAttachmentView)
	fetch := func(as *models.User, rangeHeader string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, strings.Replace(api.PathAgentAttachment, "{attachmentId}", recordingId, 1), nil)
		request.Header.Set(api.AuthenticatedUsernameHeader, as.Username)
		if rangeHeader != "" {
			request.Header.Set("Range", rangeHeader)
		}
		response := httptest.NewRecorder()
		world.router.ServeHTTP(response, request)
		return response
	}
	if response := fetch(world.bob, "bytes=4-7"); response.Code != http.StatusPartialContent || response.Body.String() != "real" || response.Header().Get("Content-Type") != "audio/mp4" {
		t.Errorf("Bob asking for a range got %d %q %q", response.Code, response.Body.String(), response.Header().Get("Content-Type"))
	}
	if response := fetch(world.carol, ""); response.Code != http.StatusNotFound {
		t.Errorf("someone outside the chat got %d", response.Code)
	}

	// Its words are not Alice's to change.
	voiceMessageId := message["id"].(string)
	if refusal := world.refused(t, world.alice, fmt.Sprintf(`mutation { EditChatMessage(messageId: %q, message: "something else") { id } }`, voiceMessageId)); !strings.Contains(refusal, "voice message cannot be changed") {
		t.Errorf("editing was refused with %q", refusal)
	}

	// A reply to it quotes what there is to quote.
	world.call(t, world.bob, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "see you then", references: [{agentMessageId: %q}]) { runId } }`, bobChat, voiceMessageId))
	reply := world.read(t, world.alice, aliceChat)[1]
	if quote := reply["references"].([]any)[0].(map[string]any)["quotedText"]; quote != "Voice message" {
		t.Errorf("a reply to a voice message with no words quotes %q", quote)
	}
	dbtest.Exec(t, world.database, `UPDATE "chat_message" SET "transcript_state" = 'done', "transcript_text" = 'Leaving the bakery now' WHERE "id" = '`+voiceMessageId+`'`)
	reply = world.read(t, world.alice, aliceChat)[1]
	if quote := reply["references"].([]any)[0].(map[string]any)["quotedText"]; quote != "Leaving the bakery now" {
		t.Errorf("a reply to a voice message quotes %q", quote)
	}

	// Deleting takes the words with it.
	world.call(t, world.alice, fmt.Sprintf(`mutation { DeleteChatMessage(messageId: %q) }`, voiceMessageId))
	deleted := world.read(t, world.bob, bobChat)[0]
	if deleted["isDeleted"] != true || deleted["isVoiceMessage"] != false || deleted["transcriptText"] != "" || deleted["transcriptState"] != "" || len(deleted["attachments"].([]any)) != 0 {
		t.Errorf("the deleted message reads %v", deleted)
	}
}

// Only a recording the caller uploaded as a voice message, alone, can be sent
// as one, and only in a chat with people.
func TestAVoiceMessageIsRefusedWhenItIsMoreThanOneRecording(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	bobChat := world.openDirect(t, world.bob, world.alice)
	recordingId := world.recording(t, world.alice, 3000)
	otherRecordingId := world.recording(t, world.alice, 3000)
	// A file uploaded the ordinary way has no length.
	plainFileId := world.recording(t, world.alice, 0)

	for name, query := range map[string]string{
		"with words":                        fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello", voiceMessageAttachmentId: %q) { runId } }`, aliceChat, recordingId),
		"with another file":                 fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", attachmentIds: [%q], voiceMessageAttachmentId: %q) { runId } }`, aliceChat, otherRecordingId, recordingId),
		"with a mention":                    fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", mentionedUserIds: [%q], voiceMessageAttachmentId: %q) { runId } }`, aliceChat, world.bob.ID, recordingId),
		"to everybody":                      fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", isAllMentioned: true, voiceMessageAttachmentId: %q) { runId } }`, aliceChat, recordingId),
		"addressing an agent":               fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", shouldAddressOwnAgent: true, voiceMessageAttachmentId: %q) { runId } }`, aliceChat, recordingId),
		"of a file that is not a recording": fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", voiceMessageAttachmentId: %q) { runId } }`, aliceChat, plainFileId),
		"of somebody else's":                fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", voiceMessageAttachmentId: %q) { runId } }`, bobChat, recordingId),
		"of a file that is not there":       fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", voiceMessageAttachmentId: "nothing") { runId } }`, aliceChat),
	} {
		sender := world.alice
		if strings.Contains(name, "somebody else's") {
			sender = world.bob
		}
		world.refused(t, sender, query)
		if messages := world.read(t, world.alice, aliceChat); len(messages) != 0 {
			t.Fatalf("%s: a message was posted anyway: %v", name, messages)
		}
	}

	// The recording named among the files as well is still one recording,
	// as the iPhone app sends it.
	world.call(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", attachmentIds: [%q], voiceMessageAttachmentId: %q) { runId } }`, aliceChat, otherRecordingId, otherRecordingId))
	if messages := world.read(t, world.bob, bobChat); len(messages) != 1 || messages[0]["isVoiceMessage"] != true {
		t.Fatalf("the recording named twice was posted as %v", messages)
	}

	// Outside a chat it is refused in plain words.
	if refusal := world.refused(t, world.frank, fmt.Sprintf(`mutation { AskAgent(message: "", voiceMessageAttachmentId: %q) { runId } }`, recordingId)); !strings.Contains(refusal, "voice message can only be sent in a chat") {
		t.Errorf("outside a chat it was refused with %q", refusal)
	}

	// After all that, the recording is still Alice's to send, once.
	world.sendVoiceMessage(t, world.alice, aliceChat, recordingId)
	world.refused(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "", voiceMessageAttachmentId: %q) { runId } }`, aliceChat, recordingId))
}

// transcriptionProvider answers every file transcription with the same words.
func transcriptionProvider(t *testing.T, words string) *httptest.Server {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_ = request.ParseMultipartForm(1 << 20)
		file, header, err := request.FormFile("file")
		if err != nil || header.Header.Get("Content-Type") != "audio/mp4" {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = file.Close()
		response.Header().Set("Content-Type", "application/json")
		encoded, _ := json.Marshal(map[string]any{"text": words, "usage": map[string]any{"type": "tokens", "input_tokens": 20, "output_tokens": 8}})
		_, _ = response.Write(encoded)
	}))
	t.Cleanup(provider.Close)
	return provider
}

// A voice message is posted waiting when an agent in the chat can pay (the
// sender's, or else another member's), the server writes its words once it is
// committed, and the other member is told the message changed; one left with
// no words is transcribed by a member who asks, paid by their agent.
func TestAVoiceMessageGetsItsWordsFromAnAgentInTheChat(t *testing.T) {
	world := newChatWorld(t)
	world.configuration.Agent.Enabled = true
	world.configuration.Agent.Features.Dreaming = new(bool)
	world.configuration.Agent.Voice.Enabled = true
	world.configuration.Agent.Providers = []config.AgentProvider{{Name: "spoken", Kind: config.AgentProviderKindOpenAI, BaseURL: transcriptionProvider(t, "Leaving the bakery now, see you at six").URL, APIKey: "k"}}
	registry, err := llm.Open(&world.configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	world.worker = agent.New(&agent.Settings{
		Database: world.database, Storage: world.files, Registry: registry, Configuration: func() *config.Configuration { return world.configuration },
		Instance: "chat-test", Tick: time.Hour, Presence: world.hub.Presence(), Phones: world.hub.Phones(), ChatEvents: world.hub,
	})
	t.Cleanup(world.worker.Stop)
	world.graph.settings.Agent = world.worker
	world.shareCircle(t, world.alice, "Home", world.frank)
	frankChat := world.openDirect(t, world.frank, world.alice)
	aliceChat := world.openDirect(t, world.alice, world.frank)
	events := world.follow(aliceChat)
	defer events.stop()

	world.sendVoiceMessage(t, world.frank, frankChat, world.recording(t, world.frank, 4000))
	posted := events.next(agent.EventSaid)
	if posted == nil || posted.Message.TranscriptState != models.VoiceTranscriptWaiting {
		t.Fatalf("Alice was first told %+v", posted)
	}
	edited := events.next(agent.EventEdited)
	if edited == nil || edited.Message.TranscriptState != models.VoiceTranscriptDone || edited.Message.TranscriptText != "Leaving the bakery now, see you at six" || edited.MessageID != posted.MessageID {
		t.Fatalf("Alice was then told %+v", edited)
	}
	if message := world.read(t, world.alice, aliceChat)[0]; message["transcriptText"] != "Leaving the bakery now, see you at six" || message["transcriptState"] != models.VoiceTranscriptDone {
		t.Errorf("Alice reads %v", message)
	}

	// Alice has no agent: Frank's, in the chat with her, pays for her words,
	// so the chat has them for whoever reads it.
	world.sendVoiceMessage(t, world.alice, aliceChat, world.recording(t, world.alice, 4000))
	if posted := events.next(agent.EventSaid); posted == nil || posted.Message.TranscriptState != models.VoiceTranscriptWaiting {
		t.Fatalf("Alice's own message was posted as %+v", posted)
	}
	if edited := events.next(agent.EventEdited); edited == nil || edited.Message.TranscriptText != "Leaving the bakery now, see you at six" {
		t.Fatalf("Alice's own message then became %+v", edited)
	}

	// One left with no words (nobody could pay when it was said) is
	// transcribed by asking: not by Alice, who has no agent to pay, but by
	// Frank, whose agent does; the words are the message's.
	offMessage := world.read(t, world.alice, aliceChat)[1]
	dbtest.Exec(t, world.database, fmt.Sprintf(`UPDATE "chat_message" SET "transcript_state" = 'off', "transcript_text" = '' WHERE "id" = '%s'`, offMessage["id"]))
	transcribe := fmt.Sprintf(`mutation { TranscribeChatVoiceMessage(messageId: %q) { id transcriptState transcriptText } }`, offMessage["id"])
	if refusal := world.refused(t, world.alice, transcribe); !strings.Contains(refusal, "your agent cannot transcribe") {
		t.Errorf("Alice's ask was refused with %q", refusal)
	}
	written := world.call(t, world.frank, transcribe)["TranscribeChatVoiceMessage"].(map[string]any)
	if written["transcriptState"] != models.VoiceTranscriptDone || written["transcriptText"] != "Leaving the bakery now, see you at six" {
		t.Fatalf("Frank's ask wrote %v", written)
	}
	if message := world.read(t, world.alice, aliceChat)[1]; message["transcriptState"] != models.VoiceTranscriptDone || message["transcriptText"] != "Leaving the bakery now, see you at six" {
		t.Errorf("Alice then reads %v", message)
	}
	// A message with words keeps them.
	if again := world.call(t, world.frank, transcribe)["TranscribeChatVoiceMessage"].(map[string]any); again["transcriptState"] != models.VoiceTranscriptDone {
		t.Errorf("asked again: %v", again)
	}
}

// The upload prepares the recording: a real sound is kept as an m4a with its
// length and waveform; one too short or not sound at all is refused in plain
// words; with no ffmpeg the server says so. Without the parameter an upload is
// left as it is.
func TestUploadingAVoiceMessagePreparesTheRecording(t *testing.T) {
	if !voicemessage.IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	world := newChatWorld(t)
	world.router.Path(api.PathAgentAttachments).Methods(http.MethodPost).HandlerFunc(world.graph.agentAttachmentsView)
	tone := func(seconds float64) []byte {
		output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.FormatFloat(seconds, 'f', 2, 64), "-c:a", "libopus", "-f", "webm", "pipe:1").Output()
		if err != nil {
			t.Fatalf("ffmpeg: %s", err)
		}
		return output
	}
	upload := func(as *models.User, query, filename string, content []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(content)
		_ = writer.Close()
		request := httptest.NewRequest(http.MethodPost, api.PathAgentAttachments+query, &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set(api.AuthenticatedUsernameHeader, as.Username)
		response := httptest.NewRecorder()
		world.router.ServeHTTP(response, request)
		return response
	}

	// Bob may chat and his agent is off: he can still send a voice message.
	response := upload(world.bob, "?"+api.AgentAttachmentVoiceMessageParameter+"=true", "recording.webm", tone(2))
	if response.Code != http.StatusOK {
		t.Fatalf("the upload got %d: %s", response.Code, response.Body.String())
	}
	var result AgentAttachmentUploadResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Attachments) != 1 {
		t.Fatalf("the upload answered %s", response.Body.String())
	}
	stored := result.Attachments[0]
	if stored.Name != "Voice message.m4a" || stored.ContentType != "audio/mp4" || stored.AudioDurationMS < 1900 || stored.AudioDurationMS > 2200 || stored.AudioWaveform == "" {
		t.Errorf("the recording was stored as %+v", stored)
	}
	content, err := world.files.GetFile(t.Context(), stored.ID)
	if err != nil || !bytes.Contains(content[:64], []byte("ftyp")) || int64(len(content)) != stored.Size {
		t.Errorf("the kept bytes are an m4a of the size said: %v, %d bytes", err, len(content))
	}

	for name, upload := range map[string]*httptest.ResponseRecorder{
		"too short":   upload(world.bob, "?"+api.AgentAttachmentVoiceMessageParameter+"=true", "recording.webm", tone(0.2)),
		"not a sound": upload(world.bob, "?"+api.AgentAttachmentVoiceMessageParameter+"=true", "note.txt", []byte("hello")),
	} {
		if upload.Code != http.StatusUnprocessableEntity || !strings.Contains(upload.Body.String(), "error") {
			t.Errorf("%s got %d: %s", name, upload.Code, upload.Body.String())
		}
	}
	if refusal := upload(world.bob, "?"+api.AgentAttachmentVoiceMessageParameter+"=true", "recording.webm", tone(0.2)).Body.String(); !strings.Contains(refusal, "between half a second and two minutes") {
		t.Errorf("a recording that is too short was refused with %s", refusal)
	}

	// Left out, the parameter leaves the file as it came.
	plain := upload(world.bob, "", "recording.webm", tone(2))
	var plainResult AgentAttachmentUploadResult
	if err := json.Unmarshal(plain.Body.Bytes(), &plainResult); err != nil || len(plainResult.Attachments) != 1 || plainResult.Attachments[0].AudioDurationMS != 0 || plainResult.Attachments[0].Name != "recording.webm" {
		t.Errorf("an ordinary upload answered %s", plain.Body.String())
	}
}

// The composer is offered the microphone for voice messages whenever ffmpeg is
// there, with the agent on or off, and with the permission to use an agent or
// only to chat.
func TestVoiceMessageAvailabilityDoesNotDependOnTheAgent(t *testing.T) {
	world := newChatWorld(t)
	want := voicemessage.IsAvailable()
	for _, person := range []*models.User{world.frank, world.grace, world.bob} {
		data := world.call(t, person, `query { ReadAgentVoice { isVoiceAvailable isVoiceMessageAvailable } }`)
		view := data["ReadAgentVoice"].(map[string]any)
		if view["isVoiceMessageAvailable"] != want || view["isVoiceAvailable"] != false {
			t.Errorf("%s is told %v, want voice messages %v and no voice", person.Username, view, want)
		}
	}
}
