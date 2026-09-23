package computer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
)

// relayComputer answers the way an attached computer does, by running the
// program's own code for the request, so the two ends are tested together.
type relayComputer struct {
	fakeComputer
	older bool
}

func (self *relayComputer) Ask(ctx context.Context, action string, args any, _ time.Duration) (json.RawMessage, error) {
	if action != "http" || self.older {
		return nil, errors.New(`"http" is not something this program does`)
	}
	encoded, _ := json.Marshal(args)
	var arguments computer.HTTPArguments
	if err := json.Unmarshal(encoded, &arguments); err != nil {
		return nil, err
	}
	result, err := computer.RunHTTP(ctx, &arguments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// A request through a computer comes back as an ordinary answer: status,
// headers and body, over the websocket's JSON and back.
func TestARequestThroughAComputerComesBackWhole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		response.Header().Set("Content-Type", "text/plain")
		response.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(response, request.Method+" "+string(body))
	}))
	defer server.Close()

	client := HTTPClient(&relayComputer{})
	response, err := client.Post(server.URL, "text/plain", strings.NewReader("sent"))
	if err != nil {
		t.Fatalf("Post: %s", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusCreated || string(body) != "POST sent" || response.Header.Get("Content-Type") != "text/plain" {
		t.Errorf("got %d %q %q", response.StatusCode, body, response.Header.Get("Content-Type"))
	}
}

// A computer running an older program is named, with what to do about it.
func TestAnOlderComputerSaysToUpdate(t *testing.T) {
	_, err := HTTPClient(&relayComputer{older: true}).Get("http://example.com/")
	if err == nil || !strings.Contains(err.Error(), "update it there") {
		t.Errorf("the error was %v", err)
	}
}

// truncatingComputer answers as the program does when an answer was longer
// than it would read.
type truncatingComputer struct{ fakeComputer }

func (self *truncatingComputer) Ask(context.Context, string, any, time.Duration) (json.RawMessage, error) {
	return json.Marshal(&computer.HTTPResult{Status: 200, Body: []byte("the start of it"), Truncated: true})
}

// An answer cut short is an error, never a response: a part of a file saved
// as the file is a corrupt file.
func TestAnAnswerCutShortIsAnError(t *testing.T) {
	if _, err := HTTPClient(&truncatingComputer{}).Get("http://example.com/large"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("a cut answer came back as %v", err)
	}
}
