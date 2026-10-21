package browser

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

// What the agent may reach through the person's own browser.
//
// The tab carries the person's session, so two things must stay out of
// the agent's reach, whatever a page it has read tells it to do:
//
//   - Files on the person's machine. The DevTools protocol can choose the
//     files of a file input (DOM.setFileInputFiles) or drop files onto a
//     page (Input.dispatchDragEvent), and a page script then reads their
//     contents back: any file the person can read. So the protocol is a list
//     of exact method names, and nothing else is sent.
//   - TeaNode itself, and the extension's own pages. A request that a page
//     script makes from the server's own origin carries the person's session
//     cookie, and the server takes it for the person's own tap on a button,
//     which is what sensitive operations ask for. The agent does not open,
//     read or act in such a page.
//
// The extension holds the same two lines (web/extension/src/reach.js); this
// is the one a server deploy closes without anyone reloading the extension.

// ownPagesRefusal is what the model is told about a page of TeaNode itself
// or of the extension.
const ownPagesRefusal = "The agent does not drive TeaNode itself or the extension's pages in the person's browser."

// cdpAllowedMethods are the DevTools methods the agent may send to the
// person's tab, by exact name: driving the page with real input, reading it,
// watching what it asks the network for, and moving it. Written as a list of
// what is allowed because the protocol grows every release and a refusal
// only covers what somebody thought of. web/extension/src/reach.js has the
// same list, and a test keeps the two equal.
var cdpAllowedMethods = map[string]bool{
	// Real input, which is what driving a page is.
	"Input.dispatchMouseEvent":      true,
	"Input.dispatchKeyEvent":        true,
	"Input.dispatchTouchEvent":      true,
	"Input.insertText":              true,
	"Input.synthesizeScrollGesture": true,
	"Input.synthesizeTapGesture":    true,
	// Watching what the page asks the network for, and reading a body back.
	// Not a request's body (Network.getRequestPostData): a form's request is
	// where a typed password travels.
	"Network.enable":              true,
	"Network.disable":             true,
	"Network.getResponseBody":     true,
	"Network.setCacheDisabled":    true,
	"Network.setExtraHTTPHeaders": true,
	// The page itself: what it is, what it looks like, where it is going.
	"Page.enable":               true,
	"Page.disable":              true,
	"Page.navigate":             true,
	"Page.reload":               true,
	"Page.getLayoutMetrics":     true,
	"Page.getNavigationHistory": true,
	// Reading and running in the page's own world, which evaluate already is.
	"Runtime.enable":             true,
	"Runtime.disable":            true,
	"Runtime.evaluate":           true,
	"Runtime.callFunctionOn":     true,
	"Runtime.awaitPromise":       true,
	"Runtime.getProperties":      true,
	"Runtime.releaseObject":      true,
	"Runtime.releaseObjectGroup": true,
	// Reading the document's structure and finding where something is.
	"DOM.enable":                     true,
	"DOM.disable":                    true,
	"DOM.getDocument":                true,
	"DOM.querySelector":              true,
	"DOM.querySelectorAll":           true,
	"DOM.getOuterHTML":               true,
	"DOM.describeNode":               true,
	"DOM.resolveNode":                true,
	"DOM.requestNode":                true,
	"DOM.getBoxModel":                true,
	"DOM.getContentQuads":            true,
	"DOM.scrollIntoViewIfNeeded":     true,
	"DOM.focus":                      true,
	"Accessibility.enable":           true,
	"Accessibility.disable":          true,
	"Accessibility.getFullAXTree":    true,
	"Accessibility.getPartialAXTree": true,
	"Emulation.setEmulatedMedia":     true,
	"Log.enable":                     true,
	"Log.disable":                    true,
}

// cdpFileMethods are the methods that put a file of the person's machine
// into a page, named so that the model is told what it asked for.
var cdpFileMethods = map[string]bool{
	"DOM.setFileInputFiles":              true,
	"DOM.getFileInfo":                    true,
	"Input.dispatchDragEvent":            true,
	"Input.setInterceptDrags":            true,
	"Page.setInterceptFileChooserDialog": true,
}

