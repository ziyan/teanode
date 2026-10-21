// The suggestion drawn under a sign-in field: an extension page in a frame
// over the website. It shows the logins saved for the site, or an invitation
// to unlock Passwords, or an offer of a generated password. A choice is a
// trusted click (a click a script made does nothing), made while the browser
// reports the frame fully visible; the content script then checks the frame
// is in front of the page before it fills anything.

import { SuggestionAnswer } from './messages'
import { clear, element, onTrustedActivate } from './pageDom'
import { focusFirstChoice, moveFocusWithKeys, startFramedPage } from './framedPage'

const root = document.getElementById('app') as HTMLElement
const page = startFramedPage(root, (data) => {
  if (data.frameMessageKind === 'render') render(data as unknown as RenderData)
  else if (data.frameMessageKind === 'focus') focusFirstChoice(root)
})
moveFocusWithKeys(root, page)

interface RenderData {
  answer: SuggestionAnswer
  role: string
  host: string
  isCrossOrigin: boolean
  topHost: string
  // formActionHost is the host the form sends to when that is not the page's
  // own, else empty.
  formActionHost: string
}

// doesNeedConfirmation: the person is asked before anything is filled when
// the form is in a frame from another site, or sends what is typed to
// another host than the page's own.
function doesNeedConfirmation(data: RenderData): boolean {
  return data.isCrossOrigin || data.formActionHost !== ''
}

function render(data: RenderData) {
  clear(root)
  const list = element('div', { className: 'page' })
  // The title, and a way out that does not depend on knowing the Escape key.
  const close = element('button', { className: 'quiet', type: 'button', title: 'Close' }, 'Close')
  close.addEventListener('click', () => page.tell({ frameMessageKind: 'close' }))
  list.append(
    element('div', { className: 'frame-header' }, element('div', { className: 'muted' }, 'TeaNode Passwords'), close),
  )
  const answer = data.answer
  if (answer.suggestionState === 'locked') {
    const unlock = element('button', { className: 'primary', type: 'button' }, 'Unlock TeaNode Passwords')
    onTrustedActivate(unlock, page.isClickAllowed, () => page.tell({ frameMessageKind: 'unlock' }))
    list.append(unlock)
  } else if (answer.suggestionState === 'generate') {
    const generate = element('button', { className: 'primary', type: 'button' }, 'Use a strong password')
    onTrustedActivate(generate, page.isClickAllowed, () =>
      page.tell({ frameMessageKind: 'generate', isConfirmed: !doesNeedConfirmation(data) }),
    )
    list.append(generate)
  } else if (answer.suggestionState === 'logins') {
    for (const login of answer.logins) {
      const choice = element(
        'button',
        { className: 'choice', type: 'button' },
        element('span', { className: 'name' }, login.name || login.siteAddress),
        element(
          'span',
          { className: 'detail' },
          data.role === 'oneTimeCode'
            ? login.userName
              ? `${login.userName}, one-time code`
              : 'One-time code'
            : login.userName,
        ),
      )
      onTrustedActivate(choice, page.isClickAllowed, () => {
        if (doesNeedConfirmation(data)) askBeforeFilling(list, data, login.id, login.userName || login.name)
        else page.tell({ frameMessageKind: 'choose', itemId: login.id, isConfirmed: false })
      })
      list.append(choice)
    }
  }
  root.append(list)
  page.reportHeight()
}

// A sign-in form in a frame from another site than the page around it, or one
// that sends what is typed to another host than its page, is filled only after
// the person was told which is which.
function askBeforeFilling(list: HTMLElement, data: RenderData, itemId: string, label: string) {
  clear(list)
  if (data.isCrossOrigin) {
    list.append(
      element('p', {}, `Fill ${label} in this frame from ${data.host}?`),
      element(
        'p',
        { className: 'muted' },
        data.topHost ? `The page around it is ${data.topHost}.` : 'The page around it is another site.',
      ),
    )
  }
  if (data.formActionHost !== '') {
    list.append(
      element('p', {}, `Fill ${label} on ${data.host}?`),
      element(
        'p',
        { className: 'muted' },
        `This form sends what you type to ${data.formActionHost}, not to ${data.host}.`,
      ),
    )
  }
  const actions = element('div', { className: 'actions' })
  const fill = element('button', { className: 'primary', type: 'button' }, 'Fill')
  const cancel = element('button', { type: 'button' }, 'Cancel')
  onTrustedActivate(fill, page.isClickAllowed, () =>
    page.tell({ frameMessageKind: 'choose', itemId, isConfirmed: true }),
  )
  onTrustedActivate(cancel, page.isClickAllowed, () => page.tell({ frameMessageKind: 'close' }))
  actions.append(fill, cancel)
  list.append(actions)
  page.reportHeight()
}
