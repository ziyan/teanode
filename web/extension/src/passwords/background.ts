// Passwords in the extension's service worker: it answers the content
// scripts in pages and the extension's own pages, holds the opened keys (see
// openedKeys.ts), and decides what each message may do from who sent it. A page
// can neither send these messages nor hear the answers; a content script is
// trusted for what the browser says about its frame (sender.url, sender.tab),
// never for what it claims, and every secret handed to one is checked against
// that frame's address first.

import { PasswordsError, PasswordsErrorCode } from '../../../src/passwords/passwordsError'
import { IndexedDBKeptUnlockStore, KeptUnlock } from '../../../src/pages/passwords/passwordsKeptUnlock'
import { LoginItem } from '../../../src/pages/passwords/passwordsProtocol'
import { agentActivity } from '../agentActivity'
import { CapturedLogins } from './capturedLogins'
import { CLEAR_CLIPBOARD_ALARM, clearClipboardIfUnchanged, copySecret } from './clipboard'
import { isSecureContextPage, isWebPage, normalizeHost, originOf, originOfSite } from './hosts'
import {
  ContentRequest,
  FillFieldsMessage,
  FillInTabAnswer,
  OtherSiteQuestion,
  PopupRequest,
  PopupState,
  PromptInfo,
  SaveOffer,
  SearchAnswer,
} from './messages'
import type { PasskeyReply } from './passkeyOverride'
import { NOT_ALLOWED_REPLY, PasskeyPrompts, PendingPrompt, PROMPT_LIFETIME_MILLISECONDS } from './passkeyPrompts'
import { callServer, isSignedIn, readServerOrigin, ServerCallError } from './serverCall'
import {
  currentOneTimeCode,
  describeOwnerKeyChanges,
  forgetPasskeySites,
  isPasskeySiteKnown,
  loginsForPage,
  loginsOf,
  lockIfIdle,
  lockPasswords,
  openItems,
  OpenReport,
  passkeysForRelyingParty,
  readPasswordsSetup,
  readPasswordsState,
  releaseItems,
  saveCapturedLogin,
  savePasskey,
  sharedByTextOf,
  sortLogins,
  StoredPasskey,
  summaryOf,
  dismissOwnerKeyNotice,
  unlockPasswords,
  PasswordsEnvironment,
  whatToDoWithCapturedLogin,
  withOpenedKeys,
  MAXIMUM_LOCK_AFTER_MINUTES,
} from './openedKeys'
import {
  credentialIdOf,
  PageContext,
  parseAssertionRequest,
  parseCreationRequest,
  makePasskey,
  signWithPasskey,
} from './webauthn'

const environment: PasswordsEnvironment = {
  session: chrome.storage.session,
  kept: new KeptUnlock(new IndexedDBKeptUnlockStore(), MAXIMUM_LOCK_AFTER_MINUTES * 60_000),
  local: chrome.storage.local,
  call: callServer,
  now: () => Date.now(),
  // Whichever way the keys are dropped (by hand, by the lock time, by a change
  // of sign-in, at browser start), what is kept beside them goes too: the
  // login waiting to be saved holds a password, and the cache and the alarm
  // are only for open keys.
  onLock: async () => {
    capturedLogins.forgetAll()
    await unmarkAllSaveOffers()
    await chrome.alarms.clear(LOCK_ALARM)
  },
  onRestore: () => startLockAlarm(),
}

const LOCK_ALARM = 'passwords-lock'
const SET_UP_FLAG = 'passwordsIsSetUp'
const CONTEXT_MENU_ID = 'passwords-fill'
const POPUP_PATH = '/passwords.html'
const PROMPT_PATH = '/prompt.html'
const OPTIONS_PATH = '/options.html'
// The window the dropdown's page opens in when Chrome cannot show the
// dropdown (a window without a toolbar, say), and the passkey question's when
// the page that asked is not the one in front.
const POPUP_SIZE = { width: 400, height: 600 }
const PROMPT_SIZE = { width: 440, height: 380 }

// The sites where the person chose "Use the browser instead", kept for the
// browser session only (chrome.storage.session).
const USE_BROWSER_SITES_KEY = 'passwordsUseBrowserSites'

const capturedLogins = new CapturedLogins()
const passkeyPrompts = new PasskeyPrompts()
let popupWindowId: number | null = null

// ---------------------------------------------------------------------------
// Words for what goes wrong (the extension is English only today, like its
// options page and panel)

const ERROR_WORDS: Record<PasswordsErrorCode, string> = {
  locked: 'Passwords is locked. Unlock it first.',
  notSetUp: 'Passwords is not set up yet. Open the dashboard to set it up.',
  iterationCountOutOfRange: 'The server sent a passphrase setting that is not safe to use.',
  itemUnreadable: 'This item could not be opened.',
  itemUnknown: 'The server did not know that item.',
  ownerOnly: 'Only the owner can change this item.',
  publicKeyMismatch: 'The server sent a public key that is not yours. Nothing was opened.',
}

function describeError(error: unknown): { error: string; errorCode?: PasswordsErrorCode } {
  if (error instanceof PasswordsError) return { error: ERROR_WORDS[error.code], errorCode: error.code }
  if (error instanceof ServerCallError) return { error: error.message }
  if (error instanceof Error && error.message) return { error: error.message }
  return { error: 'Something went wrong.' }
}

// ---------------------------------------------------------------------------
// Who is asking

interface ContentFrame {
  tabId: number
  frameId: number
  url: string
  origin: string
  isIncognito: boolean
}

// contentFrameOf is the page frame a message came from, as the browser
// describes it. Messages from extension pages, and from frames that are not
// web pages, are not content messages.
function contentFrameOf(sender: any): ContentFrame | null {
  if (!sender || sender.id !== chrome.runtime.id || !sender.tab || typeof sender.tab.id !== 'number') return null
  const url = typeof sender.url === 'string' ? sender.url : ''
  if (!isWebPage(url)) return null
  // A sandboxed document (a user's upload served with a sandbox policy, say)
  // has an opaque origin that says nothing of the site that serves it, even
  // though its address is that site's. It is offered and given nothing.
  const origin = typeof sender.origin === 'string' ? sender.origin : ''
  if (origin === '' || origin === 'null') return null
  return { tabId: sender.tab.id, frameId: sender.frameId ?? 0, url, origin, isIncognito: sender.tab.incognito === true }
}

