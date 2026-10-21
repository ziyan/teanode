package browser

import (
	"strings"

	devtools "github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/passwords"
)

// Two rules that keep a sign-in from being undone by the agent's own commands.
//
// A page the agent ran script in is not filled. A listener on the page that
// sends a field's value out when it changes, or a form whose address was
// changed to another site, is invisible to the lock (which looks only after
// the fill) and to the check of the form (which reads the page as it is, and
// a listener is not part of that). So the document a script ran in, or was
// changed by a DevTools command in, is marked, and a fill, a one-time code or
// a passkey sign-in on it is refused. The mark ends when the tab commits a new
// document (devtools.FilledValues, the lock in the other direction). Something
// that runs in every page the tab loads from then on is never undone by a
// navigation, so that tab is never filled again.
//
// A tab the agent filled a password or a one-time code into holds a session
// afterwards. Its cookies and storage are not read back through the storage
// action or the DevTools commands that return them, for as long as the tab
// lives. What a script in the page reads (document.cookie, localStorage) stays
// possible once the lock has ended: a cookie the site marked HttpOnly is not
// in document.cookie.

// scriptedFillWords is the answer to a fill on a page the agent ran script in.
const scriptedFillWords = devtools.ScriptedPageWords

// scriptOnEveryPageFillWords is the answer to a fill in a tab that was given a
// script to run in every page.
const scriptOnEveryPageFillWords = devtools.ScriptOnEveryPageWords

// sessionReadWords is the answer to reading the cookies or the storage of a tab
// the agent signed in to.
const sessionReadWords = "This tab was signed in with a password or code TeaNode filled, so its cookies, storage and network log are not read: they hold the session. Read the pages themselves instead."

// readOnlyProtocolMethods are the DevTools methods that neither run script in
// the page nor change it: moving the page, looking at it, real input. The list
// is of what is harmless, not of what is not, because a list of what is
// harmful loses to the protocol growing. Everything else counts as the agent
// scripting the page (Runtime.*, DOM.set*, DOM.remove*, DOM.insert*,
// Page.setDocumentContent, Debugger.*, Fetch.*, and so on).
var readOnlyProtocolMethods = map[string]bool{
	"Page.navigate":                        true,
	"Page.reload":                          true,
	"Page.navigateToHistoryEntry":          true,
	"Page.getNavigationHistory":            true,
	"Page.getFrameTree":                    true,
	"Page.getLayoutMetrics":                true,
	"Page.getResourceTree":                 true,
	"Page.getResourceContent":              true,
	"Page.getAppManifest":                  true,
	"Page.captureScreenshot":               true,
	"Page.captureSnapshot":                 true,
	"Page.printToPDF":                      true,
	"Page.bringToFront":                    true,
	"Page.stopLoading":                     true,
	"Page.enable":                          true,
	"Page.disable":                         true,
	"Network.enable":                       true,
	"Network.disable":                      true,
	"Network.getResponseBody":              true,
	"Network.getRequestPostData":           true,
	"DOM.enable":                           true,
	"DOM.disable":                          true,
	"DOM.getDocument":                      true,
	"DOM.getFlattenedDocument":             true,
	"DOM.querySelector":                    true,
	"DOM.querySelectorAll":                 true,
	"DOM.getOuterHTML":                     true,
	"DOM.describeNode":                     true,
	"DOM.getAttributes":                    true,
	"DOM.getBoxModel":                      true,
	"DOM.getContentQuads":                  true,
	"DOM.getNodeForLocation":               true,
	"DOM.getFrameOwner":                    true,
	"DOM.requestChildNodes":                true,
	"DOM.performSearch":                    true,
	"DOM.getSearchResults":                 true,
	"DOM.discardSearchResults":             true,
	"DOM.pushNodesByBackendIdsToFrontend":  true,
	"DOM.focus":                            true,
	"DOM.scrollIntoViewIfNeeded":           true,
	"CSS.enable":                           true,
	"CSS.disable":                          true,
	"CSS.getComputedStyleForNode":          true,
	"CSS.getMatchedStylesForNode":          true,
	"CSS.getInlineStylesForNode":           true,
	"Log.enable":                           true,
	"Log.disable":                          true,
	"Performance.enable":                   true,
	"Performance.disable":                  true,
	"Performance.getMetrics":               true,
	"Browser.getVersion":                   true,
	"Target.getTargets":                    true,
	"Emulation.setDeviceMetricsOverride":   true,
	"Emulation.clearDeviceMetricsOverride": true,
	"Emulation.setEmulatedMedia":           true,
}

