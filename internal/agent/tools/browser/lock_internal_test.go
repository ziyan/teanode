package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	devtools "github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/models"
)

// filledTab is a run whose tab 7 was filled with a password in its first
// document.
func (self *world) filledTab(test *testing.T) *signInRun {
	test.Helper()
	run := self.tabRun(shopAddress)
	run.tab.tabID = "7"
	run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-1","commitCount":3}`
	run.tab.answer = `{"result":"fine"}`
	if _, err := browse(run, fillLogin(self.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	return run
}

// The lock is the filled tab's: switching to another tab or opening one does
// not unlock it, another tab is not locked by it, and a new document of
// another tab does not unlock it.
func TestTheLockBelongsToTheFilledTabAndNoOtherTab(test *testing.T) {
	found := newWorld(test)
	run := found.filledTab(test)
	evaluate := `{"action":"evaluate","expression":"1+1"}`
	mustRefuse(test, run, evaluate, lockedWords)

	// Switching to another tab: the actions go there, where nothing was filled.
	run.tab.answer = `{"tab":8,"url":"https://other.example.test/","title":"Another"}`
	if _, err := browse(run, `{"action":"switch","tab":8}`); err != nil {
		test.Fatal(err)
	}
	if run.tab.tabID != "8" {
		test.Fatalf("the tab the actions go to is %q", run.tab.tabID)
	}
	if _, err := browse(run, evaluate); err != nil {
		test.Fatalf("a script in another tab: %s", err)
	}
	// And the call carries the tab it was checked for.
	asked := run.tab.actions()
	if last := asked[len(asked)-1]; last.action != "evaluate" || !strings.Contains(last.arguments, `"expectedTab":"8"`) {
		test.Fatalf("the call to the other tab: %+v", last)
	}
	// Opening a new tab does not unlock the first.
	run.tab.answer = `{"tab":9,"url":"https://shop.example.test/","title":"New"}`
	if _, err := browse(run, `{"action":"open","url":"https://shop.example.test/"}`); err != nil {
		test.Fatal(err)
	}
	if !run.tab.Filled().IsLocked("7") || run.tab.Filled().IsLocked("9") {
		test.Fatal("opening a tab changed the lock")
	}
	// A new document of another tab does not unlock it either.
	run.tab.Filled().Committed("8", devtools.Commit{DocumentID: "doc-80", Address: "https://other.example.test/more", Count: 1})
	run.tab.Filled().Committed("9", devtools.Commit{DocumentID: "doc-90", Address: "https://shop.example.test/x", Count: 1})
	// Back to the filled tab, by switching and by the list of tabs: refused.
	run.tab.answer = `{"tab":7,"url":"https://shop.example.test/sign-in","title":"Sign in"}`
	if _, err := browse(run, `{"action":"switch","tab":7}`); err != nil {
		test.Fatal(err)
	}
	mustRefuse(test, run, evaluate, lockedWords)
	run.tab.answer = `{"tabs":[{"tab":7,"own":true,"current":false},{"tab":9,"own":false,"current":true}]}`
	if _, err := browse(run, `{"action":"tabs"}`); err != nil {
		test.Fatal(err)
	}
	if run.tab.tabID != "9" {
		test.Fatalf("the list says the actions go to %q", run.tab.tabID)
	}
	run.tab.answer = `{"result":"fine"}`
	if _, err := browse(run, evaluate); err != nil {
		test.Fatalf("a script in the tab the list names: %s", err)
	}
	// The filled tab commits a new document: unlocked.
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 4})
	if run.tab.Filled().IsLocked("7") {
		test.Fatal("a new document of the filled tab did not unlock it")
	}
}

