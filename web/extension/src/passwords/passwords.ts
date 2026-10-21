// The Passwords window: a small popup window the extension opens from the
// suggestion, the keyboard shortcut, the context menu or the options page. It
// unlocks Passwords, lists the logins for the page it was opened for first and
// all of them after, fills a login into that page, copies a password, user
// name or one-time code, makes a password, and locks again. Everything it
// shows is asked of the service worker, which opens the items for the
// moment and lets them go; this page keeps no secret beyond what is on the
// screen while it is open.

import { DEFAULT_PASSWORD_OPTIONS, generatePassword, MAXIMUM_PASSWORD_LENGTH } from '../../../src/passwords/generator'
import { PopupState, SearchAnswer } from './messages'
import { ask, AskError, clear, element, errorText } from './pageDom'
import type { LoginSummary } from './vault'

const root = document.getElementById('app') as HTMLElement
const parameters = new URLSearchParams(window.location.search)
const tabId = parameters.has('tab') ? Number(parameters.get('tab')) : undefined
const frameId = parameters.has('frame') ? Number(parameters.get('frame')) : undefined
const target = tabId === undefined ? {} : { tabId, frameId: frameId ?? 0 }

const status = element('div', { className: 'status', role: 'status' })
let query = ''
let timer: ReturnType<typeof setInterval> | null = null

function say(text: string, tone: '' | 'good' | 'bad' = '') {
  status.textContent = text
  status.className = tone ? `status ${tone}` : 'status'
}

function stopTimer() {
  if (timer !== null) clearInterval(timer)
  timer = null
}

function header(state: PopupState | null): HTMLElement {
  const bar = element(
    'div',
    { className: 'header' },
    element('img', { className: 'mark', src: 'icons/icon48.png', alt: '', width: 22, height: 22 }),
    element('strong', {}, 'TeaNode Passwords'),
  )
  if (state?.isUnlocked) {
    const lock = element('button', { type: 'button', className: 'quiet' }, 'Lock')
    lock.addEventListener('click', async () => {
      await ask({ type: 'passwords:lock' })
      void refresh()
    })
    bar.append(lock)
  }
  return bar
}

async function refresh() {
  stopTimer()
  clear(root)
  let state: PopupState
  try {
    state = await ask<PopupState>({ type: 'passwords:state', ...target })
  } catch (error) {
    root.append(header(null), element('p', { className: 'status bad' }, errorText(error)))
    return
  }
  root.append(header(state))
  if (!state.isSignedIn)
    return showMessage('Sign in to TeaNode from the extension options first.', 'Open the options', () =>
      chrome.runtime.openOptionsPage(),
    )
  if (!state.isSetUp) {
    return showMessage(
      'Passwords is not set up yet. Set it up on the dashboard, with a passphrase and a recovery key.',
      'Open the dashboard',
      () => ask({ type: 'passwords:open-dashboard' }),
    )
  }
  if (!state.isUnlocked) return showUnlock()
  showLogins(state)
}

function showMessage(text: string, buttonText: string, onClick: () => unknown) {
  const button = element('button', { type: 'button', className: 'primary' }, buttonText)
  button.addEventListener('click', () => void onClick())
  root.append(element('p', {}, text), element('div', { className: 'actions' }, button))
}

// --- unlocking -------------------------------------------------------------

function showUnlock() {
  let isRecoveryKey = false
  const input = element('input', { type: 'password', autocomplete: 'off', spellcheck: false })
  const label = element('span', {}, 'Passphrase')
  const unlock = element('button', { type: 'submit', className: 'primary' }, 'Unlock')
  const switchKind = element('button', { type: 'button', className: 'quiet' }, 'Use the recovery key instead')
  switchKind.addEventListener('click', () => {
    isRecoveryKey = !isRecoveryKey
    label.textContent = isRecoveryKey ? 'Recovery key' : 'Passphrase'
    switchKind.textContent = isRecoveryKey ? 'Use the passphrase instead' : 'Use the recovery key instead'
    say('')
    input.value = ''
    input.focus()
  })
  const form = element(
    'form',
    {},
    element(
      'p',
      { className: 'muted' },
      'Unlock Passwords with the passphrase you chose. It never leaves this browser.',
    ),
    element('label', { className: 'field' }, label, input),
    element('div', { className: 'actions' }, unlock, switchKind),
    status,
  )
  form.addEventListener('submit', async (event) => {
    event.preventDefault()
    unlock.disabled = true
    say('Unlocking. This takes a moment.')
    try {
      await ask({ type: 'passwords:unlock', secretText: input.value, isRecoveryKey })
      input.value = ''
      say('')
      void refresh()
    } catch (error) {
      unlock.disabled = false
      say(errorText(error), 'bad')
    }
  })
  root.append(form)
  input.focus()
}

