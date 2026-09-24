package webfetch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	daemon "github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
)

// deskComputer is an attached computer that makes requests the way the
// program on it does.
type deskComputer struct{}

func (deskComputer) Ask(ctx context.Context, action string, args any, _ time.Duration) (json.RawMessage, error) {
	encoded, _ := json.Marshal(args)
	var arguments daemon.HTTPArguments
	if err := json.Unmarshal(encoded, &arguments); err != nil {
		return nil, err
	}
	result, err := daemon.RunHTTP(ctx, &arguments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func (deskComputer) Name() string        { return "desk" }
func (deskComputer) System() string      { return "linux" }
func (deskComputer) Home() string        { return "/home/someone" }
func (deskComputer) Description() string { return "" }

type deskRun struct {
	tools.Run
	configuration *config.Configuration
}

func (self *deskRun) AttachedComputers() []tools.Computer  { return []tools.Computer{deskComputer{}} }
func (self *deskRun) ComputersAllowed() bool               { return true }
func (self *deskRun) ComputersUnattended() bool            { return false }
func (self *deskRun) Headless() bool                       { return false }
func (self *deskRun) CanAsk() bool                         { return !self.Headless() }
func (self *deskRun) Configuration() *config.Configuration { return self.configuration }

// An address only the person's network reaches is refused through this
// server and read through their computer.
//
// The test server listens on loopback, which the server's own fetcher
// refuses as private; from the computer it is simply an address on its
// network, which is what going through one is for.
func TestAPrivateAddressIsReadThroughAComputerAndRefusedThroughThisServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(response, "<html><title>Inside</title><body>only from the desk</body></html>")
	}))
	defer server.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	ctx := tools.WithRun(context.Background(), &deskRun{configuration: configuration})

	arguments, _ := json.Marshal(map[string]any{"url": server.URL, "computer": "desk"})
	result, err := runWebFetch(ctx, &tools.Call{Arguments: arguments})
	if err != nil {
		t.Fatalf("through the computer: %s", err)
	}
	if !strings.Contains(result.Content, "only from the desk") || !strings.Contains(result.Note, "through desk") {
		t.Errorf("read %q, noted %q", result.Content, result.Note)
	}

	direct, _ := json.Marshal(map[string]any{"url": server.URL})
	if _, err := runWebFetch(ctx, &tools.Call{Arguments: direct}); err == nil {
		t.Error("a loopback address was fetched through this server")
	}
}

// Going through a computer asks first, as reaching out with the shell does;
// a public page through this server does not.
func TestGoingThroughAComputerAsksFirst(t *testing.T) {
	if risk := webFetchRisk(json.RawMessage(`{"url":"https://example.com"}`)); risk != tools.RiskRead {
		t.Errorf("a fetch through this server is %v", risk)
	}
	if risk := webFetchRisk(json.RawMessage(`{"url":"https://example.com","computer":"desk"}`)); risk != tools.RiskWrite {
		t.Errorf("a fetch through a computer is %v", risk)
	}
}
