import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { encodeBase64Url } from '../../../src/passwords/crypto'
import { PendingSetUp } from '../../../src/pages/passwords/passwordsProtocol'
import { createFakeServer, FakeServer, makeFixture, makeLogin, memoryArea, ownRecord, PASSPHRASE } from './testServer'

const EXTENSION = 'chrome-extension://ext'
const EXAMPLE_ID = '00000000-0000-4000-8000-000000000001'

let fixture: PendingSetUp

beforeAll(async () => {
  fixture = await makeFixture()
}, 60_000)

afterEach(() => {
  vi.unstubAllGlobals()
  vi.resetModules()
})

type Sender = Record<string, unknown>

// A frame of a web page: what the browser says about a content script.
function pageSender(url: string, options: { tabId?: number; frameId?: number; isIncognito?: boolean } = {}): Sender {
  return {
    id: 'ext',
    url,
    origin: new URL(url).origin,
    frameId: options.frameId ?? 0,
    tab: { id: options.tabId ?? 5, incognito: options.isIncognito ?? false },
  }
}

// One of the extension's own pages, opened as a page of its own.
function ownPageSender(path = 'passwords.html'): Sender {
  return { id: 'ext', url: `${EXTENSION}/${path}`, frameId: 0 }
}

async function start(options: { records?: Awaited<ReturnType<typeof ownRecord>>[]; isSetUp?: boolean } = {}) {
  const server: FakeServer = createFakeServer(fixture, options.records ?? [])
  const session = memoryArea()
  const local = memoryArea()
  const sync = memoryArea()
  await sync.set({ server: 'https://tea.example.test' })
  await local.set({ token: 'token', passwordsIsSetUp: options.isSetUp ?? true })
  const listeners: Record<string, (...args: any[]) => any> = {}
  const registrar = (name: string) => ({
    addListener: (listener: (...args: any[]) => any) => (listeners[name] = listener),
  })
  const windows = {
    create: vi.fn(async () => ({ id: 77 })),
    remove: vi.fn(async () => undefined),
    onRemoved: registrar('windows.onRemoved'),
  }
  // Where the tab is, and whether the person is looking at it; a test moves it.
  const tabState = { url: 'https://example.com/', isActive: true }
  const tabs = {
    get: vi.fn(async (tabId: number) => ({ id: tabId, url: tabState.url, active: tabState.isActive })),
    query: vi.fn(async () => []),
    create: vi.fn(async () => ({})),
    sendMessage: vi.fn(async () => ({ hasPasswordField: false })),
    onUpdated: registrar('tabs.onUpdated'),
    onRemoved: registrar('tabs.onRemoved'),
  }
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'ext',
      getURL: (path: string) => `${EXTENSION}/${path}`,
      onMessage: registrar('runtime.onMessage'),
      onStartup: registrar('runtime.onStartup'),
      onInstalled: registrar('runtime.onInstalled'),
      sendMessage: vi.fn(async () => ({ isCopied: true })),
      getContexts: vi.fn(async () => []),
    },
    storage: { session, local, sync, onChanged: registrar('storage.onChanged') },
    alarms: {
      create: vi.fn(async () => undefined),
      clear: vi.fn(async () => undefined),
      onAlarm: registrar('alarms.onAlarm'),
    },
    windows,
    tabs,
    contextMenus: { removeAll: vi.fn(), create: vi.fn(), onClicked: registrar('contextMenus.onClicked') },
    commands: { onCommand: registrar('commands.onCommand') },
    scripting: { executeScript: vi.fn(async () => [{ frameId: 0, result: 'https://example.com/' }]) },
    offscreen: { createDocument: vi.fn(async () => undefined), closeDocument: vi.fn(async () => undefined) },
  })
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init: { body: string }) => {
      const { query, variables } = JSON.parse(init.body)
      try {
        const data = await server.call(query, variables)
        return { status: 200, ok: true, json: async () => ({ data }) }
      } catch (error) {
        return { status: 200, ok: true, json: async () => ({ errors: [{ message: (error as Error).message }] }) }
      }
    }),
  )
  await import('./background')
  const send = (message: Record<string, unknown>, sender: Sender) =>
    new Promise<any>((resolve) => {
      const isAsync = listeners['runtime.onMessage'](message, sender, resolve)
      if (isAsync === false) resolve(undefined)
    })
  const unlock = () => send({ type: 'passwords:unlock', secretText: PASSPHRASE, isRecoveryKey: false }, ownPageSender())
  return { server, session, local, listeners, windows, tabs, tabState, send, unlock }
}

async function exampleRecords() {
  return [
    await ownRecord(
      fixture,
      EXAMPLE_ID,
      makeLogin({ name: 'Example', userName: 'ada', password: 'hunter2-plaintext', sites: ['example.com'] }),
    ),
    await ownRecord(
      fixture,
      '00000000-0000-4000-8000-000000000002',
      makeLogin({
        name: 'Other',
        userName: 'grace',
        password: 'other-plaintext',
        sites: ['other.test'],
        oneTimeCodeUri: 'otpauth://totp/x?secret=' + ['GEZDGNBV', 'GY3TQOJQ'].join(''),
      }),
    ),
  ]
}