// A fill locks the tab the extension says it filled, even when the server had
// another in mind, and a fill that failed still locks the tab it was sent to.
func TestAFillLocksTheTabTheExtensionNamesAndATabThatFailed(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.tabID = "7"
	run.tab.fillAnswer = `{"isFilled":true,"tab":12,"documentId":"doc-1","commitCount":3}`
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	if !run.tab.Filled().IsLocked("12") || !run.tab.Filled().IsLocked("7") || run.tab.tabID != "12" {
		test.Fatalf("tab %q, 12 locked %v, 7 locked %v", run.tab.tabID, run.tab.Filled().IsLocked("12"), run.tab.Filled().IsLocked("7"))
	}

	failing := found.tabRun(shopAddress)
	failing.tab.tabID = "5"
	failing.tab.failure = "the tab is gone"
	if _, err := browse(failing, fillLogin(found.grantedLogin)); err == nil {
		test.Fatal("a fill that failed was taken for one that worked")
	}
	if !failing.tab.Filled().IsLocked("5") {
		test.Fatal("the password may have been typed, and the tab is not locked")
	}
}

// Every DevTools method that could read a field, a request body or the
// network is refused on a locked tab; moving the page and real input are not.
func TestALockedTabAnswersOnlyInputAndNavigationOfTheDevToolsProtocol(test *testing.T) {
	found := newWorld(test)
	run := found.filledTab(test)
	asked := len(run.tab.actions())
	for _, method := range []string{
		"Runtime.evaluate", "Runtime.callFunctionOn", "Runtime.enable", "DOM.getDocument", "DOM.getOuterHTML", "DOMSnapshot.captureSnapshot",
		"Page.captureSnapshot", "Page.captureScreenshot", "Network.enable", "Network.disable", "Network.getRequestPostData", "Network.getResponseBody",
		"Network.getCookies", "Fetch.enable", "Fetch.getResponseBody", "Accessibility.getFullAXTree", "CSS.getComputedStyleForNode", "Storage.getCookies",
		"Emulation.setEmulatedMedia", "Log.enable", " Runtime.evaluate", "Input", "", "Input.setInterceptDrags", "Input.dispatchDragEvent",
	} {
		mustRefuse(test, run, fmt.Sprintf(`{"action":"cdp","method":%q}`, method), lockedWords)
	}
	for _, action := range []string{"cdp_events", "screenshot", "storage", "evaluate"} {
		mustRefuse(test, run, fmt.Sprintf(`{"action":%q,"expression":"1"}`, action), lockedWords)
	}
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked while locked: %v", run.tab.actions()[asked:])
	}
	for _, method := range []string{"Input.dispatchMouseEvent", "Input.dispatchKeyEvent", "Input.dispatchTouchEvent", "Input.insertText", "Page.reload", "Page.getNavigationHistory"} {
		if _, err := browse(run, fmt.Sprintf(`{"action":"cdp","method":%q}`, method)); err != nil {
			test.Errorf("%s on a locked tab: %s", method, err)
		}
	}
	// Typing a script address is typing: it goes to the page like any text.
	if _, err := browse(run, `{"action":"cdp","method":"Input.insertText","params":{"text":"javascript:note to self"}}`); err != nil {
		test.Errorf("typed text on a locked tab: %s", err)
	}
	if _, err := browse(run, `{"action":"cdp_stop"}`); err != nil {
		test.Fatalf("cdp_stop on a locked tab: %s", err)
	}
	// Unlocked, script goes through again. (A request's body is answered only
	// for a request cdp_events has shown, locked or not: see reach.go.)
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 4})
	if _, err := browse(run, `{"action":"cdp","method":"Runtime.evaluate"}`); err != nil {
		test.Errorf("Runtime.evaluate on an unlocked tab: %s", err)
	}
	// The network log would carry the session's cookies, so a tab signed in to
	// does not turn it on again (see script.go).
	mustRefuse(test, run, `{"action":"cdp","method":"Network.enable"}`, sessionReadWords)
}

