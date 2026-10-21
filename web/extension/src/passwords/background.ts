// Passwords in the extension's service worker: it answers the content
// scripts in pages and the extension's own pages, holds the opened keys (see
// vault.ts), and decides what each message may do from who sent it. A page
// can neither send these messages nor hear the answers; a content script is
// trusted for what the browser says about its frame (sender.url, sender.tab),
// never for what it claims, and every secret handed to one is checked against
// that frame's address first.

import { generatePassword } from '../../../src/passwords/generator'
import { PasswordsError, PasswordsErrorCode } from '../../../src/passwords/passwordsError'
import { LoginItem } from '../../../src/pages/passwords/passwordsProtocol'
import { CapturedLogins } from './capturedLogins'
import { CLEAR_CLIPBOARD_ALARM, clearClipboardIfUnchanged, copySecret } from './clipboard'
import { isWebPage, normalizeHost } from './hosts'
import {
  BarOffer,
  ContentRequest,
  FillFieldsMessage,
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
  loginsForPage,
  loginsOf,
  lockIfIdle,
  lockVault,
  openItems,
  passkeysForRelyingParty,
  readPasswordsSetup,
  readVaultState,
  releaseItems,
  saveCapturedLogin,
  savePasskey,
  sortLogins,
  StoredPasskey,
  summaryOf,
  unlockVault,
  VaultEnvironment,
  whatToDoWithCapturedLogin,
  withUnlockedVault,
} from './vault'
import {
  credentialIdOf,
  PageContext,
  parseAssertionRequest,
  parseCreationRequest,
  makePasskey,
  signWithPasskey,
} from './webauthn'

const environment: VaultEnvironment = {
  session: chrome.storage.session,
  local: chrome.storage.local,
  call: callServer,
  now: () => Date.now(),
}

const LOCK_ALARM = 'passwords-lock'
const SET_UP_FLAG = 'passwordsIsSetUp'
const CONTEXT_MENU_ID = 'passwords-fill'
const POPUP_PATH = '/passwords.html'
const PROMPT_PATH = '/prompt.html'
const OPTIONS_PATH = '/options.html'
const POPUP_SIZE = { width: 400, height: 600 }
const PROMPT_SIZE = { width: 440, height: 380 }

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

