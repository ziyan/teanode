// The extension connects the person's browser to their agent over the
// server's websocket whenever it is signed in, saying who it is with the
// token the options page signed in with. The server never touches a page:
// every action arrives here as a request, is done by the content script in
// the page, and its answer goes back.
//
// Connected, the agent may open a tab in the person's browser, in a
// "TeaNode" tab group, and act there with their session. The person may
// also attach a tab of their own, which the agent then acts in; attaching
// and detaching say which tab is theirs, and the connection stays either
// way.
//
// It is the person's own browser and their own session, so the agent acts
// as they would and nothing here refuses on their behalf. Two lines are held
// all the same, because neither is about what the agent may do in a tab: a
// password already filled in on the page is not read back into the
// conversation, and the DevTools relay refuses the handful of methods that
// reach past a tab into the whole browser.
//
// The person can also watch those tabs from the dashboard's browser page,
// on any device, and take one over: the tab is streamed to the server as
// pictures while somebody watches it, and what the person does in the
// picture comes back here as input, while they hold it. Their screen does
// not change for it.

// Passwords: the service worker's half (messages from the content scripts and
// the extension's own pages, the keys held in the session storage, the lock
// time), and the agent's fill_login and fill_one_time_code. Written in
// TypeScript with the dashboard's own encryption; see passwords/.
import './passwords/background'
// The token and the other storage the extension keeps are closed to its
// content scripts (the part of it that runs inside web pages).
import { restrictStorageToTrustedContexts } from './storageAccess'
import { serverAddressProblem } from './serverAddress'
import { fillLoginForAgent, fillOneTimeCodeForAgent } from './passwords/agentFill'
// Which document each of the agent's tabs shows and which the agent filled a
// password or code into: the server locks a filled tab until it commits a new
// document, and is told when one does.
import { TabDocuments, withoutRequestBodies } from './tabDocuments'
// What is refused on its own for a tab showing a document the agent filled.
import { lockedActionRefusal, lockedMethodRefusal } from './lockedTab'
// What the agent's actions do inside the page (snapshot, click, type ...).
import { inPage } from './inPage'

const PROTOCOL = 5

void restrictStorageToTrustedContexts()

let socket = null
const documents = new TabDocuments()
// attached is the tab the actions go to and the person's own tab, either of
// which may be none: ownTabId is theirs, attached through the panel, and
// tabId the one actions go to -- theirs, or one the agent opened.
const attached = { tabId: null, ownTabId: null, title: '', url: '' }
// groups are the "TeaNode" tab groups the agent's tabs live in, by window.
// Kept in the session's storage beside the opened tabs: the worker is
// started and stopped freely, and a group remembered only in memory was
// forgotten every time, so each tab was put in a group of its own or left
// out of one entirely.
const groups = new Map()
// opened are the tabs the agent opened, by id: what it may switch to and
// close. Kept in the session's storage, so that a worker started again
// still knows them; a tab the person moved into the group is not one.
const opened = new Set()
// isStoredTabOverruled says the person signed out or let their tab go
// while the worker was still reading what it had before it stopped: what
// it read is from before, and is not put back.
let isStoredTabOverruled = false
const restored = chrome.storage.session.get(['opened', 'groups', 'ownTabId', 'tabId', 'documents']).then(async ({ opened: kept, groups: keptGroups, ownTabId, tabId, documents: keptDocuments }) => {
  for (const id of kept || []) opened.add(id)
  // Which document each tab shows and which were filled, so that a worker
  // started again still keeps nothing of a tab that shows a filled document.
  documents.restore(keptDocuments)
  for (const [windowId, groupId] of keptGroups || []) groups.set(Number(windowId), groupId)
  // The tab the actions went to, if the worker was stopped with one and it
  // is still there. Kept in memory only, it was forgotten whenever the
  // worker stopped, and the agent was told there was no tab while the one
  // it had opened sat in front of the person.
  //
  // Each is looked at again after the wait for the tab: the person may
  // have attached another tab meanwhile, the click that started the
  // worker, and that one is what the actions go to and what is kept.
  if (tabId && attached.tabId === null && !isStoredTabOverruled) {
    await chrome.tabs.get(tabId).then(() => {
      if (attached.tabId === null && !isStoredTabOverruled) attached.tabId = tabId
    }).catch(() => attached.tabId === null && chrome.storage.session.remove('tabId'))
  }
  // The person's own tab, if they had attached one before the worker was
  // stopped, and it is still there.
  if (ownTabId && attached.ownTabId === null && !isStoredTabOverruled) {
    await chrome.tabs.get(ownTabId).then(() => {
      if (attached.ownTabId !== null || isStoredTabOverruled) return
      attached.ownTabId = ownTabId
      if (attached.tabId === null) attached.tabId = ownTabId
    }).catch(() => attached.ownTabId === null && chrome.storage.session.remove('ownTabId'))
  }
})
const rememberOpened = () => chrome.storage.session.set({ opened: [...opened] })
const rememberDocuments = () => chrome.storage.session.set({ documents: documents.keep() })
const rememberGroups = () => chrome.storage.session.set({ groups: [...groups] })
const rememberOwn = () => chrome.storage.session.set({ ownTabId: attached.ownTabId })
// useTab makes a tab, or none, the one the actions go to, and keeps that
// beside ownTabId for a worker started again.
const useTab = (tabId) => {
  attached.tabId = tabId
  return chrome.storage.session.set({ tabId })
}
let pings = null
// attempt counts the connections that did not last, for waiting longer
// before each next one; retry is the timer that makes it.
let attempt = 0
let retry = null
setBadge('')

async function serverOrigin() {
  const { server } = await chrome.storage.sync.get('server')
  return (server || '').replace(/\/+$/, '')
}

// The token the options page signed in with: what the browser says it is.
async function token() {
  const { token } = await chrome.storage.local.get('token')
  return token || ''
}

function setBadge(text, color) {
  chrome.action.setBadgeText({ text })
  if (color) chrome.action.setBadgeBackgroundColor({ color })
}

// showState is the badge for how things stand: "on" with a tab of theirs
// attached, a dot while connected with none, nothing while not connected.
function showState() {
  if (!socket || socket.readyState !== WebSocket.OPEN) return
  if (attached.ownTabId !== null) setBadge('on', '#2a7')
  else setBadge('·', '#777')
}

