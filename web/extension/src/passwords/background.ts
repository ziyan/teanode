// Passwords in the extension's service worker: it answers the content
// scripts in pages and the extension's own pages, holds the opened keys (see
// openedKeys.ts), and decides what each message may do from who sent it. A page
// can neither send these messages nor hear the answers; a content script is
// trusted for what the browser says about its frame (sender.url, sender.tab),
// never for what it claims, and every secret handed to one is checked against
// that frame's address first.

import { generatePassword } from '../../../src/passwords/generator'
import { PasswordsError, PasswordsErrorCode } from '../../../src/passwords/passwordsError'
import { LoginItem } from '../../../src/pages/passwords/passwordsProtocol'
import { CapturedLogins } from './capturedLogins'
import { CLEAR_CLIPBOARD_ALARM, clearClipboardIfUnchanged, copySecret } from './clipboard'
import { isSecureContextPage, isWebPage, normalizeHost, originOf } from './hosts'
import {
  BarOffer,
  ContentRequest,
  FillFieldsMessage,
  FillInTabAnswer,
  OtherSiteQuestion,
  PopupRequest,
  PopupState,
  PromptInfo,
  SearchAnswer,
  ShowBarMessage,
  SuggestionAnswer,
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
  trustOwnerKey,
  unlockPasswords,
  PasswordsEnvironment,
  whatToDoWithCapturedLogin,
  withOpenedKeys,
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
  local: chrome.storage.local,
  call: callServer,
  now: () => Date.now(),
  // Whichever way the keys are dropped (by hand, by the lock time, by a change
  // of sign-in, at browser start), what is kept beside them goes too: the
  // login waiting to be saved holds a password, and the cache and the alarm
  // are only for open keys.
  onLock: async () => {
    capturedLogins.forgetAll()
    suggestionCache.clear()
    await chrome.alarms.clear(LOCK_ALARM)
  },
}

const LOCK_ALARM = 'passwords-lock'
const SET_UP_FLAG = 'passwordsIsSetUp'
const CONTEXT_MENU_ID = 'passwords-fill'
const POPUP_PATH = '/passwords.html'
const PROMPT_PATH = '/prompt.html'
const OPTIONS_PATH = '/options.html'
const POPUP_SIZE = { width: 400, height: 600 }
const PROMPT_SIZE = { width: 440, height: 380 }

// The suggestion for a field is asked for each time a field gets the focus, so
// a page that moves the focus about over and over would have every login
// opened over and over. What a field was told is kept for a few seconds,
// by origin and kind of field. It holds the same names a suggestion shows,
// never a password, and is dropped when Passwords locks or something is saved.
const SUGGESTION_CACHE_MILLISECONDS = 10_000
const SUGGESTION_CACHE_MOST_ENTRIES = 64
const suggestionCache = new Map<string, { expiresAtMilliseconds: number; answer: SuggestionAnswer }>()

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
// as a page of its own. Only those may unlock, copy or list: the suggestion and
// the bar are the extension's pages too, but they live in a frame inside a
// website, which a website can frame by itself, so they are not on this list
// and send nothing.
function isOwnPage(sender: any): boolean {
  if (!sender || sender.id !== chrome.runtime.id || sender.frameId !== 0) return false
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
// remembers the answer, so the suggestion knows whether to appear. It runs
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
    // Not reachable now; asked again when the window is opened.
  }
}

async function startLockAlarm() {
  await chrome.alarms.create(LOCK_ALARM, { periodInMinutes: 1 })
}

