import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryArea } from './passwords/testServer'

// The options page runs when it is loaded, so each test loads it afresh into
// the page's own markup, with the browser's storage and the server played by
// the test.
const pageHtml = readFileSync(resolve(__dirname, 'options.html'), 'utf8')
const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

let local: ReturnType<typeof memoryArea>
let session: ReturnType<typeof memoryArea>
let sendMessage: ReturnType<typeof vi.fn>
let fetchServer: ReturnType<typeof vi.fn>
let changed: ((changes: Record<string, unknown>, area: string) => void) | null

beforeEach(() => {
  vi.resetModules()
  // The page's own markup, without its scripts (parsed, not matched by a
  // pattern, so no form of a script tag is left behind).
  const parsed = new DOMParser().parseFromString(pageHtml, 'text/html')
  for (const script of parsed.querySelectorAll('script')) script.remove()
  document.body.innerHTML = parsed.body.innerHTML
  local = memoryArea()
  session = memoryArea()
  changed = null
  sendMessage = vi.fn(async () => ({}))
  fetchServer = vi.fn(async () => ({ ok: true }))
  vi.stubGlobal('fetch', fetchServer)
  vi.stubGlobal('chrome', {
    storage: {
      local,
      session,
      onChanged: { addListener: (listener: typeof changed) => (changed = listener) },
    },
    runtime: { sendMessage },
    identity: { getRedirectURL: () => 'https://ext.example.test/authorized', launchWebAuthFlow: vi.fn() },
  })
})
afterEach(() => {
  vi.unstubAllGlobals()
})

async function load() {
  await import('./options')
  await settle()
}

const text = (id: string) => document.getElementById(id)?.textContent ?? ''

describe('a saved server address the extension will not use', () => {
  it('is named on the page, with the reason', async () => {
    await local.set({ server: 'http://192.0.2.7:8080' })
    await load()
    expect(text('saved')).toContain('The saved server address is not used.')
    expect(text('saved')).toContain('must start with https://')
    expect(document.getElementById('saved')?.className).toContain('bad')
  })

  it('says nothing about an https address or a local one', async () => {
    await local.set({ server: 'https://tea.example.test' })
    await load()
    expect(text('saved')).toBe('')
    vi.resetModules()
    await local.set({ server: 'http://localhost:8080' })
    await load()
    expect(text('saved')).toBe('')
  })
})

describe('where the server address is kept', () => {
  it('is the local storage beside the token, and the synced one is neither read nor written', async () => {
    // An address another browser put in the synced storage must not be the one
    // the token is sent to. The synced storage here fails on any use.
    const refused = async () => {
      throw new Error('the synced storage was used')
    }
    const sync = { get: vi.fn(refused), set: vi.fn(refused) }
    vi.stubGlobal('chrome', { ...(globalThis as any).chrome, storage: { ...(globalThis as any).chrome.storage, sync } })
    await load()
    expect((document.getElementById('server') as HTMLInputElement).value).toBe('')

    ;(document.getElementById('server') as HTMLInputElement).value = 'https://tea.example.test/'
    ;(document.getElementById('save') as HTMLButtonElement).click()
    await settle()
    expect(local.contents.server).toBe('https://tea.example.test')
    expect(sync.get).not.toHaveBeenCalled()
    expect(sync.set).not.toHaveBeenCalled()
  })
})

describe('signing out', () => {
  async function signedIn(server: string) {
    await local.set({ server, token: 'secret-token', tokenId: 'token-1', username: 'ada' })
    await session.set({ passwordsKeys: { held: true } })
    await load()
  }

  it('revokes the token at an https server and forgets it here', async () => {
    await signedIn('https://tea.example.test')
    ;(document.getElementById('signout') as HTMLButtonElement).click()
    await settle()
    expect(fetchServer).toHaveBeenCalledTimes(1)
    expect(fetchServer.mock.calls[0][0]).toBe('https://tea.example.test/api/v1/graphql')
    expect(fetchServer.mock.calls[0][1].headers.Authorization).toBe('Bearer secret-token')
    expect(local.contents.token).toBeUndefined()
    expect(session.contents.passwordsKeys).toBeUndefined()
    expect(text('status')).toBe('Signed out.')
  })

  it('never sends the token to a plain http address, forgets it here, and says the server could not be told', async () => {
    await signedIn('http://192.0.2.7:8080')
    ;(document.getElementById('signout') as HTMLButtonElement).click()
    await settle()
    expect(fetchServer).not.toHaveBeenCalled()
    expect(local.contents.token).toBeUndefined()
    expect(local.contents.tokenId).toBeUndefined()
    expect(local.contents.username).toBeUndefined()
    expect(session.contents.passwordsKeys).toBeUndefined()
    expect(text('status')).toContain('Signed out here.')
    expect(text('status')).toContain('could not be told')
    expect(document.getElementById('status')?.className).toContain('bad')
  })

  it('still revokes at a local plain http address', async () => {
    await signedIn('http://localhost:8080')
    ;(document.getElementById('signout') as HTMLButtonElement).click()
    await settle()
    expect(fetchServer).toHaveBeenCalledTimes(1)
    expect(text('status')).toBe('Signed out.')
  })

  it('forgets the token and says so when the server cannot be reached or refuses', async () => {
    await signedIn('https://tea.example.test')
    fetchServer.mockRejectedValueOnce(new Error('offline'))
    ;(document.getElementById('signout') as HTMLButtonElement).click()
    await settle()
    expect(local.contents.token).toBeUndefined()
    expect(text('status')).toContain('could not be told')
  })
})

describe('the sites the agent ran script on', () => {
  it('is hidden while there are none', async () => {
    await load()
    expect((document.getElementById('scripted') as HTMLElement).hidden).toBe(true)
  })

  it('lists the origins the service worker kept, and follows the storage', async () => {
    await local.set({ scriptedOrigins: ['https://shop.example.test', 'https://pay.example.test:8443'] })
    await load()
    expect((document.getElementById('scripted') as HTMLElement).hidden).toBe(false)
    expect(Array.from(document.querySelectorAll('#scripted-origins li')).map((item) => item.textContent)).toEqual([
      'https://shop.example.test',
      'https://pay.example.test:8443',
    ])
    await local.set({ scriptedOrigins: [] })
    changed?.({ scriptedOrigins: {} }, 'local')
    await settle()
    expect((document.getElementById('scripted') as HTMLElement).hidden).toBe(true)
  })

  it('asks the service worker to let the agent fill on them again', async () => {
    await local.set({ scriptedOrigins: ['https://shop.example.test'] })
    sendMessage.mockImplementation(async () => {
      await local.remove('scriptedOrigins')
      return {}
    })
    await load()
    ;(document.getElementById('scripted-clear') as HTMLButtonElement).click()
    await settle()
    expect(sendMessage).toHaveBeenCalledWith({ type: 'passwords:clear-scripted-origins' })
    expect(text('scripted-status')).toBe('The agent can fill on these sites again.')
    expect((document.getElementById('scripted') as HTMLElement).hidden).toBe(true)
  })

  it('says so when the service worker refuses', async () => {
    await local.set({ scriptedOrigins: ['https://shop.example.test'] })
    sendMessage.mockResolvedValue({ error: 'Not allowed from here.' })
    await load()
    ;(document.getElementById('scripted-clear') as HTMLButtonElement).click()
    await settle()
    expect(text('scripted-status')).toBe('Not allowed from here.')
    expect((document.getElementById('scripted') as HTMLElement).hidden).toBe(false)
  })
})