// isOwnPage says a message came from one of the extension's own pages, opened
// as a page of its own (the button's dropdown, the passkey question, the
// options). Only those may unlock, copy or list. None of them is reachable
// from a website: the manifest offers no page of the extension to be framed.
function isOwnPage(sender: any): boolean {
  if (!sender || sender.id !== chrome.runtime.id) return false
  // A page in a tab or a window is its top frame; the dropdown is in no tab,
  // and Chrome gives a frame only to a sender in a tab.
  const isTopFrame = sender.tab ? sender.frameId === 0 : sender.frameId === undefined || sender.frameId === 0
  if (!isTopFrame) return false
  const prefix = chrome.runtime.getURL('')
  const url = typeof sender.url === 'string' ? sender.url : ''
  if (!url.startsWith(prefix)) return false
  const path = '/' + url.slice(prefix.length).split(/[?#]/)[0]
  return path === POPUP_PATH || path === PROMPT_PATH || path === OPTIONS_PATH
}

// ---------------------------------------------------------------------------
// Whether there is anything to do

async function isPasswordsAvailable(): Promise<boolean> {
  if (!(await isSignedIn())) return false
  return (await chrome.storage.local.get(SET_UP_FLAG))[SET_UP_FLAG] === true
}

async function rememberIsSetUp(isSetUp: boolean) {
  await chrome.storage.local.set({ [SET_UP_FLAG]: isSetUp })
}

// learnIfSetUp asks the server whether this person has set Passwords up and
// remembers the answer, so passkey requests know whether to stop and ask. It runs
// when the extension signs in and when the browser starts; a server that
// cannot be reached leaves what was remembered.
async function learnIfSetUp() {
  if (!(await isSignedIn())) {
    await chrome.storage.local.remove(SET_UP_FLAG)
    return
  }
  try {
    await rememberIsSetUp((await readPasswordsSetup(environment)).isSetUp)
  } catch {
    // Not reachable now; asked again when the dropdown is opened.
  }
}

async function startLockAlarm() {
  await chrome.alarms.create(LOCK_ALARM, { periodInMinutes: 1 })
}

// lockEverything drops the keys; environment.onLock clears the rest.
// shouldKeepKeptCopy keeps the copy kept while the browser was closed (at
// browser start, when the session area is empty anyway).
async function lockEverything(options: { shouldKeepKeptCopy: boolean } = { shouldKeepKeptCopy: false }) {
  await lockPasswords(environment, options)
}

// ---------------------------------------------------------------------------
// Frames

// addressOfThisFrame runs inside the frame. A sandboxed document (opaque
// origin) has no address that stands for a site: it answers with none, and is
// offered and given nothing.
function addressOfThisFrame(): string {
  return window.origin === 'null' ? '' : location.href
}

async function describeFrame(tabId: number, frameId: number): Promise<string | null> {
  try {
    const [result] = await chrome.scripting.executeScript({
      target: { tabId, frameIds: [frameId] },
      func: addressOfThisFrame,
    })
    return result && typeof result.result === 'string' && isWebPage(result.result) ? result.result : null
  } catch {
    return null
  }
}

// frameWithFocus is the frame a keyboard shortcut means: the one holding the
// keyboard focus in a field, or null when none does.
async function frameWithFocus(tabId: number): Promise<number | null> {
  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId, allFrames: true },
      func: () => document.hasFocus() && !!document.activeElement && document.activeElement !== document.body,
    })
    const focused = results.find((result: { result: unknown }) => result.result === true)
    return focused ? focused.frameId : null
  } catch {
    return null
  }
}

// ---------------------------------------------------------------------------
// The windows

async function closeWindow(windowId: number | null) {
  if (windowId === null) return
  try {
    await chrome.windows.remove(windowId)
  } catch {
    // Already closed.
  }
}

// openPasswords shows the button's dropdown for a tab, as a press on the
// button does: from the keyboard shortcut and the context menu. Where Chrome
// cannot show it (a window without a toolbar), the same page opens in a small
// window instead, which is better than nothing happening.
async function openPasswords(tab: { id?: number; windowId?: number }) {
  try {
    await chrome.action.openPopup(typeof tab.windowId === 'number' ? { windowId: tab.windowId } : {})
    return
  } catch {
    // No toolbar to hang the dropdown from.
  }
  await closeWindow(popupWindowId)
  const made = await chrome.windows.create({
    url: chrome.runtime.getURL(typeof tab.id === 'number' ? `passwords.html?tab=${tab.id}` : 'passwords.html'),
    type: 'popup',
    focused: true,
    ...POPUP_SIZE,
  })
  popupWindowId = made?.id ?? null
}

// focusedFrames is, for each tab, the frame that last had a sign-in field
// focused (the content script says so), or the one holding the focus when the
// keyboard shortcut was pressed. The dropdown fills that frame: once it is
// open the page no longer has the focus, so it cannot be asked then.
const focusedFrames = new Map<number, number>()

// frameToFill is the frame of a tab the dropdown fills: the one focused last
// if it is still there, else the top frame if it shows a password field,
// else the first frame that does, else the top frame.
async function frameToFill(tabId: number): Promise<number> {
  const focused = focusedFrames.get(tabId)
  if (focused !== undefined && (await describeFrame(tabId, focused)) !== null) return focused
  try {
    const frames = ((await chrome.webNavigation.getAllFrames({ tabId })) ?? []) as { frameId: number }[]
    const ordered = [...frames].sort((first, second) => first.frameId - second.frameId)
    for (const frame of ordered) {
      const probe = await chrome.tabs
        .sendMessage(tabId, { type: 'passwords:probe' }, { frameId: frame.frameId })
        .catch(() => null)
      if (probe && probe.hasPasswordField === true) return frame.frameId
    }
  } catch {
    // The tab cannot be looked into (an internal page).
  }
  return 0
}

// The toolbar button is marked, for one tab, when a login typed there waits
// to be saved: a "+" on the button and a title saying so, in place of the bar
// on the page there used to be. The dropdown shows the offer. Which tabs are
// marked is kept for the browser session, so that a worker started again,
// which has lost the login itself, takes the marks off.
const SAVE_MARKED_TABS_KEY = 'passwordsSaveMarkedTabs'
const SAVE_MARK_TEXT = '+'
const SAVE_MARK_COLOR = '#2f6fdf'

async function readSaveMarkedTabs(): Promise<number[]> {
  const stored = (await chrome.storage.session.get(SAVE_MARKED_TABS_KEY))[SAVE_MARKED_TABS_KEY]
  return Array.isArray(stored) ? stored.filter((tabId): tabId is number => typeof tabId === 'number') : []
}

async function markSaveOffer(tabId: number, offer: SaveOffer) {
  const title =
    offer.offerAction === 'update'
      ? `TeaNode: update the saved password for ${offer.host}?`
      : `TeaNode: save the login you used on ${offer.host}?`
  try {
    await chrome.action.setBadgeText({ tabId, text: SAVE_MARK_TEXT })
    await chrome.action.setBadgeBackgroundColor({ tabId, color: SAVE_MARK_COLOR })
    await chrome.action.setTitle({ tabId, title })
  } catch {
    // The tab is gone.
    return
  }
  const marked = await readSaveMarkedTabs()
  if (!marked.includes(tabId)) await chrome.storage.session.set({ [SAVE_MARKED_TABS_KEY]: [...marked, tabId] })
}