// connectIfSignedIn opens the connection when the extension is signed in
// and none is open: when the worker starts, when the browser starts, and
// when the person signs in.
// connecting is the attempt under way, so that the worker starting and the
// browser starting at once open one connection rather than two.
let connecting = null
function connectIfSignedIn() {
  if (!connecting) {
    connecting = (async () => {
      await restored
      if (socket) return
      const origin = await serverOrigin()
      const secret = await token()
      if (!origin || !secret || serverAddressProblem(origin) !== '') return
      connect(origin, secret)
    })().finally(() => {
      connecting = null
    })
  }
  return connecting
}

// attach makes a tab the person's own: the agent's actions go to it, beside
// the tabs it opens. The connection is opened first where it is not.
async function attach(tab) {
  const origin = await serverOrigin()
  const secret = await token()
  if (!origin || !secret || serverAddressProblem(origin) !== '') {
    // Typed without a scheme, the address builds a WebSocket URL that
    // throws, and the button did nothing with nothing said. An http address
    // to another computer is refused the same way: the token would go in the clear.
    chrome.runtime.openOptionsPage()
    return
  }
  attached.ownTabId = tab.id
  await useTab(tab.id)
  attached.title = tab.title || ''
  attached.url = tab.url || ''
  await rememberOwn()
  if (socket && socket.readyState === WebSocket.OPEN) {
    tellServer()
    showState()
  } else if (!socket) {
    connect(origin, secret)
  }
  tellPanels()
}

// detach gives the person's tab back. The connection stays: the agent can
// still open a tab of its own when one is wanted.
async function detach() {
  isStoredTabOverruled = true
  const own = attached.ownTabId
  if (own === null) return
  // The protocol goes with the tab: otherwise Chrome's own "is debugging
  // this browser" bar stays up on a tab the agent can no longer reach.
  await letDebuggerGo(own)
  attached.ownTabId = null
  await rememberOwn()
  if (attached.tabId === own) {
    const mine = await agentTabs()
    await useTab(mine.length > 0 ? mine[mine.length - 1].id : null)
  }
  await current().catch(() => {})
  showState()
  tellPanels()
}

// disconnect closes the connection for good, as signing out does.
function disconnect() {
  isStoredTabOverruled = true
  clearTimeout(retry)
  retry = null
  letEveryDebuggerGo()
  const closing = socket
  socket = null
  if (closing) {
    try {
      closing.close()
    } catch {
      // Already gone.
    }
  }
  clearInterval(pings)
  attached.ownTabId = null
  void useTab(null)
  void rememberOwn()
  setBadge('')
  tellPanels()
}

// tellServer says which tab the actions go to, whether there is one, and
// which tabs there are to watch.
function tellServer() {
  if (!socket || socket.readyState !== WebSocket.OPEN) return
  const sending = socket
  const update = { type: 'update', title: attached.title, url: attached.url, hasTab: attached.tabId !== null, currentTab: attached.tabId }
  void listTabs()
    .catch(() => undefined)
    .then((tabs) => {
      if (sending.readyState !== WebSocket.OPEN) return
      // The documents the agent's tabs committed since the last update ride
      // along, taken only now that they are sent: one that could not be sent
      // is sent with the next.
      const commits = documents.takeReports()
      sending.send(JSON.stringify({ ...update, ...(tabs ? { tabs } : {}), ...(commits.length > 0 ? { commits } : {}) }))
    })
}

// tellSoon says so once a burst of tab changes settles: a page loading
// changes its title and address several times over.
let tellTimer = null
function tellSoon() {
  clearTimeout(tellTimer)
  tellTimer = setTimeout(() => {
    tellTimer = null
    tellServer()
  }, 300)
}

// isListed says a tab is one the agent may act in: the person's own, or one
// it opened.
const isListed = (tabId) => tabId === attached.ownTabId || opened.has(tabId)

// listTabs is the person's own tab, then the ones the agent opened, each
// saying whether the actions go to it.
async function listTabs() {
  const own = attached.ownTabId === null ? null : await chrome.tabs.get(attached.ownTabId).catch(() => null)
  const listed = []
  if (own) listed.push({ tab: own.id, title: own.title || '', url: own.url || '', own: true, current: own.id === attached.tabId })
  for (const mine of await agentTabs()) {
    listed.push({ tab: mine.id, title: mine.title || '', url: mine.url || '', own: false, current: mine.id === attached.tabId })
  }
  return listed
}

// connect opens the socket and puts it back when it drops, for as long as
// the extension is signed in. A closed lid, a server restarted, a proxy
// that times out an idle socket: none of them should need the person to do
// anything.
function connect(origin, secret) {
  const address = origin.replace(/^http/, 'ws') + '/api/v1/agent/tab'
  let opening
  try {
    opening = new WebSocket(address)
  } catch {
    setBadge('!', '#c33')
    return
  }
  socket = opening
  opening.onopen = async () => {
    if (attached.tabId !== null) {
      const found = await chrome.tabs.get(attached.tabId).catch(() => null)
      if (found) {
        attached.title = found.title || ''
        attached.url = found.url || ''
      } else {
        await useTab(null)
      }
    }
    const tabs = await listTabs().catch(() => [])
    opening.send(JSON.stringify({ type: 'hello', protocol: PROTOCOL, token: secret, title: attached.title, url: attached.url, hasTab: attached.tabId !== null, currentTab: attached.tabId, tabs }))
    // A word every so often keeps this worker, and the socket, alive
    // while nothing else is said.
    clearInterval(pings)
    pings = setInterval(() => {
      if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'ping' }))
    }, 20000)
  }
  opening.onmessage = async (event) => {
    let message
    try {
      message = JSON.parse(event.data)
    } catch {
      return
    }
    if (message.type === 'welcome') {
      if (message.protocol !== PROTOCOL) {
        opening.send(JSON.stringify({ type: 'bye', reason: 'protocol ' + message.protocol + ' is not known to this extension' }))
        disconnect()
        return
      }
      attempt = 0
      showState()
      tellPanels()
      // What the tabs committed while the server was away.
      tellServer()
      return
    }
    if (message.type === 'refused') {
      disconnect()
      setBadge('!', '#c33')
      // A token the server no longer takes: sign in again.
      if (/token|sign in/i.test(message.reason || '')) chrome.runtime.openOptionsPage()
      return
    }
    if (message.type === 'cast') {
      if (message.isOn) void startCast(message.tab, message)
      else void stopCast(message.tab)
      return
    }
    if (message.type === 'emulate') {
      void emulate(message)
      return
    }
    if (INPUT_TYPES.has(message.type)) {
      void dispatchInput(message)
      return
    }
    if (message.type === 'act') {
      const answer = await act(message.action, message.args || {})
      // On the socket the request came on, or not at all: the server
      // numbers a request for the connection it sent it on, and a late
      // answer sent on a newer one is nobody's.
      if (opening.readyState === WebSocket.OPEN) opening.send(JSON.stringify({ type: 'result', id: message.id, ...answer }))
    }
  }
  opening.onclose = () => {
    // A connection closed on purpose has already been let go of.
    if (socket !== opening) return
    clearInterval(pings)
    letEveryDebuggerGo()
    socket = null
    tellPanels()
    // Put it back, slower each time, up to half a minute, while the
    // extension is still signed in.
    setBadge('…', '#a80')
    const waiting = Math.min(30000, 1000 * 2 ** Math.min(5, attempt++))
    clearTimeout(retry)
    retry = setTimeout(() => {
      retry = null
      void connectIfSignedIn().then(() => {
        if (!socket) setBadge('')
      })
    }, waiting)
  }
  opening.onerror = () => setBadge('!', '#c33')
}

