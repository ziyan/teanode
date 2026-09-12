// The extension attaches one tab to the person's agent over the server's
// websocket, saying who it is with the token the options page signed in
// with. The server never touches the page: every action arrives here as a
// request, is done by the content script in the page, and its answer goes
// back.
//
// It is the person's own tab and their own session, so the agent acts as
// they would and nothing here refuses on their behalf. Two lines are held
// all the same, because neither is about what the agent may do in this
// tab: a password already filled in on the page is not read back into the
// conversation, and the DevTools relay refuses the handful of methods
// that reach past this tab into the whole browser.

const PROTOCOL = 2

let socket = null
// attached is the person's tab and, while the agent has opened tabs of
// its own, which of them is current: ownTabId is theirs, tabId the one
// actions go to.
let attached = null // { tabId, ownTabId, title, url }
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
const restored = chrome.storage.session.get(['opened', 'groups']).then(({ opened: kept, groups: keptGroups }) => {
  for (const id of kept || []) opened.add(id)
  for (const [windowId, groupId] of keptGroups || []) groups.set(Number(windowId), groupId)
})
const rememberOpened = () => chrome.storage.session.set({ opened: [...opened] })
const rememberGroups = () => chrome.storage.session.set({ groups: [...groups] })
// A worker starts with nothing attached, whatever the badge said before.
setBadge('')
let pings = null
// wanted is the attachment the person asked for, kept so that a dropped
// socket can be put back; retry is the timer that does it.
let wanted = null
let retry = null

// reconnect opens the socket again for the tab the person attached, if
// that tab is still there.
async function reconnect() {
  if (!wanted) return
  const origin = await serverOrigin()
  const secret = await token()
  if (!origin || !secret) {
    wanted = null
    setBadge('')
    return
  }
  const still = await chrome.tabs.get(wanted.tabId).catch(() => null)
  if (!still) {
    // The tab it was attached to is gone; there is nothing to go back to.
    wanted = null
    setBadge('')
    return
  }
  connect(origin, secret)
}

async function serverOrigin() {
  const { server } = await chrome.storage.sync.get('server')
  return (server || '').replace(/\/+$/, '')
}

// The token the options page signed in with: what the tab says it is.
async function token() {
  const { token } = await chrome.storage.local.get('token')
  return token || ''
}

function setBadge(text, color) {
  chrome.action.setBadgeText({ text })
  if (color) chrome.action.setBadgeBackgroundColor({ color })
}

async function attach(tab) {
  const origin = await serverOrigin()
  const secret = await token()
  if (!origin || !secret) {
    chrome.runtime.openOptionsPage()
    return
  }
  detach()
  if (!/^https?:\/\//i.test(origin)) {
    // Typed without a scheme, the address builds a WebSocket URL that
    // throws, and the button did nothing with nothing said.
    chrome.runtime.openOptionsPage()
    return
  }
  wanted = { tabId: tab.id, attempt: 0 }
  connect(origin, secret)
}

// connect opens the socket and puts it back when it drops. A closed lid,
// a server restarted, a proxy that times out an idle socket: the
// attachment used to end there, with nothing but a blank badge to say so.
function connect(origin, secret) {
  const address = origin.replace(/^http/, 'ws') + '/api/v1/agent/tab'
  try {
    socket = new WebSocket(address)
  } catch {
    setBadge('!', '#c33')
    return
  }
  const tab = { id: wanted.tabId }
  attached = { tabId: wanted.tabId, ownTabId: wanted.tabId, title: '', url: '' }
  void chrome.tabs.get(wanted.tabId).then((found) => {
    if (attached) {
      attached.title = found.title || ''
      attached.url = found.url || ''
    }
  }).catch(() => {})
  socket.onopen = async () => {
    socket.send(JSON.stringify({ type: 'hello', protocol: PROTOCOL, token: secret, title: attached.title, url: attached.url }))
    // A word every so often keeps this worker, and the socket, alive
    // while nothing else is said.
    clearInterval(pings)
    pings = setInterval(() => {
      if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'ping' }))
    }, 20000)
  }
  socket.onmessage = async (event) => {
    let message
    try {
      message = JSON.parse(event.data)
    } catch {
      return
    }
    if (message.type === 'welcome') {
      if (message.protocol !== PROTOCOL) {
        socket.send(JSON.stringify({ type: 'bye', reason: 'protocol ' + message.protocol + ' is not known to this extension' }))
        detach()
        return
      }
      if (wanted) wanted.attempt = 0
      setBadge('on', '#2a7')
      tellPanels()
      return
    }
    if (message.type === 'refused') {
      setBadge('!', '#c33')
      detach()
      // A token the server no longer takes: sign in again.
      if (/token|sign in/i.test(message.reason || '')) chrome.runtime.openOptionsPage()
      return
    }
    if (message.type === 'act') {
      const answer = await act(message.action, message.args || {})
      socket?.send(JSON.stringify({ type: 'result', id: message.id, ...answer }))
    }
  }
  socket.onclose = () => {
    clearInterval(pings)
    letEveryDebuggerGo()
    socket = null
    attached = null
    tellPanels()
    if (!wanted) {
      setBadge('')
      return
    }
    // Put it back, slower each time, up to half a minute: a server being
    // restarted should not need the person to press the button again.
    setBadge('…', '#a80')
    const waiting = Math.min(30000, 1000 * 2 ** Math.min(5, wanted.attempt++))
    clearTimeout(retry)
    retry = setTimeout(() => {
      if (wanted) void reconnect()
    }, waiting)
  }
  socket.onerror = () => setBadge('!', '#c33')
}

