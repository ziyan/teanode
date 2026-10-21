// What the agent may reach through the person's own browser.
//
// The tab carries the person's session, so two things stay out of the
// agent's reach, whatever a page it has read tells it to do:
//
//   - Files on the person's machine. The DevTools protocol can choose the
//     files of a file input (DOM.setFileInputFiles) or drop files onto a
//     page (Input.dispatchDragEvent), and a page script then reads their
//     contents back: any file the person can read. So the protocol is a list
//     of exact method names, and nothing else is sent.
//   - TeaNode itself, and this extension's own pages. A request that a page
//     script makes from the server's own origin carries the person's session
//     cookie, and the server takes it for the person's own tap on a button,
//     which is what sensitive operations ask for. The agent does not open,
//     read or act in such a page.
//
// The server holds the same two lines (internal/agent/tools/browser/reach.go)
// and refuses first, so that a server deploy closes them without anyone
// reloading this extension; this is the second layer, and the only one that
// sees where a tab is at the moment of the action.

// OWN_PAGES_REFUSAL is what the model is told about a page of TeaNode itself
// or of the extension.
export const OWN_PAGES_REFUSAL =
  "The agent does not drive TeaNode itself or the extension's pages in the person's browser."

// CDP_ALLOWED are the DevTools methods the agent may send to the person's
// tab, by exact name: driving the page with real input, reading it, watching
// what it asks the network for, and moving it.
//
// The line is not "the agent is trusted" -- it is their agent and their tab --
// but what attaching one tab is: everything that tab's own origin can reach,
// plus real input and what the page asks the network for. Anything that
// reaches past the tab into the browser, or onto the person's machine, is not
// that.
//
// Written as a list of what is allowed because the protocol grows every
// release and a refusal only covers what somebody thought of. A prefix such
// as 'DOM.' once stood here and let through DOM.setFileInputFiles, which
// puts a local file into a page; each name is now spelled out, and
// something new is refused until somebody decides otherwise. The server's
// list is the same, and a test keeps the two equal.
export const CDP_ALLOWED = [
  // Real input, which is what driving a page is.
  'Input.dispatchMouseEvent',
  'Input.dispatchKeyEvent',
  'Input.dispatchTouchEvent',
  'Input.insertText',
  'Input.synthesizeScrollGesture',
  'Input.synthesizeTapGesture',
  // Watching what the page asks the network for, and reading a body back.
  // Not Network.setCookie and friends, which are every site's cookies, and
  // not the clearBrowser* pair, which are the whole browser.
  'Network.enable',
  'Network.disable',
  'Network.getResponseBody',
  'Network.getRequestPostData',
  'Network.setCacheDisabled',
  'Network.setExtraHTTPHeaders',
  // The page itself: what it is, what it looks like, where it is going.
  'Page.enable',
  'Page.disable',
  'Page.navigate',
  'Page.reload',
  'Page.captureSnapshot',
  'Page.getLayoutMetrics',
  'Page.getFrameTree',
  'Page.getNavigationHistory',
  // Reading and running in the page's own world, which evaluate already is.
  'Runtime.enable',
  'Runtime.disable',
  'Runtime.evaluate',
  'Runtime.callFunctionOn',
  'Runtime.awaitPromise',
  'Runtime.getProperties',
  'Runtime.releaseObject',
  'Runtime.releaseObjectGroup',
  // Reading the document's structure and finding where something is.
  'DOM.enable',
  'DOM.disable',
  'DOM.getDocument',
  'DOM.querySelector',
  'DOM.querySelectorAll',
  'DOM.getOuterHTML',
  'DOM.describeNode',
  'DOM.resolveNode',
  'DOM.requestNode',
  'DOM.getBoxModel',
  'DOM.getContentQuads',
  'DOM.getNodeForLocation',
  'DOM.scrollIntoViewIfNeeded',
  'DOM.focus',
  'DOM.performSearch',
  'DOM.getSearchResults',
  'DOM.discardSearchResults',
  'DOMSnapshot.enable',
  'DOMSnapshot.disable',
  'DOMSnapshot.captureSnapshot',
  'Accessibility.enable',
  'Accessibility.disable',
  'Accessibility.getFullAXTree',
  'Accessibility.getPartialAXTree',
  'Emulation.setEmulatedMedia',
  'Log.enable',
  'Log.disable',
]