// Connected whenever signed in: when the worker starts, when the browser
// starts, and when the person signs in or out.
void connectIfSignedIn()
chrome.runtime.onStartup.addListener(() => void connectIfSignedIn())
chrome.runtime.onInstalled.addListener(() => void connectIfSignedIn())
chrome.storage.onChanged.addListener((changes, area) => {
  if (!(area === 'local' && changes.token) && !(area === 'sync' && changes.server)) return
  disconnect()
  void connectIfSignedIn()
})

// tellPanels says to the open panels whether their tab is the one attached,
// so their button reads right; a page without a panel does not mind.
async function tellPanels() {
  const tabs = await chrome.tabs.query({})
  for (const tab of tabs) {
    chrome.tabs.sendMessage(tab.id, { type: 'state', attached: attached.ownTabId === tab.id }).catch(() => {})
  }
}

// current is what the tab the actions go to shows now, or that there is
// none, told to the server so the agent's overlay names it.
async function current() {
  if (attached.tabId === null) {
    attached.title = ''
    attached.url = ''
    tellServer()
    return { tab: null }
  }
  const tab = await chrome.tabs.get(attached.tabId)
  attached.title = tab.title || ''
  attached.url = tab.url || ''
  tellServer()
  return { tab: tab.id, url: attached.url, title: attached.title }
}

// groupFor puts a tab in this window's TeaNode group, making the group
// when there is none. Grouping is how the person sees at a glance which
// tabs are the agent's, so it is worth doing -- but it is not worth
// failing the whole call for: a tab that opened and was not grouped is
// still the tab they asked for.
async function groupFor(windowId, tabId) {
  await restored
  try {
    const known = groups.get(windowId)
    if (known !== undefined) {
      // It may have been closed, or be in another window by now, in which
      // case grouping into it would move the tab out of this one.
      const existing = await chrome.tabGroups.get(known).catch(() => null)
      if (existing && existing.windowId === windowId) {
        await chrome.tabs.group({ tabIds: [tabId], groupId: known })
        return known
      }
      groups.delete(windowId)
    }
    // A group this window already has, from a worker that has since been
    // stopped: joined rather than a second one made beside it.
    const mine = await chrome.tabGroups.query({ windowId, title: 'TeaNode' }).catch(() => [])
    if (mine.length > 0) {
      await chrome.tabs.group({ tabIds: [tabId], groupId: mine[0].id })
      groups.set(windowId, mine[0].id)
      await rememberGroups()
      return mine[0].id
    }
    const groupId = await chrome.tabs.group({ tabIds: [tabId], createProperties: { windowId } })
    await chrome.tabGroups.update(groupId, { title: 'TeaNode', color: 'green' })
    groups.set(windowId, groupId)
    await rememberGroups()
    return groupId
  } catch (reason) {
    console.warn('[TeaNode] could not group the tab:', reason)
    return null
  }
}

// pickTab is the tab an action means: the number tabs gave, a piece of an
// address or a title, or the fallback when nothing was said. Only the
// person's own tab and the ones the agent opened count.
async function pickTab(args, fallback) {
  const own = attached.ownTabId === null ? null : await chrome.tabs.get(attached.ownTabId).catch(() => null)
  const candidates = [...(own ? [own] : []), ...(await agentTabs())]
  if (args.tab !== undefined && args.tab !== null && args.tab !== '') {
    const wanted = Number(args.tab)
    return candidates.some((tab) => tab.id === wanted) ? wanted : 0
  }
  const words = String(args.url || args.text || '').trim().toLowerCase()
  if (words) {
    const match = candidates.find((tab) => (tab.url || '').toLowerCase().includes(words) || (tab.title || '').toLowerCase().includes(words))
    return match ? match.id : 0
  }
  return candidates.some((tab) => tab.id === fallback) ? fallback : 0
}

// agentTabs are the tabs the agent opened, of those still open.
async function agentTabs() {
  const tabs = await chrome.tabs.query({})
  const listed = tabs.filter((tab) => opened.has(tab.id))
  for (const id of opened) {
    if (!tabs.some((tab) => tab.id === id)) opened.delete(id)
  }
  return listed
}

// The DevTools protocol, relayed. A page script can be told to click and
// type, but what it dispatches is not a real event: a site can tell, and
// some will not act on it. The protocol dispatches input the way the
// person's own mouse and keyboard do, and it is the only way to watch what
// a page asks the network for. It is the same protocol the headless
// browser beside the server speaks; here it speaks to the person's own
// tab, with their session.
//
// Attaching shows Chrome's own "is debugging this browser" bar, which is
// the person's sign that it is happening. It is attached on first use and
// let go when the tab goes, the socket closes, or nothing has used it for
// a while.
const debugging = new Map() // tabId -> { events: [], until }
// casting are the tabs streamed to the dashboard's browser page, each
// with the timer that sends a still picture while the stream is quiet.
const casting = new Map()
const DEBUGGER_PROTOCOL = '1.3'
const EVENTS_KEPT = 500
const DEBUGGER_IDLE = 10 * 60 * 1000

