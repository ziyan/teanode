package browser

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

//go:embed snapshot.js
var snapshotScript string

// Browser is a connection to a Chrome.
type Browser struct {
	connection *connection

	// Guard says whether an address may be reached; nil uses the fetch
	// guard alone.
	guard func(host string) error

	mutex    sync.Mutex
	contexts map[string]*Context
}

// Settings are what a browser needs.
type Settings struct {
	// Endpoint is http://host:9222 or a ws:// debugger address.
	Endpoint string

	// AllowPrivate lists hosts or CIDRs the operator lets the browser
	// reach although they are private.
	AllowPrivate []string
}

// Connect reaches a browser at its endpoint.
func Connect(ctx context.Context, settings *Settings) (*Browser, error) {
	address, err := endpointAddress(ctx, settings.Endpoint)
	if err != nil {
		return nil, err
	}
	connection, err := dial(ctx, address)
	if err != nil {
		return nil, err
	}
	allowed := make([]*net.IPNet, 0, len(settings.AllowPrivate))
	allowedHosts := map[string]bool{}
	for _, entry := range settings.AllowPrivate {
		entry = strings.TrimSpace(strings.ToLower(entry))
		if _, network, err := net.ParseCIDR(entry); err == nil {
			allowed = append(allowed, network)
		} else if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			allowed = append(allowed, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		} else if entry != "" {
			// A host by name: allowed as it is, without resolving it.
			allowedHosts[entry] = true
		}
	}
	self := &Browser{connection: connection, contexts: map[string]*Context{}}
	self.guard = func(host string) error {
		if allowedHosts[strings.TrimSpace(strings.ToLower(host))] {
			return nil
		}
		return guardHost(host, allowed)
	}
	return self, nil
}

// guardHost resolves a host and refuses what the fetch guard refuses,
// unless the operator allowed it.
func guardHost(host string, allowed []*net.IPNet) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return fmt.Errorf("browser: no host")
	}
	addresses, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("browser: cannot resolve %s: %w", host, err)
	}
	for _, ip := range addresses {
		permitted := false
		for _, network := range allowed {
			if network.Contains(ip) {
				permitted = true
				break
			}
		}
		if permitted {
			continue
		}
		if err := safefetch.AllowAddress(net.JoinHostPort(ip.String(), "0")); err != nil {
			return fmt.Errorf("browser: %s is not a public address", host)
		}
	}
	return nil
}

// Close ends every context and the connection.
func (self *Browser) Close() error {
	self.mutex.Lock()
	contexts := make([]*Context, 0, len(self.contexts))
	for _, context := range self.contexts {
		contexts = append(contexts, context)
	}
	self.mutex.Unlock()
	for _, context := range contexts {
		_ = context.Close()
	}
	return self.connection.close()
}

// Context is a fresh, isolated browser context with one page in it: no
// cookies, no storage, no history from anyone else.
type Context struct {
	browser   *Browser
	contextId string
	targetId  string
	sessionId string

	mutex  sync.Mutex
	closed bool
	lastAt time.Time
}

// NewContext opens a context and a blank page in it.
func (self *Browser) NewContext(ctx context.Context) (*Context, error) {
	var created struct {
		BrowserContextID string `json:"browserContextId"`
	}
	if err := self.connection.call(ctx, "", "Target.createBrowserContext", map[string]any{"disposeOnDetach": true}, &created); err != nil {
		return nil, err
	}
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := self.connection.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": created.BrowserContextID}, &target); err != nil {
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := self.connection.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attached); err != nil {
		return nil, err
	}
	page := &Context{browser: self, contextId: created.BrowserContextID, targetId: target.TargetID, sessionId: attached.SessionID, lastAt: time.Now()}
	// The page never downloads, and every request it makes is checked.
	_ = self.connection.call(ctx, attached.SessionID, "Page.enable", nil, nil)
	_ = self.connection.call(ctx, attached.SessionID, "Runtime.enable", nil, nil)
	_ = self.connection.call(ctx, attached.SessionID, "Browser.setDownloadBehavior", map[string]any{"behavior": "deny", "browserContextId": created.BrowserContextID}, nil)
	// Without the guard nothing is checked, so a page that cannot be
	// guarded is a page that is not opened.
	if err := self.connection.call(ctx, attached.SessionID, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}, nil); err != nil {
		_ = self.connection.call(ctx, "", "Target.disposeBrowserContext", map[string]any{"browserContextId": created.BrowserContextID}, nil)
		return nil, fmt.Errorf("browser: cannot guard the page's requests: %w", err)
	}
	go page.guardRequests()
	self.mutex.Lock()
	self.contexts[created.BrowserContextID] = page
	self.mutex.Unlock()
	return page, nil
}

