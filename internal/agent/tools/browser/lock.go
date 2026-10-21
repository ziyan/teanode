package browser

import (
	"encoding/json"
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

// refuseWhileLocked is the refusal for an action on a tab, or nil. The tab is
// named as the browser names it (empty for the headless browser's one page).
func refuseWhileLocked(filled *devtools.FilledValues, tab string, arguments *browserArguments) error {
	if !filled.IsLocked(tab) {
		return nil
	}
	if lockedRefusedActions[arguments.Action] {
		return refusal(scriptsLockedWords)
	}
	if arguments.Action == "cdp" && !isProtocolMethodAllowedWhileLocked(arguments.Method) {
		return refusal(scriptsLockedWords)
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