// What the protocol may be asked for, as a list of what is allowed.
//
// The line is not "the agent is trusted" -- it is their agent and their tab --
// but what attaching one tab is: everything that tab's own origin can reach,
// plus real input and what the page asks the network for. Anything that
// reaches past the tab into the browser is not that.
//
// Written as a list of refusals it kept losing, because the protocol grows
// every release and a refusal only covers what somebody thought of. Three
// methods with exactly the reach the list describes were outside it:
// Page.setDownloadBehavior -- the deprecated twin of the Browser. one, which
// was refused -- writes a file of the caller's choosing to a directory of its
// choosing, and Network.clearBrowserCookies, Network.clearBrowserCache and
// Storage.clearDataForOrigin are the whole browser rather than this page. A
// list of what is allowed cannot lose that way: something new is refused
// until somebody decides otherwise.
const CDP_ALLOWED = [
  // Real input, which is what driving a page is.
  'Input.',
  // Watching what the page asks the network for, and reading a response
  // back. Not a request's body (Network.getRequestPostData, and the body is
  // taken out of the events kept): a form's request is where a typed
  // password travels, and nothing the agent does needs it. Not
  // Network.setCookie and friends, which are every site's cookies, and not
  // the clearBrowser* pair, which are the whole browser.
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
  'Page.captureSnapshot',
  'Page.getLayoutMetrics',
  'Page.getFrameTree',
  'Page.getNavigationHistory',
  // Reading and running in the page's own world, which evaluate already is.
  'Runtime.',
  'DOM.',
  'DOMSnapshot.',
  'CSS.',
  'Accessibility.',
  'Emulation.setEmulatedMedia',
  'Log.',
]

function cdpRefusal(method, params) {
  const asked = String(method || '').trim()
  if (!/^[A-Z][A-Za-z]*\.[a-zA-Z][A-Za-z0-9]*$/.test(asked)) {
    return 'a method is written Domain.method, for example Input.dispatchMouseEvent'
  }
  // A javascript: address given to Page.navigate runs script in the document
  // the tab is showing (a filled one included), and Page.reload can be asked
  // to run a script on load. The navigate and open actions already take only
  // http and https addresses; the same holds here.
  if (asked === 'Page.navigate') {
    let address = null
    try {
      address = new URL(String(params && params.url))
    } catch {
      address = null
    }
    if (!address || (address.protocol !== 'http:' && address.protocol !== 'https:')) {
      return 'Page.navigate takes an http or https address in url'
    }
  }
  if (asked === 'Page.reload') {
    const extra = Object.keys(params || {}).filter((name) => name !== 'ignoreCache')
    if (extra.length > 0) return 'Page.reload takes only ignoreCache'
  }
  for (const allowed of CDP_ALLOWED) {
    const matches = allowed.endsWith('.') ? asked.startsWith(allowed) : asked === allowed
    if (matches) return ''
  }
  return `${asked} is not one of the methods an attached tab answers: driving it (Input.), reading it (DOM., Runtime., CSS., Accessibility.), watching what it fetches (Network.enable, Network.getResponseBody; never a request's body) and moving it (Page.navigate). Anything else reaches past this tab into the whole browser, which attaching one tab does not allow`
}

// agentUsing are the tabs whose debugger the agent's cdp action uses,
// marked before it waits for the attachment, so a stream's start or stop
// meanwhile never lets the debugger go from under it.
const agentUsing = new Set()

// attaching are the attachments under way, so that two callers at once,
// the agent and the dashboard's stream, share one rather than the second
// failing with Chrome's "another debugger is already attached".
const attaching = new Map()

async function debuggerFor(tabId) {
  const known = debugging.get(tabId)
  if (known) {
    known.until = Date.now() + DEBUGGER_IDLE
    return known
  }
  if (!attaching.has(tabId)) {
    attaching.set(
      tabId,
      chrome.debugger
        .attach({ tabId }, DEBUGGER_PROTOCOL)
        .then(() => {
          const state = { events: [], until: Date.now() + DEBUGGER_IDLE }
          debugging.set(tabId, state)
          return state
        })
        .finally(() => attaching.delete(tabId)),
    )
  }
  return attaching.get(tabId)
}

async function letDebuggerGo(tabId) {
  forgetCast(tabId)
  agentUsing.delete(tabId)
  if (!debugging.delete(tabId)) return
  await chrome.debugger.detach({ tabId }).catch(() => {})
}

function letEveryDebuggerGo() {
  // A stream whose attachment is still under way is forgotten too, so it
  // stops at its next step rather than running on after the socket.
  for (const tabId of [...casting.keys()]) forgetCast(tabId)
  for (const tabId of [...debugging.keys()]) void letDebuggerGo(tabId)
}

// What a page did, kept until it is asked for. The newest are kept: a
// page that makes a thousand requests should not push the worker over.
chrome.debugger.onEvent.addListener((source, method, params) => {
  // A picture for the dashboard is passed on, never kept with the page's
  // events.
  if (method === 'Page.screencastFrame') {
    if (casting.has(source.tabId)) sendFrame(source.tabId, params)
    return
  }
  const state = source.tabId !== undefined && debugging.get(source.tabId)
  if (!state) return
  // Nothing is kept of a document the agent filled a password or a code into
  // (a page logs, posts and echoes what it was given), and nothing of a
  // request's body in any.
  if (documents.isShowingFilledDocument(source.tabId)) return
  state.events.push({ at: Date.now(), method, params: withoutRequestBodies(params) })
  if (state.events.length > EVENTS_KEPT) state.events.splice(0, state.events.length - EVENTS_KEPT)
})

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId !== undefined) {
    debugging.delete(source.tabId)
    // Chrome let it go by itself (the person cancelled the bar, or opened
    // the developer tools on the tab): the dashboard is told rather than
    // left showing the last picture.
    if (forgetCast(source.tabId) && socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'castEnded', tab: source.tabId }))
    }
  }
})

// Streaming a tab to the dashboard's browser page, while somebody watches
// it there. Focus emulation keeps the tab painting when it is behind
// another tab or its window is minimized, which would otherwise stop the
// pictures: the person watching from elsewhere never has to change what
// this screen shows. The tabs being streamed are casting, beside
// debugging.
// FRAME_BACKLOG is how much may wait to go out on the socket before a
// picture is acknowledged: Chrome sends the next only after that, so a slow
// connection slows the stream instead of filling this worker's memory.
const FRAME_BACKLOG = 512 * 1024
// Chrome sends a screencast frame only when the tab repaints, so a still
// page sends nothing once the stream has started, and nothing at all when
// it started on a page that was not being painted. A screenshot is live
// whatever the tab's state, so one is sent as the first picture and again
// whenever the stream has been quiet this long; one that is the same as
// the last picture sent is not sent again.
const STILL_EVERY = 2000
// FRAME_LARGEST is the most a picture may be, as base64, under the
// server's limit for one message with room to spare; a larger one is not
// sent, rather than closing the connection the agent's actions use too.
const FRAME_LARGEST = 3 * 1024 * 1024

