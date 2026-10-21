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
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in"}, nil, -1, "")
	address, err := page.PageURL(context.Background())
	if err != nil || address != "https://shop.example.test/sign-in" {
		test.Fatalf("PageURL = %q, %v", address, err)
	}
	chrome.setHistory(nil, nil, -1, "")
	if _, err := page.PageURL(context.Background()); err == nil {
		test.Fatal("a browser that does not say where the page is was believed")
	}
}

// startFake connects a browser to a fake Chrome and opens a page in it.
func startFake(test *testing.T) (*fakeChrome, *Context, func()) {
	test.Helper()
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

func waitUntil(condition func() bool) bool {
	deadline := time.Now().Add(5 * time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

// FillSecret needs a ref, a site, a kind and something to fill, and never
// repeats the text in what it says.
func TestFillSecretRefusesWhatItCannotFillAndNeverEchoesTheText(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	const secret = "an-invented-secret-value"
	for name, request := range map[string]FillRequest{
		"no ref":                 {Ref: 0, Host: "example.test", Kind: FieldKindPassword, Text: secret},
		"no host":                {Ref: 1, Host: "", Kind: FieldKindPassword, Text: secret},
		"no text":                {Ref: 1, Host: "example.test", Kind: FieldKindPassword, Text: ""},
		"no kind":                {Ref: 1, Host: "example.test", Text: secret},
		"a kind that is not one": {Ref: 1, Host: "example.test", Kind: "everything", Text: secret},
	} {
		if err := page.FillSecret(context.Background(), request); err == nil {
			test.Errorf("%s: filled", name)
		}
	}
	if chrome.sent("Runtime.evaluate") != 0 || chrome.sent("Input.insertText") != 0 {
		test.Fatal("something was sent for a request that could not be made")
	}
	// The page says why the field cannot be filled: the error says it and does
	// not repeat the text.
	chrome.evaluate = func(string) any {
		return map[string]any{"isFilled": false, "problem": "no field has that ref; take a snapshot first"}
	}
	err := page.FillSecret(context.Background(), FillRequest{Ref: 4, Host: "example.test", Port: 443, Kind: FieldKindPassword, Text: secret})
	if err == nil || !strings.Contains(err.Error(), "no field has that ref") || strings.Contains(err.Error(), secret) {
		test.Fatalf("a missing field: %v", err)
	}
}

// The check and the value going in are one script: the host, the port, the
// field and its kind are checked in the page where the value is set, and the
// value is not inserted by a separate command.
func TestFillSecretIsOneScriptThatChecksAndFills(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	const secret = "an-invented-secret-value"
	var script string
	chrome.evaluate = func(expression string) any {
		script = expression
		return map[string]any{"isFilled": true, "problem": ""}
	}
	if err := page.FillSecret(context.Background(), FillRequest{Ref: 4, Host: "example.test", Port: 8443, Kind: FieldKindPassword, Text: secret}); err != nil {
		test.Fatal(err)
	}
	if chrome.sent("Runtime.evaluate") != 1 || chrome.sent("Input.insertText") != 0 || chrome.sent("Input.dispatchKeyEvent") != 0 {
		test.Fatalf("a fill is one evaluation, not typing: %v", chrome.commands)
	}
	for _, wanted := range []string{`"example.test"`, `8443`, `"password"`, `"` + secret + `"`, "location.hostname", "location.port", "instanceof HTMLInputElement", "getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')", "new Event('input'", "new Event('change'", "__teanodeFilled", "true)"} {
		if !strings.Contains(script, wanted) {
			test.Errorf("the script lacks %s:\n%s", wanted, script)
		}
	}
	// A user name is not noted as a secret.
	if err := page.FillSecret(context.Background(), FillRequest{Ref: 3, Host: "example.test", Kind: FieldKindUserName, Text: "ada"}); err != nil {
		test.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(script), `"userName", "ada", false)`) {
		test.Fatalf("the user name's arguments: %s", script[len(script)-80:])
	}
	// An error out of the page is not passed on: it could quote the script.
	chrome.evaluate = func(string) any { return nil }
	chrome.failEvaluate = "TypeError: " + secret
	err := page.FillSecret(context.Background(), FillRequest{Ref: 4, Host: "example.test", Kind: FieldKindPassword, Text: secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		test.Fatalf("the page threw: %v", err)
	}
}

// Before a value goes in the browser stops keeping what the page asks of the
// network, the page is locked, and the lock ends only when the main frame
// commits a new document: not for a frame inside it, not for a change that
// keeps the document, and not for a move back or forward through the history
// (which locks again when it returns to the filled page).
func TestPrepareToFillLocksThePageUntilItCommitsANewDocument(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	signIn := "https://shop.example.test/sign-in"
	chrome.setHistory([]string{"about:blank", signIn}, []int{1, 2}, -1, "loader-1")

	page.Filled().Remember("a-password")
	page.PrepareToFill(context.Background())
	if chrome.sent("Network.disable") != 1 || chrome.sent("Page.getFrameTree") != 1 {
		test.Fatalf("before the fill: %v", chrome.commands)
	}
	if !page.Filled().IsLocked("") {
		test.Fatal("not locked")
	}
	navigated := func(loader, address string, parent string) {
		frame := map[string]any{"id": "main", "loaderId": loader, "url": address}
		if parent != "" {
			frame["parentId"] = parent
		}
		chrome.emit("Page.frameNavigated", map[string]any{"frame": frame})
	}
	settle := func() { time.Sleep(150 * time.Millisecond) }

	// A frame inside the page, a change of the address that keeps the
	// document, and a fragment do not unlock it.
	navigated("loader-frame", "https://ads.example.test/", "main")
	chrome.emit("Page.navigatedWithinDocument", map[string]any{"frameId": "main", "url": signIn + "#next"})
	settle()
	if !page.Filled().IsLocked("") {
		test.Fatal("something that is not a new document unlocked the page")
	}

	// A new page: a new history entry.
	chrome.setHistory([]string{"about:blank", signIn, "https://shop.example.test/account"}, []int{1, 2, 3}, -1, "loader-2")
	navigated("loader-2", "https://shop.example.test/account", "")
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new page did not unlock")
	}

	// Back to a page that was not filled: still unlocked.
	chrome.setHistory([]string{"about:blank", signIn, "https://shop.example.test/account"}, []int{1, 2, 3}, 0, "loader-3")
	navigated("loader-3", "about:blank", "")
	settle()
	if page.Filled().IsLocked("") {
		test.Fatal("going back locked the page")
	}
	// Forward to the page that was filled: locked again.
	chrome.setHistory([]string{"about:blank", signIn, "https://shop.example.test/account"}, []int{1, 2, 3}, 1, "loader-4")
	navigated("loader-4", signIn, "")
	if !waitUntil(func() bool { return page.Filled().IsLocked("") }) {
		test.Fatal("coming back to the filled page did not lock it")
	}
	// A reload is a new document at the same entry: unlocked.
	navigated("loader-5", signIn, "")
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a reload did not unlock")
	}

	// A second fill locks the page again, in the document it is in now.
	page.Filled().Remember("another")
	page.PrepareToFill(context.Background())
	if !page.Filled().IsLocked("") || chrome.sent("Network.disable") != 2 {
		test.Fatal("a second fill")
	}
	// When the history cannot be read a commit is taken for a move through it.
	page.historyWait = 200 * time.Millisecond
	chrome.setHistory(nil, nil, -1, "loader-6")
	navigated("loader-6", "https://shop.example.test/elsewhere", "")
	settle()
	if !page.Filled().IsLocked("") {
		test.Fatal("a commit that could not be classified unlocked the page")
	}
	// What was filled stays hidden.
	if page.Filled().Scrub("a-password another") != "[hidden] [hidden]" {
		test.Fatal("values were forgotten")
	}
}

// While a page swaps in, the browser answers that the page is not attached to
// the history, and answers properly a moment later: the commit waits for it
// rather than taking a new page for a move through the history.
func TestACommitWaitsForTheHistoryWhileThePageSwapsIn(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	signIn := "https://shop.example.test/sign-in"
	chrome.setHistory([]string{"about:blank", signIn}, []int{1, 2}, -1, "loader-1")
	page.Filled().Remember("a-password")
	page.PrepareToFill(context.Background())

	chrome.setHistory([]string{"about:blank", signIn, "https://shop.example.test/account"}, []int{1, 2, 3}, -1, "loader-2")
	chrome.mutex.Lock()
	chrome.historyFailures = 3
	chrome.mutex.Unlock()
	chrome.emit("Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "main", "loaderId": "loader-2", "url": "https://shop.example.test/account"}})
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new page that was committed while the history was not readable did not unlock")
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
<textarea id="area" name="area"></textarea><div id="editable" contenteditable="true" role="textbox" aria-label="editable"></div><select id="choice" name="choice"><option value="a">a</option></select>
<input id="agree" name="agree" type="checkbox"><input id="plain" name="plain">
<button type="button" id="reveal" onclick="document.getElementById('secret').type='text'">Show</button>
<button type="button" id="push" onclick="history.pushState({}, '', '/pushed')">Push</button>
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

	fill := func(ref int, host string, kind FieldKind, text string) error {
		return page.FillSecret(ctx, FillRequest{Ref: ref, Host: host, Kind: kind, Text: text})
	}
	valueOf := func(id string) string {
		value, _ := page.Evaluate(ctx, `document.getElementById('`+id+`').value`, 200)
		return value
	}
	// The wrong site, and the wrong port, fill nothing.
	if err := fill(passwordRef, "evil.test", FieldKindPassword, secret); err == nil {
		test.Fatal("a page that is not the site was filled")
	}
	if err := page.FillSecret(ctx, FillRequest{Ref: passwordRef, Host: "localhost", Port: 1, Kind: FieldKindPassword, Text: secret}); err == nil {
		test.Fatal("a page on another port was filled")
	}
	if value := valueOf("secret"); value != `""` {
		test.Fatalf("the field holds %s", value)
	}
	// Only a field of the kind takes the value: not a textarea, an editable
	// region, a select, a checkbox, nor an input that is another kind.
	for kind, refs := range map[FieldKind][]int{
		FieldKindPassword:    {refOf(`textarea "area"`), refOf(`"editable"`), refOf(`select "choice"`), refOf(`input(checkbox) "agree"`), refOf(`input(text) "plain"`), refOf(`"user"`)},
		FieldKindUserName:    {refOf(`textarea "area"`), refOf(`"editable"`), refOf(`select "choice"`), refOf(`input(checkbox) "agree"`), passwordRef},
		FieldKindOneTimeCode: {refOf(`textarea "area"`), refOf(`"editable"`), refOf(`select "choice"`), refOf(`input(checkbox) "agree"`), passwordRef},
	} {
		for _, ref := range refs {
			if err := fill(ref, "localhost", kind, secret); err == nil {
				test.Fatalf("a %s went into ref %d", kind, ref)
			}
		}
	}
	for _, id := range []string{"area", "editable", "choice", "agree", "plain"} {
		if value, _ := page.Evaluate(ctx, `document.getElementById('`+id+`').value || document.getElementById('`+id+`').textContent`, 200); strings.Contains(value, secret) || strings.Contains(value, "123456") {
			test.Fatalf("%s holds %s", id, value)
		}
	}
	// A user name goes into the field that says it takes one, and a code into
	// a field that says it takes one.
	if err := fill(refOf(`"user"`), "localhost", FieldKindUserName, "ada"); err != nil {
		test.Fatal(err)
	}

	if err := fill(passwordRef, "localhost", FieldKindPassword, secret); err != nil {
		test.Fatal(err)
	}
	if value := valueOf("secret"); value != `"`+secret+`"` {
		test.Fatalf("the field holds %s", value)
	}
	// Filled again, the field is replaced, not added to.
	if err := fill(passwordRef, "localhost", FieldKindPassword, secret); err != nil {
		test.Fatal(err)
	}
	if value := valueOf("secret"); value != `"`+secret+`"` {
		test.Fatalf("the field holds %s after a second fill", value)
	}
	// The page sees its own input event, and the value is set the way its
	// typing would.
	if value, _ := page.Evaluate(ctx, `(function () { let seen = 0; const node = document.getElementById('secret'); node.addEventListener('input', () => seen++); return seen; })()`, 20); value != "0" {
		test.Fatalf("listener: %s", value)
	}
	if err := fill(refOf(`"code"`), "localhost", FieldKindOneTimeCode, "123456"); err != nil {
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
		// A page a password was filled into shows no input's text at all.
		if mode == "interactive" && (strings.Contains(snapshot.Text, `value="ada"`) || !strings.Contains(snapshot.Text, "(has text, not shown while the page is locked)")) {
			test.Fatalf("a user name is shown on a page a password was filled into: %s", snapshot.Text)
		}
	}
	name, _, err := page.Click(ctx, Target{Ref: passwordRef})
	if err != nil || strings.Contains(name, secret) {
		test.Fatalf("a click named the field %q (%v)", name, err)
	}
	// A show-password button makes the field a text input: it is still not
	// shown, because the agent filled it.
	if _, _, err := page.Click(ctx, Target{Ref: refOf(`"Show"`)}); err != nil {
		test.Fatal(err)
	}
	if value, _ := page.Evaluate(ctx, `document.getElementById('secret').type`, 20); value != `"text"` {
		test.Fatalf("the field is a %s", value)
	}
	snapshot, err := page.Snapshot(ctx, "interactive", 0)
	if err != nil || strings.Contains(snapshot.Text, secret) || !strings.Contains(snapshot.Text, "(filled, value not shown)") {
		test.Fatalf("after the field became text: %v %s", err, snapshot.Text)
	}
	name, _, err = page.Click(ctx, Target{Ref: passwordRef})
	if err != nil || strings.Contains(name, secret) {
		test.Fatalf("a click named the revealed field %q (%v)", name, err)
	}
	// A long value in a field the agent did not fill is not cut and shown:
	// it is not shown.
	if _, err := page.Evaluate(ctx, `document.getElementById('plain').value = 'q'.repeat(100); true`, 20); err != nil {
		test.Fatal(err)
	}
	snapshot, err = page.Snapshot(ctx, "interactive", 0)
	if err != nil || strings.Contains(snapshot.Text, "qqqqq") || !strings.Contains(snapshot.Text, "not shown") {
		test.Fatalf("a long value: %v %s", err, snapshot.Text)
	}

	// The lock: filling a password locks the page; a change that keeps the
	// document (history.pushState, a fragment) does not unlock it, a new
	// document does, and going back to the filled page locks it again.
	page.Filled().Remember(secret)
	page.PrepareToFill(ctx)
	if !page.Filled().IsLocked("") {
		test.Fatal("not locked after PrepareToFill")
	}
	// Text pasted or typed into any input, textarea or editable region is
	// not shown in a snapshot or named by a click while the page is locked.
	if _, err := page.Evaluate(ctx, `(function () { const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set; set.call(document.getElementById('plain'), 'pasted-copy-1'); document.getElementById('area').value = 'pasted-copy-2'; document.getElementById('editable').textContent = 'pasted-copy-3'; return true; })()`, 20); err != nil {
		test.Fatal(err)
	}
	for _, mode := range []string{"interactive", "text"} {
		snapshot, err := page.Snapshot(ctx, mode, 0)
		if err != nil || strings.Contains(snapshot.Text, "pasted-copy") {
			test.Fatalf("a locked %s snapshot shows pasted text: %v %s", mode, err, snapshot.Text)
		}
		if mode == "interactive" && (!strings.Contains(snapshot.Text, "Push") || !strings.Contains(snapshot.Text, "Show")) {
			test.Fatalf("a locked snapshot lost the buttons: %s", snapshot.Text)
		}
	}
	for _, id := range []string{"plain", "area", "editable"} {
		name, _, err := page.Click(ctx, Target{Selector: "#" + id})
		if err != nil || strings.Contains(name, "pasted-copy") {
			test.Fatalf("a click named %s %q (%v)", id, name, err)
		}
	}
	if _, _, err := page.Click(ctx, Target{Ref: refOf(`"Push"`)}); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Evaluate(ctx, `location.hash = '#next'; true`, 20); err != nil {
		test.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !page.Filled().IsLocked("") {
		test.Fatal("pushState or a fragment unlocked the page")
	}
	if _, err := page.Navigate(ctx, address+"/other"); err != nil {
		test.Fatal(err)
	}
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new document did not unlock the page")
	}
	// Going back is not a new page: it does not unlock what it returns to.
	// Filled again on a fresh load, then away, then back to the page that was
	// filled: locked again.
	if _, err := page.Navigate(ctx, address); err != nil {
		test.Fatal(err)
	}
	page.Filled().Remember(secret)
	page.PrepareToFill(ctx)
	if !page.Filled().IsLocked("") {
		test.Fatal("not locked after a second PrepareToFill")
	}
	if _, err := page.Navigate(ctx, address+"/other2"); err != nil {
		test.Fatal(err)
	}
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new document did not unlock the page")
	}
	if _, err := page.Back(ctx); err != nil {
		test.Fatal(err)
	}
	if !waitUntil(func() bool { return page.Filled().IsLocked("") }) {
		test.Fatal("going back to the filled page did not lock it")
	}
	// Away to a page that is new: unlocked again.
	if _, err := page.Navigate(ctx, address+"/other3"); err != nil {
		test.Fatal(err)
	}
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new document did not unlock the page")
	}
	// What was filled is hidden for as long as the page lives.
	if got := page.Filled().Scrub("p=" + secret); strings.Contains(got, secret) {
		test.Fatalf("scrubbed %q", got)
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

// Going back returns only once the browser has moved to the earlier entry of
// its history, and asks which document it shows then: when that is the filled
// one, restored from the history's cache before any event said so, the page is
// locked again before the caller can run anything on it.
func TestBackWaitsForTheMoveAndLocksAgainWhenTheFilledDocumentComesBack(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	signIn := "https://shop.example.test/sign-in"
	account := "https://shop.example.test/account"
	chrome.setHistory([]string{"about:blank", signIn}, []int{1, 2}, -1, "loader-1")
	page.Filled().Remember("a-password")
	page.PrepareToFill(context.Background())

	// A new page: unlocked.
	chrome.setHistory([]string{"about:blank", signIn, account}, []int{1, 2, 3}, -1, "loader-2")
	chrome.emit("Page.frameNavigated", map[string]any{"frame": map[string]any{"id": "main", "loaderId": "loader-2", "url": account}})
	if !waitUntil(func() bool { return !page.Filled().IsLocked("") }) {
		test.Fatal("a new page did not unlock")
	}

	// history.back() takes a moment to reach the browser's history, and no
	// event is sent for the move.
	chrome.mutex.Lock()
	chrome.evaluate = func(expression string) any {
		if strings.Contains(expression, "history.back") {
			go func() {
				time.Sleep(150 * time.Millisecond)
				chrome.setHistory([]string{"about:blank", signIn, account}, []int{1, 2, 3}, 1, "loader-1")
			}()
		}
		if strings.Contains(expression, "location.href") {
			return map[string]any{"url": account, "title": ""}
		}
		return true
	}
	chrome.mutex.Unlock()
	started := time.Now()
	if _, err := page.Back(context.Background()); err != nil {
		test.Fatal(err)
	}
	if time.Since(started) < 100*time.Millisecond {
		test.Fatal("Back returned before the browser had moved")
	}
	if !page.Filled().IsLocked("") {
		test.Fatal("the filled document came back and the page was not locked")
	}
}

// A command about to run asks the browser which document it shows, and a
// browser that does not say is an error for the caller to refuse on.
func TestCheckCurrentDocumentLocksOnTheFilledDocumentOnly(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in"}, []int{1, 2}, -1, "loader-1")
	// Nothing filled: no question asked.
	if err := page.CheckCurrentDocument(context.Background()); err != nil || chrome.sent("Page.getFrameTree") != 0 {
		test.Fatalf("an unfilled page: %v %d", err, chrome.sent("Page.getFrameTree"))
	}
	page.Filled().Remember("a-password")
	page.PrepareToFill(context.Background())
	asked := chrome.sent("Page.getFrameTree")
	page.Filled().Committed("", Commit{DocumentID: "loader-2", Address: "https://shop.example.test/account", Count: 9})
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in", "https://shop.example.test/account"}, []int{1, 2, 3}, -1, "loader-2")
	if err := page.CheckCurrentDocument(context.Background()); err != nil || page.Filled().IsLocked("") || chrome.sent("Page.getFrameTree") != asked+1 {
		test.Fatalf("another document: %v locked %v", err, page.Filled().IsLocked(""))
	}
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in", "https://shop.example.test/account"}, []int{1, 2, 3}, -1, "loader-1")
	if err := page.CheckCurrentDocument(context.Background()); err != nil || !page.Filled().IsLocked("") {
		test.Fatalf("the filled document: %v locked %v", err, page.Filled().IsLocked(""))
	}
	// A nil value and a document that is not known change nothing.
	var none *FilledValues
	none.NoteCurrentDocument("", "loader-1")
	page.Filled().NoteCurrentDocument("", "")
}

// While the page is locked the snapshot is asked to show no input's value, and
// a click or hover names an element by its label alone; before the lock they
// are not.
func TestSnapshotAndNamesAreToldWhetherThePageIsLocked(test *testing.T) {
	chrome, page, finish := startFake(test)
	defer finish()
	chrome.setHistory([]string{"about:blank", "https://shop.example.test/sign-in"}, []int{1, 2}, -1, "loader-1")
	var expressions []string
	chrome.mutex.Lock()
	chrome.evaluate = func(expression string) any {
		expressions = append(expressions, expression)
		return map[string]any{"ok": true, "x": 1, "y": 1, "name": "input", "text": "", "title": "", "url": ""}
	}
	chrome.mutex.Unlock()
	if _, err := page.Snapshot(context.Background(), "", 0); err != nil {
		test.Fatal(err)
	}
	if _, _, err := page.Click(context.Background(), Target{Ref: 1}); err != nil {
		test.Fatal(err)
	}
	if !strings.HasSuffix(expressions[0], `"interactive", 20000, false)`) || !strings.Contains(expressions[1], "const isLocked = false ||") {
		test.Fatalf("an unlocked page was told it was locked:\n%s\n%s", expressions[0][len(expressions[0])-40:], expressions[1])
	}
	page.Filled().Remember("a-password")
	page.PrepareToFill(context.Background())
	expressions = nil
	if _, err := page.Snapshot(context.Background(), "text", 0); err != nil {
		test.Fatal(err)
	}
	if _, err := page.Hover(context.Background(), Target{Ref: 1}); err != nil {
		test.Fatal(err)
	}
	if !strings.HasSuffix(expressions[0], `"text", 20000, true)`) || !strings.Contains(expressions[1], "const isLocked = true ||") {
		test.Fatalf("a locked page was not told so:\n%s\n%s", expressions[0][len(expressions[0])-40:], expressions[1])
	}
}
