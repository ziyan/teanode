// The toolbar button's dropdown: what a press on the TeaNode button, the
// keyboard shortcut or the context menu opens. It unlocks Passwords, lists the
// logins saved for the page in front (its search field starts with the page's
// host; clearing it lists every login), fills one into that page with Enter or
// a click, copies a password, user name or one-time code, offers to save a
// login just typed, makes a password, and locks again. Its first button opens
// the chat with the agent in the browser's side panel. Everything it shows is
// asked of the service worker, which opens the items for the moment and lets
// them go; this page keeps no secret beyond what is on the screen while it is
// open.
//
// Chrome draws the dropdown hanging from the button, where no website can
// cover or click it. Where it cannot (a window without a toolbar), the same
// page opens in a small window, with the tab it is for in its address.

import { DEFAULT_PASSWORD_OPTIONS, generatePassword, MAXIMUM_PASSWORD_LENGTH } from '../../../src/passwords/generator'
import { FillInTabAnswer, OtherSiteQuestion, PopupState, SearchAnswer } from './messages'
import { ask, AskError, clear, element, errorText } from './pageDom'
import type { LoginSummary, OwnerKeyChangeAnswer } from './openedKeys'

const root = document.getElementById('app') as HTMLElement
const parameters = new URLSearchParams(window.location.search)
// tabId is the tab the dropdown fills, frameId the frame in it that holds the
// sign-in form, which the service worker works out (see frameToFill).
let tabId: number | undefined = parameters.has('tab') ? Number(parameters.get('tab')) : undefined
let frameId: number | undefined
// windowId is this browser window, read on opening: the side panel must be
// opened in the same moment as the press that asks for it, with no wait.
let windowId: number | undefined

const statusLine = element('div', { className: 'status', role: 'status' })
// query is what the search field holds; it starts as the page's host the
// first time the logins are shown (hasQueryStarted).
let query = ''
let hasQueryStarted = false
let timer: ReturnType<typeof setInterval> | null = null

async function findTab() {
  try {
    const current = await chrome.windows.getCurrent()
    windowId = current.id
  } catch {
    // Not in a browser window.
  }
  if (tabId !== undefined) return
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true })
    if (tab && typeof tab.id === 'number') tabId = tab.id
  } catch {
    // No tab in front.
  }
}

function target(): { tabId?: number; frameId?: number } {
  if (tabId === undefined) return {}
  return frameId === undefined ? { tabId } : { tabId, frameId }
}

function say(text: string, tone: '' | 'good' | 'bad' = '') {
  statusLine.textContent = text
  statusLine.className = tone ? `status ${tone}` : 'status'
}

function stopTimer() {
  if (timer !== null) clearInterval(timer)
  timer = null
}

// openChat opens the chat with the agent in the side panel of this window and
// closes the dropdown. The side panel is the extension's own page, beside the
// website rather than on it.
function openChat() {
  const opening = windowId === undefined ? chrome.sidePanel.open({}) : chrome.sidePanel.open({ windowId })
  opening.then(
    () => window.close(),
    (error: unknown) => say(errorText(error), 'bad'),
  )
}