// unmarkSaveOffer gives the tab the button everybody else has: null hands
// the badge and the title back to what the rest of the extension sets.
async function unmarkSaveOffer(tabId: number) {
  const marked = await readSaveMarkedTabs()
  if (!marked.includes(tabId)) return
  await chrome.storage.session.set({ [SAVE_MARKED_TABS_KEY]: marked.filter((each) => each !== tabId) })
  try {
    await chrome.action.setBadgeText({ tabId, text: null })
    await chrome.action.setTitle({ tabId, title: null })
  } catch {
    // The tab is gone.
  }
}

async function unmarkAllSaveOffers() {
  for (const tabId of await readSaveMarkedTabs()) await unmarkSaveOffer(tabId)
}

// forgetCaptured drops the login waiting in a tab, and the mark with it.
async function forgetCaptured(tabId: number) {
  capturedLogins.forget(tabId)
  await unmarkSaveOffer(tabId)
}

chrome.windows.onRemoved.addListener((windowId: number) => {
  if (windowId === popupWindowId) popupWindowId = null
  const prompt = passkeyPrompts.findByWindow(windowId)
  if (prompt) {
    prompt.windowId = null
    settlePrompt(prompt.id, NOT_ALLOWED_REPLY)
  }
})

// The passkey question keeps a port open to this worker for as long as it is
// on screen, in the dropdown or in its window: the dropdown closing (a click
// elsewhere, Escape) disconnects it, which answers the website as a refusal,
// as closing its window does.
const PROMPT_PORT_NAME = 'passwords-prompt'

chrome.runtime.onConnect.addListener((port: any) => {
  if (port.name !== PROMPT_PORT_NAME || !isOwnPage(port.sender)) return
  const url = typeof port.sender.url === 'string' ? port.sender.url : ''
  let promptId = ''
  try {
    const address = new URL(url)
    if (address.pathname === PROMPT_PATH) promptId = address.searchParams.get('id') ?? ''
  } catch {
    // Not an address.
  }
  const prompt = passkeyPrompts.get(promptId)
  if (!prompt) {
    port.disconnect()
    return
  }
  prompt.port = port
  port.onDisconnect.addListener(() => {
    const still = passkeyPrompts.get(promptId)
    if (still && still.port === port) {
      still.port = null
      settlePrompt(promptId, NOT_ALLOWED_REPLY)
    }
  })
})

// ---------------------------------------------------------------------------
// From a page: which frame to fill, saving

// offerFor decides whether the button is marked for the login captured in a
// tab, and what the dropdown offers. It marks the capture as offered, so the
// next page does not mark it again.
async function offerFor(tabId: number): Promise<SaveOffer | null> {
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '' || captured.hasBeenOffered) return null
  const offer = await describeOffer(captured)
  if (!offer) {
    await forgetCaptured(tabId)
    return null
  }
  capturedLogins.markOffered(tabId)
  return offer
}

// describeOffer is what there is to offer for a captured login: saving it,
// updating the saved password, unlocking first, or nothing (it is saved as
// it is).
async function describeOffer(captured: {
  pageUrl: string
  userName: string
  password: string
  isNewPassword: boolean
}): Promise<SaveOffer | null> {
  const host = normalizeHost(captured.pageUrl)
  if (!(await readPasswordsState(environment)).isUnlocked)
    return { offerAction: 'unlock', host, userName: captured.userName }
  const offerAction = await withOpenedKeys(
    environment,
    async (keys) => {
      const items = await openItems(environment, keys)
      try {
        return whatToDoWithCapturedLogin(loginsOf(items), captured).offerAction
      } finally {
        releaseItems(items)
      }
    },
    { isUse: false },
  )
  return offerAction === 'none' ? null : { offerAction, host, userName: captured.userName }
}

// offerToSave marks the button when there is something to offer for the
// login captured in a tab.
async function offerToSave(tabId: number) {
  const offer = await offerFor(tabId)
  if (offer) await markSaveOffer(tabId, offer)
}

// pageReady is a page that signs in without leaving saying its password field
// is gone. Only the top frame counts.
async function pageReady(frame: ContentFrame, hasPasswordField: boolean): Promise<Record<string, never>> {
  if (frame.frameId === 0 && !hasPasswordField) await offerToSave(frame.tabId)
  return {}
}

// saveCaptured saves the login captured in a tab. Locked, it saves nothing
// and says so; the capture stays for the person to unlock and try again.
async function saveCaptured(
  tabId: number,
): Promise<{ saveOutcome: 'saved' | 'updated' | 'unchanged' | 'locked' | 'gone' }> {
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '') {
    await unmarkSaveOffer(tabId)
    return { saveOutcome: 'gone' }
  }
  if (!(await readPasswordsState(environment)).isUnlocked) return { saveOutcome: 'locked' }
  const saveOutcome = await withOpenedKeys(environment, async (keys) => {
    const items = await openItems(environment, keys)
    try {
      return await saveCapturedLogin(environment, keys, loginsOf(items), {
        pageUrl: captured.pageUrl,
        userName: captured.userName,
        password: captured.password,
      })
    } finally {
      releaseItems(items)
    }
  })
  await forgetCaptured(tabId)
  return { saveOutcome }
}

// isOwnPasswordsPage says the address is a page of the TeaNode dashboard's
// own Passwords: its passphrase and item-editor fields are not logins, so
// nothing is offered, filled or remembered there. Signing in to the dashboard
// itself, anywhere else on the server, is an ordinary login.
async function isOwnPasswordsPage(pageUrl: string): Promise<boolean> {
  const serverOrigin = await readServerOrigin()
  if (serverOrigin === '') return false
  try {
    const page = new URL(pageUrl)
    return (
      page.origin === new URL(serverOrigin).origin &&
      (page.pathname === '/passwords' || page.pathname.startsWith('/passwords/'))
    )
  } catch {
    return false
  }
}

const OWN_PAGE_REFUSAL = { error: 'Passwords does not fill its own page.' }

// AGENT_TAB_REFUSAL is the answer to a page in a tab the agent is working in:
// its real input looks like the person's, so what a page asks of Passwords
// there is not answered until the agent has been quiet for a minute or the
// person holds the tab.
const AGENT_TAB_REFUSAL = {
  error:
    'The agent is working in this tab, so Passwords does not fill it. Try again in a minute, or take the tab over first.',
}

async function handleContentRequest(frame: ContentFrame, message: ContentRequest): Promise<unknown> {
  if (message.type === 'passwords:captured' && (await agentActivity.isAgentDriving(frame.tabId))) return {}
  if (message.type === 'passwords:captured' && (await isOwnPasswordsPage(frame.url))) return {}
  switch (message.type) {
    case 'passwords:field-focused':
      focusedFrames.set(frame.tabId, frame.frameId)
      return {}
    case 'passwords:captured':
      if (frame.isIncognito || !(await isPasswordsAvailable())) return {}
      capturedLogins.capture(frame.tabId, {
        pageUrl: frame.url,
        userName: message.userName,
        password: message.password,
        isNewPassword: message.isNewPassword,
      })
      return {}
    case 'passwords:page-ready':
      return pageReady(frame, message.hasPasswordField)
    case 'passwords:passkey':
      return handlePasskey(frame, message)
    case 'passwords:passkey-cancel':
      cancelPasskey(frame)
      return {}
  }
  return { error: 'Unknown request.' }
}

