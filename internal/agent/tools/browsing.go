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
}

// TakeOverAsker is a turn that can ask the person to take their browser tab
// over from the dashboard and wait until they hand it back. Only a turn the
// person can answer is one.
type TakeOverAsker interface {
	AskToTakeOver(ctx context.Context, callId, request string) (string, error)
}