// cdpRefusal says why a DevTools call is not sent to the person's tab, or
// "" when it is.
func cdpRefusal(method string) string {
	if method == "" {
		return "a method is needed, for example Input.dispatchMouseEvent or Network.enable"
	}
	if cdpFileMethods[method] {
		return fmt.Sprintf("%s is not sent: the agent does not choose files for a page or drop files onto one in the person's browser, since the page could then read them back", method)
	}
	if !cdpAllowedMethods[method] {
		return fmt.Sprintf("%s is not one of the methods an attached tab answers: driving it (Input.dispatchMouseEvent, Input.dispatchKeyEvent, Input.insertText), reading it (DOM, Runtime and Accessibility methods that read the page itself), watching what it fetches (Network.enable, Network.getResponseBody) and moving it (Page.navigate). Anything else reaches past this tab into the whole browser or the person's machine, which attaching one tab does not allow", method)
	}
	return ""
}

// cdpContextParameters name an execution context, which would run a call in
// a frame of another origin that shares the tab's process.
var cdpContextParameters = []string{"contextId", "uniqueContextId", "executionContextId"}

// cdpParametersRefusal says why the parameters of an allowed method are not
// sent, or "" when they are. Two things reach past the page the tab shows,
// into the frames in it. The panel the extension puts on a page is the
// dashboard's drawer, a frame of this server's origin, and a frame of the
// same site shares the tab's process, so the protocol can see into it:
// pierce reads the documents of frames and the shadow roots the panel sits
// in, and an execution context runs script in a frame's own world. Neither
// is the page the person attached.
func cdpParametersRefusal(method string, params map[string]any) string {
	if pierce, _ := params["pierce"].(bool); pierce {
		return fmt.Sprintf("%s is not sent with pierce: reading through frames and shadow roots reaches past the page this tab shows", method)
	}
	for _, name := range cdpContextParameters {
		if _, isGiven := params[name]; isGiven {
			return fmt.Sprintf("%s is not sent with %s: the agent runs script in the page's own world, not in another context or frame", method, name)
		}
	}
	if _, isGiven := params["frameId"]; isGiven && (method == "Page.navigate" || strings.HasPrefix(method, "Accessibility.")) {
		return fmt.Sprintf("%s is not sent with frameId: the agent works on the page's own document, not on a frame inside it", method)
	}
	return ""
}

// ownHostnames are the hostnames TeaNode itself is reached by: the ones the
// configuration gives the server and the dashboard, and the one the
// person's browser connected through. Compared without the port or the
// scheme, since a session cookie goes to every port of its host.
func ownHostnames(configuration *config.Configuration, tab tools.Tab) map[string]bool {
	hostnames := map[string]bool{}
	add := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		if parsed, err := url.Parse(host); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
		if hostname, _, err := net.SplitHostPort(host); err == nil {
			host = hostname
		}
		host = strings.Trim(strings.ToLower(host), "[]")
		hostnames[strings.TrimSuffix(host, ".")] = true
	}
	if configuration != nil {
		add(configuration.Server.Name)
		add(configuration.Passkey.RelyingPartyID)
		for _, origin := range configuration.Passkey.Origins {
			add(origin)
		}
		for _, host := range configuration.TLS.Hosts {
			add(host)
		}
		add(configuration.DashboardBase())
	}
	if tab != nil {
		add(tab.ServerHost())
	}
	delete(hostnames, "")
	return hostnames
}

// addressRefusal says why the agent does not open an address in the
// person's browser, or "" when it does: only plain http and https addresses,
// and none of TeaNode's own. ownHosts is nil to check the scheme alone.
func addressRefusal(address string, ownHosts map[string]bool) string {
	const onlyWeb = "only http and https addresses are opened in the person's browser"
	// A browser reads a backslash as a slash and drops tabs and newlines,
	// so a host written around them is read differently here and there.
	if address == "" || strings.Contains(address, "\\") || strings.IndexFunc(address, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
		return onlyWeb
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return onlyWeb
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return onlyWeb
	}
	hostname := parsed.Hostname()
	if hostname == "" || !isPlainHostname(hostname) {
		return onlyWeb
	}
	if ownHosts[normalizedHostname(hostname)] {
		return ownPagesRefusal
	}
	return ""
}

// normalizedHostname is a hostname as ownHostnames keeps them.
func normalizedHostname(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(hostname), ".")
}

