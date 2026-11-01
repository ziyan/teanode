import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The passkey question runs when it is loaded, so each test loads it afresh
// into a page of its own, with the service worker played by the test.
const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

function info(choices: { label: string; credentialId: string; sharedByText: string }[]) {
  return {
    ceremonyKind: 'get',
    relyingPartyId: 'example.test',
    pageOrigin: 'https://example.test',
    userName: '',
    isUnlocked: true,
    isVerificationNeeded: false,
    choices,
  }
}

// portListeners are what the question listens for on its port to the
// service worker.
let portListeners: ((message: unknown) => void)[] = []
let connect: ReturnType<typeof vi.fn>

async function load(answer: unknown) {
  vi.resetModules()
  window.history.replaceState({}, '', '/prompt.html?id=one')
  document.body.innerHTML = '<div id="app"></div>'
  portListeners = []
  connect = vi.fn(() => ({
    onMessage: { addListener: (listener: (message: unknown) => void) => portListeners.push(listener) },
  }))
  vi.stubGlobal('chrome', { runtime: { sendMessage: vi.fn(async () => answer), connect } })
  await import('./prompt')
  await settle()
}

afterEach(() => {
  vi.unstubAllGlobals()
})
beforeEach(() => {
  vi.spyOn(window, 'close').mockImplementation(() => {})
})

describe('the passkey question', () => {
  it('says who owns the login of a passkey that somebody shared', async () => {
    await load(
      info([
        { label: 'ada', credentialId: 'one', sharedByText: '' },
        { label: 'team', credentialId: 'two', sharedByText: 'Shared by Grace' },
      ]),
    )
    const rows = Array.from(document.querySelectorAll('.choice-row'))
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toBe('ada')
    expect(rows[1].textContent).toContain('team')
    expect(rows[1].textContent).toContain('Shared by Grace')
  })

  it('says it for the only passkey too, and nothing for one the person owns', async () => {
    await load(info([{ label: 'team', credentialId: 'two', sharedByText: 'Shared by Grace' }]))
    expect(document.querySelector('.row')?.textContent).toContain('Shared by Grace')
    await load(info([{ label: 'ada', credentialId: 'one', sharedByText: '' }]))
    expect(document.querySelector('.row')?.textContent).toBe('ada')
  })

  it('keeps a port open to the service worker, and closes when the worker says the question is over', async () => {
    await load(info([{ label: 'ada', credentialId: 'one', sharedByText: '' }]))
    expect(connect).toHaveBeenCalledWith({ name: 'passwords-prompt' })
    expect(window.close).not.toHaveBeenCalled()
    for (const listener of portListeners) listener({ promptMessageKind: 'close' })
    expect(window.close).toHaveBeenCalled()
  })
})
