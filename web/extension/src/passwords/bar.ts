// The bar at the top of a page after a sign-in: "Save this login to TeaNode
// Passwords?" or "Update the password for ada@example.test?", with Save and
// Not now. Like the suggestion, an extension page in a frame over the
// website that sends only choices made by trusted clicks while it is visible.

import { BarOffer } from './messages'
import { clear, element, onTrustedActivate } from './pageDom'
import { moveFocusWithKeys, startFramedPage } from './framedPage'

const root = document.getElementById('app') as HTMLElement
const page = startFramedPage(root, (data) => {
  if (data.frameMessageKind === 'render') renderOffer(data.offer as BarOffer)
  else if (data.frameMessageKind === 'result') showResult(data)
})
moveFocusWithKeys(root, page)
const statusLine = element('div', { className: 'status', role: 'status' })
let currentOffer: BarOffer | null = null

function renderOffer(offer: BarOffer) {
  currentOffer = offer
  clear(root)
  const question =
    offer.offerAction === 'update'
      ? `Update the password for ${offer.userName || offer.host}?`
      : offer.offerAction === 'unlock'
        ? 'Unlock TeaNode Passwords to save this login?'
        : 'Save this login to TeaNode Passwords?'
  const detail = offer.userName ? `${offer.userName} on ${offer.host}` : offer.host
  const confirm = element(
    'button',
    { className: 'primary', type: 'button' },
    offer.offerAction === 'update' ? 'Update' : offer.offerAction === 'unlock' ? 'Unlock' : 'Save',
  )
  const dismiss = element('button', { type: 'button' }, 'Not now')
  onTrustedActivate(confirm, page.isClickAllowed, () => {
    confirm.disabled = true
    statusLine.textContent = ''
    page.tell({ frameMessageKind: 'answer', barAnswer: offer.offerAction === 'unlock' ? 'unlock' : 'save' })
  })
  onTrustedActivate(dismiss, page.isClickAllowed, () => page.tell({ frameMessageKind: 'answer', barAnswer: 'dismiss' }))
  root.append(
    element(
      'div',
      { className: 'page' },
      element('strong', {}, question),
      element('div', { className: 'muted' }, detail),
      element('div', { className: 'actions' }, confirm, dismiss),
      statusLine,
    ),
  )
  page.reportHeight()
}

// The content script reports how the save went.
function showResult(data: Record<string, unknown>) {
  const confirm = root.querySelector('button.primary') as HTMLButtonElement | null
  if (confirm) confirm.disabled = false
  statusLine.className = 'status'
  if (data.saveOutcome === 'saved') {
    statusLine.textContent = 'Saved to TeaNode Passwords.'
    statusLine.className = 'status good'
  } else if (data.saveOutcome === 'updated') {
    statusLine.textContent = 'Password updated.'
    statusLine.className = 'status good'
  } else if (data.saveOutcome === 'unchanged') {
    statusLine.textContent = 'Already saved.'
  } else if (data.saveOutcome === 'locked') {
    statusLine.textContent = 'Passwords is locked. Unlock it to save.'
    statusLine.className = 'status bad'
    if (currentOffer) renderOffer({ ...currentOffer, offerAction: 'unlock' })
  } else if (data.saveOutcome === 'gone') {
    statusLine.textContent = 'There is nothing left to save.'
  } else {
    statusLine.textContent = typeof data.error === 'string' && data.error ? data.error : 'Could not save. Try again.'
    statusLine.className = 'status bad'
  }
  page.reportHeight()
}
