package browser

import (
	"fmt"
	"strings"
	"testing"

	devtools "github.com/ziyan/teanode/internal/browser"
)

// A page the agent ran script in is not filled by any of the three sign-ins, in
// the person's tab or in the headless browser, and the extension is not asked.
func TestAScriptedPageIsNotFilled(test *testing.T) {
	found := newWorld(test)
	codeArguments := fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin)
	newCommit := func(run *signInRun, number int) {
		for _, tab := range []string{"", "7"} {
			run.tab.Filled().Committed(tab, devtools.Commit{DocumentID: fmt.Sprintf("doc-%d", number), Address: shopAddress, Count: int64(number)})
		}
	}
	// Each of these runs script in, or changes, the document the tab shows.
	for name, arguments := range map[string]string{
		"evaluate":                             `{"action":"evaluate","expression":"1+1"}`,
		"a script in steps":                    `{"action":"steps","steps":[{"action":"evaluate","expression":"1"}]}`,
		"Runtime.evaluate":                     `{"action":"cdp","method":"Runtime.evaluate","params":{"expression":"1"}}`,
		"Runtime.callFunctionOn":               `{"action":"cdp","method":"Runtime.callFunctionOn"}`,
		"DOM.setAttributeValue":                `{"action":"cdp","method":"DOM.setAttributeValue","params":{"nodeId":1,"name":"action","value":"https://evil.test/"}}`,
		"DOM.setOuterHTML":                     `{"action":"cdp","method":"DOM.setOuterHTML"}`,
		"DOM.removeNode":                       `{"action":"cdp","method":"DOM.removeNode"}`,
		"DOM.insertChildNodes":                 `{"action":"cdp","method":"DOM.setChildNodes"}`,
		"Page.setDocumentContent":              `{"action":"cdp","method":"Page.setDocumentContent","params":{"html":"<form></form>"}}`,
		"Page.navigate to javascript":          `{"action":"cdp","method":"Page.navigate","params":{"url":"javascript:void(0)"}}`,
		"Page.navigate to data":                `{"action":"cdp","method":"Page.navigate","params":{"url":"data:text/html,x"}}`,
		"a method that is not known":           `{"action":"cdp","method":"Some.futureMethod"}`,
		"Emulation.setScriptExecutionDisabled": `{"action":"cdp","method":"Emulation.setScriptExecutionDisabled","params":{"value":true}}`,
		"navigate to a script":                 `{"action":"navigate","url":"javascript:void(0)"}`,
	} {
		run := found.tabRun(shopAddress)
		run.tab.tabID = "7"
		run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-9","commitCount":9}`
		run.tab.answer = `{"result":"fine"}`
		newCommit(run, 1)
		// Fine before: a fill on a document nothing ran in. A method the
		// attached tab's list does not answer never reaches the page, so it
		// scripts nothing and there is nothing more to check for it.
		if _, err := browse(run, arguments); err != nil {
			if isRefusedByReach(err) {
				continue
			}
			if !strings.Contains(err.Error(), "http or https address") {
				test.Fatalf("%s: %s", name, err)
			}
		}
		before := len(run.tab.actions())
		mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedPageWords)
		mustRefuse(test, run, codeArguments, devtools.ScriptedPageWords)
		if len(run.tab.actions()) != before {
			test.Fatalf("%s: the extension was asked to fill a scripted page: %v", name, run.tab.actions()[before:])
		}
		// A new document the tab committed is filled.
		newCommit(run, 2)
		if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
			test.Fatalf("%s: a new document was not filled: %s", name, err)
		}
	}

	// What only looks, and real input, runs no script.
	for name, arguments := range map[string]string{
		"snapshot":              `{"action":"snapshot"}`,
		"click":                 `{"action":"click","ref":3}`,
		"type":                  `{"action":"type","ref":3,"text":"ada"}`,
		"navigate":              `{"action":"navigate","url":"https://shop.example.test/cart"}`,
		"Input.dispatchMouse":   `{"action":"cdp","method":"Input.dispatchMouseEvent","params":{"type":"mousePressed","x":1,"y":1}}`,
		"typed script address":  `{"action":"cdp","method":"Input.insertText","params":{"text":"javascript:note to self"}}`,
		"Page.navigate":         `{"action":"cdp","method":"Page.navigate","params":{"url":"https://shop.example.test/cart"}}`,
		"DOM.getDocument":       `{"action":"cdp","method":"DOM.getDocument"}`,
		"Accessibility.getFull": `{"action":"cdp","method":"Accessibility.getFullAXTree"}`,
	} {
		run := found.tabRun(shopAddress)
		run.tab.tabID = "7"
		run.tab.answer = `{"result":"fine"}`
		newCommit(run, 1)
		if _, err := browse(run, arguments); err != nil {
			test.Fatalf("%s: %s", name, err)
		}
		if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
			test.Fatalf("%s ran no script and the page was not filled: %s", name, err)
		}
	}

	// Something that runs in every page the tab loads is not undone by a
	// navigation. The attached tab's list of methods answers none of these, so
	// they never reach the page; asking for one still marks the tab, which is
	// the stricter of the two, and a new tab is filled.
	for _, method := range []string{"Page.addScriptToEvaluateOnNewDocument", "Runtime.addBinding", "Debugger.enable", "Fetch.enable", "Network.setRequestInterception"} {
		run := found.tabRun(shopAddress)
		run.tab.tabID = "7"
		run.tab.answer = `{"result":"fine"}`
		newCommit(run, 1)
		asked := len(run.tab.actions())
		_, err := browse(run, fmt.Sprintf(`{"action":"cdp","method":%q}`, method))
		if err == nil || !isRefusedByReach(err) {
			test.Fatalf("%s reached the tab: %v", method, err)
		}
		if len(run.tab.actions()) != asked {
			test.Fatalf("%s was sent to the tab: %v", method, run.tab.actions()[asked:])
		}
		newCommit(run, 2)
		mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptOnEveryPageWords)
		run.tab.tabID = "8"
		if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
			test.Fatalf("%s: another tab was not filled: %s", method, err)
		}
	}
}