// Moving a locked page is allowed so the sign-in can go on, but only the plain
// forms: a navigation to an http or https address and nothing else sent with
// it, a reload with at most ignoreCache. A script or data address, a script to
// run on load, or such an address in any other parameter of a command is
// refused, as is a navigate action to one, and none reaches the tab.
func TestALockedTabIsOnlyMovedToWebAddressesWithNothingElse(test *testing.T) {
	found := newWorld(test)
	run := found.filledTab(test)
	asked := len(run.tab.actions())
	for _, arguments := range []string{
		`{"action":"cdp","method":"Page.navigate"}`,
		`{"action":"cdp","method":"Page.navigate","params":{}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"javascript:alert(1)"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"JaVaScRiPt:alert(1)"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":" javascript:alert(1)"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"java\tscript:alert(1)"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"data:text/html,<script>1</script>"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"file:///etc/hostname"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"about:blank"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"https://"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":42}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"https://shop.example.test/","referrer":"javascript:1"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"https://shop.example.test/","transitionType":"typed"}}`,
		`{"action":"cdp","method":"Page.reload","params":{"scriptToEvaluateOnLoad":"document.title"}}`,
		`{"action":"cdp","method":"Page.reload","params":{"ignoreCache":"yes"}}`,
		`{"action":"cdp","method":"Page.reload","params":{"ignoreCache":true,"scriptToEvaluateOnLoad":"1"}}`,
		`{"action":"cdp","method":"Page.getFrameTree","params":{"x":{"y":["data:text/html,1"]}}}`,
		`{"action":"cdp","method":"Page.getLayoutMetrics","params":{"x":"javascript:1"}}`,
		`{"action":"navigate","url":"javascript:alert(1)"}`,
		`{"action":"navigate","url":"data:text/html,1"}`,
		`{"action":"navigate","url":"shop.example.test/cart"}`,
		`{"action":"navigate"}`,
	} {
		mustRefuse(test, run, arguments, "http or https")
	}
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked while locked: %v", run.tab.actions()[asked:])
	}
	for _, arguments := range []string{
		`{"action":"cdp","method":"Page.navigate","params":{"url":"https://shop.example.test/account"}}`,
		`{"action":"cdp","method":"Page.navigate","params":{"url":"http://shop.example.test/account"}}`,
		`{"action":"cdp","method":"Page.reload"}`,
		`{"action":"cdp","method":"Page.reload","params":{}}`,
		`{"action":"cdp","method":"Page.reload","params":{"ignoreCache":true}}`,
		`{"action":"navigate","url":"https://shop.example.test/cart"}`,
	} {
		if _, err := browse(run, arguments); err != nil {
			test.Errorf("%s on a locked tab: %s", arguments, err)
		}
	}
}

// The tab a call is checked for is the server's belief, never the model's.
func TestTheModelCannotNameTheTabACallIsCheckedFor(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.answer = `{"result":"fine"}`
	arguments := `{"action":"evaluate","expression":"1","expectedTab":"99"}`
	if _, err := browse(run, arguments); err != nil {
		test.Fatal(err)
	}
	if asked := run.tab.actions(); strings.Contains(asked[len(asked)-1].arguments, "99") || strings.Contains(asked[len(asked)-1].arguments, "expectedTab") {
		test.Fatalf("the model's tab was passed on: %s", asked[len(asked)-1].arguments)
	}
	run.tab.tabID = "7"
	if _, err := browse(run, arguments); err != nil {
		test.Fatal(err)
	}
	if asked := run.tab.actions(); !strings.Contains(asked[len(asked)-1].arguments, `"expectedTab":"7"`) || strings.Contains(asked[len(asked)-1].arguments, "99") {
		test.Fatalf("the server's tab was not passed on: %s", asked[len(asked)-1].arguments)
	}
	// An action that is not checked against a lock carries none.
	if _, err := browse(run, `{"action":"click","ref":3,"expectedTab":"99"}`); err != nil {
		test.Fatal(err)
	}
	if asked := run.tab.actions(); strings.Contains(asked[len(asked)-1].arguments, "expectedTab") {
		test.Fatalf("a click carries a tab: %s", asked[len(asked)-1].arguments)
	}
	// The fill carries it too (on a document no script ran in).
	// The first script ran before the server heard the tab's number.
	for _, tab := range []string{"", "7"} {
		run.tab.Filled().Committed(tab, devtools.Commit{DocumentID: "doc-1", Address: shopAddress, Count: 1})
	}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	if asked := run.tab.actions(); !strings.Contains(asked[len(asked)-1].arguments, `"expectedTab":"7"`) {
		test.Fatalf("a fill carries no tab: %s", asked[len(asked)-1].arguments)
	}
}

