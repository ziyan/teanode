// The bar at the top of a page after a sign-in: "Save this login to TeaNode
// Passwords?" or "Update the password for ada@example.test?", with Save and
// Not now. Like the suggestion, an extension page in a frame over the
// website that sends only choices made by trusted clicks while it is visible.

import { BarOffer } from './messages'
import { clear, element, onTrustedActivate } from './pageDom'
import { startFramedPage } from './framedPage'

const root = document.getElementById('app') as HTMLElement
const page = startFramedPage(root, (data) => {
  if (data.kind === 'render') renderOffer(data.offer as BarOffer)
})
const status = element('div', { className: 'status', role: 'status' })
let currentOffer: BarOffer | null = null

function renderOffer(offer: BarOffer) {
  currentOffer = offer
  clear(root)
  const question =
    offer.action === 'update'
      ? `Update the password for ${offer.userName || offer.host}?`
      : offer.action === 'unlock'
        ? 'Unlock TeaNode Passwords to save this login?'
        : 'Save this login to TeaNode Passwords?'
  const detail = offer.userName ? `${offer.userName} on ${offer.host}` : offer.host
  const confirm = element(
    'button',
    { className: 'primary', type: 'button' },
    offer.action === 'update' ? 'Update' : offer.action === 'unlock' ? 'Unlock' : 'Save',
  )
  const dismiss = element('button', { type: 'button' }, 'Not now')
  onTrustedActivate(confirm, page.isClickAllowed, () => {
    confirm.disabled = true
    status.textContent = ''
    page.tell({ kind: 'answer', answer: offer.action === 'unlock' ? 'unlock' : 'save' })
  })
  onTrustedActivate(dismiss, page.isClickAllowed, () => page.tell({ kind: 'answer', answer: 'dismiss' }))
  root.append(
    element(
      'div',
      { className: 'page' },
      element('strong', {}, question),
      element('div', { className: 'muted' }, detail),
      element('div', { className: 'actions' }, confirm, dismiss),
      status,
    ),
  )
  page.reportHeight()
}

// The content script reports how the save went.
window.addEventListener('message', (event) => {
  if (event.source !== window.parent) return
  const data = event.data
  if (!data || data.kind !== 'result') return
  const confirm = root.querySelector('button.primary') as HTMLButtonElement | null
  if (confirm) confirm.disabled = false
  status.className = 'status'
  if (data.outcome === 'saved') {
    status.textContent = 'Saved to TeaNode Passwords.'
    status.className = 'status good'
  } else if (data.outcome === 'updated') {
    status.textContent = 'Password updated.'
    status.className = 'status good'
  } else if (data.outcome === 'unchanged') {
    status.textContent = 'Already saved.'
  } else if (data.outcome === 'locked') {
    status.textContent = 'Passwords is locked. Unlock it to save.'
    status.className = 'status bad'
    if (currentOffer) renderOffer({ ...currentOffer, action: 'unlock' })
  } else if (data.outcome === 'gone') {
    status.textContent = 'There is nothing left to save.'
  } else {
    status.textContent = typeof data.error === 'string' && data.error ? data.error : 'Could not save. Try again.'
    status.className = 'status bad'
  }
  page.reportHeight()
})