describe('who may send what', () => {
  it("refuses the extension's own page messages from a web page", async () => {
    const { send } = await start({ records: await exampleRecords() })
    for (const type of [
      'passwords:state',
      'passwords:unlock',
      'passwords:search',
      'passwords:copy',
      'passwords:lock',
    ]) {
      expect(
        await send({ type, itemId: EXAMPLE_ID, secretText: PASSPHRASE }, pageSender('https://example.com/')),
        type,
      ).toEqual({
        error: 'Not allowed from here.',
      })
    }
  })

  it("refuses them from the extension's own frame inside a web page", async () => {
    const { send } = await start({ records: await exampleRecords() })
    const frame = { ...ownPageSender('suggestion.html'), frameId: 3, tab: { id: 5 } }
    expect(await send({ type: 'passwords:state' }, frame)).toEqual({ error: 'Not allowed from here.' })
    expect(await send({ type: 'passwords:state' }, { ...ownPageSender('passwords.html'), frameId: 3 })).toEqual({
      error: 'Not allowed from here.',
    })
  })

  it('refuses them from another extension or an unknown page of the extension', async () => {
    const { send } = await start()
    expect(await send({ type: 'passwords:state' }, { ...ownPageSender(), id: 'someone-else' })).toEqual({
      error: 'Not allowed from here.',
    })
    expect(await send({ type: 'passwords:state' }, ownPageSender('panel.html'))).toEqual({
      error: 'Not allowed from here.',
    })
  })

  it('refuses a content message that did not come from a web page', async () => {
    const { send } = await start()
    expect(await send({ type: 'passwords:suggest', role: 'currentPassword' }, ownPageSender())).toEqual({
      error: 'Not a page.',
    })
    expect(
      await send({ type: 'passwords:suggest', role: 'currentPassword' }, pageSender('chrome://settings/')),
    ).toEqual({ error: 'Not a page.' })
  })

  it('ignores a message that is not about Passwords', async () => {
    const { listeners } = await start()
    expect(listeners['runtime.onMessage']({ type: 'panel:token' }, ownPageSender(), () => {})).toBe(false)
  })
})

describe('suggestions and filling', () => {
  it('offers nothing while signed out or before Passwords is set up', async () => {
    const { send } = await start({ isSetUp: false })
    expect(
      await send({ type: 'passwords:suggest', role: 'currentPassword' }, pageSender('https://example.com/')),
    ).toEqual({ suggestionState: 'none' })
  })

  it('offers to unlock while locked, and the logins for the page once open', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    const sender = pageSender('https://www.example.com/sign-in')
    expect(await send({ type: 'passwords:suggest', role: 'currentPassword' }, sender)).toEqual({
      suggestionState: 'locked',
      host: 'example.com',
    })
    expect(await unlock()).toEqual({ isDone: true })
    const answer = await send({ type: 'passwords:suggest', role: 'currentPassword' }, sender)
    expect(answer.suggestionState).toBe('logins')
    expect(answer.logins.map((login: { name: string }) => login.name)).toEqual(['Example'])
    // A list never carries the password.
    expect(JSON.stringify(answer)).not.toContain('plaintext')
    expect(
      await send({ type: 'passwords:suggest', role: 'currentPassword' }, pageSender('https://elsewhere.test/')),
    ).toEqual({ suggestionState: 'none' })
  })

  it('offers a generated password under a new password field and the one-time code logins under a code field', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    expect(await send({ type: 'passwords:suggest', role: 'newPassword' }, pageSender('https://example.com/'))).toEqual({
      suggestionState: 'generate',
      host: 'example.com',
    })
    expect(await send({ type: 'passwords:suggest', role: 'oneTimeCode' }, pageSender('https://example.com/'))).toEqual({
      suggestionState: 'none',
    })
    const withCode = await send({ type: 'passwords:suggest', role: 'oneTimeCode' }, pageSender('https://other.test/'))
    expect(withCode.logins.map((login: { name: string }) => login.name)).toEqual(['Other'])
  })

  it('fills only a login saved for the page that asks', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    expect(await send({ type: 'passwords:fill', itemId: EXAMPLE_ID }, pageSender('https://example.com/'))).toEqual({
      userName: 'ada',
      password: 'hunter2-plaintext',
    })
    for (const url of [
      'https://evil.test/',
      'https://example.com.evil.test/',
      'https://login.example.com/',
      'http://example.com/',
    ]) {
      expect(await send({ type: 'passwords:fill', itemId: EXAMPLE_ID }, pageSender(url)), url).toEqual({
        error: 'This login is not saved for this site.',
      })
    }
  })

  it('works out a one-time code only for a page the login is saved for', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    const answer = await send(
      { type: 'passwords:fill-code', itemId: '00000000-0000-4000-8000-000000000002' },
      pageSender('https://other.test/'),
    )
    expect(answer.oneTimeCode).toMatch(/^\d{6}$/)
    expect(
      await send(
        { type: 'passwords:fill-code', itemId: '00000000-0000-4000-8000-000000000002' },
        pageSender('https://example.com/'),
      ),
    ).toEqual({
      error: 'This login is not saved for this site.',
    })
  })

  it('refuses to fill while locked', async () => {
    const { send } = await start({ records: await exampleRecords() })
    const answer = await send({ type: 'passwords:fill', itemId: EXAMPLE_ID }, pageSender('https://example.com/'))
    expect(answer).toMatchObject({ errorCode: 'locked' })
    expect(JSON.stringify(answer)).not.toContain('hunter2')
  })

  it("lists logins for the window, the page's first, and keeps secrets out of the list", async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    const answer = await send({ type: 'passwords:search', query: '', tabId: 5, frameId: 0 }, ownPageSender())
    expect(answer.forThisSite.map((login: { name: string }) => login.name)).toEqual(['Example'])
    expect(answer.others.map((login: { name: string }) => login.name)).toEqual(['Other'])
    expect(JSON.stringify(answer)).not.toContain('plaintext')
    const searched = await send({ type: 'passwords:search', query: 'grace', tabId: 5, frameId: 0 }, ownPageSender())
    expect(searched.others.map((login: { name: string }) => login.name)).toEqual(['Other'])
  })
})

