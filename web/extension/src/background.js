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

import {
  OWN_PAGES_REFUSAL,
  PANEL_ELEMENT_REFUSAL,
  addressRefusal,
  cdpRefusal,
  isOwnPage,
  pageRefusal,
  panelProbe,
  panelRefusal,
  serverHostname,
} from './reach.js'

const PROTOCOL = 4

let socket = null
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
const restored = chrome.storage.session.get(['opened', 'groups', 'ownTabId', 'tabId']).then(async ({ opened: kept, groups: keptGroups, ownTabId, tabId }) => {
  for (const id of kept || []) opened.add(id)
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
      if (!origin || !secret || !/^https?:\/\//i.test(origin)) return
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
  if (!origin || !secret || !/^https?:\/\//i.test(origin)) {
    // Typed without a scheme, the address builds a WebSocket URL that
    // throws, and the button did nothing with nothing said.
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
  const update = { type: 'update', title: attached.title, url: attached.url, hasTab: attached.tabId !== null }
  void listTabs()
    .catch(() => undefined)
    .then((tabs) => {
      if (sending.readyState === WebSocket.OPEN) sending.send(JSON.stringify({ ...update, ...(tabs ? { tabs } : {}) }))
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
    opening.send(JSON.stringify({ type: 'hello', protocol: PROTOCOL, token: secret, title: attached.title, url: attached.url, hasTab: attached.tabId !== null, tabs }))
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

// What the protocol may be asked for is a list of exact method names, and
// what the agent may reach in the browser, TeaNode's own pages included, is
// decided in reach.js; the server holds the same lines and refuses first.

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
  state.events.push({ at: Date.now(), method, params })
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

// panelIn is where the person's TeaNode panel is in a tab now, read from the
// page: see panelProbe. A page the extension cannot run a script in cannot
// have the panel on it either, since the panel is put there by a script.
async function panelIn(tabId) {
  try {
    const [probed] = await chrome.scripting.executeScript({ target: { tabId }, func: panelProbe })
    return (probed && probed.result) || { isOpen: false }
  } catch {
    return { isOpen: false }
  }
}

// act does one action in the attached tab: navigation here, everything
// else in the page through the content script; fetch through the tab's
// own session.
async function act(action, args) {
  await restored
  try {
    // Where TeaNode is: its pages, and the extension's own, are not driven
    // in the person's browser, whatever the address the agent was given.
    const origin = await serverOrigin()
    if (action === 'open') {
      const unwelcome = addressRefusal(args.url, origin)
      if (unwelcome) return { ok: false, error: unwelcome }
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
    // Where the tab is now, not where it was when it was attached or last
    // reported: a page may have gone to TeaNode since.
    const unreachable = pageRefusal(action, tab, origin)
    if (unreachable) return { ok: false, error: unreachable }
    if (action === 'navigate') {
      const unwelcome = addressRefusal(args.url, origin)
      if (unwelcome) return { ok: false, error: unwelcome }
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
      const chosen = await chrome.tabs.get(wanted).catch(() => null)
      if (chosen && (isOwnPage(chosen.url, origin) || isOwnPage(chosen.pendingUrl, origin))) return { ok: false, error: OWN_PAGES_REFUSAL }
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
      const refusal = cdpRefusal(args.method)
      if (refusal) return { ok: false, error: refusal }
      // Page.navigate is the navigate action by another name.
      if (args.method === 'Page.navigate') {
        const unwelcome = addressRefusal(args.params && args.params.url, origin)
        if (unwelcome) return { ok: false, error: unwelcome }
      }
      agentUsing.add(tab.id)
      await debuggerFor(tab.id)
      // Attaching takes a moment, in which the page may have gone to TeaNode.
      const unreachableNow = pageRefusal(action, await chrome.tabs.get(tab.id), origin)
      if (unreachableNow) return { ok: false, error: unreachableNow }
      // Input dispatched where the panel is would be a click in TeaNode
      // itself, as the person's hand. Read as late as it can be.
      if (args.method.startsWith('Input.')) {
        const covered = panelRefusal(args.method, args.params, await panelIn(tab.id))
        if (covered) return { ok: false, error: covered }
      }
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
    // An element the panel covers is not clicked, hovered, typed into or
    // chosen in: the person would see it happen under the panel.
    const panel = ['click', 'hover', 'type', 'select'].includes(action) ? await panelIn(tab.id) : { isOpen: false }
    const [result] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: inPage,
      args: [action, args, serverHostname(origin), OWN_PAGES_REFUSAL, panel, PANEL_ELEMENT_REFUSAL],
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

// inPage runs inside the page. It is self-contained: nothing from this
// file is in scope there.
async function inPage(action, args, ownHostname, ownPagesRefusal, panel, panelWords) {
  // The check that was made on the tab's address a moment ago, made again
  // where the script runs: a page can go to TeaNode in between.
  if (/^(chrome|moz)-extension:$/.test(location.protocol) || (ownHostname && location.hostname.replace(/\.$/, '').toLowerCase() === ownHostname)) {
    return { ok: false, error: ownPagesRefusal }
  }
  // Whether an element is under the person's TeaNode panel, which is where
  // the agent does not act.
  const isUnderPanel = (node) => {
    if (!panel || !panel.isOpen) return false
    const box = node.getBoundingClientRect()
    return box.left <= panel.right && box.right >= panel.left && box.top <= panel.bottom && box.bottom >= panel.top
  }
  const refs = (window.__teanodeRefs = window.__teanodeRefs || new Map())
  let next = window.__teanodeNextRef || 1
  const interactive = new Set(['a', 'button', 'input', 'select', 'textarea', 'summary', 'option', 'label'])
  const find = () => {
    // A ref comes back through a tool call, where it may be a string.
    if (args.ref) return refs.get(Number(args.ref)) || refs.get(args.ref) || null
    if (args.selector) return document.querySelector(args.selector)
    return null
  }
  // Which fields hold a secret. It decides what a snapshot says, not what
  // may be typed: a password the person has already filled in is not read
  // back into the conversation, where it would be kept and sent onward.
  // Typing into one is their agent's business and goes ahead.
  const sensitive = (node) => {
    const type = (node.type || '').toLowerCase()
    const hint = ((node.autocomplete || '') + ' ' + (node.name || '') + ' ' + (node.id || '') + ' ' + (node.getAttribute('aria-label') || '')).toLowerCase()
    return type === 'password' || /cc-|card|cvc|cvv|iban|account-?number|routing|ssn|passport/.test(hint)
  }
  const nameOf = (node) => {
    const label = node.getAttribute && (node.getAttribute('aria-label') || node.getAttribute('title') || node.getAttribute('placeholder') || node.getAttribute('name'))
    if (label) return label.trim()
    return ((node.innerText || node.value || node.textContent || '').trim().replace(/\s+/g, ' ')).slice(0, 120)
  }
  const refFor = (node) => {
    for (const [ref, candidate] of refs) if (candidate === node) return ref
    const ref = next++
    refs.set(ref, node)
    window.__teanodeNextRef = next
    return ref
  }
  switch (action) {
    case 'snapshot': {
      const maximum = args.max_characters || 20000
      if (args.mode === 'text') {
        const text = document.body ? document.body.innerText : ''
        return { ok: true, data: { title: document.title, url: location.href, text: text.slice(0, maximum), truncated: text.length > maximum } }
      }
      const lines = []
      let total = 0
      const walk = (node, depth) => {
        if (total > maximum) return
        if (node.nodeType === Node.TEXT_NODE) {
          const text = node.textContent.trim().replace(/\s+/g, ' ')
          if (text) {
            lines.push('  '.repeat(depth) + text.slice(0, 200))
            total += text.length
          }
          return
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return
        const tag = node.tagName.toLowerCase()
        if (['script', 'style', 'noscript', 'svg', 'template', 'head'].includes(tag)) return
        const style = window.getComputedStyle(node)
        if (style.display === 'none' || style.visibility === 'hidden') return
        if (interactive.has(tag) || (node.getAttribute('role') && /button|link|checkbox|radio|textbox|combobox|tab|menuitem/.test(node.getAttribute('role')))) {
          const ref = refFor(node)
          let description = node.getAttribute('role') || tag
          if (tag === 'input') description = 'input(' + (node.type || 'text') + ')'
          let extra = ''
          if (tag === 'a' && node.getAttribute('href')) extra = ' href=' + node.getAttribute('href')
          if ((tag === 'input' || tag === 'textarea') && node.value && !sensitive(node)) extra = ' value=' + JSON.stringify(node.value.slice(0, 80))
          if (sensitive(node)) extra = ' (holds a secret)'
          lines.push('  '.repeat(depth) + '[ref=' + ref + '] ' + description + ' "' + nameOf(node) + '"' + extra)
          total += 40
          if (tag === 'select') {
            for (const option of node.options) lines.push('  '.repeat(depth + 1) + '- option ' + JSON.stringify(option.text) + (option.selected ? ' (selected)' : ''))
          }
          if (['a', 'button', 'label', 'option', 'select'].includes(tag)) return
        } else if (/^h[1-6]$/.test(tag)) {
          lines.push('  '.repeat(depth) + tag + ': ' + (node.innerText || '').trim().slice(0, 200))
          return
        }
        for (const child of node.childNodes) walk(child, depth + (['div', 'span', 'section', 'main', 'article', 'body', 'html', 'p'].includes(tag) ? 0 : 1))
      }
      walk(document.documentElement, 0)
      return { ok: true, data: { title: document.title, url: location.href, text: lines.join('\n'), truncated: total > maximum } }
    }
    case 'click': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
      node.scrollIntoView({ block: 'center' })
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      node.click()
      await new Promise((resolve) => setTimeout(resolve, 300))
      return { ok: true, data: { clicked: nameOf(node), now: { url: location.href, title: document.title } } }
    }
    case 'hover': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      node.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      return { ok: true, data: { hovered: nameOf(node) } }
    }
    case 'type': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      node.focus()
      if (args.clear_first) node.value = ''
      if ('value' in node) {
        node.value = (node.value || '') + (args.text || '')
        node.dispatchEvent(new Event('input', { bubbles: true }))
        node.dispatchEvent(new Event('change', { bubbles: true }))
      } else if (node.isContentEditable) {
        document.execCommand('insertText', false, args.text || '')
      }
      if (args.submit && node.form) {
        node.form.requestSubmit ? node.form.requestSubmit() : node.form.submit()
      }
      return { ok: true, data: { typed: (args.text || '').length } }
    }
    case 'select': {
      const node = find()
      if (!node || node.tagName.toLowerCase() !== 'select') return { ok: false, error: 'not a select' }
      if (isUnderPanel(node)) return { ok: false, error: panelWords }
      let picked = null
      for (const option of node.options) if (option.value === args.value || option.text.trim() === args.value) picked = option
      if (!picked && typeof args.index === 'number') picked = node.options[args.index]
      if (!picked) return { ok: false, error: 'no such option' }
      node.value = picked.value
      node.dispatchEvent(new Event('change', { bubbles: true }))
      return { ok: true, data: { selected: picked.text } }
    }
    case 'press': {
      const active = document.activeElement || document.body
      for (const kind of ['keydown', 'keypress', 'keyup']) active.dispatchEvent(new KeyboardEvent(kind, { key: args.key, bubbles: true }))
      if (args.key === 'Enter' && active.form) active.form.requestSubmit ? active.form.requestSubmit() : active.form.submit()
      return { ok: true, data: { pressed: args.key } }
    }
    case 'scroll': {
      const node = find() || window
      const height = (node === window ? window.innerHeight : node.clientHeight) * (args.amount || 1) * (args.direction === 'up' ? -1 : 1)
      node.scrollBy({ top: height })
      return { ok: true, data: { scrolled: args.direction || 'down' } }
    }
    case 'wait': {
      const timeout = args.timeout_ms || 30000
      const started = Date.now()
      while (Date.now() - started < timeout) {
        if (args.for === 'selector' && document.querySelector(args.selector)) return { ok: true, data: { ended_by: 'selector' } }
        if ((args.for === 'navigation' || !args.for) && document.readyState === 'complete') return { ok: true, data: { ended_by: 'load' } }
        if (args.for === 'timeout') {
        // Waiting out the whole time is the point of asking for it.
        await new Promise((resolve) => setTimeout(resolve, Math.min(250, timeout)))
        continue
      }
        await new Promise((resolve) => setTimeout(resolve, 250))
      }
      return { ok: true, data: { ended_by: 'timeout' } }
    }
    case 'evaluate': {
      try {
        const value = await Promise.resolve(new Function('return (' + args.expression + ')')())
        return { ok: true, data: JSON.parse(JSON.stringify(value === undefined ? null : value)) }
      } catch (error) {
        return { ok: false, error: String(error) }
      }
    }
    case 'fetch': {
      // With the tab's own session: the primitive for "download my
      // statement". Same origin only, so a page cannot be used to reach
      // another site the person is signed into.
      try {
        const target = new URL(args.url || '', location.href)
        if (target.origin !== location.origin) return { ok: false, error: 'fetch is limited to the attached page’s own site' }
        // Following a redirect would take the person's cookies off this
      // site: any open redirect on it would otherwise read any site they
      // are signed into.
      const response = await fetch(target.toString(), { credentials: 'include', redirect: 'manual' })
      if (response.type === 'opaqueredirect' || (response.status >= 300 && response.status < 400)) {
        return { ok: false, error: 'that address redirects off this site, which is not followed with the page\'s session' }
      }
        const type = response.headers.get('content-type') || ''
        if (/^text\/|json|xml/.test(type)) {
          const text = await response.text()
          return { ok: true, data: { status: response.status, content_type: type, text: text.slice(0, args.max_characters || 20000) } }
        }
        const bytes = await response.arrayBuffer()
        if (bytes.byteLength > 5 * 1024 * 1024) return { ok: false, error: 'the file is over five megabytes' }
        let binary = ''
        for (const byte of new Uint8Array(bytes)) binary += String.fromCharCode(byte)
        return { ok: true, data: { status: response.status, content_type: type, base64: btoa(binary), bytes: bytes.byteLength } }
      } catch (error) {
        return { ok: false, error: String(error) }
      }
    }
    case 'storage': {
      // Only the attached site's own storage, and never another's.
      const entries = {}
      for (let index = 0; index < localStorage.length; index++) {
        const key = localStorage.key(index)
        entries[key] = (localStorage.getItem(key) || '').slice(0, 500)
      }
      return { ok: true, data: { origin: location.origin, localStorage: entries } }
    }
  }
  return { ok: false, error: 'unknown action ' + action }
}

// The button opens the agent's panel on the page: the dashboard's own
// drawer, framed, or the dashboard's drawer itself when the page is the
// dashboard. Where nothing can be put on the page, the options open.
chrome.action.onClicked.addListener(async (tab) => {
  try {
    await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['panel.js'] })
  } catch {
    chrome.runtime.openOptionsPage()
  }
})

// What the panel asks of this worker.
chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  const tab = sender.tab
  switch (message && message.type) {
    case 'panel:token':
      Promise.all([serverOrigin(), token(), chrome.storage.local.get('username')]).then(([server, secret, { username }]) => {
        sendResponse({ server, token: secret, username: username || '', attached: !!tab && attached.ownTabId === tab.id })
      })
      return true
    case 'panel:options':
      chrome.runtime.openOptionsPage()
      return false
    case 'panel:attach':
      if (tab) attach(tab).then(() => sendResponse({ attached: attached.ownTabId === tab.id }))
      return true
    case 'panel:detach':
      detach().then(() => sendResponse({ attached: false }))
      return true
  }
  return false
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

chrome.tabs.onRemoved.addListener((tabId) => {
  forgetCast(tabId)
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
