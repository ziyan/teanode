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

async function load(answer: unknown) {
  vi.resetModules()
  window.history.replaceState({}, '', '/prompt.html?id=one')
  document.body.innerHTML = '<div id="app"></div>'
  vi.stubGlobal('chrome', { runtime: { sendMessage: vi.fn(async () => answer) } })
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
})