// --- the logins ------------------------------------------------------------

async function showLogins(state: PopupState) {
  if (state.pendingSave) root.append(pendingSaveCard(state.pendingSave))
  if (state.isFrameCrossOrigin && state.frameHost) {
    root.append(
      element(
        'div',
        { className: 'card muted' },
        `The sign-in form is in a frame from ${state.frameHost}. The page around it is ${state.topHost || 'another site'}. Filling puts the login into that frame.`,
      ),
    )
  }
  const search = element('input', { type: 'search', placeholder: 'Search all logins', value: query, spellcheck: false })
  search.addEventListener('input', () => {
    query = search.value
    void loadList()
  })
  const listArea = element('div', { className: 'list-area' })
  root.append(search, listArea, generatorSection(), status)
  async function loadList() {
    let answer: SearchAnswer
    try {
      answer = await ask<SearchAnswer>({ type: 'passwords:search', query, ...target })
    } catch (error) {
      if (error instanceof AskError && error.code === 'locked') return void refresh()
      say(errorText(error), 'bad')
      return
    }
    clear(listArea)
    if (answer.forThisSite.length > 0) {
      listArea.append(element('h2', {}, `For ${state.frameHost || 'this site'}`), loginList(answer.forThisSite, state))
    }
    if (answer.others.length > 0) {
      listArea.append(
        element('h2', {}, answer.forThisSite.length > 0 ? 'All other logins' : 'All logins'),
        loginList(answer.others, state),
      )
    }
    if (answer.forThisSite.length === 0 && answer.others.length === 0) {
      listArea.append(element('p', { className: 'muted' }, query ? 'Nothing matches.' : 'There are no logins yet.'))
    } else if (query && answer.others.length === 0) {
      listArea.append(element('p', { className: 'muted' }, 'No other logins match.'))
    }
  }
  await loadList()
}

function pendingSaveCard(pending: { host: string; userName: string }): HTMLElement {
  const save = element('button', { type: 'button', className: 'primary' }, 'Save')
  save.addEventListener('click', async () => {
    save.disabled = true
    try {
      const answer = await ask<{ outcome: string }>({ type: 'passwords:save-pending', tabId })
      say(
        answer.outcome === 'saved' ? 'Saved.' : answer.outcome === 'updated' ? 'Password updated.' : 'Nothing to save.',
        'good',
      )
      void refresh()
    } catch (error) {
      save.disabled = false
      say(errorText(error), 'bad')
    }
  })
  return element(
    'div',
    { className: 'card' },
    element('strong', {}, 'Save the login you just used?'),
    element('div', { className: 'muted' }, pending.userName ? `${pending.userName} on ${pending.host}` : pending.host),
    element('div', { className: 'actions' }, save),
  )
}

function loginList(logins: LoginSummary[], state: PopupState): HTMLElement {
  const list = element('div', { className: 'list' })
  for (const login of logins) list.append(loginRow(login, state))
  return list
}

async function guarded<Result>(work: () => Promise<Result>): Promise<Result | undefined> {
  try {
    return await work()
  } catch (error) {
    if (error instanceof AskError && error.code === 'locked') {
      void refresh()
      return undefined
    }
    say(errorText(error), 'bad')
    return undefined
  }
}

function loginRow(login: LoginSummary, state: PopupState): HTMLElement {
  const row = element(
    'div',
    { className: 'row' },
    element('div', { className: 'name' }, login.name || login.siteAddress),
    login.userName ? element('div', { className: 'detail' }, login.userName) : null,
  )
  const actions = element('div', { className: 'actions' })
  const codeArea = element('div', {})
  const add = (text: string, work: () => Promise<unknown>, className = '') => {
    const button = element('button', { type: 'button', className }, text)
    button.addEventListener('click', () => void guarded(work))
    actions.append(button)
  }
  if (tabId !== undefined && state.frameHost !== '') {
    add(
      'Fill',
      async () => {
        const answer = await ask<{ filledCount: number }>({
          type: 'passwords:fill-in-tab',
          itemId: login.id,
          part: 'login',
          tabId,
          frameId: frameId ?? 0,
        })
        if (answer.filledCount === 0) return say('No sign-in field was found on the page.', 'bad')
        say('Filled.', 'good')
        setTimeout(() => window.close(), 500)
      },
      'primary',
    )
  }
  if (login.hasPassword) add('Copy password', () => copy(login.id, 'password'))
  if (login.userName) add('Copy user name', () => copy(login.id, 'userName'))
  if (login.hasOneTimeCode) add('One-time code', () => showCode(login, codeArea, state))
  row.append(actions, codeArea)
  return row
}

