package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/passwords"
)

// The page's address comes from the browser's navigation history.
func TestPageURLIsTheNavigationHistoryEntry(test *testing.T) {
	chrome := &fakeChrome{}
	server := chrome.serve(test)
	defer server.Close()
	browser, err := Connect(context.Background(), &Settings{Endpoint: server.URL, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	page, err := browser.NewContext(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	chrome.history = []string{"about:blank", "https://shop.example.test/sign-in"}
	address, err := page.PageURL(context.Background())
	if err != nil || address != "https://shop.example.test/sign-in" {
		test.Fatalf("PageURL = %q, %v", address, err)
	}
	chrome.history = nil
	if _, err := page.PageURL(context.Background()); err == nil {
		test.Fatal("a browser that does not say where the page is was believed")
	}
}

// FillSecret needs a ref, a site and something to fill, and never repeats the
// text in what it says.
func TestFillSecretRefusesWhatItCannotFillAndNeverEchoesTheText(test *testing.T) {
	chrome := &fakeChrome{}
	server := chrome.serve(test)
	defer server.Close()
	browser, err := Connect(context.Background(), &Settings{Endpoint: server.URL, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	page, err := browser.NewContext(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	const secret = "an-invented-secret-value"
	for name, arguments := range map[string]struct {
		ref  int
		host string
		text string
	}{"no ref": {0, "example.test", secret}, "no host": {1, "", secret}, "no text": {1, "example.test", ""}} {
		if err := page.FillSecret(context.Background(), arguments.ref, arguments.host, arguments.text); err == nil {
			test.Errorf("%s: filled", name)
		}
	}
	if chrome.sent("Input.insertText") != 0 {
		test.Fatal("text was inserted")
	}
	// The page says the field is not there: the text is not inserted.
	chrome.evaluate = func(string) any {
		return map[string]any{"isFocused": false, "problem": "no field has that ref; take a snapshot first"}
	}
	err = page.FillSecret(context.Background(), 4, "example.test", secret)
	if err == nil || !strings.Contains(err.Error(), "no field has that ref") || strings.Contains(err.Error(), secret) || chrome.sent("Input.insertText") != 0 {
		test.Fatalf("a missing field: %v", err)
	}
	// The script carries the host to check, and not the text.
	if script := chrome.paramsOf("Runtime.evaluate"); !strings.Contains(script, `\"example.test\"`) || strings.Contains(script, secret) {
		test.Fatalf("the script: %s", script)
	}
	chrome.evaluate = func(string) any { return map[string]any{"isFocused": true, "problem": ""} }
	if err := page.FillSecret(context.Background(), 4, "example.test", secret); err != nil || chrome.sent("Input.insertText") != 1 {
		test.Fatalf("fill: %v", err)
	}
}

// A passkey is put in, replaced by the next, and taken away by the page's own
// navigation, by the time running out and when the page closes.
func TestAddPasskeyIsRemovedByNavigationTimeOrClose(test *testing.T) {
	passkey := Passkey{CredentialID: []byte("credential"), RelyingPartyID: "example.test", UserHandle: []byte("handle"), PrivateKey: []byte("key")}
	start := func() (*fakeChrome, *Context, func()) {
		chrome := &fakeChrome{}
		server := chrome.serve(test)
		browser, err := Connect(context.Background(), &Settings{Endpoint: server.URL, ProxyListen: "127.0.0.1:0"})
		if err != nil {
			test.Fatal(err)
		}
		page, err := browser.NewContext(context.Background())
		if err != nil {
			test.Fatal(err)
		}
		return chrome, page, func() { _ = browser.Close(); server.Close() }
	}
	waitFor := func(condition func() bool) bool {
		deadline := time.Now().Add(5 * time.Second)
		for !condition() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		return condition()
	}

	chrome, page, finish := start()
	defer finish()
	if err := page.AddPasskey(context.Background(), Passkey{}); err == nil {
		test.Fatal("an empty passkey was added")
	}
	if err := page.AddPasskey(context.Background(), passkey); err != nil {
		test.Fatal(err)
	}
	if !page.HasPasskey() || chrome.sent("WebAuthn.addCredential") != 1 {
		test.Fatal("the passkey is not in the page")
	}
	// A second passkey replaces the first.
	if err := page.AddPasskey(context.Background(), passkey); err != nil {
		test.Fatal(err)
	}
	if chrome.sent("WebAuthn.removeVirtualAuthenticator") != 1 || chrome.sent("WebAuthn.addCredential") != 2 || !page.HasPasskey() {
		test.Fatalf("a second passkey: removed %d, added %d", chrome.sent("WebAuthn.removeVirtualAuthenticator"), chrome.sent("WebAuthn.addCredential"))
	}
	// A credential in the protocol is base64.
	if sent := chrome.paramsOf("WebAuthn.addCredential"); !strings.Contains(sent, base64.StdEncoding.EncodeToString(passkey.PrivateKey)) || !strings.Contains(sent, `"isResidentCredential":true`) {
		test.Fatalf("the credential: %s", sent)
	}

	// The time running out.
	previous := passkeyLifetimeForTests(50 * time.Millisecond)
	defer passkeyLifetimeForTests(previous)
	if err := page.AddPasskey(context.Background(), passkey); err != nil {
		test.Fatal(err)
	}
	if !waitFor(func() bool { return !page.HasPasskey() }) {
		test.Fatal("the passkey was not removed when its time ran out")
	}
	passkeyLifetimeForTests(time.Minute)

	// Closing the page ends the watch without a command to a page that is gone.
	if err := page.AddPasskey(context.Background(), passkey); err != nil {
		test.Fatal(err)
	}
	removed := chrome.sent("WebAuthn.removeVirtualAuthenticator")
	_ = page.Close()
	time.Sleep(50 * time.Millisecond)
	if page.HasPasskey() || chrome.sent("WebAuthn.removeVirtualAuthenticator") != removed {
		test.Fatal("closing the page")
	}
}

// Against a real Chrome: a password filled in is typed into the field, is not
// read back by a snapshot or by a click, and a stored passkey signs in through
// the virtual authenticator until the page navigates.
//
//	google-chrome --headless=new --remote-debugging-port=19222 \
//	  --user-data-dir=$(mktemp -d) about:blank
//	TEANODE_TEST_CHROME=http://127.0.0.1:19222 go test ./internal/browser/
func TestARealChromeFillsASecretAndSignsWithAPasskey(test *testing.T) {
	endpoint := os.Getenv("TEANODE_TEST_CHROME")
	if endpoint == "" {
		test.Skip("set TEANODE_TEST_CHROME to a DevTools endpoint to run this")
	}
	const secret = "an-invented-password-9"
	site := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(response, `<html><body><form>
<input id="user" name="user" autocomplete="username"><input id="secret" name="secret" type="password"><input id="code" name="code" autocomplete="one-time-code">
</form></body></html>`)
	}))
	defer site.Close()
	address := strings.Replace(site.URL, "127.0.0.1", "localhost", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	browser, err := Connect(ctx, &Settings{Endpoint: endpoint, AllowPrivate: []string{"localhost"}, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = browser.Close() }()
	browser.guard = func(string) error { return nil }
	page, err := browser.NewContext(ctx)
	if err != nil {
		test.Fatal(err)
	}
	defer func() { _ = page.Close() }()
	if _, err := page.Navigate(ctx, address); err != nil {
		test.Fatal(err)
	}
	if got, err := page.PageURL(ctx); err != nil || !strings.HasPrefix(got, address) {
		test.Fatalf("PageURL = %q, %v", got, err)
	}
	refOf := func(description string) int {
		snapshot, err := page.Snapshot(ctx, "interactive", 0)
		if err != nil {
			test.Fatal(err)
		}
		for _, line := range strings.Split(snapshot.Text, "\n") {
			var ref int
			if strings.Contains(line, description) {
				if _, err := fmt.Sscanf(strings.TrimSpace(line), "[ref=%d]", &ref); err == nil {
					return ref
				}
			}
		}
		test.Fatalf("no %s in %s", description, snapshot.Text)
		return 0
	}
	passwordRef := refOf(`input(password) "secret"`)

	// The wrong site fills nothing.
	if err := page.FillSecret(ctx, passwordRef, "evil.test", secret); err == nil {
		test.Fatal("a page that is not the site was filled")
	}
	if value, _ := page.Evaluate(ctx, `document.getElementById('secret').value`, 100); value != `""` {
		test.Fatalf("the field holds %s", value)
	}
	if err := page.FillSecret(ctx, passwordRef, "localhost", secret); err != nil {
		test.Fatal(err)
	}
	if value, _ := page.Evaluate(ctx, `document.getElementById('secret').value`, 100); value != `"`+secret+`"` {
		test.Fatalf("the field holds %s", value)
	}
	// Filled again, the field is replaced, not added to.
	if err := page.FillSecret(ctx, passwordRef, "localhost", secret); err != nil {
		test.Fatal(err)
	}
	if value, _ := page.Evaluate(ctx, `document.getElementById('secret').value`, 100); value != `"`+secret+`"` {
		test.Fatalf("the field holds %s after a second fill", value)
	}
	// A field with a code in it is not read back either.
	if err := page.FillSecret(ctx, refOf(`"code"`), "localhost", "123456"); err != nil {
		test.Fatal(err)
	}
	for _, mode := range []string{"interactive", "text"} {
		snapshot, err := page.Snapshot(ctx, mode, 0)
		if err != nil {
			test.Fatal(err)
		}
		if strings.Contains(snapshot.Text, secret) || strings.Contains(snapshot.Text, "123456") {
			test.Fatalf("%s snapshot carries a secret: %s", mode, snapshot.Text)
		}
		if mode == "interactive" && !strings.Contains(snapshot.Text, "(filled, value not shown)") {
			test.Fatalf("the filled password is not marked: %s", snapshot.Text)
		}
	}
	name, _, err := page.Click(ctx, Target{Ref: passwordRef})
	if err != nil || strings.Contains(name, secret) {
		test.Fatalf("a click named the field %q (%v)", name, err)
	}

	// A passkey: the page asks for an assertion and the virtual authenticator
	// answers, then the next navigation takes it away.
	credentialID, privateKey, err := passwords.NewPasskey()
	if err != nil {
		test.Fatal(err)
	}
	if err := page.AddPasskey(ctx, Passkey{CredentialID: credentialID, RelyingPartyID: "localhost", UserHandle: []byte("handle"), PrivateKey: privateKey}); err != nil {
		test.Fatal(err)
	}
	identifier, _ := json.Marshal(credentialID)
	ask := fmt.Sprintf(`navigator.credentials.get({publicKey: {challenge: new Uint8Array(32), rpId: 'localhost', userVerification: 'required', timeout: 5000,
  allowCredentials: [{type: 'public-key', id: Uint8Array.from(atob(%s), (c) => c.charCodeAt(0))}]}}).then((credential) => credential.response.signature.byteLength > 0, (failure) => failure.name)`, identifier)
	if answer, err := page.Evaluate(ctx, ask, 200); err != nil || answer != "true" {
		test.Fatalf("the passkey did not sign: %q %v", answer, err)
	}
	if _, err := page.Navigate(ctx, address); err != nil {
		test.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for page.HasPasskey() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if page.HasPasskey() {
		test.Fatal("the passkey stayed after the page navigated")
	}
	// With no authenticator the request waits for a person who is not there,
	// so it is given a few seconds, not a minute.
	shortCtx, stopWaiting := context.WithTimeout(ctx, 4*time.Second)
	defer stopWaiting()
	if answer, _ := page.Evaluate(shortCtx, ask, 200); answer == "true" {
		test.Fatal("the passkey still signs after the page navigated")
	}
}
