package agent

import (
	"encoding/json"
	"sync/atomic"
)

// The tab relay: the person's own browser tab, attached through the
// extension over the server's websocket. The extension does the acting in
// the page; the server only carries a request across and waits for the
// answer (device.go). Ask only — nobody is watching a run with nobody
// present — and the refusals that matter are the extension's, not the
// model's.

// TabConnection is what the relay needs of the extension's websocket.
type TabConnection = DeviceConnection

// tabMessage is what goes over the relay, either way.
type tabMessage = deviceMessage

// attachedTab is one person's browser, connected through the extension:
// with a tab the actions go to, or, until the agent opens one or the person
// attaches one, with none.
type attachedTab struct {
	*deviceLink
	title string
	url   string

	// hasTab says there is a tab the actions go to: the person's own,
	// attached, or one the agent opened. Read by the tool while the socket
	// says otherwise.
	hasTab atomic.Bool
}

// AttachTab records a person's tab; a second attach replaces the first.
func (self *Agent) AttachTab(agentId string, connection TabConnection, title, url string) {
	self.ConnectBrowser(agentId, connection, title, url, true)
}

// ConnectBrowser records a person's browser, connected through the
// extension, with a tab to act in or with none yet; a second connection
// replaces the first.
func (self *Agent) ConnectBrowser(agentId string, connection TabConnection, title, url string, hasTab bool) {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if self.tabs == nil {
		self.tabs = map[string]*attachedTab{}
	}
	// A tab replacing another answers for nothing the other was asked:
	// whoever waits on the old one is told it went.
	if previous := self.tabs[agentId]; previous != nil {
		previous.drop()
	}
	connected := &attachedTab{deviceLink: newDeviceLink("the person's browser", connection), title: title, url: url}
	connected.hasTab.Store(hasTab)
	self.tabs[agentId] = connected
}

// SetTabHeld is the browser saying whether it has a tab the actions go to.
func (self *Agent) SetTabHeld(agentId string, hasTab bool) {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	if tab := self.tabs[agentId]; tab != nil {
		tab.hasTab.Store(hasTab)
	}
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
		tab.drop()
		delete(self.tabs, agentId)
	}
}

// TabAnswered is the extension answering a request: the extension on this
// connection, so that a replaced tab's late answers reach nobody.
func (self *Agent) TabAnswered(agentId string, connection TabConnection, id int64, ok bool, data json.RawMessage, failure string) {
	if tab := self.tabFor(agentId); tab != nil && (connection == nil || tab.connection == connection) {
		tab.answered(id, ok, data, failure)
	}
}

func (self *Agent) tabFor(agentId string) *attachedTab {
	self.tabsMutex.Lock()
	defer self.tabsMutex.Unlock()
	return self.tabs[agentId]
}

// TabAttached says whether a person has a tab to act in, for the API.
func (self *Agent) TabAttached(agentId string) (bool, string, string) {
	tab := self.tabFor(agentId)
	if tab == nil || !tab.HasTab() {
		return false, "", ""
	}
	return true, tab.title, tab.url
}

// BrowserConnected says whether a person's browser is connected through the
// extension, with a tab to act in or not.
func (self *Agent) BrowserConnected(agentId string) bool {
	return self.tabFor(agentId) != nil
}

// Title and URL are what the tab said it shows, for the tool and the
// overlay.
func (self *attachedTab) Title() string { return self.title }
func (self *attachedTab) URL() string   { return self.url }

// HasTab says whether there is a tab the actions go to.
func (self *attachedTab) HasTab() bool {
	return self.hasTab.Load()
}