// In the headless browser too: a script run before the fill marks the document,
// a fill is refused on it (and a passkey), and a new document is filled.
func TestAScriptedHeadlessPageIsNotFilled(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, shopAddress)
	codeArguments := fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":5,"target":"headless"}`, found.grantedLogin)
	passkeyArguments := fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.grantedLogin)

	// A snapshot is the tool's own script; the agent's is evaluate.
	if _, err := browse(run, `{"action":"snapshot"}`); err != nil {
		test.Fatal(err)
	}
	if _, err := browse(run, `{"action":"evaluate","expression":"document.title"}`); err != nil {
		test.Fatal(err)
	}
	fills := len(chrome.fills(test))
	uses := len(found.usedOn(test, found.grantedLogin))
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedSiteWords)
	mustRefuse(test, run, codeArguments, devtools.ScriptedSiteWords)
	mustRefuse(test, run, passkeyArguments, devtools.ScriptedSiteWords)
	if len(chrome.fills(test)) != fills || len(chrome.sent("WebAuthn.addVirtualAuthenticator")) != 0 || len(found.usedOn(test, found.grantedLogin)) != uses {
		test.Fatal("a scripted page was filled, given a passkey, or the use was recorded")
	}
	// The sign-in page opened again is a new document of the same site, which
	// the script may have left things on.
	chrome.mutex.Lock()
	chrome.loader = "loader-2"
	chrome.mutex.Unlock()
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedSiteWords)
	// A fresh browser is filled, and a script on it marks its site again.
	if _, err := browse(run, `{"action":"reset"}`); err != nil {
		test.Fatal(err)
	}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatalf("a fresh browser: %s", err)
	}
}

// reset is the headless browser's only; the person's own browser is not thrown
// away.
func TestResetStartsAFreshHeadlessBrowserAndIsRefusedOnTheTab(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, shopAddress)
	before := len(chrome.sent("Target.createBrowserContext"))
	if _, err := browse(run, `{"action":"reset"}`); err != nil {
		test.Fatal(err)
	}
	if len(chrome.sent("Target.createBrowserContext")) != before+1 || len(chrome.sent("Target.disposeBrowserContext")) != 1 {
		test.Fatalf("a reset did not open a new browser context and dispose of the old: %v", chrome.commands)
	}
	tabRun := found.tabRun(shopAddress)
	mustRefuse(test, tabRun, `{"action":"reset","target":"tab"}`, "not reset")
	if len(tabRun.tab.actions()) != 0 {
		test.Fatalf("the person's browser was asked: %v", tabRun.tab.actions())
	}
}