// A picture of a filled page is refused in the headless browser too, and the
// network log is turned off before the first value goes in.
func TestTheHeadlessPageTurnsTheNetworkLogOffBeforeTheFirstValueAndRefusesPictures(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, shopAddress)
	if _, err := browse(run, `{"action":"screenshot"}`); err != nil {
		test.Fatalf("a picture before a fill: %s", err)
	}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	disabledAt, firstFillAt := -1, -1
	chrome.mutex.Lock()
	for index, command := range chrome.commands {
		if command.method == "Network.disable" && disabledAt < 0 {
			disabledAt = index
		}
		if command.method == "Runtime.evaluate" && strings.Contains(command.parameters, "isFilled") && firstFillAt < 0 {
			firstFillAt = index
		}
	}
	chrome.mutex.Unlock()
	if disabledAt < 0 || firstFillAt < 0 || disabledAt > firstFillAt {
		test.Fatalf("Network.disable at %d, the first fill at %d", disabledAt, firstFillAt)
	}
	shots := len(chrome.sent("Page.captureScreenshot"))
	mustRefuse(test, run, `{"action":"screenshot"}`, lockedWords)
	if stepped, err := browse(run, `{"action":"steps","steps":[{"action":"screenshot"}]}`); err != nil || !strings.Contains(stepped.Content, lockedWords) {
		test.Fatalf("a picture in steps: %v %v", stepped, err)
	}
	if len(chrome.sent("Page.captureScreenshot")) != shots {
		test.Fatal("a locked page was photographed")
	}
}

// A snapshot is scrubbed whole before it is cut into parts, so that a value on
// the edge of a part is in neither half.
func TestASnapshotIsScrubbedBeforeItIsCutIntoParts(test *testing.T) {
	found := newWorld(test)
	text := strings.Repeat("x", 18) + inventedPassword + strings.Repeat("y", 30)
	check := func(name string, first func() (*tools.Result, error), next func(offset int) (*tools.Result, error)) {
		test.Helper()
		var seen string
		offset := 0
		for index := 0; index < 20; index++ {
			var result *tools.Result
			var err error
			if index == 0 {
				result, err = first()
			} else {
				result, err = next(offset)
			}
			if err != nil {
				test.Fatalf("%s: %s", name, err)
			}
			var part struct {
				Text       string `json:"text"`
				NextOffset int    `json:"next_offset"`
			}
			if err := json.Unmarshal([]byte(result.Content), &part); err != nil {
				test.Fatalf("%s: %s: %s", name, err, result.Content)
			}
			seen += part.Text
			if part.NextOffset == 0 {
				break
			}
			offset = part.NextOffset
		}
		for _, run := range []string{"Tr0ub", "dor&3", "-example", inventedPassword} {
			if strings.Contains(seen, run) {
				test.Errorf("%s: %q reached the model in %q", name, run, seen)
			}
		}
		if !strings.Contains(seen, "[hidden]") {
			test.Errorf("%s: nothing hidden in %q", name, seen)
		}
	}

	tabRun := found.filledTab(test)
	tabRun.tab.answer = fmt.Sprintf(`{"title":"t","url":"https://shop.example.test/","text":%q}`, text)
	check("tab", func() (*tools.Result, error) { return browse(tabRun, `{"action":"snapshot","max_characters":25}`) },
		func(offset int) (*tools.Result, error) {
			return browse(tabRun, fmt.Sprintf(`{"action":"snapshot","max_characters":25,"offset":%d}`, offset))
		})

	run, chrome := found.headlessRun(test, shopAddress)
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	chrome.evaluated = map[string]any{"title": "t", "url": "https://shop.example.test/", "text": text, "truncated": false}
	check("headless", func() (*tools.Result, error) { return browse(run, `{"action":"snapshot","max_characters":25}`) },
		func(offset int) (*tools.Result, error) {
			return browse(run, fmt.Sprintf(`{"action":"snapshot","max_characters":25,"offset":%d}`, offset))
		})
}