// guardRequests answers every paused request: continued when its host is
// allowed, failed otherwise.
func (self *Context) guardRequests() {
	events, stop := self.browser.connection.listen(self.sessionId)
	defer stop()
	for event := range events {
		if event.Method != "Fetch.requestPaused" {
			continue
		}
		var paused struct {
			RequestID string `json:"requestId"`
			Request   struct {
				URL string `json:"url"`
			} `json:"request"`
		}
		if err := json.Unmarshal(event.Params, &paused); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := self.allowed(paused.Request.URL); err != nil {
			_ = self.browser.connection.call(ctx, self.sessionId, "Fetch.failRequest", map[string]any{"requestId": paused.RequestID, "errorReason": "AccessDenied"}, nil)
		} else {
			_ = self.browser.connection.call(ctx, self.sessionId, "Fetch.continueRequest", map[string]any{"requestId": paused.RequestID}, nil)
		}
		cancel()
		self.mutex.Lock()
		closed := self.closed
		self.mutex.Unlock()
		if closed {
			return
		}
	}
}

// allowed says whether an address may be reached.
func (self *Context) allowed(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	switch parsed.Scheme {
	case "http", "https":
	case "about", "data", "blob":
		return nil
	default:
		return fmt.Errorf("browser: %s addresses are not followed", parsed.Scheme)
	}
	if self.browser.guard == nil {
		return nil
	}
	return self.browser.guard(parsed.Hostname())
}

func (self *Context) touch() {
	self.mutex.Lock()
	self.lastAt = time.Now()
	self.mutex.Unlock()
}

// LastUsed is when the context last did anything, for the idle sweep.
func (self *Context) LastUsed() time.Time {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.lastAt
}

// Close discards the context and everything in it.
func (self *Context) Close() error {
	self.mutex.Lock()
	if self.closed {
		self.mutex.Unlock()
		return nil
	}
	self.closed = true
	self.mutex.Unlock()
	self.browser.mutex.Lock()
	delete(self.browser.contexts, self.contextId)
	self.browser.mutex.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return self.browser.connection.call(ctx, "", "Target.disposeBrowserContext", map[string]any{"browserContextId": self.contextId}, nil)
}

// Location is where a navigation ended.
type Location struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Status int    `json:"status,omitempty"`
}

// Navigate opens an address, after the guard, and waits for the page to
// load.
func (self *Context) Navigate(ctx context.Context, address string) (*Location, error) {
	self.touch()
	target, err := safefetch.ParseTarget(address)
	if err != nil {
		return nil, err
	}
	if err := self.allowed(target.String()); err != nil {
		return nil, err
	}
	var navigated struct {
		ErrorText string `json:"errorText"`
	}
	if err := self.call(ctx, "Page.navigate", map[string]any{"url": target.String()}, &navigated); err != nil {
		return nil, err
	}
	if navigated.ErrorText != "" {
		return nil, fmt.Errorf("browser: cannot open %s: %s", target, navigated.ErrorText)
	}
	_, _ = self.Wait(ctx, "load", "", 30*time.Second)
	return self.location(ctx)
}

func (self *Context) location(ctx context.Context) (*Location, error) {
	var location Location
	if err := self.evaluate(ctx, `({url: location.href, title: document.title})`, &location); err != nil {
		return nil, err
	}
	return &location, nil
}