// ---------------------------------------------------------------------------
// Passkeys

function passkeyChoices(
  items: Awaited<ReturnType<typeof openItems>>,
  relyingPartyId: string,
  allowed: string[],
): StoredPasskey[] {
  const found = passkeysForRelyingParty(loginsOf(items), relyingPartyId, allowed, credentialIdOf)
  return found.sort(
    (first, second) =>
      first.login.fields.name.localeCompare(second.login.fields.name) ||
      first.passkey.userName.localeCompare(second.passkey.userName),
  )
}

function choiceLabel(choice: StoredPasskey): string {
  const userName = choice.passkey.userName || choice.login.fields.userName
  return userName || choice.login.fields.name
}

// withoutFragment is an address without what follows a "#", for comparing
// where a tab is: a move within the page is not a move away from it.
function withoutFragment(address: string): string {
  const hash = address.indexOf('#')
  return hash < 0 ? address : address.slice(0, hash)
}

async function readTabUrl(tabId: number): Promise<string | null> {
  try {
    const tab = await chrome.tabs.get(tabId)
    return typeof tab.url === 'string' ? tab.url : ''
  } catch {
    return null
  }
}

// readShouldFocusPrompt: the tab is the one the person is looking at in its
// window, and that window has the keyboard focus. A tab that is active in a
// window behind another one is not being looked at.
async function readShouldFocusPrompt(tabId: number): Promise<boolean> {
  try {
    const tab = await chrome.tabs.get(tabId)
    if (tab.active !== true || typeof tab.windowId !== 'number') return false
    return (await chrome.windows.get(tab.windowId)).focused === true
  } catch {
    return false
  }
}

// After a question for a tab is refused or closed without an answer, the tab
// is not asked about again for a few seconds: a page cannot open the question
// over and over by asking over and over.
const PASSKEY_REFUSAL_PAUSE_MILLISECONDS = 3000
const passkeyPausedUntilMilliseconds = new Map<number, number>()

// settlePrompt ends a question with the reply the page gets. A refusal (the
// window closed, the time ran out, the page went away) starts the pause.
function settlePrompt(promptId: string, reply: PasskeyReply): PendingPrompt | null {
  const settled = passkeyPrompts.settle(promptId, reply)
  if (settled && reply === NOT_ALLOWED_REPLY) {
    passkeyPausedUntilMilliseconds.set(settled.tabId, Date.now() + PASSKEY_REFUSAL_PAUSE_MILLISECONDS)
  }
  return settled
}

// endPrompt answers the page and closes the question, in the dropdown or its
// window.
function endPrompt(promptId: string, reply: PasskeyReply) {
  const settled = settlePrompt(promptId, reply)
  if (!settled) return
  void closeWindow(settled.windowId)
  try {
    settled.port?.postMessage({ promptMessageKind: 'close' })
  } catch {
    // Already closed.
  }
}

// showPromptInDropdown asks the passkey question in the toolbar button's
// dropdown. For that one opening the button shows the question instead of the
// logins; it shows the logins again right after.
const PROMPT_CONNECT_MILLISECONDS = 5000

async function showPromptInDropdown(tabId: number, promptId: string): Promise<boolean> {
  try {
    const tab = await chrome.tabs.get(tabId)
    await chrome.action.setPopup({ tabId, popup: `prompt.html?id=${promptId}` })
    try {
      await chrome.action.openPopup({ windowId: tab.windowId })
    } finally {
      await chrome.action.setPopup({ tabId, popup: POPUP_PATH.slice(1) })
    }
    // A dropdown closed before its page loaded never connected: that is a
    // refusal too, not a question left open for minutes.
    setTimeout(() => {
      const prompt = passkeyPrompts.get(promptId)
      if (prompt && prompt.port === null && prompt.windowId === null) endPrompt(promptId, NOT_ALLOWED_REPLY)
    }, PROMPT_CONNECT_MILLISECONDS)
    return true
  } catch {
    return false
  }
}

// The sites the person sent to the browser for this session.
async function readBrowserSites(): Promise<string[]> {
  const stored = (await chrome.storage.session.get(USE_BROWSER_SITES_KEY))[USE_BROWSER_SITES_KEY]
  return Array.isArray(stored) ? stored.filter((site): site is string => typeof site === 'string') : []
}

async function rememberBrowserSite(origin: string) {
  const sites = await readBrowserSites()
  if (!sites.includes(origin)) await chrome.storage.session.set({ [USE_BROWSER_SITES_KEY]: [...sites, origin] })
}

// cancelledAtMilliseconds is when each frame last withdrew a passkey request,
// so that a question about to be asked for a request the page had already
// given up on is not asked.
const cancelledAtMilliseconds = new Map<string, number>()

function frameKey(tabId: number, frameId: number): string {
  return `${tabId}:${frameId}`
}

async function startPrompt(
  frame: ContentFrame,
  context: PageContext,
  askedAtMilliseconds: number,
  details: Pick<
    PendingPrompt,
    'ceremonyKind' | 'relyingPartyId' | 'userName' | 'userVerification' | 'creation' | 'assertion'
  >,
): Promise<PasskeyReply> {
  if (passkeyPrompts.isPendingForTab(frame.tabId)) return NOT_ALLOWED_REPLY
  if ((cancelledAtMilliseconds.get(frameKey(frame.tabId, frame.frameId)) ?? 0) >= askedAtMilliseconds) {
    return NOT_ALLOWED_REPLY
  }
  const tabUrl = await readTabUrl(frame.tabId)
  // The tab is gone, or went somewhere else while this was being worked out.
  if (tabUrl === null) return NOT_ALLOWED_REPLY
  const id = crypto.randomUUID()
  const reply = new Promise<PasskeyReply>((resolve) => {
    const prompt: PendingPrompt = {
      id,
      tabId: frame.tabId,
      frameId: frame.frameId,
      tabUrl,
      windowId: null,
      port: null,
      context,
      resolve,
      timer: null,
      isDeciding: false,
      ...details,
    }
    prompt.timer = setTimeout(() => endPrompt(id, NOT_ALLOWED_REPLY), PROMPT_LIFETIME_MILLISECONDS)
    passkeyPrompts.add(prompt)
  })
  try {
    // The question takes the keyboard focus only when the page that asked is
    // the tab the person is looking at, in a window that has the focus: then
    // it is asked in the button's dropdown, or a window that takes the focus
    // where there is no dropdown. Otherwise its window opens without the
    // focus, and a page in the background cannot pull the person's typing
    // into it.
    const shouldFocus = await readShouldFocusPrompt(frame.tabId)
    if (shouldFocus && (await showPromptInDropdown(frame.tabId, id))) return reply
    const made = await chrome.windows.create({
      url: chrome.runtime.getURL(`prompt.html?id=${id}`),
      type: 'popup',
      focused: shouldFocus,
      ...PROMPT_SIZE,
    })
    const prompt = passkeyPrompts.get(id)
    if (prompt) prompt.windowId = made?.id ?? null
    else await closeWindow(made?.id ?? null)
  } catch {
    passkeyPrompts.settle(id, { replyOutcome: 'fallback' })
  }
  return reply
}

