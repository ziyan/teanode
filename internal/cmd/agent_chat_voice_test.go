package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// A voice message is shown as its length and what was heard in it, or that
// the words are still coming or will not come, and its recording is not listed
// as a file beside it.
func TestAVoiceMessageIsShownWithItsLengthAndWords(test *testing.T) {
	test.Parallel()

	recording := []client.AgentAttachment{{ID: "f1", Name: "Voice message.m4a", ContentType: "audio/mp4", AudioDurationMS: 6800}}
	for name, testCase := range map[string]struct {
		message *client.AgentMessage
		want    string
	}{
		"words": {
			&client.AgentMessage{ID: "m1", Attachments: recording, IsVoiceMessage: true, TranscriptState: "done", TranscriptText: "Running ten minutes late"},
			"Mira: (voice message 0:07) Running ten minutes late  m1",
		},
		"waiting": {
			&client.AgentMessage{ID: "m1", Attachments: recording, IsVoiceMessage: true, TranscriptState: "waiting"},
			"Mira: (voice message 0:07) (transcribing)  m1",
		},
		"off": {
			&client.AgentMessage{ID: "m1", Attachments: recording, IsVoiceMessage: true, TranscriptState: "off"},
			"Mira: (voice message 0:07) (no transcript)  m1",
		},
		"failed": {
			&client.AgentMessage{ID: "m1", Attachments: recording, IsVoiceMessage: true, TranscriptState: "failed"},
			"Mira: (voice message 0:07) (no transcript)  m1",
		},
	} {
		testCase.message.CreatedAt = time.Now()
		testCase.message.Role = "user"
		testCase.message.AuthorKind, testCase.message.AuthorUserID, testCase.message.AuthorName = "person", "u2", "Mira"
		if !isChatLine(testCase.message) {
			test.Errorf("%s: a voice message is something said", name)
		}
		printed := chatMessageLines(testCase.message)
		if !strings.Contains(printed, testCase.want) {
			test.Errorf("%s: the line is %q, want it to hold %q", name, printed, testCase.want)
		}
		if strings.Contains(printed, "(file") {
			test.Errorf("%s: the recording is listed as a file: %q", name, printed)
		}
	}
}

// --voice-message uploads the recording with the voice message parameter and
// sends its id with no words; a message, a file, an agent, a mention, or no
// chat at all is refused before anything is uploaded.
func TestSendingAVoiceMessageFromTheCommandLine(test *testing.T) {
	test.Parallel()

	var lock sync.Mutex
	var uploadedQuery, asked string
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		lock.Lock()
		defer lock.Unlock()
		if request.URL.Path == api.PathAgentAttachments {
			uploads++
			uploadedQuery = request.URL.RawQuery
			_, _ = response.Write([]byte(`{"attachments":[{"id":"voice-file","name":"Voice message.m4a","contentType":"audio/mp4","size":10,"audioDurationMS":4000}]}`))
			return
		}
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(request.Body)
		asked = body.String()
		_, _ = response.Write([]byte(`{"data":{"AskAgent":{"runId":"","conversationId":"c1"}}}`))
	}))
	defer server.Close()
	recordingPath := filepath.Join(test.TempDir(), "note.m4a")
	if err := os.WriteFile(recordingPath, []byte("not really sound"), 0o600); err != nil {
		test.Fatal(err)
	}
	written := &bytes.Buffer{}
	run := func(arguments ...string) error {
		written.Reset()
		command := &cli.Command{
			Name:     "fixture",
			Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "tnt_x"}},
			Commands: []*cli.Command{NewAgentCommand()},
			Writer:   written,
		}
		return command.Run(test.Context(), append([]string{"fixture", "agent", "ask"}, arguments...))
	}

	if err := run("--conversation", "c1", "--voice-message", recordingPath); err != nil {
		test.Fatal(err)
	}
	lock.Lock()
	if uploadedQuery != api.AgentAttachmentVoiceMessageParameter+"=true" {
		test.Errorf("the recording was uploaded with %q", uploadedQuery)
	}
	if !strings.Contains(asked, `"voiceMessageAttachmentId":"voice-file"`) || !strings.Contains(asked, `"message":""`) || !strings.Contains(asked, `"conversationId":"c1"`) {
		test.Errorf("the server was asked %s", asked)
	}
	lock.Unlock()
	if written.String() != "sent in c1\n" {
		test.Errorf("the command printed %q", written.String())
	}

	lock.Lock()
	uploads = 0
	lock.Unlock()
	for name, arguments := range map[string][]string{
		"a message":      {"--conversation", "c1", "--voice-message", recordingPath, "hello"},
		"a file":         {"--conversation", "c1", "--voice-message", recordingPath, "--attach", recordingPath},
		"an agent":       {"--conversation", "c1", "--voice-message", recordingPath, "--address", "maria"},
		"your own agent": {"--conversation", "c1", "--voice-message", recordingPath, "--ask-agent"},
		"a mention":      {"--conversation", "c1", "--voice-message", recordingPath, "--mention", "maria"},
		"no chat":        {"--voice-message", recordingPath},
		"a new one":      {"--conversation", "c1", "--new", "--voice-message", recordingPath},
		"a missing file": {"--conversation", "c1", "--voice-message", recordingPath + ".missing"},
	} {
		if err := run(arguments...); err == nil {
			test.Errorf("%s: a voice message with it was not refused", name)
		}
	}
	lock.Lock()
	defer lock.Unlock()
	if uploads != 0 {
		test.Errorf("%d recording(s) were uploaded for a command that was refused", uploads)
	}
}

// The voice message is on the API, the command line's document and its flag
// under the same name.
func TestVoiceMessageIsOnEverySurfaceUnderOneName(test *testing.T) {
	test.Parallel()

	member, found := reflect.TypeFor[apigraph.AskAgentArguments]().FieldByName("VoiceMessageAttachmentID")
	if !found || strings.Split(member.Tag.Get("json"), ",")[0] != "voiceMessageAttachmentId" {
		test.Error("AskAgent does not take voiceMessageAttachmentId")
	}
	if !strings.Contains(client.DocumentAskAgent, "$voiceMessageAttachmentId:") {
		test.Error("the command line's AskAgent document does not send voiceMessageAttachmentId")
	}
	if flagNamed(commandNamed(test, NewAgentCommand(), "ask"), "voice-message") == nil {
		test.Error("teanode agent ask takes no --voice-message")
	}
	if !strings.Contains(client.DocumentReadAgentConversation, "transcriptState") || !strings.Contains(client.DocumentReadAgentConversation, "audioDurationMS") {
		test.Error("the command line does not read a voice message's length and words")
	}
}
