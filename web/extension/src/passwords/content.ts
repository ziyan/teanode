// The content script: runs in every frame of every page, in the extension's
// isolated world (the page cannot see its variables or call its functions).
// It watches for a sign-in field to be focused and offers a suggestion under
// it in a frame of the extension's own page, fills a login on a click in that
// frame, notices a sign-in form being sent so the extension can offer to save
// it, and carries passkey requests from the page's own world (passkeyOverride)
// to the service worker.
//
// What it will not do: show anything on page load, offer a suggestion under a
// field a person cannot see, fill into a hidden field, act on a choice unless
// the frame is checked to be really in front of the person, or fill from a
// frame on another origin than the page around it without that being asked
// of the person in the frame.

import {
  FieldRole,
  findLoginScope,
  isFieldVisible,
  readSubmittedLogin,
  roleOfFocusedField,
  setFieldValue,
} from './fieldDetection'
import { fillSignInForm, findFormToFill } from './fillForm'
import { FrameMessage, HostedFrame } from './hostedFrame'
import { normalizeHost } from './hosts'
import type { BarOffer, FillFieldsMessage, ShowBarMessage, SuggestionAnswer } from './messages'
import { PASSKEY_CHANNEL } from './passkeyOverride'

// --- talking to the service worker --------------------------------------

async function send<Answer = any>(message: Record<string, unknown>): Promise<Answer | null> {
  try {
    return (await chrome.runtime.sendMessage(message)) ?? null
  } catch {
    // The extension was reloaded or updated under this page.
    return null
  }
}

// --- what this frame is --------------------------------------------------

const isTopFrame = window === window.top

// frameFacts says whether some page above this frame has another origin, and
// the host of the page at the top. The browser's own list of ancestor
// origins is used; a frame whose parent cannot be read is cross-origin.
function frameFacts(): { isCrossOrigin: boolean; topHost: string } {
  const ancestors = Array.from(window.location.ancestorOrigins ?? [])
  if (isTopFrame) return { isCrossOrigin: false, topHost: normalizeHost(window.location.href) }
  if (ancestors.length > 0) {
    return {
      isCrossOrigin: ancestors.some((origin) => origin !== window.location.origin),
      topHost: normalizeHost(ancestors[ancestors.length - 1]),
    }
  }
  try {
    return { isCrossOrigin: window.top?.location.origin !== window.location.origin, topHost: '' }
  } catch {
    return { isCrossOrigin: true, topHost: '' }
  }
}

// --- the suggestion under a field ---------------------------------------

let suggestion: HostedFrame | null = null
let suggestionField: HTMLInputElement | null = null
let lastLoginField: HTMLInputElement | null = null
let suggestionCount = 0

function closeSuggestion() {
  suggestionCount += 1
  suggestion?.close()
  suggestion = null
  suggestionField = null
}

function rolesOffered(role: FieldRole): boolean {
  return role === 'userName' || role === 'currentPassword' || role === 'newPassword' || role === 'oneTimeCode'
}

async function showSuggestion(field: HTMLInputElement, role: FieldRole) {
  closeSuggestion()
  const turn = suggestionCount
  const answer = await send<SuggestionAnswer>({ type: 'passwords:suggest', role })
  // The focus moved on while the service worker was thinking.
  if (turn !== suggestionCount || !answer || answer.state === 'none') return
  if (!field.isConnected || !isFieldVisible(field) || field.ownerDocument.activeElement !== field) return
  const facts = frameFacts()
  const hosted = new HostedFrame(document, {
    page: 'suggestion.html',
    widthPixels: Math.max(260, Math.min(field.getBoundingClientRect().width, 360)),
    heightPixels: 96,
    anchor: field,
    title: 'TeaNode Passwords',
    onMessage: (message) => void onSuggestionMessage(hosted, message, answer, role, facts),
  })
  suggestion = hosted
  suggestionField = field
  hosted.show()
}