describe('saving after a sign-in', () => {
  const signIn = { type: 'passwords:captured', userName: 'grace', password: 'new-secret', isNewPassword: false }

  it('offers once, on the page after, and saves on Save', async () => {
    const { send, unlock, server } = await start({ records: await exampleRecords() })
    await unlock()
    await send(signIn, pageSender('https://new.test/sign-in'))
    // A frame inside the page, or a page that still asks for a password, shows nothing.
    expect(
      await send(
        { type: 'passwords:page-ready', hasPasswordField: false },
        pageSender('https://new.test/', { frameId: 2 }),
      ),
    ).toEqual({ offer: null })
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: true }, pageSender('https://new.test/')),
    ).toEqual({ offer: null })
    const ready = await send(
      { type: 'passwords:page-ready', hasPasswordField: false },
      pageSender('https://new.test/home'),
    )
    expect(ready).toEqual({ offer: { offerAction: 'save', host: 'new.test', userName: 'grace' } })
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home')),
    ).toEqual({ offer: null })
    expect(
      await send({ type: 'passwords:bar-answer', barAnswer: 'save' }, pageSender('https://new.test/home')),
    ).toEqual({
      saveOutcome: 'saved',
    })
    expect(server.saved.length).toBe(1)
    expect(JSON.stringify(server.saved[0].input)).not.toContain('new-secret')
    // Saved once; the capture is gone.
    expect(
      await send({ type: 'passwords:bar-answer', barAnswer: 'save' }, pageSender('https://new.test/home')),
    ).toEqual({
      saveOutcome: 'gone',
    })
  })

  it('offers an update for a known login with a new password', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    await send({ ...signIn, userName: 'ada' }, pageSender('https://example.com/sign-in'))
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://example.com/home')),
    ).toEqual({
      offer: { offerAction: 'update', host: 'example.com', userName: 'ada' },
    })
  })

  it('offers nothing for a login that is already saved', async () => {
    const { send, unlock } = await start({ records: await exampleRecords() })
    await unlock()
    await send({ ...signIn, userName: 'ada', password: 'hunter2-plaintext' }, pageSender('https://example.com/sign-in'))
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://example.com/home')),
    ).toEqual({ offer: null })
  })

  it('asks to unlock when locked, keeps the login for the window to save, and saves nothing until then', async () => {
    const { send, unlock, server, windows } = await start({ records: await exampleRecords() })
    await send(signIn, pageSender('https://new.test/sign-in'))
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home')),
    ).toEqual({
      offer: { offerAction: 'unlock', host: 'new.test', userName: 'grace' },
    })
    expect(
      await send({ type: 'passwords:bar-answer', barAnswer: 'save' }, pageSender('https://new.test/home')),
    ).toEqual({
      saveOutcome: 'locked',
    })
    expect(server.saved).toEqual([])
    await send({ type: 'passwords:bar-answer', barAnswer: 'unlock' }, pageSender('https://new.test/home'))
    expect(windows.create).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'popup', url: expect.stringContaining('passwords.html?tab=5') }),
    )
    await unlock()
    const state = await send({ type: 'passwords:state', tabId: 5, frameId: 0 }, ownPageSender())
    expect(state).toMatchObject({
      isUnlocked: true,
      hasPendingSave: true,
      pendingSave: { host: 'new.test', userName: 'grace' },
    })
    expect(JSON.stringify(state)).not.toContain('new-secret')
    expect(await send({ type: 'passwords:save-pending', tabId: 5 }, ownPageSender())).toEqual({ saveOutcome: 'saved' })
  })

  it('keeps nothing in a private window, or before Passwords is set up', async () => {
    const first = await start({ records: await exampleRecords() })
    await first.unlock()
    await first.send(signIn, pageSender('https://new.test/sign-in', { isIncognito: true }))
    expect(
      await first.send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home')),
    ).toEqual({ offer: null })
    vi.resetModules()
    const second = await start({ isSetUp: false })
    await second.send(signIn, pageSender('https://new.test/sign-in'))
    expect(
      await second.send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home')),
    ).toEqual({ offer: null })
  })

  it('forgets the login on Not now', async () => {
    const { send, unlock, server } = await start()
    await unlock()
    await send(signIn, pageSender('https://new.test/sign-in'))
    await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home'))
    await send({ type: 'passwords:bar-answer', barAnswer: 'dismiss' }, pageSender('https://new.test/home'))
    expect(
      await send({ type: 'passwords:bar-answer', barAnswer: 'save' }, pageSender('https://new.test/home')),
    ).toEqual({
      saveOutcome: 'gone',
    })
    expect(server.saved).toEqual([])
  })
})