function describeError(error: unknown): { error: string; code?: PasswordsErrorCode } {
  if (error instanceof PasswordsError) return { error: ERROR_WORDS[error.code], code: error.code }
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
  let origin = typeof sender.origin === 'string' ? sender.origin : ''
  if (origin === '' || origin === 'null') {
    try {
      origin = new URL(url).origin
    } catch {
      return null
    }
  }
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

async function lockEverything() {
  // The login waiting to be saved goes first, and at once: it holds a password.
  capturedLogins.forgetAll()
  await lockVault(environment)
  await chrome.alarms.clear(LOCK_ALARM)
}

// ---------------------------------------------------------------------------
// Frames

async function describeFrame(tabId: number, frameId: number): Promise<string | null> {
  try {
    const [result] = await chrome.scripting.executeScript({
      target: { tabId, frameIds: [frameId] },
      func: () => location.href,
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
    passkeyPrompts.settle(prompt.id, NOT_ALLOWED_REPLY)
  }
})

// ---------------------------------------------------------------------------
// From a page: suggestions, filling, saving

async function suggest(frame: ContentFrame, role: string): Promise<SuggestionAnswer> {
  if (!(await isPasswordsAvailable())) return { state: 'none' }
  const host = normalizeHost(frame.url)
  const state = await readVaultState(environment)
  if (!state.isUnlocked) return { state: 'locked', host }
  if (role === 'newPassword') return { state: 'generate', host }
  try {
    // Looking is not use: a page left open with a field focused must still lock.
    return await withUnlockedVault(
      environment,
      async (keys) => {
        const items = await openItems(environment, keys)
        try {
          let logins = loginsForPage(loginsOf(items), frame.url)
          if (role === 'oneTimeCode') logins = logins.filter((login) => login.fields.oneTimeCodeUri !== '')
          if (logins.length === 0) return { state: 'none' } as SuggestionAnswer
          return { state: 'logins', host, logins: sortLogins(logins).map(summaryOf) } as SuggestionAnswer
        } finally {
          releaseItems(items)
        }
      },
      { isUse: false },
    )
  } catch {
    return { state: 'none' }
  }
}

// findLoginForPage is the login with this id if it is saved for the page,
// else it throws: a login is never handed to a page it is not saved for.
function findLoginForPage(items: Awaited<ReturnType<typeof openItems>>, itemId: string, pageUrl: string): LoginItem {
  const login = loginsForPage(loginsOf(items), pageUrl).find((candidate) => candidate.id === itemId)
  if (!login) throw new Error('This login is not saved for this site.')
  return login
}

async function readSecretForPage(frame: ContentFrame, itemId: string, which: 'login' | 'oneTimeCode') {
  return withUnlockedVault(environment, async (keys) => {
    const items = await openItems(environment, keys)
    try {
      const login = findLoginForPage(items, itemId, frame.url)
      if (which === 'login') return { userName: login.fields.userName, password: login.fields.password }
      const code = await currentOneTimeCode(login, Date.now())
      if (!code) throw new Error('This login has no one-time code.')
      return { code: code.code }
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
  const state = await readVaultState(environment)
  if (!state.isUnlocked) {
    capturedLogins.markOffered(tabId)
    return { action: 'unlock', host, userName: captured.userName }
  }
  const decision = await withUnlockedVault(
    environment,
    async (keys) => {
      const items = await openItems(environment, keys)
      try {
        return whatToDoWithCapturedLogin(loginsOf(items), captured).action
      } finally {
        releaseItems(items)
      }
    },
    { isUse: false },
  )
  if (decision === 'none') {
    capturedLogins.forget(tabId)
    return null
  }
  capturedLogins.markOffered(tabId)
  return { action: decision, host, userName: captured.userName }
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
): Promise<{ outcome: 'saved' | 'updated' | 'unchanged' | 'locked' | 'gone' }> {
  const captured = capturedLogins.peek(tabId)
  if (!captured || captured.password === '') return { outcome: 'gone' }
  if (!(await readVaultState(environment)).isUnlocked) return { outcome: 'locked' }
  const outcome = await withUnlockedVault(environment, async (keys) => {
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
  return { outcome }
}

async function handleContentRequest(frame: ContentFrame, message: ContentRequest): Promise<unknown> {
  switch (message.type) {
    case 'passwords:suggest':
      return suggest(frame, message.role)
    case 'passwords:fill':
      return readSecretForPage(frame, message.itemId, 'login')
    case 'passwords:fill-code':
      return readSecretForPage(frame, message.itemId, 'oneTimeCode')
    case 'passwords:generate':
      if (!(await readVaultState(environment)).isUnlocked) throw new PasswordsError('locked')
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
      if (message.answer === 'dismiss') {
        capturedLogins.forget(frame.tabId)
        return {}
      }
      if (message.answer === 'unlock') {
        await openPopupWindow({ tabId: frame.tabId, frameId: 0 })
        return {}
      }
      return saveCaptured(frame.tabId)
    case 'passwords:open-popup':
      await openPopupWindow({ tabId: frame.tabId, frameId: frame.frameId })
      return {}
    case 'passwords:passkey':
      return handlePasskey(frame, message)
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

async function startPrompt(
  frame: ContentFrame,
  context: PageContext,
  details: Pick<PendingPrompt, 'kind' | 'relyingPartyId' | 'userName' | 'creation' | 'assertion'>,
): Promise<PasskeyReply> {
  if (passkeyPrompts.isPendingForTab(frame.tabId)) return NOT_ALLOWED_REPLY
  const id = crypto.randomUUID()
  const reply = new Promise<PasskeyReply>((resolve) => {
    const prompt: PendingPrompt = {
      id,
      tabId: frame.tabId,
      windowId: null,
      context,
      resolve,
      timer: null,
      isDeciding: false,
      ...details,
    }
    prompt.timer = setTimeout(() => {
      const timedOut = passkeyPrompts.settle(id, NOT_ALLOWED_REPLY)
      if (timedOut) void closeWindow(timedOut.windowId)
    }, PROMPT_LIFETIME_MILLISECONDS)
    passkeyPrompts.add(prompt)
  })
  try {
    const made = await chrome.windows.create({
      url: chrome.runtime.getURL(`prompt.html?id=${id}`),
      type: 'popup',
      focused: true,
      ...PROMPT_SIZE,
    })
    const prompt = passkeyPrompts.get(id)
    if (prompt) prompt.windowId = made?.id ?? null
    else await closeWindow(made?.id ?? null)
  } catch {
    passkeyPrompts.settle(id, { outcome: 'fallback' })
  }
  return reply
}

async function handlePasskey(
  frame: ContentFrame,
  message: Extract<ContentRequest, { type: 'passwords:passkey' }>,
): Promise<PasskeyReply> {
  // A frame inside a page of another origin is the browser's to ask about:
  // whether it may use passkeys at all is a permission policy of the page
  // around it, which the browser enforces and this extension cannot see.
  if (message.isCrossOrigin || frame.isIncognito || !(await isPasswordsAvailable())) return { outcome: 'fallback' }
  const context: PageContext = { pageUrl: frame.url, origin: frame.origin, isCrossOrigin: false }
  if (message.kind === 'create') {
    const parsed = parseCreationRequest(message.options, context)
    if (parsed.outcome === 'fallback') return { outcome: 'fallback' }
    if (parsed.outcome === 'error') return { outcome: 'error', errorName: parsed.errorName, message: parsed.reason }
    return startPrompt(frame, context, {
      kind: 'create',
      relyingPartyId: parsed.request.relyingPartyId,
      userName: parsed.request.userName,
      creation: { request: parsed.request },
      assertion: null,
    })
  }
  const parsed = parseAssertionRequest(message.options, context)
  if (parsed.outcome === 'fallback') return { outcome: 'fallback' }
  if (parsed.outcome === 'error') return { outcome: 'error', errorName: parsed.errorName, message: parsed.reason }
  const request = parsed.request
  // Unlocked, a site Passwords has no passkey for is left to the browser with
  // no question. Locked, there is no way to know, so the window asks the
  // person to unlock (or to use the browser).
  if ((await readVaultState(environment)).isUnlocked) {
    const hasChoice = await withUnlockedVault(
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
    if (!hasChoice) return { outcome: 'fallback' }
  }
  return startPrompt(frame, context, {
    kind: 'get',
    relyingPartyId: request.relyingPartyId,
    userName: '',
    creation: null,
    assertion: { request },
  })
}

async function promptInfo(promptId: string): Promise<PromptInfo | { error: string }> {
  const prompt = passkeyPrompts.get(promptId)
  if (!prompt) return { error: 'This question has already been answered.' }
  const state = await readVaultState(environment)
  let choices: PromptInfo['choices'] = []
  if (state.isUnlocked && prompt.assertion) {
    const request = prompt.assertion.request
    const found = await withUnlockedVault(
      environment,
      async (keys) => {
        const items = await openItems(environment, keys)
        try {
          return passkeyChoices(items, request.relyingPartyId, request.allowedCredentialIds).map((choice) => ({
            label: choiceLabel(choice),
            credentialId: credentialIdOf(choice.passkey),
          }))
        } finally {
          releaseItems(items)
        }
      },
      { isUse: false },
    )
    // Unlocked in this window and nothing for this site: the browser takes over.
    if (found.length === 0) {
      const settled = passkeyPrompts.settle(promptId, { outcome: 'fallback' })
      if (settled) void closeWindow(settled.windowId)
      return { error: 'There is no passkey for this site here. The browser will ask instead.' }
    }
    choices = found
  }
  return {
    kind: prompt.kind,
    relyingPartyId: prompt.relyingPartyId,
    userName: prompt.userName,
    isUnlocked: state.isUnlocked,
    choices,
  }
}

async function promptDecide(
  promptId: string,
  decision: 'allow' | 'browser',
  choiceCredentialId: string,
): Promise<{ ok: true } | { error: string; code?: PasswordsErrorCode }> {
  const prompt = passkeyPrompts.get(promptId)
  if (!prompt) return { error: 'This question has already been answered.' }
  if (decision === 'browser') {
    const settled = passkeyPrompts.settle(promptId, { outcome: 'fallback' })
    if (settled) void closeWindow(settled.windowId)
    return { ok: true }
  }
  if (prompt.isDeciding) return { error: 'Working on it. One moment.' }
  prompt.isDeciding = true
  try {
    const reply = await withUnlockedVault(environment, async (keys): Promise<PasskeyReply> => {
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
              outcome: 'error',
              errorName: 'InvalidStateError',
              message: 'A passkey for this account already exists.',
            }
          }
          const made = await makePasskey(request, prompt.context)
          // Saved before the site gets the credential, so a passkey the site
          // keeps is never one that was lost on the way.
          await savePasskey(environment, keys, logins, {
            pageUrl: prompt.context.pageUrl,
            relyingPartyName: request.relyingPartyName,
            passkey: made.passkeyFields,
          })
          return { outcome: 'credential', credential: made.credential }
        }
        const request = prompt.assertion!.request
        const choices = passkeyChoices(items, request.relyingPartyId, request.allowedCredentialIds)
        const chosen =
          choices.find((choice) => credentialIdOf(choice.passkey) === choiceCredentialId) ??
          (choices.length === 1 ? choices[0] : null)
        if (!chosen) throw new Error('Choose which passkey to use.')
        return { outcome: 'credential', credential: await signWithPasskey(request, chosen.passkey, prompt.context) }
      } finally {
        releaseItems(items)
      }
    })
    const settled = passkeyPrompts.settle(promptId, reply)
    if (settled) void closeWindow(settled.windowId)
    return { ok: true }
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

async function frameTargetState(tabId: number | undefined, frameId: number | undefined) {
  if (tabId === undefined) return { url: '', frameHost: '', topHost: '', isFrameCrossOrigin: false }
  const url = (await describeFrame(tabId, frameId ?? 0)) ?? ''
  const topUrl = await chrome.tabs
    .get(tabId)
    .then((tab: { url?: string }) => tab.url ?? '')
    .catch(() => '')
  const isFrameCrossOrigin = isOtherOrigin(url, topUrl)
  return {
    url,
    frameHost: url ? normalizeHost(url) : '',
    topHost: topUrl ? normalizeHost(topUrl) : '',
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
  const state = await readVaultState(environment)
  return {
    isSignedIn: signedIn,
    isSetUp,
    isUnlocked: state.isUnlocked,
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
  return withUnlockedVault(environment, async (keys) => {
    const items = await openItems(environment, keys)
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
      return { forThisSite: forThisSite.map(summaryOf), others: others.slice(0, 100).map(summaryOf) }
    } finally {
      releaseItems(items)
    }
  })
}

async function readLogin<Result>(itemId: string, read: (login: LoginItem) => Promise<Result>): Promise<Result> {
  return withUnlockedVault(environment, async (keys) => {
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

async function fillInTab(message: Extract<PopupRequest, { type: 'passwords:fill-in-tab' }>) {
  const url = await describeFrame(message.tabId, message.frameId)
  if (!url) throw new Error('There is no page to fill here.')
  const fill = await readLogin(message.itemId, async (login) => {
    if (!loginsForPage([login], url).length) throw new Error('This login is not saved for this site.')
    const code = message.part === 'oneTimeCode' ? await currentOneTimeCode(login, Date.now()) : null
    if (message.part === 'oneTimeCode' && !code) throw new Error('This login has no one-time code.')
    return {
      userName: message.part === 'login' ? login.fields.userName : '',
      password: message.part === 'login' ? login.fields.password : '',
      oneTimeCode: code ? code.code : '',
    }
  })
  const fields: FillFieldsMessage = { type: 'passwords:fill-fields', host: normalizeHost(url), ...fill }
  const answer = await chrome.tabs.sendMessage(message.tabId, fields, { frameId: message.frameId })
  return { filledCount: answer && typeof answer.filledCount === 'number' ? answer.filledCount : 0 }
}

async function handlePageRequest(message: PopupRequest): Promise<unknown> {
  switch (message.type) {
    case 'passwords:state':
      return popupState(message.tabId, message.frameId)
    case 'passwords:unlock':
      await unlockVault(environment, { secretText: message.secretText, isRecoveryKey: message.isRecoveryKey })
      await rememberIsSetUp(true)
      await startLockAlarm()
      return { ok: true }
    case 'passwords:lock':
      await lockEverything()
      return { ok: true }
    case 'passwords:search':
      return search(message.query, message.tabId, message.frameId)
    case 'passwords:copy': {
      const text = await readLogin(message.itemId, async (login) => {
        if (message.field === 'password') return login.fields.password
        if (message.field === 'userName') return login.fields.userName
        const code = await currentOneTimeCode(login, Date.now())
        if (!code) throw new Error('This login has no one-time code.')
        return code.code
      })
      if (text === '') throw new Error('There is nothing to copy.')
      await copySecret(text)
      return { ok: true }
    }
    case 'passwords:copy-text':
      // A password the generator made: it is not an item, but it is copied and
      // cleared the same way.
      if (message.text === '') throw new Error('There is nothing to copy.')
      await copySecret(message.text)
      return { ok: true }
    case 'passwords:one-time-code':
      return readLogin(message.itemId, async (login) => {
        const code = await currentOneTimeCode(login, Date.now())
        if (!code) throw new Error('This login has no one-time code.')
        return code
      })
    case 'passwords:fill-in-tab':
      return fillInTab(message)
    case 'passwords:save-pending':
      return saveCaptured(message.tabId)
    case 'passwords:open-dashboard': {
      const origin = await readServerOrigin()
      if (origin) await chrome.tabs.create({ url: `${origin}/passwords` })
      return { ok: true }
    }
    case 'passwords:open-popup-window':
      await openPopupWindow({})
      return { ok: true }
    case 'passwords:prompt-info':
      return promptInfo(message.promptId)
    case 'passwords:prompt-decide':
      return promptDecide(message.promptId, message.decision, message.choiceCredentialId)
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
])

chrome.runtime.onMessage.addListener((message: any, sender: any, sendResponse: (answer: unknown) => void) => {
  if (!message || typeof message.type !== 'string' || !message.type.startsWith('passwords:')) return false
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
  const prompt = passkeyPrompts.findByTab(tabId)
  if (prompt) {
    const settled = passkeyPrompts.settle(prompt.id, NOT_ALLOWED_REPLY)
    if (settled) void closeWindow(settled.windowId)
  }
})

// A page that finished loading after a sign-in was sent: if a login is waiting
// to be saved, the top frame is asked whether it still shows a password field
// (a sign-in that failed does), and otherwise shows the bar.
chrome.tabs.onUpdated.addListener(async (tabId: number, info: { status?: string }) => {
  if (info.status !== 'complete') return
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
    void lockIfIdle(environment).then(async (didLock) => {
      if (didLock) await lockEverything()
    })
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
  if (!didLock && (await readVaultState(environment)).isUnlocked) await startLockAlarm()
})