async function onSuggestionMessage(
  hosted: HostedFrame,
  message: FrameMessage,
  answer: SuggestionAnswer,
  role: FieldRole,
  facts: { isCrossOrigin: boolean; topHost: string },
) {
  if (hosted !== suggestion) return
  if (message.kind === 'ready') {
    hosted.send({ kind: 'render', answer, role, host: normalizeHost(window.location.href), ...facts })
    return
  }
  if (message.kind === 'size' && typeof message.heightPixels === 'number') {
    hosted.resize(message.heightPixels)
    return
  }
  if (message.kind === 'close') {
    closeSuggestion()
    return
  }
  // Everything below acts on a click the frame reports. The frame said it was
  // trusted and visible; the page around it is asked as well.
  const check = hosted.check()
  if (!check.isShown) {
    closeSuggestion()
    return
  }
  const field = suggestionField
  if (!field || !field.isConnected) return
  if (message.kind === 'unlock') {
    await send({ type: 'passwords:open-popup' })
    closeSuggestion()
    return
  }
  // A frame on another origin than its page is filled only after the frame
  // asked the person and they said yes.
  if (facts.isCrossOrigin && message.isConfirmed !== true) return
  if (message.kind === 'choose' && typeof message.itemId === 'string') {
    const isCode = role === 'oneTimeCode'
    const secrets = await send<{ userName?: string; password?: string; code?: string; error?: string }>({
      type: isCode ? 'passwords:fill-code' : 'passwords:fill',
      itemId: message.itemId,
    })
    closeSuggestion()
    if (!secrets || secrets.error) return
    if (isCode) fillSignInForm(field, { oneTimeCode: secrets.code })
    else fillSignInForm(field, { userName: secrets.userName, password: secrets.password })
    return
  }
  if (message.kind === 'generate') {
    const generated = await send<{ password?: string; error?: string }>({ type: 'passwords:generate' })
    closeSuggestion()
    if (!generated || !generated.password) return
    // The password field and its confirmation get the same one.
    const scope = findLoginScope(field)
    const newFields = scope.newPasswordFields.length > 0 ? scope.newPasswordFields : [field]
    for (const newField of newFields) setFieldValue(newField, generated.password)
  }
}

document.addEventListener(
  'focusin',
  (event) => {
    const target = event.composedPath()[0]
    if (!(target instanceof HTMLInputElement)) {
      closeSuggestion()
      return
    }
    const role = roleOfFocusedField(target)
    if (!role || !rolesOffered(role)) {
      closeSuggestion()
      return
    }
    lastLoginField = target
    void showSuggestion(target, role)
  },
  true,
)

document.addEventListener(
  'pointerdown',
  (event) => {
    // A press anywhere on the page but the field itself closes the suggestion.
    // A press inside the frame never reaches this document.
    if (suggestion && event.composedPath()[0] !== suggestionField) closeSuggestion()
  },
  true,
)

document.addEventListener(
  'keydown',
  (event) => {
    if (event.key === 'Escape' && suggestion) closeSuggestion()
    if (
      event.key === 'Enter' &&
      event.isTrusted &&
      event.target instanceof HTMLInputElement &&
      event.target.type === 'password'
    ) {
      captureSubmission(event.target.form ?? document.body, event.target)
    }
  },
  true,
)

function keepFramesInPlace() {
  if (suggestion) {
    if (suggestionField && (!suggestionField.isConnected || !isFieldVisible(suggestionField))) closeSuggestion()
    else suggestion.reposition()
  }
  bar?.reposition()
}
window.addEventListener('scroll', keepFramesInPlace, true)
window.addEventListener('resize', keepFramesInPlace)

// --- saving after a sign-in ----------------------------------------------

let bar: HostedFrame | null = null

function closeBar() {
  bar?.close()
  bar = null
}

async function showBar(offer: BarOffer) {
  if (!isTopFrame) return
  closeBar()
  const hosted = new HostedFrame(document, {
    page: 'bar.html',
    widthPixels: 380,
    heightPixels: 120,
    anchor: null,
    title: 'TeaNode Passwords',
    onMessage: (message) => void onBarMessage(hosted, message, offer),
  })
  bar = hosted
  hosted.show()
}

async function onBarMessage(hosted: HostedFrame, message: FrameMessage, offer: BarOffer) {
  if (hosted !== bar) return
  if (message.kind === 'ready') {
    hosted.send({ kind: 'render', offer })
    return
  }
  if (message.kind === 'size' && typeof message.heightPixels === 'number') {
    hosted.resize(message.heightPixels)
    return
  }
  if (message.kind === 'close') {
    closeBar()
    return
  }
  if (
    message.kind !== 'answer' ||
    (message.answer !== 'save' && message.answer !== 'dismiss' && message.answer !== 'unlock')
  )
    return
  if (!hosted.check().isShown) {
    closeBar()
    return
  }
  const result = await send<{ outcome?: string; error?: string }>({
    type: 'passwords:bar-answer',
    answer: message.answer,
  })
  if (message.answer !== 'save') {
    closeBar()
    return
  }
  hosted.send({ kind: 'result', outcome: result?.outcome ?? 'error', error: result?.error ?? '' })
  if (result?.outcome === 'saved' || result?.outcome === 'updated' || result?.outcome === 'unchanged') {
    setTimeout(closeBar, 1800)
  }
}