async function handlePasskey(
  frame: ContentFrame,
  message: Extract<ContentRequest, { type: 'passwords:passkey' }>,
): Promise<PasskeyReply> {
  const askedAtMilliseconds = Date.now()
  // A frame inside a page of another origin is the browser's to ask about:
  // whether it may use passkeys at all is a permission policy of the page
  // around it, which the browser enforces and this extension cannot see.
  if (message.isCrossOrigin || frame.isIncognito) return { replyOutcome: 'fallback' }
  // The tab just had a question refused or closed: the browser answers for now.
  if ((passkeyPausedUntilMilliseconds.get(frame.tabId) ?? 0) > askedAtMilliseconds) return { replyOutcome: 'fallback' }
  if (!(await isPasswordsAvailable())) return { replyOutcome: 'fallback' }
  // The person already sent this site to the browser in this session.
  if ((await readBrowserSites()).includes(frame.origin)) return { replyOutcome: 'fallback' }
  const context: PageContext = { pageUrl: frame.url, origin: frame.origin, isCrossOrigin: false }
  if (message.ceremonyKind === 'create') {
    const parsed = parseCreationRequest(message.options, context)
    if (parsed.parseOutcome === 'fallback') return { replyOutcome: 'fallback' }
    if (parsed.parseOutcome === 'error') {
      return { replyOutcome: 'error', errorName: parsed.errorName, errorMessage: parsed.refusalReason }
    }
    return startPrompt(frame, context, askedAtMilliseconds, {
      ceremonyKind: 'create',
      relyingPartyId: parsed.request.relyingPartyId,
      userName: parsed.request.userName,
      userVerification: parsed.request.userVerification,
      creation: { request: parsed.request },
      assertion: null,
    })
  }
  const parsed = parseAssertionRequest(message.options, context)
  if (parsed.parseOutcome === 'fallback') return { replyOutcome: 'fallback' }
  if (parsed.parseOutcome === 'error') {
    return { replyOutcome: 'error', errorName: parsed.errorName, errorMessage: parsed.refusalReason }
  }
  const request = parsed.request
  // Unlocked, a site Passwords has no passkey for is left to the browser with
  // no question. Locked, the passkeys cannot be looked at, but a list of the
  // sites that have one was kept the last time they were opened: a site not on
  // it is left to the browser too, with no window, and only a site that has a
  // passkey here makes the question ask the person to unlock.
  if ((await readPasswordsState(environment)).isUnlocked) {
    const hasChoice = await withOpenedKeys(
      environment,
      async (keys) => {
        const items = await openItems(environment, keys)
        try {
          return passkeyChoices(items, request.relyingPartyId, request.allowedCredentialIds).length > 0
        } finally {
          releaseItems(items)
        }
      },
      { isUse: false },
    ).catch(() => false)
    if (!hasChoice) return { replyOutcome: 'fallback' }
  } else if (!(await isPasskeySiteKnown(environment, request.relyingPartyId))) {
    return { replyOutcome: 'fallback' }
  }
  return startPrompt(frame, context, askedAtMilliseconds, {
    ceremonyKind: 'get',
    relyingPartyId: request.relyingPartyId,
    userName: '',
    userVerification: request.userVerification,
    creation: null,
    assertion: { request },
  })
}

// cancelPasskey is the page withdrawing its request (an abort signal): the
// question is ended with it, if it is this frame's.
function cancelPasskey(frame: ContentFrame) {
  cancelledAtMilliseconds.set(frameKey(frame.tabId, frame.frameId), Date.now())
  const prompt = passkeyPrompts.findByFrame(frame.tabId, frame.frameId)
  if (prompt) endPrompt(prompt.id, NOT_ALLOWED_REPLY)
}

async function promptInfo(promptId: string): Promise<PromptInfo | { error: string }> {
  const prompt = passkeyPrompts.get(promptId)
  if (!prompt) return { error: 'This question has already been answered.' }
  const passwordsState = await readPasswordsState(environment)
  let choices: PromptInfo['choices'] = []
  if (passwordsState.isUnlocked && prompt.assertion) {
    const request = prompt.assertion.request
    const found = await withOpenedKeys(
      environment,
      async (keys) => {
        const items = await openItems(environment, keys)
        try {
          return passkeyChoices(items, request.relyingPartyId, request.allowedCredentialIds).map((choice) => ({
            label: choiceLabel(choice),
            credentialId: credentialIdOf(choice.passkey),
            sharedByText: sharedByTextOf(choice.login),
          }))
        } finally {
          releaseItems(items)
        }
      },
      { isUse: false },
    )
    // Unlocked in this window and nothing for this site: the browser takes over.
    if (found.length === 0) {
      endPrompt(promptId, { replyOutcome: 'fallback' })
      return { error: 'There is no passkey for this site here. The browser will ask instead.' }
    }
    choices = found
  }
  return {
    ceremonyKind: prompt.ceremonyKind,
    relyingPartyId: prompt.relyingPartyId,
    pageOrigin: prompt.context.origin,
    userName: prompt.userName,
    isUnlocked: passwordsState.isUnlocked,
    // The site insists on the person proving who they are, and they have not
    // typed the passphrase lately: the question asks for it again.
    isVerificationNeeded:
      passwordsState.isUnlocked && prompt.userVerification === 'required' && !passwordsState.wasUserVerifiedRecently,
    choices,
  }
}

// isPageStillThere says whether the page that asked is still where it was:
// the question is still waiting, and the tab has not moved.
async function isPageStillThere(prompt: PendingPrompt): Promise<boolean> {
  if (passkeyPrompts.get(prompt.id) !== prompt) return false
  const tabUrl = await readTabUrl(prompt.tabId)
  return tabUrl !== null && withoutFragment(tabUrl) === withoutFragment(prompt.tabUrl)
}

