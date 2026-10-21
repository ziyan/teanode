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
	"Network.enable":              true,
	"Network.disable":             true,
	"Network.getResponseBody":     true,
	"Network.getRequestPostData":  true,
	"Network.setCacheDisabled":    true,
	"Network.setExtraHTTPHeaders": true,
	// The page itself: what it is, what it looks like, where it is going.
	"Page.enable":               true,
	"Page.disable":              true,
	"Page.navigate":             true,
	"Page.reload":               true,
	"Page.captureSnapshot":      true,
	"Page.getLayoutMetrics":     true,
	"Page.getFrameTree":         true,
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
	"DOM.getNodeForLocation":         true,
	"DOM.scrollIntoViewIfNeeded":     true,
	"DOM.focus":                      true,
	"DOM.performSearch":              true,
	"DOM.getSearchResults":           true,
	"DOM.discardSearchResults":       true,
	"DOMSnapshot.enable":             true,
	"DOMSnapshot.disable":            true,
	"DOMSnapshot.captureSnapshot":    true,
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
		return fmt.Sprintf("%s is not one of the methods an attached tab answers: driving it (Input.dispatchMouseEvent, Input.dispatchKeyEvent, Input.insertText), reading it (DOM, Runtime, Accessibility and DOMSnapshot methods that read), watching what it fetches (Network.enable, Network.getResponseBody) and moving it (Page.navigate). Anything else reaches past this tab into the whole browser or the person's machine, which attaching one tab does not allow", method)
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
		if arguments.Method == "Page.navigate" {
			address, _ := arguments.Params["url"].(string)
			if refusal := addressRefusal(address, ownHosts); refusal != "" {
				return refusal
			}
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
