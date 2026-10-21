// The suggestion drawn under a sign-in field: an extension page in a frame
// over the website. It shows the logins saved for the site, or an invitation
// to unlock Passwords, or an offer of a generated password. A choice is a
// trusted click (a click a script made does nothing), made while the browser
// reports the frame fully visible; the content script then checks the frame
// is in front of the page before it fills anything.

import { SuggestionAnswer } from './messages'
import { clear, element, onTrustedActivate } from './pageDom'
import { startFramedPage } from './framedPage'

const root = document.getElementById('app') as HTMLElement
const page = startFramedPage(root, (data) => render(data as unknown as RenderData))

interface RenderData {
  answer: SuggestionAnswer
  role: string
  host: string
  isCrossOrigin: boolean
  topHost: string
}

function render(data: RenderData) {
  clear(root)
  const list = element('div', { className: 'page' })
  // The title, and a way out that does not depend on knowing the Escape key.
  const close = element('button', { className: 'quiet', type: 'button', title: 'Close' }, 'Close')
  close.addEventListener('click', () => page.tell({ kind: 'close' }))
  list.append(
    element('div', { className: 'frame-header' }, element('div', { className: 'muted' }, 'TeaNode Passwords'), close),
  )
  const answer = data.answer
  if (answer.state === 'locked') {
    const unlock = element('button', { className: 'primary', type: 'button' }, 'Unlock TeaNode Passwords')
    onTrustedActivate(unlock, page.isClickAllowed, () => page.tell({ kind: 'unlock' }))
    list.append(unlock)
  } else if (answer.state === 'generate') {
    const generate = element('button', { className: 'primary', type: 'button' }, 'Use a strong password')
    onTrustedActivate(generate, page.isClickAllowed, () =>
      page.tell({ kind: 'generate', isConfirmed: !data.isCrossOrigin }),
    )
    list.append(generate)
  } else if (answer.state === 'logins') {
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
        if (data.isCrossOrigin) askAboutFrame(list, data, login.id, login.userName || login.name)
        else page.tell({ kind: 'choose', itemId: login.id, isConfirmed: false })
      })
      list.append(choice)
    }
  }
  root.append(list)
  page.reportHeight()
}

// A sign-in form in a frame from another site than the page around it is
// filled only after the person was told which is which.
function askAboutFrame(list: HTMLElement, data: RenderData, itemId: string, label: string) {
  clear(list)
  list.append(
    element('p', {}, `Fill ${label} in this frame from ${data.host}?`),
    element(
      'p',
      { className: 'muted' },
      data.topHost ? `The page around it is ${data.topHost}.` : 'The page around it is another site.',
    ),
  )
  const actions = element('div', { className: 'actions' })
  const fill = element('button', { className: 'primary', type: 'button' }, 'Fill')
  const cancel = element('button', { type: 'button' }, 'Cancel')
  onTrustedActivate(fill, page.isClickAllowed, () => page.tell({ kind: 'choose', itemId, isConfirmed: true }))
  onTrustedActivate(cancel, page.isClickAllowed, () => page.tell({ kind: 'close' }))
  actions.append(fill, cancel)
  list.append(actions)
  page.reportHeight()
}
