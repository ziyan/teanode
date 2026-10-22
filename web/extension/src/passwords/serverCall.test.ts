import { afterEach, describe, expect, it, vi } from 'vitest'
import { callServer, friendlyServerWords, ServerCallError } from './serverCall'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('friendlyServerWords', () => {
  it('shows the sentences the server writes for a person, as they are', () => {
    for (const sentence of [
      'That passphrase is not right.',
      'That recovery key is not right.',
      'This item changed on another device. Open it again.',
      'Passwords is not set up yet.',
    ]) {
      expect(friendlyServerWords([sentence])).toBe(sentence)
    }
  })

  it('puts everything else in its own words, and never shows what is meant for a log', () => {
    const cases: [string[], number, string][] = [
      [
        ['api: invalid arguments: itemKind: unknown value "x"'],
        200,
        'The server could not do that. Try again in a moment.',
      ],
      [
        ['pq: duplicate key value violates unique constraint'],
        200,
        'The server could not do that. Try again in a moment.',
      ],
      [['rate limit exceeded'], 200, 'Too many tries. Wait a minute and try again.'],
      [[], 429, 'Too many tries. Wait a minute and try again.'],
      [['unauthorized'], 200, 'Sign in to TeaNode from the extension options again.'],
      [[], 401, 'Sign in to TeaNode from the extension options again.'],
      [['permission denied'], 200, 'The server does not let this sign-in do that.'],
      [[], 403, 'The server does not let this sign-in do that.'],
      [['item not found'], 200, 'The server did not know that item.'],
      [[], 502, 'The server had a problem. Try again in a moment.'],
      [
        ['approve it on the dashboard or in the iPhone app: setting up, erasing ...'],
        200,
        'That is done on the Passwords page of the dashboard, not from the extension.',
      ],
    ]
    for (const [messages, status, words] of cases)
      expect(friendlyServerWords(messages, status), messages.join()).toBe(words)
  })
})

describe('callServer', () => {
  function stub(response: { status: number; ok: boolean; body?: unknown }, server = 'https://tea.example.test/') {
    vi.stubGlobal('chrome', {
      storage: {
        local: { get: async () => ({ server, token: 'token' }) },
      },
    })
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ status: response.status, ok: response.ok, json: async () => response.body })),
    )
  }

  it('answers the data', async () => {
    stub({ status: 200, ok: true, body: { data: { value: 1 } } })
    expect(await callServer('query { x }')).toEqual({ value: 1 })
  })

  it('throws the friendly words for a refusal, and marks being signed out', async () => {
    stub({ status: 200, ok: true, body: { errors: [{ message: 'That passphrase is not right.' }] } })
    await expect(callServer('query { x }')).rejects.toMatchObject({
      message: 'That passphrase is not right.',
      isSignedOut: false,
    })
    stub({ status: 200, ok: true, body: { errors: [{ message: 'pq: connection reset by peer' }] } })
    const error = await callServer('query { x }').catch((caught) => caught)
    expect(error).toBeInstanceOf(ServerCallError)
    expect(error.message).toBe('The server could not do that. Try again in a moment.')
    stub({ status: 401, ok: false })
    await expect(callServer('query { x }')).rejects.toMatchObject({ isSignedOut: true })
    stub({ status: 500, ok: false })
    await expect(callServer('query { x }')).rejects.toMatchObject({
      message: 'The server had a problem. Try again in a moment.',
    })
  })

  it('sends the token to plain http only on this computer', async () => {
    stub({ status: 200, ok: true, body: { data: { value: 1 } } }, 'http://tea.example.test')
    await expect(callServer('query { x }')).rejects.toMatchObject({
      message: expect.stringMatching(/must start with https:\/\//),
    })
    expect(fetch).not.toHaveBeenCalled()
    for (const server of [
      'http://localhost:8080',
      'http://127.0.0.1:8080/',
      'http://[::1]:8080',
      'http://tea.localhost',
    ]) {
      stub({ status: 200, ok: true, body: { data: { value: 1 } } }, server)
      expect(await callServer('query { x }'), server).toEqual({ value: 1 })
    }
  })
})
