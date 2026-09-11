package tools

import (
	"context"
	"encoding/json"

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
}

// Tab is a person's attached browser tab as the tool drives it.
type Tab interface {
	Ask(ctx context.Context, action string, args any) (json.RawMessage, error)
	Title() string
	URL() string
}
