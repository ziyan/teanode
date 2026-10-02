import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIError, graphql, isNotFound, setViewOnly } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('GraphQL transport retries', () => {
  it.each([
    'mutation { SendMailboxMessage { id } }',
    '  mutation Send { SendMailboxMessage { id } }',
    '# query\nmutation { SendMailboxMessage { id } }',
    'subscription { AgentEvents { id } }',
    'fragment Fields on Query { Session { authenticated } } query { ...Fields }',
  ])('does not repeat an operation whose response may have been lost: %s', async (document) => {
    const networkFailure = new TypeError('Failed to fetch')
    const fetchRequest = vi.fn().mockRejectedValue(networkFailure)
    vi.stubGlobal('fetch', fetchRequest)

    await expect(graphql(document)).rejects.toBe(networkFailure)
    expect(fetchRequest).toHaveBeenCalledTimes(1)
  })

  it.each(['query { Session { authenticated } }', '  { Session { authenticated } }'])(
    'retries an explicitly read-only operation once: %s',
    async (document) => {
      const fetchRequest = vi
        .fn()
        .mockRejectedValueOnce(new TypeError('Failed to fetch'))
        .mockResolvedValueOnce(Response.json({ data: { Session: { authenticated: true } } }))
      vi.stubGlobal('fetch', fetchRequest)

      await expect(graphql(document)).resolves.toEqual({ Session: { authenticated: true } })
      expect(fetchRequest).toHaveBeenCalledTimes(2)
    },
  )

  it('stops after a second connection failure', async () => {
    const networkFailure = new TypeError('Failed to fetch')
    const fetchRequest = vi.fn().mockRejectedValue(networkFailure)
    vi.stubGlobal('fetch', fetchRequest)

    await expect(graphql('query { Session { authenticated } }')).rejects.toBe(networkFailure)
    expect(fetchRequest).toHaveBeenCalledTimes(2)
  })

  it('does not retry an intentionally aborted read', async () => {
    const cancellation = new DOMException('Cancelled', 'AbortError')
    const fetchRequest = vi.fn().mockRejectedValue(cancellation)
    vi.stubGlobal('fetch', fetchRequest)

    await expect(graphql('query { Session { authenticated } }')).rejects.toBe(cancellation)
    expect(fetchRequest).toHaveBeenCalledTimes(1)
  })

  it('does not retry an HTTP failure', async () => {
    const fetchRequest = vi.fn().mockResolvedValue(new Response(null, { status: 503 }))
    vi.stubGlobal('fetch', fetchRequest)

    await expect(graphql('query { Session { authenticated } }')).rejects.toThrow('The server returned 503.')
    expect(fetchRequest).toHaveBeenCalledTimes(1)
  })
})

describe('isNotFound', () => {
  it('knows a thing deleted since the page was drawn from any other failure', () => {
    expect(isNotFound(new APIError('api: not found'))).toBe(true)
    expect(isNotFound(new APIError('db: not found: conversation'))).toBe(true)
    expect(isNotFound(new APIError('api: permission denied'))).toBe(false)
    expect(isNotFound(new Error('api: not found'))).toBe(false)
  })
})

describe('Signed in as somebody else', () => {
  afterEach(() => setViewOnly(false))

  // Nothing the page would write in the person's name leaves the browser,
  // however the document is written; reading and coming back do.
  it.each([
    'mutation { ReportAgentPresence(isVisible: true, idleSeconds: 0) }',
    '# a comment\nmutation { SetMailboxItemFlags(itemIds: ["a"], seen: true) }',
    'fragment Fields on RootMutation { CreateToken(name: "x") { secret } } mutation { ...Fields }',
  ])('does not send a change: %s', async (document) => {
    const fetchRequest = vi.fn()
    vi.stubGlobal('fetch', fetchRequest)
    setViewOnly(true)
    await expect(graphql(document)).rejects.toBeInstanceOf(APIError)
    expect(fetchRequest).not.toHaveBeenCalled()
  })

  it.each(['query { GetSession { username } }', 'mutation { EndImpersonation { username } }', 'mutation { Logout { username } }'])(
    'sends a read and the way back: %s',
    async (document) => {
      const fetchRequest = vi.fn().mockResolvedValue(Response.json({ data: {} }))
      vi.stubGlobal('fetch', fetchRequest)
      setViewOnly(true)
      await graphql(document)
      expect(fetchRequest).toHaveBeenCalledTimes(1)
    },
  )
})