// The server decides what counts as a script, sends that with the action, and
// marks the document the extension says the script ran in, which may be newer
// than the one it knew when it sent the action. The model reads the value alone.
func TestATabScriptIsMarkedInTheDocumentTheExtensionSaysItRanIn(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.tabID = "7"
	run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-9","commitCount":9}`
	for _, tab := range []string{"", "7"} {
		run.tab.Filled().Committed(tab, devtools.Commit{DocumentID: "doc-1", Address: shopAddress, Count: 1})
	}
	// The sign-in page replaced the page the script was sent to, and the script
	// ran in it: the extension says so, and the commit is reported afterwards.
	run.tab.answer = `{"scriptRun":{"documentId":"doc-2"},"value":{"title":"Sign in"}}`
	result, err := browse(run, `{"action":"evaluate","expression":"document.title","isScript":false}`)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(result.Content, "scriptRun") || !strings.Contains(result.Content, `"title":"Sign in"`) {
		test.Fatalf("the model reads more than the value: %s", result.Content)
	}
	asked := run.tab.actions()
	if !strings.Contains(asked[len(asked)-1].arguments, `"isScript":true`) {
		test.Fatalf("the extension was not told the action runs script, whatever the model said: %v", asked[len(asked)-1])
	}
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: shopAddress, Count: 2})
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedPageWords)
	// An action that runs no script is sent as one that does not.
	run.tab.answer = `{"result":"fine"}`
	if _, err := browse(run, `{"action":"click","ref":3,"isScript":true}`); err != nil {
		test.Fatal(err)
	}
	asked = run.tab.actions()
	if !strings.Contains(asked[len(asked)-1].arguments, `"isScript":false`) {
		test.Fatalf("a click was sent as a script: %v", asked[len(asked)-1])
	}
	if _, err := browse(run, `{"action":"cdp","method":"Runtime.evaluate","params":{"expression":"1"}}`); err != nil {
		test.Fatal(err)
	}
	asked = run.tab.actions()
	if !strings.Contains(asked[len(asked)-1].arguments, `"isScript":true`) {
		test.Fatalf("a protocol command that runs script was not sent as one: %v", asked[len(asked)-1])
	}
	// An answer in the old shape is read as it is.
	run.tab.answer = `{"result":"fine"}`
	result, err = browse(run, `{"action":"evaluate","expression":"1"}`)
	if err != nil || !strings.Contains(result.Content, "fine") {
		test.Fatalf("an answer without the document: %v %v", result, err)
	}
}