// startCast streams a tab. Every wait is a chance for a stop, or another
// start, to have come in meanwhile: each step checks this start is still
// the tab's before going on, and one that was overtaken undoes what it
// began unless a newer start has the tab.
async function startCast(tabId, options) {
  if (!isListed(tabId)) return
  // A stream started again keeps the size the last one was asked for.
  const previous = casting.get(tabId)
  forgetCast(tabId)
  const cast = {
    quality: options.quality || 60,
    maxSide: options.maxWidth || 1280,
    lastAt: 0,
    lastData: '',
    timer: null,
    emulation: previous ? previous.emulation : null,
    isReady: false,
  }
  casting.set(tabId, cast)
  const isOvertaken = () => casting.get(tabId) !== cast
  const undo = async () => {
    if (casting.has(tabId)) return
    await chrome.debugger.sendCommand({ tabId }, 'Page.stopScreencast').catch(() => {})
    await chrome.debugger.sendCommand({ tabId }, 'Emulation.clearDeviceMetricsOverride').catch(() => {})
    await chrome.debugger.sendCommand({ tabId }, 'Emulation.setFocusEmulationEnabled', { enabled: false }).catch(() => {})
    await releaseIfUnused(tabId)
  }
  try {
    await debuggerFor(tabId)
    if (isOvertaken()) return void (await releaseIfUnused(tabId))
    await chrome.debugger.sendCommand({ tabId }, 'Emulation.setFocusEmulationEnabled', { enabled: true })
    await chrome.debugger.sendCommand({ tabId }, 'Page.startScreencast', {
      format: 'jpeg',
      quality: cast.quality,
      maxWidth: cast.maxSide,
      maxHeight: options.maxHeight || 1280,
      everyNthFrame: 2,
    })
    if (isOvertaken()) return void (await undo())
    cast.isReady = true
    await applyEmulation(tabId, cast)
    if (isOvertaken()) return void (await undo())
    await sendStill(tabId)
    if (isOvertaken()) return void (await undo())
    cast.timer = setInterval(() => {
      if (Date.now() - cast.lastAt >= STILL_EVERY) void sendStill(tabId)
    }, STILL_EVERY)
  } catch (reason) {
    console.warn('[TeaNode] could not stream the tab:', reason)
    if (isOvertaken()) return
    // The server still thinks it streams, and the page waits for a
    // picture: it is told the stream ended, as Chrome ending it would,
    // before anything else can start one again.
    forgetCast(tabId)
    if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'castEnded', tab: tabId }))
    await releaseIfUnused(tabId)
  }
}

// releaseIfUnused lets the debugger go when neither a stream nor the agent
// needs it, and Chrome's bar with it.
async function releaseIfUnused(tabId) {
  if (debugging.has(tabId) && !agentUsing.has(tabId) && !casting.has(tabId)) await letDebuggerGo(tabId)
}

// emulate records the size of the phone the person holds the tab from, or
// that there is none, on the tab's stream, which lays the tab out at it so
// the picture is readable there, or as it is. A stream still starting lays
// it out once it is ready; one started again keeps what was asked. Handing
// back, the view closing and the stream ending all put the tab back, and
// Chrome puts it back by itself when the debugger goes.
async function emulate(message) {
  const tabId = message.tab
  const cast = casting.get(tabId)
  if (!cast || !isListed(tabId)) return
  cast.emulation = message.isOn && message.width > 0 && message.height > 0 ? { width: Math.round(message.width), height: Math.round(message.height) } : null
  if (cast.isReady) await applyEmulation(tabId, cast)
}

// applyEmulation lays the tab out as its stream asks: at a phone's size, or,
// asked for none, as it is, which is safe to ask for again.
async function applyEmulation(tabId, cast) {
  const asked = cast.emulation
  try {
    if (asked) {
      await chrome.debugger.sendCommand({ tabId }, 'Emulation.setDeviceMetricsOverride', {
        width: asked.width,
        height: asked.height,
        deviceScaleFactor: 0,
        mobile: true,
      })
    } else {
      await chrome.debugger.sendCommand({ tabId }, 'Emulation.clearDeviceMetricsOverride')
    }
  } catch (reason) {
    console.warn('[TeaNode] could not lay the tab out for the phone:', reason)
  }
  // Asked again while that was under way: what was asked last wins.
  if (casting.get(tabId) === cast && cast.emulation !== asked) await applyEmulation(tabId, cast)
}

// forgetCast stops treating a tab as streamed, and its stills with it.
function forgetCast(tabId) {
  const cast = casting.get(tabId)
  if (!cast) return false
  clearInterval(cast.timer)
  casting.delete(tabId)
  return true
}

async function stopCast(tabId) {
  if (!forgetCast(tabId)) return
  await chrome.debugger.sendCommand({ tabId }, 'Page.stopScreencast').catch(() => {})
  // A start that came in meanwhile owns the tab now, its layout, focus
  // emulation and the debugger with it.
  if (casting.has(tabId)) return
  // Laid out as it is whether or not a phone's size was applied: asking
  // for that again does nothing, and a size still being applied is undone.
  await chrome.debugger.sendCommand({ tabId }, 'Emulation.clearDeviceMetricsOverride').catch(() => {})
  await chrome.debugger.sendCommand({ tabId }, 'Emulation.setFocusEmulationEnabled', { enabled: false }).catch(() => {})
  // Attached only for the dashboard: let it go, and Chrome's bar with it.
  await releaseIfUnused(tabId)
}

// sendStill sends a screenshot of the tab as a picture, unless it is what
// was sent last or the socket is backed up.
async function sendStill(tabId) {
  const cast = casting.get(tabId)
  const sending = socket
  if (!cast || !sending || sending.readyState !== WebSocket.OPEN || sending.bufferedAmount > FRAME_BACKLOG) return
  try {
    // The viewport in CSS pixels, and the picture scaled so its longer
    // side is no more than the stream's: a screenshot is taken in device
    // pixels, which on a large high-density screen is several times that.
    const metrics = await chrome.debugger.sendCommand({ tabId }, 'Page.getLayoutMetrics')
    const viewport = (metrics && metrics.cssVisualViewport) || {}
    const width = viewport.clientWidth || 0
    const height = viewport.clientHeight || 0
    if (!width || !height) return
    const density = metrics.visualViewport && metrics.visualViewport.clientWidth ? metrics.visualViewport.clientWidth / width : 1
    const scale = Math.min(1, cast.maxSide / (Math.max(width, height) * Math.max(density, 1)))
    const shot = await chrome.debugger.sendCommand({ tabId }, 'Page.captureScreenshot', {
      format: 'jpeg',
      quality: cast.quality,
      clip: { x: viewport.pageX || 0, y: viewport.pageY || 0, width, height, scale },
    })
    cast.lastAt = Date.now()
    if (!shot || shot.data === cast.lastData || casting.get(tabId) !== cast || shot.data.length > FRAME_LARGEST) return
    cast.lastData = shot.data
    sending.send(JSON.stringify({ type: 'frame', tab: tabId, data: shot.data, metadata: { deviceWidth: width, deviceHeight: height } }))
  } catch (reason) {
    console.warn('[TeaNode] could not photograph the tab:', reason)
  }
}