// CDP_FILE_METHODS are the methods that put a file of the person's machine
// into a page, named so that the model is told what it asked for.
export const CDP_FILE_METHODS = [
  'DOM.setFileInputFiles',
  'DOM.getFileInfo',
  'Input.dispatchDragEvent',
  'Input.setInterceptDrags',
  'Page.setInterceptFileChooserDialog',
]

const allowedMethods = new Set(CDP_ALLOWED)
const fileMethods = new Set(CDP_FILE_METHODS)

// cdpRefusal says why a DevTools call is not sent to the person's tab, or ''
// when it is. The name is compared exactly as it was given: a method with a
// space or a different case is not one on the list.
export function cdpRefusal(method) {
  if (typeof method !== 'string' || method === '') {
    return 'a method is needed, for example Input.dispatchMouseEvent or Network.enable'
  }
  if (fileMethods.has(method)) {
    return `${method} is not sent: the agent does not choose files for a page or drop files onto one in the person's browser, since the page could then read them back`
  }
  if (!allowedMethods.has(method)) {
    return `${method} is not one of the methods an attached tab answers: driving it (Input.dispatchMouseEvent, Input.dispatchKeyEvent, Input.insertText), reading it (DOM, Runtime, Accessibility and DOMSnapshot methods that read), watching what it fetches (Network.enable, Network.getResponseBody) and moving it (Page.navigate). Anything else reaches past this tab into the whole browser or the person's machine, which attaching one tab does not allow`
  }
  return ''
}

// hostnameOf is the hostname of an address, lowercase and without a trailing
// dot, or '' when it has none. A blob address belongs to the origin inside
// it. The port and the scheme are not part of it: a session cookie goes to
// every port of its host.
function hostnameOf(address) {
  let parsed
  try {
    parsed = new URL(String(address || '').replace(/^(blob|filesystem):(?=[a-z]+:)/i, ''))
  } catch {
    return ''
  }
  return parsed.hostname
    .replace(/^\[|\]$/g, '')
    .replace(/\.$/, '')
    .toLowerCase()
}