function header(popupState: PopupState | null): HTMLElement {
  const chat = element('button', { type: 'button', className: 'chat' }, 'Chat with TeaNode')
  chat.addEventListener('click', openChat)
  const bar = element(
    'div',
    { className: 'header' },
    element('img', { className: 'mark', src: 'icons/icon48.png', alt: '', width: 22, height: 22 }),
    element('strong', {}, 'TeaNode'),
    chat,
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
    popupState = await ask<PopupState>({ type: 'passwords:state', ...target() })
    if (tabId !== undefined) frameId = popupState.frameId
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
  // The search field starts with the page's host, which lists the logins
  // saved for the page and no other; clearing it lists all of them. The host
  // is selected, so typing replaces it.
  if (!hasQueryStarted) {
    query = popupState.frameHost
    hasQueryStarted = true
  }
  const search = element('input', {
    type: 'search',
    placeholder: 'Search all logins',
    value: query,
    spellcheck: false,
  })
  search.setAttribute('aria-label', 'Search logins')
  search.addEventListener('input', () => {
    query = search.value
    void loadList()
  })
  // The keyboard works from the search field: the first login is chosen,
  // the arrows choose another, and Enter fills the chosen one.
  search.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      choose(chosenIndex + (event.key === 'ArrowDown' ? 1 : -1))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const chosen = choices[chosenIndex]
      if (chosen) void guarded(() => fillInTab(chosen.login, 'login', false))
    }
  })
  const listArea = element('div', { className: 'list-area' })
  root.append(search, listArea, generatorSection(popupState), statusLine)
  search.focus()
  search.select()
  // choices are the rows that can be filled, in the order shown; chosenIndex
  // is the one Enter fills.
  let choices: { login: LoginSummary; row: HTMLElement }[] = []
  let chosenIndex = 0
  function choose(index: number) {
    if (choices.length === 0) return
    chosenIndex = Math.max(0, Math.min(choices.length - 1, index))
    choices.forEach((choice, each) => {
      choice.row.classList.toggle('is-chosen', each === chosenIndex)
      choice.row.setAttribute('aria-selected', String(each === chosenIndex))
    })
    choices[chosenIndex].row.scrollIntoView({ block: 'nearest' })
  }
  // listTurn is the search last asked for: an answer to an older one, which
  // can arrive after a newer one while somebody types, is dropped.
  let listTurn = 0
  async function loadList() {
    const turn = ++listTurn
    let answer: SearchAnswer
    try {
      answer = await ask<SearchAnswer>({ type: 'passwords:search', query, ...target() })
    } catch (error) {
      if (error instanceof AskError && error.errorCode === 'locked') return void refresh()
      say(errorText(error), 'bad')
      return
    }
    if (turn !== listTurn) return
    clear(listArea)
    choices = []
    chosenIndex = 0
    const canFill = tabId !== undefined && popupState.frameHost !== ''
    const addList = (logins: LoginSummary[]) => {
      const list = element('div', { className: 'list', role: 'listbox' })
      for (const login of logins) {
        const row = loginRow(login, popupState)
        if (canFill) choices.push({ login, row })
        list.append(row)
      }
      return list
    }
    // A changed key comes first: after up to 100 logins nobody would reach it.
    for (const change of answer.ownerKeyChanges) listArea.append(ownerKeyChangeCard(change, loadList))
    const isThisSiteAsked =
      popupState.frameHost !== '' && query.trim().toLowerCase() === popupState.frameHost.toLowerCase()
    if (answer.forThisSite.length > 0) {
      listArea.append(element('h2', {}, `For ${popupState.frameHost || 'this site'}`), addList(answer.forThisSite))
    }
    if (answer.others.length > 0) {
      listArea.append(
        element('h2', {}, answer.forThisSite.length > 0 ? 'Other logins' : query ? 'Logins' : 'All logins'),
        addList(answer.others),
      )
    }
    if (answer.forThisSite.length === 0 && answer.others.length === 0) {
      listArea.append(
        element(
          'p',
          { className: 'muted' },
          isThisSiteAsked
            ? `No login is saved for ${popupState.frameHost}. Clear the search to see every login.`
            : query
              ? 'Nothing matches.'
              : 'There are no logins yet.',
        ),
      )
    }
    choose(0)
  }
  await loadList()
}