describe('locking', () => {
  it('drops the keys and the waiting login when the token changes (sign out)', async () => {
    const { send, unlock, listeners, session } = await start()
    await unlock()
    await send(
      { type: 'passwords:captured', userName: 'grace', password: 'new-secret', isNewPassword: false },
      pageSender('https://new.test/'),
    )
    expect(Object.keys(session.contents)).toContain('passwordsKeys')
    listeners['storage.onChanged']({ token: { oldValue: 'token' } }, 'local')
    await vi.waitFor(() => expect(Object.keys(session.contents)).not.toContain('passwordsKeys'))
    expect(
      await send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home')),
    ).toEqual({ offer: null })
  })

  it('locks by hand', async () => {
    const { send, unlock, session } = await start()
    await unlock()
    expect(await send({ type: 'passwords:lock' }, ownPageSender())).toEqual({ isDone: true })
    expect(session.contents.passwordsKeys).toBeUndefined()
  })
})

describe('passkey requests', () => {
  const challenge = encodeBase64Url(new Uint8Array(16).fill(3))
  const creation = {
    rp: { id: 'example.com', name: 'Example' },
    user: { id: encodeBase64Url(new Uint8Array([1, 2, 3])), name: 'ada', displayName: 'Ada' },
    challenge,
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
  }
  const sender = pageSender('https://login.example.com/start')

  it("leaves a request to the browser when Passwords is not available, or from another origin's frame", async () => {
    const off = await start({ isSetUp: false })
    expect(
      await off.send(
        { type: 'passwords:passkey', ceremonyKind: 'create', options: creation, isCrossOrigin: false },
        sender,
      ),
    ).toEqual({ replyOutcome: 'fallback' })
    vi.resetModules()
    const { send } = await start()
    expect(
      await send({ type: 'passwords:passkey', ceremonyKind: 'create', options: creation, isCrossOrigin: true }, sender),
    ).toEqual({ replyOutcome: 'fallback' })
    expect(
      await send(
        { type: 'passwords:passkey', ceremonyKind: 'create', options: creation, isCrossOrigin: false },
        pageSender('https://login.example.com/', { isIncognito: true }),
      ),
    ).toEqual({ replyOutcome: 'fallback' })
  })

  it('refuses a relying party that is not the page, and opens no window', async () => {
    const { send, windows } = await start()
    const answer = await send(
      {
        type: 'passwords:passkey',
        ceremonyKind: 'create',
        options: { ...creation, rp: { id: 'evil.test', name: 'x' } },
        isCrossOrigin: false,
      },
      sender,
    )
    expect(answer).toMatchObject({ replyOutcome: 'error', errorName: 'SecurityError' })
    expect(windows.create).not.toHaveBeenCalled()
  })

  it('leaves a sign-in to the browser when unlocked and there is no passkey for the site', async () => {
    const { send, unlock, windows } = await start({ records: await exampleRecords() })
    await unlock()
    expect(
      await send(
        {
          type: 'passwords:passkey',
          ceremonyKind: 'get',
          options: { rpId: 'example.com', challenge },
          isCrossOrigin: false,
        },
        sender,
      ),
    ).toEqual({ replyOutcome: 'fallback' })
    expect(windows.create).not.toHaveBeenCalled()
  })

  async function ask(
    context: Awaited<ReturnType<typeof start>>,
    ceremonyKind: 'create' | 'get',
    options: Record<string, unknown>,
  ) {
    const answer = context.send({ type: 'passwords:passkey', ceremonyKind, options, isCrossOrigin: false }, sender)
    await vi.waitFor(() => expect(context.windows.create).toHaveBeenCalled())
    const url = (context.windows.create.mock.calls[0] as unknown as [{ url: string }])[0].url
    expect(url).toContain(`${EXTENSION}/prompt.html?id=`)
    return { answer, promptId: new URL(url).searchParams.get('id') as string }
  }

  it('asks the person, makes the passkey, saves it, and only then answers the page', async () => {
    const context = await start()
    await context.unlock()
    const { answer, promptId } = await ask(context, 'create', creation)
    expect(await context.send({ type: 'passwords:prompt-info', promptId }, ownPageSender('prompt.html'))).toMatchObject(
      {
        ceremonyKind: 'create',
        relyingPartyId: 'example.com',
        userName: 'ada',
        isUnlocked: true,
      },
    )
    // Nothing has been made or saved before the person says so.
    expect(context.server.saved).toEqual([])
    expect(
      await context.send(
        { type: 'passwords:prompt-decide', promptId, promptDecision: 'allow', choiceCredentialId: '' },
        ownPageSender('prompt.html'),
      ),
    ).toEqual({ isDone: true })
    const reply = await answer
    expect(reply.replyOutcome).toBe('credential')
    expect(reply.credential.id).toBe(reply.credential.rawId)
    expect(context.server.saved.length).toBe(1)
    expect(JSON.stringify(context.server.saved[0].input)).not.toContain(reply.credential.id)
    expect(context.windows.remove).toHaveBeenCalledWith(77)
    // The question is answered once.
    expect(await context.send({ type: 'passwords:prompt-info', promptId }, ownPageSender('prompt.html'))).toMatchObject(
      { error: expect.stringContaining('already been answered') },
    )
  })

  it('hands the request to the browser on "Use the browser instead"', async () => {
    const context = await start()
    await context.unlock()
    const { answer, promptId } = await ask(context, 'create', creation)
    await context.send(
      { type: 'passwords:prompt-decide', promptId, promptDecision: 'browser', choiceCredentialId: '' },
      ownPageSender('prompt.html'),
    )
    expect(await answer).toEqual({ replyOutcome: 'fallback' })
    expect(context.server.saved).toEqual([])
  })

  it('answers as a refusal when the window is closed', async () => {
    const context = await start()
    const { answer } = await ask(context, 'create', creation)
    context.listeners['windows.onRemoved'](77)
    expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
  })

  it("does not let the page decide, only the extension's own window", async () => {
    const context = await start()
    await context.unlock()
    const { answer, promptId } = await ask(context, 'create', creation)
    expect(
      await context.send(
        { type: 'passwords:prompt-decide', promptId, promptDecision: 'allow', choiceCredentialId: '' },
        pageSender('https://login.example.com/'),
      ),
    ).toEqual({ error: 'Not allowed from here.' })
    expect(context.server.saved).toEqual([])
    await context.send(
      { type: 'passwords:prompt-decide', promptId, promptDecision: 'browser', choiceCredentialId: '' },
      ownPageSender('prompt.html'),
    )
    await answer
  })

  it('refuses while locked and keeps the window, so it can unlock', async () => {
    const context = await start()
    const { answer, promptId } = await ask(context, 'create', creation)
    expect(await context.send({ type: 'passwords:prompt-info', promptId }, ownPageSender('prompt.html'))).toMatchObject(
      { isUnlocked: false },
    )
    expect(
      await context.send(
        { type: 'passwords:prompt-decide', promptId, promptDecision: 'allow', choiceCredentialId: '' },
        ownPageSender('prompt.html'),
      ),
    ).toMatchObject({ errorCode: 'locked' })
    await context.unlock()
    expect(
      await context.send(
        { type: 'passwords:prompt-decide', promptId, promptDecision: 'allow', choiceCredentialId: '' },
        ownPageSender('prompt.html'),
      ),
    ).toEqual({ isDone: true })
    expect((await answer).replyOutcome).toBe('credential')
  })

  it('signs in with a saved passkey after the person says yes', async () => {
    const context = await start()
    await context.unlock()
    const created = await ask(context, 'create', creation)
    await context.send(
      { type: 'passwords:prompt-decide', promptId: created.promptId, promptDecision: 'allow', choiceCredentialId: '' },
      ownPageSender('prompt.html'),
    )
    const made = await created.answer
    context.windows.create.mockClear()

    const signIn = await ask(context, 'get', { rpId: 'example.com', challenge })
    const info = await context.send(
      { type: 'passwords:prompt-info', promptId: signIn.promptId },
      ownPageSender('prompt.html'),
    )
    expect(info).toMatchObject({
      ceremonyKind: 'get',
      isUnlocked: true,
      choices: [{ label: 'ada', credentialId: made.credential.id }],
    })
    await context.send(
      {
        type: 'passwords:prompt-decide',
        promptId: signIn.promptId,
        promptDecision: 'allow',
        choiceCredentialId: made.credential.id,
      },
      ownPageSender('prompt.html'),
    )
    const reply = await signIn.answer
    expect(reply.replyOutcome).toBe('credential')
    expect(reply.credential.rawId).toBe(made.credential.id)
    expect(JSON.parse(atob(reply.credential.clientDataJSON.replace(/-/g, '+').replace(/_/g, '/')))).toMatchObject({
      type: 'webauthn.get',
      origin: 'https://login.example.com',
      crossOrigin: false,
    })
  })

  it('does not ask twice at once in one tab', async () => {
    const context = await start()
    await context.unlock()
    const first = await ask(context, 'create', creation)
    expect(
      await context.send(
        { type: 'passwords:passkey', ceremonyKind: 'create', options: creation, isCrossOrigin: false },
        sender,
      ),
    ).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
    await context.send(
      { type: 'passwords:prompt-decide', promptId: first.promptId, promptDecision: 'browser', choiceCredentialId: '' },
      ownPageSender('prompt.html'),
    )
    await first.answer
  })
})