// Snapshot is the page as the model reads it.
type Snapshot struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// Snapshot reads the page: interactive, with refs, or text.
func (self *Context) Snapshot(ctx context.Context, mode string, maximum int) (*Snapshot, error) {
	self.touch()
	if maximum <= 0 {
		maximum = 20000
	}
	if mode == "" {
		mode = "interactive"
	}
	var snapshot Snapshot
	if err := self.evaluate(ctx, fmt.Sprintf("(%s)(%q, %d)", snapshotScript, mode, maximum), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// Screenshot is the viewport, or the whole page, as PNG.
func (self *Context) Screenshot(ctx context.Context, fullPage bool) ([]byte, error) {
	self.touch()
	params := map[string]any{"format": "png", "captureBeyondViewport": fullPage}
	var result struct {
		Data []byte `json:"data"`
	}
	if err := self.call(ctx, "Page.captureScreenshot", params, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// element is the script that finds an element by ref or selector.
const element = `(function (ref, selector) {
  let node = null;
  if (ref) { node = (window.__teanodeRefs || new Map()).get(ref) || null; }
  else if (selector) { node = document.querySelector(selector); }
  return node;
})`

// Target describes how an action names its element: a ref from the last
// snapshot, or a selector.
type Target struct {
	Ref      int    `json:"ref"`
	Selector string `json:"selector"`
}

func (self *Context) elementCenter(ctx context.Context, where Target) (float64, float64, string, error) {
	var found struct {
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		Name string  `json:"name"`
		OK   bool    `json:"ok"`
	}
	script := fmt.Sprintf(`(function () {
  const node = %s(%d, %q);
  if (!node) return {ok: false};
  node.scrollIntoView({block: 'center', inline: 'center'});
  const rect = node.getBoundingClientRect();
  const name = (node.getAttribute && (node.getAttribute('aria-label') || node.innerText || node.value || node.textContent) || '').trim().slice(0, 80);
  return {ok: true, x: rect.left + rect.width / 2, y: rect.top + rect.height / 2, name: node.tagName.toLowerCase() + (name ? ' "' + name + '"' : '')};
})()`, element, where.Ref, where.Selector)
	if err := self.evaluate(ctx, script, &found); err != nil {
		return 0, 0, "", err
	}
	if !found.OK {
		return 0, 0, "", fmt.Errorf("browser: nothing matches ref %d / selector %q; take a snapshot first", where.Ref, where.Selector)
	}
	return found.X, found.Y, found.Name, nil
}

// Click clicks an element and reports what it was and where the page
// went.
func (self *Context) Click(ctx context.Context, where Target) (string, *Location, error) {
	self.touch()
	x, y, name, err := self.elementCenter(ctx, where)
	if err != nil {
		return "", nil, err
	}
	for _, kind := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		params := map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1}
		if kind == "mouseMoved" {
			params["button"] = "none"
		}
		if err := self.call(ctx, "Input.dispatchMouseEvent", params, nil); err != nil {
			return "", nil, err
		}
	}
	time.Sleep(300 * time.Millisecond)
	location, err := self.location(ctx)
	return name, location, err
}

// Hover moves the pointer over an element.
func (self *Context) Hover(ctx context.Context, where Target) (string, error) {
	self.touch()
	x, y, name, err := self.elementCenter(ctx, where)
	if err != nil {
		return "", err
	}
	return name, self.call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": "mouseMoved", "x": x, "y": y, "button": "none"}, nil)
}

// Type types into an element. A password or a payment field is refused
// here, whatever was approved.
func (self *Context) Type(ctx context.Context, where Target, text string, clearFirst, submit bool) error {
	self.touch()
	var prepared struct {
		OK      bool   `json:"ok"`
		Refused string `json:"refused"`
	}
	script := fmt.Sprintf(`(function () {
  const node = %s(%d, %q);
  if (!node) return {ok: false};
  const type = (node.type || '').toLowerCase();
  const hint = ((node.autocomplete || '') + ' ' + (node.name || '') + ' ' + (node.id || '') + ' ' + (node.getAttribute('aria-label') || '')).toLowerCase();
  if (type === 'password' || /cc-|card|cvc|cvv|iban|account-?number|routing|ssn|passport/.test(hint)) return {ok: false, refused: 'a password or payment field'};
  node.focus();
  if (%t && 'value' in node) { node.value = ''; node.dispatchEvent(new Event('input', {bubbles: true})); }
  return {ok: true};
})()`, element, where.Ref, where.Selector, clearFirst)
	if err := self.evaluate(ctx, script, &prepared); err != nil {
		return err
	}
	if prepared.Refused != "" {
		return fmt.Errorf("browser: typing into %s is refused", prepared.Refused)
	}
	if !prepared.OK {
		return fmt.Errorf("browser: nothing matches ref %d / selector %q; take a snapshot first", where.Ref, where.Selector)
	}
	if err := self.call(ctx, "Input.insertText", map[string]any{"text": text}, nil); err != nil {
		return err
	}
	if submit {
		return self.Press(ctx, "Enter")
	}
	return nil
}

// Select chooses an option of a select by value, text or index.
func (self *Context) Select(ctx context.Context, where Target, value string, index int) (string, error) {
	self.touch()
	var chosen struct {
		OK    bool   `json:"ok"`
		Value string `json:"value"`
	}
	script := fmt.Sprintf(`(function () {
  const node = %s(%d, %q);
  if (!node || node.tagName.toLowerCase() !== 'select') return {ok: false};
  const value = %q; const index = %d;
  let picked = null;
  for (const option of node.options) { if (option.value === value || option.text.trim() === value) { picked = option; break; } }
  if (!picked && index >= 0 && index < node.options.length) picked = node.options[index];
  if (!picked) return {ok: false};
  node.value = picked.value;
  node.dispatchEvent(new Event('input', {bubbles: true}));
  node.dispatchEvent(new Event('change', {bubbles: true}));
  return {ok: true, value: picked.text};
})()`, element, where.Ref, where.Selector, value, index)
	if err := self.evaluate(ctx, script, &chosen); err != nil {
		return "", err
	}
	if !chosen.OK {
		return "", fmt.Errorf("browser: no such option; take a snapshot to see them")
	}
	return chosen.Value, nil
}

// Press presses a key: Enter, Tab, Escape, ArrowDown, or a character.
func (self *Context) Press(ctx context.Context, key string) error {
	self.touch()
	codes := map[string]int{"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "ArrowDown": 40, "ArrowUp": 38, "ArrowLeft": 37, "ArrowRight": 39, "PageDown": 34, "PageUp": 33, "Home": 36, "End": 35, "Delete": 46, "Space": 32}
	code, known := codes[key]
	params := map[string]any{"type": "keyDown", "key": key}
	if known {
		params["windowsVirtualKeyCode"] = code
		params["nativeVirtualKeyCode"] = code
		if key == "Enter" {
			params["text"] = "\r"
		}
	} else if len(key) == 1 {
		params["text"] = key
	}
	if err := self.call(ctx, "Input.dispatchKeyEvent", params, nil); err != nil {
		return err
	}
	return self.call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": key}, nil)
}

// Scroll scrolls the page, or an element, by a number of viewports.
func (self *Context) Scroll(ctx context.Context, direction string, amount int, where Target) error {
	self.touch()
	if amount <= 0 {
		amount = 1
	}
	sign := 1
	if direction == "up" {
		sign = -1
	}
	script := fmt.Sprintf(`(function () {
  const node = %s(%d, %q) || window;
  const height = (node === window ? window.innerHeight : node.clientHeight) * %d * %d;
  if (node === window) window.scrollBy({top: height, behavior: 'instant'}); else node.scrollBy({top: height, behavior: 'instant'});
  return true;
})()`, element, where.Ref, where.Selector, sign, amount)
	return self.evaluate(ctx, script, nil)
}

// Wait waits for a load, a selector, or the network to go quiet, up to a
// timeout, and says what ended it.
func (self *Context) Wait(ctx context.Context, condition, selector string, timeout time.Duration) (string, error) {
	self.touch()
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		script := ""
		switch condition {
		case "", "load", "navigation":
			script = `document.readyState === 'complete'`
		case "selector":
			script = fmt.Sprintf(`!!document.querySelector(%q)`, selector)
		case "network_idle":
			script = `document.readyState === 'complete' && (performance.getEntriesByType('resource').length === (window.__teanodeResources || 0) ? true : ((window.__teanodeResources = performance.getEntriesByType('resource').length), false))`
		case "timeout":
			time.Sleep(timeout)
			return "timeout", nil
		default:
			return "", fmt.Errorf("browser: %q is not something to wait for", condition)
		}
		if err := self.evaluate(ctx, script, &ok); err == nil && ok {
			return condition, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("browser: waited %s for %s", timeout, condition)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Back goes to the previous page.
func (self *Context) Back(ctx context.Context) (*Location, error) {
	self.touch()
	if err := self.evaluate(ctx, `history.back(); true`, nil); err != nil {
		return nil, err
	}
	_, _ = self.Wait(ctx, "load", "", 15*time.Second)
	return self.location(ctx)
}

// Evaluate runs an expression and returns its JSON value, bounded.
func (self *Context) Evaluate(ctx context.Context, expression string, maximum int) (string, error) {
	self.touch()
	var value json.RawMessage
	if err := self.evaluate(ctx, expression, &value); err != nil {
		return "", err
	}
	text := string(value)
	if maximum > 0 && len(text) > maximum {
		text = text[:maximum] + "…"
	}
	return text, nil
}

// evaluate runs a script in the page and decodes its value.
func (self *Context) evaluate(ctx context.Context, expression string, result any) error {
	var evaluated struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := self.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &evaluated); err != nil {
		return err
	}
	if evaluated.ExceptionDetails != nil {
		description := evaluated.ExceptionDetails.Text
		if evaluated.ExceptionDetails.Exception != nil {
			description = evaluated.ExceptionDetails.Exception.Description
		}
		return fmt.Errorf("browser: the page threw: %s", description)
	}
	if result == nil || len(evaluated.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(evaluated.Result.Value, result)
}

func (self *Context) call(ctx context.Context, method string, params any, result any) error {
	self.mutex.Lock()
	closed := self.closed
	self.mutex.Unlock()
	if closed {
		return fmt.Errorf("browser: the page is closed")
	}
	return self.browser.connection.call(ctx, self.sessionId, method, params, result)
}