function sendFrame(tabId, params) {
  const sending = socket
  if (!sending || sending.readyState !== WebSocket.OPEN) return
  const cast = casting.get(tabId)
  if (cast) {
    cast.lastAt = Date.now()
    cast.lastData = ''
  }
  if (params.data.length <= FRAME_LARGEST) {
    sending.send(JSON.stringify({ type: 'frame', tab: tabId, data: params.data, metadata: params.metadata }))
  }
  const acknowledge = () => {
    if (!casting.has(tabId)) return
    if (sending.readyState === WebSocket.OPEN && sending.bufferedAmount > FRAME_BACKLOG) {
      setTimeout(acknowledge, 50)
      return
    }
    chrome.debugger.sendCommand({ tabId }, 'Page.screencastFrameAck', { sessionId: params.sessionId }).catch(() => {})
  }
  acknowledge()
}

// What the person does in the picture of a tab they took over, as the
// DevTools protocol takes it: the server passes it on only while they hold
// the tab, and only for a tab being watched.
const INPUT_TYPES = new Set(['pointer', 'wheel', 'key', 'text'])
const POINTER_EVENTS = { down: 'mousePressed', up: 'mouseReleased', move: 'mouseMoved' }
const BUTTONS_HELD = { left: 1, right: 2, middle: 4 }

async function dispatchInput(message) {
  const tabId = message.tab
  if (!casting.has(tabId) || !isListed(tabId)) return
  const target = { tabId }
  const modifiers = Number(message.modifiers) || 0
  try {
    if (message.type === 'pointer') {
      const type = POINTER_EVENTS[message.event]
      if (!type) return
      const button = BUTTONS_HELD[message.button] ? message.button : 'none'
      await chrome.debugger.sendCommand(target, 'Input.dispatchMouseEvent', {
        type,
        x: Number(message.x) || 0,
        y: Number(message.y) || 0,
        button,
        buttons: BUTTONS_HELD[button] || 0,
        clickCount: type === 'mouseMoved' ? 0 : Number(message.clickCount) || 1,
        modifiers,
      })
    } else if (message.type === 'wheel') {
      await chrome.debugger.sendCommand(target, 'Input.dispatchMouseEvent', {
        type: 'mouseWheel',
        x: Number(message.x) || 0,
        y: Number(message.y) || 0,
        deltaX: Number(message.deltaX) || 0,
        deltaY: Number(message.deltaY) || 0,
        modifiers,
      })
    } else if (message.type === 'key') {
      const text = typeof message.text === 'string' ? message.text : ''
      const keyCode = Number(message.keyCode) || 0
      await chrome.debugger.sendCommand(target, 'Input.dispatchKeyEvent', {
        // A key that types something goes down as keyDown with its text;
        // one that does not (an arrow, Backspace) as rawKeyDown.
        type: message.event === 'up' ? 'keyUp' : text ? 'keyDown' : 'rawKeyDown',
        key: String(message.key || ''),
        code: String(message.code || ''),
        windowsVirtualKeyCode: keyCode,
        nativeVirtualKeyCode: keyCode,
        text: message.event === 'up' ? '' : text,
        unmodifiedText: message.event === 'up' ? '' : text,
        modifiers,
      })
    } else if (message.type === 'text') {
      const text = typeof message.text === 'string' ? message.text : ''
      if (text) await chrome.debugger.sendCommand(target, 'Input.insertText', { text })
    }
  } catch (reason) {
    console.warn('[TeaNode] could not pass the input on:', reason)
  }
}

// stopWatchingNetwork turns off what the debugger keeps of the tab's network
// events and forgets what it kept, before the agent fills a password or a code
// into the tab.
async function stopWatchingNetwork(tabId) {
  const state = debugging.get(tabId)
  if (!state) return
  await chrome.debugger.sendCommand({ tabId }, 'Network.disable').catch(() => {})
  state.events = []
}