function detach() {
  // Asked for: it does not come back on its own.
  wanted = null
  clearTimeout(retry)
  retry = null
  // The protocol goes with the attachment: otherwise Chrome's own
  // "is debugging this browser" bar stays up on a tab the agent can no
  // longer reach.
  letEveryDebuggerGo()
  if (socket) {
    try {
      socket.close()
    } catch {
      // Already gone.
    }
  }
  clearInterval(pings)
  socket = null
  attached = null
  setBadge('')
  tellPanels()
}

// tellPanels says to the open panels whether a tab is attached, so their
// button reads right; a page without a panel does not mind.
async function tellPanels() {
  const tabs = await chrome.tabs.query({})
  for (const tab of tabs) {
    chrome.tabs.sendMessage(tab.id, { type: 'state', attached: !!attached && attached.ownTabId === tab.id }).catch(() => {})
  }
}

// current is what the tab the actions go to shows now, told to the server
// so the agent's overlay names it.
async function current() {
  const tab = await chrome.tabs.get(attached.tabId)
  attached.title = tab.title || ''
  attached.url = tab.url || ''
  socket?.send(JSON.stringify({ type: 'update', title: attached.title, url: attached.url }))
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
  const own = await chrome.tabs.get(attached.ownTabId).catch(() => null)
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
const DEBUGGER_PROTOCOL = '1.3'
const EVENTS_KEPT = 500
const DEBUGGER_IDLE = 10 * 60 * 1000

// What the protocol may be asked for. The line is not "the agent is
// trusted" -- it is their agent and their tab -- but what the attached tab
// is: everything the page's own origin can reach, plus real input and what
// the page asks the network for. A handful of methods reach past that tab
// into the whole browser, and those are refused however they are asked
// for, because nothing about attaching one tab says yes to them.
const CDP_REFUSED = [
  // Every site's cookies, not this one's.
  'Network.getAllCookies',
  'Network.getCookies',
  'Network.setCookie',
  'Network.setCookies',
  'Network.deleteCookies',
  'Storage.getCookies',
  'Storage.setCookies',
  'Storage.clearCookies',
  // Rewriting or holding the page's own requests, on a tab signed in as
  // the person: a different thing from watching them.
  'Fetch.',
  // The browser itself rather than this page: downloads, permissions,
  // other windows, other targets.
  'Browser.',
  'Target.',
  'SystemInfo.',
  'Tethering.',
]

function cdpRefusal(method) {
  const asked = String(method || '').trim()
  if (!/^[A-Z][A-Za-z]*\.[a-zA-Z][A-Za-z0-9]*$/.test(asked)) {
    return 'a method is written Domain.method, for example Input.dispatchMouseEvent'
  }
  for (const refused of CDP_REFUSED) {
    const matches = refused.endsWith('.') ? asked.startsWith(refused) : asked === refused
    if (matches) {
      return `${asked} reaches past this tab into the whole browser, which attaching one tab does not allow; what this page itself holds is reachable with storage, fetch and evaluate`
    }
  }
  return ''
}

async function debuggerFor(tabId) {
  const known = debugging.get(tabId)
  if (known) {
    known.until = Date.now() + DEBUGGER_IDLE
    return known
  }
  await chrome.debugger.attach({ tabId }, DEBUGGER_PROTOCOL)
  const state = { events: [], until: Date.now() + DEBUGGER_IDLE }
  debugging.set(tabId, state)
  return state
}

async function letDebuggerGo(tabId) {
  if (!debugging.delete(tabId)) return
  await chrome.debugger.detach({ tabId }).catch(() => {})
}

function letEveryDebuggerGo() {
  for (const tabId of [...debugging.keys()]) void letDebuggerGo(tabId)
}

// What a page did, kept until it is asked for. The newest are kept: a
// page that makes a thousand requests should not push the worker over.
chrome.debugger.onEvent.addListener((source, method, params) => {
  const state = source.tabId !== undefined && debugging.get(source.tabId)
  if (!state) return
  state.events.push({ at: Date.now(), method, params })
  if (state.events.length > EVENTS_KEPT) state.events.splice(0, state.events.length - EVENTS_KEPT)
})

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId !== undefined) debugging.delete(source.tabId)
})