describe('passkey ceremonies and the person', () => {
  const challenge = encodeBase64Url(new Uint8Array(16).fill(3))
  const creation = {
    rp: { id: 'example.com', name: 'Example' },
    user: { id: encodeBase64Url(new Uint8Array([1, 2, 3])), name: 'ada', displayName: 'Ada' },
    challenge,
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
  }
  const sender = pageSender('https://login.example.com/start')
  const prompt = () => ownPageSender('prompt.html')

  type Context = Awaited<ReturnType<typeof start>>

  async function ask(
    context: Context,
    ceremonyKind: 'create' | 'get',
    options: Record<string, unknown>,
    from: Sender = sender,
  ) {
    context.windows.create.mockClear()
    const answer = context.send({ type: 'passwords:passkey', ceremonyKind, options, isCrossOrigin: false }, from)
    await vi.waitFor(() => expect(context.windows.create).toHaveBeenCalled())
    const url = (context.windows.create.mock.calls[0] as unknown as [{ url: string }])[0].url
    return { answer, promptId: new URL(url).searchParams.get('id') as string }
  }

  const decide = (context: Context, promptId: string, promptDecision: 'allow' | 'browser', choiceCredentialId = '') =>
    context.send({ type: 'passwords:prompt-decide', promptId, promptDecision, choiceCredentialId }, prompt())

  // flagsOf is the flags byte of the authenticator data a page was given.
  function flagsOf(credential: { authenticatorData: string }): number {
    const bytes = Uint8Array.from(
      atob(credential.authenticatorData.replace(/-/g, '+').replace(/_/g, '/')),
      (character) => character.charCodeAt(0),
    )
    return bytes[32]
  }

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('whether the person was verified', () => {
    it('marks the credential user verified when the passphrase was typed a moment ago, and only then', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      const context = await start()
      await context.unlock()
      const fresh = await ask(context, 'create', creation)
      await decide(context, fresh.promptId, 'allow')
      expect(flagsOf((await fresh.answer).credential)).toBe(0x5d)

      // A minute later the passphrase is no longer fresh: present, not verified.
      vi.setSystemTime(Date.now() + 61_000)
      const stale = await ask(context, 'create', creation)
      await decide(context, stale.promptId, 'allow')
      expect(flagsOf((await stale.answer).credential)).toBe(0x59)
    })

    it('does the same for a sign-in', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      const context = await start()
      await context.unlock()
      const made = await ask(context, 'create', creation)
      await decide(context, made.promptId, 'allow')
      const credentialId = (await made.answer).credential.id
      for (const [advance, flags] of [
        [0, 0x1d],
        [61_000, 0x19],
      ]) {
        vi.setSystemTime(Date.now() + advance)
        const signIn = await ask(context, 'get', { rpId: 'example.com', challenge })
        await decide(context, signIn.promptId, 'allow', credentialId)
        expect(flagsOf((await signIn.answer).credential)).toBe(flags)
      }
    })

    it('does not answer a site that insists on verification until the passphrase is typed again', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      const context = await start()
      await context.unlock()
      vi.setSystemTime(Date.now() + 61_000)
      const required = { ...creation, authenticatorSelection: { userVerification: 'required' } }
      const { answer, promptId } = await ask(context, 'create', required)
      expect(await context.send({ type: 'passwords:prompt-info', promptId }, prompt())).toMatchObject({
        isUnlocked: true,
        isVerificationNeeded: true,
      })
      expect(await decide(context, promptId, 'allow')).toMatchObject({ errorCode: 'verificationNeeded' })
      expect(context.server.saved).toEqual([])
      await context.unlock()
      expect(await context.send({ type: 'passwords:prompt-info', promptId }, prompt())).toMatchObject({
        isVerificationNeeded: false,
      })
      expect(await decide(context, promptId, 'allow')).toEqual({ isDone: true })
      expect(flagsOf((await answer).credential)).toBe(0x5d)
    })

    it('answers a site that insists on verification with NotAllowedError when the window is closed unverified', async () => {
      vi.useFakeTimers({ toFake: ['Date'] })
      const context = await start()
      await context.unlock()
      vi.setSystemTime(Date.now() + 61_000)
      const { answer } = await ask(context, 'create', {
        ...creation,
        authenticatorSelection: { userVerification: 'required' },
      })
      context.listeners['windows.onRemoved'](77)
      expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
      expect(context.server.saved).toEqual([])
    })
  })

  describe('the window', () => {
    it('names the page that asked, which can be a host under the relying party', async () => {
      const context = await start()
      const { answer, promptId } = await ask(context, 'create', creation)
      expect(await context.send({ type: 'passwords:prompt-info', promptId }, prompt())).toMatchObject({
        relyingPartyId: 'example.com',
        pageOrigin: 'https://login.example.com',
      })
      await decide(context, promptId, 'browser')
      await answer
    })

    it('takes the keyboard focus only when the page that asked is the tab the person is looking at', async () => {
      const context = await start()
      const visible = await ask(context, 'create', creation)
      expect(context.windows.create).toHaveBeenLastCalledWith(expect.objectContaining({ focused: true }))
      context.listeners['windows.onRemoved'](77)
      await visible.answer

      context.tabState.isActive = false
      const hidden = await ask(context, 'create', creation)
      expect(context.windows.create).toHaveBeenLastCalledWith(expect.objectContaining({ focused: false }))
      context.listeners['windows.onRemoved'](77)
      await hidden.answer
    })
  })

  describe('when the page goes away', () => {
    it("ends the question when the page withdraws its request, and only that frame's", async () => {
      const context = await start()
      const { answer } = await ask(context, 'create', creation)
      // Another frame of the tab cannot withdraw it.
      await context.send({ type: 'passwords:passkey-cancel' }, pageSender('https://login.example.com/', { frameId: 4 }))
      expect(context.windows.remove).not.toHaveBeenCalled()
      await context.send({ type: 'passwords:passkey-cancel' }, sender)
      expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
      expect(context.windows.remove).toHaveBeenCalledWith(77)
    })

    it('ends the question when the tab goes to another address, but not for a move within the page', async () => {
      const context = await start()
      const { answer } = await ask(context, 'create', creation)
      await context.listeners['tabs.onUpdated'](5, { url: 'https://example.com/#section' })
      expect(context.windows.remove).not.toHaveBeenCalled()
      await context.listeners['tabs.onUpdated'](5, { url: 'https://elsewhere.test/' })
      expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
      expect(context.windows.remove).toHaveBeenCalledWith(77)
    })

    it('ends the question when the tab is closed', async () => {
      const context = await start()
      const { answer } = await ask(context, 'create', creation)
      context.listeners['tabs.onRemoved'](5)
      expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
      expect(context.windows.remove).toHaveBeenCalledWith(77)
    })

    it('saves no passkey and answers nobody when the tab has moved on by the time the person says yes', async () => {
      const context = await start()
      await context.unlock()
      const { answer, promptId } = await ask(context, 'create', creation)
      // The tab moved, and the notice of it has not been heard yet.
      context.tabState.url = 'https://elsewhere.test/'
      expect(await decide(context, promptId, 'allow')).toMatchObject({ error: expect.stringContaining('is gone') })
      expect(context.server.saved).toEqual([])
      await context.listeners['tabs.onUpdated'](5, { url: 'https://elsewhere.test/' })
      expect(await answer).toMatchObject({ replyOutcome: 'error', errorName: 'NotAllowedError' })
      expect(context.server.saved).toEqual([])
    })

    it('saves no passkey for a question that was already answered when the passkey was made', async () => {
      const context = await start()
      await context.unlock()
      const { promptId } = await ask(context, 'create', creation)
      // The tab is closed while the passkey is being made.
      const deciding = decide(context, promptId, 'allow')
      context.listeners['tabs.onRemoved'](5)
      await deciding
      expect(context.server.saved).toEqual([])
    })
  })

  describe('locked, and the browser', () => {
    it('leaves a sign-in to the browser, with no window, when locked and nothing is known of a passkey for the site', async () => {
      const context = await start()
      expect(
        await context.send(
          {
            type: 'passwords:passkey',
            ceremonyKind: 'get',
            options: { rpId: 'example.com', challenge },
            isCrossOrigin: false,
          },
          sender,
        ),
      ).toEqual({ replyOutcome: 'fallback' })
      expect(context.windows.create).not.toHaveBeenCalled()
    })

    it('asks to unlock, when locked, for a site it has a passkey for, and for no other', async () => {
      const context = await start()
      await context.unlock()
      const made = await ask(context, 'create', creation)
      await decide(context, made.promptId, 'allow')
      await made.answer
      // Only the hash of the id was kept, not the name.
      expect(JSON.stringify(context.local.contents)).not.toContain('example.com')
      await context.send({ type: 'passwords:lock' }, ownPageSender())
      context.windows.create.mockClear()

      expect(
        await context.send(
          {
            type: 'passwords:passkey',
            ceremonyKind: 'get',
            options: { rpId: 'other.test', challenge },
            isCrossOrigin: false,
          },
          pageSender('https://other.test/'),
        ),
      ).toEqual({ replyOutcome: 'fallback' })
      expect(context.windows.create).not.toHaveBeenCalled()

      const known = await ask(context, 'get', { rpId: 'example.com', challenge })
      expect(await context.send({ type: 'passwords:prompt-info', promptId: known.promptId }, prompt())).toMatchObject({
        isUnlocked: false,
      })
      await decide(context, known.promptId, 'browser')
      await known.answer
    })

    it('forgets which sites have a passkey when the sign-in changes', async () => {
      const context = await start()
      await context.unlock()
      const made = await ask(context, 'create', creation)
      await decide(context, made.promptId, 'allow')
      await made.answer
      expect(context.local.contents.passwordsPasskeySites).toBeDefined()
      context.listeners['storage.onChanged']({ token: { oldValue: 'token' } }, 'local')
      await vi.waitFor(() => expect(context.local.contents.passwordsPasskeySites).toBeUndefined())
    })

    it('remembers "Use the browser instead" for the site for the session, and for that site only', async () => {
      const context = await start()
      await context.unlock()
      const first = await ask(context, 'create', creation)
      await decide(context, first.promptId, 'browser')
      expect(await first.answer).toEqual({ replyOutcome: 'fallback' })
      expect(JSON.stringify(context.session.contents.passwordsUseBrowserSites)).toContain('https://login.example.com')
      context.windows.create.mockClear()

      // The same site is left to the browser at once, for a create or a sign-in.
      for (const [ceremonyKind, options] of [
        ['create', creation],
        ['get', { rpId: 'example.com', challenge }],
      ] as const) {
        expect(
          await context.send({ type: 'passwords:passkey', ceremonyKind, options, isCrossOrigin: false }, sender),
        ).toEqual({ replyOutcome: 'fallback' })
      }
      expect(context.windows.create).not.toHaveBeenCalled()

      // Another origin is still asked about.
      const other = await ask(context, 'create', creation, pageSender('https://www.example.com/'))
      await decide(context, other.promptId, 'browser')
      await other.answer
      // And the choice does not outlast the browser session.
      delete context.session.contents.passwordsUseBrowserSites
      const again = await ask(context, 'create', creation)
      await decide(context, again.promptId, 'browser')
      await again.answer
    })
  })
})