async function promptDecide(
  promptId: string,
  promptDecision: 'allow' | 'browser',
  choiceCredentialId: string,
): Promise<{ isDone: true } | { error: string; errorCode?: PasswordsErrorCode | 'verificationNeeded' }> {
  const prompt = passkeyPrompts.get(promptId)
  if (!prompt) return { error: 'This question has already been answered.' }
  if (promptDecision === 'browser') {
    // Not asked again on this site until the browser closes.
    await rememberBrowserSite(prompt.context.origin)
    endPrompt(promptId, { replyOutcome: 'fallback' })
    return { isDone: true }
  }
  if (prompt.isDeciding) return { error: 'Working on it. One moment.' }
  prompt.isDeciding = true
  try {
    const passwordsState = await readPasswordsState(environment)
    const isUserVerified = passwordsState.isUnlocked && passwordsState.wasUserVerifiedRecently
    // A site that insists on the person proving who they are gets no answer
    // from someone who did not: the question asks for the passphrase, and
    // closing it answers the site with NotAllowedError.
    if (passwordsState.isUnlocked && prompt.userVerification === 'required' && !isUserVerified) {
      prompt.isDeciding = false
      return {
        error: 'This site asks you to prove it is you. Type your passphrase to go on.',
        errorCode: 'verificationNeeded',
      }
    }
    const reply = await withOpenedKeys(environment, async (keys): Promise<PasskeyReply> => {
      const items = await openItems(environment, keys)
      try {
        if (prompt.creation) {
          const request = prompt.creation.request
          const logins = loginsOf(items)
          const existing = passkeysForRelyingParty(
            logins,
            request.relyingPartyId,
            request.excludedCredentialIds,
            credentialIdOf,
          )
          if (request.excludedCredentialIds.length > 0 && existing.length > 0) {
            return {
              replyOutcome: 'error',
              errorName: 'InvalidStateError',
              errorMessage: 'A passkey for this account already exists.',
            }
          }
          const made = await makePasskey(request, prompt.context, isUserVerified)
          // A page that was closed or went elsewhere while this was worked
          // out is not given a passkey, and none is saved for it.
          if (!(await isPageStillThere(prompt))) throw new Error('The page that asked is gone.')
          // Saved before the site gets the credential, so a passkey the site
          // keeps is never one that was lost on the way.
          await savePasskey(environment, keys, logins, {
            pageUrl: prompt.context.pageUrl,
            relyingPartyName: request.relyingPartyName,
            passkey: made.passkeyFields,
          })
          return { replyOutcome: 'credential', credential: made.credential }
        }
        const request = prompt.assertion!.request
        const choices = passkeyChoices(items, request.relyingPartyId, request.allowedCredentialIds)
        const chosen =
          choices.find((choice) => credentialIdOf(choice.passkey) === choiceCredentialId) ??
          (choices.length === 1 ? choices[0] : null)
        if (!chosen) throw new Error('Choose which passkey to use.')
        if (!(await isPageStillThere(prompt))) throw new Error('The page that asked is gone.')
        return {
          replyOutcome: 'credential',
          credential: await signWithPasskey(request, chosen.passkey, prompt.context, isUserVerified),
        }
      } finally {
        releaseItems(items)
      }
    })
    endPrompt(promptId, reply)
    return { isDone: true }
  } catch (error) {
    // Locked or failed: the question stays, says why, and can still hand the
    // request to the browser.
    prompt.isDeciding = false
    return describeError(error)
  }
}

// ---------------------------------------------------------------------------
// From the extension's own pages

// isOtherOrigin says two addresses are on different origins. An address that
// cannot be read counts as none.
function isOtherOrigin(first: string, second: string): boolean {
  try {
    return first !== '' && second !== '' && new URL(first).origin !== new URL(second).origin
  } catch {
    return false
  }
}

// readFrameFacts asks the content script in the frame whether some page above
// the frame has another origin (it looks at every ancestor, not only the top
// page) and what the top page's host is. A frame that does not answer (no
// content script, a sandboxed document) is read from the tab's address alone.
async function readFrameFacts(
  tabId: number,
  frameId: number,
): Promise<{ isCrossOrigin: boolean; topHost: string } | null> {
  try {
    const answer = await chrome.tabs.sendMessage(tabId, { type: 'passwords:frame-facts' }, { frameId })
    if (answer && typeof answer.isCrossOrigin === 'boolean' && typeof answer.topHost === 'string') {
      return { isCrossOrigin: answer.isCrossOrigin, topHost: answer.topHost }
    }
  } catch {
    // The frame has no content script.
  }
  return null
}

async function frameTargetState(tabId: number | undefined, frameId: number | undefined) {
  if (tabId === undefined) return { url: '', frameHost: '', topHost: '', isFrameCrossOrigin: false }
  const url = (await describeFrame(tabId, frameId ?? 0)) ?? ''
  const topUrl = await chrome.tabs
    .get(tabId)
    .then((tab: { url?: string }) => tab.url ?? '')
    .catch(() => '')
  const facts = url ? await readFrameFacts(tabId, frameId ?? 0) : null
  // Without an answer the frame counts as cross-origin when its address is
  // not the tab's; with one, a middle frame on another origin counts too.
  const isFrameCrossOrigin = facts ? facts.isCrossOrigin || isOtherOrigin(url, topUrl) : isOtherOrigin(url, topUrl)
  return {
    url,
    frameHost: url ? normalizeHost(url) : '',
    topHost: facts && facts.topHost !== '' ? facts.topHost : topUrl ? normalizeHost(topUrl) : '',
    isFrameCrossOrigin,
  }
}

async function popupState(tabId: number | undefined, frameId: number | undefined): Promise<PopupState> {
  const signedIn = await isSignedIn()
  let isSetUp = false
  if (signedIn) {
    try {
      isSetUp = (await readPasswordsSetup(environment)).isSetUp
      await rememberIsSetUp(isSetUp)
    } catch {
      isSetUp = (await chrome.storage.local.get(SET_UP_FLAG))[SET_UP_FLAG] === true
    }
  }
  // The dropdown names its tab; the frame in it to fill is worked out here.
  const resolvedFrameId = tabId === undefined ? 0 : (frameId ?? (await frameToFill(tabId)))
  const target = await frameTargetState(tabId, resolvedFrameId)
  const captured = tabId === undefined ? null : capturedLogins.peek(tabId)
  const offer = captured && captured.password !== '' ? await describeOffer(captured).catch(() => null) : null
  // A mark left from a login that is gone (expired, or saved as it is) goes.
  if (tabId !== undefined && !offer) await unmarkSaveOffer(tabId)
  const passwordsState = await readPasswordsState(environment)
  return {
    isSignedIn: signedIn,
    isSetUp,
    isUnlocked: passwordsState.isUnlocked,
    serverOrigin: await readServerOrigin(),
    frameId: resolvedFrameId,
    frameHost: target.frameHost,
    topHost: target.topHost,
    isFrameCrossOrigin: target.isFrameCrossOrigin,
    hasPendingSave: offer !== null,
    pendingSave: offer ? { host: offer.host, userName: offer.userName, offerAction: offer.offerAction } : null,
  }
}

