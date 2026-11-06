package apigraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/voice"
)

// A piece of a message comes back as a WAV file in the voice the person chose.
// With voice off the client is told, and reads on the device instead.
func TestAPieceOfAMessageIsSaidInThePersonsVoice(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "reader", Name: "Example Reader"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		grantMCPPermissions(t, tx, person, models.PermissionAgentUse)
		if _, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie", SpeechVoice: "cedar"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	var spoken map[string]any
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewDecoder(request.Body).Decode(&spoken)
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`{"type":"speech.audio.delta","audio":"AQACAA=="}`,
			`{"type":"speech.audio.done","usage":{"input_tokens":12,"output_tokens":30}}`,
		} {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
	}))
	defer provider.Close()

	read := func(isVoiceEnabled, isSignedIn bool, body string) *httptest.ResponseRecorder {
		configuration := config.Default()
		configuration.Agent.Enabled = true
		configuration.Agent.Providers = []config.AgentProvider{{Name: "spoken", Kind: config.AgentProviderKindOpenAI, BaseURL: provider.URL + "/v1", APIKey: "k"}}
		configuration.Agent.Voice = config.AgentVoice{Enabled: isVoiceEnabled}
		resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{}}
		request := httptest.NewRequest(http.MethodPost, api.PathAgentVoiceRead, strings.NewReader(body))
		if isSignedIn {
			request.Header.Set(api.AuthenticatedUsernameHeader, "reader")
		}
		response := httptest.NewRecorder()
		resolver.voiceReadView(response, request)
		return response
	}

	said := read(true, true, `{"answerText":"Leave at nine."}`)
	if said.Code != http.StatusOK || said.Header().Get("Content-Type") != "audio/wav" || !strings.HasPrefix(said.Body.String(), "RIFF") {
		t.Fatalf("%d %q", said.Code, said.Header().Get("Content-Type"))
	}
	if said.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("somebody's words were left to be cached: %q", said.Header().Get("Cache-Control"))
	}
	if spoken["voice"] != "cedar" || spoken["input"] != "Leave at nine." {
		t.Fatalf("the provider was asked for %+v", spoken)
	}
	for name, test := range map[string]struct {
		response *httptest.ResponseRecorder
		want     int
	}{
		"voice off":   {read(false, true, `{"answerText":"Leave at nine."}`), http.StatusForbidden},
		"signed out":  {read(true, false, `{"answerText":"Leave at nine."}`), http.StatusUnauthorized},
		"no words":    {read(true, true, `{"answerText":"   "}`), http.StatusBadRequest},
		"not a piece": {read(true, true, `{"answerText":"`+strings.Repeat("a", voice.MaximumAnswerTextLength+1)+`"}`), http.StatusBadRequest},
		"not JSON":    {read(true, true, `words`), http.StatusBadRequest},
	} {
		if test.response.Code != test.want {
			t.Errorf("%s: %d, want %d", name, test.response.Code, test.want)
		}
	}
}
