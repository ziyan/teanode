package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/config"
)

// The browser: one tool that drives a page, where the page opens being
// the choice. Headless is a Chrome the operator runs beside the server —
// a fresh, isolated context per turn, discarded when the turn ends, never
// signed in as anybody. A tab is the person's own, attached through the
// extension, for what needs their session; it is never used by a run with
// nobody present. A headless run may only read: navigate, snapshot,
// scroll, wait, click and select; never type, press or evaluate.

// browserReadingActions are what a run with nobody present may do.
// browserRunner is the browser as one turn reaches it.
type browserRunner struct {
	agent *Agent

	mutex   sync.Mutex
	browser *browser.Browser
	page    *browser.Context
}

// browserFor is the turn's browser, opened on first use.
func (self *Agent) browserFor(ctx context.Context, run *AskRun) (*browser.Context, error) {
	run.mutex.Lock()
	runner := run.browser
	if runner == nil {
		runner = &browserRunner{agent: self}
		run.browser = runner
	}
	run.mutex.Unlock()
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	if runner.page != nil {
		return runner.page, nil
	}
	configuration := self.settings.Configuration()
	if !configuration.Agent.Browser.Enabled || configuration.Agent.Browser.CDPEndpoint == "" {
		return nil, fmt.Errorf("no browser is configured on this server")
	}
	if !self.reserveContext(configuration) {
		return nil, fmt.Errorf("the browser is busy with other runs; try again in a moment")
	}
	release := func() {
		self.contextsMutex.Lock()
		if self.contextsOpen > 0 {
			self.contextsOpen--
		}
		self.contextsMutex.Unlock()
	}
	opened, err := browser.Connect(ctx, &browser.Settings{Endpoint: configuration.Agent.Browser.CDPEndpoint, AllowPrivate: configuration.Agent.Browser.AllowPrivateAddresses})
	if err != nil {
		release()
		return nil, err
	}
	page, err := opened.NewContext(ctx)
	if err != nil {
		_ = opened.Close()
		release()
		return nil, err
	}
	runner.browser = opened
	runner.page = page
	return page, nil
}

// reserveContext takes a place under the operator's cap, or says there is
// none: counted in the same breath as checked, so that several turns
// opening their first page at once cannot all find room.
func (self *Agent) reserveContext(configuration *config.Configuration) bool {
	limit := configuration.Agent.Browser.MaxContexts
	if limit <= 0 {
		limit = 4
	}
	self.contextsMutex.Lock()
	defer self.contextsMutex.Unlock()
	if self.contextsOpen >= limit {
		return false
	}
	self.contextsOpen++
	return true
}

// close ends the turn's browser.
func (self *browserRunner) close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.browser == nil {
		return
	}
	_ = self.browser.Close()
	self.browser, self.page = nil, nil
	self.agent.contextsMutex.Lock()
	if self.agent.contextsOpen > 0 {
		self.agent.contextsOpen--
	}
	self.agent.contextsMutex.Unlock()
}

// idle says whether the turn's browser has sat unused past the timeout.
func (self *browserRunner) idle(timeout time.Duration) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.page != nil && time.Since(self.page.LastUsed()) > timeout
}

// sweepBrowsers closes the browsers of turns that sat idle.
func (self *Agent) sweepBrowsers() {
	timeout := self.settings.Configuration().Agent.Browser.IdleTimeout.Duration()
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	self.runsMutex.Lock()
	runs := make([]*AskRun, 0, len(self.runs))
	for _, run := range self.runs {
		runs = append(runs, run)
	}
	self.runsMutex.Unlock()
	for _, run := range runs {
		run.mutex.Lock()
		runner := run.browser
		run.mutex.Unlock()
		if runner != nil && runner.idle(timeout) {
			runner.close()
		}
	}
}