async function search(query: string, tabId: number | undefined, frameId: number | undefined): Promise<SearchAnswer> {
  const target = await frameTargetState(tabId, frameId)
  return withOpenedKeys(environment, async (keys) => {
    const report: OpenReport = { ownerKeyChanges: [] }
    const items = await openItems(environment, keys, report)
    try {
      const logins = loginsOf(items)
      const words = query.trim().toLowerCase()
      // The dropdown opens with the page's host in its search field: that
      // asks for the logins saved for the page, by the same match as filling
      // uses (a login saved for the site the page is under counts), and no
      // other. Anything else typed searches every login by its words.
      const isThisSiteAsked = words !== '' && words === target.frameHost.toLowerCase()
      const matchesWords = (login: LoginItem) =>
        words === '' ||
        [login.fields.name, login.fields.userName, ...login.fields.sites].join('\n').toLowerCase().includes(words)
      const forThisSite = target.url
        ? sortLogins(loginsForPage(logins, target.url)).filter((login) => isThisSiteAsked || matchesWords(login))
        : []
      const forThisSiteIds = new Set(forThisSite.map((login) => login.id))
      const others = isThisSiteAsked
        ? []
        : sortLogins(logins).filter((login) => !forThisSiteIds.has(login.id) && matchesWords(login))
      return {
        forThisSite: forThisSite.map(summaryOf),
        others: others.slice(0, 100).map(summaryOf),
        ownerKeyChanges: await describeOwnerKeyChanges(report.ownerKeyChanges, keys.keyPair.publicKey),
      }
    } finally {
      releaseItems(items)
    }
  })
}

async function readLogin<Result>(itemId: string, read: (login: LoginItem) => Promise<Result>): Promise<Result> {
  return withOpenedKeys(environment, async (keys) => {
    const items = await openItems(environment, keys)
    try {
      const login = loginsOf(items).find((candidate) => candidate.id === itemId)
      if (!login) throw new PasswordsError('itemUnknown')
      return await read(login)
    } finally {
      releaseItems(items)
    }
  })
}

async function fillInTab(message: Extract<PopupRequest, { type: 'passwords:fill-in-tab' }>): Promise<FillInTabAnswer> {
  const url = await describeFrame(message.tabId, message.frameId)
  if (!url) throw new Error('There is no page to fill here.')
  if (await isOwnPasswordsPage(url)) throw new Error(OWN_PAGE_REFUSAL.error)
  const pageOrigin = originOf(url)
  const fill = await readLogin(message.itemId, async (login) => {
    // A login that is not saved for the page is filled into it only after the
    // person is asked (the question names the page and the login's sites) and
    // says yes for this very origin. A page that moved since the question was
    // asked has another origin and is asked about again.
    if (!loginsForPage([login], url).length && message.confirmedOtherSiteOrigin !== pageOrigin) {
      const question: OtherSiteQuestion = {
        loginName: login.fields.name || login.fields.userName,
        origin: pageOrigin,
        savedSites: login.fields.sites.map((site) => originOfSite(site)).filter((origin) => origin !== ''),
        isPageEncrypted: isSecureContextPage(url),
      }
      return { question }
    }
    const currentCode = message.fillPart === 'oneTimeCode' ? await currentOneTimeCode(login, Date.now()) : null
    if (message.fillPart === 'oneTimeCode' && !currentCode) throw new Error('This login has no one-time code.')
    return {
      question: null,
      userName: message.fillPart === 'login' ? login.fields.userName : '',
      password: message.fillPart === 'login' ? login.fields.password : '',
      oneTimeCode: currentCode ? currentCode.oneTimeCode : '',
    }
  })
  if (fill.question) return { filledCount: 0, foreignActionHost: '', otherSite: fill.question }
  const fields: FillFieldsMessage = {
    type: 'passwords:fill-fields',
    origin: pageOrigin,
    isForeignActionConfirmed: message.isForeignActionConfirmed === true,
    userName: fill.userName ?? '',
    password: fill.password ?? '',
    oneTimeCode: fill.oneTimeCode ?? '',
    newPassword: '',
  }
  const answer = await chrome.tabs.sendMessage(message.tabId, fields, { frameId: message.frameId })
  return {
    filledCount: answer && typeof answer.filledCount === 'number' ? answer.filledCount : 0,
    foreignActionHost: answer && typeof answer.foreignActionHost === 'string' ? answer.foreignActionHost : '',
  }
}

// fillGenerated puts a password the dropdown's generator made into the new
// password fields of the page (the field and its confirmation). It is no
// stored secret, so a form that sends elsewhere is not asked about; it still
// goes to no page of the dashboard's own Passwords, and to no tab the agent is
// working in, where the page's own input looks like the person's.
async function fillGenerated(
  message: Extract<PopupRequest, { type: 'passwords:fill-generated' }>,
): Promise<FillInTabAnswer> {
  if (message.password === '') throw new Error('Generate a password first.')
  const url = await describeFrame(message.tabId, message.frameId)
  if (!url) throw new Error('There is no page to fill here.')
  if (await isOwnPasswordsPage(url)) throw new Error(OWN_PAGE_REFUSAL.error)
  if (await agentActivity.isAgentDriving(message.tabId)) throw new Error(AGENT_TAB_REFUSAL.error)
  const fields: FillFieldsMessage = {
    type: 'passwords:fill-fields',
    origin: originOf(url),
    isForeignActionConfirmed: true,
    userName: '',
    password: '',
    oneTimeCode: '',
    newPassword: message.password,
  }
  const answer = await chrome.tabs.sendMessage(message.tabId, fields, { frameId: message.frameId })
  return {
    filledCount: answer && typeof answer.filledCount === 'number' ? answer.filledCount : 0,
    foreignActionHost: '',
  }
}

async function handlePageRequest(message: PopupRequest): Promise<unknown> {
  switch (message.type) {
    case 'passwords:state':
      return popupState(message.tabId, message.frameId)
    case 'passwords:unlock':
      await unlockPasswords(environment, { secretText: message.secretText, isRecoveryKey: message.isRecoveryKey })
      await rememberIsSetUp(true)
      await startLockAlarm()
      return { isDone: true }
    case 'passwords:lock':
      await lockEverything()
      return { isDone: true }
    case 'passwords:search':
      return search(message.query, message.tabId, message.frameId)
    case 'passwords:copy': {
      const text = await readLogin(message.itemId, async (login) => {
        if (message.copiedField === 'password') return login.fields.password
        if (message.copiedField === 'userName') return login.fields.userName
        const currentCode = await currentOneTimeCode(login, Date.now())
        if (!currentCode) throw new Error('This login has no one-time code.')
        return currentCode.oneTimeCode
      })
      if (text === '') throw new Error('There is nothing to copy.')
      await copySecret(text)
      return { isDone: true }
    }
    case 'passwords:copy-text':
      // A password the generator made: it is not an item, but it is copied and
      // cleared the same way.
      if (message.text === '') throw new Error('There is nothing to copy.')
      await copySecret(message.text)
      return { isDone: true }
    case 'passwords:one-time-code':
      return readLogin(message.itemId, async (login) => {
        const currentCode = await currentOneTimeCode(login, Date.now())
        if (!currentCode) throw new Error('This login has no one-time code.')
        return currentCode
      })
    case 'passwords:fill-in-tab':
      return fillInTab(message)
    case 'passwords:fill-generated':
      return fillGenerated(message)
    case 'passwords:save-pending':
      return saveCaptured(message.tabId)
    case 'passwords:dismiss-pending':
      await forgetCaptured(message.tabId)
      return { isDone: true }
    case 'passwords:dismiss-owner-key':
      // The person dismissed the notice about an owner's changed key in the
      // window. The key stays the one used for the owner.
      await withOpenedKeys(environment, (keys) => dismissOwnerKeyNotice(environment, keys.userId, message.ownerUserId))
      return { isDone: true }
    case 'passwords:open-dashboard': {
      const origin = await readServerOrigin()
      if (origin) await chrome.tabs.create({ url: `${origin}/passwords` })
      return { isDone: true }
    }
    case 'passwords:prompt-info':
      return promptInfo(message.promptId)
    case 'passwords:prompt-decide':
      return promptDecide(message.promptId, message.promptDecision, message.choiceCredentialId)
  }
  return { error: 'Unknown request.' }
}

