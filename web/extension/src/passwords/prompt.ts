// The passkey question. A website asked this browser for a passkey and the
// extension stopped to ask, in a window of its own that the website can
// neither draw over nor click: "Save a passkey for example.com as ada?" or
// "Sign in to example.com with your passkey?". "Use the browser instead" hands
// the request back to the browser's own passkeys. Closing the window answers
// as a refusal. If Passwords is locked, the window unlocks it first instead of
// failing quietly.

import { PromptInfo } from './messages'
import { ask, AskError, clear, element, errorText } from './pageDom'

const root = document.getElementById('app') as HTMLElement
const promptId = new URLSearchParams(window.location.search).get('id') ?? ''
const status = element('div', { className: 'status', role: 'status' })

function say(text: string, tone: '' | 'bad' = '') {
  status.textContent = text
  status.className = tone ? `status ${tone}` : 'status'
}

function heading(): HTMLElement {
  return element(
    'div',
    { className: 'header' },
    element('img', { className: 'mark', src: 'icons/icon48.png', alt: '', width: 22, height: 22 }),
    element('strong', {}, 'TeaNode Passwords'),
  )
}

async function refresh() {
  clear(root)
  let info: PromptInfo
  try {
    info = await ask<PromptInfo>({ type: 'passwords:prompt-info', promptId })
  } catch (error) {
    const close = element('button', { type: 'button' }, 'Close')
    close.addEventListener('click', () => window.close())
    root.append(heading(), element('p', {}, errorText(error)), element('div', { className: 'actions' }, close))
    return
  }
  const question =
    info.kind === 'create'
      ? `Save a passkey for ${info.relyingPartyId} as ${info.userName}?`
      : `Sign in to ${info.relyingPartyId} with your passkey?`
  root.append(heading(), element('strong', {}, question))
  if (!info.isUnlocked) return showUnlock()
  showChoice(info)
}

// Cancel closes the window, which answers the website as a refusal.
function makeCancelButton(): HTMLButtonElement {
  const button = element('button', { type: 'button' }, 'Cancel')
  button.addEventListener('click', () => window.close())
  return button
}

function makeBrowserButton(): HTMLButtonElement {
  const button = element('button', { type: 'button' }, 'Use the browser instead')
  button.addEventListener('click', async () => {
    button.disabled = true
    try {
      await ask({ type: 'passwords:prompt-decide', promptId, decision: 'browser', choiceCredentialId: '' })
    } catch (error) {
      button.disabled = false
      say(errorText(error), 'bad')
    }
  })
  return button
}

function showUnlock() {
  const input = element('input', { type: 'password', autocomplete: 'off', spellcheck: false })
  const unlock = element('button', { type: 'submit', className: 'primary' }, 'Unlock')
  const form = element(
    'form',
    {},
    element('p', { className: 'muted' }, 'Passwords is locked. Unlock it with your passphrase to continue.'),
    element('label', { className: 'field' }, element('span', {}, 'Passphrase'), input),
    element('div', { className: 'actions' }, unlock, makeBrowserButton(), makeCancelButton()),
    status,
  )
  form.addEventListener('submit', async (event) => {
    event.preventDefault()
    unlock.disabled = true
    say('Unlocking. This takes a moment.')
    try {
      await ask({ type: 'passwords:unlock', secretText: input.value, isRecoveryKey: false })
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

function showChoice(info: PromptInfo) {
  let chosenCredentialId = info.choices[0]?.credentialId ?? ''
  const choices = element('div', { className: 'list' })
  if (info.kind === 'get' && info.choices.length > 1) {
    info.choices.forEach((choice, index) => {
      const radio = element('input', { type: 'radio', name: 'passkey', checked: index === 0 })
      radio.addEventListener('change', () => {
        chosenCredentialId = choice.credentialId
      })
      choices.append(element('label', { className: 'row choice-row' }, radio, element('span', {}, choice.label)))
    })
  } else if (info.kind === 'get' && info.choices.length === 1) {
    choices.append(element('div', { className: 'row' }, element('div', { className: 'name' }, info.choices[0].label)))
  }
  const allow = element(
    'button',
    { type: 'button', className: 'primary' },
    info.kind === 'create' ? 'Save passkey' : 'Sign in',
  )
  allow.addEventListener('click', async () => {
    allow.disabled = true
    say('')
    try {
      await ask({
        type: 'passwords:prompt-decide',
        promptId,
        decision: 'allow',
        choiceCredentialId: chosenCredentialId,
      })
    } catch (error) {
      allow.disabled = false
      if (error instanceof AskError && error.code === 'locked') return void refresh()
      say(errorText(error), 'bad')
    }
  })
  if (choices.childElementCount > 0) root.append(choices)
  root.append(
    element(
      'p',
      { className: 'muted' },
      info.kind === 'create'
        ? 'The passkey is kept encrypted in TeaNode Passwords.'
        : 'Your passkey signs the sign-in on this device.',
    ),
    element('div', { className: 'actions' }, allow, makeBrowserButton(), makeCancelButton()),
    status,
  )
}

void refresh()