// isPlainHostname says a hostname is one a browser reads the way this does:
// ASCII, no escapes, and a number only when it is written as a dotted
// decimal one. A browser also reads fullwidth letters as their plain
// twins, and 0x7f.1 or 2130706433 as an address, which would walk past a
// comparison with a name.
func isPlainHostname(hostname string) bool {
	if strings.ContainsAny(hostname, "%") {
		return false
	}
	for _, character := range hostname {
		if character > 0x7e || character < 0x21 {
			return false
		}
	}
	if net.ParseIP(hostname) != nil {
		return true
	}
	labels := strings.Split(strings.TrimSuffix(hostname, "."), ".")
	last := strings.ToLower(labels[len(labels)-1])
	isNumeric := last != "" && strings.Trim(last, "0123456789") == ""
	return !isNumeric && !strings.HasPrefix(last, "0x")
}

// isOwnPage says an address is a page of TeaNode itself or of the
// extension, which the agent does not drive.
func isOwnPage(address string, ownHosts map[string]bool) bool {
	address = strings.TrimSpace(address)
	// A blob or filesystem address belongs to the origin inside it.
	for _, prefix := range []string{"blob:", "filesystem:"} {
		if len(address) >= len(prefix) && strings.EqualFold(address[:len(prefix)], prefix) {
			address = address[len(prefix):]
		}
	}
	if address == "" {
		return false
	}
	lowered := strings.ToLower(address)
	if strings.HasPrefix(lowered, "chrome-extension:") || strings.HasPrefix(lowered, "moz-extension:") {
		return true
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return ownHosts[normalizedHostname(parsed.Hostname())]
}

// browserActionsOutsideAPage are the ones that do not read or act in the
// page the actions go to: listing tabs, opening and closing them, and
// choosing one (which is checked against the tab it chooses), and letting
// the protocol go.
var browserActionsOutsideAPage = map[string]bool{"tabs": true, "open": true, "switch": true, "close": true, "cdp_stop": true}

// tabReachRefusal says why an action is not carried to the person's browser,
// or "" when it is: an address that is not an ordinary site's, a page of
// TeaNode or of the extension, or a DevTools call outside the list.
func tabReachRefusal(configuration *config.Configuration, tab tools.Tab, arguments *browserArguments) string {
	ownHosts := ownHostnames(configuration, tab)
	switch arguments.Action {
	case "open", "navigate":
		if refusal := addressRefusal(arguments.URL, ownHosts); refusal != "" {
			return refusal
		}
	case "cdp":
		if refusal := cdpRefusal(arguments.Method); refusal != "" {
			return refusal
		}
		if refusal := cdpParametersRefusal(arguments.Method, arguments.Params); refusal != "" {
			return refusal
		}
		if arguments.Method == "Page.navigate" {
			address, _ := arguments.Params["url"].(string)
			if refusal := addressRefusal(address, ownHosts); refusal != "" {
				return refusal
			}
		}
	case "fetch":
		// The page's own session goes with a fetch, so where it goes matters
		// as much as where a tab goes: a page on a sibling host of the
		// server's site would send the server its cookie.
		if refusal := fetchRefusal(arguments.URL, tab.URL(), ownHosts); refusal != "" {
			return refusal
		}
	case "switch":
		for _, candidate := range switchCandidates(tab, arguments) {
			if isOwnPage(candidate.URL, ownHosts) {
				return ownPagesRefusal
			}
		}
	}
	if browserActionsOutsideAPage[arguments.Action] {
		return ""
	}
	// Where the actions go now: what the browser last said, and the tab it
	// lists as the current one.
	if isOwnPage(tab.URL(), ownHosts) {
		return ownPagesRefusal
	}
	for _, listed := range tab.ListedTabs() {
		if listed.IsCurrent && isOwnPage(listed.URL, ownHosts) {
			return ownPagesRefusal
		}
	}
	return ""
}

// fetchRefusal says why an address is not fetched with the tab's session, or
// "" when it is: the address as the page would resolve it, against where the
// tab is, has to be an ordinary site's.
func fetchRefusal(address, base string, ownHosts map[string]bool) string {
	if address == "" || strings.Contains(address, "\\") {
		return addressRefusal(address, ownHosts)
	}
	reference, err := url.Parse(address)
	if err != nil {
		return addressRefusal(address, ownHosts)
	}
	if !reference.IsAbs() {
		parsedBase, err := url.Parse(base)
		if err != nil || parsedBase.Host == "" || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") {
			// No page to resolve it against: the extension reads it in the page.
			return ""
		}
		reference = parsedBase.ResolveReference(reference)
	}
	return addressRefusal(reference.String(), ownHosts)
}