// ---------------------------------------------------------------------------
// Wiring

const CONTENT_TYPES = new Set([
  'passwords:field-focused',
  'passwords:captured',
  'passwords:page-ready',
  'passwords:passkey',
  'passwords:passkey-cancel',
])

// Messages with the passwords: prefix that another part of the service worker
// (background.js) answers. This listener leaves them alone: the first answer
// to a message is the one the sender gets, so answering "Unknown request." here
// would hide the real one.
const ANSWERED_ELSEWHERE = new Set(['passwords:clear-scripted-origins', 'passwords:scripted-origins'])

chrome.runtime.onMessage.addListener((message: any, sender: any, sendResponse: (answer: unknown) => void) => {
  if (!message || typeof message.type !== 'string' || !message.type.startsWith('passwords:')) return false
  if (ANSWERED_ELSEWHERE.has(message.type)) return false
  let work: Promise<unknown>
  if (CONTENT_TYPES.has(message.type)) {
    const frame = contentFrameOf(sender)
    work = frame ? handleContentRequest(frame, message as ContentRequest) : Promise.resolve({ error: 'Not a page.' })
  } else if (isOwnPage(sender)) {
    work = handlePageRequest(message as PopupRequest)
  } else {
    work = Promise.resolve({ error: 'Not allowed from here.' })
  }
  work.then(sendResponse, (error) => sendResponse(describeError(error)))
  return true
})

// Passkey requests that wait for the person get a content-script frame's
// answer later; a tab that is closed or navigated away ends its question.
chrome.tabs.onRemoved.addListener((tabId: number) => {
  capturedLogins.forget(tabId)
  focusedFrames.delete(tabId)
  void readSaveMarkedTabs()
    .then((marked) =>
      marked.includes(tabId)
        ? chrome.storage.session.set({ [SAVE_MARKED_TABS_KEY]: marked.filter((each) => each !== tabId) })
        : undefined,
    )
    .catch(() => {})
  agentActivity.forget(tabId)
  passkeyPausedUntilMilliseconds.delete(tabId)
  for (const key of cancelledAtMilliseconds.keys()) {
    if (key.startsWith(`${tabId}:`)) cancelledAtMilliseconds.delete(key)
  }
  const prompt = passkeyPrompts.findByTab(tabId)
  if (prompt) endPrompt(prompt.id, NOT_ALLOWED_REPLY)
})

// A page that finished loading after a sign-in was sent: if a login is waiting
// to be saved, the top frame is asked whether it still shows a password field
// (a sign-in that failed does), and otherwise the button is marked.
chrome.tabs.onUpdated.addListener(async (tabId: number, changeInfo: { status?: string; url?: string }) => {
  // A tab that went to another address ends the question its old page asked.
  if (typeof changeInfo.url === 'string') {
    const prompt = passkeyPrompts.findByTab(tabId)
    if (prompt && withoutFragment(changeInfo.url) !== withoutFragment(prompt.tabUrl)) {
      endPrompt(prompt.id, NOT_ALLOWED_REPLY)
    }
  }
  if (changeInfo.status !== 'complete') return
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '' || captured.hasBeenOffered) return
  try {
    const probe = await chrome.tabs.sendMessage(tabId, { type: 'passwords:probe' }, { frameId: 0 })
    if (!probe || probe.hasPasswordField !== false) return
    await offerToSave(tabId)
  } catch {
    // The page has no content script (an internal page): nothing to offer.
  }
})

chrome.alarms.onAlarm.addListener((alarm: { name: string }) => {
  if (alarm.name === LOCK_ALARM) {
    void lockIfIdle(environment)
  } else if (alarm.name === CLEAR_CLIPBOARD_ALARM) {
    void clearClipboardIfUnchanged().catch(() => {})
  }
})

// The keys in the session area end with the browser; the kept copy opens them
// again within the lock time. Signing out or signing in as somebody else ends
// both.
chrome.runtime.onStartup.addListener(() => {
  void lockEverything({ shouldKeepKeptCopy: true })
  void learnIfSetUp()
})
chrome.storage.onChanged.addListener((changes: Record<string, unknown>, area: string) => {
  if ((area === 'local' && changes.token) || (area === 'sync' && changes.server)) {
    void lockEverything()
    // What was kept about one person's passkeys is not for the next.
    void forgetPasskeySites(environment)
    void chrome.storage.local.remove(SET_UP_FLAG).then(learnIfSetUp)
  }
})

chrome.runtime.onInstalled.addListener(() => {
  void learnIfSetUp()
  chrome.contextMenus.removeAll(() => {
    chrome.contextMenus.create({ id: CONTEXT_MENU_ID, title: 'Fill from TeaNode Passwords', contexts: ['editable'] })
  })
})

chrome.contextMenus.onClicked.addListener(
  (info: { menuItemId: string; frameId?: number }, tab: { id?: number; windowId?: number } | undefined) => {
    if (info.menuItemId !== CONTEXT_MENU_ID || !tab || typeof tab.id !== 'number') return
    focusedFrames.set(tab.id, info.frameId ?? 0)
    void openPasswords(tab)
  },
)

// The keyboard shortcut opens the button's dropdown for the tab in front.
// The frame with the focus is read first: once the dropdown is open, the page
// no longer has it.
chrome.commands.onCommand.addListener(async (command: string) => {
  if (command !== 'fill-login') return
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true })
  if (!tab || typeof tab.id !== 'number') return
  const focusedFrameId = await frameWithFocus(tab.id)
  if (focusedFrameId !== null) focusedFrames.set(tab.id, focusedFrameId)
  await openPasswords(tab)
})

// A worker started again has lost the logins waiting to be saved: the marks
// for them go too.
void unmarkAllSaveOffers().catch(() => {})

// A worker started again after the lock time must not leave keys behind, and
// one started while Passwords is open keeps the alarm that locks it.
void lockIfIdle(environment).then(async (didLock) => {
  if (!didLock && (await readPasswordsState(environment)).isUnlocked) await startLockAlarm()
})
