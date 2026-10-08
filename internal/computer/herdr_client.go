package computer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

// The herdr socket, as this program speaks to it.
//
// Herdr is a terminal workspace manager for coding agents: it holds the
// person's terminals in panes, recognizes the Claude Code or Codex session
// running in each, and answers on a Unix socket. Each request is one line
// of JSON, {"id","method","params"}, and its answer is the line carrying
// the same id. One connection per request: herdr answers quickly, and a
// connection left open would be one more thing to watch for herdr
// restarting.

// herdrSocketWait bounds one request to herdr. Herdr answers from memory,
// so a socket slower than this is one that is not answering.
const herdrSocketWait = 10 * time.Second

// errHerdrNotRunning is a computer with no herdr running for this person.
var errHerdrNotRunning = errors.New("herdr is not running for this person on this computer")

// herdrClient talks to one herdr server.
type herdrClient struct {
	socketPath   string
	requestCount atomic.Int64
}

// newHerdrClient is the client for herdr's default socket under home.
func newHerdrClient(home string) *herdrClient {
	return &herdrClient{socketPath: filepath.Join(home, ".config", "herdr", "herdr.sock")}
}

// herdrAgent is one recognized coding agent as herdr says it, in herdr's
// own names, which mirror its contract.
type herdrAgent struct {
	PaneID                string `json:"pane_id"`
	WorkspaceID           string `json:"workspace_id"`
	TabID                 string `json:"tab_id"`
	Agent                 string `json:"agent"`
	AgentStatus           string `json:"agent_status"`
	Name                  string `json:"name"`
	Cwd                   string `json:"cwd"`
	ForegroundCwd         string `json:"foreground_cwd"`
	TerminalTitleStripped string `json:"terminal_title_stripped"`
	AgentSession          *struct {
		Value string `json:"value"`
	} `json:"agent_session"`
}

// herdrWorkspace and herdrTab are where a pane is, in herdr's own names:
// the labels the person gave them, which is how they find a pane.
type herdrWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	TabCount    int    `json:"tab_count"`
}

type herdrTab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// listWorkspaces is every workspace.
func (self *herdrClient) listWorkspaces(ctx context.Context) ([]*herdrWorkspace, error) {
	var result struct {
		Workspaces []*herdrWorkspace `json:"workspaces"`
	}
	if err := self.call(ctx, "workspace.list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Workspaces, nil
}

// listTabs is every tab of every workspace.
func (self *herdrClient) listTabs(ctx context.Context) ([]*herdrTab, error) {
	var result struct {
		Tabs []*herdrTab `json:"tabs"`
	}
	if err := self.call(ctx, "tab.list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tabs, nil
}

// herdrError is herdr refusing a request, in its own words.
type herdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (self *herdrError) Error() string {
	return self.Code + ": " + self.Message
}

// call sends one request and decodes the answer's result into result.
func (self *herdrClient) call(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, herdrSocketWait)
	defer cancel()
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "unix", self.socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			return errHerdrNotRunning
		}
		return fmt.Errorf("cannot reach herdr: %w", err)
	}
	defer func() { _ = connection.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	id := "teanode:" + strconv.FormatInt(self.requestCount.Add(1), 10)
	request, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := connection.Write(append(request, '\n')); err != nil {
		return fmt.Errorf("cannot write to herdr: %w", err)
	}
	reader := bufio.NewReader(connection)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("herdr did not answer %s: %w", method, err)
		}
		var answer struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *herdrError     `json:"error"`
		}
		// A request herdr could not read is refused with no id, since it
		// could not read the id either; on a connection of its own, that
		// refusal is this request's.
		if err := json.Unmarshal(line, &answer); err != nil || (answer.ID != id && (answer.ID != "" || answer.Error == nil)) {
			continue
		}
		if answer.Error != nil {
			return answer.Error
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(answer.Result, result); err != nil {
			return fmt.Errorf("herdr answered %s unreadably: %w", method, err)
		}
		return nil
	}
}

// listAgents is every coding agent herdr recognizes, in every workspace.
func (self *herdrClient) listAgents(ctx context.Context) ([]*herdrAgent, error) {
	var result struct {
		Agents []*herdrAgent `json:"agents"`
	}
	if err := self.call(ctx, "agent.list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Agents, nil
}

// readAgent is the text of the pane an agent is in: "visible" is the
// screen as it stands, "recent_unwrapped" the last lines with wrapped ones
// joined.
func (self *herdrClient) readAgent(ctx context.Context, paneId, source string, lineCount int) (string, error) {
	params := map[string]any{"target": paneId, "source": source}
	if lineCount > 0 {
		params["lines"] = lineCount
	}
	var result struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	if err := self.call(ctx, "agent.read", params, &result); err != nil {
		return "", err
	}
	return result.Read.Text, nil
}

// sendKeys presses named keys in the pane, in order. Herdr checks every
// name before it writes any.
func (self *herdrClient) sendKeys(ctx context.Context, paneId string, keys []string) error {
	return self.call(ctx, "agent.send_keys", map[string]any{"target": paneId, "keys": keys}, nil)
}

// sendText types text into the pane as it is, with no Enter after it.
func (self *herdrClient) sendText(ctx context.Context, paneId, text string) error {
	return self.call(ctx, "pane.send_text", map[string]any{"pane_id": paneId, "text": text}, nil)
}