let isWatchingForSignIn = false

// captureSubmission tells the service worker what was typed into a form that
// is being sent. The page may go away at once, so the form's values are read
// now and sent now.
function captureSubmission(scope: ParentNode, focused?: HTMLInputElement) {
  const submitted = readSubmittedLogin(scope, focused)
  if (!submitted) return
  void send({
    type: 'passwords:captured',
    userName: submitted.userName,
    password: submitted.password,
    isNewPassword: submitted.isNewPassword,
  })
  if (submitted.password !== '') watchForSignInEnd()
}

// watchForSignInEnd is for a page that signs in without leaving: when its
// password field is gone, the service worker is asked whether to offer a save.
function watchForSignInEnd() {
  if (isWatchingForSignIn || !isTopFrame) return
  isWatchingForSignIn = true
  let polls = 0
  const timer = setInterval(async () => {
    polls += 1
    const hasPasswordField = Array.from(document.querySelectorAll('input[type="password"]')).some((input) =>
      isFieldVisible(input as HTMLInputElement),
    )
    if (hasPasswordField && polls < 15) return
    clearInterval(timer)
    isWatchingForSignIn = false
    if (hasPasswordField) return
    const answer = await send<{ offer: BarOffer | null }>({ type: 'passwords:page-ready', hasPasswordField: false })
    if (answer?.offer) void showBar(answer.offer)
  }, 1000)
}

document.addEventListener(
  'submit',
  (event) => {
    if (event.target instanceof HTMLFormElement) captureSubmission(event.target)
  },
  true,
)

// A page with no form sends its sign-in from a button.
document.addEventListener(
  'click',
  (event) => {
    if (!event.isTrusted) return
    const target = event.composedPath()[0]
    if (!(target instanceof Element)) return
    const button = target.closest(
      'button, input[type="submit"], input[type="button"], input[type="image"], [role="button"]',
    )
    if (!button || button.closest('form')) return
    captureSubmission(document.body)
  },
  true,
)

// --- from the service worker ---------------------------------------------

chrome.runtime.onMessage.addListener((message: any, sender: any, sendResponse: (answer: unknown) => void) => {
  if (!message || sender.id !== chrome.runtime.id) return false
  if (message.type === 'passwords:probe') {
    sendResponse({
      hasPasswordField: Array.from(document.querySelectorAll('input[type="password"]')).some((input) =>
        isFieldVisible(input as HTMLInputElement),
      ),
    })
    return false
  }
  if (message.type === 'passwords:show-bar') {
    void showBar((message as ShowBarMessage).offer)
    sendResponse({})
    return false
  }
  if (message.type === 'passwords:fill-fields') {
    const fields = message as FillFieldsMessage
    // The secrets were approved for one host. A frame that has since moved
    // gets nothing.
    if (normalizeHost(window.location.href) !== fields.host) {
      sendResponse({ filledCount: 0 })
      return false
    }
    const anchor = findFormToFill(document, lastLoginField)
    sendResponse({
      filledCount: anchor
        ? fillSignInForm(anchor, {
            userName: fields.userName,
            password: fields.password,
            oneTimeCode: fields.oneTimeCode,
          })
        : 0,
    })
    return false
  }
  return false
})

// --- passkey requests from the page's own world --------------------------

function postToPage(data: Record<string, unknown>) {
  try {
    window.postMessage({ channel: PASSKEY_CHANNEL, ...data }, window.location.origin)
  } catch {
    // An opaque origin cannot be posted to; the page's script does not ask there.
  }
}

window.addEventListener('message', async (event) => {
  if (event.source !== window) return
  const data = event.data
  if (!data || typeof data !== 'object' || data.channel !== PASSKEY_CHANNEL || typeof data.id !== 'string') return
  if (data.direction === 'ping') {
    postToPage({ direction: 'pong', id: data.id })
    return
  }
  if (data.direction !== 'request' || (data.kind !== 'create' && data.kind !== 'get')) return
  // The origin and the frame facts are the browser's, added here. Whatever
  // the page's script said about them is not read.
  const answer = await send({
    type: 'passwords:passkey',
    kind: data.kind,
    options: typeof data.options === 'object' && data.options !== null ? data.options : {},
    isCrossOrigin: frameFacts().isCrossOrigin,
  })
  postToPage({
    direction: 'reply',
    id: data.id,
    reply: answer && typeof answer === 'object' && 'outcome' in answer ? answer : { outcome: 'fallback' },
  })
})
