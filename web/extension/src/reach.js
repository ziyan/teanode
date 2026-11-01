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
  // Not a request's body (Network.getRequestPostData): a form's request is
  // where a typed password travels. Not Network.setCookie and friends, which are every site's cookies, and
  // not the clearBrowser* pair, which are the whole browser.
  'Network.enable',
  'Network.disable',
  'Network.getResponseBody',
  'Network.setCacheDisabled',
  'Network.setExtraHTTPHeaders',
  // The page itself: what it is, what it looks like, where it is going.
  'Page.enable',
  'Page.disable',
  'Page.navigate',
  'Page.reload',
  'Page.getLayoutMetrics',
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
  'DOM.scrollIntoViewIfNeeded',
  'DOM.focus',
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
    return `${method} is not one of the methods an attached tab answers: driving it (Input.dispatchMouseEvent, Input.dispatchKeyEvent, Input.insertText), reading it (DOM, Runtime and Accessibility methods that read the page itself), watching what it fetches (Network.enable, Network.getResponseBody) and moving it (Page.navigate). Anything else reaches past this tab into the whole browser or the person's machine, which attaching one tab does not allow`
  }
  return ''
}

// CDP_CONTEXT_PARAMETERS name an execution context, which would run a call in
// a frame of another origin that shares the tab's process.
const CDP_CONTEXT_PARAMETERS = ['contextId', 'uniqueContextId', 'executionContextId']

// cdpParametersRefusal says why the parameters of an allowed method are not
// sent, or '' when they are. Two things reach past the page the tab shows,
// into the frames in it. A frame of the server's origin shares the tab's
// process, so the protocol can see into it: pierce reads the documents of
// frames and their shadow roots, and an execution context runs script in a
// frame's own world. Neither is the page the person attached.
export function cdpParametersRefusal(method, params) {
  const given = params && typeof params === 'object' ? params : {}
  if (given.pierce === true) {
    return `${method} is not sent with pierce: reading through frames and shadow roots reaches past the page this tab shows`
  }
  for (const name of CDP_CONTEXT_PARAMETERS) {
    if (name in given)
      return `${method} is not sent with ${name}: the agent runs script in the page's own world, not in another context or frame`
  }
  if ('frameId' in given && (method === 'Page.navigate' || method.startsWith('Accessibility.'))) {
    return `${method} is not sent with frameId: the agent works on the page's own document, not on a frame inside it`
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

// fetchRefusal says why an address is not fetched with the tab's session, or
// '' when it is: the address as the page resolves it, against where the tab
// is, has to be an ordinary site's. A page on a sibling host of the server's
// site would otherwise send the server its cookie.
export function fetchRefusal(address, base, serverOrigin) {
  if (!address) return addressRefusal(address, serverOrigin)
  let resolved
  try {
    resolved = new URL(String(address || ''), base)
  } catch {
    return addressRefusal(address, serverOrigin)
  }
  return addressRefusal(resolved.href, serverOrigin)
}

// What the agent is shown of the requests the tab made.
//
// A frame of the server's origin shares the tab's process, so its requests,
// with the Authorization header that signs the person in, are among the page's
// network events. What a page asks other sites for is what the agent watches
// the network for, and is shown.
//
// The worker keeps few events, and a page that makes many pushes a
// connection's first events out of them while its later ones, which can carry
// the header, go on arriving. So what is allowed is kept apart from the events,
// for as long as the tab is debugged: a record of every request seen created,
// with the places (hostnames, or the marks below) that its events named. A
// request is shown only when its creation was seen and no event of it named
// the server, an extension or an address that cannot be read; an event of a
// request never seen created is not shown, and nor is its body.
export const REQUESTS_KEPT = 20000
const PLACE_EXTENSION = 'extension:'
const PLACE_UNREADABLE = '?'

export const REQUEST_BODY_REFUSAL =
  'only a request that cdp_events has shown has its body read; a request to TeaNode itself, or one cdp_events has not shown, is not'

// newRequestRecord is an empty record, for a debugging session that begins.
export function newRequestRecord() {
  return new Map()
}

// placeOf is where an address goes, as the record keeps it: its hostname
// (lowercase, no trailing dot, '' for an address with none such as data:), or
// a mark.
function placeOf(address) {
  const written = String(address || '')
    .trim()
    .replace(/^(blob|filesystem):(?=[a-z]+:)/i, '')
  if (/^(chrome|moz)-extension:/i.test(written)) return PLACE_EXTENSION
  let parsed
  try {
    parsed = new URL(written)
  } catch {
    return PLACE_UNREADABLE
  }
  const hostname = parsed.hostname
    .replace(/^\[|\]$/g, '')
    .replace(/\.$/, '')
    .toLowerCase()
  if (hostname === '') return parsed.protocol === 'data:' || parsed.protocol === 'about:' ? '' : PLACE_UNREADABLE
  return hostname
}

// requestPlaces are the places the addresses in a network event name: the
// request's, the response's, the one a WebSocket was created for, and the one
// a redirect came from.
function requestPlaces(params) {
  const places = []
  const add = (container, key) => {
    if (!container || typeof container !== 'object' || !(key in container)) return
    const address = container[key]
    places.push(typeof address === 'string' && address !== '' ? placeOf(address) : PLACE_UNREADABLE)
  }
  add(params.request, 'url')
  add(params.response, 'url')
  add(params.redirectResponse, 'url')
  add(params, 'url')
  return places
}

// noteRequestEvent learns from one event of the tab's network, as it arrives:
// a request is known from its creation, which is the first request event of an
// id (not a redirect, which has the same id and a new address) or a WebSocket
// created, and every later event of it adds the places it names. The oldest
// requests are forgotten past REQUESTS_KEPT, and a forgotten request is not
// shown.
export function noteRequestEvent(record, method, params) {
  if (typeof method !== 'string' || !method.startsWith('Network.') || !params || typeof params.requestId !== 'string')
    return
  const requestId = params.requestId
  if (requestId === '') return
  const isRedirect = params.redirectResponse !== undefined && params.redirectResponse !== null
  const isCreation = (method === 'Network.requestWillBeSent' && !isRedirect) || method === 'Network.webSocketCreated'
  const places = requestPlaces(params)
  if (!record.has(requestId)) {
    if (!isCreation || places.length === 0) return
    record.set(requestId, new Set())
    while (record.size > REQUESTS_KEPT) record.delete(record.keys().next().value)
  }
  for (const place of places) record.get(requestId).add(place)
}

// isRequestShown says the agent may be shown a request, or have its body read.
export function isRequestShown(record, requestId, serverOrigin) {
  const own = serverHostname(serverOrigin)
  const places = typeof requestId === 'string' ? record.get(requestId) : undefined
  // Without the server's own name nothing can be told from it.
  if (!places || own === '') return false
  for (const place of places) {
    if (place === PLACE_EXTENSION || place === PLACE_UNREADABLE || place === own) return false
  }
  return true
}

// networkEventsShown are the events the agent may see: any that is not the
// network's, and the network's only for requests that may be shown.
export function networkEventsShown(record, events, serverOrigin) {
  return events.filter((event) => {
    if (typeof event.method !== 'string') return false
    if (!event.method.startsWith('Network.')) return true
    return isRequestShown(record, event.params && event.params.requestId, serverOrigin)
  })
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