describe('what the service worker remembers of a list', () => {
  const sender = pageSender('https://example.com/sign-in')
  const suggest = (context: Awaited<ReturnType<typeof start>>, role = 'currentPassword', from: Sender = sender) =>
    context.send({ type: 'passwords:suggest', role }, from)
  const listCount = (context: Awaited<ReturnType<typeof start>>) =>
    context.server.calls.filter((call) => call === 'ListPasswordItems').length

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens the logins once for a field that gets the focus again and again, for ten seconds', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const context = await start({ records: await exampleRecords() })
    await context.unlock()
    const first = await suggest(context)
    expect(first.suggestionState).toBe('logins')
    for (let turn = 0; turn < 20; turn++) expect(await suggest(context)).toEqual(first)
    expect(listCount(context)).toBe(1)
    vi.setSystemTime(Date.now() + 9_000)
    await suggest(context)
    expect(listCount(context)).toBe(1)
    vi.setSystemTime(Date.now() + 2_000)
    await suggest(context)
    expect(listCount(context)).toBe(2)
  })

  it('keeps no password in it, and keeps a page apart from another and a kind of field from another', async () => {
    const context = await start({ records: await exampleRecords() })
    await context.unlock()
    await suggest(context)
    expect(JSON.stringify(await suggest(context))).not.toContain('plaintext')
    expect((await suggest(context, 'currentPassword', pageSender('https://other.test/'))).suggestionState).toBe(
      'logins',
    )
    expect((await suggest(context, 'oneTimeCode')).suggestionState).toBe('none')
    expect((await suggest(context, 'currentPassword', pageSender('https://elsewhere.test/'))).suggestionState).toBe(
      'none',
    )
  })

  it('is let go when Passwords locks, and never serves a locked Passwords', async () => {
    const context = await start({ records: await exampleRecords() })
    await context.unlock()
    await suggest(context)
    await context.send({ type: 'passwords:lock' }, ownPageSender())
    expect((await suggest(context)).suggestionState).toBe('locked')
    await context.unlock()
    await suggest(context)
    expect(listCount(context)).toBe(2)
  })

  it('is let go when a login is saved', async () => {
    const context = await start({ records: await exampleRecords() })
    await context.unlock()
    await suggest(context, 'currentPassword', pageSender('https://new.test/'))
    await context.send(
      { type: 'passwords:captured', userName: 'grace', password: 'new-secret', isNewPassword: false },
      pageSender('https://new.test/sign-in'),
    )
    await context.send({ type: 'passwords:page-ready', hasPasswordField: false }, pageSender('https://new.test/home'))
    await context.send({ type: 'passwords:bar-answer', barAnswer: 'save' }, pageSender('https://new.test/home'))
    const answer = await suggest(context, 'currentPassword', pageSender('https://new.test/'))
    expect(answer.suggestionState).toBe('logins')
  })
})