// The tab's address and title, which can carry a form's value, are scrubbed in
// the overlay and in the tabs result, in the headless browser too.
func TestTheOverlayAndTheTabsResultHideWhatWasFilled(test *testing.T) {
	found := newWorld(test)
	address := "https://shop.example.test/sign-in?p=" + url.QueryEscape(inventedPassword)
	run := found.filledTab(test)
	run.tab.address = address
	run.tab.answer = fmt.Sprintf(`{"tabs":[{"tab":7,"title":%q,"url":%q,"own":true,"current":true}]}`, "Pay "+inventedPassword, address)
	result, err := browse(run, `{"action":"tabs"}`)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(result.Content, "Tr0ub4") || strings.Contains(result.Content, "Tr0ub4dor%263") || !strings.Contains(result.Content, "[hidden]") {
		test.Fatalf("the tabs result: %s", result.Content)
	}
	overlay := browserOverlay(tools.WithRun(context.Background(), run))
	if !strings.Contains(overlay, "<tab>") || strings.Contains(overlay, "Tr0ub4") || !strings.Contains(overlay, "[hidden]") {
		test.Fatalf("the overlay: %s", overlay)
	}
	// The page is not offered as what the person means: it is often one a
	// schedule left there.
	if strings.Contains(overlay, "has attached their own browser tab") || !strings.Contains(overlay, "not a sign of what they are asking about") {
		test.Fatalf("the overlay presents the page as the person's: %s", overlay)
	}
	// The headless browser's own tabs answer.
	headless, _ := found.headlessRun(test, shopAddress)
	headless.tab = run.tab
	run.tab.tabID = "7"
	tabs, err := browse(headless, `{"action":"tabs","target":"headless"}`)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(tabs.Content, "Tr0ub4") || !strings.Contains(tabs.Content, "[hidden]") {
		test.Fatalf("the headless tabs result: %s", tabs.Content)
	}
}