// serverHostname is the hostname of the server the extension is signed in to.
export function serverHostname(origin) {
  const written = String(origin || '').trim()
  if (!written) return ''
  return hostnameOf(/^[a-z][a-z0-9+.-]*:\/\//i.test(written) ? written : 'https://' + written)
}

// isOwnPage says an address is a page of TeaNode itself, on the server the
// extension is signed in to, or of an extension. An address that cannot be
// read as one is not a page to act in as the person either.
export function isOwnPage(address, serverOrigin) {
  const written = String(address || '').trim()
  if (!written) return false
  if (/^(chrome|moz)-extension:/i.test(written)) return true
  if (/^(blob|filesystem):(chrome|moz)-extension:/i.test(written)) return true
  const own = serverHostname(serverOrigin)
  return own !== '' && hostnameOf(written) === own
}

// addressRefusal says why the agent does not open an address in the person's
// browser, or '' when it does: only plain http and https addresses, and none
// of TeaNode's own.
export function addressRefusal(address, serverOrigin) {
  const onlyWeb = "only http and https addresses are opened in the person's browser"
  let parsed
  try {
    parsed = new URL(String(address || ''))
  } catch {
    return onlyWeb
  }
  // The browser's own reading of the address is what counts: it reads a
  // backslash as a slash and drops tabs and newlines, as the tab will.
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return onlyWeb
  if (isOwnPage(parsed.href, serverOrigin)) return OWN_PAGES_REFUSAL
  return ''
}

// ACTIONS_OUTSIDE_A_PAGE are the ones that do not read or act in the page the
// actions go to: listing tabs, opening and closing them, choosing one (which
// is checked against the tab it chooses), and letting the protocol go.
export const ACTIONS_OUTSIDE_A_PAGE = new Set(['tabs', 'open', 'switch', 'close', 'cdp_stop'])

// pageRefusal says why an action is not done in a tab, given where the tab
// is at this moment (its address, or the address it is on its way to), or ''
// when it is.
export function pageRefusal(action, tab, serverOrigin) {
  if (ACTIONS_OUTSIDE_A_PAGE.has(action)) return ''
  if (!tab) return ''
  if (isOwnPage(tab.url, serverOrigin) || isOwnPage(tab.pendingUrl, serverOrigin)) return OWN_PAGES_REFUSAL
  return ''
}

// The person's own TeaNode panel.
//
// panel.js puts the dashboard's drawer, signed in as the person, in a frame on
// the page the extension's button was pressed on. Input the DevTools protocol
// dispatches is real input, as the person's own hardware sends it, so a click
// the agent dispatches at the place where that frame is lands inside TeaNode
// itself, and the server cannot tell it from the person's tap. A page script
// cannot do this to a frame of another origin, so only the protocol's input
// needs the line, and a script that clicks, hovers, types or chooses in an
// element is kept off elements the panel covers for the same reason: the
// person would see a click land under the panel.
export const PANEL_REFUSAL = "The agent does not click inside the person's TeaNode panel."
export const PANEL_KEYS_REFUSAL =
  "The agent does not type while the person's TeaNode panel has focus, since the keys would go into it."
export const PANEL_ELEMENT_REFUSAL =
  "The agent does not click inside the person's TeaNode panel, and that element is under it. Ask the person to move or close the panel, or scroll the page so the element is clear of it."

// panelProbe runs in the page, in the extension's own world, and says where
// the panel is now: whether it is showing, its box in the viewport's
// coordinates (which are the protocol's), and whether the frame in it has
// focus. It is read at the moment of the action rather than reported when the
// panel moved, since a page can move its parts by a style rule that no event
// announces. It is self-contained: nothing from this file is in scope there.
// An element the page renamed is still found, as panel.js keeps it in its
// world; a panel put on the page before that was done is found by its id.
export function panelProbe() {
  const host = window.__teanodePanel || document.getElementById('teanode-agent-panel')
  if (!host || !host.isConnected) return { isOpen: false }
  const box = host.getBoundingClientRect()
  const isShowing = box.width > 0 && box.height > 0 && window.getComputedStyle(host).visibility !== 'hidden'
  if (!isShowing) return { isOpen: false }
  return {
    isOpen: true,
    left: box.left,
    top: box.top,
    right: box.right,
    bottom: box.bottom,
    hasFocus: document.activeElement === host,
  }
}

// isInsidePanel says a point, in the viewport's coordinates, is in the panel.
function isInsidePanel(x, y, panel) {
  return x >= panel.left && x <= panel.right && y >= panel.top && y <= panel.bottom
}

// overlapsPanel says a box (a DOMRect or the like) touches the panel.
export function overlapsPanel(box, panel) {
  if (!panel || !panel.isOpen) return false
  return box.left <= panel.right && box.right >= panel.left && box.top <= panel.bottom && box.bottom >= panel.top
}

const POINT_METHODS = new Set([
  'Input.dispatchMouseEvent',
  'Input.synthesizeTapGesture',
  'Input.synthesizeScrollGesture',
])
const KEY_METHODS = new Set(['Input.dispatchKeyEvent', 'Input.insertText'])

// panelRefusal says why a DevTools input call is not sent while the panel is
// where it is, or '' when it is: a point inside the panel, or keys while its
// frame has focus. Methods that are not input are not about the panel.
export function panelRefusal(method, params, panel) {
  if (!panel || !panel.isOpen || typeof method !== 'string' || !method.startsWith('Input.')) return ''
  if (KEY_METHODS.has(method)) return panel.hasFocus ? PANEL_KEYS_REFUSAL : ''
  const given = params && typeof params === 'object' ? params : {}
  let points
  if (POINT_METHODS.has(method)) points = [given]
  else if (method === 'Input.dispatchTouchEvent') points = Array.isArray(given.touchPoints) ? given.touchPoints : []
  else return PANEL_REFUSAL
  for (const point of points) {
    const x = point && point.x
    const y = point && point.y
    // A point that is not a pair of numbers is one the browser refuses too.
    if (typeof x !== 'number' || typeof y !== 'number' || !Number.isFinite(x) || !Number.isFinite(y))
      return 'x and y must be numbers'
    if (isInsidePanel(x, y, panel)) return PANEL_REFUSAL
  }
  return ''
}