// act does one action in the attached tab: navigation here, everything
// else in the page through the content script; fetch through the tab's
// own session.
async function act(action, args) {
  await restored
  try {
    if (action === 'open') {
      if (!/^https?:\/\//i.test(args.url || '')) return { ok: false, error: 'only http and https addresses' }
      // Beside the tab the actions go to, or, with none, in the window the
      // person last used; a browser with no window gets one.
      const beside = attached.tabId === null ? null : await chrome.tabs.get(attached.tabId).catch(() => null)
      let windowId = beside ? beside.windowId : null
      if (windowId === null) {
        const window = await chrome.windows.getLastFocused({ windowTypes: ['normal'] }).catch(() => null)
        windowId = window ? window.id : (await chrome.windows.create({ focused: false })).id
      }
      const made = await chrome.tabs.create({ url: args.url, windowId, active: true })
      opened.add(made.id)
      await rememberOpened()
      await groupFor(windowId, made.id)
      await useTab(made.id)
      await waitForLoad(made.id)
      return { ok: true, data: await current() }
    }
    if (action === 'tabs') {
      return { ok: true, data: { tabs: await listTabs() } }
    }
    if (attached.tabId === null) return { ok: false, error: 'no tab is open for you in this browser yet; open one first' }
    const tab = await chrome.tabs.get(attached.tabId)
    // The server checked a lock on one tab before it sent this; if the actions
    // go to another by now, it is not done there.
    if (args.expectedTab !== undefined && args.expectedTab !== null && args.expectedTab !== '' && String(args.expectedTab) !== String(tab.id)) {
      return { ok: false, error: 'the tab the actions go to has changed since this was asked for; call tabs to see which it is now' }
    }
    // A tab that shows a document the agent filled a password or code into is
    // not read back, whatever the server has been told yet: a page restored
    // from the history's cache is that same document.
    const isFilledDocument = documents.isShowingFilledDocument(tab.id)
    if (isFilledDocument) {
      const lockedRefusal = lockedActionRefusal(action, args)
      if (lockedRefusal) return { ok: false, error: lockedRefusal }
    }
    if (action === 'navigate') {
      if (!/^https?:\/\//i.test(args.url || '')) return { ok: false, error: 'only http and https addresses' }
      await chrome.tabs.update(tab.id, { url: args.url })
      await waitForLoad(tab.id)
      const updated = await chrome.tabs.get(tab.id)
      attached.title = updated.title || ''
      attached.url = updated.url || ''
      tellServer()
      return { ok: true, data: { url: attached.url, title: attached.title } }
    }
    if (action === 'switch') {
      // By number from tabs, by a piece of its address, or, given nothing,
      // back to the person's own tab.
      const wanted = await pickTab(args, attached.ownTabId)
      if (!wanted) return { ok: false, error: 'not a tab of this conversation: the person\'s own, or one you opened; tabs lists them' }
      await chrome.tabs.update(wanted, { active: true })
      await useTab(wanted)
      return { ok: true, data: await current() }
    }
    if (action === 'close') {
      // By number, by a piece of its address, or, given nothing, the tab
      // the actions go to; never the person's own.
      const mine = await agentTabs()
      const last = mine.length > 0 ? mine[mine.length - 1].id : 0
      const wanted = await pickTab(args, attached.tabId !== attached.ownTabId ? attached.tabId : last)
      if (!wanted || wanted === attached.ownTabId) return { ok: false, error: "the person's own tab is theirs to close; name one you opened, from tabs" }
      await chrome.tabs.remove(wanted)
      opened.delete(wanted)
      await rememberOpened()
      if (attached.tabId === wanted) {
        // Back to the person's own tab, or the last one opened, or none.
        const left = await agentTabs()
        await useTab(attached.ownTabId !== null ? attached.ownTabId : left.length > 0 ? left[left.length - 1].id : null)
        if (attached.tabId !== null) await chrome.tabs.update(attached.tabId, { active: true })
      }
      return { ok: true, data: await current() }
    }
    if (action === 'back') {
      await chrome.tabs.goBack(tab.id)
      await waitForLoad(tab.id)
      const updated = await chrome.tabs.get(tab.id)
      return { ok: true, data: { url: updated.url, title: updated.title } }
    }
    if (action === 'screenshot') {
      // captureVisibleTab takes the window's active tab, whichever that
      // is. Without this it photographed whatever the person had switched
      // to -- their bank, their inbox -- and sent it to the server.
      if (!tab.active) {
        return { ok: false, error: 'the tab to photograph is not the one in front; switch to it first, or take a snapshot instead' }
      }
      const image = await chrome.tabs.captureVisibleTab(tab.windowId, { format: 'png' })
      return { ok: true, data: image }
    }
    if (action === 'cdp') {
      if (!args.method) return { ok: false, error: 'a method is needed, for example Input.dispatchMouseEvent or Network.enable' }
      const refusal = cdpRefusal(args.method, args.params)
      if (refusal) return { ok: false, error: refusal }
      if (isFilledDocument) {
        const lockedRefusal = lockedMethodRefusal(args.method, args.params)
        if (lockedRefusal) return { ok: false, error: lockedRefusal }
      }
      agentUsing.add(tab.id)
      await debuggerFor(tab.id)
      const answer = await chrome.debugger.sendCommand({ tabId: tab.id }, args.method, args.params || {})
      return { ok: true, data: { result: answer === undefined ? null : answer } }
    }
    if (action === 'cdp_events') {
      const state = debugging.get(tab.id)
      if (!state) return { ok: false, error: 'nothing is being watched on this tab; send a cdp command such as Network.enable first' }
      state.until = Date.now() + DEBUGGER_IDLE
      const wanted = String(args.method || '').trim()
      const kept = wanted ? state.events.filter((event) => event.method.startsWith(wanted)) : state.events
      const most = Number(args.limit) > 0 ? Number(args.limit) : 100
      const shown = kept.slice(-most)
      if (args.forget) state.events = []
      return { ok: true, data: { events: shown, kept: state.events.length } }
    }
    if (action === 'cdp_stop') {
      // Somebody is watching the tab on the dashboard, which needs the
      // protocol: what the agent kept is let go, the attachment is not.
      if (casting.has(tab.id)) {
        const state = debugging.get(tab.id)
        if (state) state.events = []
        agentUsing.delete(tab.id)
        // What the agent may have turned on to watch is turned off; the
        // stream does not need it.
        await chrome.debugger.sendCommand({ tabId: tab.id }, 'Network.disable').catch(() => {})
        return { ok: true, data: { stopped: true } }
      }
      await letDebuggerGo(tab.id)
      return { ok: true, data: { stopped: true } }
    }
    // The server builds these arguments after checking the item is one the
    // person let the agent use and that the tab is on one of its sites; the
    // extension checks the tab's address again and never echoes a value.
    if (action === 'fill_login' || action === 'fill_one_time_code') {
      // What the page asks of the network is not kept from here on, and what
      // was kept before is let go: a form's request carries the password.
      await stopWatchingNetwork(tab.id)
      const answer = await (action === 'fill_login' ? fillLoginForAgent(tab.id, args) : fillOneTimeCodeForAgent(tab.id, args))
      if (!answer.ok) return answer
      // Which document it went into, and where the tab's commits stand, for
      // the server's lock.
      documents.markFilled(tab.id, answer.data.documentId)
      await rememberDocuments().catch(() => {})
      return { ok: true, data: { ...answer.data, tab: tab.id, commitCount: documents.countOf(tab.id) } }
    }
    const [result] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: inPage,
      // Whether the tab shows a filled document is the worker's to say, never
      // the caller's: it is set after whatever came with the action.
      args: [action, { ...args, isFilledDocument }],
    })
    return result?.result || { ok: false, error: 'the page did not answer' }
  } catch (error) {
    return { ok: false, error: String(error && error.message ? error.message : error) }
  }
}

function waitForLoad(tabId) {
  return new Promise((resolve) => {
    // Already there: a page that loaded before anybody looked.
    chrome.tabs.get(tabId).then((tab) => {
      if (tab && tab.status === 'complete') {
        chrome.tabs.onUpdated.removeListener(done)
        resolve()
      }
    }).catch(() => {})
    const done = (id, info) => {
      if (id === tabId && info.status === 'complete') {
        chrome.tabs.onUpdated.removeListener(done)
        resolve()
      }
    }
    chrome.tabs.onUpdated.addListener(done)
    setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(done)
      resolve()
    }, 30000)
  })
}

// The panel's script runs inside the page the person pressed the button on:
// it is a content script, not a page of the extension, so what it asks of this
// worker is not told from what it says. It is told by where it comes from and
// when: the worker itself put the script into that tab, and only just, when
// the button was pressed. The token goes to a tab only in the few seconds after
// that, once; the other requests (attach, detach, the place the panel was left
// at) only from a tab that has the panel in its current page. Any other
// content script, and any frame of a page, is refused.
const PANEL_TOKEN_GRANT_MILLISECONDS = 10000
const PANEL_STORAGE_KEY = 'panelTabs'

