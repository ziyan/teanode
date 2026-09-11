package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// The tab relay: the person's own browser tab, attached through the
// extension over the dashboard's websocket. The extension does the acting
// in the page; the server only carries a request across and waits for the
// answer. Ask only — nobody is watching a run with nobody present — and
// the refusals that matter are the extension's, not the model's.

// TabConnection is what the relay needs of a websocket: a way to send a
// request to the extension and a way to receive its answers.
type TabConnection interface {
	Send(message []byte) error
}

// attachedTab is one person's attached tab.
type attachedTab struct {
	connection TabConnection
	title      string
	url        string
	attachedAt time.Time

	mutex   sync.Mutex
	next    int64
	pending map[int64]chan tabAnswer
}

type tabAnswer struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// tabMessage is what goes over the relay, either way.
type tabMessage struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id,omitempty"`
	Action string          `json:"action,omitempty"`
	Args   json.RawMessage `json:"args,omitempty"`
	Title  string          `json:"title,omitempty"`
	URL    string          `json:"url,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// AttachTab records a person's tab; a second attach replaces the first.
func (self *Agent) AttachTab(agentId string, connection TabConnection, title, url string) {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if self.tabs == nil {
		self.tabs = map[string]*attachedTab{}
	}
	self.tabs[agentId] = &attachedTab{connection: connection, title: title, url: url, attachedAt: time.Now(), pending: map[int64]chan tabAnswer{}}
}

// UpdateTab is the tab saying where it went.
func (self *Agent) UpdateTab(agentId, title, url string) {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if tab := self.tabs[agentId]; tab != nil {
		tab.title, tab.url = title, url
	}
}

// DetachTab forgets a person's tab, if this connection is the one attached.
func (self *Agent) DetachTab(agentId string, connection TabConnection) {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if tab := self.tabs[agentId]; tab != nil && (connection == nil || tab.connection == connection) {
		tab.mutex.Lock()
		for id, channel := range tab.pending {
			delete(tab.pending, id)
			close(channel)
		}
		tab.mutex.Unlock()
		delete(self.tabs, agentId)
	}
}

// TabAnswered is the extension answering a request.
func (self *Agent) TabAnswered(agentId string, id int64, ok bool, data json.RawMessage, failure string) {
	self.tabsMutex.Lock()
	tab := self.tabs[agentId]
	self.tabsMutex.Unlock()
	if tab == nil {
		return
	}
	tab.mutex.Lock()
	channel, found := tab.pending[id]
	if found {
		delete(tab.pending, id)
	}
	tab.mutex.Unlock()
	if found {
		channel <- tabAnswer{OK: ok, Data: data, Error: failure}
	}
}

func (self *Agent) tabFor(agentId string) *attachedTab {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	return self.tabs[agentId]
}

// TabAttached says whether a person has a tab attached, for the API.
func (self *Agent) TabAttached(agentId string) (bool, string, string) {
	tab := self.tabFor(agentId)
	if tab == nil {
		return false, "", ""
	}
	return true, tab.title, tab.url
}

// ask sends an action to the tab and waits for its answer.
func (self *attachedTab) ask(ctx context.Context, action string, args any) (json.RawMessage, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	self.mutex.Lock()
	self.next++
	id := self.next
	channel := make(chan tabAnswer, 1)
	self.pending[id] = channel
	self.mutex.Unlock()
	message, _ := json.Marshal(tabMessage{Type: "act", ID: id, Action: action, Args: encoded})
	if err := self.connection.Send(message); err != nil {
		self.mutex.Lock()
		delete(self.pending, id)
		self.mutex.Unlock()
		return nil, fmt.Errorf("the attached tab is gone: %w", err)
	}
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case answer, ok := <-channel:
		if !ok {
			return nil, fmt.Errorf("the tab was detached")
		}
		if !answer.OK {
			return nil, fmt.Errorf("the tab refused: %s", answer.Error)
		}
		return answer.Data, nil
	case <-timer.C:
		self.mutex.Lock()
		delete(self.pending, id)
		self.mutex.Unlock()
		return nil, fmt.Errorf("the tab did not answer within a minute")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runBrowserOnTab carries a browser action to the person's tab.
func runBrowserOnTab(ctx context.Context, run *AskRun, arguments *browserArguments) (*Result, error) {
	if run.settings.Headless {
		return nil, fmt.Errorf("a run with nobody present cannot use the person's tab")
	}
	if attach := run.agent.settings.Configuration().Agent.Browser.AttachTabs; attach != nil && !*attach {
		return nil, fmt.Errorf("attaching a tab is off on this server")
	}
	tab := run.agent.tabFor(run.settings.Agent.ID)
	if tab == nil {
		return nil, fmt.Errorf("no tab is attached; ask the person to attach one with the extension, or use the headless browser")
	}
	switch arguments.Action {
	case "navigate", "snapshot", "screenshot", "click", "hover", "select", "type", "press", "scroll", "wait", "back", "evaluate", "fetch", "storage":
	default:
		return nil, fmt.Errorf("%q is not an action a tab does", arguments.Action)
	}
	data, err := tab.ask(ctx, arguments.Action, arguments)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if len(text) > askResultCharacters {
		text = text[:askResultCharacters] + "\n[cut here: the answer goes on]"
	}
	return &Result{Content: text, Untrusted: true, Note: "in the attached tab: " + arguments.Action}, nil
}
