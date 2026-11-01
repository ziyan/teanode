// The content script: runs in every frame of every page, in the extension's
// isolated world (the page cannot see its variables or call its functions).
// It tells the service worker which frame holds the sign-in field the person
// focused, fills a login there when the person picks one in the toolbar
// button's dropdown, notices a sign-in form being sent so the dropdown can
// offer to save it, and carries passkey requests from the page's own world
// (passkeyOverride) to the service worker.
//
// It draws nothing on the page: everything the person sees and chooses is in
// the extension's own dropdown, which no website can cover or click. It never
// fills a hidden field.

import { FieldRole, isFieldVisible, readSubmittedLogin, roleOfFocusedField } from './fieldDetection'
import { fillSignInForm, findFormToFill, foreignFillActionHost } from './fillForm'
import { normalizeHost } from './hosts'
import { trust } from './pageDom'
import { isUserActive } from './userActivation'
import type { FillFieldsMessage } from './messages'
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

// isOpaqueOrigin: the document is sandboxed (a user's upload served with a
// sandbox policy, say), so its origin is "null" whatever site serves it. It
// is not that site's page: nothing is offered, filled or remembered in it.
function isOpaqueOrigin(): boolean {
  return window.origin === 'null'
}

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

// --- which frame holds the sign-in form ---------------------------------

// lastLoginField is the sign-in field the person last focused in this frame:
// the form a fill from the button's dropdown goes into.
let lastLoginField: HTMLInputElement | null = null

function isRoleFilled(role: FieldRole): boolean {
  return role === 'userName' || role === 'currentPassword' || role === 'newPassword' || role === 'oneTimeCode'
}

// A sign-in field getting the focus tells the service worker which frame of
// the tab holds the form, so the dropdown, opened from the toolbar where the
// page no longer has the focus, fills that frame. It carries nothing typed.
document.addEventListener(
  'focusin',
  (event) => {
    const target = event.composedPath()[0]
    if (!(target instanceof HTMLInputElement) || isOpaqueOrigin()) return
    const role = roleOfFocusedField(target)
    if (!role || !isRoleFilled(role)) return
    if (target === lastLoginField) return
    lastLoginField = target
    void send({ type: 'passwords:field-focused' })
  },
  true,
)

document.addEventListener(
  'keydown',
  (event) => {
    if (
      event.key === 'Enter' &&
      trust.isTrusted(event) &&
      event.target instanceof HTMLInputElement &&
      event.target.type === 'password'
    ) {
      captureSubmission(event.target.form ?? document.body, event.target)
    }
  },
  true,
)

// --- saving after a sign-in ----------------------------------------------

let isWatchingForSignIn = false

// captureSubmission tells the service worker what was typed into a form that
// is being sent. The page may go away at once, so the form's values are read
// now and sent now.
function captureSubmission(scope: ParentNode, focused?: HTMLInputElement) {
  // A page's script can send a form, or click its own button, without anybody
  // touching anything. Only a sign-in sent right after a key or a click the
  // person made is remembered as theirs.
  if (!isUserActive() || isOpaqueOrigin()) return
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
// password field is gone, the service worker is told, and marks the button
// with the offer to save.
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
    void send({ type: 'passwords:page-ready', hasPasswordField: false })
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
    if (!trust.isTrusted(event)) return
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
  // A sandboxed document is answered with nothing done.
  if (isOpaqueOrigin()) {
    sendResponse(message.type === 'passwords:fill-fields' ? { filledCount: 0 } : {})
    return false
  }
  if (message.type === 'passwords:probe') {
    sendResponse({
      hasPasswordField: Array.from(document.querySelectorAll('input[type="password"]')).some((input) =>
        isFieldVisible(input as HTMLInputElement),
      ),
    })
    return false
  }
  if (message.type === 'passwords:frame-facts') {
    // What the dropdown tells the person about this frame: every page above
    // it is looked at, not only the top one.
    sendResponse(frameFacts())
    return false
  }
  if (message.type === 'passwords:fill-fields') {
    const fields = message as FillFieldsMessage
    // The secrets were approved for one origin (scheme, host and port). A
    // frame that has since moved gets nothing.
    if (window.location.origin !== fields.origin) {
      sendResponse({ filledCount: 0 })
      return false
    }
    const anchor = findFormToFill(document, lastLoginField)
    if (!anchor) {
      sendResponse({ filledCount: 0 })
      return false
    }
    // A form that sends to another host than the page's own gets the login
    // only if the person was told so and said yes. A password the generator
    // just made is not a stored secret, and the worker sends it confirmed.
    const formActionHost = foreignFillActionHost(anchor)
    if (formActionHost !== '' && fields.isForeignActionConfirmed !== true) {
      sendResponse({ filledCount: 0, foreignActionHost: formActionHost })
      return false
    }
    sendResponse({
      filledCount: fillSignInForm(anchor, {
        userName: fields.userName,
        password: fields.password,
        oneTimeCode: fields.oneTimeCode,
        newPassword: fields.newPassword,
      }),
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
  if (data.direction === 'cancel') {
    // The page gave up on the request (an abort signal): the question in the
    // extension's window is withdrawn with it.
    void send({ type: 'passwords:passkey-cancel' })
    return
  }
  if (data.direction !== 'request' || (data.ceremonyKind !== 'create' && data.ceremonyKind !== 'get')) return
  // Only a request made right after a key or a click the person made is asked
  // about; a page's script asking on its own gets the browser's own handling.
  if (isOpaqueOrigin() || !isUserActive()) {
    postToPage({ direction: 'reply', id: data.id, reply: { replyOutcome: 'fallback' } })
    return
  }
  // The origin and the frame facts are the browser's, added here. Whatever
  // the page's script said about them is not read.
  const answer = await send({
    type: 'passwords:passkey',
    ceremonyKind: data.ceremonyKind,
    options: typeof data.options === 'object' && data.options !== null ? data.options : {},
    isCrossOrigin: frameFacts().isCrossOrigin,
  })
  postToPage({
    direction: 'reply',
    id: data.id,
    reply: answer && typeof answer === 'object' && 'replyOutcome' in answer ? answer : { replyOutcome: 'fallback' },
  })
})