async function copy(itemId: string, field: 'password' | 'userName' | 'oneTimeCode') {
  await ask({ type: 'passwords:copy', itemId, field })
  say('Copied. The clipboard is cleared in 30 seconds if nothing else is copied.', 'good')
}

// showCode shows the one-time code with the seconds it holds for, and asks
// for the next one when it runs out.
async function showCode(login: LoginSummary, area: HTMLElement, state: PopupState) {
  stopTimer()
  const draw = async () => {
    const answer = await ask<{ code: string; secondsLeft: number }>({
      type: 'passwords:one-time-code',
      itemId: login.id,
    })
    let secondsLeft = answer.secondsLeft
    clear(area)
    const digits = element('span', { className: 'code' }, answer.code.replace(/(\d{3})(\d+)/, '$1 $2'))
    const countdown = element('span', { className: 'muted' }, ` ${secondsLeft}s left`)
    const copyButton = element('button', { type: 'button' }, 'Copy code')
    copyButton.addEventListener('click', () => void guarded(() => copy(login.id, 'oneTimeCode')))
    const codeActions = element('div', { className: 'actions' }, digits, countdown, copyButton)
    area.append(codeActions)
    if (tabId !== undefined && state.frameHost !== '') {
      const fill = element('button', { type: 'button' }, 'Fill code')
      fill.addEventListener(
        'click',
        () =>
          void guarded(async () => {
            const filled = await ask<{ filledCount: number }>({
              type: 'passwords:fill-in-tab',
              itemId: login.id,
              part: 'oneTimeCode',
              tabId,
              frameId: frameId ?? 0,
            })
            if (filled.filledCount === 0) return say('No one-time code field was found on the page.', 'bad')
            say('Filled.', 'good')
            setTimeout(() => window.close(), 500)
          }),
      )
      codeActions.append(fill)
    }
    stopTimer()
    timer = setInterval(() => {
      secondsLeft -= 1
      countdown.textContent = ` ${Math.max(secondsLeft, 0)}s left`
      if (secondsLeft <= 0) void guarded(draw)
    }, 1000)
  }
  await draw()
}

// --- the generator -----------------------------------------------------------

function generatorSection(): HTMLElement {
  const length = element('input', {
    type: 'number',
    min: '8',
    max: String(MAXIMUM_PASSWORD_LENGTH),
    value: String(DEFAULT_PASSWORD_OPTIONS.length),
  })
  const checkbox = (text: string, isChecked: boolean) => {
    const input = element('input', { type: 'checkbox', checked: isChecked })
    return { input, label: element('label', { className: 'inline' }, input, text) }
  }
  const letters = checkbox('Letters', DEFAULT_PASSWORD_OPTIONS.shouldIncludeLetters)
  const digits = checkbox('Digits', DEFAULT_PASSWORD_OPTIONS.shouldIncludeDigits)
  const symbols = checkbox('Symbols', DEFAULT_PASSWORD_OPTIONS.shouldIncludeSymbols)
  const lookAlikes = checkbox('Avoid look-alikes', DEFAULT_PASSWORD_OPTIONS.shouldAvoidLookAlikes)
  const output = element('input', { type: 'text', readOnly: true, spellcheck: false })
  const make = element('button', { type: 'button' }, 'Generate')
  const copyButton = element('button', { type: 'button' }, 'Copy password')
  make.addEventListener('click', () => {
    try {
      output.value = generatePassword({
        length: Number(length.value),
        shouldIncludeLetters: letters.input.checked,
        shouldIncludeDigits: digits.input.checked,
        shouldIncludeSymbols: symbols.input.checked,
        shouldAvoidLookAlikes: lookAlikes.input.checked,
      })
      say('')
    } catch (error) {
      say(errorText(error), 'bad')
    }
  })
  copyButton.addEventListener(
    'click',
    () =>
      void guarded(async () => {
        if (output.value === '') return say('Generate a password first.', 'bad')
        await ask({ type: 'passwords:copy-text', text: output.value })
        say('Copied. The clipboard is cleared in 30 seconds if nothing else is copied.', 'good')
      }),
  )
  const section = element('details', {})
  section.append(
    element('summary', {}, 'Make a password'),
    element(
      'div',
      { className: 'page-section' },
      element('label', { className: 'field' }, element('span', {}, 'Length'), length),
      element('div', { className: 'actions' }, letters.label, digits.label, symbols.label, lookAlikes.label),
      output,
      element('div', { className: 'actions' }, make, copyButton),
    ),
  )
  return section
}

// --- the foot of the window -------------------------------------------------

const dashboardLink = element('button', { type: 'button' }, 'Open Passwords on the dashboard')
dashboardLink.addEventListener('click', () => void ask({ type: 'passwords:open-dashboard' }))
document.body.append(element('div', { className: 'page' }, dashboardLink))

void refresh()
