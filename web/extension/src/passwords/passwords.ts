// The Passwords window: a small popup window the extension opens from the
// suggestion, the keyboard shortcut, the context menu or the options page. It
// unlocks Passwords, lists the logins for the page it was opened for first and
// all of them after, fills a login into that page, copies a password, user
// name or one-time code, makes a password, and locks again. Everything it
// shows is asked of the service worker, which opens the items for the
// moment and lets them go; this page keeps no secret beyond what is on the
// screen while it is open.

import { DEFAULT_PASSWORD_OPTIONS, generatePassword, MAXIMUM_PASSWORD_LENGTH } from '../../../src/passwords/generator'
import { FillInTabAnswer, OtherSiteQuestion, PopupState, SearchAnswer } from './messages'
import { ask, AskError, clear, element, errorText } from './pageDom'
import type { LoginSummary, OwnerKeyChangeAnswer } from './openedKeys'

const root = document.getElementById('app') as HTMLElement
const parameters = new URLSearchParams(window.location.search)
const tabId = parameters.has('tab') ? Number(parameters.get('tab')) : undefined
const frameId = parameters.has('frame') ? Number(parameters.get('frame')) : undefined
const target = tabId === undefined ? {} : { tabId, frameId: frameId ?? 0 }

const statusLine = element('div', { className: 'status', role: 'status' })
let query = ''
let timer: ReturnType<typeof setInterval> | null = null

function say(text: string, tone: '' | 'good' | 'bad' = '') {
  statusLine.textContent = text
  statusLine.className = tone ? `status ${tone}` : 'status'
}

function stopTimer() {
  if (timer !== null) clearInterval(timer)
  timer = null
}