describe('filling from the window', () => {
  it('tells the page the origin the login was approved for, and passes on a form that sends elsewhere', async () => {
    const context = await start({ records: await exampleRecords() })
    await context.unlock()
    context.tabs.sendMessage.mockResolvedValueOnce({ filledCount: 0, foreignActionHost: 'collect.example.test' } as any)
    const answer = await context.send(
      { type: 'passwords:fill-in-tab', itemId: EXAMPLE_ID, fillPart: 'login', tabId: 5, frameId: 0 },
      ownPageSender(),
    )
    expect(answer).toEqual({ filledCount: 0, foreignActionHost: 'collect.example.test' })
    expect(context.tabs.sendMessage).toHaveBeenCalledWith(
      5,
      expect.objectContaining({
        type: 'passwords:fill-fields',
        origin: 'https://example.com',
        isForeignActionConfirmed: false,
        password: 'hunter2-plaintext',
      }),
      { frameId: 0 },
    )
    context.tabs.sendMessage.mockResolvedValueOnce({ filledCount: 2 } as any)
    await context.send(
      {
        type: 'passwords:fill-in-tab',
        itemId: EXAMPLE_ID,
        fillPart: 'login',
        isForeignActionConfirmed: true,
        tabId: 5,
        frameId: 0,
      },
      ownPageSender(),
    )
    expect(context.tabs.sendMessage).toHaveBeenLastCalledWith(
      5,
      expect.objectContaining({ isForeignActionConfirmed: true }),
      { frameId: 0 },
    )
  })

  it('does not fill a login saved for another port or for the plain-text twin of the page', async () => {
    const context = await start({
      records: [
        await ownRecord(
          fixture,
          EXAMPLE_ID,
          makeLogin({ name: 'Example', sites: ['https://example.com:8443', 'http://other.test'] }),
        ),
      ],
    })
    await context.unlock()
    expect(
      (await context.send({ type: 'passwords:fill', itemId: EXAMPLE_ID }, pageSender('https://example.com:8443/')))
        .password,
    ).toBe('hunter2-plaintext')
    for (const url of ['https://example.com/', 'https://example.com:9443/', 'https://other.test/']) {
      expect(await context.send({ type: 'passwords:fill', itemId: EXAMPLE_ID }, pageSender(url)), url).toEqual({
        error: 'This login is not saved for this site.',
      })
    }
  })
})