// A run that content started signs in to nothing, in the person's tab or the
// headless browser, also as a step, and the extension is not asked.
func TestARunThatContentStartedFillsNothing(test *testing.T) {
	found := newWorld(test)
	tabRun := found.tabRun(shopAddress)
	tabRun.isStartedByOthers = true
	headlessRun, chrome := found.headlessRun(test, shopAddress)
	headlessRun.isStartedByOthers, headlessRun.isHeadless, headlessRun.surface = true, true, "research"
	for _, run := range []*signInRun{tabRun, headlessRun} {
		for _, arguments := range []string{
			fillLogin(found.grantedLogin),
			fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin),
			fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q}`, found.grantedLogin),
		} {
			mustRefuse(test, run, arguments, "content started")
		}
		stepped, err := browse(run, fmt.Sprintf(`{"action":"steps","steps":[%s]}`, fillLogin(found.grantedLogin)))
		if err != nil || !strings.Contains(stepped.Content, "content started") {
			test.Fatalf("a step: %v %v", stepped, err)
		}
	}
	if asked := tabRun.tab.actions(); len(asked) != 0 {
		test.Fatalf("the tab was asked %v", asked)
	}
	if filled := chrome.fills(test); len(filled) != 0 {
		test.Fatalf("the page was filled: %v", filled)
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 0 {
		test.Fatalf("a use was recorded: %v", hosts)
	}
	// The person's own run, headless or not, signs in.
	headlessRun.isStartedByOthers, headlessRun.surface = false, "schedule"
	if _, err := browse(headlessRun, fillLogin(found.grantedLogin)); err != nil {
		test.Fatalf("a schedule the person wrote: %s", err)
	}
}

// The origin is matched, the port too: a site with no port is not the same site
// on another port, and a site with a port is only that port.
func TestSignInMatchesThePortOfTheItemsSite(test *testing.T) {
	found := newWorld(test)
	for name, testCase := range map[string]struct {
		itemId  string
		address string
		wanted  string
	}{
		"a site without a port, on the default port":     {found.grantedLogin, "https://shop.example.test/sign-in", ""},
		"a site without a port, on another port":         {found.grantedLogin, "https://shop.example.test:8443/sign-in", "not one of that login's sites"},
		"a site with a port, on that port":               {found.portedLogin, "https://shop.example.test:8443/sign-in", ""},
		"a site with a port, on the default port":        {found.portedLogin, "https://shop.example.test/sign-in", "not one of that login's sites"},
		"a site with a port, on another port":            {found.portedLogin, "https://shop.example.test:9443/sign-in", "not one of that login's sites"},
		"a loopback page on http is not a site on https": {found.grantedLogin, "http://localhost:8080/", "not one of that login's sites"},
	} {
		tabRun := found.tabRun(testCase.address)
		headlessRun, chrome := found.headlessRun(test, testCase.address)
		for _, run := range []*signInRun{tabRun, headlessRun} {
			_, err := browse(run, fillLogin(testCase.itemId))
			if testCase.wanted == "" && err != nil {
				test.Errorf("%s: %s", name, err)
			}
			if testCase.wanted != "" && (err == nil || !strings.Contains(err.Error(), testCase.wanted)) {
				test.Errorf("%s: %v", name, err)
			}
		}
		if testCase.wanted != "" {
			if asked := tabRun.tab.actions(); len(asked) != 0 {
				test.Errorf("%s: the tab was asked %v", name, asked)
			}
			if filled := chrome.fills(test); len(filled) != 0 {
				test.Errorf("%s: the page was filled %v", name, filled)
			}
		}
	}
	// The extension is told the port, and the page script checks it.
	tabRun := found.tabRun("https://shop.example.test:8443/sign-in")
	if _, err := browse(tabRun, fillLogin(found.portedLogin)); err != nil {
		test.Fatal(err)
	}
	if asked := tabRun.tab.actions(); len(asked) != 1 || !strings.Contains(asked[0].arguments, `"host":"shop.example.test"`) || !strings.Contains(asked[0].arguments, `"port":8443`) {
		test.Fatalf("the extension was asked %v", asked)
	}
	headlessRun, chrome := found.headlessRun(test, "https://shop.example.test:8443/sign-in")
	if _, err := browse(headlessRun, fillLogin(found.portedLogin)); err != nil {
		test.Fatal(err)
	}
	for _, fill := range chrome.fills(test) {
		if fill.Port != 8443 {
			test.Fatalf("the page script was told port %d", fill.Port)
		}
	}
}

// The three sign-in verbs are offered only to a person who may use Passwords.
func TestTheSignInVerbsNeedThePasswordsPermission(test *testing.T) {
	browserTool := tools.Build().Get("browser")
	without := browserTool.WithHeldActions(models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}}))
	with := browserTool.WithHeldActions(models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionPasswordsUse}}))
	for _, action := range []string{"fill_login", "fill_one_time_code", "passkey_sign_in"} {
		if containsString(tools.ActionsOf(without), action) {
			test.Errorf("%s is offered to a person without passwords:use", action)
		}
		if !containsString(tools.ActionsOf(with), action) {
			test.Errorf("%s is not offered to a person with passwords:use", action)
		}
	}
	for _, action := range []string{"navigate", "snapshot", "click", "evaluate"} {
		if !containsString(tools.ActionsOf(without), action) {
			test.Errorf("%s was taken away", action)
		}
	}
}

func containsString(list []string, wanted string) bool {
	for _, candidate := range list {
		if candidate == wanted {
			return true
		}
	}
	return false
}

// Copying a filled field's text and pasting it into an ordinary one would put
// the secret where the exact-value scrubbing does not see it, so on a locked
// tab a key event that carries editing commands or a control, alt or meta
// modifier, a key that copies or pastes by itself, a pointer that moves with a
// button held (drag and drop), the middle button and every drag command are
// refused, and plain typing and clicking are not.
func TestALockedTabRefusesCopyPasteAndDragCommands(test *testing.T) {
	found := newWorld(test)
	run := found.filledTab(test)
	asked := len(run.tab.actions())
	for _, arguments := range []string{
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","commands":["selectAll"]}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"rawKeyDown","commands":["copy"]}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"rawKeyDown","commands":[]}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"rawKeyDown","commands":["paste"]}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"c","modifiers":2}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"v","modifiers":4}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","modifiers":1}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","modifiers":10}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","modifiers":"ctrl"}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"Insert","modifiers":8}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","windowsVirtualKeyCode":45}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"Delete","modifiers":8}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"Paste"}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mouseMoved","x":10,"y":10,"button":"left"}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mouseMoved","x":10,"y":10,"buttons":1}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mousePressed","x":10,"y":10,"button":"middle"}}`,
		`{"action":"press","key":"Control+a"}`,
		`{"action":"press","key":"Ctrl+c"}`,
		`{"action":"press","key":"Meta+V"}`,
		`{"action":"press","key":"ControlOrMeta+v"}`,
		`{"action":"press","key":"Shift+Insert"}`,
		`{"action":"press","key":"Alt+Tab"}`,
		`{"action":"steps","steps":[{"action":"press","key":"Control+a"}]}`,
	} {
		if strings.Contains(arguments, `"steps"`) {
			stepped, err := browse(run, arguments)
			if err != nil || !strings.Contains(stepped.Content, clipboardLockedWords) {
				test.Fatalf("%s: %v %v", arguments, stepped, err)
			}
			continue
		}
		mustRefuse(test, run, arguments, clipboardLockedWords)
	}
	mustRefuse(test, run, `{"action":"cdp","method":"Input.dispatchDragEvent","params":{"type":"drop","x":1,"y":1,"data":{"items":[]}}}`, lockedWords)
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked while locked: %v", run.tab.actions()[asked:])
	}
	// Plain typing, a click with the pointer moving without a button, a
	// shifted letter and an ordinary key still go to the page.
	for _, arguments := range []string{
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","text":"a"}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"A","text":"A","modifiers":8}}`,
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"Enter","modifiers":0}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mouseMoved","x":10,"y":10,"button":"none","buttons":0}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mousePressed","x":10,"y":10,"button":"left","clickCount":1}}`,
		`{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mouseReleased","x":10,"y":10,"button":"left","clickCount":1}}`,
		`{"action":"press","key":"Enter"}`,
		`{"action":"press","key":"a"}`,
		`{"action":"press","key":"+"}`,
	} {
		if _, err := browse(run, arguments); err != nil {
			test.Errorf("%s on a locked tab: %s", arguments, err)
		}
	}
	// Unlocked, the shortcuts go through.
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 4})
	for _, arguments := range []string{
		`{"action":"cdp","method":"Input.dispatchKeyEvent","params":{"type":"keyDown","key":"a","commands":["selectAll"]}}`,
		`{"action":"press","key":"Control+a"}`,
	} {
		if _, err := browse(run, arguments); err != nil {
			test.Errorf("%s on an unlocked tab: %s", arguments, err)
		}
	}
}