function header(popupState: PopupState | null): HTMLElement {
  const bar = element(
    'div',
    { className: 'header' },
    element('img', { className: 'mark', src: 'icons/icon48.png', alt: '', width: 22, height: 22 }),
    element('strong', {}, 'TeaNode Passwords'),
  )
  if (popupState?.isUnlocked) {
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
  let popupState: PopupState
  try {
    popupState = await ask<PopupState>({ type: 'passwords:state', ...target })
  } catch (error) {
    root.append(header(null), element('p', { className: 'status bad' }, errorText(error)))
    return
  }
  root.append(header(popupState))
  if (!popupState.isSignedIn)
    return showMessage('Sign in to TeaNode from the extension options first.', 'Open the options', () =>
      chrome.runtime.openOptionsPage(),
    )
  if (!popupState.isSetUp) {
    return showMessage(
      'Passwords is not set up yet. Set it up on the dashboard, with a passphrase and a recovery key.',
      'Open the dashboard',
      () => ask({ type: 'passwords:open-dashboard' }),
    )
  }
  if (!popupState.isUnlocked) return showUnlock()
  showLogins(popupState)
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
    statusLine,
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

async function showLogins(popupState: PopupState) {
  frameNote =
    popupState.isFrameCrossOrigin && popupState.frameHost
      ? `The sign-in form is in a frame from ${popupState.frameHost}. The page around it is ${popupState.topHost || 'another site'}.`
      : ''
  if (popupState.pendingSave) root.append(pendingSaveCard(popupState.pendingSave))
  if (popupState.isFrameCrossOrigin && popupState.frameHost) {
    root.append(
      element(
        'div',
        { className: 'card muted' },
        `The sign-in form is in a frame from ${popupState.frameHost}. The page around it is ${popupState.topHost || 'another site'}. Filling puts the login into that frame.`,
      ),
    )
  }
  const search = element('input', { type: 'search', placeholder: 'Search all logins', value: query, spellcheck: false })
  search.addEventListener('input', () => {
    query = search.value
    void loadList()
  })
  const listArea = element('div', { className: 'list-area' })
  root.append(search, listArea, generatorSection(), statusLine)
  async function loadList() {
    let answer: SearchAnswer
    try {
      answer = await ask<SearchAnswer>({ type: 'passwords:search', query, ...target })
    } catch (error) {
      if (error instanceof AskError && error.errorCode === 'locked') return void refresh()
      say(errorText(error), 'bad')
      return
    }
    clear(listArea)
    if (answer.forThisSite.length > 0) {
      listArea.append(
        element('h2', {}, `For ${popupState.frameHost || 'this site'}`),
        loginList(answer.forThisSite, popupState),
      )
    }
    if (answer.others.length > 0) {
      listArea.append(
        element('h2', {}, answer.forThisSite.length > 0 ? 'All other logins' : 'All logins'),
        loginList(answer.others, popupState),
      )
    }
    for (const change of answer.ownerKeyChanges) listArea.append(ownerKeyChangeCard(change, loadList))
    if (answer.ownerKeyChanges.length === 0 && answer.ownerKeyChangedCount > 0) {
      listArea.append(
        element(
          'div',
          { className: 'card muted' },
          answer.ownerKeyChangedCount === 1
            ? "A shared login is not shown. Its owner's key changed; open Passwords on the dashboard to check it."
            : `${answer.ownerKeyChangedCount} shared logins are not shown. Their owners' keys changed; open Passwords on the dashboard to check them.`,
        ),
      )
    }
    if (answer.forThisSite.length === 0 && answer.others.length === 0 && answer.ownerKeyChangedCount === 0) {
      listArea.append(element('p', { className: 'muted' }, query ? 'Nothing matches.' : 'There are no logins yet.'))
    } else if (query && answer.others.length === 0) {
      listArea.append(element('p', { className: 'muted' }, 'No other logins match.'))
    }
  }
  await loadList()
}

// ownerKeyChangeCard is the comparison the dashboard offers when the owner of
// shared logins has a key other than the one this browser kept: both safety
// codes, and a button that confirms the new key after the person compared
// them, in the words of the dashboard. The owner's logins stay closed until then.
function ownerKeyChangeCard(change: OwnerKeyChangeAnswer, reload: () => Promise<void>): HTMLElement {
  const name = change.ownerName || 'The person who shared logins with you'
  const confirm = element('button', { type: 'button', className: 'primary' }, 'The new safety code matches')
  confirm.addEventListener('click', () => {
    confirm.disabled = true
    void guarded(async () => {
      try {
        await ask({
          type: 'passwords:confirm-owner-key',
          ownerUserId: change.ownerUserId,
          newPublicKey: change.newPublicKey,
        })
      } catch (error) {
        confirm.disabled = false
        throw error
      }
      say('You confirmed this key.', 'good')
      await reload()
    })
  })
  return element(
    'div',
    { className: 'card', role: 'alert' },
    element('strong', {}, `${name}'s key changed`),
    element(
      'p',
      { className: 'muted' },
      `${name} shared logins with you, and their key is not the one you saw before. It changes when they set Passwords up again, but it could also be somebody else. Their logins stay closed until you confirm the new key.`,
    ),
    element(
      'p',
      { className: 'muted' },
      `Ask ${name} to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.`,
    ),
    element('div', {}, element('span', { className: 'muted' }, 'Safety code before '), codeText(change.oldSafetyCode)),
    element('div', {}, element('span', { className: 'muted' }, 'Safety code now '), codeText(change.newSafetyCode)),
    element('div', { className: 'actions' }, confirm),
  )
}

function codeText(safetyCode: string): HTMLElement {
  return safetyCode === ''
    ? element('span', { className: 'muted' }, 'could not be read')
    : element('span', { className: 'code' }, safetyCode)
}

function pendingSaveCard(pending: { host: string; userName: string }): HTMLElement {
  const save = element('button', { type: 'button', className: 'primary' }, 'Save')
  save.addEventListener('click', async () => {
    save.disabled = true
    try {
      const answer = await ask<{ saveOutcome: string }>({ type: 'passwords:save-pending', tabId })
      say(
        answer.saveOutcome === 'saved'
          ? 'Saved.'
          : answer.saveOutcome === 'updated'
            ? 'Password updated.'
            : 'Nothing to save.',
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

function loginList(logins: LoginSummary[], popupState: PopupState): HTMLElement {
  const list = element('div', { className: 'list' })
  for (const login of logins) list.append(loginRow(login, popupState))
  return list
}

async function guarded<Result>(work: () => Promise<Result>): Promise<Result | undefined> {
  try {
    return await work()
  } catch (error) {
    if (error instanceof AskError && error.errorCode === 'locked') {
      void refresh()
      return undefined
    }
    say(errorText(error), 'bad')
    return undefined
  }
}

function loginRow(login: LoginSummary, popupState: PopupState): HTMLElement {
  const row = element(
    'div',
    { className: 'row' },
    element('div', { className: 'name' }, login.name || login.siteAddress),
    login.userName ? element('div', { className: 'detail' }, login.userName) : null,
    // A login somebody else owns says whose it is.
    login.sharedByText ? element('div', { className: 'detail' }, login.sharedByText) : null,
  )
  const actions = element('div', { className: 'actions' })
  const codeArea = element('div', {})
  const add = (text: string, work: () => Promise<unknown>, className = '') => {
    const button = element('button', { type: 'button', className }, text)
    button.addEventListener('click', () => void guarded(work))
    actions.append(button)
  }
  if (tabId !== undefined && popupState.frameHost !== '') {
    add('Fill', () => fillInTab(login, 'login', false), 'primary')
  }
  if (login.hasPassword) add('Copy password', () => copy(login.id, 'password'))
  if (login.userName) add('Copy user name', () => copy(login.id, 'userName'))
  if (login.hasOneTimeCode) add('One-time code', () => showCode(login, codeArea, popupState))
  row.append(actions, codeArea)
  return row
}

// fillInTab fills the page the window was opened for. A form that sends what
// is typed to another host than the page's own is filled only after the
// person is told so and presses the button that says to go on. A login that
// is not saved for the page is filled only after the person is asked and says
// yes, for that very page (confirmedOtherSiteOrigin).
async function fillInTab(
  login: LoginSummary,
  fillPart: 'login' | 'oneTimeCode',
  isForeignActionConfirmed: boolean,
  confirmedOtherSiteOrigin?: string,
) {
  const answer = await ask<FillInTabAnswer>({
    type: 'passwords:fill-in-tab',
    itemId: login.id,
    fillPart,
    isForeignActionConfirmed,
    confirmedOtherSiteOrigin,
    tabId,
    frameId: frameId ?? 0,
  })
  forgetOtherSiteQuestion()
  if (answer.otherSite) return askAboutOtherSite(login, fillPart, answer.otherSite)
  if (answer.foreignActionHost) {
    const goOn = element('button', { type: 'button', className: 'primary' }, 'Fill anyway')
    goOn.addEventListener('click', () => void guarded(() => fillInTab(login, fillPart, true, confirmedOtherSiteOrigin)))
    statusLine.className = 'status bad'
    statusLine.textContent = `This form sends what you type to ${answer.foreignActionHost}, not to the page's own host. Nothing was filled.`
    statusLine.append(' ', goOn)
    return
  }
  if (answer.filledCount === 0) {
    return say(
      fillPart === 'login'
        ? 'No sign-in field was found on the page.'
        : 'No one-time code field was found on the page.',
      'bad',
    )
  }
  say('Filled.', 'good')
  setTimeout(() => window.close(), 500)
}

let otherSiteCard: HTMLElement | null = null
// frameNote says where the form is when it is in a frame from another site
// than the page around it, for the questions that name where a login goes.
let frameNote = ''

function forgetOtherSiteQuestion() {
  otherSiteCard?.remove()
  otherSiteCard = null
}

// askAboutOtherSite asks what the iPhone asks before it fills a login into a
// page the login is not saved for: "Fill <name> into <site>? It is saved for
// <its sites>." Nothing is filled until the person presses Fill.
function askAboutOtherSite(login: LoginSummary, fillPart: 'login' | 'oneTimeCode', question: OtherSiteQuestion) {
  say('')
  const fill = element('button', { type: 'button', className: 'primary' }, 'Fill')
  fill.addEventListener('click', () => {
    fill.disabled = true
    void guarded(() => fillInTab(login, fillPart, false, question.origin))
  })
  const cancel = element('button', { type: 'button' }, 'Cancel')
  cancel.addEventListener('click', forgetOtherSiteQuestion)
  const details = [
    question.savedSites.length > 0
      ? `It is saved for ${question.savedSites.join(', ')}.`
      : 'It is not saved for any site.',
  ]
  if (!question.isPageEncrypted) details.push('This page is not encrypted.')
  if (frameNote !== '') details.push(frameNote)
  const card = element(
    'div',
    { className: 'card', role: 'alert' },
    element('strong', {}, `Fill ${question.loginName} into ${question.origin}?`),
    element('div', { className: 'muted' }, details.join(' ')),
    element('div', { className: 'actions' }, fill, cancel),
  )
  otherSiteCard = card
  statusLine.before(card)
}

async function copy(itemId: string, copiedField: 'password' | 'userName' | 'oneTimeCode') {
  await ask({ type: 'passwords:copy', itemId, copiedField })
  say('Copied. The clipboard is cleared in 30 seconds if nothing else is copied.', 'good')
}

// showCode shows the one-time code with the seconds it holds for, and asks
// for the next one when it runs out.
async function showCode(login: LoginSummary, area: HTMLElement, popupState: PopupState) {
  stopTimer()
  const draw = async () => {
    const answer = await ask<{ oneTimeCode: string; secondsLeft: number }>({
      type: 'passwords:one-time-code',
      itemId: login.id,
    })
    let secondsLeft = answer.secondsLeft
    clear(area)
    const digits = element('span', { className: 'code' }, answer.oneTimeCode.replace(/(\d{3})(\d+)/, '$1 $2'))
    const countdown = element('span', { className: 'muted' }, ` ${secondsLeft}s left`)
    const copyButton = element('button', { type: 'button' }, 'Copy code')
    copyButton.addEventListener('click', () => void guarded(() => copy(login.id, 'oneTimeCode')))
    const codeActions = element('div', { className: 'actions' }, digits, countdown, copyButton)
    area.append(codeActions)
    if (tabId !== undefined && popupState.frameHost !== '') {
      const fill = element('button', { type: 'button' }, 'Fill code')
      fill.addEventListener('click', () => void guarded(() => fillInTab(login, 'oneTimeCode', false)))
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