async function readPanelTabs() {
  const stored = (await chrome.storage.session.get(PANEL_STORAGE_KEY))[PANEL_STORAGE_KEY]
  return {
    tabs: stored && Array.isArray(stored.tabs) ? stored.tabs.filter((id) => typeof id === 'number') : [],
    grants: stored && stored.grants && typeof stored.grants === 'object' ? stored.grants : {},
  }
}

async function writePanelTabs(panel) {
  await chrome.storage.session.set({ [PANEL_STORAGE_KEY]: panel })
}

async function markPanelPressed(tabId) {
  const panel = await readPanelTabs()
  if (!panel.tabs.includes(tabId)) panel.tabs.push(tabId)
  panel.grants[tabId] = Date.now()
  await writePanelTabs(panel)
}

async function forgetPanelTab(tabId) {
  const panel = await readPanelTabs()
  if (!panel.tabs.includes(tabId) && !(tabId in panel.grants)) return
  panel.tabs = panel.tabs.filter((id) => id !== tabId)
  delete panel.grants[tabId]
  await writePanelTabs(panel)
}

// isPanelSender says whether a message is from the panel script in the top
// frame of a tab that has it. With wantsToken it must also be within the few
// seconds after the button was pressed, and the grant is used up.
async function isPanelSender(sender, wantsToken) {
  if (!sender || sender.id !== chrome.runtime.id || !sender.tab || typeof sender.tab.id !== 'number') return false
  if (sender.frameId !== undefined && sender.frameId !== 0) return false
  const panel = await readPanelTabs()
  if (!panel.tabs.includes(sender.tab.id)) return false
  if (!wantsToken) return true
  const pressedAt = panel.grants[sender.tab.id]
  if (typeof pressedAt !== 'number' || Date.now() - pressedAt > PANEL_TOKEN_GRANT_MILLISECONDS) return false
  delete panel.grants[sender.tab.id]
  await writePanelTabs(panel)
  return true
}

// The button opens the agent's panel on the page: the dashboard's own
// drawer, framed, or the dashboard's drawer itself when the page is the
// dashboard. Where nothing can be put on the page, the options open.
chrome.action.onClicked.addListener(async (tab) => {
  try {
    await markPanelPressed(tab.id)
    await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['panel.js'] })
  } catch {
    await forgetPanelTab(tab.id).catch(() => {})
    chrome.runtime.openOptionsPage()
  }
})

// A tab that loads a new page no longer has the panel in it.
chrome.tabs.onUpdated.addListener((tabId, info) => {
  if (info.status === 'loading') void forgetPanelTab(tabId).catch(() => {})
})

const PANEL_GEOMETRY_PREFIX = 'panel:geometry:'

function panelGeometryKey(sender) {
  try {
    return PANEL_GEOMETRY_PREFIX + new URL(sender.url).origin
  } catch {
    return null
  }
}

function isPanelGeometry(value) {
  return (
    !!value &&
    ['across', 'down', 'width', 'height'].every((name) => typeof value[name] === 'number' && Number.isFinite(value[name]))
  )
}

// What the panel asks of this worker.
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  const type = message && message.type
  if (typeof type !== 'string' || !type.startsWith('panel:')) return false
  const tab = sender.tab
  isPanelSender(sender, type === 'panel:token').then(async (isAllowed) => {
    if (!isAllowed) return sendResponse({ error: 'Not allowed from here.' })
    switch (type) {
      case 'panel:token': {
        const [server, secret, { username }] = await Promise.all([
          serverOrigin(),
          token(),
          chrome.storage.local.get('username'),
        ])
        return sendResponse({ server, token: secret, username: username || '', attached: attached.ownTabId === tab.id })
      }
      case 'panel:options':
        chrome.runtime.openOptionsPage()
        return sendResponse({})
      case 'panel:attach':
        await attach(tab)
        return sendResponse({ attached: attached.ownTabId === tab.id })
      case 'panel:detach':
        await detach()
        return sendResponse({ attached: false })
      case 'panel:geometry-read': {
        const key = panelGeometryKey(sender)
        const stored = key ? (await chrome.storage.local.get(key))[key] : null
        return sendResponse({ geometry: isPanelGeometry(stored) ? stored : null })
      }
      case 'panel:geometry-write': {
        const key = panelGeometryKey(sender)
        if (key && isPanelGeometry(message.geometry)) {
          const { across, down, width, height } = message.geometry
          await chrome.storage.local.set({ [key]: { across, down, width, height } })
        }
        return sendResponse({})
      }
    }
    return sendResponse({ error: 'Unknown request.' })
  }).catch(() => sendResponse({ error: 'Something went wrong.' }))
  return true
})

chrome.tabs.onUpdated.addListener((tabId, info, tab) => {
  if (attached.tabId === tabId && info.status === 'complete') {
    attached.title = tab.title || ''
    attached.url = tab.url || ''
    tellServer()
  } else if (isListed(tabId) && (info.title !== undefined || info.url !== undefined || info.status === 'complete')) {
    // The dashboard's list of tabs to watch names each by its title.
    tellSoon()
  }
})

// A tab committing a document is what ends the server's lock on a tab the
// agent filled a password into. Only the main frame counts, and not a change
// that keeps the document (history.pushState, a fragment).
if (chrome.webNavigation) {
  chrome.webNavigation.onCommitted.addListener(async (details) => {
    // A commit that wakes the worker waits for what it kept (the tabs the
    // agent may act in, the documents it filled) to be read back first.
    await restored.catch(() => {})
    const report = documents.committed(details, isListed(details.tabId))
    void rememberDocuments().catch(() => {})
    if (report) tellServer()
  })
}

chrome.tabs.onRemoved.addListener((tabId) => {
  void forgetPanelTab(tabId).catch(() => {})
  forgetCast(tabId)
  documents.forget(tabId)
  // A worker that was asleep reads back what it kept, which still holds this
  // tab: it is forgotten again once that is read.
  void restored.catch(() => {}).then(() => {
    documents.forget(tabId)
    return rememberDocuments()
  }).catch(() => {})
  if (opened.has(tabId)) tellSoon()
  if (opened.delete(tabId)) rememberOpened()
  // Their own tab, closed: it is detached, and the connection stays.
  if (attached.ownTabId === tabId) {
    void detach()
    return
  }
  // A tab the agent opened, closed by the person: back to their own, or to
  // the last one it opened, or to none.
  if (attached.tabId === tabId) {
    void agentTabs().then(async (left) => {
      await useTab(attached.ownTabId !== null ? attached.ownTabId : left.length > 0 ? left[left.length - 1].id : null)
      return current()
    }).catch(() => {})
  }
})

// act is exported for the tests, which drive it as the socket does.
export { act }
