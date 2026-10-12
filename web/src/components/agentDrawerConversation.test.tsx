import { beforeEach, describe, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../api'
import { readConversationSnapshot } from './agentDrawer'

// The drawer asks the window about the screen when it loads, which jsdom
// does not answer.
vi.hoisted(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    }),
  })
})
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const snapshot = (conversationId: string) => ({
  ReadAgentConversation: { conversation: { id: conversationId }, messages: [], todos: [] },
  ListAgentInteractions: [],
})

// The drawer reopens the conversation this browser showed last, which may
// be another account's or deleted: it opens the main one instead, quietly.
describe('readConversationSnapshot', () => {
  beforeEach(() => vi.mocked(graphql).mockReset())

  it('opens the main conversation when the remembered one is not there', async () => {
    vi.mocked(graphql)
      .mockRejectedValueOnce(new APIError('api: not found'))
      .mockResolvedValueOnce(snapshot('main-conversation'))
    const read = await readConversationSnapshot('someone-elses', new AbortController().signal)
    expect(read.conversation.id).toBe('main-conversation')
    expect(vi.mocked(graphql).mock.calls[1][1]).toMatchObject({ conversationId: undefined })
  })

  it('still reports any other failure', async () => {
    vi.mocked(graphql).mockRejectedValueOnce(new APIError('api: forbidden'))
    await expect(readConversationSnapshot('a-conversation', new AbortController().signal)).rejects.toThrow('forbidden')
    expect(graphql).toHaveBeenCalledTimes(1)
  })

  it('does not retry when the main conversation itself is missing', async () => {
    vi.mocked(graphql).mockRejectedValueOnce(new APIError('api: not found'))
    await expect(readConversationSnapshot('', new AbortController().signal)).rejects.toThrow('not found')
    expect(graphql).toHaveBeenCalledTimes(1)
  })
})
