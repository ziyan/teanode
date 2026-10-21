package browser

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	devtools "github.com/ziyan/teanode/internal/browser"
)

// The lock on a page the agent filled a password or a one-time code into (see
// devtools.FilledValues). While a tab is locked nothing that could read a
// field's value back is done on it: not a script, not a picture, not what the
// page asked of the network (a form's request carries the password in its
// body), and not a DevTools command that reads the page or the network. The
// lock is the tab's and the document's: it ends when that tab commits a new
// document, whatever the model did with other tabs.

// scriptsLockedWords is the answer to what the lock refuses.
const scriptsLockedWords = "Scripts, pictures, storage, network logs and DevTools commands that read a page are not run on a page TeaNode filled a password or code into, until that tab loads a new page. Click and type the rest of the sign-in; once the site has taken you to another page, or you navigate to one, it works again."

// addressLockedWords is the answer to a move of a locked page that is not a
// plain one: an address that is not http or https, or a navigation or reload
// that carries anything else.
const addressLockedWords = "A page TeaNode filled a password or code into can only be sent to an http or https address (and reloaded as it is) until that tab loads a new page. Script and data addresses, and anything else sent along with a navigation or reload, are not done."

// lockedRefusedActions are the actions refused on a locked tab: each can read
// a filled field or what was sent from it.
var lockedRefusedActions = map[string]bool{"evaluate": true, "screenshot": true, "storage": true, "cdp_events": true}

// lockedProtocolMethods are the DevTools methods still answered on a locked
// tab: real mouse, keyboard and touch input, and moving the page. Everything
// else (Network., Fetch., Runtime., DOM., DOMSnapshot., Page.captureSnapshot,
// Accessibility., CSS., the drag methods of Input., which hand back the data
// being dragged, and whatever the protocol adds) is refused, since a list of
// what is refused loses to the protocol growing.
var lockedProtocolMethods = []string{
	"Input.dispatchMouseEvent",
	"Input.dispatchKeyEvent",
	"Input.dispatchTouchEvent",
	"Input.insertText",
	"Input.imeSetComposition",
	"Page.navigate",
	"Page.reload",
	"Page.getNavigationHistory",
	"Page.getFrameTree",
	"Page.getLayoutMetrics",
	"Page.enable",
	"Page.disable",
}

// isProtocolMethodAllowedWhileLocked says a DevTools method may be sent to a
// tab that is locked.
func isProtocolMethodAllowedWhileLocked(method string) bool {
	method = strings.TrimSpace(method)
	for _, allowed := range lockedProtocolMethods {
		if method == allowed {
			return true
		}
	}
	return false
}

// isLockedNavigationAllowed says the parameters of a Page.navigate or a
// Page.reload may be sent to a locked tab. Moving the page is allowed so the
// sign-in can go on, but both commands can also run script in the page they
// leave (a javascript: address, a script to evaluate on load) or load a
// document that reads the field back, so only the plain form is: navigate with
// a url that is an http or https address and nothing else, reload with at most
// ignoreCache.
func isLockedNavigationAllowed(method string, parameters map[string]any) bool {
	switch strings.TrimSpace(method) {
	case "Page.navigate":
		if len(parameters) != 1 {
			return false
		}
		address, isText := parameters["url"].(string)
		return isText && isWebAddress(address)
	case "Page.reload":
		for name, value := range parameters {
			if _, isBoolean := value.(bool); name != "ignoreCache" || !isBoolean {
				return false
			}
		}
	}
	return true
}

// isWebAddress says an address is an http or https one with a host, written
// without anything before it that a browser would strip (a space, a newline)
// to find a different scheme.
func isWebAddress(address string) bool {
	if address != strings.TrimSpace(address) {
		return false
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// hasScriptAddress says a DevTools parameter, anywhere in it, holds an address
// that runs script or carries a document in itself (javascript:, data:,
// vbscript:), written in any case and with the spaces and control characters a
// browser ignores before and inside the scheme.
func hasScriptAddress(value any) bool {
	switch typed := value.(type) {
	case string:
		squeezed := strings.Map(func(character rune) rune {
			if character <= ' ' || character == 0x7f {
				return -1
			}
			return character
		}, strings.ToLower(typed))
		for _, scheme := range []string{"javascript:", "data:", "vbscript:"} {
			if strings.HasPrefix(squeezed, scheme) {
				return true
			}
		}
	case map[string]any:
		for _, inner := range typed {
			if hasScriptAddress(inner) {
				return true
			}
		}
	case []any:
		for _, inner := range typed {
			if hasScriptAddress(inner) {
				return true
			}
		}
	}
	return false
}

// refuseWhileLocked is the refusal for an action on a tab, or nil. The tab is
// named as the browser names it (empty for the headless browser's one page).
func refuseWhileLocked(filled *devtools.FilledValues, tab string, arguments *browserArguments) error {
	if !filled.IsLocked(tab) {
		return nil
	}
	if lockedRefusedActions[arguments.Action] {
		return refusal(scriptsLockedWords)
	}
	switch arguments.Action {
	case "cdp":
		if !isProtocolMethodAllowedWhileLocked(arguments.Method) {
			return refusal(scriptsLockedWords)
		}
		if !isLockedNavigationAllowed(arguments.Method, arguments.Params) {
			return refusal(addressLockedWords)
		}
		// What is typed with the keyboard is the page's to receive; any other
		// parameter holding a javascript: or data: address is a way to run
		// script in the page.
		if !strings.HasPrefix(strings.TrimSpace(arguments.Method), "Input.") && hasScriptAddress(arguments.Params) {
			return refusal(addressLockedWords)
		}
	case "navigate":
		if !isWebAddress(arguments.URL) {
			return refusal(addressLockedWords)
		}
	}
	return nil
}

// tabGuardedActions are the actions sent to the person's browser with the tab
// the server checked the lock of; the extension refuses them when the actions
// go to another tab by then.
var tabGuardedActions = map[string]bool{"evaluate": true, "screenshot": true, "storage": true, "cdp": true, "cdp_events": true}

// noteCurrentTab records the tab the actions go to when an answer of the
// extension says it: opening, switching and closing answer with the tab, and
// the list of tabs says which is current.
func noteCurrentTab(tab tools.Tab, action string, data json.RawMessage) {
	switch action {
	case "open", "switch", "close":
		var answer struct {
			Tab *int64 `json:"tab"`
		}
		if json.Unmarshal(data, &answer) == nil {
			if answer.Tab != nil {
				tab.NoteCurrentTab(strconv.FormatInt(*answer.Tab, 10))
			} else {
				tab.NoteCurrentTab("")
			}
		}
	case "tabs":
		var answer struct {
			Tabs []struct {
				Tab     int64 `json:"tab"`
				Current bool  `json:"current"`
			} `json:"tabs"`
		}
		if json.Unmarshal(data, &answer) == nil {
			for _, listed := range answer.Tabs {
				if listed.Current {
					tab.NoteCurrentTab(strconv.FormatInt(listed.Tab, 10))
				}
			}
		}
	}
}

// scrubbedAnswer hides what was filled from the words of a snapshot the
// person's browser answered, whole, so that the parts it is cut into (and
// the page's own cuts) never hold a value in two halves.
func scrubbedAnswer(filled *devtools.FilledValues, data json.RawMessage) json.RawMessage {
	var answer map[string]any
	if json.Unmarshal(data, &answer) != nil {
		return json.RawMessage(filled.Scrub(string(data)))
	}
	for _, key := range []string{"text", "title", "url"} {
		if text, isText := answer[key].(string); isText {
			answer[key] = filled.Scrub(text)
		}
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return data
	}
	return encoded
}