// readOnlyProtocolPrefixes are whole domains that only look or give input.
var readOnlyProtocolPrefixes = []string{"Input.", "DOMSnapshot.", "Accessibility."}

// isProtocolMethodReadOnly says a DevTools method does not run script in the
// page or change it.
func isProtocolMethodReadOnly(method string) bool {
	method = strings.TrimSpace(method)
	if readOnlyProtocolMethods[method] {
		return true
	}
	for _, prefix := range readOnlyProtocolPrefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}

// scriptOnEveryPagePrefixes and scriptOnEveryPageMethods are the DevTools
// methods that put something in every page the tab loads from then on, or
// that sit between the page and the network and can change what it is given.
var scriptOnEveryPagePrefixes = []string{"Debugger.", "Fetch."}

var scriptOnEveryPageMethods = map[string]bool{
	"Page.addScriptToEvaluateOnNewDocument": true,
	"Page.addScriptToEvaluateOnLoad":        true,
	"Runtime.addBinding":                    true,
	"Network.setRequestInterception":        true,
}

// isScriptOnEveryPageMethod says a DevTools method makes something that runs
// in every page the tab loads from now on.
func isScriptOnEveryPageMethod(method string) bool {
	method = strings.TrimSpace(method)
	if scriptOnEveryPageMethods[method] {
		return true
	}
	for _, prefix := range scriptOnEveryPagePrefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}

// noteScriptRun records that an action of the person's tab is about to run
// script in the page, or change it, so that the document is not filled
// afterwards. The caller has already let the action through the lock.
func noteScriptRun(filled *devtools.FilledValues, tabKey, address string, arguments *browserArguments) {
	switch arguments.Action {
	case "evaluate":
		filled.NoteScripted(tabKey, devtools.Document{Address: address})
	case "navigate", "open":
		// A script address or a document carried in its address runs as, or
		// in place of, the page.
		if !isWebAddress(arguments.URL) && strings.TrimSpace(arguments.URL) != "" {
			filled.NoteScripted(tabKey, devtools.Document{Address: address})
		}
	case "cdp":
		switch {
		case isScriptOnEveryPageMethod(arguments.Method):
			filled.NoteScriptOnEveryPage(tabKey)
		case isSessionProtocolMethod(arguments.Method):
			// Cookies and storage are read and written beside the page, not
			// by script in it.
		case !isProtocolMethodReadOnly(arguments.Method):
			filled.NoteScripted(tabKey, devtools.Document{Address: address})
		case !strings.HasPrefix(strings.TrimSpace(arguments.Method), "Input.") && hasScriptAddress(arguments.Params):
			filled.NoteScripted(tabKey, devtools.Document{Address: address})
		}
	}
}

// refuseScriptedTab is the refusal of a sign-in on the person's tab whose page
// the agent ran script in, or nil.
func refuseScriptedTab(filled *devtools.FilledValues, tabKey string) error {
	if filled.HasScriptOnEveryPage(tabKey) {
		return refusal(scriptOnEveryPageFillWords)
	}
	if filled.IsScripted(tabKey, "") {
		return refusal(scriptedFillWords)
	}
	return nil
}

// sessionProtocolMethods and sessionProtocolPrefixes are the DevTools methods
// that return cookies, storage or what the page sent with them.
var sessionProtocolMethods = map[string]bool{
	"Network.getCookies":    true,
	"Network.getAllCookies": true,
	"Network.enable":        true,
	"Storage.getCookies":    true,
	"Page.getCookies":       true,
}

var sessionProtocolPrefixes = []string{"Storage.", "DOMStorage.", "IndexedDB.", "CacheStorage.", "Fetch."}

// isSessionProtocolMethod says a DevTools method returns cookies, storage, or
// the requests the page makes with them.
func isSessionProtocolMethod(method string) bool {
	method = strings.TrimSpace(method)
	if sessionProtocolMethods[method] {
		return true
	}
	for _, prefix := range sessionProtocolPrefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}

// refuseSessionRead is the refusal of reading the cookies or storage of a tab
// the agent filled a password or code into, or nil. The storage action reads
// the page's own site; the DevTools methods can name other sites (all the
// cookies of the browser, for one), so they are refused in the whole tab.
func refuseSessionRead(filled *devtools.FilledValues, tabKey, address string, arguments *browserArguments) error {
	switch arguments.Action {
	case "storage":
		host := passwords.NormalizedHost(address)
		if filled.HasSecretHost(tabKey, host) {
			return refusal(sessionReadWords)
		}
	case "cdp":
		if isSessionProtocolMethod(arguments.Method) && filled.HasSecretHost(tabKey, "") {
			return refusal(sessionReadWords)
		}
	}
	return nil
}