// ownerKeyChangeCard is the notice about an owner of shared logins who has a
// key other than the one this browser used before. Nothing waits on it: their
// logins are listed and fill as usual. "Compare codes" shows both of their
// codes and the person's own, to compare with the owner; "Dismiss" removes the
// notice.
function ownerKeyChangeCard(change: OwnerKeyChangeAnswer, reload: () => Promise<void>): HTMLElement {
  const name = change.ownerName || 'The person who shared logins with you'
  const compare = element('button', { type: 'button' }, 'Compare codes')
  const dismiss = element('button', { type: 'button' }, 'Dismiss')
  const codes = element('div', {})
  compare.addEventListener('click', () => {
    clear(codes)
    codes.append(
      element(
        'div',
        {},
        element('span', { className: 'muted' }, `Safety code before `),
        codeText(change.oldSafetyCode),
      ),
      element('div', {}, element('span', { className: 'muted' }, `Safety code now `), codeText(change.newSafetyCode)),
      element('div', {}, element('span', { className: 'muted' }, 'Your safety code '), codeText(change.ownSafetyCode)),
      element(
        'p',
        { className: 'muted' },
        `Ask ${name} to open Passwords settings on their own device and read you their safety code. It should match the one now.`,
      ),
    )
    compare.disabled = true
  })
  dismiss.addEventListener('click', () => {
    dismiss.disabled = true
    void guarded(async () => {
      try {
        await ask({ type: 'passwords:dismiss-owner-key', ownerUserId: change.ownerUserId })
      } catch (error) {
        dismiss.disabled = false
        throw error
      }
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
      'This happens when they set Passwords up again. If you did not expect it, compare safety codes with them.',
    ),
    codes,
    element('div', { className: 'actions' }, compare, dismiss),
  )
}

function codeText(safetyCode: string): HTMLElement {
  return safetyCode === ''
    ? element('span', { className: 'muted' }, 'could not be read')
    : element('span', { className: 'code' }, safetyCode)
}

function pendingSaveCard(pending: NonNullable<PopupState['pendingSave']>): HTMLElement {
  const save = element(
    'button',
    { type: 'button', className: 'primary' },
    pending.offerAction === 'update' ? 'Update' : 'Save',
  )
  const notNow = element('button', { type: 'button' }, 'Not now')
  notNow.addEventListener('click', () => {
    notNow.disabled = true
    void guarded(async () => {
      await ask({ type: 'passwords:dismiss-pending', tabId })
      void refresh()
    })
  })
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
    element(
      'strong',
      {},
      pending.offerAction === 'update' ? 'Update the saved password?' : 'Save the login you just used?',
    ),
    element('div', { className: 'muted' }, pending.userName ? `${pending.userName} on ${pending.host}` : pending.host),
    element('div', { className: 'actions' }, save, notNow),
  )
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
    { className: 'row', role: 'option' },
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

// fillInTab fills the page the dropdown is for. A form that sends what
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
  // Closing hands the keyboard back to the page, in the field just filled.
  setTimeout(() => window.close(), 300)
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

function generatorSection(popupState: PopupState): HTMLElement {
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
  // Fill puts it into the page's new password field and its confirmation.
  const fill = element('button', { type: 'button' }, 'Fill')
  fill.hidden = tabId === undefined || popupState.frameHost === ''
  fill.addEventListener(
    'click',
    () =>
      void guarded(async () => {
        if (output.value === '' || tabId === undefined) return say('Generate a password first.', 'bad')
        const answer = await ask<FillInTabAnswer>({
          type: 'passwords:fill-generated',
          password: output.value,
          tabId,
          frameId: frameId ?? 0,
        })
        if (answer.filledCount === 0) return say('No new password field was found on the page.', 'bad')
        await ask({ type: 'passwords:copy-text', text: output.value })
        say('Filled, and copied in case the page asks for it again.', 'good')
      }),
  )
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
      element('div', { className: 'actions' }, make, copyButton, fill),
    ),
  )
  return section
}

// --- the foot of the dropdown -----------------------------------------------

const dashboardLink = element('button', { type: 'button' }, 'Open Passwords on the dashboard')
dashboardLink.addEventListener('click', () => void ask({ type: 'passwords:open-dashboard' }))
const foot = element('div', { className: 'page' }, dashboardLink)
document.body.append(foot)

// Chrome gives the fill shortcut only when the extension is installed, and
// only if no other extension holds it then. Without one the dropdown says so
// and opens the page where it is set.
async function sayIfShortcutMissing() {
  const commands = (await chrome.commands.getAll()) as { name?: string; shortcut?: string }[]
  const fillCommand = commands.find((command) => command.name === 'fill-login')
  if (fillCommand === undefined || fillCommand.shortcut) return
  const setShortcut = element('button', { type: 'button' }, 'Set a shortcut')
  setShortcut.addEventListener('click', () => void chrome.tabs.create({ url: 'chrome://extensions/shortcuts' }))
  foot.append(
    element(
      'p',
      { className: 'muted' },
      'Fill from TeaNode Passwords has no keyboard shortcut: another extension had it when this one was installed.',
    ),
    setShortcut,
  )
}
void sayIfShortcutMissing().catch(() => {})

void findTab().then(refresh)