// switchCandidates are the listed tabs a switch could mean, the way the
// extension picks one: by number, by a piece of an address or title, or
// with neither the person's own. Every tab a piece of text could mean is
// one, since the extension takes the first and this does not know which.
func switchCandidates(tab tools.Tab, arguments *browserArguments) []tools.TabAddress {
	listed := tab.ListedTabs()
	var candidates []tools.TabAddress
	if arguments.Tab != nil {
		wanted := strconv.FormatInt(*arguments.Tab, 10)
		for _, one := range listed {
			if one.Tab == wanted {
				candidates = append(candidates, one)
			}
		}
		return candidates
	}
	words := strings.ToLower(strings.TrimSpace(arguments.URL))
	if words == "" {
		words = strings.ToLower(strings.TrimSpace(arguments.Text))
	}
	for _, one := range listed {
		switch {
		case words != "":
			if strings.Contains(strings.ToLower(one.URL), words) || strings.Contains(strings.ToLower(one.Title), words) {
				candidates = append(candidates, one)
			}
		case one.IsOwn:
			candidates = append(candidates, one)
		}
	}
	return candidates
}

// answerReachRefusal says why an answer from the person's browser is not
// handed on: the page an action ended on is TeaNode itself or the
// extension's, as a link, a redirect or a script took it there. The
// action has been done; what is refused is going on from it. Only the
// actions that say where the page went are read for it, since what a
// script returns may hold any address.
func answerReachRefusal(configuration *config.Configuration, tab tools.Tab, action string, answer json.RawMessage) string {
	var decoded struct {
		URL string `json:"url"`
		Now struct {
			URL string `json:"url"`
		} `json:"now"`
	}
	if json.Unmarshal(answer, &decoded) != nil {
		return ""
	}
	address := ""
	switch action {
	case "open", "navigate", "switch", "back", "close":
		address = decoded.URL
	case "click":
		address = decoded.Now.URL
	}
	if isOwnPage(address, ownHostnames(configuration, tab)) {
		return ownPagesRefusal + " The tab is on one now: open another page, or say what the person needs to do."
	}
	return ""
}

// hideOwnRequests takes out of what the tab watched every request made to
// this server's host, with the events that go with it: the panel is a frame
// of this server's origin, and a frame of the same site is in the tab's
// process, so its requests, with the Authorization header that signs the
// person in, are among the page's. What a page asks other sites for is what
// the agent watches the network for, and stays.
func hideOwnRequests(answer json.RawMessage, ownHosts map[string]bool) json.RawMessage {
	var decoded map[string]json.RawMessage
	if json.Unmarshal(answer, &decoded) != nil {
		return answer
	}
	var events []map[string]any
	if json.Unmarshal(decoded["events"], &events) != nil {
		return answer
	}
	isOwnAddress := func(value any) bool {
		address, _ := value.(string)
		return address != "" && isOwnPage(address, ownHosts)
	}
	hidden := map[string]bool{}
	for _, event := range events {
		params, _ := event["params"].(map[string]any)
		request, _ := params["request"].(map[string]any)
		response, _ := params["response"].(map[string]any)
		if isOwnAddress(request["url"]) || isOwnAddress(response["url"]) || isOwnAddress(params["url"]) {
			if requestID, _ := params["requestId"].(string); requestID != "" {
				hidden[requestID] = true
			}
		}
	}
	kept := make([]map[string]any, 0, len(events))
	for _, event := range events {
		params, _ := event["params"].(map[string]any)
		if requestID, _ := params["requestId"].(string); hidden[requestID] {
			continue
		}
		kept = append(kept, event)
	}
	keptEvents, err := json.Marshal(kept)
	if err != nil {
		return answer
	}
	decoded["events"] = keptEvents
	filtered, err := json.Marshal(decoded)
	if err != nil {
		return answer
	}
	return filtered
}
