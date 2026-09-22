import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from './api'

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

    await expect(graphql('query { Session { authenticated } }')).rejects.toThrow('the server returned 503')
    expect(fetchRequest).toHaveBeenCalledTimes(1)
  })
})