// A CSS selector is a yes or no question to the page (an attribute selector
// can match the start of a field's value that the page mirrors into its
// markup), so a locked page, in either browser and also inside steps, is acted
// on by ref alone, and waiting for a selector is refused.
func TestALockedPageIsActedOnByRefAndNeverBySelector(test *testing.T) {
	found := newWorld(test)
	tabRun := found.filledTab(test)
	headlessRun, chrome := found.headlessRun(test, shopAddress)
	chrome.evaluated = map[string]any{"ok": true, "x": 5, "y": 5, "name": "button"}
	if _, err := browse(headlessRun, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	asked := len(tabRun.tab.actions())
	evaluations := len(chrome.sent("Runtime.evaluate"))
	for name, run := range map[string]*signInRun{"tab": tabRun, "headless": headlessRun} {
		target := ""
		if name == "tab" {
			target = `"target":"tab",`
		}
		for _, arguments := range []string{
			`{"action":"wait","for":"selector","selector":"input[type=password][value^=\"a\"]","timeout_ms":100}`,
			`{"action":"wait","for":"selector","timeout_ms":100}`,
			`{"action":"wait","selector":"#x","timeout_ms":100}`,
			`{"action":"click","selector":"input[type=password][value^=\"a\"]"}`,
			`{"action":"hover","selector":"#x"}`,
			`{"action":"scroll","selector":"#x"}`,
			`{"action":"type","selector":"#x","text":"a"}`,
			`{"action":"select","selector":"#x","value":"a"}`,
		} {
			arguments = strings.Replace(arguments, "{", "{"+target, 1)
			mustRefuse(test, run, arguments, selectorLockedWords)
			stepped, err := browse(run, `{`+target+`"action":"steps","steps":[`+strings.Replace(arguments, `{`+target, `{`, 1)+`]}`)
			if err != nil || !strings.Contains(stepped.Content, selectorLockedWords) {
				test.Fatalf("a selector in steps (%s): %v %v", name, stepped, err)
			}
		}
	}
	if len(tabRun.tab.actions()) != asked {
		test.Fatalf("the tab was asked while locked: %v", tabRun.tab.actions()[asked:])
	}
	// The headless page ran nothing for them beyond its own lock check.
	for _, parameters := range chrome.sent("Runtime.evaluate")[evaluations:] {
		if strings.Contains(parameters, "querySelector") {
			test.Fatalf("a selector reached the page: %s", parameters)
		}
	}
	// By ref still works, and so does a selector on a page nothing was filled into.
	if _, err := browse(headlessRun, `{"action":"hover","ref":2}`); err != nil {
		test.Fatalf("a ref on a locked page: %s", err)
	}
	other := found.tabRun(shopAddress)
	other.tab.answer = `{"result":"fine"}`
	if _, err := browse(other, `{"target":"tab","action":"click","selector":"#x"}`); err != nil {
		test.Fatalf("a selector on a page nothing was filled into: %s", err)
	}
}

// Going back can bring the filled document back from the history's cache
// before the event that tells the lock, so the headless page's browser is
// asked which document it shows right before a command that could read it.
func TestTheHeadlessPageIsLockedAgainWhenTheFilledDocumentIsBackBeforeAnyEvent(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, shopAddress)
	chrome.evaluated = map[string]any{"ok": true, "x": 5, "y": 5, "name": "button"}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	mustRefuse(test, run, `{"action":"evaluate","expression":"1"}`, lockedWords)
	// The page moves on to a new document: unlocked.
	chrome.mutex.Lock()
	chrome.loader = "loader-2"
	chrome.mutex.Unlock()
	run.page.Filled().Committed("", devtools.Commit{DocumentID: "loader-2", Address: "https://shop.example.test/account", Count: 50})
	if _, err := browse(run, `{"action":"evaluate","expression":"1"}`); err != nil {
		test.Fatalf("a script on the new document: %s", err)
	}
	// The browser goes back to the filled document, with no event yet.
	chrome.mutex.Lock()
	chrome.loader = "loader-1"
	chrome.mutex.Unlock()
	for _, arguments := range []string{
		`{"action":"evaluate","expression":"1"}`,
		`{"action":"screenshot"}`,
		`{"action":"storage"}`,
		`{"action":"cdp_events"}`,
		`{"action":"cdp","method":"Runtime.evaluate"}`,
		`{"action":"steps","steps":[{"action":"wait","for":"timeout","timeout_ms":1},{"action":"evaluate","expression":"1"}]}`,
	} {
		if strings.Contains(arguments, "steps") {
			stepped, err := browse(run, arguments)
			if err != nil || !strings.Contains(stepped.Content, lockedWords) {
				test.Fatalf("%s: %v %v", arguments, stepped, err)
			}
			continue
		}
		mustRefuse(test, run, arguments, lockedWords)
	}
	// When the browser does not say which document it shows, a command that
	// could read the page is not run, and one that cannot is.
	chrome.mutex.Lock()
	chrome.failFrameTree = true
	chrome.mutex.Unlock()
	run.page.Filled().Committed("", devtools.Commit{DocumentID: "loader-3", Address: "https://shop.example.test/other", Count: 51})
	mustRefuse(test, run, `{"action":"evaluate","expression":"1"}`, documentUnknownWords)
	if _, err := browse(run, `{"action":"hover","ref":2}`); err != nil {
		test.Fatalf("a hover when the document is not known: %s", err)
	}
}