// lockEverything drops the keys; environment.onLock clears the rest.
async function lockEverything() {
  await lockPasswords(environment)
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
// keyboard focus in a field, or the top frame.
async function frameWithFocus(tabId: number): Promise<number> {
  try {
    const results = await chrome.scripting.executeScript({
      target: { tabId, allFrames: true },
      func: () => document.hasFocus() && !!document.activeElement && document.activeElement !== document.body,
    })
    const focused = results.find((result: { result: unknown }) => result.result === true)
    return focused ? focused.frameId : 0
  } catch {
    return 0
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

// openPopupWindow opens (or replaces) the window with the logins: for a page
// when there is one, for a save when there is a login waiting to be saved.
async function openPopupWindow(target: { tabId?: number; frameId?: number }) {
  await closeWindow(popupWindowId)
  const query = new URLSearchParams()
  if (target.tabId !== undefined) query.set('tab', String(target.tabId))
  if (target.frameId !== undefined) query.set('frame', String(target.frameId))
  const made = await chrome.windows.create({
    url: chrome.runtime.getURL(`passwords.html${query.toString() === '' ? '' : '?' + query.toString()}`),
    type: 'popup',
    focused: true,
    ...POPUP_SIZE,
  })
  popupWindowId = made?.id ?? null
}

chrome.windows.onRemoved.addListener((windowId: number) => {
  if (windowId === popupWindowId) popupWindowId = null
  const prompt = passkeyPrompts.findByWindow(windowId)
  if (prompt) {
    prompt.windowId = null
    settlePrompt(prompt.id, NOT_ALLOWED_REPLY)
  }
})

// ---------------------------------------------------------------------------
// From a page: suggestions, filling, saving

async function suggest(frame: ContentFrame, role: string): Promise<SuggestionAnswer> {
  if (!(await isPasswordsAvailable())) return { suggestionState: 'none' }
  const host = normalizeHost(frame.url)
  const passwordsState = await readPasswordsState(environment)
  if (!passwordsState.isUnlocked) return { suggestionState: 'locked', host }
  if (role === 'newPassword') return { suggestionState: 'generate', host }
  const cacheKey = `${frame.origin}\n${role}`
  const cached = suggestionCache.get(cacheKey)
  if (cached && cached.expiresAtMilliseconds > Date.now()) return cached.answer
  try {
    // Looking is not use: a page left open with a field focused must still lock.
    const answer = await withOpenedKeys(
      environment,
      async (keys) => {
        const items = await openItems(environment, keys)
        try {
          let logins = loginsForPage(loginsOf(items), frame.url)
          if (role === 'oneTimeCode') logins = logins.filter((login) => login.fields.oneTimeCodeUri !== '')
          if (logins.length === 0) return { suggestionState: 'none' } as SuggestionAnswer
          return {
            suggestionState: 'logins',
            host,
            logins: sortLogins(logins).map(summaryOf),
          } as SuggestionAnswer
        } finally {
          releaseItems(items)
        }
      },
      { isUse: false },
    )
    if (suggestionCache.size >= SUGGESTION_CACHE_MOST_ENTRIES) suggestionCache.clear()
    suggestionCache.set(cacheKey, { expiresAtMilliseconds: Date.now() + SUGGESTION_CACHE_MILLISECONDS, answer })
    return answer
  } catch {
    return { suggestionState: 'none' }
  }
}

// findLoginForPage is the login with this id if it is saved for the page,
// else it throws: a login is never handed to a page it is not saved for.
function findLoginForPage(items: Awaited<ReturnType<typeof openItems>>, itemId: string, pageUrl: string): LoginItem {
  const login = loginsForPage(loginsOf(items), pageUrl).find((candidate) => candidate.id === itemId)
  if (!login) throw new Error('This login is not saved for this site.')
  return login
}

async function readSecretForPage(frame: ContentFrame, itemId: string, secretPart: 'login' | 'oneTimeCode') {
  return withOpenedKeys(environment, async (keys) => {
    const items = await openItems(environment, keys)
    try {
      const login = findLoginForPage(items, itemId, frame.url)
      if (secretPart === 'login') return { userName: login.fields.userName, password: login.fields.password }
      const currentCode = await currentOneTimeCode(login, Date.now())
      if (!currentCode) throw new Error('This login has no one-time code.')
      return { oneTimeCode: currentCode.oneTimeCode }
    } finally {
      releaseItems(items)
    }
  })
}

// offerFor decides whether the bar is shown for the login captured in a tab,
// and what it says. It marks the capture as offered, so the next page does not
// show it again.
async function offerFor(tabId: number): Promise<BarOffer | null> {
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '' || captured.hasBeenOffered) return null
  const host = normalizeHost(captured.pageUrl)
  const passwordsState = await readPasswordsState(environment)
  if (!passwordsState.isUnlocked) {
    capturedLogins.markOffered(tabId)
    return { offerAction: 'unlock', host, userName: captured.userName }
  }
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
  if (offerAction === 'none') {
    capturedLogins.forget(tabId)
    return null
  }
  capturedLogins.markOffered(tabId)
  return { offerAction, host, userName: captured.userName }
}

// pageReady is a page that signs in without leaving asking whether to show
// the bar. Only the top frame shows it.
async function pageReady(frame: ContentFrame, hasPasswordField: boolean): Promise<{ offer: BarOffer | null }> {
  if (frame.frameId !== 0 || hasPasswordField) return { offer: null }
  return { offer: await offerFor(frame.tabId) }
}

// saveCaptured saves the login captured in a tab. Locked, it saves nothing
// and says so; the capture stays for the person to unlock and try again.
async function saveCaptured(
  tabId: number,
): Promise<{ saveOutcome: 'saved' | 'updated' | 'unchanged' | 'locked' | 'gone' }> {
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '') return { saveOutcome: 'gone' }
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
  capturedLogins.forget(tabId)
  suggestionCache.clear()
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

async function handleContentRequest(frame: ContentFrame, message: ContentRequest): Promise<unknown> {
  if (
    (message.type === 'passwords:suggest' ||
      message.type === 'passwords:fill' ||
      message.type === 'passwords:fill-code' ||
      message.type === 'passwords:generate' ||
      message.type === 'passwords:captured') &&
    (await isOwnPasswordsPage(frame.url))
  ) {
    if (message.type === 'passwords:suggest') return { suggestionState: 'none' }
    if (message.type === 'passwords:captured') return {}
    return OWN_PAGE_REFUSAL
  }
  switch (message.type) {
    case 'passwords:suggest':
      return suggest(frame, message.role)
    case 'passwords:fill':
      return readSecretForPage(frame, message.itemId, 'login')
    case 'passwords:fill-code':
      return readSecretForPage(frame, message.itemId, 'oneTimeCode')
    case 'passwords:generate':
      if (!(await readPasswordsState(environment)).isUnlocked) throw new PasswordsError('locked')
      return { password: generatePassword() }
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
    case 'passwords:bar-answer':
      if (message.barAnswer === 'dismiss') {
        capturedLogins.forget(frame.tabId)
        return {}
      }
      if (message.barAnswer === 'unlock') {
        await openPopupWindow({ tabId: frame.tabId, frameId: 0 })
        return {}
      }
      return saveCaptured(frame.tabId)
    case 'passwords:open-popup':
      await openPopupWindow({ tabId: frame.tabId, frameId: frame.frameId })
      return {}
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
// is not asked about again for a few seconds: a page cannot open the window
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

// endPrompt answers the page and closes the question's window.
function endPrompt(promptId: string, reply: PasskeyReply) {
  const settled = settlePrompt(promptId, reply)
  if (settled) void closeWindow(settled.windowId)
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
      context,
      resolve,
      timer: null,
      isDeciding: false,
      ...details,
    }
    prompt.timer = setTimeout(() => {
      const timedOut = settlePrompt(id, NOT_ALLOWED_REPLY)
      if (timedOut) void closeWindow(timedOut.windowId)
    }, PROMPT_LIFETIME_MILLISECONDS)
    passkeyPrompts.add(prompt)
  })
  try {
    // The window takes the keyboard focus only when the page that asked is the
    // tab the person is looking at, in a window that has the focus; otherwise
    // it opens without it, and a page in the background cannot pull the
    // person's typing into a window.
    const made = await chrome.windows.create({
      url: chrome.runtime.getURL(`prompt.html?id=${id}`),
      type: 'popup',
      focused: await readShouldFocusPrompt(frame.tabId),
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
  // passkey here makes the window ask the person to unlock.
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
    // typed the passphrase lately: the window asks for it again.
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
    // from someone who did not: the window asks for the passphrase, and
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
          suggestionCache.clear()
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
    // Locked or failed: the window stays, says why, and can still hand the
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
  const target = await frameTargetState(tabId, frameId)
  const pending = tabId === undefined ? null : capturedLogins.peek(tabId)
  const passwordsState = await readPasswordsState(environment)
  return {
    isSignedIn: signedIn,
    isSetUp,
    isUnlocked: passwordsState.isUnlocked,
    serverOrigin: await readServerOrigin(),
    frameHost: target.frameHost,
    topHost: target.topHost,
    isFrameCrossOrigin: target.isFrameCrossOrigin,
    hasPendingSave: pending !== null && pending.password !== '',
    pendingSave:
      pending && pending.password !== '' ? { host: normalizeHost(pending.pageUrl), userName: pending.userName } : null,
  }
}

async function search(query: string, tabId: number | undefined, frameId: number | undefined): Promise<SearchAnswer> {
  const target = await frameTargetState(tabId, frameId)
  return withOpenedKeys(environment, async (keys) => {
    const report: OpenReport = { ownerKeyChangedCount: 0, ownerKeyChanges: [] }
    const items = await openItems(environment, keys, report)
    try {
      const logins = loginsOf(items)
      const forThisSite = target.url ? sortLogins(loginsForPage(logins, target.url)) : []
      const forThisSiteIds = new Set(forThisSite.map((login) => login.id))
      const words = query.trim().toLowerCase()
      const others = sortLogins(logins).filter(
        (login) =>
          !forThisSiteIds.has(login.id) &&
          (words === '' ||
            [login.fields.name, login.fields.userName, ...login.fields.sites].join('\n').toLowerCase().includes(words)),
      )
      return {
        forThisSite: forThisSite.map(summaryOf),
        others: others.slice(0, 100).map(summaryOf),
        ownerKeyChangedCount: report.ownerKeyChangedCount,
        ownerKeyChanges: await describeOwnerKeyChanges(report.ownerKeyChanges),
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
        requestedSite: normalizeHost(url),
        origin: pageOrigin,
        savedSites: login.fields.sites.map((site) => normalizeHost(site)).filter((host) => host !== ''),
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
  }
  const answer = await chrome.tabs.sendMessage(message.tabId, fields, { frameId: message.frameId })
  return {
    filledCount: answer && typeof answer.filledCount === 'number' ? answer.filledCount : 0,
    foreignActionHost: answer && typeof answer.foreignActionHost === 'string' ? answer.foreignActionHost : '',
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
    case 'passwords:save-pending':
      return saveCaptured(message.tabId)
    case 'passwords:trust-owner-key':
      // The person compared the safety codes in the window and pressed the
      // button. Every list built from the old key is dropped with it.
      await withOpenedKeys(environment, (keys) =>
        trustOwnerKey(environment, keys, message.ownerUserId, message.newPublicKey),
      )
      suggestionCache.clear()
      return { isDone: true }
    case 'passwords:open-dashboard': {
      const origin = await readServerOrigin()
      if (origin) await chrome.tabs.create({ url: `${origin}/passwords` })
      return { isDone: true }
    }
    case 'passwords:open-popup-window':
      await openPopupWindow({})
      return { isDone: true }
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
  'passwords:suggest',
  'passwords:fill',
  'passwords:fill-code',
  'passwords:generate',
  'passwords:captured',
  'passwords:page-ready',
  'passwords:bar-answer',
  'passwords:open-popup',
  'passwords:passkey',
  'passwords:passkey-cancel',
])

// Messages with the passwords: prefix that another part of the service worker
// (background.js) answers. This listener leaves them alone: the first answer
// to a message is the one the sender gets, so answering "Unknown request." here
// would hide the real one.
const ANSWERED_ELSEWHERE = new Set(['passwords:clear-scripted-origins'])

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
  passkeyPausedUntilMilliseconds.delete(tabId)
  for (const key of cancelledAtMilliseconds.keys()) {
    if (key.startsWith(`${tabId}:`)) cancelledAtMilliseconds.delete(key)
  }
  const prompt = passkeyPrompts.findByTab(tabId)
  if (prompt) endPrompt(prompt.id, NOT_ALLOWED_REPLY)
})

// A page that finished loading after a sign-in was sent: if a login is waiting
// to be saved, the top frame is asked whether it still shows a password field
// (a sign-in that failed does), and otherwise shows the bar.
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
    const offer = await offerFor(tabId)
    if (offer)
      await chrome.tabs.sendMessage(tabId, { type: 'passwords:show-bar', offer } satisfies ShowBarMessage, {
        frameId: 0,
      })
  } catch {
    // The page has no content script (an internal page): nothing to show.
  }
})

chrome.alarms.onAlarm.addListener((alarm: { name: string }) => {
  if (alarm.name === LOCK_ALARM) {
    void lockIfIdle(environment)
  } else if (alarm.name === CLEAR_CLIPBOARD_ALARM) {
    void clearClipboardIfUnchanged().catch(() => {})
  }
})

// The keys end with the browser (the session storage does), and with signing
// out or signing in as somebody else.
chrome.runtime.onStartup.addListener(() => {
  void lockEverything()
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
  (info: { menuItemId: string; frameId?: number }, tab: { id?: number } | undefined) => {
    if (info.menuItemId !== CONTEXT_MENU_ID || !tab || typeof tab.id !== 'number') return
    void openPopupWindow({ tabId: tab.id, frameId: info.frameId ?? 0 })
  },
)

chrome.commands.onCommand.addListener(async (command: string) => {
  if (command !== 'fill-login') return
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true })
  if (!tab || typeof tab.id !== 'number') return
  await openPopupWindow({ tabId: tab.id, frameId: await frameWithFocus(tab.id) })
})

// A worker started again after the lock time must not leave keys behind, and
// one started while Passwords is open keeps the alarm that locks it.
void lockIfIdle(environment).then(async (didLock) => {
  if (!didLock && (await readPasswordsState(environment)).isUnlocked) await startLockAlarm()
})
