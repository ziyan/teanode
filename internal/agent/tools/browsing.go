package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/browser"
)

// Browsing is what a run offers the browser tool: a page of its own in
// the operator's headless browser, and the person's own attached tab when
// they attached one. A run that has no browser does not implement it.
type Browsing interface {
	// BrowserPage is the turn's page, opened on first use.
	BrowserPage(ctx context.Context) (*browser.Context, error)

	// AttachedTab is the person's tab, or nil.
	AttachedTab() Tab

	// TabsAllowed says whether the operator lets people attach a tab.
	TabsAllowed() bool

	// ReconnectedTab waits up to wait for the person's browser to connect
	// again after the connection behind dropped went, and is the new one,
	// or nil when none came. It only waits: connecting again is the
	// extension's own doing.
	ReconnectedTab(ctx context.Context, dropped Tab, wait time.Duration) Tab
}

// Rendering is a run that can open a page of its own in the operator's
// headless browser for one read, apart from the turn's page, so that a
// read never moves what the browser tool is on. The function closes it.
type Rendering interface {
	RenderingPage(ctx context.Context) (*browser.Context, func(), error)
}

// ErrDeviceDetached is a device's connection ending while a request to it
// was open, or before the request could be sent: whether the device did
// what it was asked is not known.
var ErrDeviceDetached = errors.New("was detached")

// Tab is a person's attached browser tab as the tool drives it.
type Tab interface {
	Ask(ctx context.Context, action string, args any) (json.RawMessage, error)
	Title() string
	URL() string
	// HasTab says there is a tab to act in: a browser connected through
	// the extension may have none until one is opened or attached.
	HasTab() bool
	// IsTakenOver says the person has taken the tab over from the
	// dashboard: it may be read, not acted in, until they hand it back.
	IsTakenOver() bool
	// IsWatchable says the person can watch and take the tab over from the
	// dashboard: their extension is new enough.
	IsWatchable() bool
	// CanFillPasswords says the extension can fill a login, a one-time code
	// or a passkey that the server builds the arguments for (protocol 5).
	CanFillPasswords() bool
	// Filled is what the agent's sign-in put into the person's browser, kept
	// in memory: the values to hide from the model and which tabs are
	// locked against what could read a field back.
	Filled() *browser.FilledValues
	// CurrentTabID is the number the extension gave the tab the actions go
	// to, or empty when it did not say. A lock is kept for a tab by it.
	CurrentTabID() string
	// NoteCurrentTab records that an answer of the extension said the
	// actions now go to this tab, without waiting for its next update.
	NoteCurrentTab(tab string)
	// ServerHost is the host, with its port, that the person's browser
	// reached this server by when it connected: a name the person is signed
	// in to, whatever the configuration calls the server.
	ServerHost() string
	// ListedTabs are the tabs the person's browser last listed as ones the
	// agent may act in, each with the address it shows.
	ListedTabs() []TabAddress
}

// TabAddress is one tab the person's browser listed: its number, as the
// browser tool names it, and what it shows.
type TabAddress struct {
	Tab       string
	Title     string
	URL       string
	IsOwn     bool
	IsCurrent bool
}

// TakeOverAsker is a turn that can ask the person to take their browser tab
// over from the dashboard and wait until they hand it back. Only a turn the
// person can answer is one.
type TakeOverAsker interface {
	AskToTakeOver(ctx context.Context, callId, request string) (string, error)
}
