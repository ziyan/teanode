package browser

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// The person's tab carries their session, so what the agent may send to it
// and where it may go are held on the server, before the extension is asked.

func newReachRun(address string) *fakeBrowsingRun {
	return &fakeBrowsingRun{first: &fakeLink{address: address}, serverName: "mail.example.com"}
}

// A method that is on the list goes through to the person's browser.
func TestAMethodOnTheListIsSent(t *testing.T) {
	for _, method := range []string{"Input.dispatchMouseEvent", "Input.dispatchKeyEvent", "Input.insertText", "Network.enable", "Page.getLayoutMetrics", "Runtime.evaluate", "DOM.getDocument", "DOM.querySelector"} {
		run := newReachRun("")
		if _, err := runOnTab(t, run, fmt.Sprintf(`{"action":"cdp","target":"tab","method":%q}`, method)); err != nil {
			t.Errorf("%s was refused: %s", method, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 1 {
			t.Errorf("%s: the browser was asked %v", method, asked)
		}
	}
}

// Choosing files for a page and dropping files onto one put a file of the
// person's machine where the page can read it back, so they are refused by
// name, and so is every method that is not on the list, however it is
// written.
func TestAFileOrDragMethodIsRefusedAndNothingOffTheListIsSent(t *testing.T) {
	for _, method := range []string{"DOM.setFileInputFiles", "DOM.getFileInfo", "Input.dispatchDragEvent", "Input.setInterceptDrags", "Page.setInterceptFileChooserDialog"} {
		run := newReachRun("")
		_, err := runOnTab(t, run, fmt.Sprintf(`{"action":"cdp","target":"tab","method":%q,"params":{"files":["~/example"],"nodeId":1}}`, method))
		if err == nil || !strings.Contains(err.Error(), "does not choose files") {
			t.Errorf("%s: the model was told %v", method, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("%s: the browser was asked %v", method, asked)
		}
	}
	for _, method := range []string{
		"DOM.setAttributeValue", "DOM.", "Input.", "Input.dispatchDragEventX", "input.dispatchMouseEvent", " Input.dispatchMouseEvent",
		"Input.dispatchMouseEvent ", "Browser.setDownloadBehavior", "Page.setDownloadBehavior", "Network.clearBrowserCookies",
		"Storage.clearDataForOrigin", "Target.attachToTarget", "Fetch.enable", "Runtime.addBinding", "Emulation.setDeviceMetricsOverride",
		// What reads through frames and shadow roots, or the tab whole.
		"DOMSnapshot.captureSnapshot", "DOMSnapshot.enable", "Page.captureSnapshot", "Page.getFrameTree", "DOM.getNodeForLocation", "DOM.performSearch",
		"DOM.getSearchResults", "CSS.getComputedStyleForNode",
	} {
		run := newReachRun("")
		_, err := runOnTab(t, run, fmt.Sprintf(`{"action":"cdp","target":"tab","method":%q}`, method))
		if err == nil || !strings.Contains(err.Error(), "is not one of the methods an attached tab answers") {
			t.Errorf("%q: the model was told %v", method, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("%q: the browser was asked %v", method, asked)
		}
	}
	run := newReachRun("")
	if _, err := runOnTab(t, run, `{"action":"cdp","target":"tab"}`); err == nil || !strings.Contains(err.Error(), "a method is needed") {
		t.Errorf("a call with no method: %v", err)
	}
}

// The same refusals inside steps stop the steps there.
func TestARefusedCallInStepsStopsThem(t *testing.T) {
	run := newReachRun("")
	result, err := runOnTab(t, run, `{"action":"steps","target":"tab","steps":[{"action":"snapshot"},{"action":"cdp","method":"DOM.setFileInputFiles"},{"action":"snapshot"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "does not choose files") {
		t.Errorf("the steps answered %q", result.Content)
	}
	if asked := run.first.actionsAsked(); len(asked) != 1 || asked[0] != "snapshot" {
		t.Errorf("the browser was asked %v", asked)
	}
}

// An address that is the server's own, however it is written, is not opened,
// navigated to or reached by the protocol; nor is anything but http and
// https; an ordinary site is.
func TestTheServersOwnAddressIsNotOpenedNavigatedToOrReachedByTheProtocol(t *testing.T) {
	own := []string{
		"https://mail.example.com/",
		"http://mail.example.com/settings",
		"https://MAIL.Example.COM/",
		"https://mail.example.com:8443/x",
		"https://mail.example.com./x",
		"https://someone@mail.example.com/x",
	}
	// Written so that a browser reads a different host than this does, they
	// are refused all the same, if not in the same words.
	disguised := []string{"https://mail.example.com\\@example.org/", "https://mail.example.com%2e/x", "https://\tmail.example.com/", "https://mail.example.com\\x"}
	notWebsites := []string{"chrome-extension://abcdefgh/options.html", "file:///etc/hosts", "javascript:1", "data:text/html,hi", "blob:https://mail.example.com/1", "about:blank", "mail.example.com", "", " https://example.org/"}
	for _, action := range []string{"open", "navigate"} {
		for _, address := range own {
			run := newReachRun("")
			_, err := runOnTab(t, run, fmt.Sprintf(`{"action":%q,"target":"tab","url":%q}`, action, address))
			if err == nil || err.Error() != ownPagesRefusal {
				t.Errorf("%s %q: the model was told %v", action, address, err)
			}
			if asked := run.first.actionsAsked(); len(asked) != 0 {
				t.Errorf("%s %q: the browser was asked %v", action, address, asked)
			}
		}
		for _, address := range append(append([]string{}, disguised...), notWebsites...) {
			run := newReachRun("")
			_, err := runOnTab(t, run, fmt.Sprintf(`{"action":%q,"target":"tab","url":%q}`, action, address))
			if err == nil || !strings.Contains(err.Error(), "only http and https addresses") {
				t.Errorf("%s %q: the model was told %v", action, address, err)
			}
			if asked := run.first.actionsAsked(); len(asked) != 0 {
				t.Errorf("%s %q: the browser was asked %v", action, address, asked)
			}
		}
		run := newReachRun("")
		if _, err := runOnTab(t, run, fmt.Sprintf(`{"action":%q,"target":"tab","url":"https://shop.example.org/cart"}`, action)); err != nil {
			t.Errorf("%s of an ordinary site was refused: %s", action, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 1 || asked[0] != action {
			t.Errorf("%s of an ordinary site: the browser was asked %v", action, asked)
		}
	}
	for _, address := range append(append(append([]string{}, own...), disguised...), notWebsites...) {
		run := newReachRun("")
		_, err := runOnTab(t, run, fmt.Sprintf(`{"action":"cdp","target":"tab","method":"Page.navigate","params":{"url":%q}}`, address))
		if err == nil {
			t.Errorf("Page.navigate to %q was sent", address)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("Page.navigate to %q: the browser was asked %v", address, asked)
		}
	}
	run := newReachRun("")
	if _, err := runOnTab(t, run, `{"action":"cdp","target":"tab","method":"Page.navigate","params":{"url":"https://shop.example.org/"}}`); err != nil {
		t.Errorf("Page.navigate to an ordinary site was refused: %s", err)
	}
}

// The names the server has are the configured one, the one a passkey is
// bound to, the dashboard's origins, and the host the browser connected by.
func TestEveryNameTheServerHasIsItsOwnAddress(t *testing.T) {
	run := newReachRun("")
	run.first.serverHost = "localhost:8080"
	for address, isOwn := range map[string]bool{
		"http://localhost:9999/x":        true,
		"https://mail.example.com/":      true,
		"https://shop.example.org/":      false,
		"http://localhost.example.org/":  false,
		"https://mail.example.com.test/": false,
	} {
		_, err := runOnTab(t, run, fmt.Sprintf(`{"action":"open","target":"tab","url":%q}`, address))
		if isOwn != (err != nil && err.Error() == ownPagesRefusal) {
			t.Errorf("%q: own %v, but the model was told %v", address, isOwn, err)
		}
	}
	configuration := run.Configuration()
	configuration.Passkey.RelyingPartyID = "login.example.net"
	configuration.Passkey.Origins = []string{"https://dashboard.example.net:8443"}
	hosts := ownHostnames(configuration, run.first)
	for _, name := range []string{"mail.example.com", "login.example.net", "dashboard.example.net", "localhost"} {
		if !hosts[name] {
			t.Errorf("%s is not among %v", name, hosts)
		}
	}
}

// Every action that reads or acts in a tab is refused while the tab shows
// the server or a page of the extension, and the browser is not asked.
func TestNothingIsDoneInATabThatShowsTheServerOrTheExtension(t *testing.T) {
	actions := []string{
		`{"action":"navigate","url":"https://shop.example.org/"}`,
		`{"action":"snapshot"}`, `{"action":"screenshot"}`, `{"action":"click","ref":1}`, `{"action":"hover","ref":1}`,
		`{"action":"select","ref":1,"value":"a"}`, `{"action":"type","ref":1,"text":"a"}`, `{"action":"press","key":"Enter"}`,
		`{"action":"scroll"}`, `{"action":"wait"}`, `{"action":"back"}`, `{"action":"evaluate","expression":"1"}`,
		`{"action":"fetch","url":"/"}`, `{"action":"storage"}`, `{"action":"cdp","method":"Input.dispatchMouseEvent"}`,
		`{"action":"cdp","method":"Runtime.evaluate"}`, `{"action":"cdp_events"}`,
	}
	for _, address := range []string{"https://mail.example.com/", "https://mail.example.com:8443/inbox", "chrome-extension://abcdefgh/options.html", "blob:https://mail.example.com/1"} {
		for _, action := range actions {
			run := newReachRun(address)
			body := strings.Replace(action, `{`, `{"target":"tab",`, 1)
			_, err := runOnTab(t, run, body)
			if err == nil || err.Error() != ownPagesRefusal {
				t.Errorf("%s in %s: the model was told %v", action, address, err)
			}
			if asked := run.first.actionsAsked(); len(asked) != 0 {
				t.Errorf("%s in %s: the browser was asked %v", action, address, asked)
			}
		}
	}
	// The tab the browser lists as the current one counts as well, in case
	// the address it last gave is the older word.
	run := newReachRun("https://shop.example.org/")
	run.first.listed = []tools.TabAddress{{Tab: "7", URL: "https://mail.example.com/", IsOwn: true, IsCurrent: true}}
	if _, err := runOnTab(t, run, `{"action":"snapshot","target":"tab"}`); err == nil || err.Error() != ownPagesRefusal {
		t.Errorf("a current tab that is the server's: %v", err)
	}
	// What is not in a page is still done: listing the tabs, opening another
	// beside, letting the protocol go, closing one.
	for _, action := range []string{`{"action":"tabs"}`, `{"action":"open","url":"https://shop.example.org/"}`, `{"action":"cdp_stop"}`, `{"action":"close"}`} {
		run := newReachRun("https://mail.example.com/")
		body := strings.Replace(action, `{`, `{"target":"tab",`, 1)
		if _, err := runOnTab(t, run, body); err != nil {
			t.Errorf("%s was refused: %s", action, err)
		}
	}
}

// An ordinary site is read and acted in as ever.
func TestAnOrdinarySiteIsStillDrivenInEveryWay(t *testing.T) {
	for _, action := range []string{
		`{"action":"snapshot"}`, `{"action":"screenshot"}`, `{"action":"click","ref":1}`, `{"action":"type","ref":1,"text":"a"}`,
		`{"action":"evaluate","expression":"1"}`, `{"action":"fetch","url":"/"}`, `{"action":"storage"}`, `{"action":"back"}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent"}`, `{"action":"cdp_events"}`, `{"action":"switch"}`, `{"action":"tabs"}`,
	} {
		run := newReachRun("https://shop.example.org/cart")
		run.first.listed = []tools.TabAddress{{Tab: "7", URL: "https://shop.example.org/cart", IsOwn: true, IsCurrent: true}}
		body := strings.Replace(action, `{`, `{"target":"tab",`, 1)
		if _, err := runOnTab(t, run, body); err != nil {
			t.Errorf("%s was refused: %s", action, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 1 {
			t.Errorf("%s: the browser was asked %v", action, asked)
		}
	}
}

// A switch to a tab that shows the server or an extension page is refused,
// by number, by a piece of its address or title, and with nothing said when
// the person's own tab is the one that does.
func TestSwitchingToTheServersTabIsRefused(t *testing.T) {
	listed := []tools.TabAddress{
		{Tab: "7", Title: "A shop", URL: "https://shop.example.org/", IsOwn: true, IsCurrent: true},
		{Tab: "8", Title: "Your agent", URL: "https://mail.example.com/drawer"},
		{Tab: "9", Title: "Options", URL: "chrome-extension://abcdefgh/options.html"},
		{Tab: "10", Title: "A news page", URL: "https://news.example.net/"},
	}
	for _, body := range []string{`{"tab":8}`, `{"tab":9}`, `{"url":"mail.example.com"}`, `{"url":"abcdefgh"}`, `{"text":"Your agent"}`, `{"url":"example"}`} {
		run := newReachRun("https://shop.example.org/")
		run.first.listed = listed
		_, err := runOnTab(t, run, strings.Replace(body, `{`, `{"action":"switch","target":"tab",`, 1))
		if err == nil || err.Error() != ownPagesRefusal {
			t.Errorf("switch %s: the model was told %v", body, err)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("switch %s: the browser was asked %v", body, asked)
		}
	}
	for _, body := range []string{`{"tab":10}`, `{"tab":7}`, `{"url":"news.example.net"}`, `{}`, `{"tab":99}`} {
		run := newReachRun("https://shop.example.org/")
		run.first.listed = listed
		if _, err := runOnTab(t, run, strings.Replace(body, `{`, `{"action":"switch","target":"tab",`, 1)); err != nil {
			t.Errorf("switch %s was refused: %s", body, err)
		}
	}
	// With no word, the person's own tab: refused when that is the server's.
	run := newReachRun("https://news.example.net/")
	run.first.listed = []tools.TabAddress{{Tab: "7", URL: "https://mail.example.com/", IsOwn: true}, {Tab: "10", URL: "https://news.example.net/", IsCurrent: true}}
	if _, err := runOnTab(t, run, `{"action":"switch","target":"tab"}`); err == nil || err.Error() != ownPagesRefusal {
		t.Errorf("switch to the person's own tab, which is the server's: %v", err)
	}
	// The number reaches the browser, which it did not.
	run = newReachRun("https://shop.example.org/")
	run.first.listed = listed
	if _, err := runOnTab(t, run, `{"action":"switch","target":"tab","tab":10}`); err != nil {
		t.Fatal(err)
	}
}

// A page that ends on the server by a link or a redirect is not gone on
// from: the model is told so, in the same words.
func TestAnAnswerThatEndedOnTheServerIsNotGoneOnFrom(t *testing.T) {
	for action, answer := range map[string]string{
		"open":     `{"tab":101,"url":"https://mail.example.com/","title":"Dashboard"}`,
		"navigate": `{"url":"https://mail.example.com:8443/x","title":"Dashboard"}`,
		"switch":   `{"tab":8,"url":"chrome-extension://abcdefgh/options.html","title":"Options"}`,
		"back":     `{"url":"https://mail.example.com/","title":"Dashboard"}`,
		"click":    `{"clicked":"a","now":{"url":"https://mail.example.com/","title":"Dashboard"}}`,
	} {
		run := newReachRun("https://shop.example.org/")
		run.first.answer = answer
		body := `{"action":"` + action + `","target":"tab","url":"https://redirects.example.org/"}`
		_, err := runOnTab(t, run, body)
		if err == nil || !strings.HasPrefix(err.Error(), ownPagesRefusal) {
			t.Errorf("%s: the model was told %v", action, err)
		}
	}
	// What a script returns may hold any address and is not read for one.
	run := newReachRun("https://shop.example.org/")
	run.first.answer = `{"url":"https://mail.example.com/"}`
	if _, err := runOnTab(t, run, `{"action":"evaluate","target":"tab","expression":"({url: 1})"}`); err != nil {
		t.Errorf("an evaluate whose value held an address was refused: %s", err)
	}
}

// The server's list of DevTools methods and the extension's are the same.
func TestTheExtensionAndTheServerAllowTheSameMethods(t *testing.T) {
	source, err := os.ReadFile("../../../../web/extension/src/reach.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	listed := func(name string) []string {
		start := strings.Index(text, "export const "+name+" = [")
		if start < 0 {
			t.Fatalf("reach.js has no %s", name)
		}
		end := strings.Index(text[start:], "\n]")
		var methods []string
		for _, match := range regexp.MustCompile(`'([A-Z][A-Za-z]*\.[a-zA-Z][A-Za-z0-9]*)'`).FindAllStringSubmatch(text[start:start+end], -1) {
			methods = append(methods, match[1])
		}
		sort.Strings(methods)
		return methods
	}
	same := func(name string, expected map[string]bool) {
		var want []string
		for method := range expected {
			want = append(want, method)
		}
		sort.Strings(want)
		if got := listed(name); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("the extension's %s are\n%v\nand the server's\n%v", name, got, want)
		}
	}
	same("CDP_ALLOWED", cdpAllowedMethods)
	same("CDP_FILE_METHODS", cdpFileMethods)
	if !strings.Contains(text, "OWN_PAGES_REFUSAL = \""+ownPagesRefusal+"\"") && !strings.Contains(text, "OWN_PAGES_REFUSAL =\n  \""+ownPagesRefusal+"\"") {
		t.Errorf("the extension says something else than %q", ownPagesRefusal)
	}
}

// The panel on a page is a frame of the server's origin, and a frame of the
// same site is in the tab's process: what reads through frames, shadow
// roots and other execution contexts is refused by its parameters.
func TestWhatReadsThroughFramesAndContextsIsRefusedByItsParameters(t *testing.T) {
	refused := []string{
		`{"method":"DOM.getDocument","params":{"depth":-1,"pierce":true}}`,
		`{"method":"DOM.describeNode","params":{"nodeId":1,"depth":-1,"pierce":true}}`,
		`{"method":"DOM.querySelectorAll","params":{"nodeId":1,"selector":"iframe","pierce":true}}`,
		`{"method":"Runtime.evaluate","params":{"expression":"1","contextId":3}}`,
		`{"method":"Runtime.evaluate","params":{"expression":"1","uniqueContextId":"abc"}}`,
		`{"method":"Runtime.callFunctionOn","params":{"functionDeclaration":"1","executionContextId":3}}`,
		`{"method":"DOM.resolveNode","params":{"nodeId":1,"executionContextId":3}}`,
		`{"method":"Accessibility.getFullAXTree","params":{"frameId":"A1B2"}}`,
		`{"method":"Page.navigate","params":{"url":"https://shop.example.org/","frameId":"A1B2"}}`,
	}
	for _, body := range refused {
		run := newReachRun("")
		_, err := runOnTab(t, run, `{"action":"cdp","target":"tab",`+body[1:])
		if err == nil {
			t.Errorf("%s was sent", body)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("%s: the browser was asked %v", body, asked)
		}
	}
	for _, body := range []string{
		`{"method":"DOM.getDocument","params":{"depth":-1}}`,
		`{"method":"DOM.getDocument","params":{"depth":-1,"pierce":false}}`,
		`{"method":"Runtime.evaluate","params":{"expression":"1"}}`,
		`{"method":"Accessibility.getFullAXTree"}`,
	} {
		run := newReachRun("")
		if _, err := runOnTab(t, run, `{"action":"cdp","target":"tab",`+body[1:]); err != nil {
			t.Errorf("%s was refused: %s", body, err)
		}
	}
}

// A fetch goes out with the page's session, so where it goes is held to the
// same lines as where a tab goes, resolved as the page would.
func TestAFetchIsHeldToTheSameAddressesAsATab(t *testing.T) {
	for _, address := range []string{
		"https://mail.example.com/api/v1/graphql", "http://MAIL.example.com:8443/x", "//mail.example.com/x", "file:///etc/hosts",
		"https://mail.example.com\\@example.org/", "javascript:1", "chrome-extension://abcdefgh/options.html", "",
	} {
		run := newReachRun("https://app.example.com/page")
		_, err := runOnTab(t, run, fmt.Sprintf(`{"action":"fetch","target":"tab","url":%q}`, address))
		if err == nil {
			t.Errorf("fetch %q was sent", address)
		}
		if asked := run.first.actionsAsked(); len(asked) != 0 {
			t.Errorf("fetch %q: the browser was asked %v", address, asked)
		}
	}
	for _, address := range []string{"/statements/2025.pdf", "statements?id=3", "https://app.example.com/x", "https://cdn.example.net/a.json"} {
		run := newReachRun("https://app.example.com/page")
		if _, err := runOnTab(t, run, fmt.Sprintf(`{"action":"fetch","target":"tab","url":%q}`, address)); err != nil {
			t.Errorf("fetch %q was refused: %s", address, err)
		}
	}
	// The same inside steps.
	run := newReachRun("https://app.example.com/page")
	result, err := runOnTab(t, run, `{"action":"steps","target":"tab","steps":[{"action":"fetch","url":"https://mail.example.com/x"}]}`)
	if err != nil || !strings.Contains(result.Content, ownPagesRefusal) {
		t.Errorf("steps: %v %v", result, err)
	}
}

// eventsAnswer is what the extension says to cdp_events: the events, as
// JSON, and the session they belong to.
func eventsAnswer(session string, events ...string) string {
	return fmt.Sprintf(`{"kept":%d,"session":%q,"events":[%s]}`, len(events), session, strings.Join(events, ","))
}

func requestCreated(requestID, address string) string {
	return fmt.Sprintf(`{"method":"Network.requestWillBeSent","params":{"requestId":%q,"request":{"url":%q}}}`, requestID, address)
}

func socketCreated(requestID, address string) string {
	return fmt.Sprintf(`{"method":"Network.webSocketCreated","params":{"requestId":%q,"url":%q}}`, requestID, address)
}

func frameSent(requestID, text string) string {
	return fmt.Sprintf(`{"method":"Network.webSocketFrameSent","params":{"requestId":%q,"response":{"opcode":1,"payloadData":%q}}}`, requestID, text)
}

// Requests to the server's host, with the Authorization header the panel's
// frame signs in with, are among a same-site page's, and are not shown.
func TestRequestsToTheServerAreNotShownInWhatTheTabWatched(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.answer = `{"kept":5,"events":[
		{"method":"Network.requestWillBeSentExtraInfo","params":{"requestId":"1.1","headers":{"authorization":"Bearer example-token"}}},
		{"method":"Network.requestWillBeSent","params":{"requestId":"1.1","request":{"url":"https://mail.example.com/api/v1/graphql","headers":{"Authorization":"Bearer example-token"}}}},
		{"method":"Network.responseReceived","params":{"requestId":"1.1","response":{"url":"https://mail.example.com/api/v1/graphql","status":200}}},
		{"method":"Network.loadingFinished","params":{"requestId":"1.1"}},
		{"method":"Network.requestWillBeSent","params":{"requestId":"2.2","request":{"url":"https://cdn.example.net/a.js"}}}]}`
	result, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "example-token") || strings.Contains(result.Content, "mail.example.com") || strings.Contains(result.Content, "1.1") {
		t.Errorf("the server's requests are shown: %s", result.Content)
	}
	if !strings.Contains(result.Content, "cdn.example.net") {
		t.Errorf("another site's request is gone: %s", result.Content)
	}
}

// A page that makes many requests pushes the first events of a connection
// out of the few the extension keeps, while the frames that carry the
// header go on arriving. A frame of a connection whose creation nobody saw
// is not shown, and one that was seen made to another host is.
func TestAConnectionWhoseCreationWasPushedOutIsNotShown(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	var flood []string
	for index := 0; index < 600; index++ {
		flood = append(flood, requestCreated(fmt.Sprintf("9.%d", index), "https://cdn.example.net/a.js"))
	}
	run.first.sequence = []string{
		// The first window holds the creation of both connections.
		eventsAnswer("tab-1", socketCreated("5.1", "wss://mail.example.com/api/v1/socket"), socketCreated("5.2", "wss://live.example.net/feed"), frameSent("5.1", "first"), frameSent("5.2", "first")),
		// Then the flood, and a frame of each connection, the creations gone.
		eventsAnswer("tab-1", append(flood, frameSent("5.1", "Bearer example-token"), frameSent("5.2", "later"))...),
	}
	first, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab","limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Content, "mail.example.com") || strings.Contains(first.Content, "5.1") {
		t.Errorf("the first window shows the server's connection: %s", first.Content)
	}
	if !strings.Contains(first.Content, "live.example.net") {
		t.Errorf("the first window does not show the other host's connection: %s", first.Content)
	}
	flooded, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab","limit":10}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flooded.Content, "example-token") || strings.Contains(flooded.Content, `"5.1"`) {
		t.Errorf("a frame of the server's connection is shown once its creation is gone: %s", flooded.Content)
	}
	if !strings.Contains(flooded.Content, `"later"`) {
		t.Errorf("a frame of the other host's connection is gone: %s", flooded.Content)
	}
}

// What was not seen created in the answers this server has had is not shown,
// whatever it looks like, and nothing is shown that the server cannot place.
func TestEventsOfARequestNeverSeenCreatedAreNotShown(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.answer = eventsAnswer("tab-1",
		frameSent("7.1", "Bearer example-token"),
		`{"method":"Network.responseReceived","params":{"requestId":"7.2","response":{"url":"https://cdn.example.net/b.js"}}}`,
		`{"method":"Network.dataReceived","params":{"requestId":"7.3"}}`,
		`{"method":"Network.policyUpdated","params":{}}`,
		`{"method":"Log.entryAdded","params":{"entry":{"text":"a message"}}}`)
	result, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "example-token") || strings.Contains(result.Content, "7.") || strings.Contains(result.Content, "policyUpdated") {
		t.Errorf("an event nobody saw created is shown: %s", result.Content)
	}
	if !strings.Contains(result.Content, "a message") {
		t.Errorf("an event that is not the network's is gone: %s", result.Content)
	}
}

// A request that went to another host and was sent on to the server is not
// shown from the redirect on, and one that started at the server and was
// sent elsewhere never was.
func TestARedirectToTheServerHidesTheRequestAndOneFromItNeverShows(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.answer = eventsAnswer("tab-1",
		requestCreated("3.1", "https://cdn.example.net/a"),
		`{"method":"Network.requestWillBeSent","params":{"requestId":"3.1","request":{"url":"https://mail.example.com/x"},"redirectResponse":{"url":"https://cdn.example.net/a","status":302}}}`,
		frameSent("3.1", "after"),
		requestCreated("3.2", "https://mail.example.com/y"),
		`{"method":"Network.requestWillBeSent","params":{"requestId":"3.2","request":{"url":"https://cdn.example.net/z"},"redirectResponse":{"url":"https://mail.example.com/y","status":302}}}`)
	result, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, shown := range []string{"3.1", "3.2", "mail.example.com", "after"} {
		if strings.Contains(result.Content, shown) {
			t.Errorf("%s is shown: %s", shown, result.Content)
		}
	}
}

// An answer that cannot be read shows nothing; the method and the limit
// choose from what may be shown; the extension is asked for everything.
func TestWhatTheTabWatchedIsChosenFromAfterItIsJudged(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.answer = eventsAnswer("tab-1",
		requestCreated("4.1", "https://cdn.example.net/1"),
		requestCreated("4.2", "https://cdn.example.net/2"),
		requestCreated("4.3", "https://cdn.example.net/3"),
		`{"method":"Log.entryAdded","params":{"entry":{"text":"a message"}}}`)
	result, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab","method":"Network.","limit":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "4.1") || !strings.Contains(result.Content, "4.2") || !strings.Contains(result.Content, "4.3") || strings.Contains(result.Content, "a message") {
		t.Errorf("limit and method: %s", result.Content)
	}
	broken := newReachRun("https://app.example.com/page")
	broken.first.answer = `{"events":"not a list"}`
	if _, err := runOnTab(t, broken, `{"action":"cdp_events","target":"tab"}`); err == nil {
		t.Error("an answer that cannot be read is passed on")
	}
}

// A body is read only for a request cdp_events has shown, so the server's
// requests and ones nobody saw created are not read.
func TestABodyIsReadOnlyForARequestThatWasShown(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.sequence = []string{
		eventsAnswer("tab-1", requestCreated("6.1", "https://mail.example.com/api"), requestCreated("6.2", "https://cdn.example.net/a")),
		`{"result":{"body":"hello"}}`,
	}
	if _, err := runOnTab(t, run, `{"action":"cdp","target":"tab","method":"Network.getResponseBody","params":{"requestId":"6.2"}}`); err == nil {
		t.Error("the body of a request nobody listed is read")
	}
	run.first.sequence = []string{eventsAnswer("tab-1", requestCreated("6.1", "https://mail.example.com/api"), requestCreated("6.2", "https://cdn.example.net/a"))}
	if _, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab"}`); err != nil {
		t.Fatal(err)
	}
	for _, requestID := range []string{"6.1", "6.9", ""} {
		for _, method := range []string{"Network.getResponseBody", "Network.getRequestPostData"} {
			if _, err := runOnTab(t, run, fmt.Sprintf(`{"action":"cdp","target":"tab","method":%q,"params":{"requestId":%q}}`, method, requestID)); err == nil {
				t.Errorf("%s of %q is read", method, requestID)
			}
		}
	}
	run.first.sequence = []string{`{"result":{"body":"hello"}}`}
	if result, err := runOnTab(t, run, `{"action":"cdp","target":"tab","method":"Network.getResponseBody","params":{"requestId":"6.2"}}`); err != nil || !strings.Contains(result.Content, "hello") {
		t.Errorf("the body of a request that was shown: %v %v", result, err)
	}
}

// Letting the protocol go forgets what was seen, so a connection's frames
// are not shown from a window that no longer holds its creation.
func TestLettingTheProtocolGoForgetsTheRequestsSeen(t *testing.T) {
	run := newReachRun("https://app.example.com/page")
	run.first.sequence = []string{
		eventsAnswer("tab-1", socketCreated("8.1", "wss://live.example.net/feed")),
		`{"stopped":true}`,
		eventsAnswer("tab-1", frameSent("8.1", "late")),
	}
	for _, arguments := range []string{`{"action":"cdp_events","target":"tab"}`, `{"action":"cdp_stop","target":"tab"}`} {
		if _, err := runOnTab(t, run, arguments); err != nil {
			t.Fatal(err)
		}
	}
	result, err := runOnTab(t, run, `{"action":"cdp_events","target":"tab"}`)
	if err != nil || strings.Contains(result.Content, "late") {
		t.Errorf("a frame of a forgotten connection is shown: %v %v", result, err)
	}
}

func TestPlacesAreTheHostnamesAndMarks(t *testing.T) {
	for address, wanted := range map[string]string{
		"https://Mail.Example.com:8443/x":      "mail.example.com",
		"wss://mail.example.com./socket":       "mail.example.com",
		"blob:https://mail.example.com/0f1e":   "mail.example.com",
		"chrome-extension://abcdef/panel.html": placeExtension,
		"blob:chrome-extension://abcdef/0f1e":  placeExtension,
		"data:text/plain;base64,AAAA":          "",
		"http:mail.example.com/":               placeUnreadable,
		"https://\u0000bad/":                   placeUnreadable,
		"https://0x7f.1/":                      placeUnreadable,
		"https://exa%6Dple.com/":               placeUnreadable,
		"%zz":                                  placeUnreadable,
		"https:///path":                        placeUnreadable,
		"https://cdn.example.net/a.js":         "cdn.example.net",
	} {
		if got := placeOf(address); got != wanted {
			t.Errorf("placeOf(%q) = %q, wanted %q", address, got, wanted)
		}
	}
}