// act does one action in the attached tab: navigation here, everything
// else in the page through the content script; fetch through the tab's
// own session.
async function act(action, args) {
  if (!attached) return { ok: false, error: 'no tab is attached' }
  try {
    const tab = await chrome.tabs.get(attached.tabId)
    if (action === 'navigate') {
      if (!/^https?:\/\//i.test(args.url || '')) return { ok: false, error: 'only http and https addresses' }
      await chrome.tabs.update(tab.id, { url: args.url })
      await waitForLoad(tab.id)
      const updated = await chrome.tabs.get(tab.id)
      attached.title = updated.title || ''
      attached.url = updated.url || ''
      socket?.send(JSON.stringify({ type: 'update', title: attached.title, url: attached.url }))
      return { ok: true, data: { url: attached.url, title: attached.title } }
    }
    if (action === 'open') {
      if (!/^https?:\/\//i.test(args.url || '')) return { ok: false, error: 'only http and https addresses' }
      const made = await chrome.tabs.create({ url: args.url, windowId: tab.windowId, active: true })
      opened.add(made.id)
      await rememberOpened()
      await groupFor(tab.windowId, made.id)
      attached.tabId = made.id
      await waitForLoad(made.id)
      return { ok: true, data: await current() }
    }
    if (action === 'tabs') {
      const own = await chrome.tabs.get(attached.ownTabId).catch(() => null)
      const listed = []
      if (own) listed.push({ tab: own.id, title: own.title || '', url: own.url || '', own: true, current: own.id === attached.tabId })
      for (const opened of await agentTabs()) {
        listed.push({ tab: opened.id, title: opened.title || '', url: opened.url || '', own: false, current: opened.id === attached.tabId })
      }
      return { ok: true, data: { tabs: listed } }
    }
    if (action === 'switch') {
      // By number from tabs, by a piece of its address, or, given nothing,
      // back to the person's own tab.
      const wanted = await pickTab(args, attached.ownTabId)
      if (!wanted) return { ok: false, error: 'not a tab of this conversation: the person\'s own, or one you opened; tabs lists them' }
      await chrome.tabs.update(wanted, { active: true })
      attached.tabId = wanted
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
        attached.tabId = attached.ownTabId
        await chrome.tabs.update(attached.ownTabId, { active: true })
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
      const refusal = cdpRefusal(args.method)
      if (refusal) return { ok: false, error: refusal }
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
      await letDebuggerGo(tab.id)
      return { ok: true, data: { stopped: true } }
    }
    const [result] = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: inPage,
      args: [action, args],
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
async function inPage(action, args) {
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
      node.click()
      await new Promise((resolve) => setTimeout(resolve, 300))
      return { ok: true, data: { clicked: nameOf(node), now: { url: location.href, title: document.title } } }
    }
    case 'hover': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches' }
      node.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
      return { ok: true, data: { hovered: nameOf(node) } }
    }
    case 'type': {
      const node = find()
      if (!node) return { ok: false, error: 'nothing matches; take a snapshot first' }
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
        sendResponse({ server, token: secret, username: username || '', attached: !!attached && !!tab && attached.ownTabId === tab.id })
      })
      return true
    case 'panel:options':
      chrome.runtime.openOptionsPage()
      return false
    case 'panel:attach':
      if (tab) attach(tab).then(() => sendResponse({ attached: !!attached && attached.ownTabId === tab.id }))
      return true
    case 'panel:detach':
      detach()
      sendResponse({ attached: false })
      return false
  }
  return false
})

chrome.tabs.onUpdated.addListener((tabId, info, tab) => {
  if (attached && attached.tabId === tabId && info.status === 'complete' && socket) {
    attached.title = tab.title || ''
    attached.url = tab.url || ''
    socket.send(JSON.stringify({ type: 'update', title: attached.title, url: attached.url }))
  }
})

chrome.tabs.onRemoved.addListener((tabId) => {
  if (opened.delete(tabId)) rememberOpened()
  if (!attached) return
  if (attached.ownTabId === tabId) {
    detach()
    return
  }
  // A tab the agent opened, closed by the person: back to their own.
  if (attached.tabId === tabId) {
    attached.tabId = attached.ownTabId
    current().catch(() => {})
  }
})
