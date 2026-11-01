import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The side panel's script runs when it is loaded, so each test loads it afresh
// into a page of its own, with the service worker played by the test.

const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

let answers
let workerListener
let asked

function stateAnswer(overrides = {}) {
  return {
    server: 'https://tea.example.test',
    isSignedIn: true,
    username: 'ada',
    attachedTabId: null,
    ...overrides,
  }
}

beforeEach(async () => {
  vi.resetModules()
  document.documentElement.removeAttribute('data-theme')
  document.body.innerHTML = `
    <span id="mark"></span>
    <button id="attach" type="button" hidden></button>
    <p id="attach-error" hidden></p>
    <div id="message" hidden><p id="message-text"></p><button id="options" hidden></button></div>
    <div id="frame-box"></div>`
  asked = []
  answers = {
    'sidepanel:state': () => stateAnswer(),
    'sidepanel:token': () => ({ token: 'example-token' }),
    'sidepanel:attach': () => ({ attachedTabId: 5 }),
  }
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'extension-id',
      sendMessage: vi.fn(async (request) => {
        asked.push(request)
        return answers[request.type]?.(request) ?? {}
      }),
      onMessage: {
        addListener: vi.fn((listener) => {
          workerListener = listener
        }),
      },
    },
    tabs: {
      query: vi.fn(async () => [{ id: 5 }]),
      onActivated: { addListener: vi.fn() },
      onUpdated: { addListener: vi.fn() },
    },
  })
  await import('./sidepanel')
  await settle()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const stateRequests = () => asked.filter((request) => request.type === 'sidepanel:state')
const fromWorker = (request) => workerListener(request, { id: 'extension-id' })

describe('the side panel being told how things stand', () => {
  it('shows the dashboard of the person signed in', () => {
    expect(document.querySelector('iframe').src).toBe('https://tea.example.test/drawer')
    expect(stateRequests()).toHaveLength(1)
  })

  it('only redraws the button when the same person is still signed in', async () => {
    fromWorker({ type: 'state', ...stateAnswer({ attachedTabId: 5 }) })
    await settle()
    expect(stateRequests()).toHaveLength(1)
  })

  it('starts again when the person signed out', async () => {
    answers['sidepanel:state'] = () => stateAnswer({ isSignedIn: false, username: '' })
    fromWorker({ type: 'state', ...stateAnswer({ isSignedIn: false, username: '' }) })
    await settle()
    expect(stateRequests()).toHaveLength(2)
    expect(document.querySelector('iframe')).toBeNull()
    expect(document.getElementById('message-text').textContent).toBe(
      'Sign in to TeaNode in the extension options first.',
    )
  })

  it('starts again when somebody else signed in', async () => {
    answers['sidepanel:state'] = () => stateAnswer({ username: 'grace' })
    fromWorker({ type: 'state', ...stateAnswer({ username: 'grace' }) })
    await settle()
    expect(stateRequests()).toHaveLength(2)
  })

  it('ignores a push from a content script in a page', async () => {
    workerListener({ type: 'state', ...stateAnswer({ isSignedIn: false }) }, { id: 'extension-id', tab: { id: 9 } })
    await settle()
    expect(stateRequests()).toHaveLength(1)
  })
})

describe('the button to attach the tab in front', () => {
  it('says why the tab could not be attached', async () => {
    answers['sidepanel:attach'] = () => ({ error: 'Something went wrong.' })
    document.getElementById('attach').click()
    await settle()
    const line = document.getElementById('attach-error')
    expect(line.hidden).toBe(false)
    expect(line.textContent).toBe('Could not attach this tab: Something went wrong.')
  })

  it('takes the line away on the next press that works', async () => {
    answers['sidepanel:attach'] = () => ({ error: 'Something went wrong.' })
    document.getElementById('attach').click()
    await settle()
    answers['sidepanel:attach'] = () => ({ attachedTabId: 5 })
    document.getElementById('attach').click()
    await settle()
    expect(document.getElementById('attach-error').hidden).toBe(true)
  })
})