// After a password or code is filled into a tab, the cookies, the storage and
// the network log of that tab are not read, for the life of the tab; a script
// in the page still is (document.cookie holds no HttpOnly cookie).
func TestTheSessionOfASignedInTabIsNotRead(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.tabID = "7"
	run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-1","commitCount":3}`
	run.tab.answer = `{"result":"fine"}`
	sessionMethods := []string{
		"Network.getCookies", "Network.getAllCookies", "Storage.getCookies", "Storage.getUsageAndQuota", "DOMStorage.getDOMStorageItems", "DOMStorage.enable",
		"IndexedDB.requestDatabaseNames", "IndexedDB.requestData", "CacheStorage.requestCacheNames", "CacheStorage.requestCachedResponse", "Network.enable", "Page.getCookies",
	}
	cdp := func(method string) string { return fmt.Sprintf(`{"action":"cdp","method":%q}`, method) }

	// Before a sign-in each works, unless the attached tab's list of methods
	// does not answer it at all (most of them read the whole browser).
	for _, method := range sessionMethods {
		if _, err := browse(run, cdp(method)); err != nil && !isRefusedByReach(err) {
			test.Fatalf("%s before a sign-in: %s", method, err)
		}
	}
	if _, err := browse(run, `{"action":"storage"}`); err != nil {
		test.Fatalf("storage before a sign-in: %s", err)
	}
	// A user name alone holds no session.
	if _, err := browse(run, fmt.Sprintf(`{"action":"fill_login","item_id":%q,"user_name_ref":3}`, found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	if _, err := browse(run, `{"action":"storage"}`); err != nil {
		test.Fatalf("storage after only a user name: %s", err)
	}

	// A new document the sign-in script did not run in.
	for _, tab := range []string{"", "7"} {
		run.tab.Filled().Committed(tab, devtools.Commit{DocumentID: "doc-0", Address: shopAddress, Count: 1})
	}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	asked := len(run.tab.actions())
	// While the page is locked the lock refuses them.
	for _, method := range sessionMethods {
		mustRefuse(test, run, cdp(method), "")
	}
	mustRefuse(test, run, `{"action":"storage"}`, "")
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked for its session: %v", run.tab.actions()[asked:])
	}

	// The lock ends with a new document; the session stays unread.
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 4})
	run.tab.address = "https://shop.example.test/account"
	for _, method := range sessionMethods {
		mustRefuse(test, run, cdp(method), sessionReadWords)
	}
	mustRefuse(test, run, `{"action":"storage"}`, "cookies, storage and network log are not read")
	// A step reports its refusal in the answer, which stops the steps.
	if stepped, err := browse(run, `{"action":"steps","steps":[{"action":"storage"}]}`); err != nil || !strings.Contains(stepped.Content, sessionReadWords) {
		test.Fatalf("storage in steps: %v %v", stepped, err)
	}
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked for its session: %v", run.tab.actions()[asked:])
	}
	// What a script reads stays possible, and the other ways of looking.
	for _, arguments := range []string{`{"action":"evaluate","expression":"document.cookie"}`, `{"action":"snapshot"}`, cdp("Page.getNavigationHistory"), cdp("DOM.getDocument")} {
		if _, err := browse(run, arguments); err != nil {
			test.Errorf("%s: %s", arguments, err)
		}
	}
	// On another site in the same tab the storage action reads that site, but
	// the DevTools commands, which can name every site, stay closed.
	run.tab.address = "https://news.example.org/"
	if _, err := browse(run, `{"action":"storage"}`); err != nil {
		test.Errorf("storage of a site nothing was filled into: %s", err)
	}
	mustRefuse(test, run, cdp("Network.getAllCookies"), sessionReadWords)
	// The same site as the www form of it.
	run.tab.address = "https://www.shop.example.test/"
	mustRefuse(test, run, `{"action":"storage"}`, sessionReadWords)

	// Another tab was not signed in to.
	other := found.tabRun(shopAddress)
	other.tab.tabID = "8"
	other.tab.answer = `{"result":"fine"}`
	if _, err := browse(other, `{"action":"storage"}`); err != nil {
		test.Errorf("storage of a tab nothing was filled into: %s", err)
	}

	// A one-time code holds a session too.
	codeRun := found.tabRun(shopAddress)
	codeRun.tab.tabID = "7"
	codeRun.tab.answer = `{"result":"fine"}`
	if _, err := browse(codeRun, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	codeRun.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-5", Address: "https://shop.example.test/account", Count: 5})
	mustRefuse(test, codeRun, `{"action":"storage"}`, sessionReadWords)
}

// The headless browser answers no storage or protocol command, so the session
// it signed in to is read only by a script in the page.
func TestTheHeadlessBrowserHasNoStorageOrProtocolAction(test *testing.T) {
	found := newWorld(test)
	run, _ := found.headlessRun(test, shopAddress)
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	for _, action := range []string{"storage", "cdp"} {
		mustRefuse(test, run, fmt.Sprintf(`{"action":%q,"method":"Network.getAllCookies"}`, action), "")
	}
}

// isRefusedByReach says the attached tab's list of DevTools methods turned the
// call away before it reached the page.
func isRefusedByReach(err error) bool {
	return strings.Contains(err.Error(), "is not one of the methods an attached tab answers") || strings.Contains(err.Error(), "only http and https addresses")
}
