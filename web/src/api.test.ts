import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIError, AGENT_OPEN_EVENT, graphql, isNotFound, sendToAgentConversation } from './api'

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

describe('sendToAgentConversation', () => {
  function opened(conversationId: string, message: string) {
    const details: { conversationId: string; draft?: string; shouldSendDraft?: boolean }[] = []
    const listener = (event: Event) => details.push((event as CustomEvent).detail)
    window.addEventListener(AGENT_OPEN_EVENT, listener)
    sendToAgentConversation(conversationId, message)
    window.removeEventListener(AGENT_OPEN_EVENT, listener)
    return details
  }

  it('sends a message into the conversation it opens', () => {
    const [detail] = opened('chat-one', 'Plan the week.')
    expect(detail.draft).toBe('Plan the week.')
    expect(detail.shouldSendDraft).toBe(true)
  })

  it('only opens the conversation when there is no message, as for an idea that started a goal', () => {
    const [detail] = opened('goal-one', '')
    expect(detail.conversationId).toBe('goal-one')
    expect(detail.shouldSendDraft).toBeFalsy()
    expect(detail.draft).toBeFalsy()
  })
})
